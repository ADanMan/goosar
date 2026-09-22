#!/usr/bin/env bash
# Tests for the offline (perimeter) build kit: script syntax, the kit
# verification logic of build-offline.sh, gen-font-kit.mjs argument
# validation, and the Dockerfile guards that keep the online build path
# unchanged. Runs without docker and without network.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FAILURES=0

pass() { printf 'ok   %s\n' "$1"; }
failed() {
  printf 'FAIL %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

expect_success() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$name"; else failed "$name"; fi
}

expect_failure() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then failed "$name (expected non-zero exit)"; else pass "$name"; fi
}

# --- Script syntax ---------------------------------------------------------
expect_success "make-kit.sh parses" bash -n offline/make-kit.sh
expect_success "build-offline.sh parses" bash -n offline/build-offline.sh
expect_success "gen-font-kit.mjs parses" node --check offline/gen-font-kit.mjs
expect_success "save-release-images.sh parses" bash -n offline/save-release-images.sh
expect_success "load-release-images.sh parses" bash -n offline/load-release-images.sh

# --- Image digest pinning (issue #68) --------------------------------------
# postgres must be pinned by digest, not a bare mutable tag.
if grep -Eq '^\s*image: pgvector/pgvector:pg17@sha256:[0-9a-f]{64}\s*$' docker-compose.selfhost.yml; then
  pass "pgvector is digest-pinned in docker-compose.selfhost.yml"
else
  failed "pgvector is not digest-pinned (expected pgvector:pg17@sha256:<64 hex>)"
fi
# The release images must carry the optional digest-override suffix so an
# operator can pin the exact image the release tag resolved to. Empty default
# keeps repo:tag behavior — proven separately by scripts/selfhost-config.test.sh.
for var in GOOSAR_BACKEND_DIGEST GOOSAR_WEB_DIGEST; do
  if grep -q "\${${var}:-}" docker-compose.selfhost.yml; then
    pass "$var digest override wired into the compose image ref"
  else
    failed "$var digest override missing from docker-compose.selfhost.yml"
  fi
done

# --- Dockerfile guards -----------------------------------------------------
# The offline switch must default to 0 in every stage that consults it, so a
# build without --build-arg runs the online path. Base-image ARG defaults
# must stay the previously hardcoded tags (make-kit.sh derives its pull list
# from these same lines).
check_grep_count() {
  local name="$1" expected="$2" pattern="$3" file="$4"
  local actual
  actual="$(grep -c "$pattern" "$file" || true)"
  if [ "$actual" = "$expected" ]; then
    pass "$name"
  else
    failed "$name (expected $expected matches of '$pattern' in $file, got $actual)"
  fi
}

check_grep_count "Dockerfile: offline arg defaults to 0 in both stages" \
  2 '^ARG GOOSAR_OFFLINE_BUILD=0$' Dockerfile
check_grep_count "Dockerfile.web: offline arg defaults to 0 in cli/deps/builder" \
  3 '^ARG GOOSAR_OFFLINE_BUILD=0$' Dockerfile.web
check_grep_count "Dockerfile: GO_IMAGE default is a golang tag" \
  1 '^ARG GO_IMAGE=golang:' Dockerfile
check_grep_count "Dockerfile: RUNTIME_IMAGE default is an alpine tag" \
  1 '^ARG RUNTIME_IMAGE=alpine:' Dockerfile
check_grep_count "Dockerfile.web: GO_IMAGE default is a golang tag" \
  1 '^ARG GO_IMAGE=golang:' Dockerfile.web
check_grep_count "Dockerfile.web: NODE_IMAGE default is a node tag" \
  1 '^ARG NODE_IMAGE=node:' Dockerfile.web

# Dockerfile.web unconditionally copies offline/ into build stages, so the
# committed skeleton must exist even without a generated kit.
expect_success "offline/ skeleton exists for docker COPY" test -f offline/make-kit.sh

# The Go image tags of the two Dockerfiles must not drift apart: the kit
# ships exactly one golang image.
go_backend="$(grep -m1 '^ARG GO_IMAGE=' Dockerfile | cut -d= -f2)"
go_web="$(grep -m1 '^ARG GO_IMAGE=' Dockerfile.web | cut -d= -f2)"
if [ -n "$go_backend" ] && [ "$go_backend" = "$go_web" ]; then
  pass "GO_IMAGE tags match between Dockerfile and Dockerfile.web"
else
  failed "GO_IMAGE tags differ: Dockerfile='$go_backend' Dockerfile.web='$go_web'"
fi

# make-kit.sh derives the postgres image from the compose file — the grep
# must keep resolving to a non-empty image reference.
pg_image="$(grep -m1 'image: pgvector' docker-compose.selfhost.yml | awk '{print $2}')"
if [ -n "$pg_image" ]; then
  pass "pgvector image derivable from docker-compose.selfhost.yml"
else
  failed "pgvector image not derivable from docker-compose.selfhost.yml"
fi

# --- Base-image digest pinning (issue #137) --------------------------------
# Every base-image ARG default in both Dockerfiles must be pinned by digest —
# a mutable tag alone lets the same ref resolve to different content later.
# The regression this guards against: someone bumps a tag (e.g. alpine:3.22)
# without also refreshing the digest, silently un-pinning the image again.
check_digest_pinned() {
  local name="$1" arg="$2" file="$3"
  local value
  value="$(grep -m1 "^ARG ${arg}=" "$file" | cut -d= -f2-)"
  if printf '%s' "$value" | grep -Eq '@sha256:[0-9a-f]{64}$'; then
    pass "$name"
  else
    failed "$name (ARG $arg=$value in $file is not pinned by @sha256:<64 hex>)"
  fi
}

check_digest_pinned "Dockerfile: GO_IMAGE is digest-pinned" GO_IMAGE Dockerfile
check_digest_pinned "Dockerfile: RUNTIME_IMAGE is digest-pinned" RUNTIME_IMAGE Dockerfile
check_digest_pinned "Dockerfile.web: GO_IMAGE is digest-pinned" GO_IMAGE Dockerfile.web
check_digest_pinned "Dockerfile.web: NODE_IMAGE is digest-pinned" NODE_IMAGE Dockerfile.web

# make-kit.sh derives RUNTIME_IMAGE's tag from RUNTIME_BASE by stripping down
# to the last ":" — with a digest-pinned "alpine:3.21@sha256:…" value that
# must land on the human tag ("3.21"), not the digest hex. Exercise the exact
# shell expansion make-kit.sh uses so a future edit that breaks it fails here
# instead of only inside a full (docker-requiring) kit assembly.
runtime_base="$(grep -m1 '^ARG RUNTIME_IMAGE=' Dockerfile | cut -d= -f2)"
runtime_base_tag="${runtime_base%%@*}"
derived_runtime_tag="${runtime_base_tag##*:}"
if printf '%s' "$derived_runtime_tag" | grep -Eq '^[0-9]+\.[0-9]+$'; then
  pass "make-kit.sh RUNTIME_IMAGE tag derivation survives digest-pinned RUNTIME_BASE (got '$derived_runtime_tag')"
else
  failed "make-kit.sh RUNTIME_IMAGE tag derivation broke on digest-pinned RUNTIME_BASE (got '$derived_runtime_tag' from '$runtime_base')"
fi

# pgvector must be pinned to the SAME digest in docker-compose.selfhost.yml
# and deploy/helm/goosar/values.yaml — two delivery paths for one image must
# not drift apart.
compose_pg_digest="$(grep -m1 'image: pgvector' docker-compose.selfhost.yml |
  sed -n 's/.*@\(sha256:[0-9a-f]\{64\}\).*/\1/p')"
helm_pg_digest="$(grep -m1 'digest:' deploy/helm/goosar/values.yaml |
  sed -n 's/.*digest:[[:space:]]*\(sha256:[0-9a-f]\{64\}\).*/\1/p')"
if [ -n "$compose_pg_digest" ] && [ -n "$helm_pg_digest" ] && [ "$compose_pg_digest" = "$helm_pg_digest" ]; then
  pass "pgvector digest matches between compose and Helm values"
else
  failed "pgvector digest mismatch: compose='$compose_pg_digest' helm='$helm_pg_digest'"
fi

# --- Release image tag: no hand-pinned version anywhere (issues #157, #280) --
# A hand-edited version literal went five minor releases stale, so the pin no
# longer lives in the repo at all: .env.example ships GOOSAR_IMAGE_TAG empty,
# the compose default is `latest`, and scripts/selfhost-env.sh pins the newest
# release tag from the clone's git tags when it first creates .env (proven by
# scripts/selfhost-env.test.sh). Guard both halves of that invariant so a
# hardcoded tag cannot creep back and drift again.
compose_image_tags="$(grep -o 'GOOSAR_IMAGE_TAG:-[^}]*' docker-compose.selfhost.yml |
  sed 's/^GOOSAR_IMAGE_TAG:-//' | LC_ALL=C sort -u)"
env_image_tag="$(grep -m1 '^GOOSAR_IMAGE_TAG=' .env.example | cut -d= -f2-)"
if [ "$compose_image_tags" = "latest" ] && [ -z "$env_image_tag" ]; then
  pass "GOOSAR_IMAGE_TAG is unpinned (.env.example empty, compose default latest)"
else
  failed "GOOSAR_IMAGE_TAG pin crept back: compose default(s)='$(printf '%s' "$compose_image_tags" | tr '\n' ',')' .env.example='$env_image_tag'"
fi

# --- sha256sum has a shasum fallback in every offline script (issue #141) ----
# macOS ships `shasum`, not `sha256sum`. #142 fixed save-release-images.sh and
# #157 fixed load-release-images.sh; guard the whole family so no offline
# script reintroduces a bare `sha256sum` call that breaks on macOS. Invariant:
# any offline/*.sh that names sha256sum must also provide the `shasum -a 256`
# fallback.
for script in offline/*.sh; do
  name="$(basename "$script")"
  if grep -q 'sha256sum' "$script"; then
    if grep -q 'shasum -a 256' "$script"; then
      pass "$name pairs sha256sum with a shasum -a 256 fallback"
    else
      failed "$name calls sha256sum without a 'shasum -a 256' fallback (breaks on macOS)"
    fi
  fi
done

# --- build-offline.sh kit verification ------------------------------------
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi

tmp_kit="$(mktemp -d)"
trap 'rm -rf "$tmp_kit"' EXIT

mkdir -p "$tmp_kit/images" "$tmp_kit/context/fonts"
printf 'git_rev=0000000000000000000000000000000000000000\nplatform=linux/amd64\nruntime_image=goosar-offline-runtime:test\npg_image=pgvector/pgvector:pg17\n' \
  > "$tmp_kit/manifest.txt"
printf 'fake-image-tar\n' > "$tmp_kit/images/images.tar"
printf 'fake-go-vendor\n' > "$tmp_kit/go-vendor.tar.gz"
printf 'module.exports = {};\n' > "$tmp_kit/context/fonts/mocked-responses.js"
(
  cd "$tmp_kit"
  find . -type f ! -name checksums.txt | LC_ALL=C sort | xargs $SHA256 > checksums.txt
)

expect_success "verify-only passes on an intact kit" \
  bash offline/build-offline.sh --kit "$tmp_kit" --verify-only

# --- checksum-coverage guard (issue #162) ----------------------------------
# `sha256sum -c` only checks entries PRESENT in checksums.txt, so deleting a
# payload's line and swapping the file passes without a coverage guard. Prove
# the guard rejects a kit whose checksums.txt no longer covers images/images.tar
# even though every remaining line still verifies. Work on a copy so the pristine
# $tmp_kit stays intact for the tests below.
cov_kit="$(mktemp -d)"
cp -R "$tmp_kit/." "$cov_kit/"
# Drop the images.tar line, then swap the tar — the classic bypass.
grep -v 'images/images\.tar$' "$cov_kit/checksums.txt" > "$cov_kit/checksums.txt.tmp"
mv "$cov_kit/checksums.txt.tmp" "$cov_kit/checksums.txt"
printf 'swapped-malicious-tar\n' > "$cov_kit/images/images.tar"
cov_out="$(bash offline/build-offline.sh --kit "$cov_kit" --verify-only 2>&1)" && cov_status=0 || cov_status=$?
if [ "$cov_status" -ne 0 ] && printf '%s' "$cov_out" | grep -q "does not cover images/images.tar"; then
  pass "verify-only fails when the images.tar checksum line is deleted (coverage guard)"
else
  failed "verify-only fails when the images.tar checksum line is deleted (coverage guard) (status=$cov_status)"
  printf '%s\n' "$cov_out" >&2
fi
# The same guard must cover the Go vendor tree the build unpacks.
rm -rf "$cov_kit"
cov_kit="$(mktemp -d)"
cp -R "$tmp_kit/." "$cov_kit/"
grep -v 'go-vendor\.tar\.gz$' "$cov_kit/checksums.txt" > "$cov_kit/checksums.txt.tmp"
mv "$cov_kit/checksums.txt.tmp" "$cov_kit/checksums.txt"
cov_out="$(bash offline/build-offline.sh --kit "$cov_kit" --verify-only 2>&1)" && cov_status=0 || cov_status=$?
if [ "$cov_status" -ne 0 ] && printf '%s' "$cov_out" | grep -q "does not cover go-vendor.tar.gz"; then
  pass "verify-only fails when the go-vendor checksum line is deleted (coverage guard)"
else
  failed "verify-only fails when the go-vendor checksum line is deleted (coverage guard) (status=$cov_status)"
  printf '%s\n' "$cov_out" >&2
fi
rm -rf "$cov_kit"

# --- git_rev mismatch is a hard failure by default (issue #138) ------------
# The fake kit's git_rev is 40 zeros, which can never equal this checkout's
# real HEAD, so a full run (not --verify-only) must refuse before it ever
# reaches docker — these two assertions stay docker-independent because the
# rev check now runs before the "docker is required" check.
rev_status=0
rev_output="$(bash offline/build-offline.sh --kit "$tmp_kit" 2>&1)" || rev_status=$?
if [ "$rev_status" -ne 0 ] && printf '%s' "$rev_output" | grep -q "refusing to build"; then
  pass "build refuses a git_rev mismatch by default"
else
  failed "build refuses a git_rev mismatch by default (status=$rev_status)"
  printf '%s\n' "$rev_output" >&2
fi

# --allow-rev-mismatch must get PAST the rev gate with a loud warning instead
# of the hard failure above. What happens next depends on docker/the fake
# tarball being loadable in this environment, so this assertion only checks
# the rev gate itself, not the eventual exit status.
allow_output="$(bash offline/build-offline.sh --kit "$tmp_kit" --allow-rev-mismatch 2>&1 || true)"
if printf '%s' "$allow_output" | grep -q "proceeding because --allow-rev-mismatch" &&
  ! printf '%s' "$allow_output" | grep -q "refusing to build"; then
  pass "--allow-rev-mismatch proceeds past the git_rev gate with a warning"
else
  failed "--allow-rev-mismatch proceeds past the git_rev gate with a warning"
  printf '%s\n' "$allow_output" >&2
fi

printf 'tampered\n' >> "$tmp_kit/images/images.tar"
expect_failure "verify-only fails on a tampered kit" \
  bash offline/build-offline.sh --kit "$tmp_kit" --verify-only

rm "$tmp_kit/checksums.txt"
expect_failure "verify-only fails without checksums.txt" \
  bash offline/build-offline.sh --kit "$tmp_kit" --verify-only

expect_failure "verify-only fails on a missing kit directory" \
  bash offline/build-offline.sh --kit "$tmp_kit/does-not-exist" --verify-only

# --- gen-font-kit.mjs argument validation ----------------------------------
expect_failure "gen-font-kit rejects missing arguments" \
  node offline/gen-font-kit.mjs
expect_failure "gen-font-kit rejects a relative --prefix" \
  node offline/gen-font-kit.mjs --out "$tmp_kit/fonts-out" --prefix relative/path

# --- load-release-images.sh checksum-coverage guard (issue #162) -----------
# Same bypass as build-offline.sh: `$SHA256 -c` only checks lines that exist,
# so deleting the release-images.tar line and swapping the tar passes. The
# guard now runs BEFORE the docker requirement, so it is exercisable here with
# no docker daemon.
img_kit="$(mktemp -d)"
printf 'fake-release-tar\n' > "$img_kit/release-images.tar"
printf 'pgvector/pgvector:pg17\n' > "$img_kit/images.list"
(
  cd "$img_kit"
  $SHA256 release-images.tar images.list > checksums.txt
)

# An intact kit must get PAST the coverage guard (it may still fail later at the
# docker requirement, but never with a coverage error).
intact_out="$(bash offline/load-release-images.sh --kit-dir "$img_kit" 2>&1 || true)"
if printf '%s' "$intact_out" | grep -q "does not cover"; then
  failed "load-release-images.sh coverage guard rejects an intact kit"
  printf '%s\n' "$intact_out" >&2
else
  pass "load-release-images.sh coverage guard passes an intact kit"
fi

# Delete the tar's checksum line and swap the tar — must FAIL at the guard.
grep -v 'release-images\.tar$' "$img_kit/checksums.txt" > "$img_kit/checksums.txt.tmp"
mv "$img_kit/checksums.txt.tmp" "$img_kit/checksums.txt"
printf 'swapped-malicious-tar\n' > "$img_kit/release-images.tar"
img_out="$(bash offline/load-release-images.sh --kit-dir "$img_kit" 2>&1)" && img_status=0 || img_status=$?
if [ "$img_status" -ne 0 ] && printf '%s' "$img_out" | grep -q "does not cover release-images.tar"; then
  pass "load-release-images.sh fails when the release-images.tar checksum line is deleted"
else
  failed "load-release-images.sh fails when the release-images.tar checksum line is deleted (status=$img_status)"
  printf '%s\n' "$img_out" >&2
fi
rm -rf "$img_kit"

# --- load-release-images.sh strict verification (issue #167) ---------------
# Review found the coverage guard above is itself bypassable two ways:
#
#   1. Path-traversal decoy: the guard regex accepts any checksums.txt line
#      ending in "release-images.tar", including "../release-images.tar". A
#      kit can list/verify a decoy file next to the real tar so both the grep
#      coverage check and `$SHA256 -c` pass, while the real (unverified) tar
#      in $KIT_DIR still gets docker-loaded.
#   2. Non-strict verify: `$SHA256 -c` without --strict only warns (exit 0)
#      on a malformed checksum line, e.g. a too-short "digest" like
#      "deadbeef release-images.tar" — that line satisfies the coverage grep
#      and is silently ignored by `-c`, so a tampered tar with no real digest
#      on record still loads.
#
# Both must be hard failures before docker load ever runs.

# 1) Path-traversal decoy: checksums.txt covers "../release-images.tar" (a
#    sibling file outside $KIT_DIR) instead of the real payload inside it.
trav_root="$(mktemp -d)"
trav_kit="$trav_root/kit"
mkdir -p "$trav_kit"
printf 'malicious-real-tar\n' > "$trav_kit/release-images.tar"
printf 'decoy-content\n' > "$trav_root/release-images.tar"
decoy_hash="$( (cd "$trav_root" && $SHA256 release-images.tar) | awk '{print $1}')"
printf '%s  ../release-images.tar\n' "$decoy_hash" > "$trav_kit/checksums.txt"
# This environment happens to have a real docker daemon, and our fixture tar
# is not a valid tarball, so `docker load` would ALSO fail on its own — a
# nonzero exit alone would not prove the checksum stage caught the decoy.
# The real assertion: the script must refuse BEFORE it ever reaches "Loading
# images" / docker load, i.e. the checksum stage itself must be the reason
# for the failure, not an incidental docker/tar-format error downstream.
trav_out="$(bash offline/load-release-images.sh --kit-dir "$trav_kit" 2>&1)" && trav_status=0 || trav_status=$?
if [ "$trav_status" -ne 0 ] && ! printf '%s' "$trav_out" | grep -q "Loading images"; then
  pass "load-release-images.sh rejects a path-traversal decoy checksum entry (../release-images.tar) before docker load"
else
  failed "load-release-images.sh rejects a path-traversal decoy checksum entry (../release-images.tar) before docker load (status=$trav_status)"
  printf '%s\n' "$trav_out" >&2
fi
rm -rf "$trav_root"

# 2) Malformed digest: a too-short "hash" for release-images.tar must be
#    rejected outright, not silently skipped with exit 0.
mal_kit="$(mktemp -d)"
printf 'real-tar-content\n' > "$mal_kit/release-images.tar"
printf 'deadbeef release-images.tar\n' > "$mal_kit/checksums.txt"
# Same reasoning as the path-traversal case above: assert the checksum stage
# itself refused, not merely that the run ended nonzero for an incidental
# reason further down the script.
mal_out="$(bash offline/load-release-images.sh --kit-dir "$mal_kit" 2>&1)" && mal_status=0 || mal_status=$?
if [ "$mal_status" -ne 0 ] && ! printf '%s' "$mal_out" | grep -q "Loading images"; then
  pass "load-release-images.sh rejects a malformed (too-short) checksum digest before docker load"
else
  failed "load-release-images.sh rejects a malformed (too-short) checksum digest before docker load (status=$mal_status)"
  printf '%s\n' "$mal_out" >&2
fi
rm -rf "$mal_kit"

# 3) Happy path: a correctly-produced kit (exact filename, full digest) must
#    still verify and get past the checksum stage cleanly.
good_kit="$(mktemp -d)"
printf 'real-release-tar\n' > "$good_kit/release-images.tar"
printf 'pgvector/pgvector:pg17\n' > "$good_kit/images.list"
( cd "$good_kit" && $SHA256 release-images.tar images.list > checksums.txt )
good_out="$(bash offline/load-release-images.sh --kit-dir "$good_kit" 2>&1)" && good_status=0 || good_status=$?
# No docker daemon is required for this repository's default test environment,
# so the run may still fail later at "docker is required" / "docker daemon is
# not running" — that is fine. It must never fail at the checksum stage.
if printf '%s' "$good_out" | grep -Eq "does not cover|malformed"; then
  failed "load-release-images.sh happy path passes checksum verification"
  printf '%s\n' "$good_out" >&2
else
  pass "load-release-images.sh happy path passes checksum verification"
fi
rm -rf "$good_kit"

if [ "$FAILURES" -gt 0 ]; then
  echo "$FAILURES offline-kit test(s) failed" >&2
  exit 1
fi
echo "All offline-kit tests passed"
