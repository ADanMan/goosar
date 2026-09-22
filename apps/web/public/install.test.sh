#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INSTALL_SH="$ROOT_DIR/apps/web/public/install.sh"

_sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

_expected_archive_name() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *) echo "unsupported test host OS: $(uname -s)" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported test host arch: $(uname -m)" >&2; exit 1 ;;
  esac
  printf 'goosar-cli-%s-%s.tar.gz' "$os" "$arch"
}

_expected_agent_archive_name() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *) echo "unsupported test host OS: $(uname -s)" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported test host arch: $(uname -m)" >&2; exit 1 ;;
  esac
  printf 'hermes-%s-%s.tar.gz' "$os" "$arch"
}

# _build_agent_archive builds a hermes agent tarball whose
# bin/install-artifact.sh follows the real hermes-agent contract: it
# requires --artifact <dir> (dies otherwise, like the real script's
# `[ -n "$ARTIFACT" ] || die "нужен --artifact <каталог>"`), and installs
# into ${HERMES_HOME:-~/.hermes}/runtime/<version> and flips a `current`
# symlink. Good enough to prove install.sh invokes it correctly without
# depending on the real hermes-agent repository.
#
# build-artifact.sh (scripts/build-artifact.sh in hermes-agent) packs the
# real archive with a top-level hermes-<version>-<os>-<arch>/ wrapper
# directory rather than a flat bin/. `wrapped` picks between that layout
# (default, matches production) and the flat layout, so both are covered by
# the test suite.
_build_agent_archive() {
  local tmp="$1"
  local wrapped="${2:-1}"
  local payload_root="$tmp/agent-payload"
  local tar_c_dir="$payload_root"
  local tar_arg="bin"
  if [ "$wrapped" = "1" ]; then
    tar_c_dir="$tmp/agent-payload"
    payload_root="$tmp/agent-payload/hermes-1.0.0-test-wrapper"
    tar_arg="hermes-1.0.0-test-wrapper"
  fi
  local bin_dir="$payload_root/bin"
  mkdir -p "$bin_dir"
  cat >"$bin_dir/install-artifact.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
artifact=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--artifact" ]; then
    artifact="$arg"
  fi
  prev="$arg"
done
[ -n "$artifact" ] || { echo "ошибка: нужен --artifact <каталог>" >&2; exit 1; }
[ -d "$artifact" ] || { echo "ошибка: не каталог: $artifact" >&2; exit 1; }
home="${HERMES_HOME:-$HOME/.hermes}"
version_dir="$home/runtime/test-version"
mkdir -p "$version_dir/bin"
printf '#!/usr/bin/env bash\necho "hermes 1.0.0-test"\n' >"$version_dir/bin/hermes"
chmod +x "$version_dir/bin/hermes"
ln -sfn "$version_dir" "$home/runtime/current"
STUB
  chmod +x "$bin_dir/install-artifact.sh"
  tar -czf "$tmp/hermes.tar.gz" -C "$tar_c_dir" "$tar_arg"
}

_setup_sandbox() {
  local tmp="$1"
  local wrapped="${2:-1}"
  mkdir -p "$tmp/stub-bin" "$tmp/install-bin" "$tmp/payload"

  cat >"$tmp/payload/goosar" <<'STUB'
#!/usr/bin/env bash
echo "goosar v0.3.2 (commit: test)"
STUB
  chmod +x "$tmp/payload/goosar"
  tar -czf "$tmp/goosar.tar.gz" -C "$tmp/payload" goosar
  _build_agent_archive "$tmp" "$wrapped"

  cat >"$tmp/stub-bin/curl" <<'STUB'
#!/usr/bin/env bash
out=""
url=""
write=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
  elif [ "$prev" = "-w" ]; then
    write="$arg"
  elif [ "${arg#-}" = "$arg" ]; then
    url="$arg"
  fi
  prev="$arg"
done

printf '%s\n' "$url" >>"${GOOSAR_TEST_CURL_LOG:-/dev/null}"

case "$url" in
  */checksums.txt)
    if [ -n "${GOOSAR_TEST_NO_CHECKSUMS:-}" ]; then
      echo "stub curl: simulated checksums.txt download failure" >&2
      exit 22
    fi
    cp "$GOOSAR_TEST_CHECKSUMS_FILE" "$out"
    if [ -n "$write" ]; then printf '200'; fi
    ;;
  */hermes-*)
    if [ -n "${GOOSAR_TEST_AGENT_404:-}" ]; then
      : >"$out"
      if [ -n "$write" ]; then printf '404'; fi
      exit 0
    fi
    cp "$GOOSAR_TEST_AGENT_ARCHIVE" "$out"
    if [ -n "$write" ]; then printf '200'; fi
    ;;
  *)
    cp "$GOOSAR_TEST_ARCHIVE" "$out"
    if [ -n "$write" ]; then printf '200'; fi
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/curl"
}

_build_no_checksum_tool_bin() {
  local dir="$1"
  mkdir -p "$dir"
  local tool resolved
  for tool in bash tar awk mktemp chmod mv cp rm uname cat sed grep; do
    resolved="$(command -v "$tool" || true)"
    if [ -n "$resolved" ]; then
      ln -sf "$resolved" "$dir/$tool"
    fi
  done
}

_run_installer_raw() {
  local tmp="$1"; shift
  local out="$tmp/install.out"
  local err="$tmp/install.err"
  local tool_path="${GOOSAR_TEST_TOOL_PATH:-/usr/bin:/bin}"
  local status=0
  PATH="$tmp/stub-bin:$tool_path" \
    GOOSAR_TEST_ARCHIVE="$tmp/goosar.tar.gz" \
    GOOSAR_TEST_AGENT_ARCHIVE="$tmp/hermes.tar.gz" \
    GOOSAR_TEST_CHECKSUMS_FILE="$tmp/checksums.txt" \
    GOOSAR_TEST_NO_CHECKSUMS="${GOOSAR_TEST_NO_CHECKSUMS:-}" \
    GOOSAR_TEST_AGENT_404="${GOOSAR_TEST_AGENT_404:-}" \
    GOOSAR_TEST_CURL_LOG="$tmp/curl.log" \
    GOOSAR_RUNNER="${GOOSAR_RUNNER:-}" \
    GOOSAR_INSTALL_INTERACTIVE="${GOOSAR_INSTALL_INTERACTIVE:-}" \
    GOOSAR_INSTALL_TTY="${GOOSAR_INSTALL_TTY:-/dev/null}" \
    HOME="$tmp/home" \
    bash "$INSTALL_SH" --app-url "https://goosar.test" --install-dir "$tmp/install-bin" "$@" \
    >"$out" 2>"$err" || status=$?
  return "$status"
}

# _run_installer is the helper every goosar-CLI-focused test in this file
# uses. It passes --runner none so those tests keep exercising exactly the
# goosar CLI checksum/install logic, without the agent install step (which has
# its own checksums.txt entry requirement, covered by the runner tests below).
_run_installer() {
  local tmp="$1"; shift
  _run_installer_raw "$tmp" --runner none "$@"
}

_assert_contains() {
  local file="$1" needle="$2" label="$3"
  if ! grep -qF -- "$needle" "$file"; then
    echo "expected $label to contain: $needle" >&2
    echo "--- $file ---" >&2
    cat "$file" >&2 || true
    return 1
  fi
}

test_missing_checksums_file_aborts() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"

  local status=0
  GOOSAR_TEST_NO_CHECKSUMS=1 _run_installer "$tmp" || status=$?

  if [ "$status" -eq 0 ]; then
    echo "expected install.sh to abort when checksums.txt cannot be downloaded" >&2
    return 1
  fi
  if [ -e "$tmp/install-bin/goosar" ]; then
    echo "goosar must not be installed when checksum verification is refused" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "Could not download checksums.txt" stderr
  _assert_contains "$tmp/install.err" "--skip-checksum" stderr
}

test_no_entry_for_file_aborts() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "some-other-file.tar.gz" >"$tmp/checksums.txt"

  local status=0
  _run_installer "$tmp" || status=$?

  if [ "$status" -eq 0 ]; then
    echo "expected install.sh to abort when checksums.txt has no matching entry" >&2
    return 1
  fi
  if [ -e "$tmp/install-bin/goosar" ]; then
    echo "goosar must not be installed when checksum verification is refused" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "has no entry for" stderr
}

test_no_checksum_utility_aborts() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf 'deadbeef00000000000000000000000000000000000000000000000000000000  %s\n' "$(_expected_archive_name)" >"$tmp/checksums.txt"

  local no_checksum_tools="$tmp/no-checksum-tools"
  _build_no_checksum_tool_bin "$no_checksum_tools"

  local status=0
  GOOSAR_TEST_TOOL_PATH="$no_checksum_tools" _run_installer "$tmp" || status=$?

  if [ "$status" -eq 0 ]; then
    echo "expected install.sh to abort when neither shasum nor sha256sum is available" >&2
    return 1
  fi
  if [ -e "$tmp/install-bin/goosar" ]; then
    echo "goosar must not be installed when checksum verification is refused" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "Neither shasum nor sha256sum" stderr
}

test_mismatch_aborts() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf 'deadbeef00000000000000000000000000000000000000000000000000000000  %s\n' "$(_expected_archive_name)" >"$tmp/checksums.txt"

  local status=0
  _run_installer "$tmp" || status=$?

  if [ "$status" -eq 0 ]; then
    echo "expected install.sh to abort on checksum mismatch" >&2
    return 1
  fi
  if [ -e "$tmp/install-bin/goosar" ]; then
    echo "goosar must not be installed on checksum mismatch" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "Checksum mismatch" stderr
}

test_match_installs() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  if ! _run_installer "$tmp"; then
    echo "expected install.sh to succeed on a matching checksum" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed after a matching checksum" >&2
    return 1
  fi
  _assert_contains "$tmp/install.out" "Checksum verified" stdout
}

test_skip_checksum_flag_installs_with_warning() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"

  local status=0
  GOOSAR_TEST_NO_CHECKSUMS=1 _run_installer "$tmp" --skip-checksum || status=$?

  if [ "$status" -ne 0 ]; then
    echo "expected --skip-checksum to install despite an unreachable checksums.txt" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed with --skip-checksum" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "Checksum verification skipped" stderr
}

test_skip_checksum_env_var_installs_with_warning() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"

  local out="$tmp/install.out"
  local err="$tmp/install.err"
  local status=0
  PATH="$tmp/stub-bin:${GOOSAR_TEST_TOOL_PATH:-/usr/bin:/bin}" \
    GOOSAR_TEST_ARCHIVE="$tmp/goosar.tar.gz" \
    GOOSAR_TEST_CHECKSUMS_FILE="$tmp/checksums.txt" \
    GOOSAR_TEST_NO_CHECKSUMS=1 \
    GOOSAR_SKIP_CHECKSUM=1 \
    HOME="$tmp/home" \
    bash "$INSTALL_SH" --app-url "https://goosar.test" --install-dir "$tmp/install-bin" --runner none \
    >"$out" 2>"$err" || status=$?

  if [ "$status" -ne 0 ]; then
    echo "expected GOOSAR_SKIP_CHECKSUM=1 to install despite an unreachable checksums.txt" >&2
    cat "$out" "$err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed with GOOSAR_SKIP_CHECKSUM=1" >&2
    return 1
  fi
  _assert_contains "$err" "Checksum verification skipped" stderr
}

# --- #566: an installer must not report success on a binary that does not run
test_broken_binary_fails() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  # Wrong-architecture / truncated binary: --version exits non-zero.
  printf '#!/usr/bin/env bash\necho "exec format error" >&2\nexit 1\n' >"$tmp/payload/goosar"
  chmod +x "$tmp/payload/goosar"
  tar -czf "$tmp/goosar.tar.gz" -C "$tmp/payload" goosar
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  if _run_installer "$tmp"; then
    echo "expected install.sh to fail when the installed binary cannot run" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  _assert_contains "$tmp/install.err" "$tmp/install-bin/goosar --version" stderr
  _assert_contains "$tmp/install.err" "exec format error" stderr
}

# --- #566: a deployment serves exactly one CLI version, so --version cannot work
test_version_flag_is_refused() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  if _run_installer "$tmp" --version v1.2.3; then
    echo "expected install.sh to refuse --version" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "one CLI version" stderr
  if [ -e "$tmp/install-bin/goosar" ]; then
    echo "expected nothing to be installed after a refused --version" >&2
    return 1
  fi
}

# --- #566: a PATH entry with a trailing slash is the same directory
test_path_hint_ignores_trailing_slash() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  if ! GOOSAR_TEST_TOOL_PATH="/usr/bin:/bin:$tmp/install-bin/" _run_installer "$tmp"; then
    echo "expected install.sh to succeed" >&2
    cat "$tmp/install.err" >&2 || true
    return 1
  fi
  if grep -qF "is not on your PATH" "$tmp/install.out"; then
    echo "expected no PATH hint when the dir is on PATH with a trailing slash" >&2
    cat "$tmp/install.out" >&2
    return 1
  fi
}

# --- --runner hermes downloads, verifies, and installs the hermes agent
# runtime via bin/install-artifact.sh, then prints GOOSAR_HERMES_PATH.
test_runner_hermes_installs_agent_runtime() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  {
    printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)"
    printf '%s  %s\n' "$(_sha256_of "$tmp/hermes.tar.gz")" "$(_expected_agent_archive_name)"
  } >"$tmp/checksums.txt"

  if ! _run_installer_raw "$tmp" --runner hermes; then
    echo "expected install.sh --runner hermes to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed" >&2
    return 1
  fi
  if [ ! -x "$tmp/home/.hermes/runtime/current/bin/hermes" ]; then
    echo "expected bin/install-artifact.sh to install hermes under \$HOME/.hermes/runtime/current/bin" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  _assert_contains "$tmp/install.out" "GOOSAR_HERMES_PATH=$tmp/home/.hermes/runtime/current/bin/hermes" stdout
}

# Same as above but with a flat archive layout (bin/ at the tarball root, no
# top-level hermes-<version>-<os>-<arch>/ wrapper). install.sh must invoke
# install-artifact.sh with --artifact regardless of which layout the archive
# uses.
test_runner_hermes_installs_agent_runtime_flat_layout() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp" 0
  {
    printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)"
    printf '%s  %s\n' "$(_sha256_of "$tmp/hermes.tar.gz")" "$(_expected_agent_archive_name)"
  } >"$tmp/checksums.txt"

  if ! _run_installer_raw "$tmp" --runner hermes; then
    echo "expected install.sh (flat agent archive) to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/home/.hermes/runtime/current/bin/hermes" ]; then
    echo "expected bin/install-artifact.sh to install hermes under \$HOME/.hermes/runtime/current/bin (flat layout)" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
}

# --with-agent is kept as an alias of --runner hermes, GOOSAR_RUNNER selects
# the runner like the flag does.
test_with_agent_alias_and_env_select_hermes() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  {
    printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)"
    printf '%s  %s\n' "$(_sha256_of "$tmp/hermes.tar.gz")" "$(_expected_agent_archive_name)"
  } >"$tmp/checksums.txt"

  if ! _run_installer_raw "$tmp" --with-agent; then
    echo "expected --with-agent to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  [ -x "$tmp/home/.hermes/runtime/current/bin/hermes" ] || { echo "--with-agent did not install hermes" >&2; return 1; }

  rm -rf "$tmp/home"
  if ! GOOSAR_RUNNER=hermes _run_installer_raw "$tmp"; then
    echo "expected GOOSAR_RUNNER=hermes to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  [ -x "$tmp/home/.hermes/runtime/current/bin/hermes" ] || { echo "GOOSAR_RUNNER=hermes did not install hermes" >&2; return 1; }
}

# The default is no runner: without any flag and without a terminal nothing
# runner-related is requested from the deployment or installed.
test_default_installs_no_runner() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  if ! _run_installer_raw "$tmp"; then
    echo "expected the default (no runner) install to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed" >&2
    return 1
  fi
  if [ -e "$tmp/home/.hermes" ]; then
    echo "expected the default install to leave hermes alone" >&2
    return 1
  fi
  if grep -q 'hermes' "$tmp/curl.log"; then
    echo "expected no hermes download by default; curl saw:" >&2
    cat "$tmp/curl.log" >&2
    return 1
  fi
  _assert_contains "$tmp/install.out" "Agent runner: none installed" stdout
  _assert_contains "$tmp/install.out" "--runner hermes" stdout
}

# --runner none and its alias --without-agent install no runner either.
test_runner_none_and_without_agent_alias_skip_agent_runtime() {
  local tmp flag
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  for flag in "--runner none" "--without-agent"; do
    : >"$tmp/curl.log"
    # shellcheck disable=SC2086
    if ! _run_installer_raw "$tmp" $flag; then
      echo "expected '$flag' to succeed" >&2
      cat "$tmp/install.out" "$tmp/install.err" >&2 || true
      return 1
    fi
    if [ -e "$tmp/home/.hermes" ] || grep -q 'hermes' "$tmp/curl.log"; then
      echo "expected '$flag' to skip the hermes runtime entirely" >&2
      return 1
    fi
  done
}

# An unknown runner is refused before anything is downloaded, and the message
# lists what is available.
test_unknown_runner_is_refused() {
  local tmp status=0
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  _run_installer_raw "$tmp" --runner nonesuch || status=$?
  if [ "$status" -eq 0 ]; then
    echo "expected an unknown runner to fail" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "Unknown runner 'nonesuch'" stderr
  _assert_contains "$tmp/install.err" "none, hermes" stderr
  if [ -s "$tmp/curl.log" ]; then
    echo "expected nothing to be downloaded for a refused runner; curl saw:" >&2
    cat "$tmp/curl.log" >&2
    return 1
  fi
}

# An interactive run with no explicit choice is asked, the prompt lists the
# choices with `none` as the default, and the answer decides.
test_interactive_prompt_lists_choices_and_honours_answer() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  {
    printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)"
    printf '%s  %s\n' "$(_sha256_of "$tmp/hermes.tar.gz")" "$(_expected_agent_archive_name)"
  } >"$tmp/checksums.txt"

  # Empty answer: the default, none.
  printf '\n' >"$tmp/answer"
  if ! GOOSAR_INSTALL_INTERACTIVE=1 GOOSAR_INSTALL_TTY="$tmp/answer" _run_installer_raw "$tmp"; then
    echo "expected the interactive default answer to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  _assert_contains "$tmp/install.out" "1) none" stdout
  _assert_contains "$tmp/install.out" "2) hermes" stdout
  _assert_contains "$tmp/install.out" "Choice [1]" stdout
  [ ! -e "$tmp/home/.hermes" ] || { echo "an empty answer must install no runner" >&2; return 1; }

  # "2" and the name both select hermes.
  local answer
  for answer in 2 hermes; do
    rm -rf "$tmp/home"
    printf '%s\n' "$answer" >"$tmp/answer"
    if ! GOOSAR_INSTALL_INTERACTIVE=1 GOOSAR_INSTALL_TTY="$tmp/answer" _run_installer_raw "$tmp"; then
      echo "expected answer '$answer' to succeed" >&2
      cat "$tmp/install.out" "$tmp/install.err" >&2 || true
      return 1
    fi
    [ -x "$tmp/home/.hermes/runtime/current/bin/hermes" ] || { echo "answer '$answer' did not install hermes" >&2; return 1; }
  done

  # Anything else falls back to none, with a warning.
  rm -rf "$tmp/home"
  printf 'maybe\n' >"$tmp/answer"
  GOOSAR_INSTALL_INTERACTIVE=1 GOOSAR_INSTALL_TTY="$tmp/answer" _run_installer_raw "$tmp" || {
    echo "expected an unrecognised answer to fall back to none" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  }
  _assert_contains "$tmp/install.err" "Unrecognised choice" stderr
  [ ! -e "$tmp/home/.hermes" ] || { echo "an unrecognised answer must install no runner" >&2; return 1; }
}

# An explicit choice never prompts.
test_explicit_runner_skips_prompt() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  printf '2\n' >"$tmp/answer"
  if ! GOOSAR_INSTALL_INTERACTIVE=1 GOOSAR_INSTALL_TTY="$tmp/answer" _run_installer_raw "$tmp" --runner none; then
    echo "expected --runner none to succeed" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if grep -q 'Choice \[1\]' "$tmp/install.out"; then
    echo "expected an explicit --runner to skip the prompt" >&2
    return 1
  fi
  [ ! -e "$tmp/home/.hermes" ] || { echo "--runner none must win over the tty answer" >&2; return 1; }
}

# A 404 for this platform's agent artifact must warn, not fail — the goosar CLI
# itself already installed successfully.
test_agent_404_warns_and_still_succeeds() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  _setup_sandbox "$tmp"
  printf '%s  %s\n' "$(_sha256_of "$tmp/goosar.tar.gz")" "$(_expected_archive_name)" >"$tmp/checksums.txt"

  local status=0
  GOOSAR_TEST_AGENT_404=1 _run_installer_raw "$tmp" --runner hermes || status=$?

  if [ "$status" -ne 0 ]; then
    echo "expected a 404 on the agent artifact to still exit 0" >&2
    cat "$tmp/install.out" "$tmp/install.err" >&2 || true
    return 1
  fi
  if [ ! -x "$tmp/install-bin/goosar" ]; then
    echo "expected goosar to be installed despite the agent 404" >&2
    return 1
  fi
  if [ -e "$tmp/home/.hermes" ]; then
    echo "expected no hermes runtime to be installed after a 404" >&2
    return 1
  fi
  _assert_contains "$tmp/install.err" "No hermes agent artifact published" stderr
}

test_missing_checksums_file_aborts
test_no_entry_for_file_aborts
test_no_checksum_utility_aborts
test_mismatch_aborts
test_match_installs
test_skip_checksum_flag_installs_with_warning
test_skip_checksum_env_var_installs_with_warning
test_broken_binary_fails
test_version_flag_is_refused
test_path_hint_ignores_trailing_slash
test_runner_hermes_installs_agent_runtime
test_runner_hermes_installs_agent_runtime_flat_layout
test_with_agent_alias_and_env_select_hermes
test_default_installs_no_runner
test_runner_none_and_without_agent_alias_skip_agent_runtime
test_unknown_runner_is_refused
test_interactive_prompt_lists_choices_and_honours_answer
test_explicit_runner_skips_prompt
test_agent_404_warns_and_still_succeeds
echo "apps/web/public/install.sh checksum tests passed"
