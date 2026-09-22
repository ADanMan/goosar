#!/usr/bin/env bash
# Tests scripts/stage-agent-artifacts.sh against a fake hermes-agent dist:
# a directory artifact, a pre-packed tarball, and two versions for one
# platform (checks the newest-wins + warning behavior). Runs without docker.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/stage-agent-artifacts.sh"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() {
  printf 'FAIL %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

DIST="$WORK/dist"
OUT="$WORK/out"
mkdir -p "$DIST" "$OUT"

# Directory artifact (darwin/x86_64 -> amd64), older version
mkdir -p "$DIST/hermes-0.13.0-darwin-x86_64/bin"
echo "old" > "$DIST/hermes-0.13.0-darwin-x86_64/bin/hermes"

# Directory artifact (darwin/x86_64 -> amd64), newer version (should win)
mkdir -p "$DIST/hermes-0.14.0-darwin-x86_64/bin"
echo "new" > "$DIST/hermes-0.14.0-darwin-x86_64/bin/hermes"

# Directory artifact (darwin/arm64), single version
mkdir -p "$DIST/hermes-0.14.0-darwin-arm64/bin"
echo "arm" > "$DIST/hermes-0.14.0-darwin-arm64/bin/hermes"

# Pre-packed tarball (linux/amd64), given as hermes-<ver>-<os>-<arch>.tar.gz
mkdir -p "$WORK/pack/hermes-0.14.0-linux-amd64/bin"
echo "linux" > "$WORK/pack/hermes-0.14.0-linux-amd64/bin/hermes"
tar -C "$WORK/pack" -czf "$DIST/hermes-0.14.0-linux-amd64.tar.gz" hermes-0.14.0-linux-amd64

STDERR="$WORK/stderr.log"
sh "$SCRIPT" "$DIST" "$OUT" 2>"$STDERR"

expect_file() {
  local f="$1"
  if [ -e "$OUT/$f" ]; then pass "produces $f"; else failed "produces $f (missing)"; fi
}

expect_file "hermes-darwin-amd64.tar.gz"
expect_file "hermes-darwin-arm64.tar.gz"
expect_file "hermes-linux-amd64.tar.gz"

# The stable name must be the newer version's payload, not the older one.
if tar -xzOf "$OUT/hermes-darwin-amd64.tar.gz" hermes-0.14.0-darwin-x86_64/bin/hermes 2>/dev/null | grep -q '^new$'; then
  pass "newest version wins for darwin-amd64"
else
  failed "newest version wins for darwin-amd64"
fi

if grep -q "multiple hermes-agent versions" "$STDERR"; then
  pass "warns about discarded older version"
else
  failed "warns about discarded older version"
fi

if [ -f "$OUT/checksums.txt" ] && grep -q "hermes-darwin-amd64.tar.gz" "$OUT/checksums.txt" \
  && grep -q "hermes-darwin-arm64.tar.gz" "$OUT/checksums.txt" \
  && grep -q "hermes-linux-amd64.tar.gz" "$OUT/checksums.txt"; then
  pass "checksums.txt lists stable names"
else
  failed "checksums.txt lists stable names"
fi

# Empty/missing dist dir must not fail and must not create checksums.txt.
EMPTY_OUT="$WORK/empty-out"
mkdir -p "$EMPTY_OUT" "$WORK/empty-dist"
if sh "$SCRIPT" "$WORK/empty-dist" "$EMPTY_OUT" >/dev/null 2>&1 && [ ! -e "$EMPTY_OUT/checksums.txt" ]; then
  pass "empty dist dir is a no-op"
else
  failed "empty dist dir is a no-op"
fi

if [ "$FAILURES" -eq 0 ]; then
  echo "All stage-agent-artifacts tests passed."
  exit 0
else
  echo "$FAILURES test(s) failed." >&2
  exit 1
fi
