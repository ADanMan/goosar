#!/usr/bin/env bash
set -euo pipefail

# Tests for #386 — TLS on the backend -> PostgreSQL hop.
#
#   bash scripts/selfhost-pg-tls.test.sh
#
# Phase 1 is hermetic (`docker compose config` only): the base stack must keep
# sslmode=disable by default, must honour POSTGRES_SSLMODE, and the TLS overlay
# must render verify-full with a CA path.
#
# Phase 2 is a real handshake: generate certificates, boot the pgvector image
# with the overlay's ssl flags and connect with sslmode=verify-full. Skipped
# when SKIP_DOCKER_RUN=1. Everything is throwaway — its own network, random
# container names, no published host ports.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require() {
  local haystack=$1 needle=$2
  if ! grep -Fq "$needle" <<<"$haystack"; then
    echo "Missing expected value:"
    echo "  $needle"
    exit 1
  fi
}

refute() {
  local haystack=$1 needle=$2
  if grep -Fq "$needle" <<<"$haystack"; then
    echo "Unexpected value present:"
    echo "  $needle"
    exit 1
  fi
}

tmp_env="$(mktemp)"
trap 'rm -f "$tmp_env"' EXIT
# JWT_SECRET has no compose default; the example env ships empty on purpose.
{ cat .env.example; printf '\nJWT_SECRET=ci-synthetic-jwt-secret-for-compose-config-test\n'; } >"$tmp_env"

base_config="$(docker compose --env-file "$tmp_env" -f docker-compose.selfhost.yml config)"

# The shipped default must stay sslmode=disable: the bundled Postgres has no
# certificates, so flipping it would break every existing stand on upgrade.
require "$base_config" 'sslmode=disable'
# #384 guards, same file, cheap to assert here.
require "$base_config" 'no-new-privileges:true'
require "$base_config" 'read_only: true'

require_mode="$(
  POSTGRES_SSLMODE=require docker compose --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml config
)"
require "$require_mode" 'sslmode=require'
refute "$require_mode" 'sslmode=disable'

tls_config="$(
  docker compose --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml -f docker-compose.selfhost.tls.yml config
)"
require "$tls_config" 'sslmode=verify-full'
require "$tls_config" 'sslrootcert=/certs/ca.crt'
require "$tls_config" 'ssl_cert_file=/var/lib/postgresql/tls/server.crt'

echo "compose TLS config rendering ok"

if [ "${SKIP_DOCKER_RUN:-0}" = "1" ]; then
  echo "SKIP_DOCKER_RUN=1 — skipping the live TLS handshake."
  exit 0
fi

SUFFIX="$(date +%s)-$$"
NET="goosar-pgtls-$SUFFIX"
PG="goosar-pgtls-$SUFFIX"
CERT_DIR="$(mktemp -d)"

live_cleanup() {
  docker rm -f "$PG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  rm -rf "$CERT_DIR"
  rm -f "$tmp_env"
}
trap live_cleanup EXIT

bash scripts/selfhost-pg-tls.sh "$CERT_DIR" >/dev/null
for f in ca.crt ca.key server.crt server.key; do
  [ -s "$CERT_DIR/$f" ] || { echo "selfhost-pg-tls.sh did not write $f"; exit 1; }
done
# The server certificate must name the compose service the backend dials,
# otherwise verify-full fails at runtime instead of here.
openssl x509 -in "$CERT_DIR/server.crt" -noout -text | grep -q 'DNS:postgres' \
  || { echo "server.crt has no SAN DNS:postgres"; exit 1; }

PG_IMAGE="$(grep -m1 -oE 'pgvector/pgvector:pg17@sha256:[0-9a-f]+' docker-compose.selfhost.yml)"

# Boot with the EXACT command the overlay renders, not a copy of it — a
# transcription that drifts (or a YAML scalar that folds badly and drops the
# ssl flags) is precisely the failure this test exists to catch.
pg_command="$(
  docker compose --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml -f docker-compose.selfhost.tls.yml \
    config --format json |
  python3 -c '
import json, sys
cmd = json.load(sys.stdin)["services"]["postgres"]["command"]
if isinstance(cmd, list):
    if len(cmd) != 1:
        sys.exit("expected a single-element postgres command in the TLS overlay")
    cmd = cmd[0]
sys.stdout.write(cmd)
'
)"
case "$pg_command" in
  *"-c ssl=on"*) ;;
  *) echo "TLS overlay postgres command lost its ssl flags: $pg_command"; exit 1 ;;
esac

docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" --network-alias postgres \
  -e POSTGRES_DB=goosar -e POSTGRES_USER=goosar -e POSTGRES_PASSWORD=goosar \
  -v "$CERT_DIR:/certs:ro" \
  --entrypoint /bin/sh "$PG_IMAGE" -c "$pg_command" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$PG" pg_isready -U goosar -d goosar >/dev/null 2>&1; then break; fi
  sleep 1
done

out="$(
  docker run --rm --network "$NET" -v "$CERT_DIR:/certs:ro" \
    -e PGPASSWORD=goosar "$PG_IMAGE" \
    psql "postgresql://goosar@postgres:5432/goosar?sslmode=verify-full&sslrootcert=/certs/ca.crt" \
    -tAc "select ssl from pg_stat_ssl where pid = pg_backend_pid()" 2>&1
)" || { echo "verify-full connection failed:"; echo "$out"; docker logs "$PG" 2>&1 | tail -20; exit 1; }

case "$out" in
  *t*) ;;
  *) echo "connection was not encrypted: $out"; exit 1 ;;
esac

echo "postgres TLS handshake ok (sslmode=verify-full against the generated CA)"
