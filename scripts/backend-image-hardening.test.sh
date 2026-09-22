#!/usr/bin/env bash
set -euo pipefail

# Runtime smoke check for #384: the backend image must actually start, run
# migrations and serve /health as an unprivileged user, with a read-only root
# filesystem, no-new-privileges and every capability dropped — the same
# settings docker-compose.selfhost.yml and the Helm chart apply.
#
#   bash scripts/backend-image-hardening.test.sh
#
# Everything it creates is throwaway and namespaced with a random suffix: its
# own network, its own Postgres container, no published host ports. It never
# touches an operator's running stack.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SUFFIX="$(date +%s)-$$"
NET="goosar-hardening-$SUFFIX"
PG="goosar-hardening-pg-$SUFFIX"
BE="goosar-hardening-backend-$SUFFIX"
IMAGE="${GOOSAR_BACKEND_TEST_IMAGE:-goosar-backend-hardening-test:$SUFFIX}"
BUILT_IMAGE=0

PG_IMAGE="$(grep -m1 -oE 'pgvector/pgvector:pg17@sha256:[0-9a-f]+' docker-compose.selfhost.yml)"

cleanup() {
  docker rm -f "$BE" "$PG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  if [ "$BUILT_IMAGE" = 1 ]; then
    docker image rm -f "$IMAGE" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "--- backend logs ---" >&2
  docker logs "$BE" 2>&1 | tail -40 >&2 || true
  exit 1
}

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "==> Building $IMAGE from Dockerfile..."
  docker build -f Dockerfile -t "$IMAGE" . >/dev/null
  BUILT_IMAGE=1
fi

# Static guard: the image itself must declare a non-root USER, so a plain
# `docker run` without compose/Helm settings is unprivileged too.
image_user="$(docker image inspect -f '{{.Config.User}}' "$IMAGE")"
[ -n "$image_user" ] || fail "image declares no USER (runs as root)"
echo "==> Image USER: $image_user"

echo "==> Starting throwaway Postgres ($PG_IMAGE)..."
docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" --network-alias postgres \
  -e POSTGRES_DB=goosar -e POSTGRES_USER=goosar -e POSTGRES_PASSWORD=goosar \
  "$PG_IMAGE" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$PG" pg_isready -U goosar -d goosar >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$PG" pg_isready -U goosar -d goosar >/dev/null 2>&1 \
  || { echo "FAIL: throwaway Postgres never became ready" >&2; exit 1; }

echo "==> Starting backend with the hardened runtime settings..."
docker run -d --name "$BE" --network "$NET" \
  --user 1001:1001 \
  --read-only --tmpfs /tmp \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  -v "$BE-uploads:/app/data/uploads" \
  -e DATABASE_URL="postgres://goosar:goosar@postgres:5432/goosar?sslmode=disable" \
  -e JWT_SECRET=hardening-smoke-not-a-real-secret \
  -e APP_ENV=production \
  -e PORT=8080 \
  "$IMAGE" >/dev/null

ready=0
for _ in $(seq 1 90); do
  if docker exec "$BE" wget -q -O - http://127.0.0.1:8080/health >/dev/null 2>&1; then
    ready=1
    break
  fi
  docker inspect -f '{{.State.Running}}' "$BE" 2>/dev/null | grep -q true \
    || fail "backend container exited before serving /health"
  sleep 1
done
[ "$ready" = 1 ] || fail "backend never served /health"

uid="$(docker exec "$BE" id -u)"
[ "$uid" = "1001" ] || fail "backend process runs as uid $uid, expected 1001"

# A read-only root filesystem is only real if a write outside the mounts fails.
if docker exec "$BE" sh -c 'touch /app/writable-probe' >/dev/null 2>&1; then
  fail "root filesystem is writable under --read-only"
fi
docker exec "$BE" sh -c 'touch /tmp/probe && rm /tmp/probe' \
  || fail "/tmp is not writable (multipart upload spooling would break)"
docker exec "$BE" sh -c 'touch /app/data/uploads/probe && rm /app/data/uploads/probe' \
  || fail "uploads volume is not writable by uid 1001"

body="$(docker exec "$BE" wget -q -O - http://127.0.0.1:8080/health)"
echo "==> /health: $body"

# The documented upgrade recipe (SELF_HOSTING.md) must work under the same
# cap_drop ALL that `compose run` inherits: root alone cannot chown there.
docker run --rm --user root --cap-drop ALL --cap-add CHOWN \
  -v "$BE-uploads:/app/data/uploads" --entrypoint sh "$IMAGE" \
  -c 'chown -R 1001:1001 /app/data/uploads' >/dev/null 2>&1 \
  || fail "documented chown recipe fails with --cap-drop ALL --cap-add CHOWN"

docker volume rm -f "$BE-uploads" >/dev/null 2>&1 || true
echo "backend image hardening ok (non-root uid 1001, read-only rootfs, /health served)"
