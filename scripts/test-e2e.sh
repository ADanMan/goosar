#!/usr/bin/env bash
# ==========================================================================
# E2E gate (issue #212): bring up backend+frontend for THIS checkout, wait
# for health, run the Playwright suite, tear down only what we started.
#
# Exit codes are distinguishable on purpose:
#   0  specs passed
#   1  specs FAILED (stack was healthy, Playwright reported failures)
#   2  bootstrap FAILED (browser install, DB, or a server never became healthy)
#
# Env selection mirrors e2e/env.ts: an explicit $ENV_FILE wins, otherwise
# .env.worktree (worktree isolation), otherwise .env.
#
# Wired into scripts/release-local.sh (release gate) and `make e2e`.
# Deliberately NOT part of `pnpm test` — too slow for iteration.
#
# Minimal env for a release-gate run (issue #450):
#   DATABASE_URL     a THROWAWAY database — the suite truncates it
#   BACKEND_PORT     backend port (also read as PORT/API_PORT/SERVER_PORT)
#   FRONTEND_PORT    next dev port
#   FRONTEND_ORIGIN  http://localhost:$FRONTEND_PORT
#   GOOSAR_PUBLIC_URL  the same origin, for links the backend renders
#   umask 022        so the pnpm store and .next stay readable
# TURBO_ENV_MODE=loose and REMOTE_API_URL are set by this script (see below).
# NEXT_PUBLIC_API_URL / NEXT_PUBLIC_WS_URL are UNSET by this script: they point
# the browser at the backend directly, and a cross-origin websocket fails the
# realtime spec. The browser stays same-origin and reaches the API through the
# Next proxy. (Warning about them was not enough — .env.worktree and the
# Makefile both set them by default.)
#
# Operator knobs:
#   E2E_WARMUP_TIMEOUT      wall-clock budget for the WHOLE warm-up phase, in
#                           seconds (default 1800; must be a positive integer).
#                           Exhausting it is a warning, not a failure — the
#                           specs then pay the compile.
#   E2E_WARMUP_SLUG         workspace slug used for the warm-up requests
#
# Test seams (used only by scripts/test-e2e.test.sh):
#   GOOSAR_E2E_SKIP_DB=1   skip scripts/ensure-postgres.sh + migrations
# ==========================================================================
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

EXIT_BOOTSTRAP=2
EXIT_SPECS=1

fail_bootstrap() { echo "E2E BOOTSTRAP FAILED: $1" >&2; exit "$EXIT_BOOTSTRAP"; }

if [ -z "${ENV_FILE:-}" ]; then
  if [ -f .env.worktree ]; then ENV_FILE=.env.worktree; else ENV_FILE=.env; fi
fi
[ -f "$ENV_FILE" ] || fail_bootstrap "missing env file: $ENV_FILE (run 'make worktree-env' or create .env)"

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
# shellcheck disable=SC1091
. scripts/local-env.sh

# The browser must talk to the API through the Next proxy, same-origin.
# `next dev` runs under turbo, whose strict env mode drops every variable that
# turbo.json globalEnv does not list — REMOTE_API_URL is not there, so the
# proxy fell back to its build-time upstream and the whole suite silently
# tested a different backend (on the v0.9.0 gate: the operator's own stack on
# :8083, reported as `404 user not found` in every spec). Loose mode lets it
# through. NEXT_PUBLIC_* is NOT the fix: it moves the browser off the frontend
# origin and the realtime spec then fails on a cross-origin websocket.
export TURBO_ENV_MODE="${TURBO_ENV_MODE:-loose}"
export REMOTE_API_URL="${REMOTE_API_URL:-http://localhost:${PORT}}"

# ...and warning about NEXT_PUBLIC_* is not enough, because the environment
# this script runs in sets them by default: `scripts/init-worktree-env.sh`
# writes both into .env.worktree (the file the release gate hands over first)
# and the Makefile exports them to every recipe, `make e2e` included. Unset
# them for the servers we start, so the browser keeps talking to the frontend
# origin. Node-side Playwright fixtures are unaffected: e2e/env.ts re-reads the
# env file, and their fallback is http://localhost:$PORT — the same backend.
NEXT_PUBLIC_WAS_SET="${NEXT_PUBLIC_API_URL:-}${NEXT_PUBLIC_WS_URL:-}"
unset NEXT_PUBLIC_API_URL NEXT_PUBLIC_WS_URL

# Operator knobs are validated here, before the multi-minute browser install,
# so a typo is a bootstrap failure with a clear message. `E2E_WARMUP_TIMEOUT=15m`
# used to crash the warm-up arithmetic and exit 1 — the code this script's
# contract reserves for "the specs failed".
WARMUP_TIMEOUT="${E2E_WARMUP_TIMEOUT:-1800}"
case "$WARMUP_TIMEOUT" in
  '' | *[!0-9]* | 0)
    fail_bootstrap "E2E_WARMUP_TIMEOUT must be a positive whole number of SECONDS (got '$WARMUP_TIMEOUT'; write 1800, not 15m)" ;;
esac

BACKEND_PID=""
FRONTEND_PID=""
E2E_LOG_DIR="${E2E_LOG_DIR:-/tmp}"

# Job control, so each background job below lands in its own process group and
# cleanup can kill the whole tree. Both launchers run the process that actually
# holds the port as a CHILD (`go run` builds then runs a binary, `turbo dev`
# spawns `next dev`), so killing the parent alone left a listener on the gate's
# port and the next run died with "already running" (issue #450).
set -m

stop_pid() { # stop_pid <pid> — kill the process group we started, then reap it
  local pid=$1
  [ -n "$pid" ] || return 0
  kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  echo "    Stopped PID $pid"
}

cleanup() {
  # Tear down ONLY processes this script started — never a stack it found
  # already running. Runs on every outcome: pass, spec failure, bootstrap
  # failure, Ctrl-C.
  local backend=$BACKEND_PID frontend=$FRONTEND_PID
  BACKEND_PID=""
  FRONTEND_PID=""
  stop_pid "$backend"
  stop_pid "$frontend"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

wait_for_url() { # wait_for_url <url> <name> <max_seconds>
  local url=$1 name=$2 max_wait=$3 elapsed=0
  echo "    Waiting for $name at $url..."
  while ! curl -sf "$url" > /dev/null 2>&1; do
    sleep 1
    elapsed=$((elapsed + 1))
    if [ "$elapsed" -ge "$max_wait" ]; then
      fail_bootstrap "$name did not become healthy at $url within ${max_wait}s (see $E2E_LOG_DIR/goosar-e2e-*.log)"
    fi
  done
  echo "    $name ready (${elapsed}s)"
}

# --------------------------------------------------------------------------
# Step 1: Playwright browser bootstrap.
#
# `pnpm exec playwright install chromium` deterministically HANGS on macOS
# arm64: the unzip step inside "Google Chrome for Testing.app" never
# completes. Known workaround: kill the install after a timeout, unpack the
# zip Playwright already downloaded into the cache with `ditto -x -k`
# (Apple's archiver handles the app bundle correctly), then create the
# INSTALLATION_COMPLETE / DEPENDENCIES_VALIDATED marker files Playwright
# checks before deciding the browser is installed.
# --------------------------------------------------------------------------
default_pw_cache="$HOME/Library/Caches/ms-playwright"
[ "$(uname)" = "Darwin" ] || default_pw_cache="$HOME/.cache/ms-playwright"
PW_CACHE="${PLAYWRIGHT_BROWSERS_PATH:-$default_pw_cache}"
INSTALL_TIMEOUT="${E2E_INSTALL_TIMEOUT:-180}"

run_with_timeout() { # run_with_timeout <seconds> <cmd...> — no coreutils dep
  local secs=$1; shift
  "$@" &
  local cmd_pid=$!
  ( sleep "$secs"; kill "$cmd_pid" 2>/dev/null ) &
  local watchdog=$!
  local rc=0
  wait "$cmd_pid" 2>/dev/null || rc=$?
  kill "$watchdog" 2>/dev/null
  wait "$watchdog" 2>/dev/null
  return "$rc"
}

ditto_fallback() {
  local fixed=0 dir zip
  for zip in "$PW_CACHE"/*.zip "$PW_CACHE"/chromium*/*.zip; do
    [ -f "$zip" ] || continue
    # A zip at the cache root unpacks into a directory of the same name;
    # a zip left INSIDE a chromium-* dir unpacks into that dir.
    case "$(basename "$(dirname "$zip")")" in
      chromium*) dir="$(dirname "$zip")" ;;
      *)         dir="${zip%.zip}" ;;
    esac
    echo "    ditto fallback: unpacking $zip -> $dir"
    mkdir -p "$dir"
    ditto -x -k "$zip" "$dir" || return 1
    touch "$dir/INSTALLATION_COMPLETE" "$dir/DEPENDENCIES_VALIDATED"
    fixed=1
  done
  [ "$fixed" = 1 ]
}

browser_ready() {
  local marker
  for marker in "$PW_CACHE"/chromium*/INSTALLATION_COMPLETE; do
    [ -f "$marker" ] && return 0
  done
  return 1
}

echo "==> [1/4] Playwright browser bootstrap..."
if ! run_with_timeout "$INSTALL_TIMEOUT" pnpm exec playwright install chromium || ! browser_ready; then
  echo "    playwright install hung/failed or left no browser marker — trying ditto fallback"
  ditto_fallback && browser_ready || fail_bootstrap "chromium install failed and ditto fallback found nothing usable under $PW_CACHE"
fi

# --------------------------------------------------------------------------
# Step 2: DB + migrations
# --------------------------------------------------------------------------
echo ""
echo "==> [2/4] Database (env file: $ENV_FILE)..."
if [ "${GOOSAR_E2E_SKIP_DB:-0}" = "1" ]; then
  echo "    GOOSAR_E2E_SKIP_DB=1 — skipping"
else
  bash scripts/ensure-postgres.sh "$ENV_FILE" || fail_bootstrap "ensure-postgres.sh failed"
  (cd server && go run ./cmd/migrate up) || fail_bootstrap "migrations failed"
fi

# --------------------------------------------------------------------------
# Step 3: Backend + frontend (reuse if already healthy)
# --------------------------------------------------------------------------
echo ""
echo "==> [3/4] Services..."
REQUIRE_FRESH="${GOOSAR_E2E_REQUIRE_FRESH:-0}"
if curl -sf "http://localhost:${PORT}/health" > /dev/null 2>&1; then
  # In release mode a pre-existing server may be a stale binary — the gate
  # would then green-light code it never ran. Refuse instead of reusing.
  [ "$REQUIRE_FRESH" != "1" ] || fail_bootstrap "backend already running on :$PORT but GOOSAR_E2E_REQUIRE_FRESH=1 (release gate must start the code under test) — stop it and retry"
  echo "    Backend already running on :$PORT (leaving it alone)"
  # The raised rate limits below only reach a backend THIS script starts (#387).
  # A backend started by `make dev` runs the production defaults, and the suite
  # logs in dozens of fixture users from one address — 5 send-code per minute.
  # Say so here, so a wall of `send-code failed: 429` is not read as a product bug.
  echo "    NOTE: it runs with ITS OWN rate limits. If specs fail with 429 on login," >&2
  echo "          restart it with RATE_LIMIT_AUTH=1000 RATE_LIMIT_AUTH_EMAIL=1000 RATE_LIMIT_API=100000." >&2
else
  echo "    Starting backend..."
  # Rate limits (#387) are ON by default, including without Redis. The suite
  # logs in dozens of fixture users from 127.0.0.1, which is exactly the
  # pattern the per-IP login budget exists to stop, so the harness raises the
  # ceilings for the process it starts. They are raised, never disabled — a
  # spec that manages to trip these numbers has found a real runaway loop.
  export RATE_LIMIT_AUTH="${RATE_LIMIT_AUTH:-1000}"
  export RATE_LIMIT_AUTH_VERIFY="${RATE_LIMIT_AUTH_VERIFY:-1000}"
  export RATE_LIMIT_AUTH_EMAIL="${RATE_LIMIT_AUTH_EMAIL:-1000}"
  export RATE_LIMIT_TOKEN="${RATE_LIMIT_TOKEN:-1000}"
  export RATE_LIMIT_API="${RATE_LIMIT_API:-100000}"
  (cd server && go run ./cmd/server) > "$E2E_LOG_DIR/goosar-e2e-backend.log" 2>&1 &
  BACKEND_PID=$!
  wait_for_url "http://localhost:${PORT}/health" "Backend" "${E2E_BACKEND_WAIT:-90}"
fi

if curl -sf "http://localhost:${FRONTEND_PORT}" > /dev/null 2>&1; then
  [ "$REQUIRE_FRESH" != "1" ] || fail_bootstrap "frontend already running on :$FRONTEND_PORT but GOOSAR_E2E_REQUIRE_FRESH=1 (release gate must start the code under test) — stop it and retry"
  echo "    Frontend already running on :$FRONTEND_PORT (leaving it alone)"
else
  echo "    Starting frontend..."
  pnpm dev:web > "$E2E_LOG_DIR/goosar-e2e-frontend.log" 2>&1 &
  FRONTEND_PID=$!
  wait_for_url "http://localhost:${FRONTEND_PORT}" "Frontend" "${E2E_FRONTEND_WAIT:-120}"
fi

# --------------------------------------------------------------------------
# Step 3b: route warm-up (issue #519)
#
# The release gate runs from a FRESH worktree, so `next dev` compiles each
# route on its first request. The specs' navigation budgets are not
# compilation budgets (navigation.spec: ROUTE_CHANGE_TIMEOUT 30s; auth.spec:
# waitForURL("**/login") 10s; playwright.config: 60s per test), and on a
# machine that just finished a Go race run they lose that race. Compile every
# route the specs reach before the specs start.
#
# The list mirrors the routes e2e/*.spec.ts navigate to (page.goto /
# waitForURL / sidebar clicks), ORDERED BY WHAT THE SPECS NEED FIRST: every
# spec starts in e2e/helpers.ts `loginAsDefault`, which logs in at /login and
# lands on /{slug}/issues, so those two must be warm before anything else.
# Any slug and any id compile the same `[workspaceSlug]/(dashboard)/<route>`
# module — every detail page is a client component, so the dev server answers
# 200 without fixture data.
#
# The requests are fired CONCURRENTLY. Next compiles a queue; feeding it eight
# route requests at once costs roughly the slowest compile, while eight serial
# round-trips cost their sum (measured on the release machine, warm .next,
# loaded: /login 27s, /inbox 9s, /agents 27s, /settings 102s, /issues 540s —
# ~743s serially, which is why the budget below is not 900s).
#
# Bounded by ONE wall clock for the WHOLE phase, not per route: curl gets the
# remaining budget as --max-time, so a dev server that accepts the connection
# and never answers (compile deadlock, HMR loop) cannot wedge the release gate.
# Exhausting the budget is a WARNING that names the routes that stayed cold —
# the specs then pay the compile, exactly as before. The gate must never hide
# a spec failure behind the warm-up.
# --------------------------------------------------------------------------
WARMUP_SLUG="${E2E_WARMUP_SLUG:-e2e-warmup}"
WARMUP_ID="00000000-0000-0000-0000-000000000000"
warmup_deadline=$((SECONDS + WARMUP_TIMEOUT))

WARMUP_ROUTES=(
  "/login"
  "/$WARMUP_SLUG/issues"
  "/$WARMUP_SLUG/inbox"
  "/$WARMUP_SLUG/agents"
  "/$WARMUP_SLUG/settings"
  "/onboarding"
  "/$WARMUP_SLUG/issues/$WARMUP_ID"
  "/$WARMUP_SLUG/agents/$WARMUP_ID"
)

warm_route() { # warm_route <path> — request until it answers 200, bounded
  local path=$1 started=$SECONDS url="http://localhost:${FRONTEND_PORT}$1" remaining
  while true; do
    remaining=$((warmup_deadline - SECONDS))
    [ "$remaining" -gt 0 ] || return 1
    if curl -sf --connect-timeout 5 --max-time "$remaining" -o /dev/null "$url"; then
      echo "    warmed $path ($((SECONDS - started))s)"
      return 0
    fi
    sleep 1
  done
}

echo ""
echo "    Warming routes concurrently (budget ${WARMUP_TIMEOUT}s): ${WARMUP_ROUTES[*]}"
warmup_pids=()
for path in "${WARMUP_ROUTES[@]}"; do
  warm_route "$path" &
  warmup_pids+=($!)
done
warmup_cold=""
for i in $(seq 0 $((${#WARMUP_ROUTES[@]} - 1))); do
  wait "${warmup_pids[$i]}" || warmup_cold="$warmup_cold ${WARMUP_ROUTES[$i]}"
done
if [ -n "$warmup_cold" ]; then
  echo "    WARNING: warm-up budget (${WARMUP_TIMEOUT}s) exhausted, still cold:$warmup_cold" >&2
  echo "             the first spec that opens each of these pays the compile" >&2
fi

# --------------------------------------------------------------------------
# Step 4: Specs
# --------------------------------------------------------------------------
echo ""
echo "    Effective URLs:"
echo "      backend        http://localhost:${PORT}"
echo "      frontend       ${FRONTEND_ORIGIN}"
echo "      browser -> API ${PLAYWRIGHT_BASE_URL} proxied to ${REMOTE_API_URL} (TURBO_ENV_MODE=${TURBO_ENV_MODE})"
if [ -n "$NEXT_PUBLIC_WAS_SET" ]; then
  echo "    NOTE: NEXT_PUBLIC_API_URL/NEXT_PUBLIC_WS_URL were unset for this run — they take the" >&2
  echo "          browser off the frontend origin and the realtime spec fails on the websocket." >&2
fi

echo ""
echo "==> [4/4] Playwright E2E specs..."
if pnpm exec playwright test; then
  echo "✓ E2E passed."
  exit 0
else
  echo "E2E SPECS FAILED (stack was healthy; this is a test failure, not a bootstrap failure)" >&2
  exit "$EXIT_SPECS"
fi
