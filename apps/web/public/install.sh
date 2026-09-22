#!/usr/bin/env bash
# Goosar — CLI installer served by the Goosar server itself.
#
# The Goosar repository is private, so this installer never talks to
# GitHub. It downloads the `goosar` CLI from the same Goosar deployment that
# served this script.
#
# Install:
#   curl -fsSL https://goosar.example.com/install.sh | bash -s -- \
#     --app-url https://goosar.example.com \
#     --server-url https://goosar.example.com
#
# When piped into bash the script cannot know its own origin, so --app-url
# (or GOOSAR_APP_URL) is required.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
APP_URL="${GOOSAR_APP_URL:-}"
SERVER_URL="${GOOSAR_SERVER_URL:-}"
INSTALL_DIR="${GOOSAR_BIN_DIR:-}"
DEFAULT_INSTALL_DIR="$HOME/.local/bin"
# Escape hatch for GH #136's fail-closed checksum verification (see
# verify_checksum). Empty means "verify"; any non-empty value skips
# verification with a loud warning. Set via --skip-checksum or this env var.
SKIP_CHECKSUM="${GOOSAR_SKIP_CHECKSUM:-}"
# Agent runner selection. The default is "no runner": nothing runner-related is
# downloaded or installed unless it is chosen. The choice comes from --runner
# (or GOOSAR_RUNNER); with neither, an interactive run is asked once and a
# non-interactive run gets `none`.
RUNNER="${GOOSAR_RUNNER:-}"
# The runners this installer can set up. Keep `none` first: it is the default.
RUNNER_CHOICES="none hermes"
# Where the prompt reads its answer from. `curl | bash` occupies stdin with the
# script itself, so the answer comes from the controlling terminal.
PROMPT_TTY="${GOOSAR_INSTALL_TTY:-/dev/tty}"

OS=""
ARCH=""
TMP_DIR=""
STAGED_FILE=""

# Colors (disabled when not a terminal)
if [ -t 1 ] || [ -t 2 ]; then
  BOLD='\033[1m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  RED='\033[0;31m'
  CYAN='\033[0;36m'
  RESET='\033[0m'
else
  BOLD='' GREEN='' YELLOW='' RED='' CYAN='' RESET=''
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok()    { printf "${BOLD}${GREEN}[ok] %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}[warn] %s${RESET}\n" "$*" >&2; }
fail()  { printf "${BOLD}${RED}[error] %s${RESET}\n" "$*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
  if [ -n "$STAGED_FILE" ] && [ -f "$STAGED_FILE" ]; then
    rm -f "$STAGED_FILE"
  fi
  return 0
}
trap cleanup EXIT INT TERM

usage() {
  cat <<'USAGE'
Goosar — CLI installer

Usage:
  curl -fsSL https://<goosar-host>/install.sh | bash -s -- --app-url https://<goosar-host> [options]

Options:
  --app-url <url>       Goosar web URL that serves this script and the CLI
                        binaries. Required (env: GOOSAR_APP_URL).
  --server-url <url>    Goosar API URL. When given, the installer prints the
                        exact CLI commands to connect (env: GOOSAR_SERVER_URL).
  --install-dir <dir>   Where to put the `goosar` binary
                        (default: $HOME/.local/bin, env: GOOSAR_BIN_DIR).
  --skip-checksum       Skip checksums.txt verification of the downloaded
                        archive (env: GOOSAR_SKIP_CHECKSUM). Prints a loud
                        warning. Only use this in environments you trust —
                        by default the installer refuses to install an
                        archive it could not verify.
  --runner <name>       Agent runner to install: none | hermes
                        (default: none; env: GOOSAR_RUNNER). Without the
                        flag an interactive run asks; a non-interactive run
                        installs no runner. `hermes` is the agent runtime
                        bundled with this deployment.
  --with-agent          Alias of --runner hermes.
  --without-agent       Alias of --runner none.
  --help, -h            Show this help.

Download URL convention (served by the Goosar deployment):
  <app-url>/cli/goosar-cli-<os>-<arch>.tar.gz
  <app-url>/cli/hermes-<os>-<arch>.tar.gz     (agent runtime, only with --runner hermes)
  <app-url>/cli/checksums.txt                  (required unless --skip-checksum)

A deployment serves exactly one CLI version — the one matching its server.
To get another version, install from a deployment of that version.

where <os> is darwin|linux and <arch> is amd64|arm64.

No sudo is required and the install is safe to re-run.
USAGE
}

# Strip trailing slashes and default the scheme to https.
normalize_url() {
  local url="$1"
  while [ "$url" != "${url%/}" ]; do
    url="${url%/}"
  done
  case "$url" in
    *://*) : ;;
    *) url="https://$url" ;;
  esac
  printf '%s' "$url"
}

has_http_scheme() {
  case "$1" in
    http://*|https://*) return 0 ;;
    *) return 1 ;;
  esac
}

path_contains() {
  # #566: "$HOME/.local/bin/" and "$HOME/.local/bin" are the same directory;
  # comparing raw strings printed the PATH hint to people who already had it.
  local want="${1%/}" entry
  local IFS=:
  for entry in $PATH; do
    [ "${entry%/}" = "$want" ] && return 0
  done
  return 1
}

shell_rc_file() {
  local shell_name
  shell_name="$(basename "${SHELL:-sh}")"
  case "$shell_name" in
    zsh) printf '%s' "$HOME/.zshrc" ;;
    bash)
      if [ "$OS" = "darwin" ]; then
        printf '%s' "$HOME/.bash_profile"
      else
        printf '%s' "$HOME/.bashrc"
      fi
      ;;
    *) printf '%s' "$HOME/.profile" ;;
  esac
}

detect_platform() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
      fail "This installer does not support Windows. In PowerShell run:

  & ([scriptblock]::Create((irm $APP_URL/install.ps1))) -AppUrl $APP_URL${SERVER_URL:+ -ServerUrl $SERVER_URL}

or install the Goosar desktop app instead." ;;
    *)
      fail "Unsupported operating system: $(uname -s).
The Goosar CLI supports macOS, Linux and Windows (install.ps1)." ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)   ARCH="amd64" ;;
    arm64|aarch64)  ARCH="arm64" ;;
    *) fail "Unsupported architecture: $(uname -m). Supported: x86_64/amd64, arm64/aarch64." ;;
  esac
}

tarball_name() {
  printf 'goosar-cli-%s-%s.tar.gz' "$OS" "$ARCH"
}

agent_tarball_name() {
  printf 'hermes-%s-%s.tar.gz' "$OS" "$ARCH"
}

sha256_of() {
  local file="$1"
  if command_exists shasum; then
    shasum -a 256 "$file" | awk '{print $1}'
  elif command_exists sha256sum; then
    sha256sum "$file" | awk '{print $1}'
  else
    printf ''
  fi
}

# Fail-closed (GH #136): checksums.txt not downloadable, no entry for this
# file, and no sha256 utility available now ALL abort the install, same as an
# actual mismatch. Previously only a mismatch aborted and the other three
# cases silently continued with an unverified binary - an attacker able to
# swap the published archive could just as easily suppress checksums.txt (or
# the entry for their forged file) and verification was bypassed entirely.
# --skip-checksum / GOOSAR_SKIP_CHECKSUM is the one explicit escape hatch
# for genuinely constrained environments; it warns loudly and is never the
# default.
verify_checksum() {
  local archive="$1"
  local name="$2"
  local url="$3"
  local sums="$TMP_DIR/checksums.txt"
  local hint="If you understand the risk and need to proceed anyway, re-run with --skip-checksum (or set GOOSAR_SKIP_CHECKSUM=1)."

  if [ -n "$SKIP_CHECKSUM" ]; then
    warn "Checksum verification skipped (--skip-checksum). The downloaded archive is NOT verified -- only use this in environments you trust."
    return 0
  fi

  if ! curl -fsSL "$url" -o "$sums" 2>/dev/null; then
    fail "Could not download checksums.txt from:
  $url

Aborting: the download cannot be verified.
$hint"
  fi

  local expected
  expected="$(awk -v n="$name" '{ f=$2; sub(/^\*/, "", f); sub(/^.*\//, "", f); if (f == n) { print $1; exit } }' "$sums")"
  if [ -z "$expected" ]; then
    fail "checksums.txt has no entry for $name.

Aborting: the download cannot be verified.
$hint"
  fi

  local actual
  actual="$(sha256_of "$archive")"
  if [ -z "$actual" ]; then
    fail "Neither shasum nor sha256sum is available.

Aborting: the download cannot be verified. Install shasum or sha256sum, or:
$hint"
  fi

  if [ "$expected" != "$actual" ]; then
    fail "Checksum mismatch for $name.
  expected: $expected
  actual:   $actual
Aborting: the download is corrupted or has been tampered with."
  fi

  ok "Checksum verified"
}

download_cli() {
  local name url
  name="$(tarball_name)"
  url="$APP_URL/cli/$name"

  TMP_DIR="$(mktemp -d)"

  info "Downloading $url"
  if ! curl -fsSL "$url" -o "$TMP_DIR/$name"; then
    fail "Failed to download the CLI from:
  $url

Check that:
  - $APP_URL is reachable from this machine
  - this Goosar deployment bundles CLI binaries under /cli/ (it may not)
  - a $OS/$ARCH build exists on this deployment

Ask your Goosar administrator to publish the CLI binaries, or install the
Goosar desktop app instead."
  fi

  verify_checksum "$TMP_DIR/$name" "$name" "$APP_URL/cli/checksums.txt"

  if ! tar -xzf "$TMP_DIR/$name" -C "$TMP_DIR" goosar 2>/dev/null; then
    fail "Downloaded archive does not contain a 'goosar' executable: $name"
  fi
  if [ ! -f "$TMP_DIR/goosar" ]; then
    fail "Downloaded archive does not contain a 'goosar' executable: $name"
  fi
}

install_cli() {
  local dir="$1"

  mkdir -p "$dir" || fail "Could not create install directory: $dir"
  if [ ! -w "$dir" ]; then
    fail "Install directory is not writable: $dir
Pick a writable location with --install-dir <dir>."
  fi

  # Stage inside the target directory so the final move is atomic and also
  # replaces a binary that is currently running.
  STAGED_FILE="$dir/.goosar.install.$$"
  cp "$TMP_DIR/goosar" "$STAGED_FILE"
  chmod +x "$STAGED_FILE"
  mv -f "$STAGED_FILE" "$dir/goosar"
  STAGED_FILE=""

  ok "Installed $dir/goosar"
}

verify_cli() {
  local path="$1"
  local output
  if output="$("$path" --version 2>&1)"; then
    ok "$output"
  else
    # #566: this was a warning, and the one way this installer reported
    # success on a broken install — a wrong-architecture binary, a truncated
    # download the checksum happened to match, a missing loader.
    fail "Installed, but '$path --version' failed:
$(printf '%s\n' "$output" | sed 's/^/  /')

The binary was written to $path but does not run on this machine.
Check that $OS/$ARCH is the platform you expected ('uname -sm'), then
re-run the installer. Remove $path if you do not want the broken copy around."
  fi
}

# runner_is_known <name> — true when the installer can set that runner up.
runner_is_known() {
  case " $RUNNER_CHOICES " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

# runner_prompt_available — an interactive run: stdout is a terminal and the
# prompt device can really be opened (/dev/tty exists as a file even when the
# process has no controlling terminal). GOOSAR_INSTALL_INTERACTIVE=1 forces the
# prompt for scripted runs that feed answers through GOOSAR_INSTALL_TTY.
runner_prompt_available() {
  [ "${GOOSAR_INSTALL_INTERACTIVE:-}" = "1" ] && return 0
  [ -t 1 ] || return 1
  ( exec 3<"$PROMPT_TTY" ) 2>/dev/null
}

# choose_runner settles RUNNER: an explicit choice wins, otherwise an
# interactive run is asked and everything else gets `none`.
choose_runner() {
  if [ -n "$RUNNER" ]; then
    runner_is_known "$RUNNER" || fail "Unknown runner '$RUNNER'. Available: ${RUNNER_CHOICES// /, }"
    return 0
  fi
  if ! runner_prompt_available; then
    RUNNER="none"
    return 0
  fi

  local answer=""
  printf "\n"
  printf "  ${BOLD}Agent runner${RESET}\n"
  printf "  Choose an agent runner to install next to the goosar CLI:\n"
  printf "\n"
  printf "     1) none      only the goosar CLI (default)\n"
  printf "     2) hermes   the agent runtime bundled with this deployment\n"
  printf "\n"
  printf "  Choice [1]: "
  IFS= read -r answer <"$PROMPT_TTY" || answer=""
  answer="$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  case "$answer" in
    ""|1|none) RUNNER="none" ;;
    2|hermes) RUNNER="hermes" ;;
    *)
      warn "Unrecognised choice '$answer' — installing no runner."
      RUNNER="none" ;;
  esac
}

# install_agent_runtime downloads the hermes agent tarball, verifies it with
# the same fail-closed checksum logic as the goosar CLI (verify_checksum),
# then hands it to the archive's own bin/install-artifact.sh — that script
# owns the hermes-agent install contract (${HERMES_HOME:-~/.hermes}/runtime/<version>
# plus a `current` symlink), this installer never reimplements it.
#
# A 404 for this OS/arch is not fatal: the goosar CLI already installed
# successfully, so this warns and returns 0 rather than failing the whole run.
install_agent_runtime() {
  local name url status archive_dir extract_dir
  name="$(agent_tarball_name)"
  url="$APP_URL/cli/$name"

  info "Downloading agent runtime $url"
  archive_dir="$(mktemp -d)"
  status="$(curl -sS -o "$archive_dir/$name" -w '%{http_code}' "$url" 2>/dev/null || true)"

  if [ "$status" = "404" ]; then
    warn "No hermes agent artifact published for $OS/$ARCH at:
  $url

Skipping agent install -- the goosar CLI itself installed fine. Ask your
Goosar administrator to publish agent binaries, or install hermes
separately later."
    rm -rf "$archive_dir"
    return 0
  fi
  if [ "$status" != "200" ]; then
    fail "Failed to download the hermes agent runtime from:
  $url
(HTTP status: $status)"
  fi

  verify_checksum "$archive_dir/$name" "$name" "$APP_URL/cli/checksums.txt"

  extract_dir="$(mktemp -d)"
  if ! tar -xzf "$archive_dir/$name" -C "$extract_dir"; then
    fail "Downloaded agent archive is corrupt: $name"
  fi

  # build-artifact.sh packs the archive with a top-level
  # hermes-<version>-<os>-<arch>/ directory, so bin/install-artifact.sh may
  # live directly under extract_dir (flat) or one level down (wrapped).
  # Detect whichever layout is present rather than assuming one.
  local artifact_root=""
  local candidate
  for candidate in "$extract_dir" "$extract_dir"/*/; do
    candidate="${candidate%/}"
    if [ -x "$candidate/bin/install-artifact.sh" ]; then
      artifact_root="$candidate"
      break
    fi
  done
  if [ -z "$artifact_root" ]; then
    fail "Downloaded agent archive does not contain an executable bin/install-artifact.sh: $name"
  fi

  info "Installing agent runtime"
  if ! "$artifact_root/bin/install-artifact.sh" --artifact "$artifact_root"; then
    fail "bin/install-artifact.sh failed to install the hermes agent runtime."
  fi
  rm -rf "$archive_dir" "$extract_dir"

  local agent_home agent_path
  agent_home="${HERMES_HOME:-$HOME/.hermes}"
  agent_path="$agent_home/runtime/current/bin/hermes"
  ok "Installed hermes agent runtime"
  printf "\n"
  printf "  ${BOLD}Point the daemon at the agent runtime:${RESET}\n"
  printf "\n"
  printf "     ${CYAN}export GOOSAR_HERMES_PATH=%s${RESET}\n" "$agent_path"
  printf "\n"
}

print_path_hint() {
  local dir="$1"
  local rc
  rc="$(shell_rc_file)"

  printf "\n"
  printf "  ${BOLD}%s is not on your PATH.${RESET} Add it:\n" "$dir"
  printf "\n"
  printf "     ${CYAN}echo 'export PATH=\"%s:\$PATH\"' >> %s${RESET}\n" "$dir" "$rc"
  printf "     ${CYAN}source %s${RESET}\n" "$rc"
}

print_next_steps() {
  local dir="$1"

  printf "\n"
  if [ "$RUNNER" = "none" ]; then
    printf "  ${BOLD}Agent runner:${RESET} none installed.\n"
    printf "  Agents need a runner on the machine that executes tasks. Add the bundled\n"
    printf "  one later by re-running this installer with ${BOLD}--runner hermes${RESET}, or install\n"
    printf "  an agent CLI yourself; ${CYAN}goosar doctor${RESET} shows what is missing.\n"
    printf "\n"
  fi
  if [ -n "$SERVER_URL" ]; then
    printf "  ${BOLD}Next: connect the CLI to your Goosar server${RESET}\n"
    printf "\n"
    printf "     ${CYAN}goosar config set server_url %s${RESET}\n" "$SERVER_URL"
    printf "     ${CYAN}goosar config set app_url %s${RESET}\n" "$APP_URL"
    printf "     ${CYAN}goosar login --token <YOUR_TOKEN>${RESET}\n"
    printf "     ${CYAN}goosar daemon start${RESET}\n"
    printf "\n"
    printf "  Create a token in the Goosar web app under ${BOLD}Settings > API Tokens${RESET}.\n"
    printf "  Or run the guided equivalent in one step:\n"
    printf "\n"
    printf "     ${CYAN}goosar setup self-host --server-url %s --app-url %s${RESET}\n" "$SERVER_URL" "$APP_URL"
  else
    printf "  ${BOLD}Next: point the CLI at your Goosar server${RESET}\n"
    printf "\n"
    printf "     ${CYAN}goosar setup self-host --server-url <API_URL> --app-url %s${RESET}\n" "$APP_URL"
    printf "\n"
    printf "  Re-run this installer with ${BOLD}--server-url${RESET} to get the exact commands.\n"
  fi
  printf "\n"
  printf "  Binary: %s/goosar\n" "$dir"
  printf "\n"
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
main() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --app-url)
        [ $# -ge 2 ] || fail "Missing value for --app-url"
        APP_URL="$2"; shift ;;
      --server-url)
        [ $# -ge 2 ] || fail "Missing value for --server-url"
        SERVER_URL="$2"; shift ;;
      --install-dir)
        [ $# -ge 2 ] || fail "Missing value for --install-dir"
        INSTALL_DIR="$2"; shift ;;
      --version)
        # #566: the flag requested a versioned tarball no deployment ever
        # served (Dockerfile.web publishes one unversioned archive per
        # platform), so it always 404'd. Refuse it with the reason instead.
        fail "--version is not supported: a deployment serves exactly one CLI version,
the one matching its server. Run the installer without --version, or install
from a deployment of the version you need." ;;
      --skip-checksum)
        SKIP_CHECKSUM=1 ;;
      --runner)
        [ $# -ge 2 ] || fail "Missing value for --runner"
        RUNNER="$2"; shift ;;
      --with-agent)
        RUNNER="hermes" ;;
      --without-agent)
        RUNNER="none" ;;
      --help|-h)
        usage
        exit 0 ;;
      *)
        warn "Unknown option: $1" ;;
    esac
    shift
  done

  if [ -z "$APP_URL" ]; then
    fail "Missing --app-url.

This script is piped into bash, so it cannot detect the Goosar server it came
from. Pass the same host you downloaded it from:

  curl -fsSL https://goosar.example.com/install.sh | bash -s -- \\
    --app-url https://goosar.example.com \\
    --server-url https://goosar.example.com

You can also set GOOSAR_APP_URL and GOOSAR_SERVER_URL instead."
  fi

  APP_URL="$(normalize_url "$APP_URL")"
  has_http_scheme "$APP_URL" || fail "--app-url must be an http:// or https:// URL: $APP_URL"

  if [ -n "$SERVER_URL" ]; then
    SERVER_URL="$(normalize_url "$SERVER_URL")"
    has_http_scheme "$SERVER_URL" || fail "--server-url must be an http:// or https:// URL: $SERVER_URL"
  fi

  command_exists curl || fail "curl is required but was not found. Install curl and re-run."
  command_exists tar  || fail "tar is required but was not found. Install tar and re-run."

  local dir
  dir="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

  printf "\n"
  printf "${BOLD}  Goosar — CLI Installer${RESET}\n"
  printf "  Source: %s\n" "$APP_URL"
  printf "\n"

  detect_platform
  info "Platform: $OS/$ARCH"

  # Settled before any download so an interactive run answers once, up front.
  choose_runner

  download_cli
  install_cli "$dir"
  verify_cli "$dir/goosar"

  case "$RUNNER" in
    hermes) install_agent_runtime ;;
    none) : ;;
  esac

  if ! path_contains "$dir"; then
    print_path_hint "$dir"
  fi

  print_next_steps "$dir"
}

main "$@"
