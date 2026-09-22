#!/usr/bin/env bash
# Gate tests for scripts/release-local.sh (issue #170). Only the argument /
# tag-shape gates are covered: they run before any tool, daemon, or network
# check, so the tests are hermetic on any machine.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/release-local.sh"

PASS=0
FAIL=0

expect_fail() { # expect_fail <name> <stderr-substring> [args...]
  local name="$1" want="$2"
  shift 2
  local out rc=0
  out="$(bash "$SCRIPT" "$@" 2>&1)" || rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "FAIL: $name — expected non-zero exit"
    FAIL=$((FAIL + 1))
    return
  fi
  if ! printf '%s' "$out" | grep -qF "$want"; then
    echo "FAIL: $name — stderr missing '$want'; got: $out"
    FAIL=$((FAIL + 1))
    return
  fi
  echo "ok: $name"
  PASS=$((PASS + 1))
}

# No tag at all.
expect_fail "no-tag" "usage: bash scripts/release-local.sh"

# Malformed tags must be rejected by the same strict-semver gate release.yml uses.
expect_fail "bad-tag-plain" "must look like vX.Y.Z" "0.6.6"
expect_fail "bad-tag-two-part" "must look like vX.Y.Z" "v0.6"
expect_fail "bad-tag-garbage" "must look like vX.Y.Z" "vabc"

# Dirty tags are refused even though they match the shape regex.
expect_fail "dirty-tag" "refusing to release from dirty tag" "v0.6.6-dirty.0"

# Unknown flags fail fast instead of being silently ignored.
expect_fail "unknown-flag" "unknown flag" "v0.6.6" --frobnicate

# A second positional argument is an error, not a silent overwrite.
expect_fail "extra-positional" "unexpected argument" "v0.6.6" "v0.6.7"

# --help exits 0 and prints usage without touching docker/gh.
if bash "$SCRIPT" --help | grep -q "Usage:"; then
  echo "ok: help"
  PASS=$((PASS + 1))
else
  echo "FAIL: help — expected usage text"
  FAIL=$((FAIL + 1))
fi

# --help must document the PARTIAL exit code (issue #279): the whole point of
# exit 2 is that a caller/operator can tell a partial release from a clean one,
# which is useless if --help still implies "it either works or it dies".
if bash "$SCRIPT" --help | grep -q "PARTIAL release"; then
  echo "ok: help documents the PARTIAL exit code"
  PASS=$((PASS + 1))
else
  echo "FAIL: help documents the PARTIAL exit code"
  FAIL=$((FAIL + 1))
fi

# --- env loading (issue #351) ------------------------------------------------
# v0.8.3 died in ensure_test_db because the release run never read .env and
# test-go.sh fell back to postgres://…localhost:5432. The env block runs before
# every tool/daemon check, so GOOSAR_RELEASE_PRINT_ENV keeps these hermetic.
# ENV_FILE is the same explicit override scripts/test-e2e.sh honors; the
# .env.worktree / .env auto-discovery below it is the same three lines.
ENV_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP"' EXIT
printf 'DATABASE_URL=postgres://u:p@localhost:5433/from_env_file?sslmode=disable\n' > "$ENV_TMP/.env"

expect_env() { # expect_env <name> <expected-DATABASE_URL> [env assignments...]
  local name="$1" want="$2"
  shift 2
  local out
  out="$(env "$@" GOOSAR_RELEASE_PRINT_ENV=1 ENV_FILE="$ENV_TMP/.env" \
    bash "$SCRIPT" v0.0.1 2>/dev/null | tail -n 1)" || true
  if [ "$out" = "DATABASE_URL=$want" ]; then
    echo "ok: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name — expected 'DATABASE_URL=$want'; got: $out"
    FAIL=$((FAIL + 1))
  fi
}

# Nothing exported: the env file supplies DATABASE_URL, so the gate runs against
# the checkout's real database instead of test-go.sh's localhost:5432 guess.
expect_env "env-file-supplies-database-url" \
  "postgres://u:p@localhost:5433/from_env_file?sslmode=disable" -u DATABASE_URL

# An explicitly exported DATABASE_URL still wins over the file.
expect_env "exported-database-url-wins" \
  "postgres://u:p@localhost:5433/explicit?sslmode=disable" \
  DATABASE_URL=postgres://u:p@localhost:5433/explicit?sslmode=disable

# Auto-discovery (.env.worktree over .env) is what a bare
# `bash scripts/release-local.sh vX.Y.Z` relies on, so it is exercised against a
# copy of the script in a temp tree: the script resolves both files relative to
# its own parent directory, never the caller's cwd.
COPY_ROOT="$ENV_TMP/copy"
mkdir -p "$COPY_ROOT/scripts"
cp "$SCRIPT" "$COPY_ROOT/scripts/release-local.sh"
printf 'DATABASE_URL=postgres://u:p@localhost:5433/from_dot_env?sslmode=disable\n' > "$COPY_ROOT/.env"

expect_discovered() { # expect_discovered <name> <expected-DATABASE_URL>
  local name="$1" want="$2" out
  out="$(cd / && env -u DATABASE_URL -u ENV_FILE GOOSAR_RELEASE_PRINT_ENV=1 \
    bash "$COPY_ROOT/scripts/release-local.sh" v0.0.1 2>/dev/null | tail -n 1)" || true
  if [ "$out" = "DATABASE_URL=$want" ]; then
    echo "ok: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name — expected 'DATABASE_URL=$want'; got: $out"
    FAIL=$((FAIL + 1))
  fi
}

expect_discovered "discovers-dot-env-from-any-cwd" \
  "postgres://u:p@localhost:5433/from_dot_env?sslmode=disable"

# A RELEASE takes the deployment env, NOT a developer's worktree isolation
# file: .env.worktree carries worktree ports and origins (FRONTEND_ORIGIN=
# http://localhost:PORT), and sourcing those leaked into the Go gate and broke
# service tests that assert default-origin behaviour. .env wins here even when
# a worktree file exists; test-e2e.sh keeps its own worktree-first rule, which
# is correct for e2e's isolated ports.
printf 'DATABASE_URL=postgres://u:p@localhost:5433/from_worktree?sslmode=disable\n' > "$COPY_ROOT/.env.worktree"
expect_discovered "dot-env-beats-worktree-env" \
  "postgres://u:p@localhost:5433/from_dot_env?sslmode=disable"

# ...and the worktree file is still the fallback when there is no .env.
mv "$COPY_ROOT/.env" "$COPY_ROOT/.env.disabled"
expect_discovered "worktree-env-used-when-no-dot-env" \
  "postgres://u:p@localhost:5433/from_worktree?sslmode=disable"
mv "$COPY_ROOT/.env.disabled" "$COPY_ROOT/.env"

# REGRESSION (v0.8.4 release gate): sourcing the whole env file exported the
# deployment's RESEND_API_KEY / SMTP_* into the e2e stack, whose backend then
# answered send-code with 500 and failed every fixture login. Only
# DATABASE_URL may cross over.
printf 'DATABASE_URL=postgres://u:p@localhost:5433/from_dot_env?sslmode=disable\nRESEND_API_KEY=leak-me\nSMTP_HOST=smtp.leak.invalid\n' > "$COPY_ROOT/.env"
rm -f "$COPY_ROOT/.env.worktree"
leak_out="$(cd / && GOOSAR_RELEASE_PRINT_ENV=1 bash "$SCRIPT" v9.9.9 2>&1; env | grep -E '^(RESEND_API_KEY|SMTP_HOST)=' || true)"
if printf '%s' "$leak_out" | grep -q "leak-me\|smtp.leak.invalid"; then
  echo "FAIL: env-file-does-not-leak-non-database-vars — the release exported more than DATABASE_URL"
  FAIL=$((FAIL + 1))
else
  echo "ok: env-file-does-not-leak-non-database-vars"
  PASS=$((PASS + 1))
fi

# The header is printed by --help through a `set -euo` sentinel, not a line
# range: its last paragraph must survive every future header edit.
if bash "$SCRIPT" --help | grep -q "test-e2e.sh re-sources the same env file"; then
  echo "ok: help prints the whole header"
  PASS=$((PASS + 1))
else
  echo "FAIL: help is truncated before the end of the header block"
  FAIL=$((FAIL + 1))
fi

# --- SBOM + vulnerability scanning (issue #385) ------------------------------
# The SBOM/scan step lives in scripts/sbom-scan.sh so it can be exercised
# without docker, gh, goreleaser or a tag: release-local.sh calls it by name
# and turns its summary into ledger lines. Both paths are covered here — tools
# present (fake executables on PATH) and tools absent — because the whole point
# of #385 is that a missing scanner degrades to a documented warning instead of
# either failing the release or vanishing silently.
SBOM_SCRIPT="$ROOT_DIR/scripts/sbom-scan.sh"
SBOM_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP" "$SBOM_TMP"' EXIT

make_fake_tools() { # make_fake_tools <bin-dir> <trivy-severity-to-emit>
  local bin="$1" severity="$2"
  mkdir -p "$bin"
  cat > "$bin/syft" <<'FAKE'
#!/bin/sh
# Fake syft: honours the -o cyclonedx-json=<path> form only.
for arg in "$@"; do
  case "$arg" in
    -o) next=out ;;
    cyclonedx-json=*) printf '{"bomFormat":"CycloneDX","components":[]}\n' > "${arg#cyclonedx-json=}" ;;
  esac
done
exit 0
FAKE
  cat > "$bin/trivy" <<FAKE
#!/bin/sh
# Fake trivy: writes a report at --output and emits $severity findings.
out=""
prev=""
for arg in "\$@"; do
  [ "\$prev" = "--output" ] && out="\$arg"
  prev="\$arg"
done
[ -n "\$out" ] || exit 0
if [ -n "$severity" ]; then
  printf '{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-2026-0001","Severity":"$severity"}]}]}\n' > "\$out"
else
  printf '{"Results":[{"Vulnerabilities":[]}]}\n' > "\$out"
fi
exit 0
FAKE
  chmod +x "$bin/syft" "$bin/trivy"
}

expect_line() { # expect_line <name> <output> <expected-line>
  if printf '%s\n' "$2" | grep -qx -- "$3"; then
    echo "ok: $1"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $1 — expected line '$3'; got:"
    printf '%s\n' "$2" | sed 's/^/    /'
    FAIL=$((FAIL + 1))
  fi
}

# Path 1: syft and trivy present, nothing above the threshold.
CLEAN_BIN="$SBOM_TMP/clean-bin"
make_fake_tools "$CLEAN_BIN" ""
CLEAN_OUT="$SBOM_TMP/clean-out"
mkdir -p "$SBOM_TMP/src"
clean_rc=0
clean_out="$(PATH="$CLEAN_BIN:$PATH" bash "$SBOM_SCRIPT" --tag v1.0.0 --out "$CLEAN_OUT" \
  --image ghcr.io/adanman/goosar-backend:v1.0.0 \
  --image ghcr.io/adanman/goosar-web:v1.0.0 \
  --source-dir "$SBOM_TMP/src" 2>&1)" || clean_rc=$?
expect_line "sbom-tools-present-status" "$clean_out" "SBOM_STATUS=ok"
expect_line "scan-tools-present-status" "$clean_out" "SCAN_STATUS=ok"
expect_line "scan-tools-present-no-findings" "$clean_out" "SCAN_FINDINGS=0"
if [ "$clean_rc" -ne 0 ]; then
  echo "FAIL: sbom-scan-exit-zero — the step must never fail the release itself (rc=$clean_rc)"
  FAIL=$((FAIL + 1))
else
  echo "ok: sbom-scan-exit-zero"
  PASS=$((PASS + 1))
fi
# One CycloneDX SBOM per image plus one for the repo source.
sbom_count="$(ls "$CLEAN_OUT"/*.cyclonedx.json 2>/dev/null | wc -l | tr -d '[:space:]')"
if [ "$sbom_count" = "3" ]; then
  echo "ok: sbom-file-per-image-and-source"
  PASS=$((PASS + 1))
else
  echo "FAIL: sbom-file-per-image-and-source — expected 3 CycloneDX files, got $sbom_count"
  FAIL=$((FAIL + 1))
fi

# Path 2: tools present, a CRITICAL finding. The gate marks the ledger line,
# it does not abort — the operator decides whether to ship with an exception.
DIRTY_BIN="$SBOM_TMP/dirty-bin"
make_fake_tools "$DIRTY_BIN" "CRITICAL"
dirty_rc=0
dirty_out="$(PATH="$DIRTY_BIN:$PATH" bash "$SBOM_SCRIPT" --tag v1.0.0 --out "$SBOM_TMP/dirty-out" \
  --image ghcr.io/adanman/goosar-backend:v1.0.0 2>&1)" || dirty_rc=$?
expect_line "scan-critical-status" "$dirty_out" "SCAN_STATUS=findings"
if printf '%s\n' "$dirty_out" | grep -q '^SCAN_FINDINGS=[1-9]'; then
  echo "ok: scan-critical-counted"
  PASS=$((PASS + 1))
else
  echo "FAIL: scan-critical-counted — expected a non-zero SCAN_FINDINGS"
  FAIL=$((FAIL + 1))
fi
if [ "$dirty_rc" -ne 0 ]; then
  echo "FAIL: scan-critical-does-not-abort — findings must not fail the step (rc=$dirty_rc)"
  FAIL=$((FAIL + 1))
else
  echo "ok: scan-critical-does-not-abort"
  PASS=$((PASS + 1))
fi

# Path 3: neither tool installed. A stripped PATH keeps this hermetic; if the
# machine really does ship syft/trivy in /usr/bin the case is not reachable.
if PATH=/usr/bin:/bin command -v syft >/dev/null 2>&1 || PATH=/usr/bin:/bin command -v trivy >/dev/null 2>&1; then
  echo "skip: sbom-tools-missing (syft/trivy live in /usr/bin on this machine)"
else
  missing_rc=0
  missing_out="$(PATH=/usr/bin:/bin bash "$SBOM_SCRIPT" --tag v1.0.0 --out "$SBOM_TMP/missing-out" \
    --image ghcr.io/adanman/goosar-backend:v1.0.0 2>&1)" || missing_rc=$?
  expect_line "sbom-tools-missing-status" "$missing_out" "SBOM_STATUS=skipped-tool-missing"
  expect_line "scan-tools-missing-status" "$missing_out" "SCAN_STATUS=skipped-tool-missing"
  if [ "$missing_rc" -ne 0 ]; then
    echo "FAIL: sbom-tools-missing-exit-zero — a missing tool must not fail the release (rc=$missing_rc)"
    FAIL=$((FAIL + 1))
  else
    echo "ok: sbom-tools-missing-exit-zero"
    PASS=$((PASS + 1))
  fi
  # A silent skip is the failure mode #385 exists to prevent: the operator has
  # to be told what to install.
  for hint in "https://github.com/anchore/syft" "https://trivy.dev/latest/docs/target/container_image/"; do
    if printf '%s' "$missing_out" | grep -qF "$hint"; then
      echo "ok: install-hint $hint"
      PASS=$((PASS + 1))
    else
      echo "FAIL: install-hint $hint — a missing tool was skipped without telling the operator how to install it"
      FAIL=$((FAIL + 1))
    fi
  done
fi

# The ignorefile must come from the tree actually being scanned
# (--source-dir), not from wherever this script happens to live: an
# operator whose own checkout trails the tag under release must not have
# trivy silently filter findings against a stale .trivyignore.yaml while
# scanning the tag's newer, already-updated one (#520 follow-up).
IGNOREFILE_BIN="$SBOM_TMP/ignorefile-bin"
mkdir -p "$IGNOREFILE_BIN" "$SBOM_TMP/ignorefile-src"
IGNOREFILE_SEEN="$SBOM_TMP/ignorefile-seen"
: > "$IGNOREFILE_SEEN"
printf 'stale-repo-root-marker\n' > "$ROOT_DIR/.trivyignore.yaml.test-marker-do-not-commit"
printf 'source-dir-marker\n' > "$SBOM_TMP/ignorefile-src/.trivyignore.yaml"
cat > "$IGNOREFILE_BIN/syft" <<'FAKE'
#!/bin/sh
exit 0
FAKE
cat > "$IGNOREFILE_BIN/trivy" <<FAKE
#!/bin/sh
out=""
ignorefile=""
prev=""
for arg in "\$@"; do
  [ "\$prev" = "--output" ] && out="\$arg"
  [ "\$prev" = "--ignorefile" ] && ignorefile="\$arg"
  prev="\$arg"
done
printf '%s\n' "\$ignorefile" >> "$IGNOREFILE_SEEN"
[ -n "\$out" ] && printf '{"Results":[{"Vulnerabilities":[]}]}\n' > "\$out"
exit 0
FAKE
chmod +x "$IGNOREFILE_BIN/syft" "$IGNOREFILE_BIN/trivy"
PATH="$IGNOREFILE_BIN:$PATH" bash "$SBOM_SCRIPT" --tag v1.0.0 --out "$SBOM_TMP/ignorefile-out" \
  --image ghcr.io/adanman/goosar-backend:v1.0.0 \
  --source-dir "$SBOM_TMP/ignorefile-src" >/dev/null 2>&1 || true
rm -f "$ROOT_DIR/.trivyignore.yaml.test-marker-do-not-commit"
if grep -qx "$SBOM_TMP/ignorefile-src/.trivyignore.yaml" "$IGNOREFILE_SEEN"; then
  echo "ok: sbom-scan-ignorefile-from-source-dir"
  PASS=$((PASS + 1))
else
  echo "FAIL: sbom-scan-ignorefile-from-source-dir — trivy was invoked with --ignorefile $(cat "$IGNOREFILE_SEEN"), expected $SBOM_TMP/ignorefile-src/.trivyignore.yaml"
  FAIL=$((FAIL + 1))
fi

# release-local.sh must actually call the step and document it.
if grep -q "sbom-scan.sh" "$SCRIPT"; then
  echo "ok: release-local-calls-sbom-scan"
  PASS=$((PASS + 1))
else
  echo "FAIL: release-local-calls-sbom-scan — the SBOM step is not wired into the release"
  FAIL=$((FAIL + 1))
fi
if bash "$SCRIPT" --help | grep -q -- "--no-sbom"; then
  echo "ok: help documents --no-sbom"
  PASS=$((PASS + 1))
else
  echo "FAIL: help documents --no-sbom"
  FAIL=$((FAIL + 1))
fi
expect_fail "no-sbom-flag-recognized" "unexpected argument" --no-sbom v0.6.6 v0.6.7

# --- ghcr login must not clobber a working session ---------------------------
# The login step used to be unconditional and always used `gh auth token`.
# That token is an OAuth-app token, which GHCR refuses for package writes on
# some accounts, so a release could replace the operator's working PAT session
# on the way in and then fail every push AFTER building both images.
if grep -q 'GOOSAR_GHCR_TOKEN' "$SCRIPT" &&
   grep -q 'keeping the existing docker login' "$SCRIPT"; then
  echo "ok: ghcr-login-respects-existing-session"
  PASS=$((PASS + 1))
else
  echo "FAIL: ghcr-login-respects-existing-session — the release still overwrites whatever ghcr.io login the operator made"
  FAIL=$((FAIL + 1))
fi

# --- helm must reuse the token that pushed the images ------------------------
# v0.10.1 pushed both images on the operator's PAT and then lost the chart to
# `403 Forbidden` on the blob HEAD: run_helm did its own `helm registry login`
# with `gh auth token`, and helm shares ~/.docker/config.json with docker, so
# the bad token replaced the good session mid-run.
if grep -q 'helm_registry_login' "$SCRIPT" &&
   ! grep -q 'GITHUB_TOKEN_VALUE" | helm registry login' "$SCRIPT"; then
  echo "ok: helm-oci-push-reuses-the-working-ghcr-token"
  PASS=$((PASS + 1))
else
  echo "FAIL: helm-oci-push-reuses-the-working-ghcr-token — run_helm still logs in with its own token"
  FAIL=$((FAIL + 1))
fi

# --- worktree hygiene (#594) --------------------------------------------------
# v0.10.1 shipped five `-dirty` desktop builds: dependency install and the e2e
# gate leave a regenerated tracked file and two untracked ones in the tag
# tree, and `git describe --dirty` stamped every artifact. The helper is
# lifted out by its fence and run against a real throwaway repository.
HYG_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP" "$SBOM_TMP" "$GATE_TMP" "$HYG_TMP"' EXIT
awk '/# >>> worktree-hygiene/ { on = 1; next } /# <<< worktree-hygiene/ { on = 0 } on { print }' "$SCRIPT" > "$HYG_TMP/hygiene.sh"
if grep -q 'worktree_hygiene()' "$HYG_TMP/hygiene.sh"; then
  echo "ok: the worktree-hygiene block is fenced for extraction"; PASS=$((PASS + 1))
else
  echo "FAIL: the worktree-hygiene block is fenced for extraction (markers moved?)"; FAIL=$((FAIL + 1))
fi
(
  set -e
  cd "$HYG_TMP"
  git init -q repo && cd repo
  git config user.email t@example.invalid && git config user.name t
  mkdir -p apps/web && printf '// generated\n' > apps/web/next-env.d.ts && printf 'x\n' > README.md
  git add -A && git commit -qm init && git tag v9.9.9
  git worktree add --detach ../tree v9.9.9 >/dev/null 2>&1
  cd ../tree
  printf '// regenerated by next\n' > apps/web/next-env.d.ts   # tracked, modified
  printf 'agent notes\n' > apps/web/AGENTS.md                    # untracked
  git describe --tags --dirty > ../before.txt
  bash -c 'warn() { printf "WARN|%s\n" "$*" >> "$LEDGER"; }; . "$1"; worktree_hygiene "$PWD"' _ "$HYG_TMP/hygiene.sh" 2>/dev/null
  git describe --tags --dirty > ../after.txt
) && hyg_rc=0 || hyg_rc=$?
if [ "$hyg_rc" -eq 0 ] && grep -q -- '-dirty$' "$HYG_TMP/before.txt" && ! grep -q -- '-dirty' "$HYG_TMP/after.txt"; then
  echo "ok: worktree_hygiene turns a describe of v9.9.9-dirty back into v9.9.9"; PASS=$((PASS + 1))
else
  echo "FAIL: worktree_hygiene — rc=$hyg_rc before=$(cat "$HYG_TMP/before.txt" 2>/dev/null) after=$(cat "$HYG_TMP/after.txt" 2>/dev/null)"; FAIL=$((FAIL + 1))
fi
# The untracked file is gone from the throwaway tree, and NOTHING landed in the
# operator's repository: info/exclude is shared between a repo and its
# worktrees, so writing there would have left a mark in a checkout the release
# does not own.
if [ ! -e "$HYG_TMP/tree/apps/web/AGENTS.md" ] && ! grep -q 'AGENTS.md' "$HYG_TMP/repo/.git/info/exclude" 2>/dev/null; then
  echo "ok: the untracked file is removed from the tag tree and the operator's repo is untouched"; PASS=$((PASS + 1))
else
  echo "FAIL: untracked handling — file present: $([ -e "$HYG_TMP/tree/apps/web/AGENTS.md" ] && echo yes || echo no); repo exclude: $(cat "$HYG_TMP/repo/.git/info/exclude" 2>/dev/null | tr '\n' ' ')"; FAIL=$((FAIL + 1))
fi
# The sweep: any artifact whose name carries -dirty is MISSING by name.
if grep -q 'record_missing "dirty artifacts"' "$SCRIPT" &&
   grep -q "name '\*-dirty\*'" "$SCRIPT"; then
  echo "ok: dirty artifacts in the kit directories are MISSING in the ledger"; PASS=$((PASS + 1))
else
  echo "FAIL: dirty artifacts sweep is not in the ledger"; FAIL=$((FAIL + 1))
fi

# --- hermes-dist-stage (T-27, #655) -----------------------------------------
# The web image's build context is the TAG worktree, not this checkout, so a
# resolved GOOSAR_HERMES_DIST_DIR (possibly a sibling of this checkout) has
# to be copied INTO that worktree before the build-arg can point at it. The
# helper is lifted out by its fence and run standalone against fake dirs.
HERMES_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP" "$SBOM_TMP" "$GATE_TMP" "$HYG_TMP" "$HERMES_TMP"' EXIT
awk '/# >>> hermes-dist-stage/ { on = 1; next } /# <<< hermes-dist-stage/ { on = 0 } on { print }' "$SCRIPT" > "$HERMES_TMP/stage.sh"
if grep -q 'stage_hermes_dist_for_web()' "$HERMES_TMP/stage.sh"; then
  echo "ok: the hermes-dist-stage block is fenced for extraction"; PASS=$((PASS + 1))
else
  echo "FAIL: the hermes-dist-stage block is fenced for extraction (markers moved?)"; FAIL=$((FAIL + 1))
fi

mkdir -p "$HERMES_TMP/dist/hermes-0.14.0-darwin-arm64" "$HERMES_TMP/wt"
echo bin > "$HERMES_TMP/dist/hermes-0.14.0-darwin-arm64/bin"
stage_out="$(
  record_ok()      { printf 'OK|%s|%s\n' "$1" "$2"; }
  record_skipped() { printf 'SKIP|%s|%s\n' "$1" "$2"; }
  GOOSAR_HERMES_DIST_DIR="$HERMES_TMP/dist"
  . "$HERMES_TMP/stage.sh"
  stage_hermes_dist_for_web "$HERMES_TMP/wt"
  echo "ARG=$HERMES_DIST_ARG"
)"
if printf '%s' "$stage_out" | grep -q '^OK|agent artifacts (web /cli)|staged: hermes-0.14.0-darwin-arm64$' \
   && printf '%s' "$stage_out" | grep -q '^ARG=offline/agent-dist$' \
   && [ -e "$HERMES_TMP/wt/offline/agent-dist/hermes-0.14.0-darwin-arm64/bin" ]; then
  echo "ok: stage_hermes_dist_for_web copies dist entries into the worktree and records ok"; PASS=$((PASS + 1))
else
  echo "FAIL: stage_hermes_dist_for_web with a real dist — $stage_out"; FAIL=$((FAIL + 1))
fi

stage_out2="$(
  record_ok()      { printf 'OK|%s|%s\n' "$1" "$2"; }
  record_skipped() { printf 'SKIP|%s|%s\n' "$1" "$2"; }
  unset GOOSAR_HERMES_DIST_DIR
  . "$HERMES_TMP/stage.sh"
  stage_hermes_dist_for_web "$HERMES_TMP/wt-none"
  echo "ARG=$HERMES_DIST_ARG"
)"
if printf '%s' "$stage_out2" | grep -q '^SKIP|agent artifacts (web /cli)|dist not found' \
   && printf '%s' "$stage_out2" | grep -q '^ARG=$'; then
  echo "ok: stage_hermes_dist_for_web is a no-op (skipped, not MISSING) with no dist configured"; PASS=$((PASS + 1))
else
  echo "FAIL: stage_hermes_dist_for_web with no dist — $stage_out2"; FAIL=$((FAIL + 1))
fi

if grep -q 'WEB_ARGS+=(--build-arg "GOOSAR_HERMES_DIST_DIR=$HERMES_DIST_ARG")' "$SCRIPT"; then
  echo "ok: the resolved build-arg is wired into WEB_ARGS"; PASS=$((PASS + 1))
else
  echo "FAIL: the resolved build-arg is not wired into WEB_ARGS"; FAIL=$((FAIL + 1))
fi

# --- delivery manifest (issue #437) ------------------------------------------
# The acceptance act needs one list of what was handed over. It is generated by
# offline/offline-manifest.sh (mode `delivery`) and wired here as a ledger line.
if grep -q "offline-manifest.sh" "$SCRIPT"; then
  echo "ok: release-local-generates-delivery-manifest"
  PASS=$((PASS + 1))
else
  echo "FAIL: release-local-generates-delivery-manifest — DELIVERY-MANIFEST.md is not produced by the release"
  FAIL=$((FAIL + 1))
fi

# --- e2e gate preflight (issue #450) -----------------------------------------
# v0.9.0 shipped after a gate that ran the operator checkout (20 commits behind
# the tag) and, on every failure, left a backend and a `next dev` holding the
# gate's ports. Both are guarded here through GOOSAR_RELEASE_GATE_PREFLIGHT,
# which runs only the port + tag-tree checks — no docker, gh or network.
GATE_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP" "$SBOM_TMP" "$GATE_TMP" "$HYG_TMP"' EXIT
GATE_BIN="$GATE_TMP/bin"
mkdir -p "$GATE_BIN"
# lsof/ps are faked so "the port is busy" is a decision of the test, not of
# whatever happens to listen on this machine.
cat > "$GATE_BIN/lsof" <<'FAKE'
#!/bin/sh
[ -n "${FAKE_PORT_OWNER_PID:-}" ] || exit 1
printf '%s\n' "$FAKE_PORT_OWNER_PID"
FAKE
cat > "$GATE_BIN/ps" <<'FAKE'
#!/bin/sh
printf 'fake-goosar-server\n'
FAKE
chmod +x "$GATE_BIN/lsof" "$GATE_BIN/ps"

GATE_ENV="$GATE_TMP/gate.env"
printf 'BACKEND_PORT=59181\nFRONTEND_PORT=59182\n' > "$GATE_ENV"

# A real (tiny) git repo: the tag-tree check must compare an actual HEAD.
GATE_TREE="$GATE_TMP/tree"
mkdir -p "$GATE_TREE"
git -C "$GATE_TREE" init -q
git -C "$GATE_TREE" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
GATE_HEAD="$(git -C "$GATE_TREE" rev-parse HEAD)"

run_preflight() { # run_preflight <extra env...>
  env PATH="$GATE_BIN:$PATH" GOOSAR_RELEASE_GATE_PREFLIGHT=1 \
    GOOSAR_E2E_ENV_FILE="$GATE_ENV" "$@" bash "$SCRIPT" v0.0.1 2>&1
}

expect_gate() { # expect_gate <name> <want-substring> <extra env...>
  local name="$1" want="$2"
  shift 2
  local out
  out="$(run_preflight "$@")" || true
  if printf '%s' "$out" | grep -qF "$want"; then
    echo "ok: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name — expected '$want'; got: $out"
    FAIL=$((FAIL + 1))
  fi
}

# Ports free + HEAD == SHA: the only combination that may proceed.
expect_gate "gate-preflight-passes-on-matching-tree" "GATE_PREFLIGHT=ok" \
  WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"

# The gate tree is at a different commit — exactly the v0.9.0 failure.
expect_gate "gate-tree-mismatch-is-refused" "not the tag commit" \
  WORKTREE="$GATE_TREE" SHA=0000000000000000000000000000000000000000
# ...and the refusal is a hard failure, not a warning.
mismatch_rc=0
run_preflight WORKTREE="$GATE_TREE" SHA=0000000000000000000000000000000000000000 >/dev/null 2>&1 || mismatch_rc=$?
if [ "$mismatch_rc" -ne 0 ]; then
  echo "ok: gate-tree-mismatch-exits-non-zero"
  PASS=$((PASS + 1))
else
  echo "FAIL: gate-tree-mismatch-exits-non-zero — a stale gate tree must stop the release"
  FAIL=$((FAIL + 1))
fi

# A busy port must name the PID that owns it, instead of dying deep inside
# test-e2e.sh with "already running".
expect_gate "busy-port-names-the-owning-pid" "PID 4242" \
  FAKE_PORT_OWNER_PID=4242 WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"
expect_gate "busy-port-names-the-port" "port 59181" \
  FAKE_PORT_OWNER_PID=4242 WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"

# Ports come from the gate's env file, not from a hardcoded default.
expect_gate "gate-ports-read-from-env-file" "backend 59181, frontend 59182" \
  WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"
# ...and fall back to the local-env.sh defaults when the file says nothing.
printf '# empty\n' > "$GATE_TMP/empty.env"
expect_gate "gate-ports-default-to-8081-3001" "backend 8081, frontend 3001" \
  GOOSAR_E2E_ENV_FILE="$GATE_TMP/empty.env" WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"
# With no env file at all those defaults are the operator's OWN dev ports, so
# the gate must name the missing file instead of blaming a stack it did not
# start (and instead of test-e2e.sh hunting for an .env inside the tag tree).
expect_gate "gate-refuses-without-an-env-file" "no env file for the e2e gate" \
  GOOSAR_E2E_ENV_FILE="$GATE_TMP/does-not-exist.env" WORKTREE="$GATE_TREE" SHA="$GATE_HEAD"

# The tag tree gets its own node_modules: without this the gate runs the tag's
# specs against the operator checkout's dependencies.
if grep -q 'ensure_worktree_deps' "$SCRIPT" && grep -q 'cd "$WORKTREE" && pnpm install --frozen-lockfile' "$SCRIPT"; then
  echo "ok: gate-installs-dependencies-in-the-tag-tree"
  PASS=$((PASS + 1))
else
  echo "FAIL: gate-installs-dependencies-in-the-tag-tree"
  FAIL=$((FAIL + 1))
fi

# The minimal gate env is documented where the operator reads it (--help).
for doc in "DATABASE_URL" "BACKEND_PORT" "FRONTEND_ORIGIN" "GOOSAR_PUBLIC_URL" "TURBO_ENV_MODE=loose" "umask 022" "NEXT_PUBLIC_API_URL"; do
  if bash "$SCRIPT" --help | grep -qF "$doc"; then
    echo "ok: help documents $doc"
    PASS=$((PASS + 1))
  else
    echo "FAIL: help documents $doc — the gate env is undocumented again"
    FAIL=$((FAIL + 1))
  fi
done

# --- desktop platform matrix (issue #401, ADR-0009) --------------------------
# The release builds five desktop platforms, and the operator has to be able to
# see WHICH before committing an hour to a run. GOOSAR_RELEASE_PRINT_PLATFORMS
# resolves the matrix and exits before any tool/daemon/network check, so every
# case below is hermetic — and it doubles as the plan mode for a host that
# cannot build every target.
platforms_out() { # platforms_out <flags...>
  env GOOSAR_RELEASE_PRINT_PLATFORMS=1 bash "$SCRIPT" "$@" v0.0.1 2>/dev/null || true
}

expect_matrix() { # expect_matrix <name> <expected keys, space separated> <flags...>
  local name="$1" want="$2"
  shift 2
  local got
  got="$(platforms_out "$@" | sed -n 's/^PLATFORM=\([^ ]*\).*/\1/p' | tr '\n' ' ')"
  got="${got% }"
  if [ "$got" = "$want" ]; then
    echo "ok: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name — expected '$want'; got '$got'"
    FAIL=$((FAIL + 1))
  fi
}

# No platform flag: exactly the platform this script built before #401. A
# release that silently started building five targets would surprise every
# existing runbook.
expect_matrix "matrix-defaults-to-mac-arm64" "mac-arm64"
expect_matrix "matrix-single-flag" "linux-x64" --linux-x64
expect_matrix "matrix-accumulates-in-order" "mac-x64 linux-arm64" --mac-x64 --linux-arm64
expect_matrix "matrix-all-platforms" \
  "mac-arm64 mac-x64 win-x64 linux-x64 linux-arm64" --all-platforms
# Repeats are a typo, not a request to build the same target twice.
expect_matrix "matrix-deduplicates" "linux-x64 mac-arm64" \
  --linux-x64 --mac-arm64 --linux-x64
expect_matrix "matrix-all-platforms-plus-flag-still-deduplicates" \
  "mac-arm64 mac-x64 win-x64 linux-x64 linux-arm64" --all-platforms --win-x64

expect_spec() { # expect_spec <name> <key> <expected spec>
  local name="$1" key="$2" want="$3" got
  got="$(platforms_out --all-platforms | sed -n "s/^PLATFORM=$key spec=\(.*\) host=.*/\1/p")"
  if [ "$got" = "$want" ]; then
    echo "ok: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name — expected '$want'; got '$got'"
    FAIL=$((FAIL + 1))
  fi
}

# The row a stage is driven from: package.mjs flags, the GOOS/GOARCH the
# CLI/daemon is cross-compiled for, and the label the ledger prints.
expect_spec "spec-mac-arm64" mac-arm64 "--mac --arm64|darwin|arm64|macOS arm64"
expect_spec "spec-mac-x64" mac-x64 "--mac --x64|darwin|amd64|macOS x64"
expect_spec "spec-win-x64" win-x64 "--win --x64|windows|amd64|Windows x64"
expect_spec "spec-linux-x64" linux-x64 "--linux --x64|linux|amd64|Linux x64"
expect_spec "spec-linux-arm64" linux-arm64 "--linux --arm64|linux|arm64|Linux arm64"

# The plan mode names what this host cannot build, per platform, rather than
# discovering it twenty minutes into an electron-builder run.
#
# Windows and Linux cross-build from macOS. electron-builder 26 carries its own
# makensis, so the NSIS installer is produced on a mac with no wine installed —
# checked here because a host gate written from the older "NSIS needs wine"
# advice would refuse a target this pipeline actually builds.
if [ "$(uname -s)" = "Darwin" ]; then
  for cross in --win-x64 --linux-x64 --linux-arm64; do
    if platforms_out "$cross" | grep -q "host=ok"; then
      echo "ok: plan-mode-allows-cross-build $cross"
      PASS=$((PASS + 1))
    else
      echo "FAIL: plan-mode-allows-cross-build $cross — this host does build it"
      FAIL=$((FAIL + 1))
    fi
  done
else
  echo "skip: plan-mode-allows-cross-build (not a macOS host)"
fi

# Every platform flag is a recognized flag: paired with the SAME "unexpected
# argument" gate the extra-positional test uses, so the case stays inside the
# pure argument-parsing loop.
for flag in --mac-arm64 --mac-x64 --win-x64 --linux-x64 --linux-arm64 --all-platforms; do
  expect_fail "platform-flag-recognized $flag" "unexpected argument" "$flag" v0.6.6 v0.6.7
  if bash "$SCRIPT" --help | grep -q -- "$flag"; then
    echo "ok: help documents $flag"
    PASS=$((PASS + 1))
  else
    echo "FAIL: help documents $flag"
    FAIL=$((FAIL + 1))
  fi
done

# NON-ABORT SEMANTICS. A failing platform must record MISSING and let the next
# one run; the whole point of the matrix is that four working builds are not
# thrown away by the fifth. A `fail` (which exits 1) inside the desktop section
# would undo that silently, so the section is checked for one.
DESKTOP_SECTION="$(sed -n '/^# --- 4\. desktop/,/^# --- 5\. helm/p' "$SCRIPT")"
if printf '%s' "$DESKTOP_SECTION" | grep -q 'record_missing "desktop \$platform_key"' \
   && printf '%s' "$DESKTOP_SECTION" | grep -q 'record_missing "cli \$platform_key"'; then
  echo "ok: per-platform failures are recorded in the ledger"
  PASS=$((PASS + 1))
else
  echo "FAIL: per-platform failures are recorded in the ledger"
  FAIL=$((FAIL + 1))
fi
if printf '%s' "$DESKTOP_SECTION" | grep -qE '^\s*fail '; then
  echo "FAIL: desktop-stage-does-not-abort-the-run — a `fail` call exits the whole release"
  FAIL=$((FAIL + 1))
else
  echo "ok: desktop-stage-does-not-abort-the-run"
  PASS=$((PASS + 1))
fi

# A desktop package without the bundled CLI is a package whose daemon never
# starts: the runtime repair download hits a private GitHub repo (#565).
# bundle-cli.mjs used to skip quietly when `go`/the binary was absent and the
# build still succeeded. The release must (a) tell the bundler it is a release
# and (b) refuse to ship a stage whose staging dir has no binary.
if printf '%s' "$DESKTOP_SECTION" | grep -q 'GOOSAR_RELEASE_BUILD=1' \
   && printf '%s' "$DESKTOP_SECTION" | grep -q 'resources/bin'; then
  echo "ok: desktop-stage-refuses-a-package-without-the-cli"
  PASS=$((PASS + 1))
else
  echo "FAIL: desktop-stage-refuses-a-package-without-the-cli — a DMG can still ship without goosar"
  FAIL=$((FAIL + 1))
fi

# --- electron-builder targets vs the platforms the script drives (#401) ------
# Static, so it runs anywhere: the script drives mac/win/linux, and
# electron-builder.yml must declare exactly the targets those stages expect to
# collect. A target added there and not collected here is an artifact that
# never reaches the delivery; one removed there is a stage that produces
# nothing and reports MISSING for no visible reason.
EB_YML="$ROOT_DIR/apps/desktop/electron-builder.yml"
eb_targets() { # eb_targets <top-level key>
  awk -v want="$1" '
    /^[a-z]+:/ { top = $1; sub(":", "", top); intarget = 0 }
    top == want && /^  target:/ { intarget = 1; next }
    intarget && /^    - / { print $2; next }
    intarget && !/^    - / { intarget = 0 }
  ' "$EB_YML" | tr '\n' ' ' | sed 's/ $//'
}

expect_targets() { # expect_targets <platform> <expected>
  local got
  got="$(eb_targets "$1")"
  if [ "$got" = "$2" ]; then
    echo "ok: electron-builder $1 targets = $2"
    PASS=$((PASS + 1))
  else
    echo "FAIL: electron-builder $1 targets — expected '$2'; got '$got'"
    FAIL=$((FAIL + 1))
  fi
}

# mac: `zip` only — the DMG is built and verified by
# apps/desktop/scripts/build-mac-dmg.mjs after electron-builder, because
# electron-builder under-sized the image and shipped a truncated app (#186).
expect_targets mac "zip"
expect_targets win "nsis"
expect_targets linux "AppImage deb rpm"

# ...and every extension those targets produce is collected into the offline
# kit, or it is simply not delivered.
for ext in dmg zip exe AppImage deb rpm; do
  if grep -q "^DESKTOP_ARTIFACT_EXTS=.*$ext" "$SCRIPT"; then
    echo "ok: the release collects .$ext"
    PASS=$((PASS + 1))
  else
    echo "FAIL: the release collects .$ext — electron-builder produces it and nothing picks it up"
    FAIL=$((FAIL + 1))
  fi
done

# --- delivery-manifest rows for every platform's artifacts (#401) ------------
# The acceptance act is signed against DELIVERY-MANIFEST.md. A Linux .deb that
# offline-manifest.sh does not classify is an artifact the customer received
# and nobody signed for.
MANIFEST_TMP="$(mktemp -d)"
trap 'rm -rf "$ENV_TMP" "$SBOM_TMP" "$GATE_TMP" "$MANIFEST_TMP"' EXIT
mkdir -p "$MANIFEST_TMP/kit-desktop" "$MANIFEST_TMP/kit-cli"
for artifact in \
  kit-desktop/goosar-desktop-1.0.0-mac-arm64.dmg \
  kit-desktop/goosar-desktop-1.0.0-mac-x64.dmg \
  kit-desktop/goosar-desktop-1.0.0-windows-x64.exe \
  kit-desktop/goosar-desktop-1.0.0-linux-x64.AppImage \
  kit-desktop/goosar-desktop-1.0.0-linux-arm64.deb \
  kit-cli/goosar-cli-1.0.0-linux-arm64.tar.gz; do
  printf 'bytes\n' > "$MANIFEST_TMP/$artifact"
done
manifest_out="$(bash "$ROOT_DIR/offline/offline-manifest.sh" generate --tag v1.0.0 \
  --dir "$MANIFEST_TMP" --out "$MANIFEST_TMP/offline-manifest.txt" 2>&1 || true)"
for row in \
  "kind=desktop file=kit-desktop/goosar-desktop-1.0.0-mac-arm64.dmg" \
  "kind=desktop file=kit-desktop/goosar-desktop-1.0.0-mac-x64.dmg" \
  "kind=desktop file=kit-desktop/goosar-desktop-1.0.0-windows-x64.exe" \
  "kind=desktop file=kit-desktop/goosar-desktop-1.0.0-linux-x64.AppImage" \
  "kind=desktop file=kit-desktop/goosar-desktop-1.0.0-linux-arm64.deb" \
  "kind=cli file=kit-cli/goosar-cli-1.0.0-linux-arm64.tar.gz"; do
  if grep -qF "$row" "$MANIFEST_TMP/offline-manifest.txt" 2>/dev/null; then
    echo "ok: manifest row ${row#kind=* file=}"
    PASS=$((PASS + 1))
  else
    echo "FAIL: manifest row '$row' missing; generate said: $manifest_out"
    FAIL=$((FAIL + 1))
  fi
done

# --- CLI ldflags parity (issue #525) -----------------------------------------
# The CLI a self-hosted Goosar serves under /cli/* is cross-compiled by the
# `cli` stage of Dockerfile.web, NOT by goreleaser or by the kit-cli build in
# release-local.sh. It must carry the same -X stamps as those, otherwise
# `goosar --version` prints "commit: unknown, built: unknown" for everyone who
# installed through install.sh.

record() { # record <ok|no> <name> [why-it-failed]
  if [ "$1" = ok ]; then
    echo "ok: $2"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $2 — $3"
    FAIL=$((FAIL + 1))
  fi
}

# The -ldflags string that stamps ./cmd/goosar in a given file. Anchored on
# main.version so an unrelated `-ldflags "-s -w"` earlier in the file cannot
# make this compare the wrong pair.
goosar_ldflags() { grep -o -- '-ldflags "[^"]*main\.version[^"]*"' "$1" | head -1 | sed 's/^-ldflags "//; s/"$//'; }
# -X keys only, case preserved so a camelCase rename on one side is visible.
x_keys() { printf '%s\n' "$1" | grep -o 'main\.[A-Za-z]*' | sort -u | tr '\n' ' '; }

web_ldflags="$(goosar_ldflags "$ROOT_DIR/Dockerfile.web")"
kit_ldflags="$(goosar_ldflags "$ROOT_DIR/scripts/release-local.sh")"
web_keys="$(x_keys "$web_ldflags")"
kit_keys="$(x_keys "$kit_ldflags")"
# goreleaser writes its stamps as a YAML list, not one -ldflags string.
gor_keys="$(x_keys "$(grep -o -- '-X main\.[A-Za-z]*' "$ROOT_DIR/.goreleaser.yml")")"

if [ -n "$kit_keys" ] && [ "$web_keys" = "$kit_keys" ]; then
  record ok "cli-ldflags-keys-match-kit" ""
else
  record no "cli-ldflags-keys-match-kit" "Dockerfile.web stamps [$web_keys], release-local.sh stamps [$kit_keys]"
fi
if [ -n "$gor_keys" ] && [ "$web_keys" = "$gor_keys" ]; then
  record ok "cli-ldflags-keys-match-goreleaser" ""
else
  record no "cli-ldflags-keys-match-goreleaser" "Dockerfile.web stamps [$web_keys], .goreleaser.yml stamps [$gor_keys]"
fi

# The stage that runs that build must declare the ARGs it interpolates. Without
# them Docker expands ${COMMIT}/${DATE} to the empty string and silently drops
# the matching --build-arg, which is worse than the "unknown" default.
missing_args=""
for arg in COMMIT DATE; do
  grep -qE "^ARG $arg=" "$ROOT_DIR/Dockerfile.web" || missing_args="$missing_args $arg"
done
if [ -z "$missing_args" ]; then
  record ok "dockerfile-web-declares-stamp-args" ""
else
  record no "dockerfile-web-declares-stamp-args" "Dockerfile.web has no ARG for:$missing_args; \${…} would expand to empty"
fi

# release-local.sh must pass those build args, and pass meaningful values: a
# short SHA and a UTC timestamp, not the tag and not an empty/typo'd variable.
web_args_line="$(grep '^WEB_ARGS=' "$ROOT_DIR/scripts/release-local.sh" | head -1)"
check_web_arg() { # check_web_arg <build-arg> <expected-rhs-pattern> <what>
  local key="$1" want="$2" what="$3" value var rhs
  value="$(printf '%s' "$web_args_line" | sed -n "s/.*$key=\([^\"]*\)\".*/\1/p")"
  case "$value" in
    "") record no "web-image-$key-build-arg" "WEB_ARGS passes no non-empty $key= value: $web_args_line"; return ;;
    \$*) var="${value#\$}"; var="${var#\{}"; var="${var%\}}" ;;
    *) record no "web-image-$key-build-arg" "$key is the literal '$value', not a variable holding $what"; return ;;
  esac
  rhs="$(grep -E "^[[:space:]]*$var=" "$ROOT_DIR/scripts/release-local.sh" | head -1 || true)"
  if [ -z "$rhs" ]; then
    record no "web-image-$key-build-arg" "$key=\$$var but \$$var is never assigned in release-local.sh (expands to empty)"
  elif ! printf '%s' "$rhs" | grep -qE "$want"; then
    record no "web-image-$key-build-arg" "$key=\$$var, but \$$var does not hold $what: $rhs"
  else
    record ok "web-image-$key-build-arg" ""
  fi
}
check_web_arg COMMIT 'rev-parse --short' "a short commit SHA"
check_web_arg DATE 'date -u' "a UTC build timestamp"

# Every release build site of Dockerfile.web must feed those args, or the ARG
# defaults ("unknown") ship. Add new build sites here.
for site in .github/workflows/release.yml docker-compose.selfhost.build.yml offline/build-offline.sh; do
  # Only the lines that belong to this file's Dockerfile.web build — both files
  # also build Dockerfile (the backend), which has always been stamped.
  window="$(grep -A14 'Dockerfile\.web' "$ROOT_DIR/$site" || true)"
  miss=""
  for arg in COMMIT DATE; do
    printf '%s' "$window" | grep -q "$arg" || miss="$miss $arg"
  done
  if [ -z "$miss" ]; then
    record ok "web-build-site-stamped:$site" ""
  else
    record no "web-build-site-stamped:$site" "builds Dockerfile.web without:$miss"
  fi
done


# Behavioral: build ./cmd/goosar with exactly the Dockerfile.web ldflags (its
# ${ARG} placeholders filled from the environment, an unset one collapsing to
# the empty string) and read what `goosar --version` actually prints.
if command -v go >/dev/null 2>&1 && command -v perl >/dev/null 2>&1; then
  LDF_TMP="$(mktemp -d "${TMPDIR:-/tmp}/goosar-ldflags-XXXXXX")"
  trap 'rm -rf "$ENV_TMP" "$SBOM_TMP" "$GATE_TMP" "$MANIFEST_TMP" "$LDF_TMP"' EXIT
  want_commit=abc1234
  want_date=2026-01-02T03:04:05Z
  resolved="$(printf '%s' "$web_ldflags" | NEXT_PUBLIC_APP_VERSION=v9.9.9 COMMIT="$want_commit" DATE="$want_date" \
    perl -pe 's/\$\{(\w+)\}/$ENV{$1} \/\/ ""/ge')"
  build_rc=0
  (cd "$ROOT_DIR/server" && go build -ldflags "$resolved" -o "$LDF_TMP/goosar" ./cmd/goosar) >"$LDF_TMP/build.log" 2>&1 || build_rc=$?
  if [ "$build_rc" -ne 0 ]; then
    record no "cli-version-prints-commit-and-date" "go build failed: $(cat "$LDF_TMP/build.log")"
  else
    version_out="$("$LDF_TMP/goosar" --version 2>&1 || true)"
    missing=""
    case "$version_out" in *"$want_commit"*) ;; *) missing="commit" ;; esac
    case "$version_out" in *"$want_date"*) ;; *) missing="$missing date" ;; esac
    if [ -z "$missing" ]; then
      record ok "cli-version-prints-commit-and-date" ""
    else
      record no "cli-version-prints-commit-and-date" "--version is missing$missing; got: $version_out"
    fi
  fi
else
  record no "cli-version-prints-commit-and-date" "go and perl are required to check the served CLI stamps"
fi

echo
echo "release-local.test.sh: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
