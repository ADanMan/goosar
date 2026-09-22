#!/usr/bin/env bash
# Tests for scripts/acceptance-client.sh (0.11.0, T-19 / #582): the clean-VM
# client acceptance as a script. Hermetic — a fake `goosar` on PATH answers
# every subcommand from files the case controls, HOME is a temp dir, and no
# step reaches a network or a GUI (the operator prompts are answered from
# stdin).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/acceptance-client.sh"
PASS=0; FAIL=0
ok() { echo "ok: $1"; PASS=$((PASS + 1)); }
bad() { echo "FAIL: $1 — $2"; FAIL=$((FAIL + 1)); }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/home"

# The fake goosar: each subcommand reads its answer from $FAKE_DIR/<name>.
cat > "$TMP/bin/goosar" <<'STUB'
#!/usr/bin/env bash
echo "goosar $*" >> "$FAKE_LOG"
case "$1 $2" in
  "version --output"|"version "*) cat "$FAKE_DIR/version.json"; exit 0 ;;
  "doctor --server") cat "$FAKE_DIR/doctor-server.json"; [ -f "$FAKE_DIR/doctor-server.fail" ] && exit 1; exit 0 ;;
  "doctor "*|"doctor") cat "$FAKE_DIR/doctor.json"; [ -f "$FAKE_DIR/doctor.fail" ] && exit 1; exit 0 ;;
  "config set") exit 0 ;;
  "login --token") exit "${FAKE_LOGIN_EXIT:-0}" ;;
  "daemon start") exit 0 ;;
  "daemon status") cat "$FAKE_DIR/daemon-status.json"; exit 0 ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/goosar"

healthy_fixtures() {
  local d="$1"; mkdir -p "$d"
  printf '{"version":"0.11.0","commit":"abc"}\n' > "$d/version.json"
  printf '{"ok":false,"results":[{"id":"goosar","status":"ok","name":"goosar в PATH"},{"id":"agent-cli","status":"missing","name":"Agent CLI"},{"id":"node","status":"ok","name":"Node.js"}]}\n' > "$d/doctor.json"
  printf '{"ok":true,"results":[{"id":"goosar","status":"ok"},{"id":"agent-cli","status":"missing"},{"id":"config","status":"ok"},{"id":"server","status":"ok","detail":"ответил, версия v0.11.0"},{"id":"certificate","status":"ok"},{"id":"daemon","status":"ok"},{"id":"kit-platform","status":"ok","detail":"5 доступно, 5 установлено"}]}\n' > "$d/doctor-server.json"
  printf '{"status":"running","device_name":"vm"}\n' > "$d/daemon-status.json"
}

run_acceptance() { # run_acceptance <fixture-dir> <stdin-text> [args...]
  local d="$1" input="$2"; shift 2
  : > "$d/calls.log"
  printf '%s' "$input" | PATH="$TMP/bin:$PATH" HOME="$TMP/home" FAKE_DIR="$d" FAKE_LOG="$d/calls.log" \
    GOOSAR_ACCEPTANCE_WAIT=1 NO_COLOR=1 bash "$SCRIPT" "$@" 2>&1
}

# --- a healthy machine with a token: every scripted step passes, GUI steps ask -
D1="$TMP/ok"; healthy_fixtures "$D1"
out="$(run_acceptance "$D1" $'y\ny\n' --tag v0.11.0 --server-url https://stand.local --app-url https://stand.local --token gsl_x --ca-file "$TMP/ca.pem")" && rc=0 || rc=$?
if [ "$rc" -eq 0 ]; then ok "healthy run exits 0"; else bad "healthy run exits 0" "rc=$rc $out"; fi
for row in "goosar в PATH" "версия" "doctor" "конфигурация" "вход" "демон" "doctor --server" "провижининг" "задача агенту"; do
  if printf '%s' "$out" | grep -q "PASS.*$row"; then ok "row PASS: $row"; else bad "row PASS: $row" "$out"; fi
done
if grep -q "goosar config set server_url https://stand.local" "$D1/calls.log" &&
   grep -q "goosar login --token" "$D1/calls.log" &&
   grep -q "goosar daemon start" "$D1/calls.log"; then
  ok "the non-interactive path configures, logs in with the token and starts the daemon"
else
  bad "non-interactive path" "$(cat "$D1/calls.log")"
fi
if printf '%s' "$out" | grep -q "agent-cli"; then ok "the missing agent CLI is reported, not hidden"; else bad "agent-cli reported" "$out"; fi

# --- the token must never appear in the log ----------------------------------
if grep -q "gsl_x" "$D1/calls.log"; then bad "token not on argv" "the token reached the CLI's argv"; else ok "the token travels on stdin, never on argv"; fi

# --- version mismatch is a FAIL row -----------------------------------------
D2="$TMP/ver"; healthy_fixtures "$D2"; printf '{"version":"0.10.1"}\n' > "$D2/version.json"
out="$(run_acceptance "$D2" $'y\ny\n' --tag v0.11.0 --server-url https://s --app-url https://s --token gsl_x)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "FAIL.*версия.*0.10.1"; then ok "a version mismatch fails and names both versions"; else bad "version mismatch" "rc=$rc $out"; fi

# --- a daemon that never reaches running is a FAIL row ------------------------
D3="$TMP/daemon"; healthy_fixtures "$D3"; printf '{"status":"stopped"}\n' > "$D3/daemon-status.json"
out="$(run_acceptance "$D3" $'y\ny\n' --tag v0.11.0 --server-url https://s --app-url https://s --token gsl_x)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "FAIL.*демон"; then ok "a stopped daemon fails the run"; else bad "stopped daemon" "rc=$rc $out"; fi

# --- the operator answering "n" to the task step fails the run ----------------
D4="$TMP/task"; healthy_fixtures "$D4"
out="$(run_acceptance "$D4" $'y\nn\n' --tag v0.11.0 --server-url https://s --app-url https://s --token gsl_x)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "FAIL.*задача агенту"; then ok "an operator 'n' on the task step fails the run"; else bad "operator n" "rc=$rc $out"; fi

# --- no goosar at all: the first row fails and the rest are skipped ---------
D5="$TMP/none"; healthy_fixtures "$D5"
out="$(printf '' | PATH="$TMP/nowhere:/usr/bin:/bin" HOME="$TMP/home" FAKE_DIR="$D5" FAKE_LOG="$D5/calls.log" GOOSAR_ACCEPTANCE_WAIT=1 NO_COLOR=1 bash "$SCRIPT" --tag v0.11.0 --server-url https://s --app-url https://s --token gsl_x 2>&1)" && rc=0 || rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "FAIL.*goosar в PATH" && printf '%s' "$out" | grep -q "SKIPPED.*демон"; then
  ok "without goosar the first row fails and the dependent rows are skipped"
else
  bad "no goosar" "rc=$rc $out"
fi

# --- --report writes a Markdown table the acceptance doc pastes ---------------
D6="$TMP/report"; healthy_fixtures "$D6"
run_acceptance "$D6" $'y\ny\n' --tag v0.11.0 --server-url https://s --app-url https://s --token gsl_x --report "$TMP/report.md" >/dev/null || true
if [ -f "$TMP/report.md" ] && grep -q '^| PASS | goosar в PATH' "$TMP/report.md"; then ok "--report writes a Markdown table"; else bad "--report" "$(cat "$TMP/report.md" 2>/dev/null)"; fi

echo
echo "acceptance-client.test.sh: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
