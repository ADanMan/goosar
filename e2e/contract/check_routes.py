#!/usr/bin/env python3
"""Compares the route table registered in server/cmd/server/router.go against
docs/50-api-contract.yaml and reports any mismatch.

This is a clean-room verification tool: it may read server/cmd/server/router.go
(a spec author is allowed to), but the OpenAPI document it checks against must
never leak router.go's own structure back into the contract docs.

Usage:
    python3 e2e/contract/check_routes.py \
        [--router server/cmd/server/router.go] \
        [--openapi docs/50-api-contract.yaml]

Exit code is 0 when the two route sets match exactly, 1 otherwise.
"""
import argparse
import re
import sys
from dataclasses import dataclass, field

import yaml

HTTP_METHODS = ("Get", "Post", "Put", "Patch", "Delete", "Head", "Options")
OPENAPI_METHODS = {m.lower() for m in HTTP_METHODS}


# ---------------------------------------------------------------------------
# A minimal Go-source scanner: masks out the contents of string/rune/comment
# literals (replacing braces inside them with a neutral character) so brace
# depth can be computed correctly elsewhere in the file, while leaving the
# original text untouched for regex extraction of route calls.
# ---------------------------------------------------------------------------
def mask_strings_and_comments(src: str) -> str:
    out = list(src)
    i = 0
    n = len(src)
    while i < n:
        c = src[i]
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            # line comment
            j = src.find("\n", i)
            j = n if j == -1 else j
            for k in range(i, j):
                if out[k] not in "\n":
                    out[k] = " "
            i = j
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j == -1 else j + 2
            for k in range(i, min(j, n)):
                if out[k] != "\n":
                    out[k] = " "
            i = j
            continue
        if c == '"':
            j = i + 1
            while j < n and src[j] != '"':
                if src[j] == "\\":
                    j += 1
                j += 1
            j = min(j + 1, n)
            for k in range(i, j):
                if out[k] in "{}":
                    out[k] = "x"
            i = j
            continue
        if c == "`":
            j = src.find("`", i + 1)
            j = n if j == -1 else j + 1
            for k in range(i, j):
                if out[k] in "{}":
                    out[k] = "x"
            i = j
            continue
        if c == "'":
            # rune literal
            j = i + 1
            while j < n and src[j] != "'":
                if src[j] == "\\":
                    j += 1
                j += 1
            j = min(j + 1, n)
            for k in range(i, j):
                if out[k] in "{}":
                    out[k] = "x"
            i = j
            continue
        i += 1
    return "".join(out)


def find_matching_brace(masked: str, open_brace_pos: int) -> int:
    """Given the index of an opening '{' in the masked text, return the index
    of its matching closing '}'."""
    depth = 1
    i = open_brace_pos + 1
    n = len(masked)
    while i < n:
        if masked[i] == "{":
            depth += 1
        elif masked[i] == "}":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    raise ValueError("unbalanced braces starting at %d" % open_brace_pos)


@dataclass
class RouteBlock:
    prefix: str
    start: int  # position right after opening '{'
    end: int  # position of matching closing '}'


@dataclass
class RouteCall:
    method: str
    path_literal: str
    pos: int


ROUTE_RE = re.compile(
    r'\br\.Route\(\s*"((?:[^"\\]|\\.)*)"\s*,\s*func\(\s*r\s+chi\.Router\s*\)\s*\{',
)
METHOD_RE = re.compile(
    r'\.\s*(Get|Post|Put|Patch|Delete|Head|Options)\(\s*"((?:[^"\\]|\\.)*)"',
)


def unescape(s: str) -> str:
    return s.encode().decode("unicode_escape") if "\\" in s else s


def extract_routes(router_go_text: str):
    masked = mask_strings_and_comments(router_go_text)

    blocks: list[RouteBlock] = []
    for m in ROUTE_RE.finditer(router_go_text):
        prefix = unescape(m.group(1))
        open_brace_pos = m.end() - 1
        close_pos = find_matching_brace(masked, open_brace_pos)
        blocks.append(RouteBlock(prefix=prefix, start=open_brace_pos + 1, end=close_pos))

    calls: list[RouteCall] = []
    for m in METHOD_RE.finditer(router_go_text):
        calls.append(RouteCall(method=m.group(1), path_literal=unescape(m.group(2)), pos=m.start()))

    routes = set()
    details = []
    for call in calls:
        containing = [b for b in blocks if b.start <= call.pos < b.end]
        containing.sort(key=lambda b: b.start)
        full = "".join(b.prefix for b in containing) + call.path_literal
        full = re.sub(r"/{2,}", "/", full)
        if full != "/" and full.endswith("/"):
            full = full[:-1]
        if full == "":
            full = "/"
        method = call.method.upper()
        routes.add((method, full))
        details.append((method, full, call.pos))

    return routes, details


def normalize_template(path: str) -> str:
    """Replace every {param} segment with a single canonical placeholder so
    routes that use different parameter names for the same position (e.g.
    router.go's {id} vs a spec's {workspaceId}) still compare as equal. A
    chi wildcard segment (`/*`) is likewise normalized to the same
    placeholder as an OpenAPI path parameter, since both mean "the rest of
    the path is a value", just spelled differently."""
    path = re.sub(r"\{[^{}/]+\}", "{}", path)
    path = re.sub(r"/\*$", "/{}", path)
    return path


def load_openapi_routes(openapi_path: str):
    with open(openapi_path, encoding="utf-8") as f:
        doc = yaml.safe_load(f)
    routes = set()
    for path, methods in (doc.get("paths") or {}).items():
        if not isinstance(methods, dict):
            continue
        for method, op in methods.items():
            if method.lower() not in OPENAPI_METHODS:
                continue
            if not isinstance(op, dict):
                continue
            routes.add((method.upper(), path))
    return routes


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--router", default="server/cmd/server/router.go")
    ap.add_argument("--openapi", default="docs/50-api-contract.yaml")
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    with open(args.router, encoding="utf-8") as f:
        router_src = f.read()

    router_routes, details = extract_routes(router_src)
    openapi_routes = load_openapi_routes(args.openapi)

    router_norm = {(m, normalize_template(p)) for m, p in router_routes}
    openapi_norm = {(m, normalize_template(p)) for m, p in openapi_routes}

    missing_in_openapi = sorted(router_norm - openapi_norm)
    extra_in_openapi = sorted(openapi_norm - router_norm)

    print(f"router.go routes (excluding non-contract): {len(router_norm)}")
    print(f"OpenAPI routes: {len(openapi_norm)}")

    if args.verbose:
        print("\n-- router.go routes --")
        for m, p in sorted(router_norm):
            print(f"  {m:7s} {p}")

    ok = True
    if missing_in_openapi:
        ok = False
        print(f"\n{len(missing_in_openapi)} route(s) in router.go but NOT documented in OpenAPI:")
        for m, p in missing_in_openapi:
            print(f"  MISSING   {m:7s} {p}")
    if extra_in_openapi:
        ok = False
        print(f"\n{len(extra_in_openapi)} route(s) documented in OpenAPI but NOT in router.go:")
        for m, p in extra_in_openapi:
            print(f"  EXTRA     {m:7s} {p}")

    if ok:
        print("\nOK: router.go and docs/50-api-contract.yaml describe exactly the same route set.")
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
