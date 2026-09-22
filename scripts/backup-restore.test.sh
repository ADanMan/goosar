#!/usr/bin/env bash
# Hermetic tests for scripts/backup.sh and scripts/restore.sh (issue #383).
#
# No docker, no postgres, no network: a fake `docker` on PATH logs every argv
# it is called with and fabricates the payloads the real one would stream. The
# tests assert the argv contract, the manifest contents, and every refusal
# path — checksum mismatch, missing --confirm, schema newer than the checkout,
# and a failing sub-step aborting without leaving a partial backup behind.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() {
  printf 'FAIL %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# Fixture: a miniature checkout with the two scripts and an .env. The checkout
# deliberately has NO server/migrations directory and a deliberately NEWER
# one is planted in a later test: the schema gate must consult the backend
# image, never the checkout.
make_fixture() {
  local dir="$1"
  mkdir -p "$dir/scripts" "$dir/bin"
  cp "$ROOT_DIR/scripts/backup.sh" "$ROOT_DIR/scripts/restore.sh" "$dir/scripts/"
  : >"$dir/docker-compose.selfhost.yml"
  # POSTGRES_* here are DECOYS: the scripts must take the database name and
  # role from the running postgres container, never from .env.
  cat >"$dir/.env" <<'EOF'
POSTGRES_DB=stale-env-db
POSTGRES_USER=stale-env-user
GOOSAR_IMAGE_TAG=v1.0.0
EOF

  # Fake docker: logs argv, then plays the role of whatever it was asked for.
  # FAKE_DOCKER_FAIL matches against the joined argv to force a sub-step
  # failure; FAKE_SCHEMA_VERSION overrides what psql reports;
  # FAKE_IMAGE_SCHEMA is the newest migration the backend image ships (272);
  # FAKE_TARGET_DB/USER are what the postgres container was started with.
  cat >"$dir/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_LOG"
if [ -n "${FAKE_DOCKER_FAIL:-}" ] && printf '%s' "$*" | grep -q -- "$FAKE_DOCKER_FAIL"; then
  echo "fake docker: forced failure" >&2
  exit 17
fi
case "$*" in
  *"FROM schema_migrations"*)
    echo "${FAKE_SCHEMA_VERSION:-272}" ;;
  *pg_dump*) printf 'PGDMP-fake-custom-format-dump\n' ;;
  *"tar -cf -"*) printf 'fake-uploads-tar-stream\n' ;;
  *"ls /app/migrations"*)
    printf '001_init.up.sql\n001_init.down.sql\n020_a.up.sql\n020_b.up.sql\n%s_latest.up.sql\n' "${FAKE_IMAGE_SCHEMA:-272}" ;;
  *"printenv POSTGRES_DB"*) echo "${FAKE_TARGET_DB:-goosardb}" ;;
  *"printenv POSTGRES_USER"*) echo "${FAKE_TARGET_USER:-goosaruser}" ;;
  *) : ;;
esac
exit 0
EOF
  chmod +x "$dir/bin/docker"
}

# Destructive compose calls: anything that stops, drops, restores or extracts.
DESTRUCTIVE_RE='stop backend|DROP DATABASE|CREATE DATABASE|pg_restore|tar -xf|up -d'
assert_nothing_destroyed() {
  if ! grep -qE -- "$DESTRUCTIVE_RE" "$2" 2>/dev/null; then
    pass "$1"
  else
    failed "$1: $(grep -E -- "$DESTRUCTIVE_RE" "$2")"
  fi
}

run_backup() {
  local dir="$1"; shift
  (
    cd "$dir"
    PATH="$dir/bin:$PATH" DOCKER_LOG="$dir/docker.log" \
      bash scripts/backup.sh --out "$dir/backups" "$@"
  )
}

run_restore() {
  local dir="$1"; shift
  (
    cd "$dir"
    PATH="$dir/bin:$PATH" DOCKER_LOG="$dir/docker.log" \
      bash scripts/restore.sh "$@"
  )
}

# --- Scripts parse ----------------------------------------------------------
for s in backup.sh restore.sh; do
  if bash -n "$ROOT_DIR/scripts/$s"; then
    pass "$s parses"
  else
    failed "$s does not parse"
  fi
done

# --- Happy-path backup ------------------------------------------------------
D="$TMP_ROOT/backup-ok"
make_fixture "$D"
if run_backup "$D" >"$D/out.txt" 2>&1; then
  pass "backup.sh succeeds against the fake stack"
else
  failed "backup.sh failed: $(cat "$D/out.txt")"
fi

BK="$(ls -d "$D"/backups/goosar-* 2>/dev/null | head -n1 || true)"
if [ -n "$BK" ] && [ -f "$BK/db.dump" ] && [ -f "$BK/uploads.tar" ] && [ -f "$BK/manifest.json" ]; then
  pass "backup directory holds db.dump, uploads.tar and manifest.json"
else
  failed "backup directory incomplete (dir='$BK')"
fi

case "$(basename "${BK:-none}")" in
  goosar-????????T??????Z) pass "backup directory name is a UTC timestamp" ;;
  *) failed "unexpected backup directory name '$(basename "${BK:-none}")'" ;;
esac

# The dump is the whole database and the tar carries every attachment, so
# neither may be created world-readable on a shared host.
dump_mode="$(ls -l "$BK/db.dump" | cut -c1-10)"
tar_mode="$(ls -l "$BK/uploads.tar" | cut -c1-10)"
if [ "$dump_mode" = "-rw-------" ] && [ "$tar_mode" = "-rw-------" ]; then
  pass "backup artifacts are owner-only"
else
  failed "backup artifacts readable by other accounts (db.dump='$dump_mode' uploads.tar='$tar_mode')"
fi

if [ -z "$(ls -d "$D"/backups/*.partial 2>/dev/null || true)" ]; then
  pass "no .partial directory left after a successful run"
else
  failed ".partial directory survived a successful run"
fi

# --- argv contract ----------------------------------------------------------
LOG="$D/docker.log"
assert_log() {
  if grep -qF -- "$2" "$LOG"; then
    pass "argv: $1"
  else
    failed "argv: $1 — not found in docker.log ($2)"
  fi
}
assert_log "schema version read as the numeric migration prefix (version is TEXT)" \
  "exec -T postgres psql -U goosaruser -d goosardb -tAc SELECT COALESCE(MAX(split_part(version, '_', 1)::bigint), 0) FROM schema_migrations"
assert_log "pg_dump uses custom format with the .env credentials" \
  "exec -T postgres pg_dump -U goosaruser -d goosardb -Fc"
assert_log "uploads tarred from a throwaway --no-deps run container" \
  "run --rm --no-deps -T --entrypoint sh backend -c tar -cf - -C /app/data/uploads ."

# Documented consistency window: the DB snapshot is taken first, so every
# file a restored row references existed when the volume was archived.
dump_line="$(grep -n "pg_dump" "$LOG" | head -n1 | cut -d: -f1)"
tar_line="$(grep -n "tar -cf -" "$LOG" | head -n1 | cut -d: -f1)"
if [ -n "$dump_line" ] && [ -n "$tar_line" ] && [ "$dump_line" -lt "$tar_line" ]; then
  pass "database is dumped before the uploads volume is archived"
else
  failed "ordering wrong: pg_dump=$dump_line tar=$tar_line"
fi

# --- Manifest contents ------------------------------------------------------
man="$BK/manifest.json"
check_manifest() {
  if grep -qF -- "$2" "$man"; then
    pass "manifest: $1"
  else
    failed "manifest: $1 — missing ($2) in $(cat "$man")"
  fi
}
check_manifest "records the image tag from .env" '"image_tag": "v1.0.0"'
check_manifest "records the schema migration number" '"schema_version": 272'
check_manifest "records the database name from the running container, not .env" '"postgres_db": "goosardb"'
check_manifest "records the database user from the running container, not .env" '"postgres_user": "goosaruser"'
if grep -q "stale-env" "$man" "$LOG"; then
  failed "a .env POSTGRES_* value leaked into the manifest or a docker call"
else
  pass "no .env POSTGRES_* value reaches the manifest or docker"
fi
check_manifest "records db.dump sha256" "\"sha256\": \"$(sha256_of "$BK/db.dump")\""
check_manifest "records uploads.tar sha256" "\"sha256\": \"$(sha256_of "$BK/uploads.tar")\""
if grep -qE '"created_at": "[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"' "$man"; then
  pass "manifest: records an ISO-8601 UTC creation time"
else
  failed "manifest: created_at missing or not ISO-8601 UTC"
fi

# --- --out is relative to the caller's cwd, not the checkout ----------------
D="$TMP_ROOT/backup-relout"
make_fixture "$D"
mkdir -p "$D/elsewhere"
if (
  cd "$D/elsewhere"
  PATH="$D/bin:$PATH" DOCKER_LOG="$D/docker.log" bash "$D/scripts/backup.sh" --out here
) >"$D/out.txt" 2>&1 && ls -d "$D"/elsewhere/here/goosar-* >/dev/null 2>&1; then
  pass "relative --out resolves against the caller's cwd"
else
  failed "relative --out did not land in the caller's cwd: $(cat "$D/out.txt")"
fi

# --- A failing sub-step aborts loudly and leaves nothing --------------------
D="$TMP_ROOT/backup-fail"
make_fixture "$D"
if (
  cd "$D"
  PATH="$D/bin:$PATH" DOCKER_LOG="$D/docker.log" FAKE_DOCKER_FAIL="tar -cf -" \
    bash scripts/backup.sh --out "$D/backups"
) >"$D/out.txt" 2>&1; then
  failed "backup.sh returned 0 although the uploads step failed"
else
  pass "backup.sh fails when a sub-step fails"
fi
if grep -q "uploads archive failed" "$D/out.txt"; then
  pass "backup.sh names the failing step"
else
  failed "backup.sh did not report the failing step: $(cat "$D/out.txt")"
fi
if [ -z "$(ls -A "$D/backups" 2>/dev/null || true)" ]; then
  pass "no partial backup left behind after a failed run"
else
  failed "partial backup survived: $(ls -A "$D/backups")"
fi

# --- Restore: refuses without --confirm -------------------------------------
D="$TMP_ROOT/restore"
make_fixture "$D"
run_backup "$D" >/dev/null 2>&1
BK="$(ls -d "$D"/backups/goosar-* | head -n1)"
: >"$D/docker.log"
if run_restore "$D" "$BK" >"$D/out.txt" 2>&1; then
  failed "restore.sh ran without --confirm"
else
  pass "restore.sh refuses without --confirm"
fi
if grep -q -- "--confirm" "$D/out.txt"; then
  pass "restore.sh says how to confirm"
else
  failed "restore.sh refusal message does not mention --confirm"
fi
assert_nothing_destroyed "restore.sh destroys nothing before --confirm is given" "$D/docker.log"

# --- Restore: refuses on checksum mismatch ----------------------------------
cp -R "$BK" "$D/tampered"
printf 'corrupted' >>"$D/tampered/db.dump"
: >"$D/docker.log"
if run_restore "$D" "$D/tampered" --confirm >"$D/out.txt" 2>&1; then
  failed "restore.sh accepted a corrupted db.dump"
else
  pass "restore.sh refuses on checksum mismatch"
fi
if grep -q "checksum mismatch on db.dump" "$D/out.txt"; then
  pass "restore.sh names the corrupted file"
else
  failed "restore.sh checksum message unclear: $(cat "$D/out.txt")"
fi
if [ ! -s "$D/docker.log" ]; then
  pass "restore.sh touches docker at all only after the checksums pass"
else
  failed "restore.sh invoked docker despite a bad checksum"
fi

# --- Restore: refuses a backup newer than the backend IMAGE -----------------
# The checkout is planted with a migration newer than the dump; only the
# image (272) is older. An older binary must not eat a newer dump, and a
# freshly pulled checkout must not fool the gate.
D2="$TMP_ROOT/restore-newer"
make_fixture "$D2"
mkdir -p "$D2/server/migrations"
: >"$D2/server/migrations/999_future.up.sql"
(
  cd "$D2"
  PATH="$D2/bin:$PATH" DOCKER_LOG="$D2/docker.log" FAKE_SCHEMA_VERSION=300 \
    bash scripts/backup.sh --out "$D2/backups"
) >/dev/null 2>&1
NEWBK="$(ls -d "$D2"/backups/goosar-* | head -n1)"
: >"$D2/docker.log"
if run_restore "$D2" "$NEWBK" --confirm >"$D2/out.txt" 2>&1; then
  failed "restore.sh accepted a dump newer than the backend image schema"
else
  pass "restore.sh refuses a dump newer than the backend image schema"
fi
if grep -q "newer than the backend image (272)" "$D2/out.txt"; then
  pass "restore.sh explains the schema mismatch with both versions"
else
  failed "restore.sh schema message unclear: $(cat "$D2/out.txt")"
fi
if grep -q "GOOSAR_IMAGE_TAG" "$D2/out.txt" && grep -q "(tag v1.0.0)" "$D2/out.txt"; then
  pass "restore.sh names the image tag to restore with"
else
  failed "restore.sh does not name the tag: $(cat "$D2/out.txt")"
fi
assert_nothing_destroyed "restore.sh destroys nothing on a schema mismatch" "$D2/docker.log"
if grep -q "ls /app/migrations" "$D2/docker.log" && grep -q -- "--no-deps" "$D2/docker.log"; then
  pass "schema gate reads migrations from a --no-deps backend container"
else
  failed "schema gate did not consult the image: $(cat "$D2/docker.log")"
fi

# --- Restore: refuses when the image cannot report its migrations ----------
: >"$D2/docker.log"
if (
  cd "$D2"
  PATH="$D2/bin:$PATH" DOCKER_LOG="$D2/docker.log" FAKE_DOCKER_FAIL="ls /app/migrations" \
    bash scripts/restore.sh "$NEWBK" --confirm
) >"$D2/out.txt" 2>&1; then
  failed "restore.sh proceeded although the image migrations could not be listed"
else
  pass "restore.sh refuses when the image migrations cannot be listed"
fi
assert_nothing_destroyed "restore.sh destroys nothing when the image is unreadable" "$D2/docker.log"

# --- Restore: targets the stack's database, not the manifest's name --------
# A drill stack started with its own POSTGRES_DB must receive the data under
# THAT name; restoring under the source name would leave it unread.
D3="$TMP_ROOT/restore-drill"
make_fixture "$D3"
run_backup "$D3" >/dev/null 2>&1
DRILLBK="$(ls -d "$D3"/backups/goosar-* | head -n1)"
: >"$D3/docker.log"
if (
  cd "$D3"
  PATH="$D3/bin:$PATH" DOCKER_LOG="$D3/docker.log" FAKE_TARGET_DB=drilldb FAKE_TARGET_USER=drilluser \
    bash scripts/restore.sh "$DRILLBK" --confirm
) >"$D3/out.txt" 2>&1; then
  pass "restore.sh completes into a stack with a different POSTGRES_DB"
else
  failed "restore.sh failed against a drill stack: $(cat "$D3/out.txt")"
fi
if grep -qF 'DROP DATABASE IF EXISTS "drilldb" WITH (FORCE)' "$D3/docker.log" &&
  grep -qF 'CREATE DATABASE "drilldb" OWNER "drilluser"' "$D3/docker.log" &&
  grep -qF 'pg_restore -U drilluser -d drilldb' "$D3/docker.log" &&
  ! grep -q '"goosardb"' "$D3/docker.log"; then
  pass "restore.sh drops/creates/restores the TARGET database, never the manifest name"
else
  failed "restore.sh used the wrong database: $(grep -E 'DATABASE|pg_restore' "$D3/docker.log")"
fi
if grep -q "restoring into database 'drilldb' (backup was taken from 'goosardb')" "$D3/out.txt"; then
  pass "restore.sh says when the target database name differs from the backup"
else
  failed "restore.sh did not note the name difference: $(cat "$D3/out.txt")"
fi

# --- Restore: refuses an incomplete backup directory ------------------------
cp -R "$BK" "$D/truncated"
rm -f "$D/truncated/uploads.tar"
if run_restore "$D" "$D/truncated" --confirm >"$D/out.txt" 2>&1; then
  failed "restore.sh accepted a backup with no uploads.tar"
else
  pass "restore.sh refuses an incomplete backup directory"
fi

# --- Restore: happy path argv contract --------------------------------------
: >"$D/docker.log"
if run_restore "$D" "$BK" --confirm >"$D/out.txt" 2>&1; then
  pass "restore.sh completes against the fake stack"
else
  failed "restore.sh failed: $(cat "$D/out.txt")"
fi
LOG="$D/docker.log"
assert_log "application services stopped before the restore" \
  "compose -f docker-compose.selfhost.yml stop backend frontend"
assert_log "postgres started for the restore" \
  "compose -f docker-compose.selfhost.yml up -d postgres"
assert_log "readiness probed with pg_isready" \
  "exec -T postgres pg_isready -U goosaruser -d postgres"
assert_log "database dropped with FORCE" \
  'DROP DATABASE IF EXISTS "goosardb" WITH (FORCE)'
assert_log "database recreated with the manifest owner" \
  'CREATE DATABASE "goosardb" OWNER "goosaruser"'
assert_log "pg_restore runs ownerless, fail-fast and atomically (single transaction)" \
  "pg_restore -U goosaruser -d goosardb --no-owner --no-privileges --exit-on-error --single-transaction"
assert_log "uploads volume emptied and re-extracted" \
  "tar -xf - -C /app/data/uploads"
assert_log "stack brought back up" \
  "compose -f docker-compose.selfhost.yml up -d"

# The stop must precede the destructive drop.
stop_line="$(grep -n "stop backend frontend" "$LOG" | head -n1 | cut -d: -f1)"
drop_line="$(grep -n "DROP DATABASE" "$LOG" | head -n1 | cut -d: -f1)"
if [ -n "$stop_line" ] && [ -n "$drop_line" ] && [ "$stop_line" -lt "$drop_line" ]; then
  pass "services are stopped before the database is dropped"
else
  failed "ordering wrong: stop=$stop_line drop=$drop_line"
fi

# --- Restore: a failing uploads step is reported as a half-restore ----------
: >"$D/docker.log"
if (
  cd "$D"
  PATH="$D/bin:$PATH" DOCKER_LOG="$D/docker.log" FAKE_DOCKER_FAIL="tar -xf" \
    bash scripts/restore.sh "$BK" --confirm
) >"$D/out.txt" 2>&1; then
  failed "restore.sh returned 0 although the uploads step failed"
else
  pass "restore.sh fails when the uploads step fails"
fi
if grep -q "database IS restored but the uploads volume" "$D/out.txt"; then
  pass "restore.sh states the half-restored condition"
else
  failed "restore.sh hid the half-restore: $(cat "$D/out.txt")"
fi
if ! grep -q "up -d$" "$D/docker.log"; then
  pass "restore.sh does not start the stack after a failed uploads step"
else
  failed "restore.sh started the stack on a half-restore"
fi

echo ""
if [ "$FAILURES" -eq 0 ]; then
  echo "All backup/restore script tests passed."
else
  echo "$FAILURES check(s) failed." >&2
  exit 1
fi
