#!/usr/bin/env bash
# Take a consistent backup of a self-hosted Goosar deployment (#383).
#
# Two data stores are covered:
#   1. PostgreSQL      -> db.dump      (pg_dump custom format, via the postgres service)
#   2. backend uploads -> uploads.tar  (provisioning store + attachments volume)
#
# Everything is written into <out>/goosar-<UTC timestamp>.partial and renamed
# to its final name only after every part succeeded and was checksummed. A
# failing sub-step aborts the run loudly and leaves no directory behind, so a
# partial backup can never be mistaken for a good one.
#
# Usage:
#   bash scripts/backup.sh [--out DIR] [-f COMPOSE_FILE]
#
# Env overrides: COMPOSE (default "docker compose"), COMPOSE_FILE, BACKUP_DIR.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COMPOSE="${COMPOSE:-docker compose}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.selfhost.yml}"
OUT_DIR="${BACKUP_DIR:-$ROOT_DIR/backups}"

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT_DIR="$2"; shift 2 ;;
    -f | --file) COMPOSE_FILE="$2"; shift 2 ;;
    -h | --help)
      sed -n '2,17p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "backup.sh: unknown argument '$1'" >&2; exit 2 ;;
  esac
done

# Resolve --out against the caller's cwd before moving to the checkout root.
case "$OUT_DIR" in /*) ;; *) OUT_DIR="$PWD/$OUT_DIR" ;; esac
cd "$ROOT_DIR"

die() { echo "backup.sh: $*" >&2; exit 1; }

env_val() { sed -n "s/^$1=//p" .env 2>/dev/null | tail -n1 || true; }

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

file_bytes() { wc -c <"$1" | tr -d ' '; }

IMAGE_TAG="$(env_val GOOSAR_IMAGE_TAG)"; IMAGE_TAG="${IMAGE_TAG:-latest}"

# Database name and role come from the RUNNING postgres container, not from
# .env: that is the one place they cannot disagree with what the backend's
# DATABASE_URL points at (a COMPOSE override with --env-file, an edited .env
# without a restart, ...). restore.sh reads the target the same way.
PG_DB="$($COMPOSE -f "$COMPOSE_FILE" exec -T postgres printenv POSTGRES_DB | tr -d '[:space:]')" ||
  die "could not read POSTGRES_DB from the postgres container — is the stack running?"
PG_USER="$($COMPOSE -f "$COMPOSE_FILE" exec -T postgres printenv POSTGRES_USER | tr -d '[:space:]')" ||
  die "could not read POSTGRES_USER from the postgres container"
[ -n "$PG_DB" ] && [ -n "$PG_USER" ] || die "postgres container has no POSTGRES_DB/POSTGRES_USER — refusing to guess"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
CREATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
FINAL="$OUT_DIR/goosar-$STAMP"
WORK="$FINAL.partial"

[ -e "$FINAL" ] && die "$FINAL already exists — refusing to overwrite"
# db.dump is the whole database — auth tables, sessions, every issue and
# comment, and (without GOOSAR_MCP_SECRET_KEY) integration credentials in
# plaintext; uploads.tar carries the attachments and the workspace exports the
# server itself writes 0600. Under a normal umask the redirects below would
# create them 0644 in a 0755 directory, i.e. readable by every local account.
umask 077
rm -rf "$WORK"
mkdir -p "$WORK"

# Any failure below (set -e) hits this trap; the partial directory never
# survives a failed run.
cleanup_partial() { rm -rf "$WORK"; }
trap cleanup_partial EXIT

echo "==> Backing up to $FINAL"

# --- 1. Schema version ------------------------------------------------------
# Recorded so restore.sh can refuse a dump that is newer than the code it is
# being restored into. schema_migrations.version is TEXT holding the whole
# migration stem ("265_agent_mcp_server_server_index"), so the numeric prefix
# has to be extracted and compared as a number — a text MAX() would rank
# "99_x" above "265_x".
SCHEMA_VERSION="$($COMPOSE -f "$COMPOSE_FILE" exec -T postgres \
  psql -U "$PG_USER" -d "$PG_DB" -tAc "SELECT COALESCE(MAX(split_part(version, '_', 1)::bigint), 0) FROM schema_migrations")" ||
  die "could not read schema_migrations — is the stack running?"
SCHEMA_VERSION="$(printf '%s' "$SCHEMA_VERSION" | tr -d '[:space:]')"
case "$SCHEMA_VERSION" in
  '' | *[!0-9]*) die "unexpected schema version '$SCHEMA_VERSION'" ;;
esac
echo "    schema version: $SCHEMA_VERSION"

# --- 2. PostgreSQL ----------------------------------------------------------
echo "    dumping database '$PG_DB'..."
$COMPOSE -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U "$PG_USER" -d "$PG_DB" -Fc >"$WORK/db.dump" ||
  die "pg_dump failed"
[ -s "$WORK/db.dump" ] || die "pg_dump produced an empty file"

# --- 3. Uploads volume ------------------------------------------------------
# A throwaway `run` container (not `exec`) so the backup also works while the
# stack is stopped; --no-deps keeps it from starting postgres/frontend.
echo "    archiving uploads volume..."
$COMPOSE -f "$COMPOSE_FILE" run --rm --no-deps -T --entrypoint sh backend \
  -c 'tar -cf - -C /app/data/uploads .' >"$WORK/uploads.tar" ||
  die "uploads archive failed"
[ -s "$WORK/uploads.tar" ] || die "uploads archive is empty"

# --- 4. Manifest ------------------------------------------------------------
DB_SHA="$(sha256_of "$WORK/db.dump")"
UP_SHA="$(sha256_of "$WORK/uploads.tar")"
cat >"$WORK/manifest.json" <<JSON
{
  "format": 1,
  "created_at": "$CREATED_AT",
  "image_tag": "$IMAGE_TAG",
  "schema_version": $SCHEMA_VERSION,
  "postgres_db": "$PG_DB",
  "postgres_user": "$PG_USER",
  "files": {
    "db.dump": { "sha256": "$DB_SHA", "bytes": $(file_bytes "$WORK/db.dump") },
    "uploads.tar": { "sha256": "$UP_SHA", "bytes": $(file_bytes "$WORK/uploads.tar") }
  }
}
JSON

trap - EXIT
mv "$WORK" "$FINAL"

echo "==> Backup complete: $FINAL"
echo "    image tag $IMAGE_TAG, schema version $SCHEMA_VERSION"
echo "    restore with: bash scripts/restore.sh $FINAL --confirm"
