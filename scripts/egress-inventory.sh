#!/usr/bin/env bash
# Outbound-connection inventory (#436).
#
# Greps the shipped source for `https://<host>` literals and diffs the result
# against a committed allowlist, so a NEW destination cannot reach a release
# without someone writing down what it is for. The prose that explains each host
# lives in SELF_HOSTING.md («Профиль доставки и исходящий трафик»)
# and docs/compliance/data-map.md; this file is only the machine-checkable half.
#
# Deliberately a grep and not a call graph: a host that appears in a string
# literal is a host a reviewer must look at, whether or not that particular line
# is reachable. False positives are cheap (one allowlist line saying "comment,
# not a call"); a missed destination is the failure this gate exists to prevent.
#
# Usage:
#   bash scripts/egress-inventory.sh            # print the hosts found
#   bash scripts/egress-inventory.sh --check    # fail on any drift vs the allowlist
#
# The script is safe to source: sourcing defines scan_hosts() and runs nothing.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="$REPO_ROOT/scripts/egress-allowlist.txt"

# Paths whose host literals are not product egress.
#
# Tests and fixtures name fake hosts on purpose. Docs and the changelog quote
# destinations that are already documented there. llm-presets.ts holds the
# operator-selectable LLM gateways: those are deployment data the operator picks
# in onboarding, and the outbound call they produce is the single
# `GOOSAR_LLM_BASE_URL` row of the egress table, not one row per preset.
is_excluded_path() {
  # apps/docs is a shipped Next.js app, not prose: its components open
  # connections from the reader's browser (the video embeds), so the */docs/*
  # rule below must not swallow the whole application. Its documentation is
  # .mdx, which the scan never reads anyway.
  case "$1" in
    */apps/docs/*.test.* | */apps/docs/*.spec.*) return 0 ;;
    */apps/docs/*) return 1 ;;
  esac
  case "$1" in
    */node_modules/* | */dist/* | */build/* | */.next/* | */out/* | */.git/*) return 0 ;;
    *_test.go | *.test.ts | *.test.tsx | *.test.mjs | *.test.js | *.test.sh) return 0 ;;
    *.spec.ts | *.spec.tsx | *_test.sh) return 0 ;;
    */__tests__/* | */testdata/* | */e2e/* | */__mocks__/*) return 0 ;;
    */docs/* | */vendored/* | */builtin_skills/*) return 0 ;;
    */llm-presets.ts) return 0 ;;
  esac
  return 1
}

# Hosts that carry no information: placeholder domains reserved for examples,
# and the leftovers of string interpolation (`https://${host}/x` greps as
# `https://`), which have no dot or no alphabetic TLD.
is_placeholder_host() {
  local host="$1"
  case "$host" in
    *.example | *.example.* | example.* | *.test | *.test.* | *.invalid | *.local \
      | *.localhost | localhost | *.internal | *.acme.* | *.corp.* | evil* | attacker*) return 0 ;;
  esac
  # Must look like a domain: at least one dot and an alphabetic TLD of 2+ chars.
  case "$host" in
    *.*) ;;
    *) return 0 ;;
  esac
  local tld="${host##*.}"
  case "$tld" in
    [a-z][a-z]*) [[ "$tld" =~ ^[a-z]+$ ]] || return 0 ;;
    *) return 0 ;;
  esac
  return 1
}

# scan_hosts <root> -> sorted unique host list on stdout.
#
# Known blind spots, deliberate and named so nobody reads a clean run as a
# complete perimeter: documentation URLs inside the embedded skill/template
# corpus (*/vendored/*, */builtin_skills/*, carried in .md and .json, shipped
# inside the server binary and handed to agents), and destinations expressed
# in .json / .toml / lock files.
# Those are documented by hand in the allowlist instead.
scan_hosts() {
  local root="$1" file host
  local -a files=()
  while IFS= read -r file; do
    is_excluded_path "$file" && continue
    files+=("$file")
  done < <(find "$root" \
    \( -name node_modules -o -name .git -o -name dist -o -name .next -o -name out \) -prune -o \
    -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.mjs' \
    -o -name '*.js' -o -name '*.sh' -o -name '*.yml' -o -name '*.yaml' \
    -o -name '*.ps1' \) -print | sort)

  [ ${#files[@]} -eq 0 ] && return 0

  grep -hoE 'https://[a-zA-Z0-9._-]+' "${files[@]}" 2>/dev/null \
    | sed 's|^https://||; s|\.$||' \
    | tr 'A-Z' 'a-z' \
    | sort -u \
    | while IFS= read -r host; do
        [ -n "$host" ] || continue
        is_placeholder_host "$host" && continue
        printf '%s\n' "$host"
      done
}

# The allowlist keeps a `#` comment above each host explaining what calls it.
# `~host` lines are destinations reached from inside a dependency: real, but
# invisible to a grep over this repository, so they are documentation only and
# never part of the diff.
allowlist_hosts() {
  sed 's/#.*//' "$ALLOWLIST" | tr -d '[:blank:]' | grep -v '^$' | grep -v '^~' | sort -u
}

main() {
  local found
  found="$(scan_hosts "$REPO_ROOT")"

  if [ "${1:-}" != "--check" ]; then
    printf '%s\n' "$found"
    return 0
  fi

  if [ ! -f "$ALLOWLIST" ]; then
    echo "egress-inventory: missing allowlist $ALLOWLIST" >&2
    return 1
  fi

  local diff_out
  if diff_out="$(diff <(allowlist_hosts) <(printf '%s\n' "$found") 2>&1)"; then
    echo "egress-inventory: OK — $(printf '%s\n' "$found" | grep -c .) hosts, all documented."
    return 0
  fi

  echo "egress-inventory: the set of outbound hosts in the source differs from" >&2
  echo "scripts/egress-allowlist.txt ('>' = new in the code, '<' = gone):" >&2
  printf '%s\n' "$diff_out" >&2
  echo >&2
  echo "A new host is a new destination for customer data. Add it to the" >&2
  echo "allowlist with a comment saying what calls it and how the operator" >&2
  echo "turns it off, and document it in SELF_HOSTING.md and" >&2
  echo "docs/compliance/data-map.md." >&2
  return 1
}

# Only run when executed, so the hermetic test can source the helpers.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
