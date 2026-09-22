#!/usr/bin/env bash
# Hermetic tests for scripts/test-e2e.sh (issue #212). Fakes pnpm/go/curl/
# ditto on PATH (each logs its argv), so no real browser, DB, or server is
# ever touched. Also asserts release-local.sh wires the e2e gate into the
# verify block.
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/test-e2e.sh"

PASS=0
FAIL=0
ok()   { echo "ok: $1"; PASS=$((PASS + 1)); }
bad()  { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/bin"
mkdir -p "$BIN"
export ARGV_LOG="$TMP/argv.log"
export STATE="$TMP/state"
mkdir -p "$STATE"

# --- fakes ----------------------------------------------------------------
cat > "$BIN/pnpm" <<'EOF'
#!/usr/bin/env bash
echo "pnpm $*" >> "$ARGV_LOG"
case "$*" in
  "exec playwright install chromium")
    case "${FAKE_PW_INSTALL:-ok}" in
      ok)   exit 0 ;;
      fail) exit 1 ;;
      hang) exec sleep 600 ;;
    esac ;;
  "exec playwright test")
    exit "${FAKE_PW_TEST_RC:-0}" ;;
  "dev:web")
    # Record the browser-facing env the frontend was actually started with.
    { echo "NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL-<unset>}"
      echo "NEXT_PUBLIC_WS_URL=${NEXT_PUBLIC_WS_URL-<unset>}"
    } > "$STATE/frontend-env"
    touch "$STATE/frontend-up"; exec sleep 600 ;;
esac
EOF
cat > "$BIN/go" <<'EOF'
#!/usr/bin/env bash
echo "go $*" >> "$ARGV_LOG"
case "$*" in
  "run ./cmd/server")
    if [ "${FAKE_BACKEND:-up}" = up ]; then touch "$STATE/backend-up"; fi
    # A CHILD that outlives this shell — the real `go run` does the same: it
    # builds a binary and runs it as a child, and THAT is what holds the port.
    # Killing only the recorded PID orphaned it (issue #450).
    sleep 600 &
    echo $! > "$STATE/backend-child.pid"
    exec sleep 600 ;;
esac
EOF
# The fake curl honours the flags the real one would: -f decides success from
# the status code, --max-time bounds a server that accepts the connection and
# never answers. A warm-up that drops either flag must be visible here.
cat > "$BIN/curl" <<'EOF'
#!/usr/bin/env bash
echo "curl $*" >> "$ARGV_LOG"
url="${@: -1}"
max_time=""
fail_on_error=0
prev=""
for a in "$@"; do
  case "$a" in
    --max-time) : ;;
    -f|-sf|-sfo) fail_on_error=1 ;;
  esac
  case "$prev" in --max-time) max_time="$a" ;; esac
  prev="$a"
done
case "$url" in
  *"/health"*) [ -f "$STATE/backend-up" ]; exit $? ;;
esac
# A URL with a path (not just the origin) is a warm-up request.
case "$url" in
  *:[0-9][0-9][0-9][0-9][0-9]/?*)
    case "${FAKE_WARMUP:-ok}" in
      fail) exit 22 ;;
      block)
        # A server that accepts the connection and never answers. Real curl
        # gives up after --max-time; without it, it blocks forever.
        sleep "${max_time:-600}"; exit 28 ;;
      slow404)
        # Answers fast, always non-2xx: only -f turns that into a failure.
        [ "$fail_on_error" = 1 ] && exit 22 || exit 0 ;;
      slow)
        # A compile: answers 200, but only after a while. Serial warm-up pays
        # this once PER ROUTE; a concurrent one pays it once for all of them.
        sleep "${FAKE_WARMUP_DELAY:-3}"
        [ -f "$STATE/frontend-up" ]; exit $? ;;
    esac
    [ -f "$STATE/frontend-up" ]; exit $? ;;
esac
[ -f "$STATE/frontend-up" ]
EOF
cat > "$BIN/ditto" <<'EOF'
#!/usr/bin/env bash
echo "ditto $*" >> "$ARGV_LOG"
exit 0
EOF
chmod +x "$BIN"/*

ENVF="$TMP/env"
cat > "$ENVF" <<EOF
BACKEND_PORT=59181
FRONTEND_PORT=59182
EOF

run_script() { # run_script <extra env...> — runs test-e2e.sh with fakes first on PATH
  env PATH="$BIN:$PATH" ENV_FILE="$ENVF" GOOSAR_E2E_SKIP_DB=1 \
    E2E_BACKEND_WAIT=3 E2E_FRONTEND_WAIT=3 E2E_INSTALL_TIMEOUT=2 \
    E2E_LOG_DIR="$TMP" PLAYWRIGHT_BROWSERS_PATH="$PW" \
    "$@" bash "$SCRIPT" 2>&1
}

run_script_bounded() { # run_script_bounded <seconds> <extra env...> — never hangs the suite
  local limit=$1; shift
  set -m   # own process group per job, so the watchdog can kill the whole tree
  run_script "$@" > "$TMP/bounded.out" 2>&1 &
  local pid=$!
  set +m
  # >/dev/null: the watchdog must not hold the caller's $( ) pipe open for
  # $limit seconds after the run itself has finished.
  ( sleep "$limit"; kill -9 -- "-$pid" 2>/dev/null; kill -9 "$pid" 2>/dev/null ) >/dev/null 2>&1 &
  local watchdog=$!
  local rc=0
  wait "$pid" 2>/dev/null || rc=$?
  kill "$watchdog" 2>/dev/null; wait "$watchdog" 2>/dev/null
  cat "$TMP/bounded.out"
  return "$rc"
}

reset_state() {
  rm -f "$ARGV_LOG" "$STATE"/* 2>/dev/null
  PW="$TMP/pw"; rm -rf "$PW"; mkdir -p "$PW/chromium-1"
  touch "$PW/chromium-1/INSTALLATION_COMPLETE"
}

# --- 1. happy path: gate ordering + teardown of what it started -----------
reset_state
out="$(run_script FAKE_PW_TEST_RC=0)"; rc=$?
[ "$rc" -eq 0 ] && ok "happy path exits 0" || bad "happy path exit=$rc: $out"
# ordering: install -> server -> dev:web -> playwright test
seq="$(grep -nE 'playwright install|run \./cmd/server|dev:web|playwright test' "$ARGV_LOG" | cut -d: -f2- | paste -sd'|' -)"
case "$seq" in
  *"install chromium"*"run ./cmd/server"*"dev:web"*"playwright test"*) ok "gate ordering install->backend->frontend->specs" ;;
  *) bad "gate ordering; got: $seq" ;;
esac
printf '%s' "$out" | grep -q "Stopped PID" && ok "teardown ran for started services" || bad "no teardown in: $out"

# --- 2. specs fail: exit 1, distinct message, still torn down -------------
reset_state
out="$(run_script FAKE_PW_TEST_RC=1)"; rc=$?
[ "$rc" -eq 1 ] && ok "spec failure exits 1" || bad "spec failure exit=$rc"
printf '%s' "$out" | grep -q "E2E SPECS FAILED" && ok "spec failure message distinct" || bad "missing SPECS FAILED: $out"
printf '%s' "$out" | grep -q "Stopped PID" && ok "teardown on spec failure" || bad "no teardown on spec failure"

# ...and the teardown must reach the whole process TREE it started. Killing
# only the recorded PID left `go run`'s real server (and `turbo dev`'s
# `next dev`) holding the gate's port, so the next release run died with
# "already running" and the operator had to hunt the PID by hand (issue #450).
child="$(cat "$STATE/backend-child.pid" 2>/dev/null || true)"
child_alive=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ -n "$child" ] && kill -0 "$child" 2>/dev/null || { child_alive=0; break; }
  child_alive=1
  sleep 0.2
done
if [ "$child_alive" = 1 ]; then
  bad "trap left the backend's child process $child alive — the gate port stays busy"
  kill -9 "$child" 2>/dev/null
else
  ok "trap kills the whole started process group, not just the recorded PID"
fi

# --- 2b. proxy env defaults (issue #450) ----------------------------------
# The browser must reach the API through the Next proxy on the frontend
# origin. turbo's strict env mode drops REMOTE_API_URL (not in globalEnv), and
# the v0.9.0 gate silently tested whatever backend the proxy defaulted to.
reset_state
out="$(run_script FAKE_PW_TEST_RC=0)"
printf '%s' "$out" | grep -q "proxied to http://localhost:59181" \
  && ok "effective URLs printed with the proxy upstream" || bad "no effective URLs in: $out"
printf '%s' "$out" | grep -q "TURBO_ENV_MODE=loose" \
  && ok "next dev runs with TURBO_ENV_MODE=loose" || bad "TURBO_ENV_MODE not loose"
# An operator-supplied value must win over the default.
out="$(run_script FAKE_PW_TEST_RC=0 REMOTE_API_URL=http://localhost:19999)"
printf '%s' "$out" | grep -q "proxied to http://localhost:19999" \
  && ok "REMOTE_API_URL from the environment wins" || bad "REMOTE_API_URL default overrode the caller"
# NEXT_PUBLIC_* is the fix that does NOT work: it takes the browser
# cross-origin and the realtime spec fails on the websocket. The script must
# never set it...
grep -qE '^[[:space:]]*export[[:space:]]+NEXT_PUBLIC_' "$SCRIPT" \
  && bad "test-e2e.sh exports NEXT_PUBLIC_* — that breaks the realtime spec" \
  || ok "test-e2e.sh does not export NEXT_PUBLIC_*"
# ...and warning about an inherited value is not enough: init-worktree-env.sh
# writes both into .env.worktree (the file the release gate hands over first)
# and the Makefile exports them to `make e2e`, so the gate got them on every
# real run. They must be UNSET for the frontend this script starts.
reset_state
out="$(run_script FAKE_PW_TEST_RC=0 NEXT_PUBLIC_API_URL=http://localhost:59181 NEXT_PUBLIC_WS_URL=ws://localhost:59181/ws)"
printf '%s' "$out" | grep -q "NEXT_PUBLIC_API_URL/NEXT_PUBLIC_WS_URL were unset" \
  && ok "a preset NEXT_PUBLIC_* is called out" || bad "no note for a preset NEXT_PUBLIC_*"
frontend_env="$(cat "$STATE/frontend-env" 2>/dev/null)"
case "$frontend_env" in
  *"NEXT_PUBLIC_API_URL=<unset>"*"NEXT_PUBLIC_WS_URL=<unset>"*)
    ok "the frontend is started without NEXT_PUBLIC_*" ;;
  *) bad "NEXT_PUBLIC_* reached the frontend: $frontend_env" ;;
esac

# --- 3. backend never healthy: exit 2, distinct message, teardown ---------
reset_state
out="$(run_script FAKE_BACKEND=down)"; rc=$?
[ "$rc" -eq 2 ] && ok "server-didn't-start exits 2" || bad "server-didn't-start exit=$rc"
printf '%s' "$out" | grep -q "E2E BOOTSTRAP FAILED" && ok "bootstrap failure message distinct" || bad "missing BOOTSTRAP FAILED: $out"
printf '%s' "$out" | grep -q "Stopped PID" && ok "teardown on bootstrap failure" || bad "no teardown on bootstrap failure"
grep -q "playwright test" "$ARGV_LOG" && bad "specs ran despite dead backend" || ok "specs not run when backend dead"

# --- 3b. REQUIRE_FRESH: pre-existing stack is refused, not reused ---------
reset_state
touch "$STATE/backend-up" "$STATE/frontend-up"
out="$(run_script GOOSAR_E2E_REQUIRE_FRESH=1)"; rc=$?
[ "$rc" -eq 2 ] && printf '%s' "$out" | grep -q "GOOSAR_E2E_REQUIRE_FRESH=1" \
  && ok "REQUIRE_FRESH refuses a pre-existing stack (exit 2)" || bad "REQUIRE_FRESH exit=$rc: $out"
grep -q "playwright test" "$ARGV_LOG" && bad "specs ran against pre-existing stack under REQUIRE_FRESH" || ok "specs not run under REQUIRE_FRESH refusal"
# and without the flag the same stack is reused
reset_state
touch "$STATE/backend-up" "$STATE/frontend-up"
out="$(run_script FAKE_PW_TEST_RC=0)"; rc=$?
[ "$rc" -eq 0 ] && printf '%s' "$out" | grep -q "leaving it alone" \
  && ok "without REQUIRE_FRESH a healthy stack is reused" || bad "reuse path exit=$rc: $out"
printf '%s' "$out" | grep -q "Stopped PID" && bad "tore down a stack it did not start" || ok "no teardown of a reused stack"

# --- 4. install fails + no marker: ditto fallback unpacks zip + markers ---
reset_state
rm -rf "$PW"; mkdir -p "$PW"
: > "$PW/chromium-9.zip"
out="$(run_script FAKE_PW_INSTALL=fail FAKE_PW_TEST_RC=0)"; rc=$?
grep -q "ditto -x -k $PW/chromium-9.zip" "$ARGV_LOG" && ok "ditto fallback invoked with -x -k" || bad "no ditto call: $(cat "$ARGV_LOG")"
[ -f "$PW/chromium-9/INSTALLATION_COMPLETE" ] && [ -f "$PW/chromium-9/DEPENDENCIES_VALIDATED" ] \
  && ok "fallback creates INSTALLATION_COMPLETE + DEPENDENCIES_VALIDATED" || bad "markers missing after fallback"
[ "$rc" -eq 0 ] && ok "run proceeds after fallback" || bad "fallback run exit=$rc: $out"

# --- 5. install hangs: timeout fires, fallback still reached --------------
reset_state
rm -rf "$PW"; mkdir -p "$PW"
: > "$PW/chromium-9.zip"
start=$SECONDS
out="$(run_script FAKE_PW_INSTALL=hang FAKE_PW_TEST_RC=0)"; rc=$?
[ $((SECONDS - start)) -lt 60 ] && ok "hung install is killed by timeout" || bad "install hang not bounded"
[ "$rc" -eq 0 ] && grep -q "ditto" "$ARGV_LOG" && ok "hang path falls through to ditto" || bad "hang path exit=$rc"

# --- 6. install fails and no zip anywhere: bootstrap failure (exit 2) -----
reset_state
rm -rf "$PW"; mkdir -p "$PW"
out="$(run_script FAKE_PW_INSTALL=fail)"; rc=$?
[ "$rc" -eq 2 ] && printf '%s' "$out" | grep -q "E2E BOOTSTRAP FAILED" \
  && ok "no-browser-no-zip is a bootstrap failure" || bad "no-zip exit=$rc: $out"

# --- 6b. route warm-up before the specs (issue #519) ----------------------
# The release gate runs from a FRESH worktree: `next dev` compiles each route
# on the first request, and no spec budget is a compilation budget
# (navigation.spec 30s, auth.spec waitForURL 10s, playwright.config 60s).
# Warm every route the specs reach.
reset_state
out="$(run_script FAKE_PW_TEST_RC=0)"; rc=$?
[ "$rc" -eq 0 ] && ok "warm-up run exits 0" || bad "warm-up run exit=$rc: $out"
UUID_RE='[0-9a-f-]\{36\}'
for route in "/login" "/onboarding" "/[^/]*/inbox" "/[^/]*/agents" "/[^/]*/settings" "/[^/]*/issues" "/[^/]*/issues/$UUID_RE" "/[^/]*/agents/$UUID_RE"; do
  grep -q "curl .*localhost:59182$route\( \|$\)" "$ARGV_LOG" \
    && ok "warm-up requests $route" || bad "no warm-up request for $route: $(cat "$ARGV_LOG")"
done
# The warm-up must not fire requests that escape the phase deadline. The RSC
# leg (`-H 'RSC: 1'`, hardcoded --max-time 60) did: measured, it costs 0.08s
# after the document request — it compiles nothing the document did not.
grep -q "RSC" "$ARGV_LOG" && bad "warm-up still fires an RSC request outside the phase deadline" \
  || ok "warm-up fires only the document request, all under one deadline"
grep -q -- "--max-time 60" "$ARGV_LOG" \
  && bad "a warm-up request uses a hardcoded --max-time instead of the remaining budget" \
  || ok "every warm-up request is clamped to the remaining budget"
# ordering: frontend up -> warm-up -> specs
seq="$(grep -nE 'dev:web|localhost:59182/[^/]*/inbox|playwright test' "$ARGV_LOG" | cut -d: -f2- | paste -sd'|' -)"
case "$seq" in
  *"dev:web"*"/inbox"*"playwright test"*) ok "warm-up runs after next dev and before the specs" ;;
  *) bad "warm-up ordering; got: $seq" ;;
esac

# Order follows what the specs actually need: EVERY spec starts in
# e2e/helpers.ts `loginAsDefault`, which lands on /${slug}/issues, so /login
# and /issues must be warmed before the routes only some specs reach. The
# order is printed once, so this assertion cannot race the concurrent requests.
order_line="$(printf '%s' "$out" | grep 'Warming routes')"
case "$order_line" in
  *"/login"*"/issues"*"/inbox"*"/agents"*) ok "warm-up order puts /login and /issues first" ;;
  *) bad "warm-up order does not lead with /login + /issues: $order_line" ;;
esac
case "$order_line" in
  *"/onboarding"*) ok "the order line lists the whole route set" ;;
  *) bad "the order line is not the full route list: $order_line" ;;
esac
# The default budget must cover a cold tree: measured serially on the release
# machine the eight routes took ~743s, which 900s barely covers and a loaded
# machine does not.
reset_state
out="$(run_script FAKE_PW_TEST_RC=0)"
printf '%s' "$out" | grep -q "budget 1800s" \
  && ok "the default warm-up budget is 1800s" || bad "default budget is not 1800s: $(printf '%s' "$out" | grep -i budget)"

# The requests are fired CONCURRENTLY: Next compiles a queue, and eight serial
# round-trips make the phase the sum of eight compiles. With a 3s "compile" per
# route, a serial phase costs 24s+ and a concurrent one ~3s.
reset_state
start=$SECONDS
run_script_bounded 90 FAKE_PW_TEST_RC=0 FAKE_WARMUP=slow FAKE_WARMUP_DELAY=3 > /tmp/.warm-conc.out 2>&1
took=$((SECONDS - start))
[ "$took" -lt 15 ] && ok "warm-up requests are fired concurrently (${took}s for 8x3s routes)" \
  || bad "warm-up is still serial: ${took}s for 8 routes of 3s each"
grep -c "warmed " /tmp/.warm-conc.out > /dev/null && \
  [ "$(grep -c 'warmed ' /tmp/.warm-conc.out)" -eq 8 ] \
  && ok "all 8 routes report as warmed" || bad "warmed lines: $(grep -c 'warmed ' /tmp/.warm-conc.out)"

# When the budget expires the gate must SAY which routes stayed cold, and still
# run the specs — a warm-up warning must never hide a spec failure.
reset_state
out="$(run_script_bounded 60 FAKE_PW_TEST_RC=0 FAKE_WARMUP=fail E2E_WARMUP_TIMEOUT=2)"; rc=$?
printf '%s' "$out" | grep -q "still cold:.*/login" \
  && ok "an exhausted budget names the routes that stayed cold" \
  || bad "no cold-route list on budget exhaustion: $out"
cold_named="$(printf '%s' "$out" | grep 'still cold:' | grep -o '/[A-Za-z0-9./-]*' | wc -l | tr -d ' ')"
[ "$cold_named" -ge 8 ] && ok "every cold route is named, not just the first ($cold_named)" \
  || bad "only $cold_named cold routes named"
[ "$rc" -eq 0 ] && ok "an exhausted budget still runs the specs (exit 0)" || bad "exhausted budget exit=$rc"


# Success must be judged by the STATUS CODE (`curl -f`), not by "the server
# answered": a route that 500s in dev would otherwise count as warmed and the
# specs would silently pay the full cold compile.
reset_state
out="$(run_script_bounded 60 FAKE_PW_TEST_RC=0 FAKE_WARMUP=slow404 E2E_WARMUP_TIMEOUT=3)"; rc=$?
printf '%s' "$out" | grep -q "WARNING: warm-up budget" \
  && ok "a non-2xx route is not counted as warmed (curl -f)" || bad "non-2xx counted as warmed: $out"
[ "$rc" -eq 0 ] && ok "a non-2xx warm-up does not fail the gate" || bad "non-2xx warm-up exit=$rc"

# A dev server that accepts the connection and NEVER answers must not wedge the
# release gate: curl gets --max-time and the phase has one wall clock.
reset_state
start=$SECONDS
out="$(run_script_bounded 60 FAKE_PW_TEST_RC=0 FAKE_WARMUP=block E2E_WARMUP_TIMEOUT=5)"; rc=$?
took=$((SECONDS - start))
[ "$took" -lt 45 ] && ok "an unresponsive route is bounded by wall clock (${took}s)" \
  || bad "warm-up not bounded against a hanging server (${took}s)"
grep -q -- "--max-time" "$ARGV_LOG" && ok "warm-up curl passes --max-time" || bad "warm-up curl has no --max-time"
grep -q "playwright test" "$ARGV_LOG" && ok "specs still run after a hanging warm-up" || bad "specs skipped after hanging warm-up"

# A route that never becomes ready must be bounded and non-fatal: the specs
# still run (with their own timeouts) instead of the gate hanging forever.
reset_state
start=$SECONDS
out="$(run_script_bounded 60 FAKE_PW_TEST_RC=0 FAKE_WARMUP=fail E2E_WARMUP_TIMEOUT=2)"; rc=$?
[ $((SECONDS - start)) -lt 60 ] && ok "warm-up is bounded when a route never returns 200" || bad "warm-up not bounded"
[ "$rc" -eq 0 ] && ok "a failed warm-up does not fail the gate" || bad "failed warm-up exit=$rc: $out"
printf '%s' "$out" | grep -qi "warm" && ok "a failed warm-up is reported" || bad "no warm-up warning: $out"
grep -q "playwright test" "$ARGV_LOG" && ok "specs still run after a failed warm-up" || bad "specs skipped after failed warm-up"

# The whole phase shares ONE budget: 8 unreachable routes must not add
# 8 x budget to the gate's failure latency.
reset_state
start=$SECONDS
run_script_bounded 60 FAKE_PW_TEST_RC=0 FAKE_WARMUP=fail E2E_WARMUP_TIMEOUT=4 > /dev/null 2>&1
took=$((SECONDS - start))
[ "$took" -lt 30 ] && ok "the warm-up budget is for the whole phase, not per route (${took}s)" \
  || bad "per-route budgets multiply: ${took}s"

# The reported time must be the wall clock the compile actually took, not a
# retry counter: the common case is one blocking request that then succeeds.
reset_state
touch "$STATE/backend-up" "$STATE/frontend-up"
out="$(run_script FAKE_PW_TEST_RC=0)"
printf '%s' "$out" | grep -q "warmed /login (" && ok "warm-up prints per-route timing" || bad "no timing line: $out"

# The warm-up must also happen for a stack this script did NOT start.
reset_state
touch "$STATE/backend-up" "$STATE/frontend-up"
out="$(run_script FAKE_PW_TEST_RC=0)"
grep -q "curl .*localhost:59182/[^/]*/inbox" "$ARGV_LOG" \
  && ok "reused stack is warmed too" || bad "no warm-up on the reuse path"

# The budget knob is an operator knob: a wrong value must be a BOOTSTRAP
# failure with a clear message, not a crash. `E2E_WARMUP_TIMEOUT=15m` used to
# blow up the warm-up arithmetic and exit 1 — the code this script's contract
# reserves for "the specs failed".
# (an EMPTY value means "unset" and falls back to the default, as everywhere else)
for badval in 15m 0 -5 1e3 900s; do
  reset_state
  out="$(run_script FAKE_PW_TEST_RC=0 E2E_WARMUP_TIMEOUT="$badval")"; rc=$?
  [ "$rc" -eq 2 ] && printf '%s' "$out" | grep -q "E2E_WARMUP_TIMEOUT must be a positive" \
    && ok "E2E_WARMUP_TIMEOUT='$badval' is a bootstrap failure with a clear message" \
    || bad "E2E_WARMUP_TIMEOUT='$badval' exit=$rc: $out"
done
# ...and it is rejected UP FRONT, before the multi-minute browser install.
reset_state
run_script FAKE_PW_TEST_RC=0 E2E_WARMUP_TIMEOUT=15m > /dev/null 2>&1
grep -q "playwright install" "$ARGV_LOG" \
  && bad "a bad E2E_WARMUP_TIMEOUT was caught only after the browser install" \
  || ok "a bad E2E_WARMUP_TIMEOUT is caught before the browser install"
# A good value still runs.
reset_state
out="$(run_script FAKE_PW_TEST_RC=0 E2E_WARMUP_TIMEOUT=120)"; rc=$?
[ "$rc" -eq 0 ] && ok "a valid E2E_WARMUP_TIMEOUT runs the gate" || bad "valid budget exit=$rc: $out"

# Drift guard: the route families are READ OUT OF THE SPECS, not hardcoded
# here — otherwise a spec that starts visiting a new route (a Skills entry in
# the sidebar walk, say) leaves that route cold and both this suite and a gate
# on an already-warm dev tree stay green until the next release build.
# Every `/segment` a spec navigates to (goto / waitForURL / toHaveURL) must
# appear in the warm-up block. The leading-character class keeps regex starts
# such as `not.toHaveURL(/connected=notion/)` out: a real path segment is
# preceded by a quote, a backtick, `}`, `*`, `\` or `/`.
spec_routes_in() { # spec_routes_in <dir> — route paths the specs there navigate to
  # `[A-Za-z0-9/-]` keeps NESTED segments: `/${slug}/agents/new` is /agents/new,
  # not /agents. A trailing `/` (from `/${slug}/issues/${issueId}`) is trimmed.
  grep -hoE '(goto|waitForURL|toHaveURL)\([^)]*' "$1"/*.ts \
    | grep -oE '[\\"`}*/]/[A-Za-z][A-Za-z0-9/-]*' \
    | sed -e 's|^.||' -e 's|/*$||' | sort -u
}
warm_block_of() { # warm_block_of <script> — Step 3b's route list, comments stripped
  # Comments must not count: a `/login` mentioned in the block's own prose is
  # not a warmed route.
  sed -n '/Step 3b/,/Step 4: Specs/p' "$1" | sed '/^[[:space:]]*#/d'
}
uncovered() { # uncovered <routes> <block> — the routes the block does not warm
  local lit missing=""
  for lit in $1; do
    printf '%s' "$2" | grep -q -- "$lit" || missing="$missing $lit"
  done
  printf '%s' "$missing"
}

# The extractor itself, pinned on a fixture: a nested route must survive, and a
# route named only in a comment must NOT count as warmed.
FIXDIR="$TMP/specfix"; mkdir -p "$FIXDIR"
cat > "$FIXDIR/fake.spec.ts" <<'FIXEOF'
await page.goto(`/${slug}/agents/new`, { waitUntil: "domcontentloaded" });
await page.goto("/login");
await expect(page).not.toHaveURL(/connected=notion/);
FIXEOF
fix_routes="$(spec_routes_in "$FIXDIR" | tr "\n" " ")"
case " $fix_routes " in
  *" /agents/new "*) ok "extractor keeps nested segments (/agents/new)" ;;
  *) bad "extractor lost the nested segment; got: $fix_routes" ;;
esac
case " $fix_routes " in
  *" /connected "*) bad "extractor picked up a regex fragment: $fix_routes" ;;
  *) ok "extractor ignores regex fragments such as /connected=notion/" ;;
esac
fix_block="$(printf '%s\n' '# Step 3b' '#   "/login" \' '  "/$WARMUP_SLUG/agents" \' '# Step 4: Specs')"
fix_block="$(printf '%s' "$fix_block" | sed '/^[[:space:]]*#/d')"
fix_missing="$(uncovered "$fix_routes" "$fix_block")"
case " $fix_missing " in
  *" /login "*) ok "a route named only in a comment is reported as uncovered" ;;
  *) bad "a commented-out /login counted as warmed (missing:$fix_missing)" ;;
esac
case " $fix_missing " in
  *" /agents/new "*) ok "a nested route absent from the block is reported" ;;
  *) bad "/agents/new not reported although the block only warms /agents" ;;
esac

# ...and the guard itself, over the real specs and the real script.
spec_routes="$(spec_routes_in "$ROOT_DIR/e2e")"
missing="$(uncovered "$spec_routes" "$(warm_block_of "$SCRIPT")")"
[ -n "$spec_routes" ] && ok "drift guard read $(printf '%s' "$spec_routes" | grep -c .) route families out of e2e/*.ts" \
  || bad "drift guard found no routes in e2e/*.ts — the extraction broke"
[ -z "$missing" ] && ok "warm-up list covers the spec routes" || bad "warm-up list missing:$missing"

# --- 7. release-local.sh wiring: e2e gate inside verify block, before docker
RL="$ROOT_DIR/scripts/release-local.sh"
# `[[:space:]]` not `\s`: BSD grep reads `\s` as a literal `s`, so on macOS the
# comment filter matched nothing and the header's mention of scripts/test-e2e.sh
# was taken for the gate.
gate_line="$(grep -n 'scripts/test-e2e.sh' "$RL" | grep -v '^[0-9]*:[[:space:]]*#' | head -1 | cut -d: -f1)"
race_line="$(grep -n 'bash scripts/test-go.sh --race' "$RL" | head -1 | cut -d: -f1)"
docker_line="$(grep -n '2\. docker images' "$RL" | head -1 | cut -d: -f1)"
skip_fi_line="$(awk "NR>$race_line && /^fi/ {print NR; exit}" "$RL")"
if [ -n "$gate_line" ] && [ "$gate_line" -gt "$race_line" ] && [ "$gate_line" -lt "$skip_fi_line" ] && [ "$gate_line" -lt "$docker_line" ]; then
  ok "release-local.sh runs e2e gate after go tests, inside the verify block, before docker"
else
  bad "release-local.sh wiring (gate=$gate_line race=$race_line fi=$skip_fi_line docker=$docker_line)"
fi
grep -q 'set -euo pipefail' "$RL" && ok "release-local.sh aborts on e2e failure (set -e)" || bad "release-local.sh not set -e"
grep -q 'GOOSAR_E2E_REQUIRE_FRESH=1 bash scripts/test-e2e.sh' "$RL" \
  && ok "release-local.sh requires a fresh stack for the e2e gate" || bad "release-local.sh missing GOOSAR_E2E_REQUIRE_FRESH=1"
# The gate must run the TAG's tree, not the operator checkout (issue #450).
grep -q 'cd "\$WORKTREE" && ENV_FILE="\$E2E_ENV_FILE" GOOSAR_E2E_REQUIRE_FRESH=1 bash scripts/test-e2e.sh' "$RL" \
  && ok "release-local.sh runs the e2e gate from the tag worktree" \
  || bad "release-local.sh still runs the e2e gate from the operator checkout"

echo
echo "test-e2e.test.sh: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
