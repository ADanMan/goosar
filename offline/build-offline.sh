#!/usr/bin/env bash
# Goosar — offline (perimeter) image build.
#
# Run on the ISOLATED machine, from the repository checkout that was copied
# together with offline/kit/ (produced by offline/make-kit.sh on a connected
# machine). Verifies the kit, loads the base images, unpacks the Go vendor
# tree, and builds the backend + web images with --network=none — the build
# itself is the perimeter acceptance test: any step that tries to reach the
# internet fails the build.
#
# Usage:
#   bash offline/build-offline.sh [options]
#
# Options:
#   --kit DIR             Kit directory (default: offline/kit)
#   --tag TAG              Image tag (default: offline-<short git rev from manifest>)
#   --verify-only           Verify kit checksums and exit
#   --skip-verify           Skip checksum verification. NOT recommended: the kit's
#                           integrity goes completely unverified. Only for debugging
#                           a kit you already trust.
#   --allow-network         Build without --network=none (debugging only)
#   --allow-rev-mismatch    Proceed even if the kit's recorded git_rev does not match
#                           this checkout's HEAD (see "Provenance" below)
#
# Requirements: docker with the default (docker) builder — a remote/container
# buildx driver cannot see docker-loaded local images.
#
# Provenance: manifest.txt records the git commit the kit was built from. If
# this checkout is at a different commit, the build refuses by default — a
# kit built from one commit and applied to a different checkout is exactly
# how a "verified" build ships unexpected code (issue #138). Pass
# --allow-rev-mismatch only once you have verified the mismatch is
# intentional (for example, deliberately re-applying an older kit).
#
# See SELF_HOSTING.md, «Закрытый контур (offline-поставка)», for the full flow.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

KIT_DIR="$ROOT_DIR/offline/kit"
TAG=""
VERIFY_ONLY=0
SKIP_VERIFY=0
ALLOW_REV_MISMATCH=0
NETWORK_FLAG="--network=none"

info() { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --kit)
      KIT_DIR="$(cd "${2:?--kit requires a value}" && pwd)"
      shift
      ;;
    --tag)
      TAG="${2:?--tag requires a value}"
      shift
      ;;
    --verify-only) VERIFY_ONLY=1 ;;
    --skip-verify) SKIP_VERIFY=1 ;;
    --allow-network) NETWORK_FLAG="" ;;
    --allow-rev-mismatch) ALLOW_REV_MISMATCH=1 ;;
    --help|-h)
      sed -n '2,33p' "$ROOT_DIR/offline/build-offline.sh" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1 (see --help)" >&2
      exit 1
      ;;
  esac
  shift
done

[ -d "$KIT_DIR" ] || fail "kit directory not found: $KIT_DIR (run offline/make-kit.sh on a connected machine first)"
[ -f "$KIT_DIR/manifest.txt" ] || fail "no manifest.txt in $KIT_DIR — incomplete kit?"

# sha256 tool portability (Linux sha256sum, macOS shasum)
if command -v sha256sum >/dev/null 2>&1; then
  SHA256_CHECK="sha256sum -c"
else
  SHA256_CHECK="shasum -a 256 -c"
fi

manifest_value() {
  grep -m1 "^$1=" "$KIT_DIR/manifest.txt" | cut -d= -f2-
}

# --- 1. Verify the kit -----------------------------------------------------
if [ "$SKIP_VERIFY" = "1" ]; then
  warn "Skipping kit checksum verification (--skip-verify). The kit's integrity is"
  warn "COMPLETELY UNVERIFIED past this point — checksums.txt ships inside the kit"
  warn "itself, so this also skips the only tamper check build-offline.sh has."
  warn "Only use this to debug a kit you already trust."
else
  [ -f "$KIT_DIR/checksums.txt" ] || fail "no checksums.txt in $KIT_DIR — incomplete kit?"
  # `sha256sum -c` only verifies entries that are PRESENT in checksums.txt, so a
  # tamperer can delete the line for a payload and swap the file underneath it
  # and still pass (issue #162). Before trusting the -c result, assert that
  # checksums.txt actually COVERS the payloads this script loads/unpacks — the
  # docker image tar and the Go vendor tree. `[./]*` tolerates the `./` prefix
  # `find .` writes into the kit's checksums.txt.
  require_kit_coverage() {
    grep -Eq "[[:space:]][./]*$1\$" "$KIT_DIR/checksums.txt" ||
      fail "checksums.txt does not cover $2 — refusing to build (a deleted checksum line would let a swapped payload pass verification)"
  }
  require_kit_coverage 'images/images\.tar' 'images/images.tar'
  require_kit_coverage 'go-vendor\.tar\.gz' 'go-vendor.tar.gz'
  info "Verifying kit checksums"
  (cd "$KIT_DIR" && $SHA256_CHECK --quiet checksums.txt) || fail "kit checksum verification FAILED — do not use this kit"
  info "Kit checksums OK"
fi

if [ "$VERIFY_ONLY" = "1" ]; then
  exit 0
fi

KIT_REV="$(manifest_value git_rev)"
KIT_PLATFORM="$(manifest_value platform)"
RUNTIME_IMAGE="$(manifest_value runtime_image)"
PG_IMAGE="$(manifest_value pg_image)"
[ -n "$KIT_REV" ] && [ -n "$RUNTIME_IMAGE" ] || fail "manifest.txt is missing git_rev/runtime_image"

# --- 2. Verify provenance (git_rev) -----------------------------------------
# Checked BEFORE the docker requirement below on purpose: a provenance
# mismatch should refuse immediately, not after paying for a docker check
# (or, worse, after images are already loading).
if command -v git >/dev/null 2>&1 && git -C "$ROOT_DIR" rev-parse HEAD >/dev/null 2>&1; then
  HEAD_REV="$(git -C "$ROOT_DIR" rev-parse HEAD)"
  if [ "$HEAD_REV" != "$KIT_REV" ]; then
    if [ "$ALLOW_REV_MISMATCH" = "1" ]; then
      warn "checkout is at $HEAD_REV but the kit was built from $KIT_REV — proceeding because --allow-rev-mismatch was passed."
      warn "THE IMAGES YOU ARE ABOUT TO BUILD MAY NOT MATCH THIS CHECKOUT. Only proceed if you have verified this is intentional."
    else
      fail "checkout is at $HEAD_REV but the kit was built from $KIT_REV — refusing to build: a kit built from one commit and applied to a different checkout can ship unexpected code (issue #138). Re-run offline/make-kit.sh from this checkout, or pass --allow-rev-mismatch once you have verified the mismatch is intentional."
    fi
  fi
fi

if [ -z "$TAG" ]; then
  TAG="offline-$(printf '%s' "$KIT_REV" | cut -c1-9)"
fi

# --- 3. docker required from here on ----------------------------------------
command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "docker daemon is not running"
cd "$ROOT_DIR"

# --- 4. Load images ----------------------------------------------------------
info "Loading docker images from the kit"
docker load -i "$KIT_DIR/images/images.tar"

# --- 5. Unpack the Go vendor tree -------------------------------------------
info "Unpacking server/vendor"
rm -rf server/vendor
tar -xzf "$KIT_DIR/go-vendor.tar.gz" -C server

# --- 6. Build images with no network -----------------------------------------
if [ -n "$NETWORK_FLAG" ]; then
  info "Building with $NETWORK_FLAG — a network-touching step fails the build (perimeter acceptance)"
else
  info "Building WITH network access (--allow-network)"
fi

VERSION_ARG="$TAG"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
info "Building goosar-backend:$TAG"
docker build $NETWORK_FLAG \
  -f Dockerfile \
  --build-arg GOOSAR_OFFLINE_BUILD=1 \
  --build-arg RUNTIME_IMAGE="$RUNTIME_IMAGE" \
  --build-arg VERSION="$VERSION_ARG" \
  --build-arg COMMIT="$KIT_REV" \
  --build-arg DATE="$BUILD_DATE" \
  -t "goosar-backend:$TAG" \
  .

info "Building goosar-web:$TAG"
docker build $NETWORK_FLAG \
  -f Dockerfile.web \
  --build-arg GOOSAR_OFFLINE_BUILD=1 \
  --build-arg NEXT_PUBLIC_APP_VERSION="$VERSION_ARG" \
  --build-arg COMMIT="$KIT_REV" \
  --build-arg DATE="$BUILD_DATE" \
  -t "goosar-web:$TAG" \
  .

# --- 7. Done ----------------------------------------------------------------
info "Offline images built"
echo ""
echo "  goosar-backend:$TAG"
echo "  goosar-web:$TAG"
echo "  $PG_IMAGE (loaded for docker compose)"
[ -n "$KIT_PLATFORM" ] && echo "  platform: $KIT_PLATFORM"
echo ""
echo "Deploy with docker-compose.selfhost.yml by pointing it at these images."
echo "In .env (alongside JWT_SECRET etc.):"
echo ""
echo "  GOOSAR_BACKEND_IMAGE=goosar-backend"
echo "  GOOSAR_WEB_IMAGE=goosar-web"
echo "  GOOSAR_IMAGE_TAG=$TAG"
echo "  GOOSAR_DELIVERY_PROFILE=perimeter"
echo ""
echo "Then: docker compose -f docker-compose.selfhost.yml up -d"
echo "See SELF_HOSTING.md, «Закрытый контур (offline-поставка)», for the acceptance checklist."
