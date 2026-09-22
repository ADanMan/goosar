#!/usr/bin/env bash
# Hermetic test for the vendored-skill copy filter
# (scripts/sync-vendored-template-skills.sh). No git, no network: the script is
# sourced for its helpers and copy_skill_tree is driven over a fixture skill.
#
# What it guards (#435): upstream skill bundles are Apache-2.0 / MIT, and the
# snapshot under server/internal/agenttmpl/vendored/ is redistributed inside the
# server binary, the DMG and provisioning packages. Dropping LICENSE/NOTICE on
# the way in is a licence defect that only a legal review would find.
#
# Usage: bash scripts/sync-vendored-template-skills.test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck disable=SC1091
. "$repo_root/scripts/sync-vendored-template-skills.sh"

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

# --- fixture skill -------------------------------------------------------
src="$work/src/demo-skill"
mkdir -p "$src/references"
printf -- '---\nname: demo\n---\nbody\n' > "$src/SKILL.md"
printf 'Apache License 2.0 text\n' > "$src/LICENSE"
printf 'NOTICE text\n' > "$src/NOTICE"
printf 'MIT\n' > "$src/references/LICENSE.md"
printf 'reference\n' > "$src/references/guide.md"
printf 'PNG\n' > "$src/references/diagram.png"

dest="$work/dest/demo-skill"
copied="$(copy_skill_tree "$src" "$dest")"

echo "==> vendored copy keeps licences, drops binaries"
check "SKILL.md copied" "yes" "$([ -f "$dest/SKILL.md" ] && echo yes || echo no)"
check "LICENSE copied" "yes" "$([ -f "$dest/LICENSE" ] && echo yes || echo no)"
check "NOTICE copied" "yes" "$([ -f "$dest/NOTICE" ] && echo yes || echo no)"
check "nested LICENSE.md copied" "yes" "$([ -f "$dest/references/LICENSE.md" ] && echo yes || echo no)"
check "supporting file copied" "yes" "$([ -f "$dest/references/guide.md" ] && echo yes || echo no)"
check "binary asset dropped" "no" "$([ -f "$dest/references/diagram.png" ] && echo yes || echo no)"
check "copied count" "5" "$copied"
check "LICENSE text preserved" "Apache License 2.0 text" "$(cat "$dest/LICENSE")"

# --- the shape upstream actually has -------------------------------------
# anthropics/skills, vercel-labs/agent-skills and obra/superpowers-skills keep
# ONE LICENSE at the repository root and none inside skills/<name>/. A copy that
# walks only the skill directory therefore vendors no licence at all, which is
clone="$work/clone"
mkdir -p "$clone/skills/rootlic-skill"
printf 'Apache License 2.0 — repository root\n' > "$clone/LICENSE"
printf 'root notice\n' > "$clone/NOTICE"
printf 'not a licence\n' > "$clone/README.md"
printf -- '---\nname: rootlic\n---\nbody\n' > "$clone/skills/rootlic-skill/SKILL.md"

rootdest="$work/dest/rootlic-skill"
copy_skill_tree "$clone/skills/rootlic-skill" "$rootdest" > /dev/null
root_copied="$(copy_root_licenses "$clone" "$rootdest")"

echo "==> the repository-root licence reaches the vendored skill"
check "root LICENSE copied" "Apache License 2.0 — repository root" "$(cat "$rootdest/LICENSE" 2>/dev/null)"
check "root NOTICE copied" "root notice" "$(cat "$rootdest/NOTICE" 2>/dev/null)"
check "README not copied" "no" "$([ -f "$rootdest/README.md" ] && echo yes || echo no)"
check "root copy count" "2" "$root_copied"

# A licence inside the skill directory is the more specific one and must win.
check "skill licence not overwritten by the root one" "Apache License 2.0 text" \
  "$(copy_root_licenses "$clone" "$dest" > /dev/null; cat "$dest/LICENSE")"

echo "==> licence-file matcher"
for name in LICENSE license.txt LICENCE NOTICE notice.md COPYING COPYING.LESSER; do
  check "$name is a licence file" "yes" "$(is_license_basename "$name" && echo yes || echo no)"
done
for name in SKILL.md guide.md licensing-notes.md; do
  check "$name is not a licence file" "no" "$(is_license_basename "$name" && echo yes || echo no)"
done

if [ "$failures" -ne 0 ]; then
  echo "FAILED: $failures check(s)" >&2
  exit 1
fi
echo "All checks passed."
