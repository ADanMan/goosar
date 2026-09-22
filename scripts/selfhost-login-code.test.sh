#!/usr/bin/env bash
# Tests for scripts/selfhost-login-code.sh (#295). Uses a stub `docker` so no
# real compose stack is needed.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/selfhost-login-code.sh"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() { printf 'FAIL %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

bash -n "$SCRIPT" && pass "script parses" || failed "script does not parse"

make_stub() {
  # stub compose command that prints $1 as backend logs
  cat >"$TMP/fake-compose" <<STUB
#!/usr/bin/env bash
cat "$1"
STUB
  chmod +x "$TMP/fake-compose"
}

# --- prints the LATEST code when several exist ------------------------------
cat >"$TMP/logs-two" <<'EOF'
backend-1  | some noise
backend-1  | [DEV] Verification code for a@b.c: 111111
backend-1  | more noise
backend-1  | [DEV] Verification code for a@b.c: 222222 (login link: http://x)
EOF
make_stub "$TMP/logs-two"
out="$(COMPOSE="$TMP/fake-compose" bash "$SCRIPT")"
if [ "$(printf '%s' "$out" | grep -c 'Verification code')" = "1" ] &&
   printf '%s' "$out" | grep -q '222222'; then
  pass "prints only the latest verification code line"
else
  failed "expected latest code 222222 only, got: $out"
fi

# --- exits 1 with guidance when no code logged ------------------------------
cat >"$TMP/logs-none" <<'EOF'
backend-1  | starting up
EOF
make_stub "$TMP/logs-none"
if COMPOSE="$TMP/fake-compose" bash "$SCRIPT" >"$TMP/out" 2>"$TMP/err"; then
  failed "expected non-zero exit without a code"
else
  if grep -q 'make selfhost-code' "$TMP/err"; then
    pass "no code -> exit 1 with re-run guidance"
  else
    failed "guidance missing from stderr: $(cat "$TMP/err")"
  fi
fi

# --- Makefile wires the target ----------------------------------------------
if grep -q 'selfhost-code:' "$ROOT_DIR/Makefile" &&
   grep -q 'scripts/selfhost-login-code.sh' "$ROOT_DIR/Makefile"; then
  pass "make selfhost-code target delegates to the script"
else
  failed "make selfhost-code target missing"
fi

if [ "$FAILURES" -gt 0 ]; then
  printf '\n%d selfhost-login-code test(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf '\nall selfhost-login-code tests passed\n'
