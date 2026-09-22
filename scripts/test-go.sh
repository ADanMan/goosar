#!/usr/bin/env bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
GUARD_SCRIPT="$SCRIPT_DIR/go-test-with-agent-cli-guard.sh"

usage() {
  echo "usage: $0 [--race]" >&2
}

go_test_args=(test)
case "$#" in
  0) ;;
  1)
    if [ "$1" != "--race" ]; then
      usage
      exit 2
    fi
    go_test_args+=(-race)
    ;;
  *)
    usage
    exit 2
    ;;
esac

# #167 review finding #2: the handler suite's TestMain used to silently
# os.Exit(0) ("Skipping tests") when Postgres was unreachable, so a DB-less
# run reported `ok` with zero tests executed. This script always runs against
# a Postgres the caller is expected to have provisioned (CI's `backend` job
# starts a postgres service; `make test` / local dev do the same), so treat a
# DB connection failure here as fatal rather than a silent skip.
export GOOSAR_REQUIRE_TEST_DB=1

# #195: the release gate runs under `make release-local`, whose bare `export`
# leaks every .env variable — including RESEND_API_KEY — into child processes.
# handler tests build the real EmailService from env, so a leaked key made the
# auth tests call the actual Resend API and fail on its answers. Tests must be
# hermetic: scrub the whole mail family here so EmailService always takes the
# DEV stdout path. handler TestMain additionally fails loud if any of these
# survive (mirror of the GOOSAR_REQUIRE_TEST_DB pin above).
unset RESEND_API_KEY RESEND_FROM_EMAIL \
      SMTP_HOST SMTP_PORT SMTP_USERNAME SMTP_PASSWORD \
      SMTP_TLS SMTP_TLS_INSECURE SMTP_EHLO_NAME SMTP_FROM_EMAIL

# #201: the suite must never share a database with a live deployment. The
# local Docker stack's backend polls the same Postgres as .env's DATABASE_URL,
# and its webhook delivery worker claims the tests' queued deliveries via
# SKIP LOCKED — dispatching test autopilots into the live database and flaking
# any test that expects to perform the claim itself (#197 was this, not an
# in-gate race). Derive a dedicated `<name>_test` database, create + migrate
# it, and run the whole suite there.
base_db_url="${DATABASE_URL:-postgres://goosar:goosar@localhost:5433/goosar?sslmode=disable}"
url_query=""
url_rest="$base_db_url"
case "$url_rest" in
  *\?*)
    url_query="?${url_rest#*\?}"
    url_rest="${url_rest%%\?*}"
    ;;
esac
url_dbname="${url_rest##*/}"
url_prefix="${url_rest%/*}"
case "$url_prefix" in
  *://*) ;;
  *)
    echo "FATAL: DATABASE_URL has no database path; cannot derive the hermetic test database" >&2
    exit 1
    ;;
esac
if [ -z "$url_dbname" ]; then
  echo "FATAL: DATABASE_URL has an empty database name; cannot derive the hermetic test database" >&2
  exit 1
fi
case "$url_dbname" in
  *_test) export DATABASE_URL="$base_db_url" ;;
  *) export DATABASE_URL="${url_prefix}/${url_dbname}_test${url_query}" ;;
esac

cd "$REPO_ROOT/server"
go run ./cmd/ensure_test_db
go run ./cmd/migrate up
packages=$(go list ./...)
regular_packages=()
for package in $packages; do
  case "$package" in
    */pkg/agent|*/pkg/agent/*) ;;
    *) regular_packages+=("$package") ;;
  esac
done

"$GUARD_SCRIPT" -- go "${go_test_args[@]}" "${regular_packages[@]}"
# Subprocess-backed agent tests have hard deadlines. Limit both package and
# within-package parallelism so race builds do not starve their parent loops.
"$GUARD_SCRIPT" -- go "${go_test_args[@]}" -p 2 -parallel 2 ./pkg/agent/...
