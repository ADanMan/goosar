#!/usr/bin/env bash
# Hermetic test for scripts/egress-inventory.sh (#436).
#
# Sources the script for scan_hosts() and drives it over a fixture tree, so it
# needs neither the real repository nor the network. What it guards: the scan
# actually reaches source files (a scan that silently finds nothing is a green
# gate that checks nothing), the exclusions really exclude, and the placeholder
# filter does not swallow a real host.
#
# Usage: bash scripts/egress-inventory.test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck disable=SC1091
. "$repo_root/scripts/egress-inventory.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0
check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "  ok: $label"
  else
    echo "  FAIL: $label — expected '$expected', got '$actual'" >&2
    failures=$((failures + 1))
  fi
}

# --- fixture tree ---------------------------------------------------------
root="$work/repo"
mkdir -p "$root/server/internal/handler" "$root/server/internal/handler/testdata" \
  "$root/apps/web/app" "$root/node_modules/pkg" "$root/docs" "$root/scripts"

printf 'const u = "https://api.vendor.io/v1";\n' > "$root/apps/web/app/client.ts"
printf 'const url = "https://catalog.partner.ru/api"\n' > "$root/server/internal/handler/skill.go"
# Excluded: tests, testdata, docs, node_modules, and interpolation leftovers.
printf 'const t = "https://only-in-a-test.dev";\n' > "$root/server/internal/handler/skill_test.go"
printf 'https://only-in-testdata.dev\n' > "$root/server/internal/handler/testdata/fixture.go"
printf 'const d = "https://only-in-docs.dev";\n' > "$root/docs/guide.ts"
# NOT excluded: apps/docs is a shipped Next.js app whose components open
# connections from the reader's browser (video embeds).
mkdir -p "$root/apps/docs/components"
printf 'const v = "https://player.partner.io/embed";\n' > "$root/apps/docs/components/video.tsx"
printf 'const n = "https://only-in-node-modules.dev";\n' > "$root/node_modules/pkg/index.js"
printf 'const i = `https://${host}/path`;\n' > "$root/apps/web/app/interp.ts"
# Excluded: placeholder domains that fixtures and examples use on purpose.
printf 'const p = "https://api.example.com/x https://foo.test https://evil.co";\n' > "$root/apps/web/app/examples.ts"
# Not scanned at all: a file type the inventory does not cover.
printf 'https://only-in-json.dev\n' > "$root/apps/web/app/data.json"

found="$(scan_hosts "$root")"

echo "==> scan finds real hosts in source"
check "vendor host found" "yes" "$(printf '%s\n' "$found" | grep -qx 'api.vendor.io' && echo yes || echo no)"
check "partner host found" "yes" "$(printf '%s\n' "$found" | grep -qx 'catalog.partner.ru' && echo yes || echo no)"
check "apps/docs component host found" "yes" "$(printf '%s\n' "$found" | grep -qx 'player.partner.io' && echo yes || echo no)"

echo "==> scan excludes what it must"
for host in only-in-a-test.dev only-in-testdata.dev only-in-docs.dev \
  only-in-node-modules.dev only-in-json.dev api.example.com foo.test evil.co; do
  check "$host excluded" "no" "$(printf '%s\n' "$found" | grep -qx "$host" && echo yes || echo no)"
done
check "interpolation leftover dropped" "no" "$(printf '%s\n' "$found" | grep -qx '' && echo yes || echo no)"
check "exactly three hosts" "3" "$(printf '%s\n' "$found" | grep -c .)"

echo "==> output is deterministic and sorted"
check "second run identical" "same" "$([ "$found" = "$(scan_hosts "$root")" ] && echo same || echo different)"
check "sorted" "sorted" "$([ "$found" = "$(printf '%s\n' "$found" | sort)" ] && echo sorted || echo unsorted)"

echo "==> placeholder filter keeps real domains"
for host in api.github.com clawhub.ai us.i.posthog.com ghcr.io; do
  check "$host is not a placeholder" "no" "$(is_placeholder_host "$host" && echo yes || echo no)"
done
for host in api.example.com goosar.test host.invalid localhost evil.example; do
  check "$host is a placeholder" "yes" "$(is_placeholder_host "$host" && echo yes || echo no)"
done

echo "==> a ~host line documents a dependency's destination without joining the diff"
# The destination is real (the Resend SDK holds the host, not our source), so it
# must be written down; a grep can never find it, so it must not be diffed.
tilde="$work/tilde-allowlist.txt"
printf '# comment\napi.example-real.io\n~api.inside-a-dependency.io\n' > "$tilde"
check "literal host kept" "yes" \
  "$(ALLOWLIST="$tilde" allowlist_hosts | grep -qx 'api.example-real.io' && echo yes || echo no)"
check "~host excluded from the diff" "no" \
  "$(ALLOWLIST="$tilde" allowlist_hosts | grep -qx 'api.inside-a-dependency.io' && echo yes || echo no)"
check "~ prefix not merely stripped" "no" \
  "$(ALLOWLIST="$tilde" allowlist_hosts | grep -q 'inside-a-dependency' && echo yes || echo no)"

echo "==> the committed allowlist matches the committed source"
# The gate itself: if this fails, either a new host appeared or the allowlist
# drifted. Run against the real repository, which is why it is last.
check "--check passes on HEAD" "0" \
  "$(bash "$repo_root/scripts/egress-inventory.sh" --check > /dev/null 2>&1 && echo 0 || echo 1)"

echo "==> --check fails when a host is missing from the allowlist"
stale="$work/stale-allowlist.txt"
grep -v '^clawhub\.ai$' "$repo_root/scripts/egress-allowlist.txt" > "$stale"
check "stale allowlist rejected" "1" \
  "$(ALLOWLIST="$stale" bash -c '
      . "'"$repo_root"'/scripts/egress-inventory.sh"
      ALLOWLIST="'"$stale"'" main --check' > /dev/null 2>&1 && echo 0 || echo 1)"

if [ "$failures" -ne 0 ]; then
  echo "FAILED: $failures check(s)" >&2
  exit 1
fi
echo "All checks passed."
