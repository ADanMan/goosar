#!/usr/bin/env bash
# Goosar — save the deployment images for air-gapped transfer.
#
# The offline build kit (offline/make-kit.sh) rebuilds the images from source
# inside the perimeter. This script is the other supported variant from
# issue #68: when the perimeter CAN receive prebuilt release images (just not
# reach a public registry), export the exact images docker-compose.selfhost.yml
# resolves to — pinned by digest — into one tarball to carry across the gap.
#
# Run on a CONNECTED machine with the .env you will deploy with (it decides the
# GOOSAR_IMAGE_TAG / *_DIGEST the compose file resolves). Produces
# offline/kit-images/ :
#
#   offline/kit-images/
#     release-images.tar   docker save of postgres + backend + web
#     images.list          the exact image refs saved (digest-pinned)
#     checksums.txt        sha256 of the tarball
#
# Usage:
#   bash offline/save-release-images.sh [--env-file PATH] [--platform PLAT]
#
# --env-file defaults to ./.env; --platform defaults to linux/amd64 and must
# match the machine that will RUN the images inside the perimeter.
#
# On the perimeter side, load with offline/load-release-images.sh.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_DIR="$ROOT_DIR/offline/kit-images"
COMPOSE_FILE="$ROOT_DIR/docker-compose.selfhost.yml"
ENV_FILE="$ROOT_DIR/.env"
PLATFORM="linux/amd64"

while [ $# -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="${2:?--env-file requires a value}"; shift ;;
    --platform) PLATFORM="${2:?--platform requires a value}"; shift ;;
    --help|-h) sed -n '2,29p' "$ROOT_DIR/offline/save-release-images.sh" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $1 (see --help)" >&2; exit 1 ;;
  esac
  shift
done

info() { printf '==> %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "docker daemon is not running"
[ -f "$COMPOSE_FILE" ] || fail "compose file not found: $COMPOSE_FILE"
[ -f "$ENV_FILE" ] || fail "env file not found: $ENV_FILE (pass --env-file)"

# Resolve the exact image refs from the compose file under this .env. `config`
# performs the ${VAR} interpolation, so GOOSAR_IMAGE_TAG / *_DIGEST pins are
# reflected here exactly as the deployment will pull them.
info "Resolving image refs from $(basename "$COMPOSE_FILE")"
# Read into an array WITHOUT `mapfile`: that builtin is bash 4+, and macOS
# still ships bash 3.2 — a likely "connected machine" for this step, where
# mapfile would abort the script with a cryptic "command not found".
IMAGES=()
while IFS= read -r image; do
  [ -n "$image" ] && IMAGES+=("$image")
done < <(
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config 2>/dev/null \
    | awk '$1 == "image:" { print $2 }' | sort -u
)
[ "${#IMAGES[@]}" -gt 0 ] || fail "no images resolved from the compose file"

mkdir -p "$OUT_DIR"
: > "$OUT_DIR/images.list"
for image in "${IMAGES[@]}"; do
  info "Pulling $image ($PLATFORM)"
  docker pull --platform "$PLATFORM" "$image"
  printf '%s\n' "$image" >> "$OUT_DIR/images.list"
done

info "Saving ${#IMAGES[@]} images to release-images.tar"
docker save -o "$OUT_DIR/release-images.tar" "${IMAGES[@]}"

info "Writing checksums"
# sha256 tool portability: macOS ships `shasum`, Linux ships `sha256sum`.
# Without this fallback the script dies here on macOS (the "connected machine"
# for Step 0) under `set -euo pipefail`, after docker save already wrote the
# tarball — leaving a kit with no checksums.txt.
(
  cd "$OUT_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum release-images.tar images.list > checksums.txt
  else
    shasum -a 256 release-images.tar images.list > checksums.txt
  fi
)

info "Done. Copy offline/kit-images/ into the perimeter, then run:"
info "  bash offline/load-release-images.sh"
