#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Build a self-contained sandbox with stub `curl` and a tarball that the
# release-binary fallback path will download. Each test supplies its own
# `brew` stub to model a specific Homebrew failure mode.
_setup_sandbox() {
  local tmp="$1"
  local stub_bin="$tmp/stub-bin"
  local install_bin="$tmp/install-bin"
  local payload_dir="$tmp/payload"
  mkdir -p "$stub_bin" "$install_bin" "$payload_dir"

  cat >"$payload_dir/goosar" <<'STUB'
#!/usr/bin/env bash
echo "goosar v0.3.2 (commit: test)"
STUB
  chmod +x "$payload_dir/goosar"
  tar -czf "$tmp/goosar.tar.gz" -C "$payload_dir" goosar

  cat >"$stub_bin/curl" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == *"-sI"* ]]; then
  printf 'HTTP/2 302\r\nlocation: https://github.com/adanman/goosar/releases/tag/v0.3.2\r\n'
  exit 0
fi

out=""
url=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done

if [[ -z "$out" ]]; then
  echo "stub curl expected -o" >&2
  exit 2
fi

# checksums.txt is served for the asset the installer asked for a moment ago,
# so the stub does not have to know the host's OS/ARCH naming.
if [[ "$url" == *checksums.txt ]]; then
  if [[ "${GOOSAR_TEST_NO_CHECKSUMS:-}" == "1" ]]; then
    exit 22
  fi
  asset="$(cat "$GOOSAR_TEST_STATE/last-asset" 2>/dev/null || true)"
  sum="$(shasum -a 256 "$GOOSAR_TEST_ARCHIVE" | awk '{print $1}')"
  printf '%s  %s\n' "$sum" "$asset" >"$out"
  exit 0
fi

basename "$url" >"$GOOSAR_TEST_STATE/last-asset"
cp "$GOOSAR_TEST_ARCHIVE" "$out"
STUB
  chmod +x "$stub_bin/curl"
}

_run_installer() {
  local tmp="$1"
  local out="$tmp/install.out"
  local err="$tmp/install.err"
  if ! PATH="$tmp/stub-bin:$tmp/install-bin:/usr/bin:/bin" \
    GOOSAR_BIN_DIR="$tmp/install-bin" \
    GOOSAR_TEST_ARCHIVE="$tmp/goosar.tar.gz" \
    GOOSAR_TEST_STATE="$tmp" \
    bash "$ROOT_DIR/scripts/install.sh" >"$out" 2>"$err"; then
    echo "install.sh exited non-zero" >&2
    cat "$out" >&2 || true
    cat "$err" >&2 || true
    return 1
  fi

  if [[ ! -x "$tmp/install-bin/goosar" ]]; then
    echo "expected fallback binary at $tmp/install-bin/goosar" >&2
    cat "$out" >&2 || true
    cat "$err" >&2 || true
    return 1
  fi

  if ! grep -q "Homebrew output (last 80 lines):" "$err"; then
    echo "expected diagnostic tail in stderr" >&2
    cat "$err" >&2 || true
    return 1
  fi
}

test_brew_install_failure_falls_back_to_release_binary() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    exit 0
    ;;
  install)
    echo "simulated brew install failure" >&2
    exit 42
    ;;
  list)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  _run_installer "$tmp"
}

# An attacker who can substitute the archive can equally make checksums.txt
# 404, so "could not verify" must abort rather than install. This script drops
# the binary into /usr/local/bin, with sudo when needed.
test_missing_checksums_aborts_the_install() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap) exit 0 ;;
  install) echo "simulated brew install failure" >&2; exit 42 ;;
  list) exit 1 ;;
  *) exit 0 ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  if (
    export GOOSAR_TEST_NO_CHECKSUMS=1
    _run_installer "$tmp" 2>/dev/null
  ); then
    echo "installer succeeded without a verifiable checksum" >&2
    return 1
  fi
  if [[ -e "$tmp/install-bin/goosar" ]]; then
    echo "installer placed an unverified binary" >&2
    return 1
  fi
}

test_brew_tap_failure_falls_back_to_release_binary() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    echo "simulated brew tap failure" >&2
    exit 17
    ;;
  *)
    echo "brew $* should not be reached after tap failure" >&2
    exit 99
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  _run_installer "$tmp"
}

test_remote_ssh_install_prints_token_login_hint() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    exit 0
    ;;
  install)
    echo "simulated brew install failure" >&2
    exit 42
    ;;
  list)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  (
    export SSH_CONNECTION="192.0.2.10 54321 198.51.100.20 22"
    _run_installer "$tmp"
  )

  if ! grep -q "Looks like a remote/SSH session" "$tmp/install.out"; then
    echo "expected remote/SSH token-login hint in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if ! grep -q "https://goosar.ru/settings?tab=tokens" "$tmp/install.out"; then
    echo "expected direct API Tokens settings URL in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if ! grep -q "Settings > API Tokens" "$tmp/install.out"; then
    echo "expected API Tokens tab name in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if ! grep -q "goosar login --token <YOUR_TOKEN>" "$tmp/install.out"; then
    echo "expected token login command in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if grep -q "goosar config set server_url" "$tmp/install.out"; then
    echo "did not expect default cloud server config command in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if grep -q "goosar config set app_url" "$tmp/install.out"; then
    echo "did not expect default cloud app config command in installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
}

test_local_install_does_not_print_token_login_hint() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    exit 0
    ;;
  install)
    echo "simulated brew install failure" >&2
    exit 42
    ;;
  list)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  (
    unset SSH_CONNECTION SSH_CLIENT SSH_TTY
    _run_installer "$tmp"
  )

  if grep -q "Looks like a remote/SSH session" "$tmp/install.out"; then
    echo "did not expect remote/SSH token-login hint in local installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
  if grep -q "goosar login --token <YOUR_TOKEN>" "$tmp/install.out"; then
    echo "did not expect token login command in local installer output" >&2
    cat "$tmp/install.out" >&2 || true
    return 1
  fi
}

test_env_override_flags_write_env_entries() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'RESEND_API_KEY=\nGOOSAR_LLM_API_KEY=\nGOOSAR_LLM_BASE_URL=\n' >"$tmp/.env"

  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_RESEND_KEY="re_test123"
    OPT_LLM_KEY="sk-test"
    OPT_LLM_BASE_URL="https://llm.example.com/v1"
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>&1

  grep -q '^RESEND_API_KEY=re_test123$' "$tmp/.env" || { echo "RESEND_API_KEY not written" >&2; return 1; }
  grep -q '^GOOSAR_LLM_API_KEY=sk-test$' "$tmp/.env" || { echo "GOOSAR_LLM_API_KEY not written" >&2; return 1; }
  grep -q '^GOOSAR_LLM_BASE_URL=https://llm.example.com/v1$' "$tmp/.env" || { echo "GOOSAR_LLM_BASE_URL not written" >&2; return 1; }
}

test_env_override_appends_missing_keys_and_leaves_others() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'JWT_SECRET=abc\n' >"$tmp/.env"

  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_RESEND_KEY="re_append"
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>&1

  grep -q '^JWT_SECRET=abc$' "$tmp/.env" || { echo "unrelated key clobbered" >&2; return 1; }
  grep -q '^RESEND_API_KEY=re_append$' "$tmp/.env" || { echo "missing key not appended" >&2; return 1; }
  if grep -q 'GOOSAR_LLM' "$tmp/.env"; then
    echo "unset flags must not write LLM keys" >&2
    return 1
  fi
}

test_llm_key_without_base_url_warns() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'GOOSAR_LLM_API_KEY=\nGOOSAR_LLM_BASE_URL=\n' >"$tmp/.env"

  local err="$tmp/err"
  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_LLM_KEY="sk-test"
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>"$err"

  grep -q 'llm-base-url' "$err" || { echo "expected base-url warning, got: $(cat "$err")" >&2; return 1; }
}

test_env_override_value_with_sed_specials_written_verbatim() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'RESEND_API_KEY=\nGOOSAR_LLM_BASE_URL=\n' >"$tmp/.env"

  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_RESEND_KEY='re_a&b#c\d'
    OPT_LLM_BASE_URL='https://llm.example.com/v1?a=1&b=2#frag'
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>&1 || { echo "apply_env_overrides failed on sed-special value" >&2; return 1; }

  grep -qF 'RESEND_API_KEY=re_a&b#c\d' "$tmp/.env" || { echo "sed-special key corrupted: $(grep RESEND "$tmp/.env")" >&2; return 1; }
  grep -qF 'GOOSAR_LLM_BASE_URL=https://llm.example.com/v1?a=1&b=2#frag' "$tmp/.env" || { echo "sed-special url corrupted: $(grep BASE_URL "$tmp/.env")" >&2; return 1; }
}

# --- SMTP flags (#429) ------------------------------------------------------
test_smtp_flags_write_env_entries() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'SMTP_HOST=\n' >"$tmp/.env"

  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_SMTP_HOST="smtp.example.com"
    OPT_SMTP_PORT="587"
    OPT_SMTP_USER="mailer@example.com"
    OPT_SMTP_PASSWORD='p@ss&word#1'
    OPT_SMTP_FROM="noreply@example.com"
    OPT_SMTP_TLS="starttls"
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>&1

  grep -q '^SMTP_HOST=smtp.example.com$' "$tmp/.env" || { echo "SMTP_HOST not written" >&2; return 1; }
  grep -q '^SMTP_PORT=587$' "$tmp/.env" || { echo "SMTP_PORT not written" >&2; return 1; }
  grep -q '^SMTP_USERNAME=mailer@example.com$' "$tmp/.env" || { echo "SMTP_USERNAME not written" >&2; return 1; }
  grep -qF 'SMTP_PASSWORD=p@ss&word#1' "$tmp/.env" || { echo "SMTP_PASSWORD corrupted: $(grep SMTP_PASSWORD "$tmp/.env")" >&2; return 1; }
  grep -q '^SMTP_FROM_EMAIL=noreply@example.com$' "$tmp/.env" || { echo "SMTP_FROM_EMAIL not written" >&2; return 1; }
  grep -q '^SMTP_TLS=starttls$' "$tmp/.env" || { echo "SMTP_TLS not written" >&2; return 1; }
}

# The server refuses to start with SMTP_HOST and no sender address (#429), so
# the installer must say that here rather than leave a crash-looping container.
test_smtp_host_without_from_warns() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  printf 'SMTP_HOST=\nSMTP_FROM_EMAIL=\n' >"$tmp/.env"

  local err="$tmp/err"
  (
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_SMTP_HOST="smtp.example.com"
    apply_env_overrides "$tmp/.env"
  ) >/dev/null 2>"$err"

  grep -q 'smtp-from' "$err" || { echo "expected smtp-from warning, got: $(cat "$err")" >&2; return 1; }
}

test_smtp_flags_require_a_value() {
  for flag in --smtp-host --smtp-port --smtp-user --smtp-password --smtp-from --smtp-tls; do
    if bash "$ROOT_DIR/scripts/install.sh" "$flag" >/dev/null 2>&1; then
      echo "$flag without a value must fail" >&2
      return 1
    fi
  done
}

# #428: --with-server must bootstrap .env through scripts/selfhost-env.sh, so
# the recommended install path gets all four secrets, the image pin and the
# delivery profile — not the two-secret half-copy the installer used to carry.
_make_checkout_fixture() {
  local dir="$1"
  mkdir -p "$dir/scripts"
  cp "$ROOT_DIR/.env.example" "$dir/.env.example"
  cp "$ROOT_DIR/scripts/selfhost-env.sh" "$dir/scripts/selfhost-env.sh"
  git -C "$dir" init -q
  git -C "$dir" -c user.email=t@t -c user.name=t commit -q --allow-empty -m x
  git -C "$dir" tag v1.2.3
}

_env_val() { sed -n "s/^$2=//p" "$1" | tail -n1; }

test_bootstrap_env_generates_every_secret_and_pins_the_installed_tag() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _make_checkout_fixture "$tmp"

  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    bootstrap_env v1.2.3
  ) >/dev/null 2>&1

  local key
  for key in JWT_SECRET POSTGRES_PASSWORD GOOSAR_VCS_SECRET_KEY GOOSAR_MCP_SECRET_KEY; do
    if [ -z "$(_env_val "$tmp/.env" "$key")" ]; then
      echo "installer .env bootstrap left $key empty" >&2
      return 1
    fi
  done

  local tag profile
  tag="$(_env_val "$tmp/.env" GOOSAR_IMAGE_TAG)"
  [ "$tag" = "v1.2.3" ] || { echo "GOOSAR_IMAGE_TAG=$tag, want the installed version v1.2.3 (not :latest)" >&2; return 1; }
  profile="$(_env_val "$tmp/.env" GOOSAR_DELIVERY_PROFILE)"
  [ "$profile" = "perimeter" ] || { echo "GOOSAR_DELIVERY_PROFILE=$profile, want perimeter by default" >&2; return 1; }
}

test_bootstrap_env_honors_profile_flag_and_derives_public_host() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _make_checkout_fixture "$tmp"
  # PUBLIC_HOST derivation is part of what the installer path was missing.
  sed -i.bak 's|^PUBLIC_HOST=.*|PUBLIC_HOST=goosar.corp.example|' "$tmp/.env.example"

  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_PROFILE=cloud
    bootstrap_env v1.2.3
  ) >/dev/null 2>&1

  local profile origin
  profile="$(_env_val "$tmp/.env" GOOSAR_DELIVERY_PROFILE)"
  [ "$profile" = "cloud" ] || { echo "--profile cloud not applied (got '$profile')" >&2; return 1; }
  origin="$(_env_val "$tmp/.env" FRONTEND_ORIGIN)"
  [ "$origin" = "https://goosar.corp.example" ] || { echo "PUBLIC_HOST derivation did not run through the installer path (FRONTEND_ORIGIN='$origin')" >&2; return 1; }
}

# ADR-0015 (#530): --profile must actually reach .env for all four deployment
# names. selfhost-config.test.sh exercises selfhost-env.sh directly; only this
# test covers install.sh's own flag parsing, where `dev` and `local` are the
# two values a `grep` for the name cannot distinguish from /dev/null and the
# shell keyword.
test_deployment_profile_flag_reaches_the_env_for_every_name() {
  local profile root tmp got
  root="$(mktemp -d)"
  trap 'rm -rf "$root"' RETURN
  for profile in perimeter demo dev local; do
    # Through the real flag parser: --help is handled after --profile, so a
    # value the parser rejects fails here. (`grep -q dev install.sh` cannot
    # tell the profile apart from /dev/null, nor `local` from the keyword.)
    if ! bash "$ROOT_DIR/scripts/install.sh" --profile "$profile" --help >/dev/null 2>&1; then
      echo "install.sh rejects --profile $profile" >&2
      return 1
    fi
    tmp="$root/$profile"
    mkdir -p "$tmp"
    _make_checkout_fixture "$tmp"
    (
      cd "$tmp"
      # shellcheck disable=SC1091
      source "$ROOT_DIR/scripts/install.sh"
      OPT_PROFILE="$profile"
      bootstrap_env v1.2.3
    ) >/dev/null 2>&1
    got="$(_env_val "$tmp/.env" GOOSAR_DEPLOYMENT_PROFILE)"
    [ "$got" = "$profile" ] || { echo "--profile $profile did not reach .env (GOOSAR_DEPLOYMENT_PROFILE='$got')" >&2; return 1; }
    # A deployment profile must never switch the egress profile.
    got="$(_env_val "$tmp/.env" GOOSAR_DELIVERY_PROFILE)"
    [ "$got" = "perimeter" ] || { echo "--profile $profile changed GOOSAR_DELIVERY_PROFILE to '$got'" >&2; return 1; }
    got="$(_env_val "$tmp/.env" ALLOW_SIGNUP)"
    if [ "$profile" = "perimeter" ]; then
      [ "$got" = "false" ] || { echo "--profile perimeter left registration open (ALLOW_SIGNUP='$got')" >&2; return 1; }
    else
      [ "$got" = "true" ] || { echo "--profile $profile did not open registration (ALLOW_SIGNUP='$got')" >&2; return 1; }
    fi
  done
}

# install.ps1 falls back to $env:GOOSAR_DEPLOYMENT_PROFILE when -Profile is
# absent, and SELF_HOSTING.md advertises the variable; the bash installer used
# to pass an unconditional empty value and silently discard it.
test_exported_deployment_profile_applies_without_the_flag() {
  local tmp got
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _make_checkout_fixture "$tmp"
  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    export GOOSAR_DEPLOYMENT_PROFILE=perimeter
    bootstrap_env v1.2.3
  ) >/dev/null 2>&1
  got="$(_env_val "$tmp/.env" GOOSAR_DEPLOYMENT_PROFILE)"
  [ "$got" = "perimeter" ] || { echo "exported GOOSAR_DEPLOYMENT_PROFILE was discarded (got '$got')" >&2; return 1; }
  got="$(_env_val "$tmp/.env" ALLOW_SIGNUP)"
  [ "$got" = "false" ] || { echo "exported perimeter profile left ALLOW_SIGNUP='$got'" >&2; return 1; }
}

test_profile_flag_rejects_unknown_values() {
  if bash "$ROOT_DIR/scripts/install.sh" --profile bogus >/dev/null 2>&1; then
    echo "--profile bogus must fail" >&2
    return 1
  fi
  for flag in --profile --tag; do
    if bash "$ROOT_DIR/scripts/install.sh" "$flag" >/dev/null 2>&1; then
      echo "$flag without a value must fail" >&2
      return 1
    fi
  done
}

# #428: an upgrade with no installation must say so instead of half-running.
test_upgrade_without_installation_fails_clearly() {
  local tmp err
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  err="$tmp/err"
  if GOOSAR_INSTALL_DIR="$tmp/absent" bash "$ROOT_DIR/scripts/install.sh" --upgrade >/dev/null 2>"$err"; then
    echo "--upgrade against a missing installation must fail" >&2
    return 1
  fi
  grep -q "No Goosar installation found" "$err" || { echo "unclear --upgrade error: $(cat "$err")" >&2; return 1; }
}

# #428: a typo'd --tag must fail before .env is repointed. checkout_server_ref
# swallows a failed checkout, so writing the pin first would leave the stand on
# the old tree with .env naming a tag that does not exist — it breaks on the
# operator's next `docker compose up -d`, far from the command that caused it.
test_upgrade_rejects_a_tag_that_does_not_exist() {
  local tmp err
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  err="$tmp/err"
  mkdir -p "$tmp/install" "$tmp/bin"
  # Stub docker so check_docker passes without a daemon; the run must fail on
  # the tag check, long before any docker command that matters.
  printf '#!/bin/sh\nexit 0\n' > "$tmp/bin/docker"
  chmod +x "$tmp/bin/docker"
  git -C "$tmp/install" init -q
  git -C "$tmp/install" -c user.email=t@t -c user.name=t commit -q --allow-empty -m x
  git -C "$tmp/install" tag v1.2.3
  printf 'GOOSAR_IMAGE_TAG=v1.2.3\n' > "$tmp/install/.env"

  if PATH="$tmp/bin:$PATH" GOOSAR_INSTALL_DIR="$tmp/install" \
      bash "$ROOT_DIR/scripts/install.sh" --upgrade --tag v9.9.9 >/dev/null 2>"$err"; then
    echo "--upgrade --tag v9.9.9 must fail on a tag that does not exist" >&2
    return 1
  fi
  grep -q "No such release tag" "$err" || { echo "unclear bad-tag error: $(cat "$err")" >&2; return 1; }
  local pin
  pin="$(_env_val "$tmp/install/.env" GOOSAR_IMAGE_TAG)"
  [ "$pin" = "v1.2.3" ] || { echo ".env was repointed to a nonexistent tag (GOOSAR_IMAGE_TAG=$pin)" >&2; return 1; }
}

# --profile only takes effect when .env is created. On a re-run it is a silent
# no-op, and silence on an egress-relevant flag is the failure mode.
test_profile_on_an_existing_env_warns_that_it_is_ignored() {
  local tmp err
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _make_checkout_fixture "$tmp"
  err="$tmp/err"

  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    bootstrap_env v1.2.3            # creates .env (perimeter)
    OPT_PROFILE=cloud
    bootstrap_env v1.2.3            # re-run: must not silently do nothing
  ) >/dev/null 2>"$err"

  grep -q -- "--profile cloud ignored" "$err" || { echo "re-run with --profile did not warn: $(cat "$err")" >&2; return 1; }
  local profile
  profile="$(_env_val "$tmp/.env" GOOSAR_DELIVERY_PROFILE)"
  [ "$profile" = "perimeter" ] || { echo "existing .env profile was rewritten to '$profile'" >&2; return 1; }
}

# #530 review: `--profile perimeter` writes ALLOW_SIGNUP=false, and the only
# way past that gate is ALLOWED_EMAIL_DOMAINS / ALLOWED_EMAILS. Without one of
# them the installer finishes with a green banner on a stand nobody — the
# operator included — can sign into. The mid-run preset echo is lost behind
# `docker pull` output, so the check has to be in the final summary.
test_closed_signup_without_an_allowlist_is_gated_in_the_summary() {
  local tmp err
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _make_checkout_fixture "$tmp"
  err="$tmp/err"

  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    OPT_PROFILE=perimeter
    bootstrap_env v1.2.3
    warn_signup_closed_without_allowlist .env
  ) >/dev/null 2>"$err"
  grep -q "ALLOWED_EMAIL_DOMAINS" "$err" || { echo "closed signup with no allowlist was not gated: $(cat "$err")" >&2; return 1; }

  # With an allowlist the gate must stay quiet, or it is noise operators learn
  # to ignore.
  (
    cd "$tmp"
    # shellcheck disable=SC1091
    source "$ROOT_DIR/scripts/install.sh"
    set_env_kv .env ALLOWED_EMAIL_DOMAINS example.test
    warn_signup_closed_without_allowlist .env
  ) >/dev/null 2>"$tmp/err2"
  if grep -q "ALLOWED_EMAIL_DOMAINS" "$tmp/err2"; then
    echo "gate fired even though an allowlist is set: $(cat "$tmp/err2")" >&2
    return 1
  fi

  # ...and a summary must actually call it: a helper nobody invokes is the same
  # silent success this test exists to prevent. Since R-18g the caller is
  # run_upgrade — --with-server no longer provisions anything itself.
  local body
  body="$(_function_body run_upgrade)"
  grep -q "warn_signup_closed_without_allowlist" <<<"$body" ||
    { echo "run_upgrade does not call warn_signup_closed_without_allowlist" >&2; return 1; }
}

# Everything from `name()` to the first line that is exactly `}`.
_function_body() {
  awk -v fn="$1" '$0 ~ "^"fn"\\(\\)" {f=1} f {print} f && /^}$/ {exit}' "$ROOT_DIR/scripts/install.sh"
}

# 0.11.0 (T-29, ADR-0016): --with-server is gone. The flag is still parsed so
# an old runbook gets the new command and exit 2, not "unknown option"; the
# in-tree provisioning it once did must stay gone.
test_with_server_is_removed_and_names_make_selfhost() {
  local tmp out rc=0
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  mkdir -p "$tmp/scripts"
  cp "$ROOT_DIR/scripts/install.sh" "$tmp/scripts/install.sh"
  # A selfhost-env.sh that must NOT be called: the wrapper is removed, not routed.
  printf '#!/usr/bin/env bash\necho ENV-CALLED\n' > "$tmp/scripts/selfhost-env.sh"; chmod +x "$tmp/scripts/selfhost-env.sh"
  out="$(bash "$tmp/scripts/install.sh" --with-server --smtp-host relay.invalid --resend-key secret 2>&1)" || rc=$?
  [ "$rc" -eq 2 ] || { echo "--with-server exited $rc, want 2: $out" >&2; return 1; }
  grep -q "removed in 0.11.0" <<<"$out" || { echo "no removal notice: $out" >&2; return 1; }
  grep -q "make selfhost" <<<"$out" || { echo "the notice does not name the new command: $out" >&2; return 1; }
  grep -q "ENV-CALLED" <<<"$out" && { echo "--with-server still bootstraps a deployment" >&2; return 1; }
  if grep -q "^setup_server()" "$ROOT_DIR/scripts/install.sh"; then
    echo "setup_server still exists: --with-server has a live path again" >&2
    return 1
  fi
  return 0
}

test_upgrade_prints_rollback_recipe_source() {
  # The rollback recipe is the point of --upgrade: images roll back, the
  # database does not. Guard the text so it cannot be dropped silently.
  local body
  body="$(awk '/^run_upgrade\(\)/{f=1} f' "$ROOT_DIR/scripts/install.sh")"
  for needle in "Rollback recipe" "scripts/restore.sh" "GOOSAR_IMAGE_TAG"; do
    grep -qF "$needle" <<<"$body" || { echo "run_upgrade does not mention '$needle'" >&2; return 1; }
  done
}

test_help_says_the_legacy_flags_map_to_make_selfhost() {
  local out
  out="$(bash "$ROOT_DIR/scripts/install.sh" --help)"
  grep -qi "deprecated" <<<"$out" ||
    { echo "--help does not call the legacy server flags deprecated" >&2; return 1; }
  grep -q "make selfhost" <<<"$out" ||
    { echo "--help does not say the legacy flags map to make selfhost" >&2; return 1; }
}

test_help_documents_env_override_flags() {
  local out
  out="$(bash "$ROOT_DIR/scripts/install.sh" --help)"
  for flag in resend-key llm-key llm-base-url smtp-host smtp-port smtp-user smtp-password smtp-from smtp-tls profile upgrade tag; do
    if ! grep -q -- "--$flag" <<<"$out"; then
      echo "--help does not document --$flag" >&2
      return 1
    fi
  done
}

test_help_says_the_legacy_flags_map_to_make_selfhost
test_brew_install_failure_falls_back_to_release_binary
test_brew_tap_failure_falls_back_to_release_binary
test_missing_checksums_aborts_the_install
test_remote_ssh_install_prints_token_login_hint
test_local_install_does_not_print_token_login_hint
test_env_override_flags_write_env_entries
test_env_override_appends_missing_keys_and_leaves_others
test_llm_key_without_base_url_warns
test_env_override_value_with_sed_specials_written_verbatim
test_smtp_flags_write_env_entries
test_smtp_host_without_from_warns
test_smtp_flags_require_a_value
test_help_documents_env_override_flags
test_bootstrap_env_generates_every_secret_and_pins_the_installed_tag
test_bootstrap_env_honors_profile_flag_and_derives_public_host
test_deployment_profile_flag_reaches_the_env_for_every_name
test_exported_deployment_profile_applies_without_the_flag
test_profile_flag_rejects_unknown_values
test_upgrade_without_installation_fails_clearly
test_upgrade_rejects_a_tag_that_does_not_exist
test_profile_on_an_existing_env_warns_that_it_is_ignored
test_closed_signup_without_an_allowlist_is_gated_in_the_summary
test_upgrade_prints_rollback_recipe_source
test_with_server_is_removed_and_names_make_selfhost
echo "install.sh tests passed"
