#!/usr/bin/env bash
set -euo pipefail

# Generate the certificates docker-compose.selfhost.tls.yml needs to encrypt
# the backend -> PostgreSQL hop (#386).
#
#   bash scripts/selfhost-pg-tls.sh [output-dir]
#
# Writes ca.crt / ca.key / server.crt / server.key. The server certificate is
# issued for the compose service name "postgres" (CN + SAN), which is exactly
# the host the backend dials, so sslmode=verify-full validates against ca.crt.
#
# This is a private CA for one compose network, not a public PKI: the CA key
# stays next to the certificates it signed. Rotate by deleting the directory
# and re-running (then restart the stack).

OUT_DIR="${1:-certs}"
HOSTNAME_CN="${POSTGRES_TLS_HOST:-postgres}"
DAYS="${POSTGRES_TLS_DAYS:-825}"

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate PostgreSQL TLS certificates." >&2
  exit 1
fi

if [ -e "$OUT_DIR/server.key" ]; then
  echo "$OUT_DIR/server.key already exists — refusing to overwrite."
  echo "Delete $OUT_DIR and re-run to rotate the certificates."
  exit 1
fi

mkdir -p "$OUT_DIR"

openssl req -x509 -newkey rsa:4096 -sha256 -days "$DAYS" -nodes \
  -keyout "$OUT_DIR/ca.key" -out "$OUT_DIR/ca.crt" \
  -subj "/CN=Goosar selfhost PostgreSQL CA" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" >/dev/null 2>&1

openssl req -newkey rsa:2048 -sha256 -nodes \
  -keyout "$OUT_DIR/server.key" -out "$OUT_DIR/server.csr" \
  -subj "/CN=$HOSTNAME_CN" >/dev/null 2>&1

openssl x509 -req -in "$OUT_DIR/server.csr" -sha256 -days "$DAYS" \
  -CA "$OUT_DIR/ca.crt" -CAkey "$OUT_DIR/ca.key" -CAcreateserial \
  -out "$OUT_DIR/server.crt" \
  -extfile <(printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\n' "$HOSTNAME_CN") \
  >/dev/null 2>&1

rm -f "$OUT_DIR/server.csr" "$OUT_DIR/ca.srl"
chmod 600 "$OUT_DIR/ca.key" "$OUT_DIR/server.key"
chmod 644 "$OUT_DIR/ca.crt" "$OUT_DIR/server.crt"

echo "Wrote $OUT_DIR/{ca.crt,ca.key,server.crt,server.key} for host '$HOSTNAME_CN'."
echo
echo "Start the stack with TLS to Postgres:"
echo "  docker compose -f docker-compose.selfhost.yml \\"
echo "                 -f docker-compose.selfhost.tls.yml up -d"
