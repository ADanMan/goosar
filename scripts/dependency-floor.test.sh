#!/usr/bin/env bash
set -euo pipefail

# Version floors for every dependency trivy reported HIGH/CRITICAL against the
# v0.10.0 release images and source tree (#520). A `pnpm up` or a `go get` that
# silently walks one of them back re-opens the finding, and the next release
# only notices at the trivy gate in scripts/release-local.sh — after the images
# are built. This test is that gate, one second earlier.
#
#   bash scripts/dependency-floor.test.sh
#
# Floors are per major version: a package that ships fixes on several branches
# (brace-expansion, next, ws) is fine on any branch as long as that branch is
# patched. A major above every listed floor passes — upstream cannot have
# shipped the fix later than it shipped the major.
#
# No-fix findings are NOT listed here; they live in .trivyignore.yaml with a
# rationale, a path scope and an expiry, which is where trivy itself reads
# them. The checks at the bottom assert that shape — not which ids are in it.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FAILED=0

fail() {
  echo "FAIL: $*" >&2
  FAILED=1
}

# lt <a> <b> — true when semver a is strictly below b.
lt() {
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ]
}

# --- Go modules (backend image) ----------------------------------------------
# floor lines: <module> <minimum version, no leading v>
GO_FLOORS="
golang.org/x/crypto 0.55.0
golang.org/x/net 0.56.0
golang.org/x/text 0.39.0
github.com/go-jose/go-jose/v4 4.1.4
"

while read -r module floor; do
  [ -n "$module" ] || continue
  found="$(sed -n "s|^[[:space:]]*$module v\([0-9][^ ]*\).*|\1|p" server/go.mod | head -n1)"
  if [ -z "$found" ]; then
    fail "$module is not in server/go.mod — the floor line is stale"
    continue
  fi
  if lt "$found" "$floor"; then
    fail "server/go.mod: $module v$found < required v$floor (trivy HIGH/CRITICAL, #520)"
  fi
done <<<"$GO_FLOORS"

# --- npm packages (web image + source scan) ----------------------------------
# floor lines: <package> <major>:<minimum version> [<major>:<minimum> ...]
NPM_FLOORS="
next 15:15.5.24 16:16.3.3
tar 7:7.5.21
nanoid 3:3.3.18
brace-expansion 1:1.1.18 2:2.1.4 5:5.0.9
postcss 8:8.5.18
ip-address 10:10.3.1
picomatch 2:2.3.2 4:4.0.4
sharp 0:0.35.0
fast-uri 3:3.1.6
protobufjs 7:7.6.1
react-router 7:7.18.2
electron 39:39.8.10
ws 7:7.5.11 8:8.21.0
js-yaml 3:3.15.1 4:4.3.1
shell-quote 1:1.9.0
vite 8:8.0.16
path-to-regexp 6:6.3.0 8:8.4.0
browserslist 4:4.28.7
linkify-it 5:5.0.2
hono 4:4.12.25
builder-util-runtime 9:9.7.0
@xmldom/xmldom 0:0.9.12
@tiptap/core 3:3.30.5
"

# Every version pnpm resolved for a package, read off the lockfile keys.
locked_versions() {
  # pnpm-lock.yaml quotes a scoped package key ('@scope/name@1.2.3':) but
  # leaves an unscoped one bare (name@1.2.3:) -- match both, then strip the
  # quote/colon/parenthetical the quoted form leaves on the version. Under
  # set -e, a grep that finds zero matches (expected -- a package is only
  # ever one form or the other) must not abort the pipeline, so '|| true'
  # each side.
  { grep -oE "^  /?$1@[0-9]+\.[0-9]+\.[0-9]+[^:(']*" pnpm-lock.yaml || true
    grep -oE "^  '$1@[0-9]+\.[0-9]+\.[0-9]+[^']*" pnpm-lock.yaml || true; } |
    sed "s|.*$1@||" | sort -uV
}

while read -r package floors; do
  [ -n "$package" ] || continue
  versions="$(locked_versions "$package")"
  if [ -z "$versions" ]; then
    # Dropping a flagged dependency entirely is a valid fix; the floor line is
    # then dead weight, but not a failure.
    continue
  fi
  for version in $versions; do
    major="${version%%.*}"
    floor=""
    for entry in $floors; do
      [ "${entry%%:*}" = "$major" ] && floor="${entry#*:}"
    done
    if [ -z "$floor" ]; then
      # A major ABOVE every listed one is newer than every fix we know about.
      # A major BELOW the lowest listed one is a downgrade past the fix — the
      # floors say nothing about it precisely because nobody expected to see it.
      lowest_major=""
      for entry in $floors; do
        if [ -z "$lowest_major" ] || lt "${entry%%:*}" "$lowest_major"; then
          lowest_major="${entry%%:*}"
        fi
      done
      if lt "$major" "$lowest_major"; then
        fail "pnpm-lock.yaml: $package@$version is below every floor major ($floors) — no fixed release exists on that branch (trivy HIGH/CRITICAL, #520)"
      fi
      continue
    fi
    if lt "$version" "$floor"; then
      fail "pnpm-lock.yaml: $package@$version < required $floor (trivy HIGH/CRITICAL, #520)"
    fi
  done
done <<<"$NPM_FLOORS"

# --- web runtime OpenSSL ------------------------------------------------------
# libcrypto3/libssl3 3.5.7-r0 in the v0.10.0 web image came from the node:22
# alpine base. Upstream has not rebuilt that base yet — every node:22-alpine
# and node:24-alpine tag checked on 09.09.2026 still ships 3.5.7-r0, and there
# is no node:*-alpine3.25 tag — so the only fix that exists today is upgrading
# the two packages from the Alpine repository in the runtime stage, which is
# what the backend image already does for ca-certificates.
if ! grep -Eq 'apk upgrade .*libssl3' Dockerfile.web; then
  fail "Dockerfile.web runtime stage does not upgrade libssl3/libcrypto3 (trivy HIGH, #520)"
fi

# The packages are not version-pinned on the apk command line (a pin breaks the
# day Alpine ships 3.5.9), so the only thing standing between a mirror that
# answered with a stale index and a silently vulnerable image is the read-back
# comparison. Assert the floor it compares against is the one #520 requires.
DOCKERFILE_OPENSSL_FLOOR="3.5.8"
found_floor="$(sed -n 's/.*GOOSAR_OPENSSL_FLOOR="\([0-9][0-9.]*\)".*/\1/p' Dockerfile.web | head -n1)"
if [ -z "$found_floor" ]; then
  fail "Dockerfile.web does not read the installed libssl3 version back after apk upgrade — a mirror that served 3.5.7 would pass silently (trivy HIGH, #520)"
elif lt "$found_floor" "$DOCKERFILE_OPENSSL_FLOOR"; then
  fail "Dockerfile.web: GOOSAR_OPENSSL_FLOOR=$found_floor < required $DOCKERFILE_OPENSSL_FLOOR (trivy HIGH, #520)"
fi

# ...and that the step still fails the build when the read-back is below it.
# `|| echo WARN` on the whole step (the shape this started as) would swallow it.
if ! grep -q 'is below \$GOOSAR_OPENSSL_FLOOR after apk upgrade' Dockerfile.web; then
  fail "Dockerfile.web does not fail the build when apk upgrade leaves libssl3 below the floor (#520)"
fi

# An offline (perimeter) source build has no repository to reach, so it keeps
# the base image's 3.5.7-r0 and that HIGH stays open in THAT image. The build
# must not fail there — that would make an isolated source build impossible —
# so the only thing that can keep the gap honest is the documentation. Both
# places a reader lands must name the CVE and the way around it (carry the
# prebuilt release image), or the release notes' "clean scans" line is a lie
# for exactly the one image it does not cover.
if ! grep -q 'Сборка OpenSSL в закрытом контуре' SELF_HOSTING.md; then
  fail "SELF_HOSTING.md does not document that an offline source build keeps the base image OpenSSL"
fi
grep -q 'CVE-2026-14456' SELF_HOSTING.md ||
  fail "SELF_HOSTING.md does not name CVE-2026-14456 as what an offline source build leaves open"
grep -q 'carry the prebuilt release image' SELF_HOSTING.md ||
  fail "SELF_HOSTING.md states the gap without the instruction that avoids it (carry the prebuilt release image)"

grep -q 'SELF_HOSTING.md' docs/release-0.10.1/40-release-notes.md ||
  fail "docs/release-0.10.1/40-release-notes.md claims clean image scans without the perimeter OpenSSL caveat (#520)"
grep -q 'CVE-2026-14456' docs/release-0.10.1/40-release-notes.md ||
  fail "docs/release-0.10.1/40-release-notes.md does not list CVE-2026-14456 in the perimeter build as an accepted risk (#520)"
grep -q 'переносите готовый образ релиза' docs/release-0.10.1/40-release-notes.md ||
  fail "docs/release-0.10.1/40-release-notes.md does not tell the operator to carry the prebuilt release image instead of building in the perimeter (#520)"


# The runtime image serves a Next.js standalone bundle with `node server.js`
# and never installs anything, but the node base ships npm and corepack — and
# npm's own bundled tar/pacote/sigstore/brace-expansion/picomatch/ip-address
# accounted for every remaining HIGH in the v0.10.0 web image. Deleting the two
# package managers is what closes them; nothing in the image calls either.
if ! grep -q 'rm -rf /usr/local/lib/node_modules/npm' Dockerfile.web; then
  fail "Dockerfile.web runtime stage still ships npm and its vulnerable bundle (#520)"
fi

# --- no-fix findings ----------------------------------------------------------
# Findings with no fixed version upstream are suppressed in .trivyignore.yaml.
# The YAML form is the only one that can express the two things that keep a
# suppression honest: an expiry, so a stale entry fails this test instead of
# hiding a finding forever, and a path scope, so a build-time-only CVE is
# suppressed for the source (lockfile) scan and still reported the day it turns
# up in a shipped image.
#
# What is deliberately NOT asserted here: that specific CVE ids are present.
# That assertion pinned the suppressions in place — dropping an entry the day
# upstream ships a fix would have failed the test. The expiry is the forcing
# function instead. A cross-check of "this entry names a package that already
# meets its floor above" is not possible from the file either: trivy ignores
# are keyed by CVE id, not by package, so there is nothing to join on.
IGNORE_YAML=".trivyignore.yaml"
if [ ! -f "$IGNORE_YAML" ]; then
  fail "$IGNORE_YAML is missing — no-fix findings must be recorded in the scoped, expiring form (#520)"
fi
if [ -f .trivyignore ]; then
  fail "a plain .trivyignore is back: it carries neither expiry nor path scope, and trivy reads it for every target (#520)"
fi

# One line per entry: <id> <has-paths> <has-statement> <first-path> <expiry>.
if [ -f "$IGNORE_YAML" ]; then
  today="$(date -u +%Y-%m-%d)"
  entries="$(awk '
    function flush() {
      if (id != "") print id, has_paths, has_statement, (path == "" ? "-" : path), (expiry == "" ? "-" : expiry)
    }
    /^  - id:/ { flush(); id = $3; has_paths = 0; has_statement = 0; path = ""; expiry = ""; next }
    /^    paths:/ { has_paths = 1; next }
    /^    statement:/ { has_statement = 1; next }
    /^    expired_at:/ { expiry = $2; next }
    /^      - / { if (path == "") { path = $2; gsub(/"/, "", path) } next }
    END { flush() }
  ' "$IGNORE_YAML")"

  if [ -z "$entries" ]; then
    fail "$IGNORE_YAML has no parseable entries — an ignore file trivy silently accepts but does not apply is worse than none (#520)"
  fi

  while read -r id has_paths has_statement path expiry; do
    [ -n "$id" ] || continue
    [ "$has_statement" = 1 ] || fail "$IGNORE_YAML: $id has no statement — an unexplained suppression cannot be reviewed (#520)"
    if [ "$has_paths" != 1 ]; then
      fail "$IGNORE_YAML: $id has no paths scope — it would be suppressed in shipped images too (#520)"
    elif [ "$path" != "pnpm-lock.yaml" ]; then
      fail "$IGNORE_YAML: $id is scoped to '$path'; the accepted no-fix findings are build-time only and belong to the source scan (pnpm-lock.yaml) (#520)"
    fi
    if [ "$expiry" = "-" ]; then
      fail "$IGNORE_YAML: $id has no expired_at — a suppression without an expiry outlives the reason for it (#520)"
    elif ! [[ "$expiry" > "$today" ]]; then
      fail "$IGNORE_YAML: $id expired on $expiry — re-check upstream for a fixed release, then re-date or drop the entry (#520)"
    fi
  done <<<"$entries"
fi

# The scan step has to actually read that file, or the release gate reports the
# accepted findings again from a working directory nobody controls. Assert the
# flag on the real command lines: a `--ignorefile` sitting in a comment (the
# shape this check started as) proves nothing.
scan_commands() {
  sed 's/[[:space:]]*#.*$//' "$1" | awk '
    { line = line $0 }
    /\\[[:space:]]*$/ { sub(/\\[[:space:]]*$/, " ", line); next }
    { print line; line = "" }
  '
}
sbom_commands="$(scan_commands scripts/sbom-scan.sh)"
grep -Eq 'trivy image .*--ignorefile' <<<"$sbom_commands" ||
  fail "scripts/sbom-scan.sh: the trivy image scan does not pass --ignorefile (#520)"
grep -Eq 'trivy fs .*--ignorefile' <<<"$sbom_commands" ||
  fail "scripts/sbom-scan.sh: the trivy fs scan does not pass --ignorefile (#520)"
grep -q -- '\.trivyignore\.yaml' scripts/sbom-scan.sh ||
  fail "scripts/sbom-scan.sh points at a plain .trivyignore instead of the scoped .trivyignore.yaml (#520)"

if [ "$FAILED" = 0 ]; then
  echo "OK: every trivy-flagged dependency is at or above its fixed version"
fi
exit "$FAILED"
