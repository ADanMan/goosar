#!/usr/bin/env bash
# Static parity gate for the served Windows installer (#567).
#
# Same shape as scripts/install-ps1.test.sh and for the same reason: most
# machines and CI jobs have no PowerShell, and a behavioral test alone would
# leave apps/web/public/install.ps1 unchecked almost everywhere. The checks
# derive what they expect from install.sh and Dockerfile.web — the bash
# installer is the source of truth for flags, env vars and the checksum
# contract; the Dockerfile is the source of truth for what /cli/ serves.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
PS1="$ROOT_DIR/apps/web/public/install.ps1"
SH="$ROOT_DIR/apps/web/public/install.sh"
DOCKERFILE="$ROOT_DIR/Dockerfile.web"

fail() { echo "web install-ps1 parity: $*" >&2; exit 1; }

[ -f "$PS1" ] || fail "apps/web/public/install.ps1 is missing"

# --- 0. lexable as PowerShell ------------------------------------------------
test_no_bash_quote_escapes_leaked_into_powershell() {
  if grep -n "'\"'\"'" "$PS1"; then
    fail "install.ps1 contains a bash-style quote escape — PowerShell cannot parse it"
  fi
}

test_braces_balance() {
  local opens closes
  opens="$(tr -cd '{' < "$PS1" | wc -c | tr -d ' ')"
  closes="$(tr -cd '}' < "$PS1" | wc -c | tr -d ' ')"
  [ "$opens" = "$closes" ] || fail "install.ps1 has $opens '{' and $closes '}'"
}

# --- 1. every install.sh flag has a PowerShell parameter --------------------
test_every_install_sh_flag_has_a_parameter() {
  local flag param
  while read -r flag; do
    case "$flag" in
      --app-url) param="AppUrl" ;;
      --server-url) param="ServerUrl" ;;
      --install-dir) param="InstallDir" ;;
      --skip-checksum) param="SkipChecksum" ;;
      --runner) param="Runner" ;;
      # Aliases of --runner hermes / --runner none: the Windows installer has
      # no bundled runner to alias them to, -Runner covers the choice.
      --with-agent|--without-agent) continue ;;
      --version) param="Version" ;;   # refused with a reason on both sides (#566)
      --help) param="Help" ;;
      *) fail "install.sh grew a flag this gate does not map: $flag" ;;
    esac
    grep -qE "^\s*\[(string|switch)\]\\\$$param\b" "$PS1" || fail "install.sh has $flag but install.ps1 has no -$param"
  done < <(grep -oE '^\s+--[a-z-]+\)' "$SH" | tr -d ' )' | sort -u)
}

# --- 2. the same env equivalents ---------------------------------------------
test_env_variables_match() {
  local var
  for var in GOOSAR_APP_URL GOOSAR_SERVER_URL GOOSAR_BIN_DIR GOOSAR_SKIP_CHECKSUM GOOSAR_RUNNER; do
    grep -q "$var" "$SH" || fail "install.sh no longer reads $var — update this gate"
    grep -q "env:$var" "$PS1" || fail "install.ps1 does not read $var"
  done
}

# --- 3. the four fail-closed checksum branches -------------------------------
test_checksum_contract_is_fail_closed() {
  grep -q "Checksum verification skipped" "$PS1" || fail "no loud warning for -SkipChecksum"
  grep -q "Could not download checksums.txt - aborting" "$PS1" || fail "a missing checksums.txt must abort"
  grep -q "has no entry for" "$PS1" || fail "a missing entry must abort"
  grep -q "Checksum verification failed" "$PS1" || fail "a mismatch must abort"
  grep -q "Checksum verified" "$PS1" || fail "a match must be reported"
  # The message alone is not the gate: pin the comparison that guards it, so a
  # flipped -ne/-eq cannot leave the text in place and the check inverted.
  grep -q 'if (\$actual -ne \$expected)' "$PS1" || fail "the checksum mismatch branch must be `if (\$actual -ne \$expected)`"
}

# --- 4. the archive it asks for is the one Dockerfile.web builds -------------
test_archive_name_matches_dockerfile() {
  grep -q 'windows/amd64' "$DOCKERFILE" || fail "Dockerfile.web builds no windows/amd64 CLI"
  grep -q 'bin=goosar.exe' "$DOCKERFILE" || fail "Dockerfile.web does not name the Windows binary goosar.exe"
  grep -q '"goosar-cli-windows-amd64.tar.gz"' "$PS1" || fail "install.ps1 asks for a different archive than Dockerfile.web produces"
  grep -q 'tar.exe -xzf' "$PS1" || fail "install.ps1 must extract with the tar.exe Windows ships (no zip is built)"
}

# --- 5. PATH is edited without expanding %VAR% entries ------------------------
test_user_path_is_read_unexpanded_and_written_as_expand_string() {
  grep -q 'DoNotExpandEnvironmentNames' "$PS1" || fail "user PATH must be read unexpanded (REG_EXPAND_SZ)"
  grep -q 'RegistryValueKind\]::ExpandString' "$PS1" || fail "user PATH must be written back as ExpandString"
  if grep -q 'SetEnvironmentVariable("Path"' "$PS1"; then
    fail "install.ps1 uses [Environment]::SetEnvironmentVariable for PATH — that expands and destroys %VAR% entries"
  fi
}

# --- 6. no GitHub, no sudo — it installs from the deployment ------------------
test_no_github_dependency() {
  if grep -qi 'github.com' "$PS1"; then
    fail "install.ps1 references github.com — the served installer must only use <app-url>/cli/"
  fi
}

# --- 7. a broken binary fails the install (#566 parity) -----------------------
test_version_is_refused_like_install_sh() {
  grep -q 'one CLI version' "$SH" || fail "install.sh no longer refuses --version — update this gate"
  grep -q 'one CLI version' "$PS1" || fail "install.ps1 must refuse -Version with the same reason as install.sh"
}

test_verify_is_fatal() {
  grep -q -- '--version' "$PS1" || fail "install.ps1 does not run goosar.exe --version"
  grep -q 'does not run on this machine' "$PS1" || fail "a failing --version must be fatal with the reason"
  grep -q 'if (\$probeExit -ne 0)' "$PS1" || fail "the --version verdict must be the exit code: `if (\$probeExit -ne 0)`"
  grep -q '\$ErrorActionPreference = "Continue"' "$PS1" || fail "the --version probe must relax ErrorActionPreference (PS 5.1 turns merged stderr into a terminating error)"
}

# --- 8. install.sh points Windows users at this script ------------------------
test_install_sh_points_windows_users_here() {
  grep -q 'install.ps1' "$SH" || fail "install.sh does not tell Windows users about install.ps1"
}

test_no_bash_quote_escapes_leaked_into_powershell
test_braces_balance
test_every_install_sh_flag_has_a_parameter
test_env_variables_match
test_checksum_contract_is_fail_closed
test_archive_name_matches_dockerfile
test_user_path_is_read_unexpanded_and_written_as_expand_string
test_no_github_dependency
test_version_is_refused_like_install_sh
test_verify_is_fatal
test_install_sh_points_windows_users_here

if command -v pwsh >/dev/null 2>&1; then
  # Cheapest possible behavioral check where pwsh exists: the script parses
  # and -Help exits 0 before touching the network.
  pwsh -NoProfile -File "$PS1" -Help >/dev/null || fail "pwsh could not parse or run install.ps1 -Help"
  echo "apps/web/public/install.ps1 parity checks passed (static + pwsh -Help)"
else
  echo "apps/web/public/install.ps1 parity checks passed (static; pwsh not found)"
fi
