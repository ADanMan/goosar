#!/usr/bin/env bash
# Tests for scripts/check-ui-strings.mjs (#430). The scanner is the regression
# guard that stops new hardcoded English UI copy from landing in the component
# directories this issue translated, so it needs its own proof that it still
# detects a violation -- a scanner that silently matches nothing would keep
# passing forever.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/check-ui-strings.mjs"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() { printf 'FAIL %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ ! -f "$SCRIPT" ]; then
  printf 'FAIL scanner missing at %s\n' "$SCRIPT" >&2
  exit 1
fi

node --check "$SCRIPT" && pass "scanner parses" || failed "scanner does not parse"

# --- the repository itself is clean -----------------------------------------
if node "$SCRIPT" >"$TMP/repo-out" 2>&1; then
  pass "guarded directories are free of hardcoded UI strings"
else
  failed "scanner reports violations in the repo:
$(cat "$TMP/repo-out")"
fi

# --- JSX text node ----------------------------------------------------------
mkdir -p "$TMP/case-jsx"
cat >"$TMP/case-jsx/bad.tsx" <<'EOF'
export function Panel() {
  return <div>Endpoint unreachable</div>;
}
EOF
if node "$SCRIPT" --dir "$TMP/case-jsx" >"$TMP/out" 2>&1; then
  failed "expected non-zero exit for a hardcoded JSX text node"
else
  grep -q 'Endpoint unreachable' "$TMP/out" &&
    pass "flags hardcoded JSX text and names the string" ||
    failed "violation text missing from output: $(cat "$TMP/out")"
fi

# --- user-facing call argument ----------------------------------------------
mkdir -p "$TMP/case-call"
cat >"$TMP/case-call/bad.tsx" <<'EOF'
export function save() {
  toast.error("Daemon settings saved");
}
EOF
if node "$SCRIPT" --dir "$TMP/case-call" >"$TMP/out" 2>&1; then
  failed "expected non-zero exit for a hardcoded toast string"
else
  pass "flags hardcoded toast/alert copy"
fi

# --- user-facing JSX attribute ----------------------------------------------
mkdir -p "$TMP/case-attr"
cat >"$TMP/case-attr/bad.tsx" <<'EOF'
export function Close() {
  return <button aria-label="Close the daemon panel" />;
}
EOF
if node "$SCRIPT" --dir "$TMP/case-attr" >"$TMP/out" 2>&1; then
  failed "expected non-zero exit for a hardcoded aria-label"
else
  pass "flags hardcoded aria-label/placeholder/title copy"
fi

# --- description prop: where the settings rows keep their longest copy -------
mkdir -p "$TMP/case-description"
cat >"$TMP/case-description/bad.tsx" <<'EOF'
export function Row() {
  return (
    <SettingsRow
      description="Automatically start the daemon when the app opens."
    />
  );
}
EOF
if node "$SCRIPT" --dir "$TMP/case-description" >"$TMP/out" 2>&1; then
  failed "expected non-zero exit for a hardcoded description prop"
else
  grep -q 'Automatically start the daemon' "$TMP/out" &&
    pass "flags hardcoded description copy" ||
    failed "description violation missing from output: $(cat "$TMP/out")"
fi

# --- translated code passes -------------------------------------------------
mkdir -p "$TMP/case-good"
cat >"$TMP/case-good/good.tsx" <<'EOF'
import { useT } from "@goosar/views/i18n";

export function Panel({ runtime }: { runtime: string }) {
  const { t } = useT("settings");
  // Endpoint unreachable is only described in this comment.
  return (
    <div className="flex items-center gap-2" data-testid="daemon-panel">
      <span>{t(($) => $.desktop.daemon.endpoint_unreachable)}</span>
      <code>GOOSAR_GATEWAY_URL</code>
      <button aria-label={t(($) => $.desktop.daemon.logs.close)}>
        {runtime}
      </button>
    </div>
  );
}
EOF
if node "$SCRIPT" --dir "$TMP/case-good" >"$TMP/out" 2>&1; then
  pass "i18n-routed code, class names, identifiers and comments pass"
else
  failed "false positive on translated code: $(cat "$TMP/out")"
fi

# --- allowlisted brand names and identifiers pass ---------------------------
mkdir -p "$TMP/case-allow"
cat >"$TMP/case-allow/allow.tsx" <<'EOF'
export function Brands() {
  return (
    <>
      <span>Kerberos</span>
      <span>Codex</span>
      <span>pipx</span>
      <span>{"—"}</span>
    </>
  );
}
EOF
if node "$SCRIPT" --dir "$TMP/case-allow" >"$TMP/out" 2>&1; then
  pass "brand names and identifiers are allowlisted"
else
  failed "allowlist did not cover brand names: $(cat "$TMP/out")"
fi

# --- CI runs the guard ------------------------------------------------------
if grep -q 'bash scripts/check-ui-strings.test.sh' "$ROOT_DIR/.github/workflows/ci.yml"; then
  pass "CI runs the guard"
else
  failed "CI does not run scripts/check-ui-strings.test.sh"
fi

if [ "$FAILURES" -gt 0 ]; then
  printf '\n%d check-ui-strings test(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf '\nall check-ui-strings tests passed\n'
