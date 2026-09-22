#!/usr/bin/env python3
"""Rename local Go variables and functions to their house Russian transliteration.

Usage:
    python3 scripts/translate-idents.py --dry-run <dir> [<dir> ...]
    python3 scripts/translate-idents.py --write <dir> [<dir> ...]

Only local variables and (unexported) functions/methods whose spelling
exactly matches a dictionary entry are renamed. Exported (capitalized)
identifiers, struct/interface field and method names, type names,
package-level var/const names, and package names are never touched.

This is a lexical, regex-based tool (stdlib only, no Go AST), so it is
conservative by design: a name is only renamed when it is unambiguously a
`:=` target, a local (indented) `var`/`const` name, or a function/method
name, AND it never appears anywhere in the same package as a struct field,
a type name, a package-level declaration, or a package/import identifier.
Any name with a single risky occurrence is left untouched everywhere in
that package. If a file still fails to build after a run, fix it by hand.
"""
import argparse
import re
import sys
from collections import defaultdict
from pathlib import Path

# Dictionary: English concept -> house Russian transliteration. Aliases
# (e.g. task/todo) map to the same value.
DICTIONARY = {
    "agent": "агент",
    "workspace": "рабочее_место",
    "deploy": "деплой",
    "runtime": "раннер",
    "task": "задача",
    "todo": "задача",
    "issue": "тикет",
    "member": "участник",
    "channel": "канал",
    "message": "сообщение",
    "msg": "сообщение",
    "session": "сессия",
    "sess": "сессия",
    "token": "токен",
    "profile": "профиль",
    "config": "конфиг",
    "cfg": "конфиг",
    "secret": "секрет",
    "credential": "доверие",
    "deployment": "накат",
    "provision": "подготовка",
    "provisioning": "подготовка",
    "bundle": "пакет",
    "artifact": "артефакт",
    "manifest": "манифест",
    "offline": "офлайн",
    "stand": "стенд",
    "daemon": "демон",
    "client": "клиент",
    "server": "сервер",
    "router": "роутер",
    "handler": "обработчик",
    "middleware": "фильтр",
    "store": "хранилище",
    "queue": "очередь",
    "cache": "кэш",
    "metrics": "метрики",
    "pricing": "тариф",
    "usage": "потребление",
    "billing": "биллинг",
    "license": "лицензия",
    "lic": "лицензия",
    "compliance": "соответствие",
    "perimeter": "периметр",
    "preflight": "проверка",
    "onboarding": "подключение",
    "offboarding": "уход",
    "audit": "журнал",
    "journal": "журнал",
    "policy": "политика",
}

# Comments and string/rune literals, blanked out before any structural
# analysis so identifiers inside them are never matched.
_MASK_RE = re.compile(
    r"/\*.*?\*/"
    r"|//[^\n]*"
    r"|`[^`]*`"
    r'|"(?:\\.|[^"\\])*"'
    r"|'(?:\\.|[^'\\])*'",
    re.DOTALL,
)

IDENT_RE = re.compile(r"[A-Za-z_]\w*")
PACKAGE_RE = re.compile(r"\bpackage\s+([A-Za-z_]\w*)")
TYPE_DECL_RE = re.compile(r"\btype\s+([A-Za-z_]\w*)")
TYPE_GROUP_OPEN_RE = re.compile(r"(?m)^[ \t]*type[ \t]*\(")
VAR_CONST_GROUP_OPEN_RE = re.compile(r"(?m)^([ \t]*)(var|const)[ \t]*\(")
VAR_CONST_SIMPLE_RE = re.compile(
    r"(?m)^([ \t]*)(?:var|const)[ \t]+((?:[A-Za-z_]\w*\s*,\s*)*[A-Za-z_]\w*)"
)
FUNC_DECL_RE = re.compile(r"\bfunc\s+([a-z_]\w*)\s*\(")
METHOD_DECL_RE = re.compile(r"\)\s*([a-z_]\w*)\s*\(")
# A field/parameter-shaped line: leading identifier immediately followed by
# what looks like the start of a type (struct fields, interface methods'
# neighbours, and multi-line function parameter lists all share this shape).
FIELD_LINE_RE = re.compile(r"(?m)^\t+([A-Za-z_]\w*)[ \t]+(?=[A-Za-z_*\[])")


def is_unexported(name):
    return bool(name) and (name[0].islower() or name[0] == "_")


def build_masked(src):
    """Return src with comment/string/rune contents blanked (same length/offsets)."""
    out = list(src)
    for m in _MASK_RE.finditer(src):
        start, end = m.span()
        for i in range(start, end):
            if out[i] != "\n":
                out[i] = " "
    return "".join(out)


def find_matching_close(masked, open_paren_pos):
    depth = 0
    i = open_paren_pos
    n = len(masked)
    while i < n:
        c = masked[i]
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return n


def leading_idents(line):
    m = re.match(r"\s*((?:[A-Za-z_]\w*\s*,\s*)*[A-Za-z_]\w*)", line)
    if not m:
        return []
    return [x.strip() for x in m.group(1).split(",") if x.strip()]


def collect_from_masked(masked, protected, renameable):
    m = PACKAGE_RE.search(masked)
    if m:
        protected.add(m.group(1))

    for m in TYPE_DECL_RE.finditer(masked):
        protected.add(m.group(1))

    for m in TYPE_GROUP_OPEN_RE.finditer(masked):
        open_pos = m.end() - 1
        close_pos = find_matching_close(masked, open_pos)
        for line in masked[open_pos + 1 : close_pos].split("\n"):
            protected.update(leading_idents(line))

    group_spans = []
    for m in VAR_CONST_GROUP_OPEN_RE.finditer(masked):
        top_level = m.group(1) == ""
        open_pos = m.end() - 1
        close_pos = find_matching_close(masked, open_pos)
        group_spans.append((m.start(), close_pos))
        for line in masked[open_pos + 1 : close_pos].split("\n"):
            for name in leading_idents(line):
                if top_level:
                    protected.add(name)
                elif is_unexported(name):
                    renameable.add(name)

    for m in VAR_CONST_SIMPLE_RE.finditer(masked):
        top_level = m.group(1) == ""
        names = [x.strip() for x in m.group(2).split(",") if x.strip()]
        for name in names:
            if top_level:
                protected.add(name)
            elif is_unexported(name):
                renameable.add(name)

    for m in FUNC_DECL_RE.finditer(masked):
        renameable.add(m.group(1))
    for m in METHOD_DECL_RE.finditer(masked):
        # Method names are always called through a selector (recv.Name()),
        # which the dot-guard in rewrite() always protects — renaming the
        # declaration here but never its call sites would break the build.
        # Protect method names outright instead of renaming them.
        protected.add(m.group(1))

    for m in FIELD_LINE_RE.finditer(masked):
        pos = m.start()
        if any(s <= pos < e for s, e in group_spans):
            continue
        protected.add(m.group(1))


def rewrite(src, masked, final_renameable):
    edits = []
    for m in IDENT_RE.finditer(masked):
        text = m.group()
        if text not in final_renameable:
            continue
        start, end = m.span()

        # Scan `masked`, not `src`, so a comment's prose (e.g. a trailing
        # sentence period right before the next line's code) can never be
        # mistaken for a real '.' selector or ':' key/label in the source.
        i = start - 1
        while i >= 0 and masked[i] in " \t\r\n":
            i -= 1
        if i >= 0 and masked[i] == ".":
            continue  # selector: package member, field, or method access

        j = end
        while j < len(masked) and masked[j] in " \t\r\n":
            j += 1
        if j < len(masked) and masked[j] == ":" and not (j + 1 < len(masked) and masked[j + 1] == "="):
            continue  # composite literal key or label

        edits.append((start, end, DICTIONARY[text]))

    if not edits:
        return src, []

    parts = []
    pos = 0
    for start, end, repl in edits:
        parts.append(src[pos:start])
        parts.append(repl)
        pos = end
    parts.append(src[pos:])
    return "".join(parts), edits


def find_packages(roots):
    """Group .go files by directory (Go package boundary is one directory)."""
    by_dir = defaultdict(list)
    for root in roots:
        root_path = Path(root)
        if root_path.is_file():
            by_dir[root_path.parent].append(root_path)
            continue
        for path in sorted(root_path.rglob("*.go")):
            by_dir[path.parent].append(path)
    return by_dir


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("paths", nargs="+", help="Go source directories (or files) to process")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true", help="report planned renames without writing")
    mode.add_argument("--write", action="store_true", help="apply the renames in place")
    args = parser.parse_args()

    by_dir = find_packages(args.paths)
    if not by_dir:
        print("No .go files found under: " + " ".join(args.paths), file=sys.stderr)
        return 1

    total_files_changed = 0
    total_edits = 0

    for directory, files in sorted(by_dir.items()):
        protected = set()
        renameable = set()
        parsed = {}
        for path in files:
            src = path.read_text(encoding="utf-8")
            masked = build_masked(src)
            parsed[path] = (src, masked)
            collect_from_masked(masked, protected, renameable)

        final_renameable = {n for n in renameable if n in DICTIONARY} - protected
        if not final_renameable:
            continue

        for path in files:
            src, masked = parsed[path]
            new_src, edits = rewrite(src, masked, final_renameable)
            if not edits:
                continue
            total_files_changed += 1
            total_edits += len(edits)
            renamed = sorted({repl for _, _, repl in edits})
            prefix = "[dry-run] " if args.dry_run else "[write] "
            print(f"{prefix}{path}: {len(edits)} occurrence(s) -> {', '.join(renamed)}")
            if args.write:
                path.write_text(new_src, encoding="utf-8")

    print(f"\nTotal: {total_files_changed} file(s), {total_edits} identifier occurrence(s).")
    if args.dry_run:
        print("Dry run only — re-run with --write to apply.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
