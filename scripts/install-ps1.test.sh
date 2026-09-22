#!/usr/bin/env bash
# Gate for the Windows installer (#447).
#
# Two layers, because CI and most developer machines have no PowerShell:
#
#  1. Static parity checks — always run. They derive the variable list from
#     scripts/selfhost-env.sh itself and assert install.ps1 carries the same
#     one, so a secret added on the bash side cannot quietly leave Windows
#     stands without it (which is exactly how #447 happened).
#  2. scripts/install.test.ps1 — the behavioral mirror of install.test.sh, run
#     only when `pwsh` exists; skipped with a clear message otherwise.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PS1="$ROOT_DIR/scripts/install.ps1"
SH_ENV="$ROOT_DIR/scripts/selfhost-env.sh"
SH_INSTALL="$ROOT_DIR/scripts/install.sh"

fail() { echo "install-ps1 parity: $*" >&2; exit 1; }

# --- 0. the file must at least be lexable as PowerShell ---------------------
# Layer 2 (install.test.ps1) is skipped wherever pwsh is missing, which is most
# machines and was enough to ship an install.ps1 that PowerShell could not
# PARSE: a bash single-quote escape ('"'"'), pasted into a PowerShell
# double-quoted string, left the line with an unterminated string and the whole
# installer dead on every invocation. These two checks cost nothing and catch
# that class without a PowerShell runtime.
test_no_bash_quote_escapes_leaked_into_powershell() {
  if grep -n "'\"'\"'" "$PS1"; then
    fail "install.ps1 contains a bash-style quote escape ('\"'\"') — PowerShell cannot parse it"
  fi
}

test_single_line_output_statements_have_balanced_quotes() {
  local bad
  # Here-string openers (`@"`) legitimately end a line on one quote; every
  # other Write-* line is a single-line string statement and must balance.
  bad="$(awk '/Write-(Host|Info|Fail|Warn|Ok)/ && !/@"$/ { n = gsub(/"/, "\""); if (n % 2) printf "%d: %s\n", FNR, $0 }' "$PS1")"
  [ -z "$bad" ] || fail "unbalanced double quotes in install.ps1 (PowerShell parse error):"$'\n'"$bad"
}

# --- 1. every variable selfhost-env.sh writes must exist in install.ps1 -----
# The bash source is the single source of truth; this reads it rather than
# repeating a hand-kept list that would drift the same way the installer did.
test_generated_variables_match_selfhost_env() {
  local vars key
  # Two ways selfhost-env.sh writes a variable: a sed anchor (^KEY=) and
  # set_env_kv KEY. Reading only the first missed everything the deployment
  # presets write (ALLOW_SIGNUP, RATE_LIMIT_*, GOOSAR_EXTERNAL_IMAGES, ...).
  vars="$( { grep -oE '\^[A-Z][A-Z0-9_]*=' "$SH_ENV" | tr -d '^='
             grep -oE 'set_env_kv [A-Z][A-Z0-9_]*' "$SH_ENV" | awk '{print $2}'
           } | sort -u)"
  [ -n "$vars" ] || fail "could not extract any variable names from selfhost-env.sh"
  for key in $vars; do
    grep -q "$key" "$PS1" || fail "$key is written by selfhost-env.sh but never appears in install.ps1"
  done
}

# The PUBLIC_HOST-derived origin list lives in one `for key in ...` line in
# selfhost-env.sh; install.ps1 must derive the same set.
test_public_host_derivation_covers_the_same_keys() {
  local line key
  line="$(grep -E '^\s*for key in .*FRONTEND_ORIGIN' "$SH_ENV" | head -n1)"
  [ -n "$line" ] || fail "could not find the PUBLIC_HOST derivation list in selfhost-env.sh"
  line="${line#*for key in }"
  line="${line%; do}"
  for key in $line; do
    grep -q "\"$key\"" "$PS1" || fail "PUBLIC_HOST derives $key in selfhost-env.sh but not in install.ps1"
  done
  grep -q 'PUBLIC_HOST' "$PS1" || fail "install.ps1 has no PUBLIC_HOST handling at all"
}

# --- 2. the behaviors #428 gave the bash installer -------------------------
test_delivery_profile_default_is_perimeter() {
  grep -q 'GOOSAR_DELIVERY_PROFILE' "$PS1" || fail "install.ps1 never sets GOOSAR_DELIVERY_PROFILE"
  grep -q '"perimeter"' "$PS1" || fail "install.ps1 does not default the delivery profile to perimeter"
  grep -qE '"cloud",\s*"perimeter"' "$PS1" || fail "install.ps1 does not validate the delivery profile against cloud|perimeter"
}

test_image_tag_is_pinned_from_release_tags() {
  grep -q 'GOOSAR_IMAGE_TAG' "$PS1" || fail "install.ps1 never pins GOOSAR_IMAGE_TAG"
  grep -q "sort=-v:refname" "$PS1" || fail "install.ps1 does not derive the newest release tag from the clone"
}

test_secret_encodings_match() {
  # secretbox.ValidateKeyEnv requires exactly 32 base64-decoded bytes: the two
  # secretbox keys must be base64, everything else hex, same as selfhost-env.sh.
  grep -q 'New-RandomBase64 32' "$PS1" || fail "install.ps1 must generate the secretbox keys as 32 random bytes in base64"
  grep -q 'ToBase64String' "$PS1" || fail "install.ps1 has no base64 encoder"
  grep -q 'RandomNumberGenerator' "$PS1" || fail "install.ps1 must use System.Security.Cryptography.RandomNumberGenerator"
  grep -q 'New-RandomHex 32' "$PS1" || fail "JWT_SECRET must be 32 random bytes as hex"
  grep -q 'New-RandomHex 24' "$PS1" || fail "the passwords must be 24 random bytes as hex"
}

# docker compose reads a BOM and a CR as part of the value, so .env must be
# UTF-8 without BOM and LF-only. Set-Content gives CRLF (and a BOM on Windows
# PowerShell), Get-Content decodes a BOM-less file through the ANSI codepage.
test_env_is_written_as_utf8_lf() {
  grep -q 'UTF8Encoding($false)' "$PS1" || fail "install.ps1 must write .env as UTF-8 without BOM"
  grep -q 'WriteAllText' "$PS1" || fail "install.ps1 must write .env through System.IO.File, not Set-Content"
  grep -q 'ReadAllLines' "$PS1" || fail "install.ps1 must read .env through System.IO.File, not Get-Content"
  grep -qE 'Set-Content[^"]*"?\.env' "$PS1" && fail "install.ps1 must not write .env with Set-Content (CRLF + BOM)"
  return 0
}

# --- 2b. #530 review: order and gate, checkable without a PowerShell runtime -
# A rejected profile must fail BEFORE `Copy-Item .env.example .env`, or the
# stand keeps a full, profile-less .env and the corrective re-run silently does
# nothing — the bug fixed on the bash side in scripts/selfhost-env.sh.
test_profile_validation_precedes_the_env_copy() {
  local validate delivery copy
  validate="$(grep -n "GOOSAR_DEPLOYMENT_PROFILE must be" "$PS1" | head -n1 | cut -d: -f1)"
  delivery="$(grep -n "GOOSAR_DELIVERY_PROFILE must be" "$PS1" | head -n1 | cut -d: -f1)"
  copy="$(grep -n 'Copy-Item ".env.example" ".env"' "$PS1" | head -n1 | cut -d: -f1)"
  [ -n "$validate" ] || fail "install.ps1 does not validate GOOSAR_DEPLOYMENT_PROFILE at all"
  [ -n "$delivery" ] || fail "install.ps1 does not validate GOOSAR_DELIVERY_PROFILE at all"
  [ -n "$copy" ] || fail "could not find the .env.example copy in install.ps1"
  [ "$validate" -lt "$copy" ] ||
    fail "install.ps1 validates the deployment profile at line $validate, after copying .env.example at line $copy — a typo would leave a profile-less .env behind"
  [ "$delivery" -lt "$copy" ] ||
    fail "install.ps1 validates the delivery profile at line $delivery, after copying .env.example at line $copy"
}

# ALLOW_SIGNUP=false with no allowlist is a stand nobody — the operator
# included — can sign into. Both installers gate it in the FINAL summary, where
# the operator looks: the mid-run message is lost behind `docker pull` output.
test_closed_signup_is_gated_in_the_summary() {
  local summary gate
  grep -q "warn_signup_closed_without_allowlist" "$SH_INSTALL" ||
    fail "install.sh no longer gates closed signup — update this parity check"
  grep -q "ALLOWED_EMAIL_DOMAINS" "$PS1" ||
    fail "install.ps1 never mentions ALLOWED_EMAIL_DOMAINS, the only way past ALLOW_SIGNUP=false"
  summary="$(awk '/^function Start-LocalInstall/{f=1} f' "$PS1")"
  grep -q "Write-SignupClosedWithoutAllowlistWarning" <<<"$summary" ||
    fail "Start-LocalInstall does not call Write-SignupClosedWithoutAllowlistWarning"
  # The gate itself must read both allowlists, not just the domain one.
  gate="$(awk '/^function Write-SignupClosedWithoutAllowlistWarning/{f=1} f{print} f && /^}/{exit}' "$PS1")"
  grep -q "ALLOW_SIGNUP" <<<"$gate" || fail "the signup gate does not read ALLOW_SIGNUP"
  grep -q "ALLOWED_EMAIL_DOMAINS" <<<"$gate" || fail "the signup gate does not read ALLOWED_EMAIL_DOMAINS"
  grep -q "ALLOWED_EMAILS" <<<"$gate" || fail "the signup gate does not read ALLOWED_EMAILS"
}

test_existing_env_is_never_overwritten() {
  grep -q 'Test-Path ".env"' "$PS1" || fail "install.ps1 does not check for an existing .env"
  grep -q 'left untouched' "$PS1" || fail "install.ps1 does not report that an existing .env is left alone"
  # ...and says so about the profile too: silence reads as "the profile switched".
  grep -q 'was NOT applied' "$PS1" ||
    fail "install.ps1 does not warn that a profile is NOT applied to an existing .env"
  grep -q 'was NOT applied' "$SH_ENV" ||
    fail "selfhost-env.sh no longer carries that warning — update this parity check"
}

# --- 3. flag parity with install.sh ----------------------------------------
# Every install.sh option must have a documented PowerShell equivalent. The
# mapping table lives in the install.ps1 header; this checks both sides of it.
test_every_install_sh_flag_has_a_powershell_equivalent() {
  # bash 3.2 on macOS has no associative arrays: "flag=Equivalent" pairs.
  local pairs="--with-server=-WithServer --upgrade=-Upgrade --stop=-Stop --tag=-Tag
    --profile=-DeliveryProfile --resend-key=-ResendKey --smtp-host=-SmtpHost
    --smtp-port=-SmtpPort --smtp-user=-SmtpUser --smtp-password=-SmtpPassword
    --smtp-from=-SmtpFrom --smtp-tls=-SmtpTls --llm-key=-LlmKey
    --llm-base-url=-LlmBaseUrl"
  local pair flag ps
  for pair in $pairs; do
    flag="${pair%%=*}"
    ps="${pair#*=}"
    grep -q -- "$flag" "$SH_INSTALL" || fail "$flag is no longer an install.sh flag — update the parity table"
    grep -q -- "$ps" "$PS1" || fail "install.sh $flag has no $ps in install.ps1"
    # Documented side by side in the header table.
    grep -q -- "$flag " "$PS1" || fail "install.ps1 header does not document the $flag equivalent"
  done
}

test_env_keys_written_by_the_flags_match() {
  local key
  for key in RESEND_API_KEY SMTP_HOST SMTP_PORT SMTP_USERNAME SMTP_PASSWORD SMTP_FROM_EMAIL \
             SMTP_TLS GOOSAR_LLM_API_KEY GOOSAR_LLM_BASE_URL; do
    grep -q "$key" "$SH_INSTALL" || fail "$key is no longer written by install.sh — update this list"
    grep -q "$key" "$PS1" || fail "install.sh writes $key from a flag; install.ps1 does not"
  done
}

test_generated_variables_match_selfhost_env
test_public_host_derivation_covers_the_same_keys
test_no_bash_quote_escapes_leaked_into_powershell
test_single_line_output_statements_have_balanced_quotes
test_delivery_profile_default_is_perimeter
test_image_tag_is_pinned_from_release_tags
test_secret_encodings_match
test_existing_env_is_never_overwritten
test_profile_validation_precedes_the_env_copy
test_closed_signup_is_gated_in_the_summary
test_env_is_written_as_utf8_lf
test_every_install_sh_flag_has_a_powershell_equivalent
test_env_keys_written_by_the_flags_match
echo "install.ps1 static parity checks passed"

# --- 4. behavioral tests, when PowerShell is available ----------------------
if ! command -v pwsh >/dev/null 2>&1; then
  echo "SKIP: pwsh not found — scripts/install.test.ps1 not run."
  echo "      Install PowerShell (brew install --cask powershell, or the Windows"
  echo "      runner in CI) to exercise the .env bootstrap itself."
  exit 0
fi

pwsh -NoProfile -File "$ROOT_DIR/scripts/install.test.ps1"
