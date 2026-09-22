#!/usr/bin/env bash
# Goosar installer — installs the CLI and optionally provisions a self-host server.
#
# The repository is private, so raw.githubusercontent.com 404s for everyone: run
# this script from a checkout.
#
# Install / upgrade CLI only:
#   bash scripts/install.sh
#
# Raise a Goosar deployment (backend): that is `make selfhost`
# (docker-compose.selfhost.yml), not this script. `--with-server` was removed
# in 0.11.0 and only prints the new command.
#   make selfhost
#
# CLI only, against a Goosar deployment that is already running: that deployment
# serves its own installer and binaries, and no checkout is needed —
#   curl -fsSL <app-url>/install.sh | bash -s -- --app-url <app-url> --server-url <api-url>
#
# After installation, run `goosar setup` to configure your environment.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/adanman/goosar.git"
REPO_WEB_URL="https://github.com/adanman/goosar"  # without .git, for GitHub web APIs
INSTALL_DIR="${GOOSAR_INSTALL_DIR:-$HOME/.goosar/server}"
BREW_PACKAGE="adanman/tap/goosar"

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
ok()    { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail()  { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

running_in_ssh_session() {
  [ -n "${SSH_CONNECTION:-}" ] || [ -n "${SSH_CLIENT:-}" ] || [ -n "${SSH_TTY:-}" ]
}

print_remote_server_token_hint() {
  if ! running_in_ssh_session; then
    return
  fi

  printf "  ${BOLD}Looks like a remote/SSH session.${RESET} Browser login may not be able to call back to this machine's localhost.\n"
  printf "  Token login is usually simpler here:\n"
  printf "     1. On your local computer, open ${CYAN}https://goosar.ru/settings?tab=tokens${RESET}\n"
  printf "        and create a token under ${BOLD}Settings > API Tokens${RESET}.\n"
  printf "     2. On this server, run:\n"
  printf "        ${CYAN}goosar login --token <YOUR_TOKEN>${RESET}\n"
  printf "        ${CYAN}goosar daemon start${RESET}\n"
  printf "\n"
}

env_file_value() {
  local file="$1"
  local key="$2"
  local default="$3"
  local line value
  line="$(grep -E "^${key}=" "$file" 2>/dev/null | tail -n 1 || true)"
  if [ -z "$line" ]; then
    printf "%s" "$default"
    return
  fi
  value="${line#*=}"
  value="${value%$'\r'}"
  value="${value%\"}"
  value="${value#\"}"
  value="${value%\'}"
  value="${value#\'}"
  if [ -z "$value" ]; then
    printf "%s" "$default"
  else
    printf "%s" "$value"
  fi
}

# ALLOW_SIGNUP=false with no allowlist is a stand nobody can sign into — the
# operator included, since the login flow is the only account-creation path.
# `--profile perimeter` writes exactly that, and the preset's own echo scrolls
# past behind `docker pull` output, so the check is repeated in the final
# summary where the operator actually looks.
warn_signup_closed_without_allowlist() {
  local file="${1:-.env}"
  [ -f "$file" ] || return 0
  [ "$(env_file_value "$file" ALLOW_SIGNUP "true")" = "false" ] || return 0
  [ -z "$(env_file_value "$file" ALLOWED_EMAIL_DOMAINS "")" ] || return 0
  [ -z "$(env_file_value "$file" ALLOWED_EMAILS "")" ] || return 0
  warn "Registration is CLOSED (ALLOW_SIGNUP=false) and no allowlist is set: nobody can sign in yet."
  printf "     Set ${CYAN}ALLOWED_EMAIL_DOMAINS${RESET} (or ${CYAN}ALLOWED_EMAILS${RESET}) in %s and restart the backend:\n" "$file" >&2
  printf "        ${CYAN}cd %s && docker compose -f docker-compose.selfhost.yml up -d${RESET}\n" "$INSTALL_DIR" >&2
  printf "     GOOSAR_DEPLOYMENT_ADMIN_EMAILS does NOT substitute: it grants the admin role to\n" >&2
  printf "     an account that already exists, it never creates one.\n" >&2
}

selfhost_backend_port() {
  local file="${1:-.env}"
  local value
  for key in BACKEND_PORT API_PORT SERVER_PORT PORT; do
    value="$(env_file_value "$file" "$key" "")"
    if [ -n "$value" ]; then
      printf "%s" "$value"
      return
    fi
  done
  printf "8081"
}

selfhost_frontend_port() {
  env_file_value "${1:-.env}" "FRONTEND_PORT" "3001"
}

# Optional --with-server .env overrides (#295): set at install time so the
# operator does not have to hand-edit .env and restart afterwards.
OPT_RESEND_KEY=""
OPT_LLM_KEY=""
OPT_LLM_BASE_URL=""
OPT_LLM_MODEL=""
# SMTP relay (#429): the self-hosted alternative to Resend. Without one of the
# two the server has no way to deliver login codes at all.
OPT_SMTP_HOST=""
OPT_SMTP_PORT=""
OPT_SMTP_USER=""
OPT_SMTP_PASSWORD=""
OPT_SMTP_FROM=""
OPT_SMTP_TLS=""
# Profile (#428, ADR-0015 #530). Empty means "let scripts/selfhost-env.sh
# decide", and its self-host default is `perimeter` — self-hosting IS the
# on-prem product. The four ADR-0015 values (perimeter, demo, dev, local) name
# the deployment TYPE and apply that profile's variable set; the legacy
# `cloud` value only opts back into the managed-cloud egress defaults
# (GOOSAR_DELIVERY_PROFILE).
OPT_PROFILE=""
# --upgrade [--tag vX.Y.Z]
OPT_TAG=""

# set_env_kv <file> <key> <value>: replace the KEY= line or append one.
set_env_kv() {
  local file="$1" key="$2" value="$3" esc
  # Escape sed-replacement specials (`\`, `&`, and the `#` delimiter) so an
  # API key or URL containing them lands in .env verbatim instead of breaking
  # or corrupting the sed under `set -e`.
  esc="$(printf '%s' "$value" | sed -e 's/[\\&#]/\\&/g')"
  if grep -q "^${key}=" "$file"; then
    if [ "$(uname -s)" = "Darwin" ]; then
      sed -i '' "s#^${key}=.*#${key}=${esc}#" "$file"
    else
      sed -i "s#^${key}=.*#${key}=${esc}#" "$file"
    fi
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

# Write the --resend-key / --smtp-* / --llm-* values into .env.
# Runs before the compose stack starts, so no extra restart is needed.
apply_env_overrides() {
  local file="$1"
  [ -n "$OPT_RESEND_KEY" ] && set_env_kv "$file" RESEND_API_KEY "$OPT_RESEND_KEY"
  [ -n "$OPT_SMTP_HOST" ] && set_env_kv "$file" SMTP_HOST "$OPT_SMTP_HOST"
  [ -n "$OPT_SMTP_PORT" ] && set_env_kv "$file" SMTP_PORT "$OPT_SMTP_PORT"
  [ -n "$OPT_SMTP_USER" ] && set_env_kv "$file" SMTP_USERNAME "$OPT_SMTP_USER"
  [ -n "$OPT_SMTP_PASSWORD" ] && set_env_kv "$file" SMTP_PASSWORD "$OPT_SMTP_PASSWORD"
  [ -n "$OPT_SMTP_FROM" ] && set_env_kv "$file" SMTP_FROM_EMAIL "$OPT_SMTP_FROM"
  [ -n "$OPT_SMTP_TLS" ] && set_env_kv "$file" SMTP_TLS "$OPT_SMTP_TLS"
  [ -n "$OPT_LLM_KEY" ] && set_env_kv "$file" GOOSAR_LLM_API_KEY "$OPT_LLM_KEY"
  [ -n "$OPT_LLM_BASE_URL" ] && set_env_kv "$file" GOOSAR_LLM_BASE_URL "$OPT_LLM_BASE_URL"
  if [ -n "$OPT_LLM_KEY" ] && [ -z "$OPT_LLM_BASE_URL" ] &&
     [ -z "$(env_file_value "$file" GOOSAR_LLM_BASE_URL "")" ]; then
    warn "--llm-key set without --llm-base-url: the server requires an explicit GOOSAR_LLM_BASE_URL for the LLM layer to activate."
  fi
  # The server refuses to start with SMTP_HOST and no sender address (#429).
  if [ -n "$OPT_SMTP_HOST" ] && [ -z "$OPT_SMTP_FROM" ] &&
     [ -z "$(env_file_value "$file" SMTP_FROM_EMAIL "")" ]; then
    warn "--smtp-host set without --smtp-from: the server refuses to start without a sender address (SMTP_FROM_EMAIL)."
  fi
  if [ -n "$OPT_RESEND_KEY$OPT_LLM_KEY$OPT_LLM_BASE_URL$OPT_SMTP_HOST$OPT_SMTP_PORT$OPT_SMTP_USER$OPT_SMTP_PASSWORD$OPT_SMTP_FROM$OPT_SMTP_TLS" ]; then
    ok "Applied installer flags to $file"
  fi
}

detect_os() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
            fail "This script does not support Windows. Use the PowerShell installer from a checkout instead:
  powershell -ExecutionPolicy Bypass -File scripts\\install.ps1" ;;
    *)      fail "Unsupported operating system: $(uname -s). Goosar supports macOS, Linux, and Windows." ;;
  esac

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       fail "Unsupported architecture: $ARCH" ;;
  esac
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
_dump_brew_log() {
  local log="$1"
  if [ -s "$log" ]; then
    warn "Homebrew output (last 80 lines):"
    tail -n 80 "$log" | sed 's/^/  /' >&2
  fi
}

install_cli_brew() {
  info "Installing Goosar CLI via Homebrew..."
  local brew_log
  brew_log=$(mktemp)
  if ! brew tap adanman/tap >"$brew_log" 2>&1; then
    warn "Failed to add Homebrew tap. Falling back to GitHub Releases binary install."
    _dump_brew_log "$brew_log"
    rm -f "$brew_log"
    return 1
  fi
  # brew install exits non-zero if already installed on older Homebrew versions
  if ! brew install "$BREW_PACKAGE" >"$brew_log" 2>&1; then
    if brew list "$BREW_PACKAGE" >/dev/null 2>&1; then
      rm -f "$brew_log"
      ok "Goosar CLI already installed via Homebrew"
    else
      warn "Failed to install goosar via Homebrew. Falling back to GitHub Releases binary install."
      _dump_brew_log "$brew_log"
      rm -f "$brew_log"
      return 1
    fi
  else
    rm -f "$brew_log"
    ok "Goosar CLI installed via Homebrew"
  fi
}

# Fail-closed verification of a release asset, the same contract the served
# installer (apps/web/public/install.sh) and the CLI self-updater already
# apply: a missing checksums.txt, a missing entry for this asset, a missing
# sha256 utility and an actual mismatch ALL abort. This script installs into
# /usr/local/bin, with sudo when needed, so an unverified archive here is an
# unverified binary running as root. GOOSAR_SKIP_CHECKSUM=1 is the one
# explicit escape hatch.
verify_release_checksum() {
  local tmp_dir="$1" tag="$2" name="$3" archive="$4"
  local url="https://github.com/adanman/goosar/releases/download/${tag}/checksums.txt"
  local hint="If you understand the risk, re-run with GOOSAR_SKIP_CHECKSUM=1."

  if [ -n "${GOOSAR_SKIP_CHECKSUM:-}" ]; then
    warn "Checksum verification skipped (GOOSAR_SKIP_CHECKSUM). The downloaded archive is NOT verified."
    return 0
  fi

  if ! curl -fsSL "$url" -o "$tmp_dir/checksums.txt" 2>/dev/null; then
    rm -rf "$tmp_dir"
    fail "Could not download checksums.txt from $url — aborting: the download cannot be verified. $hint"
  fi
  local expected
  expected="$(awk -v n="$name" '{ f=$2; sub(/^\*/, "", f); sub(/^.*\//, "", f); if (f == n) { print $1; exit } }' "$tmp_dir/checksums.txt")"
  if [ -z "$expected" ]; then
    rm -rf "$tmp_dir"
    fail "checksums.txt has no entry for $name — aborting: the download cannot be verified. $hint"
  fi
  local actual=""
  if command_exists shasum; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  elif command_exists sha256sum; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  fi
  if [ -z "$actual" ]; then
    rm -rf "$tmp_dir"
    fail "Neither shasum nor sha256sum is available — aborting: the download cannot be verified. $hint"
  fi
  if [ "$expected" != "$actual" ]; then
    rm -rf "$tmp_dir"
    fail "Checksum mismatch for $name (expected $expected, got $actual)."
  fi
  info "Checksum verified"
}

install_cli_binary() {
  info "Installing Goosar CLI from GitHub Releases..."

  # Get latest release tag
  local latest
  latest=$(curl -sI "$REPO_WEB_URL/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n' || true)
  if [ -z "$latest" ]; then
    fail "Could not determine latest release. Check your network connection."
  fi

  local version="${latest#v}"
  local url="https://github.com/adanman/goosar/releases/download/${latest}/goosar-cli-${version}-${OS}-${ARCH}.tar.gz"
  local tmp_dir
  tmp_dir=$(mktemp -d)

  info "Downloading $url ..."
  if ! curl -fsSL "$url" -o "$tmp_dir/goosar.tar.gz"; then
    rm -rf "$tmp_dir"
    fail "Failed to download CLI binary."
  fi

  verify_release_checksum "$tmp_dir" "$latest" "goosar-cli-${version}-${OS}-${ARCH}.tar.gz" "$tmp_dir/goosar.tar.gz"

  tar -xzf "$tmp_dir/goosar.tar.gz" -C "$tmp_dir" goosar

  # Try /usr/local/bin first, fall back to ~/.local/bin. Tests and scripted
  # installs can override the first choice with GOOSAR_BIN_DIR.
  local bin_dir="${GOOSAR_BIN_DIR:-/usr/local/bin}"
  if [ -w "$bin_dir" ]; then
    mv "$tmp_dir/goosar" "$bin_dir/goosar"
  elif command_exists sudo; then
    sudo mv "$tmp_dir/goosar" "$bin_dir/goosar"
  else
    bin_dir="$HOME/.local/bin"
    mkdir -p "$bin_dir"
    mv "$tmp_dir/goosar" "$bin_dir/goosar"
    chmod +x "$bin_dir/goosar"
    # Add to PATH if not already there
    if ! echo "$PATH" | tr ':' '\n' | grep -q "^$bin_dir$"; then
      export PATH="$bin_dir:$PATH"
      add_to_path "$bin_dir"
    fi
  fi

  rm -rf "$tmp_dir"
  ok "Goosar CLI installed to $bin_dir/goosar"
}

add_to_path() {
  local dir="$1"
  local line="export PATH=\"$dir:\$PATH\""
  for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
    if [ -f "$rc" ] && ! grep -qF "$dir" "$rc"; then
      printf '\n# Added by Goosar installer\n%s\n' "$line" >> "$rc"
    fi
  done
}

get_latest_version() {
  # grep exits 1 when no match; use `|| true` to avoid triggering pipefail
  curl -sI "$REPO_WEB_URL/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n' || true
}

get_selfhost_ref() {
  if [ -n "${GOOSAR_SELFHOST_REF:-}" ]; then
    printf '%s' "$GOOSAR_SELFHOST_REF"
    return
  fi

  local latest
  latest=$(get_latest_version)
  if [ -n "$latest" ]; then
    printf '%s' "$latest"
    return
  fi

  printf '%s' "main"
}

checkout_server_ref() {
  local ref="$1"

  if [ "$ref" = "main" ]; then
    git fetch origin main --depth 1 2>/dev/null || true
    git checkout --force main 2>/dev/null || true
    git reset --hard origin/main 2>/dev/null || true
    return
  fi

  git fetch origin --tags --force 2>/dev/null || true
  if git rev-parse --verify --quiet "refs/tags/$ref" >/dev/null; then
    git checkout --force "$ref" 2>/dev/null || git checkout --force "tags/$ref" 2>/dev/null || true
    return
  fi

  git fetch origin "$ref" --depth 1 2>/dev/null || true
  git checkout --force "$ref" 2>/dev/null || true
}

pull_official_selfhost_images() {
  if docker compose -f docker-compose.selfhost.yml pull; then
    return
  fi

  echo ""
  warn "Official images for the selected self-host channel are not published yet."
  echo "This can happen before the first GHCR release is available."
  echo "From $INSTALL_DIR, build from source instead:"
  echo "  docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build"
  exit 1
}

upgrade_cli_brew() {
  info "Upgrading Goosar CLI via Homebrew..."
  brew update 2>/dev/null || true
  if brew upgrade "$BREW_PACKAGE" 2>/dev/null; then
    ok "Goosar CLI upgraded via Homebrew"
  else
    # brew upgrade exits non-zero if already up to date
    ok "Goosar CLI is already the latest version"
  fi
}

install_cli() {
  if command_exists goosar; then
    local current_ver
    # `goosar version` outputs "goosar 0.3.23 (commit: f46b929eb, built: 2026-06-16T10:11:56Z)" — extract just the version
    current_ver=$(goosar version 2>/dev/null | awk 'NR==1{print $2}' || echo "unknown")

    local latest_ver
    latest_ver=$(get_latest_version)

    # Normalize: strip leading 'v' for comparison
    local current_cmp="${current_ver#v}"
    local latest_cmp="${latest_ver#v}"

    if [ -z "$latest_ver" ] || [ "$current_cmp" = "$latest_cmp" ]; then
      ok "Goosar CLI is up to date ($current_ver)"
      return 0
    fi

    info "Goosar CLI $current_ver installed, latest is $latest_ver — upgrading..."
    if command_exists brew && brew list "$BREW_PACKAGE" >/dev/null 2>&1; then
      upgrade_cli_brew
    else
      install_cli_binary
    fi

    local new_ver
    new_ver=$(goosar version 2>/dev/null | awk 'NR==1{print $2}' || echo "unknown")
    ok "Goosar CLI upgraded ($current_ver → $new_ver)"
    return 0
  fi

  if command_exists brew; then
    install_cli_brew || install_cli_binary
  else
    install_cli_binary
  fi

  # Verify
  if ! command_exists goosar; then
    fail "CLI installed but 'goosar' not found on PATH. You may need to restart your shell."
  fi
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
check_docker() {
  if ! command_exists docker; then
    printf "\n"
    fail "Docker is not installed. Goosar self-hosting requires Docker and Docker Compose.

Install Docker:
  macOS:  https://docs.docker.com/desktop/install/mac-install/
  Linux:  https://docs.docker.com/engine/install/

After installing Docker, raise the deployment with: make selfhost"
  fi

  if ! docker info >/dev/null 2>&1; then
    fail "Docker is installed but not running. Please start Docker and re-run this script."
  fi

  ok "Docker is available"
}

# ---------------------------------------------------------------------------
# .env bootstrap — delegated to the single source of truth (#428)
# ---------------------------------------------------------------------------
# scripts/selfhost-env.sh is what `make selfhost` runs. The installer used to
# carry a half-copy of it that generated only JWT_SECRET and
# POSTGRES_PASSWORD, so the recommended install path shipped stands without a
# VCS key (integrations 503), without an MCP key (integration credentials
# stored as plaintext in the DB and every backup), on :latest images, and
# without PUBLIC_HOST derivation. Runs with the CWD already at the checkout.
bootstrap_env() {
  local server_ref="$1"
  local had_env="no"
  [ -f .env ] && had_env="yes"

  # Pin GOOSAR_IMAGE_TAG to the version this installer actually installs.
  # selfhost-env.sh honors an explicit GOOSAR_IMAGE_TAG, falling back to the
  # newest release tag in the clone; a non-release ref (main, a branch) has no
  # tag to pin, so leave the derivation to the script.
  local pin=""
  case "$server_ref" in
    v[0-9]*) pin="$server_ref" ;;
  esac

  # One flag, two variables: `cloud` is the egress switch (#428), the four
  # ADR-0015 names are the deployment type (#530). A deployment profile leaves
  # GOOSAR_DELIVERY_PROFILE at its self-host default (perimeter) — no stand
  # type opts back into cloud egress.
  #
  # With no --profile, an exported GOOSAR_DEPLOYMENT_PROFILE still applies:
  # `GOOSAR_DEPLOYMENT_PROFILE=demo make selfhost` is a documented path, and
  # passing an unconditional empty value here would silently discard it — the
  # PowerShell installer falls back to the same variable.
  local delivery="" deployment="${GOOSAR_DEPLOYMENT_PROFILE:-}"
  case "$OPT_PROFILE" in
    cloud) delivery="cloud" ;;
    perimeter|demo|dev|local) deployment="$OPT_PROFILE" ;;
  esac

  GOOSAR_IMAGE_TAG="${GOOSAR_IMAGE_TAG:-$pin}" \
  GOOSAR_DELIVERY_PROFILE="$delivery" \
  GOOSAR_DEPLOYMENT_PROFILE="$deployment" \
    bash scripts/selfhost-env.sh

  if [ "$had_env" = "yes" ]; then
    ok "Using existing .env (secrets and profile left untouched)"
    # selfhost-env.sh only writes the profile when it creates .env, so on a
    # re-run --profile is a silent no-op. Say so: an operator who passes
    # `--profile cloud` expecting an egress change must not walk away
    # believing the deployment switched.
    if [ -n "$OPT_PROFILE" ]; then
      warn "--profile $OPT_PROFILE ignored: .env already exists. Edit GOOSAR_DEPLOYMENT_PROFILE (or GOOSAR_DELIVERY_PROFILE for cloud) in $INSTALL_DIR/.env and restart the backend."
    fi
  fi
}

# ---------------------------------------------------------------------------
# Main: Default mode (install / upgrade CLI only)
# ---------------------------------------------------------------------------
run_default() {
  printf "\n"
  printf "${BOLD}  Goosar — Installer${RESET}\n"
  printf "\n"

  detect_os
  install_cli

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Goosar CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Next: configure your environment${RESET}\n"
  printf "\n"
  printf "     ${CYAN}goosar setup${RESET}                # Connect to Goosar Cloud (goosar.ru)\n"
  printf "     ${CYAN}goosar setup self-host${RESET}       # Connect to a self-hosted server\n"
  printf "\n"
  print_remote_server_token_hint
  printf "  ${BOLD}Self-hosting?${RESET} Raise the deployment from a repository checkout:\n"
  printf "     make selfhost\n"
  printf "\n"
}

# ---------------------------------------------------------------------------
# --with-server: removed in 0.11.0 (R-18g, ADR-0016)
# ---------------------------------------------------------------------------
# Deploying a backend is not this script's job: install.sh is the client
# installer served from /install.sh. 0.10.1 kept --with-server for one release
# as a wrapper that handed over to a separate wizard; that release is over. The
# flag is still recognised so an old runbook gets the new command instead of
# "unknown option".
run_with_server() {
  printf "${BOLD}${RED}✗ install.sh --with-server was removed in 0.11.0.${RESET}\n" >&2
  printf "     A deployment is raised from a checkout:\n" >&2
  printf "        ${CYAN}make selfhost${RESET}\n" >&2
  printf "     It creates .env with generated secrets; the old --resend-key / --smtp-* /\n" >&2
  printf "     --llm-* flags are variables in that .env (see SELF_HOSTING.md,\n" >&2
  printf "     «Deployment flows»). Without a checkout:\n" >&2
  printf "        git clone %s && cd goosar && make selfhost\n" "$REPO_URL" >&2
  exit 2
}

# ---------------------------------------------------------------------------
# Upgrade: move an existing --with-server installation to another image tag
# ---------------------------------------------------------------------------
# The compose stack runs whatever GOOSAR_IMAGE_TAG .env pins, so an upgrade
# is: repoint the pin, pull, `up -d`. Migrations run on backend startup and
# are NOT reversible, which is why the rollback recipe below pairs the old tag
# with a restore of the pre-upgrade database dump.
run_upgrade() {
  printf "\n"
  printf "${BOLD}  Goosar — Self-Host Upgrade${RESET}\n"
  printf "\n"

  [ -d "$INSTALL_DIR/.git" ] || fail "No Goosar installation found at $INSTALL_DIR. Install one first: bash scripts/install.sh --with-server"
  check_docker
  cd "$INSTALL_DIR"
  [ -f .env ] || fail "$INSTALL_DIR/.env is missing — this installation was not provisioned by --with-server."

  local previous
  previous="$(env_file_value .env GOOSAR_IMAGE_TAG "latest")"

  info "Fetching release tags..."
  git fetch origin --tags --force 2>/dev/null || warn "Could not fetch tags — falling back to the tags already in the clone."

  local target="$OPT_TAG"
  if [ -z "$target" ]; then
    target="$(git tag --list 'v[0-9]*' --sort=-v:refname | head -n1)"
    [ -n "$target" ] || fail "No release tags found in $INSTALL_DIR. Pass an explicit tag: --upgrade --tag vX.Y.Z"
  else
    # checkout_server_ref swallows a failed checkout, so a typo'd --tag would
    # leave the working tree on the old release while .env gets pinned to a
    # tag that does not exist — the stand then breaks on the operator's next
    # `docker compose up -d`, not here. Refuse before anything is written.
    git rev-parse --verify --quiet "refs/tags/$target" >/dev/null ||
      fail "No such release tag: $target. Available: $(git tag --list 'v[0-9]*' --sort=-v:refname | head -n5 | tr '\n' ' ')"
  fi

  if [ "$target" = "$previous" ]; then
    ok "Already on $previous — nothing to upgrade."
    return
  fi

  info "Upgrading $previous → $target"
  # Take the compose/self-host assets of the target release too: compose file,
  # migrations and scripts must match the images being started.
  checkout_server_ref "$target"
  set_env_kv .env GOOSAR_IMAGE_TAG "$target"

  info "Pulling images for $target..."
  pull_official_selfhost_images
  info "Starting the upgraded stack..."
  docker compose -f docker-compose.selfhost.yml up -d

  printf "\n"
  ok "Upgraded to $target (was $previous)"
  printf "\n"
  printf "  ${BOLD}Back up first, always:${RESET} ${CYAN}bash scripts/backup.sh${RESET} — migrations run on backend\n"
  printf "  startup and are one-way.\n"
  printf "\n"
  printf "  ${BOLD}Rollback recipe${RESET} (from %s):\n" "$INSTALL_DIR"
  printf "     ${CYAN}git checkout --force %s${RESET}\n" "$previous"
  printf "     ${CYAN}sed -i.bak 's/^GOOSAR_IMAGE_TAG=.*/GOOSAR_IMAGE_TAG=%s/' .env${RESET}\n" "$previous"
  printf "     ${CYAN}docker compose -f docker-compose.selfhost.yml pull && docker compose -f docker-compose.selfhost.yml up -d${RESET}\n"
  printf "\n"
  printf "  ${BOLD}⚠ Images roll back; the database does not.${RESET} If %s applied a\n" "$target"
  printf "  migration, the older backend will refuse to start against the newer schema.\n"
  printf "  In that case restore the pre-upgrade dump as well:\n"
  printf "     ${CYAN}bash scripts/restore.sh <backup-taken-before-the-upgrade>${RESET}\n"
  printf "  Everything written since the upgrade is lost with it — that is the price of\n"
  printf "  a rollback across a migration.\n"
  printf "\n"
  # An upgrade is the other moment an operator looks at this output, and a stand
  # nobody can sign into stays that way across upgrades unless it is said again.
  warn_signup_closed_without_allowlist "$INSTALL_DIR/.env"
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
run_stop() {
  printf "\n"
  info "Stopping Goosar services..."

  if [ -d "$INSTALL_DIR" ]; then
    cd "$INSTALL_DIR"
    if [ -f docker-compose.selfhost.yml ]; then
      docker compose -f docker-compose.selfhost.yml down
      ok "Docker services stopped"
    else
      warn "No docker-compose.selfhost.yml found at $INSTALL_DIR"
    fi
  else
    warn "No Goosar installation found at $INSTALL_DIR"
  fi

  if command_exists goosar; then
    goosar daemon stop 2>/dev/null && ok "Daemon stopped" || true
  fi

  printf "\n"
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
main() {
  local mode="default"

  while [ $# -gt 0 ]; do
    case "$1" in
      --with-server) mode="with-server" ;;
      --local)       mode="with-server" ;;  # backwards compat alias
      --stop)        mode="stop" ;;
      --upgrade)     mode="upgrade" ;;
      --tag)
        [ $# -ge 2 ] || fail "--tag requires a value"
        OPT_TAG="$2"; shift ;;
      --profile)
        [ $# -ge 2 ] || fail "--profile requires a value"
        case "$2" in
          perimeter|demo|dev|local|cloud) OPT_PROFILE="$2" ;;
          *) fail "--profile must be perimeter, demo, dev, local or cloud (got '$2')" ;;
        esac
        shift ;;
      --resend-key)
        [ $# -ge 2 ] || fail "--resend-key requires a value"
        OPT_RESEND_KEY="$2"; shift ;;
      --smtp-host)
        [ $# -ge 2 ] || fail "--smtp-host requires a value"
        OPT_SMTP_HOST="$2"; shift ;;
      --smtp-port)
        [ $# -ge 2 ] || fail "--smtp-port requires a value"
        OPT_SMTP_PORT="$2"; shift ;;
      --smtp-user)
        [ $# -ge 2 ] || fail "--smtp-user requires a value"
        OPT_SMTP_USER="$2"; shift ;;
      --smtp-password)
        [ $# -ge 2 ] || fail "--smtp-password requires a value"
        OPT_SMTP_PASSWORD="$2"; shift ;;
      --smtp-from)
        [ $# -ge 2 ] || fail "--smtp-from requires a value"
        OPT_SMTP_FROM="$2"; shift ;;
      --smtp-tls)
        [ $# -ge 2 ] || fail "--smtp-tls requires a value"
        OPT_SMTP_TLS="$2"; shift ;;
      --llm-key)
        [ $# -ge 2 ] || fail "--llm-key requires a value"
        OPT_LLM_KEY="$2"; shift ;;
      --llm-base-url)
        [ $# -ge 2 ] || fail "--llm-base-url requires a value"
        OPT_LLM_BASE_URL="$2"; shift ;;
      --llm-model)
        [ $# -ge 2 ] || fail "--llm-model requires a value"
        OPT_LLM_MODEL="$2"; shift ;;
      --help|-h)
        echo "Usage: install.sh [--with-server | --upgrade | --stop] [options]"
        echo ""
        echo "  (default)       Install / upgrade the Goosar CLI"
        echo "  --with-server   REMOVED in 0.11.0: prints the new command and exits 2."
        echo "                  A deployment is raised by: make selfhost"
        echo "  --upgrade       Move an existing self-host installation to a newer"
        echo "                  image tag (newest release tag unless --tag is given)"
        echo "  --stop          Stop a self-hosted installation"
        echo ""
        echo "Options for --upgrade:"
        echo "  --tag <vX.Y.Z>          Target release tag (default: newest in the clone)"
        echo ""
        echo "DEPRECATED server options (removed in 0.11.0). They used to be written"
        echo "into the server .env by --with-server; they are variables in that .env"
        echo "now and are accepted here only so an old runbook is told where they went:"
        echo "  make selfhost        (PROFILE=<profile> selects the deployment profile)"
        echo "Nothing secret belongs on a command line:"
        echo "  --profile <profile>     Deployment profile (ADR-0015), written as"
        echo "                          GOOSAR_DEPLOYMENT_PROFILE together with the"
        echo "                          rest of that profile's variable set:"
        echo "                            perimeter  customer network (default): signup"
        echo "                                       closed, external images blocked"
        echo "                            demo       preview stand: signup open"
        echo "                            dev        development stand"
        echo "                            local      developer machine"
        echo "                          Legacy value: cloud — sets only"
        echo "                          GOOSAR_DELIVERY_PROFILE=cloud (managed-cloud"
        echo "                          egress). See SELF_HOSTING.md →"
        echo "                          «Профили развёртывания» and → «Профиль доставки и исходящий трафик»."
        echo "  --resend-key <key>      RESEND_API_KEY — email login codes instead of logs"
        echo "  --smtp-host <host>      SMTP_HOST — own relay instead of Resend"
        echo "  --smtp-port <port>      SMTP_PORT (default 25)"
        echo "  --smtp-user <user>      SMTP_USERNAME (omit for an unauthenticated relay)"
        echo "  --smtp-password <pass>  SMTP_PASSWORD"
        echo "  --smtp-from <address>   SMTP_FROM_EMAIL — required with --smtp-host"
        echo "  --smtp-tls <mode>       SMTP_TLS: starttls (default) or implicit"
        echo "  --llm-key <key>         GOOSAR_LLM_API_KEY — enable the server LLM layer"
        echo "  --llm-base-url <url>    GOOSAR_LLM_BASE_URL — required with --llm-key"
        echo "  --llm-model <model>     GOOSAR_LLM_DEFAULT_MODEL"
        echo ""
        echo "Environment variables:"
        echo "  GOOSAR_INSTALL_DIR   Self-host server install directory"
        echo "                        (default: \$HOME/.goosar/server)"
        echo "  GOOSAR_BIN_DIR       Target directory for the CLI binary when"
        echo "                        installing from GitHub Releases"
        echo "                        (default: /usr/local/bin, then \$HOME/.local/bin)"
        echo "  GOOSAR_SELFHOST_REF  Git ref to check out for self-host assets"
        echo "                        (default: latest release tag, falling back to main)"
        echo ""
        echo "After installation, run 'goosar setup' to configure your environment."
        exit 0
        ;;
      *) warn "Unknown option: $1" ;;
    esac
    shift
  done

  case "$mode" in
    default)     run_default ;;
    with-server) run_with_server ;;
    upgrade)     run_upgrade ;;
    stop)        run_stop ;;
  esac
}

# Allow sourcing for tests (install.test.sh exercises helpers directly).
if [ "${BASH_SOURCE[0]:-}" = "${0}" ]; then
  main "$@"
fi
