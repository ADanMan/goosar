#!/usr/bin/env bash
# Goosar — offline (perimeter) build kit assembler.
#
# Run on a CONNECTED machine, from a checkout of the exact ref you intend to
# deploy inside the perimeter. Produces offline/kit/ — everything an isolated
# machine needs to build and run the self-host images without internet:
#
#   offline/kit/
#     images/images.tar        docker base images (+ derived runtime image)
#     go-vendor.tar.gz         server/ Go module vendor tree
#     context/pnpm-store/      pnpm store with every lockfile package
#     context/corepack/        corepack cache holding the pinned pnpm
#     context/fonts/           vendored Google Fonts + mocked-responses.js
#     manifest.txt             git rev, platform, image list
#     checksums.txt            sha256 of every kit file
#
# Usage:
#   bash offline/make-kit.sh [--platform linux/amd64|linux/arm64]
#
# The platform defaults to linux/amd64 (typical perimeter server). It must
# match the machine that will RUN the offline build; docker emulates foreign
# platforms during kit assembly if needed.
#
# Requirements on this machine: docker, git, node + a completed
# `pnpm install` (the font generator resolves next from apps/web).
#
# See SELF_HOSTING.md, «Закрытый контур (offline-поставка)», for the full flow.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

KIT_DIR="$ROOT_DIR/offline/kit"
PLATFORM="linux/amd64"

while [ $# -gt 0 ]; do
  case "$1" in
    --platform)
      PLATFORM="${2:?--platform requires a value}"
      shift
      ;;
    --help|-h)
      sed -n '2,28p' "$ROOT_DIR/offline/make-kit.sh" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1 (see --help)" >&2
      exit 1
      ;;
  esac
  shift
done

info() { printf '==> %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || fail "docker is required"
command -v git >/dev/null 2>&1 || fail "git is required"
command -v node >/dev/null 2>&1 || fail "node is required"
docker info >/dev/null 2>&1 || fail "docker daemon is not running"

# sha256 tool portability (macOS ships shasum, Linux sha256sum)
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi

GIT_REV="$(git rev-parse HEAD)"
if [ -n "$(git status --porcelain)" ]; then
  echo "WARNING: working tree is dirty — the kit will not exactly match $GIT_REV" >&2
fi

# Base image tags come from the Dockerfiles so they can never drift.
GO_IMAGE="$(grep -m1 '^ARG GO_IMAGE=' Dockerfile | cut -d= -f2)"
RUNTIME_BASE="$(grep -m1 '^ARG RUNTIME_IMAGE=' Dockerfile | cut -d= -f2)"
NODE_IMAGE="$(grep -m1 '^ARG NODE_IMAGE=' Dockerfile.web | cut -d= -f2)"
PG_IMAGE="$(grep -m1 'image: pgvector' docker-compose.selfhost.yml | awk '{print $2}')"
[ -n "$GO_IMAGE" ] && [ -n "$RUNTIME_BASE" ] && [ -n "$NODE_IMAGE" ] && [ -n "$PG_IMAGE" ] ||
  fail "could not derive base image tags from Dockerfile / Dockerfile.web / docker-compose.selfhost.yml"

# RUNTIME_BASE is now digest-pinned (issue #137): "alpine:3.21@sha256:…". Strip
# the "@sha256:…" suffix before taking the tag, or "${RUNTIME_BASE##*:}" would
# grab the digest hex instead of "3.21" (the last ":" in the string is the one
# before the digest, not the one before the tag).
RUNTIME_BASE_TAG="${RUNTIME_BASE%%@*}"
RUNTIME_IMAGE="goosar-offline-runtime:${RUNTIME_BASE_TAG##*:}"

info "Assembling offline kit for $PLATFORM at $GIT_REV"
rm -rf "$KIT_DIR"
mkdir -p "$KIT_DIR/images" "$KIT_DIR/context"

# --- 1. Base images -------------------------------------------------------
info "Pulling base images ($PLATFORM)"
for image in "$GO_IMAGE" "$NODE_IMAGE" "$RUNTIME_BASE" "$PG_IMAGE"; do
  docker pull --platform "$PLATFORM" "$image"
done

info "Building derived runtime image ($RUNTIME_IMAGE: $RUNTIME_BASE + ca-certificates/tzdata)"
printf 'FROM %s\nRUN apk add --no-cache ca-certificates tzdata\n' "$RUNTIME_BASE" |
  docker build --platform "$PLATFORM" -t "$RUNTIME_IMAGE" -f - "$KIT_DIR/images"

info "Saving images to images.tar"
docker save -o "$KIT_DIR/images/images.tar" \
  "$GO_IMAGE" "$NODE_IMAGE" "$PG_IMAGE" "$RUNTIME_IMAGE"

# --- 2. Go module vendor tree --------------------------------------------
info "Vendoring Go modules (in $GO_IMAGE)"
docker run --rm --platform "$PLATFORM" \
  -v "$ROOT_DIR/server":/src -w /src \
  -e HOME=/tmp -e GOMODCACHE=/tmp/gomodcache -e GOCACHE=/tmp/gocache \
  "$GO_IMAGE" \
  sh -c "go mod vendor && chown -R $(id -u):$(id -g) vendor"
tar -czf "$KIT_DIR/go-vendor.tar.gz" -C server vendor

# --- 3. pnpm store + corepack cache ---------------------------------------
# The repo is mounted read-only and `pnpm fetch` runs in a scratch workdir
# holding only the lockfile inputs: fetch needs nothing else, and this keeps
# pnpm away from the host checkout's node_modules (it would otherwise try to
# remove a platform-mismatched modules directory).
info "Fetching pnpm store + corepack cache (in $NODE_IMAGE)"
docker run --rm --platform "$PLATFORM" \
  -v "$ROOT_DIR":/repo:ro \
  -v "$KIT_DIR/context":/out \
  -e HOME=/tmp -e CI=true -e COREPACK_HOME=/out/corepack \
  "$NODE_IMAGE" \
  sh -c "
    mkdir -p /work && cd /work &&
    cp /repo/package.json /repo/pnpm-lock.yaml /repo/pnpm-workspace.yaml /repo/.npmrc . &&
    corepack enable &&
    corepack prepare \"\$(node -p 'require(\"./package.json\").packageManager')\" --activate &&
    pnpm fetch --store-dir /out/pnpm-store &&
    chown -R $(id -u):$(id -g) /out
  "

# --- 4. Vendored Google Fonts ---------------------------------------------
info "Vendoring Google Fonts (next/font payload)"
node offline/gen-font-kit.mjs \
  --out offline/kit/context/fonts \
  --prefix /offline/kit/context/fonts

# --- 5. Manifest + checksums ----------------------------------------------
info "Writing manifest and checksums"
{
  echo "git_rev=$GIT_REV"
  echo "platform=$PLATFORM"
  echo "created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go_image=$GO_IMAGE"
  echo "node_image=$NODE_IMAGE"
  echo "runtime_image=$RUNTIME_IMAGE"
  echo "pg_image=$PG_IMAGE"
} > "$KIT_DIR/manifest.txt"

# Kit file names are all machine-generated (no whitespace), so plain
# find|sort|xargs is safe and portable across BSD/GNU userlands.
(
  cd "$KIT_DIR"
  find . -type f ! -name checksums.txt | LC_ALL=C sort | xargs $SHA256 > checksums.txt
)

KIT_SIZE="$(du -sh "$KIT_DIR" | cut -f1)"
info "Offline kit ready: $KIT_DIR ($KIT_SIZE)"

# checksums.txt travels INSIDE the kit, so it is self-attesting only: whoever
# tampers with the kit payload can also recompute checksums.txt to match
# (issue #138). Printing its own sha256 here, to be carried to the isolated
# machine through a channel the kit itself did NOT travel through (chat,
# phone, ticket), is what turns "checksums.txt says the kit is intact" into
# an actual provenance check — see «Закрытый контур (offline-поставка)» in
# SELF_HOSTING.md.
CHECKSUMS_SHA256="$($SHA256 "$KIT_DIR/checksums.txt" | awk '{print $1}')"
echo ""
echo "=============================================================================="
echo "  checksums.txt sha256: $CHECKSUMS_SHA256"
echo ""
echo "  Carry this value to the isolated machine through a DIFFERENT channel than"
echo "  the kit itself, and compare it there BEFORE running build-offline.sh."
echo "  See «Закрытый контур (offline-поставка)» in SELF_HOSTING.md."
echo "=============================================================================="
echo ""
echo "Next steps:"
echo "  1. Copy this repository checkout (including offline/kit/) to the isolated machine."
echo "  2. There, run: bash offline/build-offline.sh"
echo "  See SELF_HOSTING.md, «Закрытый контур (offline-поставка)», for details."
