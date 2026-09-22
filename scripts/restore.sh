#!/usr/bin/env bash
# Restore a self-hosted Goosar deployment from a scripts/backup.sh
# directory (#383).
#
# This is DESTRUCTIVE: the database is dropped and recreated and the uploads
# volume is emptied before the archive is unpacked. Nothing happens without
# --confirm.
#
# Safety gates, all checked before anything is touched:
#   * manifest.json present and parseable, both payload files present;
#   * sha256 of db.dump and uploads.tar match the manifest;
#   * the backup's schema version is not newer than the newest migration in
#     the backend image the stack runs (restoring forward is fine — the backend
#     migrates on start — restoring a newer dump into older code is not);
#   * --confirm given.
#
# Usage:
#   bash scripts/restore.sh <backup-dir> --confirm [-f COMPOSE_FILE]
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COMPOSE="${COMPOSE:-docker compose}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.selfhost.yml}"
BACKUP=""
CONFIRM=0

while [ $# -gt 0 ]; do
  case "$1" in
    --confirm | --yes) CONFIRM=1; shift ;;
    -f | --file) COMPOSE_FILE="$2"; shift 2 ;;
    -h | --help)
      sed -n '2,19p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    -*) echo "restore.sh: unknown flag '$1'" >&2; exit 2 ;;
    *) BACKUP="$1"; shift ;;
  esac
done

die() { echo "restore.sh: $*" >&2; exit 1; }

[ -n "$BACKUP" ] || die "usage: bash scripts/restore.sh <backup-dir> --confirm"
BACKUP="$(cd "$BACKUP" 2>/dev/null && pwd)" || die "no such backup directory"

cd "$ROOT_DIR"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# Minimal flat-JSON reader — the manifest is written by backup.sh, not by an
# arbitrary producer, so a grep is enough and keeps jq off the dependency list.
manifest_str() { sed -n "s/.*\"$1\": *\"\([^\"]*\)\".*/\1/p" "$BACKUP/manifest.json" | head -n1; }
manifest_num() { sed -n "s/.*\"$1\": *\([0-9][0-9]*\).*/\1/p" "$BACKUP/manifest.json" | head -n1; }
manifest_file_sha() {
  sed -n "s/.*\"$1\": *{ *\"sha256\": *\"\([^\"]*\)\".*/\1/p" "$BACKUP/manifest.json" | head -n1
}

# --- Gate 1: structure ------------------------------------------------------
for f in manifest.json db.dump uploads.tar; do
  [ -f "$BACKUP/$f" ] || die "backup is incomplete — missing $f in $BACKUP"
done

SCHEMA_VERSION="$(manifest_num schema_version)"
IMAGE_TAG="$(manifest_str image_tag)"
PG_DB="$(manifest_str postgres_db)"
PG_USER="$(manifest_str postgres_user)"
[ -n "$SCHEMA_VERSION" ] && [ -n "$PG_DB" ] && [ -n "$PG_USER" ] ||
  die "manifest.json is missing required fields — refusing to guess"

echo "==> Backup $BACKUP"
echo "    created:  $(manifest_str created_at)"
echo "    tag:      $IMAGE_TAG"
echo "    schema:   $SCHEMA_VERSION"
echo "    database: $PG_DB (owner $PG_USER)"

# --- Gate 2: checksums ------------------------------------------------------
for f in db.dump uploads.tar; do
  want="$(manifest_file_sha "$f")"
  [ -n "$want" ] || die "manifest.json has no sha256 for $f"
  got="$(sha256_of "$BACKUP/$f")"
  [ "$want" = "$got" ] ||
    die "checksum mismatch on $f (manifest $want, file $got) — the backup is corrupt, refusing to restore"
done
echo "    checksums OK"

# --- Gate 3: schema compatibility -------------------------------------------
# Newest migration shipped in the backend IMAGE the stack will run — not this
# checkout, which may be newer than GOOSAR_IMAGE_TAG. The image is what
# migrates and serves the restored database, so it is the only source that
# can answer "will this binary understand the dump". Read-only: a throwaway
# --no-deps container listing /app/migrations.
IMAGE_SCHEMA="$($COMPOSE -f "$COMPOSE_FILE" run --rm --no-deps -T --entrypoint sh backend \
  -c 'ls /app/migrations' | sed -n 's/^\([0-9][0-9]*\)_.*\.up\.sql$/\1/p' |
  sed 's/^0*//' | sort -n | tail -n1)" ||
  die "could not list migrations in the backend image — is it pulled and does the compose file name a 'backend' service?"
case "$IMAGE_SCHEMA" in
  '' | *[!0-9]*) die "backend image reports no migrations — refusing to guess schema compatibility" ;;
esac
if [ "$SCHEMA_VERSION" -gt "$IMAGE_SCHEMA" ]; then
  die "backup schema version $SCHEMA_VERSION is newer than the backend image ($IMAGE_SCHEMA).
    Restoring it would leave the database ahead of the code and there is no
    down-migration path this script can take. Set GOOSAR_IMAGE_TAG to the
    release the backup was taken from (tag $IMAGE_TAG) and restore with that."
fi
[ "$SCHEMA_VERSION" -lt "$IMAGE_SCHEMA" ] &&
  echo "    note: backup schema $SCHEMA_VERSION < image $IMAGE_SCHEMA — the backend will migrate forward on start" || true

# --- Gate 4: explicit confirmation ------------------------------------------
if [ "$CONFIRM" -ne 1 ]; then
  cat >&2 <<EOF
restore.sh: refusing to run without --confirm.
    This DESTROYS the target stack's database and backend uploads volume
    (compose file '$COMPOSE_FILE').
    Re-run: bash scripts/restore.sh "$BACKUP" --confirm
EOF
  exit 1
fi

# --- Restore ----------------------------------------------------------------
echo "==> Stopping application services (postgres stays up for the restore)"
$COMPOSE -f "$COMPOSE_FILE" stop backend frontend || die "could not stop services"

echo "==> Starting postgres"
$COMPOSE -f "$COMPOSE_FILE" up -d postgres || die "could not start postgres"

# The database the backend will connect to is whatever POSTGRES_DB/USER the
# TARGET stack was started with (DATABASE_URL is built from them). The
# manifest only says what the source was called; restoring under the source
# name into a stack configured with a different one (a drill with its own
# env file) would leave the data in a database nothing reads.
TARGET_DB="$($COMPOSE -f "$COMPOSE_FILE" exec -T postgres printenv POSTGRES_DB | tr -d '[:space:]')" ||
  die "could not read POSTGRES_DB from the postgres container"
TARGET_USER="$($COMPOSE -f "$COMPOSE_FILE" exec -T postgres printenv POSTGRES_USER | tr -d '[:space:]')" ||
  die "could not read POSTGRES_USER from the postgres container"
[ -n "$TARGET_DB" ] && [ -n "$TARGET_USER" ] || die "postgres container has no POSTGRES_DB/POSTGRES_USER — refusing to guess"
[ "$TARGET_DB" = "$PG_DB" ] || echo "    note: restoring into database '$TARGET_DB' (backup was taken from '$PG_DB')"

for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
  if $COMPOSE -f "$COMPOSE_FILE" exec -T postgres pg_isready -U "$TARGET_USER" -d postgres >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
[ "${ready:-0}" = 1 ] || die "postgres did not become ready"

echo "==> Recreating database '$TARGET_DB'"
$COMPOSE -f "$COMPOSE_FILE" exec -T postgres psql -U "$TARGET_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS \"$TARGET_DB\" WITH (FORCE)" || die "could not drop database"
$COMPOSE -f "$COMPOSE_FILE" exec -T postgres psql -U "$TARGET_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "CREATE DATABASE \"$TARGET_DB\" OWNER \"$TARGET_USER\"" || die "could not create database"

# --single-transaction: a failing pg_restore rolls the whole load back, so
# "left empty" below is literally true instead of "left with whatever
# objects loaded before the error".
echo "==> Restoring database"
$COMPOSE -f "$COMPOSE_FILE" exec -T postgres \
  pg_restore -U "$TARGET_USER" -d "$TARGET_DB" --no-owner --no-privileges --exit-on-error --single-transaction \
  <"$BACKUP/db.dump" ||
  die "pg_restore failed — database '$TARGET_DB' is left empty and the stack is stopped; fix the cause and re-run"

echo "==> Restoring uploads volume"
$COMPOSE -f "$COMPOSE_FILE" run --rm --no-deps -T --entrypoint sh backend \
  -c 'rm -rf /app/data/uploads/..?* /app/data/uploads/.[!.]* /app/data/uploads/* 2>/dev/null; exec tar -xf - -C /app/data/uploads' \
  <"$BACKUP/uploads.tar" ||
  die "uploads restore failed — the database IS restored but the uploads volume is partial or empty; re-run to redo both, or fix and re-extract uploads.tar by hand"

echo "==> Starting the stack"
$COMPOSE -f "$COMPOSE_FILE" up -d || die "could not start the stack"

echo "==> Restore complete from $BACKUP"
