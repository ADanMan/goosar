#!/usr/bin/env bash
# Goosar — load air-gapped deployment images (perimeter side).
#
# Counterpart to offline/save-release-images.sh (issue #68). Run inside the
# perimeter after copying offline/kit-images/ across the gap. Verifies the
# tarball against its checksum, then `docker load`s it so
# `docker compose -f docker-compose.selfhost.yml up -d` finds every image
# locally without touching a registry.
#
# Usage:
#   bash offline/load-release-images.sh [--kit-dir DIR]
#
# --kit-dir defaults to offline/kit-images.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIT_DIR="$ROOT_DIR/offline/kit-images"

while [ $# -gt 0 ]; do
  case "$1" in
    --kit-dir) KIT_DIR="${2:?--kit-dir requires a value}"; shift ;;
    --help|-h) sed -n '2,15p' "$ROOT_DIR/offline/load-release-images.sh" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $1 (see --help)" >&2; exit 1 ;;
  esac
  shift
done

info() { printf '==> %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# sha256 tool portability (macOS ships shasum, Linux sha256sum) — same fallback
# as offline/make-kit.sh and offline/save-release-images.sh (issue #141).
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi

[ -d "$KIT_DIR" ] || fail "kit dir not found: $KIT_DIR (pass --kit-dir)"
[ -f "$KIT_DIR/release-images.tar" ] || fail "release-images.tar missing in $KIT_DIR"
[ -f "$KIT_DIR/checksums.txt" ] || fail "checksums.txt missing in $KIT_DIR (refusing to load an unverified tar)"

# `$SHA256 -c` only verifies entries that are PRESENT in checksums.txt, so
# deleting the release-images.tar line and swapping the tar underneath it still
# exits 0 (issue #162). Assert the checksums file actually COVERS the payload we
# are about to docker-load before trusting the -c result. This whole block runs
# BEFORE the docker requirement so a tampered kit is rejected even where docker
# is absent — and so the coverage guard is testable without a docker daemon.
#
# The coverage check itself was found bypassable two ways (issue #167):
#   1. A checksums.txt line for "../release-images.tar" (or any path prefix)
#      still satisfies a "does the file end in release-images.tar" grep, so a
#      decoy file next to the real tar can be listed/verified instead of the
#      real payload in $KIT_DIR.
#   2. `$SHA256 -c` without --strict only warns (exit 0) on a malformed line,
#      e.g. a too-short "digest" — the line still counts as "coverage" but
#      never actually verifies anything.
# require_exact_checksum_entry closes both: it demands a checksums.txt line
# whose filename field is EXACTLY the expected basename (no leading path,
# no "..", no extra characters) and whose digest is a full 64-hex sha256.
require_exact_checksum_entry() {
  local expected_name="$1" entry digest
  # Match the filename field for EXACT equality (not suffix), so
  # "../release-images.tar" or "sub/release-images.tar" cannot satisfy the
  # check for "release-images.tar". We deliberately do NOT strip a leading
  # "*" binary-mode marker: save-release-images.sh always writes GNU/BSD
  # text-mode records (two spaces, bare name), so a "*"-prefixed field is
  # never legitimate here — accepting it would conflate the mode marker with
  # a real name and let a crafted "*release-images.tar" decoy line match.
  # A binary-mode entry simply fails to match and the kit is rejected.
  entry="$(awk -v name="$expected_name" '
    { if ($2 == name) { print; exit } }
  ' "$KIT_DIR/checksums.txt")"
  [ -n "$entry" ] ||
    fail "checksums.txt does not cover $expected_name — refusing to load (a deleted, renamed, or path-prefixed checksum line would let a swapped file pass verification)"
  digest="$(printf '%s\n' "$entry" | awk '{print $1}')"
  printf '%s' "$digest" | grep -Eq '^[0-9a-f]{64}$' ||
    fail "checksums.txt has a malformed sha256 digest for $expected_name — refusing to load"
}

require_exact_checksum_entry "release-images.tar"
# images.list is informational, but when present it must be covered too so it
# cannot be swapped for a misleading manifest of what was loaded.
[ -f "$KIT_DIR/images.list" ] && require_exact_checksum_entry "images.list"

info "Verifying checksums"
# --strict rejects any remaining malformed line outright instead of the
# default "print a warning and exit 0" behavior (issue #167). Both GNU
# coreutils sha256sum and the macOS/Perl `shasum` fallback support --strict.
( cd "$KIT_DIR" && $SHA256 -c --strict checksums.txt ) || fail "checksum mismatch — the kit is corrupt or tampered; do not load it"

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "docker daemon is not running"

info "Loading images"
docker load -i "$KIT_DIR/release-images.tar"

info "Loaded:"
[ -f "$KIT_DIR/images.list" ] && sed 's/^/    /' "$KIT_DIR/images.list"
info "Now deploy: docker compose -f docker-compose.selfhost.yml up -d"
