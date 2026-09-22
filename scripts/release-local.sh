#!/usr/bin/env bash
# Goosar — build and publish a full release WITHOUT GitHub Actions.
#
# Local equivalent of .github/workflows/release.yml (issue #170), for when the
# Actions pipeline is unavailable (account billing, quota) or a release must be
# produced from an operator machine. Produces the same artifacts:
#
#   1. CI gate        scripts/test-go.test.sh + scripts/test-go.sh --race
#                     (what release.yml's `verify` job runs; --skip-tests to skip)
#   2. Docker images  backend + web for linux/amd64 + linux/arm64
#                     GHCR push is best-effort (--no-ghcr to skip); an offline
#                     images tar (load-release-images.sh format) is ALWAYS
#                     written so a quota-blocked registry can never block deploy.
#                     A buildx failure is classified before it is reported:
#                     build failure vs registry/push failure vs a dead buildkit
#                     builder (OOM) — each gets its own remediation (issue #279)
#   2b. SBOM + scan  scripts/sbom-scan.sh: syft CycloneDX SBOMs for both images
#                     and the repo source, trivy vuln+misconfig scan with a
#                     HIGH/CRITICAL gate (issue #385). Findings mark the ledger
#                     line and make the release PARTIAL; they never abort it.
#                     A missing syft/trivy is recorded as "skipped: tool
#                     missing" with the install page, never a silent skip.
#   3. CLI + Release  goreleaser release --clean (creates the GitHub Release;
#                     Releases/assets do not consume Actions minutes)
#   4. Desktop        one stage per requested platform (issue #401, ADR-0009):
#                     the Go CLI/daemon cross-compiled for the target
#                     (CGO_ENABLED=0) into offline/kit-cli/, then
#                     node scripts/package.mjs <platform flags> --thin
#                     --publish always. Builds are UNSIGNED until the security
#                     review (ADR-0003). A platform that fails, or that this
#                     host cannot build, is recorded MISSING and the remaining
#                     platforms still run.
#   5. Helm chart     helm package; OCI push best-effort; .tgz attached to the
#                     GitHub Release either way
#   6. Delivery       offline/offline-manifest.sh delivery writes
#      manifest       offline/DELIVERY-MANIFEST.md — the single hand-over list
#                     an acceptance act refers to (issue #437).
#
# The build runs FROM THE TAG'S TREE via a temporary `git worktree` at the tag
# commit, so this script (living on main) can release a tag that predates it,
# and no working-copy leftovers can leak into artifacts.
#
# This script NEVER pushes git refs or tags — the operator does that.
#
# Usage:
#   bash scripts/release-local.sh vX.Y.Z [options]
#
# Options:
#   --skip-tests          skip the CI-equivalent test gate
#   --no-ghcr             do not push images/chart to ghcr.io (tar + .tgz only)
#   --no-cli              skip goreleaser (NOTE: goreleaser creates the GitHub
#                         Release; skipping it means desktop/helm uploads need
#                         an existing release for the tag)
#   --no-desktop          skip the desktop build (all platforms)
#   --mac-arm64           desktop platform: macOS arm64 (the default when no
#                         platform flag is given)
#   --mac-x64             desktop platform: macOS x64
#   --win-x64             desktop platform: Windows x64 (NSIS, unsigned)
#   --linux-x64           desktop platform: Linux x64
#   --linux-arm64         desktop platform: Linux arm64
#   --all-platforms       every platform above
#
# Platform flags accumulate; the stages run in the order listed above. Each
# stage is independent: a failure (or a host that cannot build the target —
# a macOS app needs a mac host) records MISSING in the ledger and the next
# platform still runs. The final summary exits non-zero. Windows and Linux
# cross-build from macOS; a Linux cross-build produces the AppImage only.
#   --no-helm             skip the helm chart
#   --no-sbom             skip the SBOM + vulnerability scan step
#   --platforms LIST      image platforms (default: linux/amd64,linux/arm64)
#   --tar-platform PLAT   platform saved into the offline tar (default: linux/amd64,
#                         must match the machine that will RUN the images)
#
# Exit codes:
#   0  every expected artifact was published
#   1  a pre-flight gate failed (bad tag, missing tool, unusable environment)
#   2  PARTIAL release — the run finished but artifacts are MISSING. Stages that
#      do not depend on each other (images, CLI, desktop, helm)
#      all run even after one of them fails, and the final ledger names every
#      artifact with its status and reason. "release done" is printed only when
#      nothing is MISSING.
#
# Requirements: docker (buildx), gh (authenticated), goreleaser, helm, node,
# pnpm, go. `syft` and `trivy` are optional — without them the SBOM/scan step
# records "skipped: tool missing" and prints the install page. For the mac desktop build: the hermes agent artifact must be
# resolvable (GOOSAR_HERMES_ARTIFACT / GOOSAR_HERMES_DIST_DIR / the
# ../hermes-agent/dist sibling of the main checkout).
#
# Env file: the checkout's env file is loaded before the gates, so DATABASE_URL
# reaches the Go gate without the operator exporting it by hand (issue #351 —
# v0.8.3 died in ensure_test_db against test-go.sh's localhost:5432 default while
# a working .env sat in the repo). Selection mirrors scripts/test-e2e.sh: an
# explicit $ENV_FILE wins, otherwise .env, otherwise .env.worktree. A
# DATABASE_URL exported by the operator beats the file — for the Go gate;
# test-e2e.sh re-sources the same env file itself and its value wins there.
#
# E2E gate env (issue #450): the gate runs from the TAG TREE and is handed the
# operator checkout's env file by absolute path (.env.worktree first, then
# .env; override with GOOSAR_E2E_ENV_FILE). That file must carry:
#
#   DATABASE_URL        a THROWAWAY database — the suite truncates it
#   BACKEND_PORT        backend port (PORT/API_PORT/SERVER_PORT are read too)
#   FRONTEND_PORT       next dev port
#   FRONTEND_ORIGIN     http://localhost:$FRONTEND_PORT
#   GOOSAR_PUBLIC_URL  the same origin, for links the backend renders
#
# plus `umask 022` in the shell that starts the release. TURBO_ENV_MODE=loose
# and REMOTE_API_URL=http://localhost:$PORT are set by test-e2e.sh itself so
# the browser reaches the API through the Next proxy, same-origin. It also
# UNSETS NEXT_PUBLIC_API_URL / NEXT_PUBLIC_WS_URL, which .env.worktree carries
# by default: they move the browser off the frontend origin and the realtime
# spec then fails on the cross-origin websocket.
# Both gate ports must be free — a leftover stack is reported by PID before
# the gate starts, never discovered deep inside test-e2e.sh.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

info() { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33mWARN:\033[0m %s\n' "$1" >&2; }
fail() { printf '\033[31mERROR:\033[0m %s\n' "$1" >&2; exit 1; }

# --- artifact ledger ---------------------------------------------------------
# Every stage records exactly one line per artifact. The final report prints the
# ledger verbatim and the script exits non-zero when something the operator
# expects is MISSING — a partial release must never print "release done"
# (issue #279).
#   ok       produced/published
#   skipped  the operator asked for it to be skipped (a --no-* flag)
#   MISSING  it should have been produced and was not
#   WARN     a gap the owner decides about (#581: no binary package for a
#            platform) — recorded by name, never PARTIAL
STATUS_LINES=()
MISSING_COUNT=0
record_ok()      { STATUS_LINES+=("$(printf '%-24s ok       %s' "$1" "$2")"); }
record_skipped() { STATUS_LINES+=("$(printf '%-24s skipped  %s' "$1" "$2")"); }
record_warn()    { STATUS_LINES+=("$(printf '%-24s WARN     %s' "$1" "$2")"); }
record_missing() {
  STATUS_LINES+=("$(printf '%-24s MISSING  %s' "$1" "$2")")
  MISSING_COUNT=$((MISSING_COUNT + 1))
}

TAG=""
SKIP_TESTS=0
NO_GHCR=0
NO_CLI=0
NO_DESKTOP=0
NO_HELM=0
NO_SBOM=0
PLATFORMS="linux/amd64,linux/arm64"
TAR_PLATFORM="linux/amd64"
# Desktop platform matrix (issue #401, ADR-0009). Empty until the flags below
# fill it; an empty list falls back to mac-arm64, the only platform this script
# built before #401.
DESKTOP_PLATFORMS=()
ALL_DESKTOP_PLATFORMS="mac-arm64 mac-x64 win-x64 linux-x64 linux-arm64"

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-tests)     SKIP_TESTS=1 ;;
    --no-ghcr)        NO_GHCR=1 ;;
    --no-cli)         NO_CLI=1 ;;
    --no-desktop)     NO_DESKTOP=1 ;;
    --mac-arm64)      DESKTOP_PLATFORMS+=(mac-arm64) ;;
    --mac-x64)        DESKTOP_PLATFORMS+=(mac-x64) ;;
    --win-x64)        DESKTOP_PLATFORMS+=(win-x64) ;;
    --linux-x64)      DESKTOP_PLATFORMS+=(linux-x64) ;;
    --linux-arm64)    DESKTOP_PLATFORMS+=(linux-arm64) ;;
    # shellcheck disable=SC2206 — word splitting is the point here.
    --all-platforms)  DESKTOP_PLATFORMS+=($ALL_DESKTOP_PLATFORMS) ;;
    --no-helm)        NO_HELM=1 ;;
    --no-sbom)        NO_SBOM=1 ;;
    --platforms)      shift; PLATFORMS="${1:?--platforms needs a value}" ;;
    --tar-platform)   shift; TAR_PLATFORM="${1:?--tar-platform needs a value}" ;;
    # Print the whole header block, stopping at `set -euo` — a line range drifts
    # every time the header grows (it already shipped truncated once).
    --help|-h) sed -n '2,/^set -euo/p' "$ROOT_DIR/scripts/release-local.sh" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) fail "unknown flag: $1" ;;
    *)  [ -z "$TAG" ] || fail "unexpected argument: $1"; TAG="$1" ;;
  esac
  shift
done

# --- desktop platform matrix (issue #401, ADR-0009) --------------------------
# One row per platform the desktop client can be built for. Fields, `|`
# separated:
#
#   1 package.mjs flags   what apps/desktop/scripts/package.mjs is driven with
#   2 GOOS                for the CLI/daemon cross-compile
#   3 GOARCH              same
#   4 label               what the operator reads in the ledger
platform_spec() { # platform_spec <key>
  case "$1" in
    mac-arm64)   printf '%s' "--mac --arm64|darwin|arm64|macOS arm64" ;;
    mac-x64)     printf '%s' "--mac --x64|darwin|amd64|macOS x64" ;;
    win-x64)     printf '%s' "--win --x64|windows|amd64|Windows x64" ;;
    linux-x64)   printf '%s' "--linux --x64|linux|amd64|Linux x64" ;;
    linux-arm64) printf '%s' "--linux --arm64|linux|arm64|Linux arm64" ;;
    *) return 1 ;;
  esac
}
spec_field() { # spec_field <key> <1-based field>
  platform_spec "$1" | cut -d'|' -f"$2"
}

# Deduplicate while keeping the order the operator asked for, and fall back to
# the pre-#401 default when no platform flag was given.
resolve_desktop_platforms() {
  local seen="" key resolved=""
  for key in ${DESKTOP_PLATFORMS[@]+"${DESKTOP_PLATFORMS[@]}"}; do
    case " $seen " in *" $key "*) continue ;; esac
    seen="$seen $key"
    resolved="$resolved $key"
  done
  [ -n "$resolved" ] || resolved=" mac-arm64"
  # shellcheck disable=SC2206 — word splitting is the point here.
  DESKTOP_PLATFORMS=($resolved)
}
resolve_desktop_platforms

# desktop_host_block <key> — prints why THIS host cannot build that platform,
# or nothing when it can. Checked before the build so an impossible target is
# reported as a named gap in the ledger instead of a wall of electron-builder
# output twenty minutes in.
#
# One rule, and it is electron-builder's own: a macOS app can only be packaged
# on macOS. Windows and Linux DO cross-build from a macOS host — verified here
# with electron-builder 26, which carries its own makensis and needs no wine
# for NSIS (the widely repeated "NSIS needs wine" no longer holds; a gate on
# wine would refuse a target this host builds fine). A Linux cross-build is
# restricted to AppImage by package.mjs, because .deb/.rpm need dpkg/fpm.
desktop_host_block() { # desktop_host_block <key>
  local key="$1" host
  host="$(uname -s)"
  case "$key" in
    mac-*)
      [ "$host" = "Darwin" ] || printf 'needs a macOS host (this is %s)' "$host" ;;
  esac
}

# Test seam (scripts/release-local.test.sh only): print the resolved matrix and
# exit, before any tool/daemon/network check. This is also the plan mode an
# operator uses to see what a run WOULD build on this host before committing to
# it — each row carries whether the host can build that platform at all.
if [ -n "${GOOSAR_RELEASE_PRINT_PLATFORMS:-}" ]; then
  for key in ${DESKTOP_PLATFORMS[@]+"${DESKTOP_PLATFORMS[@]}"}; do
    reason="$(desktop_host_block "$key" || true)"
    printf 'PLATFORM=%s spec=%s host=%s\n' "$key" "$(platform_spec "$key")" \
      "${reason:-ok}"
  done
  exit 0
fi

# --- env file (issue #351) ---------------------------------------------------
# The gate below shells out to test-go.sh, which falls back to a localhost:5432
# guess when DATABASE_URL is unset — and it runs in the bare tag worktree, which
# has no .env of its own. Load the checkout's env file here so it sees the DB
# this checkout actually uses. (test-e2e.sh sources the same file itself and
# needs nothing from us.) `set -a` + source
# would clobber a DATABASE_URL the operator exported on purpose (releasing
# against a scratch DB), so that one is restored afterwards.
#
# Test seam (scripts/release-local.test.sh only): GOOSAR_RELEASE_PRINT_ENV=1
# prints the resolved DATABASE_URL and exits, before any tool/daemon check.
if [ -z "${ENV_FILE:-}" ]; then
  # A RELEASE runs against the deployment env, not a developer's worktree
  # isolation file: .env.worktree exports worktree ports/origins (e.g.
  # FRONTEND_ORIGIN=http://localhost:13059) that leak into the Go gate.
  if [ -f .env ]; then ENV_FILE=.env
  elif [ -f .env.worktree ]; then ENV_FILE=.env.worktree
  fi
fi
if [ -n "${ENV_FILE:-}" ] && [ -f "$ENV_FILE" ]; then
  # Take DATABASE_URL ONLY — never source the whole file. The deployment .env
  # carries RESEND_API_KEY / SMTP_* / origins; sourcing it exported those into
  # the e2e stack, whose backend then took the real email path and answered
  # send-code with 500, failing every fixture login (caught by the v0.8.4
  # release gate). The Go gate is the only step that needs a database URL.
  if [ -z "${DATABASE_URL:-}" ]; then
    ENV_DATABASE_URL="$(sed -n 's/^[[:space:]]*DATABASE_URL=//p' "$ENV_FILE" | tail -n1)"
    # Strip one layer of surrounding quotes, if the file uses them.
    ENV_DATABASE_URL="${ENV_DATABASE_URL%\"}"; ENV_DATABASE_URL="${ENV_DATABASE_URL#\"}"
    ENV_DATABASE_URL="${ENV_DATABASE_URL%\'}"; ENV_DATABASE_URL="${ENV_DATABASE_URL#\'}"
    if [ -n "$ENV_DATABASE_URL" ]; then
      export DATABASE_URL="$ENV_DATABASE_URL"
      info "DATABASE_URL taken from $ENV_FILE (nothing else is sourced)"
    fi
  else
    info "using the exported DATABASE_URL; $ENV_FILE not read"
  fi
elif [ -z "${DATABASE_URL:-}" ]; then
  warn "no env file (.env / .env.worktree) and no exported DATABASE_URL — the test gate will fall back to its localhost default"
fi
if [ -n "${GOOSAR_RELEASE_PRINT_ENV:-}" ]; then
  printf 'DATABASE_URL=%s\n' "${DATABASE_URL:-}"
  exit 0
fi

# --- hermes-agent dist source (T-27, #655) ----------------------------------
# Resolve GOOSAR_HERMES_DIST_DIR so the web-image stage below can stage the
# agent artifacts into the build context: an already-exported value wins, then
# GOOSAR_HERMES_DIST_DIR in the release env file ($GOOSAR_RELEASE_ENV, default
# ~/.goosar-release.env — the file is read line by line, never sourced), then
# the ../hermes-agent/dist sibling of the main checkout, the convention
# apps/desktop/scripts/bundle-agent.mjs already uses.
load_hermes_dist_source() {
  local root="$1" file line var value sibling
  if [ -z "${GOOSAR_HERMES_DIST_DIR:-}" ]; then
    file="${GOOSAR_RELEASE_ENV:-$HOME/.goosar-release.env}"
    if [ -f "$file" ]; then
      while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in ''|'#'*) continue ;; esac
        var="${line%%=*}"
        value="${line#*=}"
        if [ "$var" = "GOOSAR_HERMES_DIST_DIR" ] && [ -n "$value" ]; then
          export GOOSAR_HERMES_DIST_DIR="$value"
          break
        fi
      done < "$file"
    fi
  fi
  if [ -z "${GOOSAR_HERMES_DIST_DIR:-}" ]; then
    sibling="$root/../hermes-agent/dist"
    [ -d "$sibling" ] && export GOOSAR_HERMES_DIST_DIR="$sibling"
  fi
  if [ -n "${GOOSAR_HERMES_DIST_DIR:-}" ]; then
    printf 'source %-32s %s\n' "GOOSAR_HERMES_DIST_DIR" "$GOOSAR_HERMES_DIST_DIR"
  fi
  return 0
}
load_hermes_dist_source "$ROOT_DIR"

# --- e2e gate preflight (issue #450) -----------------------------------------
# The e2e gate runs FROM THE TAG TREE, but its env file (ports, DB, origins)
# only exists in the operator checkout, so it is handed over by absolute path.
# Selection follows test-e2e.sh's own rule (worktree file first), NOT the
# release rule above: e2e wants the isolated worktree ports.
E2E_ENV_FILE="${GOOSAR_E2E_ENV_FILE:-}"
if [ -z "$E2E_ENV_FILE" ]; then
  if [ -f "$ROOT_DIR/.env.worktree" ]; then E2E_ENV_FILE="$ROOT_DIR/.env.worktree"
  elif [ -f "$ROOT_DIR/.env" ]; then E2E_ENV_FILE="$ROOT_DIR/.env"
  fi
fi

# env_file_value <key> — the last assignment of <key> in $E2E_ENV_FILE, unquoted.
env_file_value() {
  [ -n "$E2E_ENV_FILE" ] && [ -f "$E2E_ENV_FILE" ] || return 0
  local v
  v="$(sed -n "s/^[[:space:]]*$1=//p" "$E2E_ENV_FILE" | tail -n1)"
  v="${v%\"}"; v="${v#\"}"; v="${v%\'}"; v="${v#\'}"
  printf '%s' "$v"
}

# Mirrors scripts/local-env.sh, so the ports checked here are the ports the
# gate will actually bind.
gate_ports() {
  local backend frontend
  backend="$(env_file_value BACKEND_PORT)"
  [ -n "$backend" ] || backend="$(env_file_value API_PORT)"
  [ -n "$backend" ] || backend="$(env_file_value SERVER_PORT)"
  [ -n "$backend" ] || backend="$(env_file_value PORT)"
  [ -n "$backend" ] || backend=8081
  frontend="$(env_file_value FRONTEND_PORT)"
  [ -n "$frontend" ] || frontend=3001
  printf '%s %s' "$backend" "$frontend"
}

# A leftover backend/frontend from an earlier failed gate makes the run die
# deep inside test-e2e.sh with "already running". Name the PID that owns the
# port instead, so the operator can kill exactly that and retry.
check_gate_ports() {
  local ports backend frontend entry port label pid
  # Without an env file the defaults below are 8081/3001 — the ports an
  # operator's own dev stack usually holds — so the gate would refuse with a
  # port complaint about a stack that has nothing to do with it, and
  # test-e2e.sh would then look for an env file inside the bare tag tree.
  # Say what is actually missing instead.
  [ -n "$E2E_ENV_FILE" ] && [ -f "$E2E_ENV_FILE" ] \
    || fail "no env file for the e2e gate (looked for $ROOT_DIR/.env.worktree, then $ROOT_DIR/.env; override with GOOSAR_E2E_ENV_FILE) — see 'bash scripts/release-local.sh --help' for what it must carry"
  ports="$(gate_ports)"
  backend="${ports% *}"
  frontend="${ports#* }"
  for entry in "$backend:backend" "$frontend:frontend"; do
    label="${entry#*:}"
    port="${entry%%:*}"
    # `|| true`: lsof exits 1 when nothing listens, and under `set -e` that
    # would abort the release on the healthy path.
    pid="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -n1 || true)"
    [ -n "$pid" ] || continue
    fail "e2e gate $label port $port is already in use by PID $pid ($(ps -o comm= -p "$pid" 2>/dev/null | tr -d ' ' || true)) — the gate must start the code under test; stop that process and re-run"
  done
  info "e2e gate ports free: backend $backend, frontend $frontend"
}

# The gate must run the TAG's tree. On v0.9.0 it ran the operator checkout,
# whose main sat 20 commits behind the tag, so the release was green-lit by a
# suite that never saw migrations 273-292.
assert_gate_tree() { # assert_gate_tree <dir> <sha>
  local head
  head="$(git -C "$1" rev-parse HEAD 2>/dev/null)" || fail "e2e gate tree $1 is not a git checkout"
  [ "$head" = "$2" ] || fail "e2e gate tree $1 is at $head, not the tag commit $2 — the gate would test the wrong code"
  info "e2e gate tree verified at $head"
}

# Test seam (scripts/release-local.test.sh only): run just this preflight
# against a WORKTREE/SHA supplied by the caller, then exit.
if [ -n "${GOOSAR_RELEASE_GATE_PREFLIGHT:-}" ]; then
  check_gate_ports
  [ -z "${WORKTREE:-}" ] || assert_gate_tree "$WORKTREE" "${SHA:-}"
  echo "GATE_PREFLIGHT=ok"
  exit 0
fi

# --- gates (mirror release.yml `verify`) -------------------------------------
[ -n "$TAG" ] || fail "usage: bash scripts/release-local.sh vX.Y.Z [options]"
if ! printf '%s' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
  fail "release tags must look like vX.Y.Z or vX.Y.Z-suffix; got '$TAG'"
fi
case "$TAG" in *-dirty*) fail "refusing to release from dirty tag '$TAG'" ;; esac
IS_STABLE=1
case "$TAG" in *-*) IS_STABLE=0 ;; esac

for tool in docker gh goreleaser helm node pnpm go perl; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done
docker info >/dev/null 2>&1 || fail "docker daemon is not running"
gh auth status >/dev/null 2>&1 || fail "gh is not authenticated (gh auth login)"

if command -v sha256sum >/dev/null 2>&1; then SHA256=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then SHA256=(shasum -a 256)
else fail "neither sha256sum nor shasum found"; fi

SHA="$(git rev-list -n1 "$TAG" 2>/dev/null)" || fail "tag not found: $TAG"
SHORT_SHA="$(git rev-parse --short=7 "$SHA")"
info "releasing $TAG = $SHA (stable=$IS_STABLE)"

GITHUB_TOKEN_VALUE="$(gh auth token)"
[ -n "$GITHUB_TOKEN_VALUE" ] || fail "gh auth token returned empty"
GH_LOGIN="$(gh api user --jq .login)"

BACKEND_IMAGE="ghcr.io/adanman/goosar-backend"
WEB_IMAGE="ghcr.io/adanman/goosar-web"

# --- tag-tree worktree -------------------------------------------------------
# Build from the tag's committed tree, never the operator's working copy. A
# worktree (not `git archive`) because goreleaser and the desktop version stamp
# both need the .git history/tags (git describe).
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/goosar-release-XXXXXX")"
WORKTREE="$STAGE/tree"
cleanup() {
  git worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true
  rm -rf "$STAGE"
}
trap cleanup EXIT
git worktree add --detach "$WORKTREE" "$SHA" >/dev/null
info "tag tree checked out at $WORKTREE"

# The tag tree needs node_modules for the e2e gate AND for the desktop build.
# Installed once, on first use — the gate needs it before the desktop stage,
# and --skip-tests must still leave the desktop build with dependencies.
WORKTREE_DEPS_READY=0
ensure_worktree_deps() {
  [ "$WORKTREE_DEPS_READY" = "1" ] && return 0
  info "pnpm install --frozen-lockfile in the tag tree"
  (cd "$WORKTREE" && pnpm install --frozen-lockfile) || return 1
  WORKTREE_DEPS_READY=1
}

# >>> worktree-hygiene (extracted and executed by scripts/release-local.test.sh)
# worktree_hygiene <tree>: keep `git describe --dirty` honest in the tag tree.
#
# v0.10.1 shipped five desktop builds stamped `-dirty`, and goreleaser
# refused the CLI, because dependency install and the e2e gate leave marks in
# the tree: `apps/web/next-env.d.ts` (tracked, regenerated) and untracked
# `AGENTS.md`/`CLAUDE.md`. None of it is the operator's code. Untracked files
# are removed — the tree is a throwaway checkout of the tag, and info/exclude
# is SHARED with the operator's own repository in a worktree, so excluding
# there would leave a mark in a checkout this script does not own. Modified
# tracked files are marked assume-unchanged so describe stops seeing them.
# Both are named in the log: a file this function did not expect is worth a
# look, not a silent hide.
worktree_hygiene() {
  local tree="$1" untracked modified
  untracked="$(git -C "$tree" ls-files --others --exclude-standard)"
  modified="$(git -C "$tree" diff --name-only)"
  [ -n "$untracked$modified" ] || return 0
  if [ -n "$untracked" ]; then
    warn "untracked files appeared in the tag tree — removed so the build is not stamped -dirty:"
    printf '%s\n' "$untracked" | sed 's/^/    /' >&2
    (cd "$tree" && printf '%s\n' "$untracked" | xargs rm -f --)
  fi
  if [ -n "$modified" ]; then
    warn "tracked files were modified by install/gate in the tag tree — marked assume-unchanged:"
    printf '%s\n' "$modified" | sed 's/^/    /' >&2
    printf '%s\n' "$modified" | git -C "$tree" update-index --assume-unchanged --stdin
  fi
}
# <<< worktree-hygiene

# --- 1. CI-equivalent gate ---------------------------------------------------
if [ "$SKIP_TESTS" = "1" ]; then
  warn "--skip-tests: skipping the CI-equivalent test gate"
else
  info "running the verify gate (test-go.test.sh + test-go.sh --race)"
  (cd "$WORKTREE" && bash scripts/test-go.test.sh)
  (cd "$WORKTREE" && bash scripts/test-go.sh --race)
  # E2E gate (issue #212): Actions is billing-blocked, so this is the only
  # place the Playwright suite guards a release. Runs from the TAG TREE — on
  # v0.9.0 it ran from the operator checkout, whose main was behind the tag,
  # and the release was green-lit by a suite that never saw the tag's
  # migrations (issue #450). Only the env file crosses over from ROOT_DIR;
  # node_modules is installed in the tree itself. `set -e` aborts on failure.
  info "running the e2e gate from the tag tree ($WORKTREE)"
  check_gate_ports
  ensure_worktree_deps || fail "pnpm install --frozen-lockfile failed in the tag tree"
  assert_gate_tree "$WORKTREE" "$SHA"
  # REQUIRE_FRESH: a pre-existing dev server may be stale code; the release
  # gate must only pass against servers it started itself.
  (cd "$WORKTREE" && ENV_FILE="$E2E_ENV_FILE" GOOSAR_E2E_REQUIRE_FRESH=1 bash scripts/test-e2e.sh)
fi

# --- 1b. third-party notices (#435) ------------------------------------------
# The delivery redistributes Go modules, npm packages and vendored skills; the

# --- 2. docker images --------------------------------------------------------
# Multi-arch pushes need the docker-container buildx driver; keep a dedicated
# builder so we never depend on what the default builder happens to be.
BUILDER="goosar-release"
if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container >/dev/null
fi

GHCR_OK=0
GHCR_SKIP_REASON=""
# The token `helm registry login` should use, or empty when the run rides on a
# docker session helm can read out of ~/.docker/config.json by itself.
GHCR_HELM_TOKEN=""
if [ "$NO_GHCR" = "1" ]; then
  info "--no-ghcr: skipping registry push"
  GHCR_SKIP_REASON="--no-ghcr"
else
  # Which token reaches ghcr.io, in order:
  #
  #   1 GOOSAR_GHCR_TOKEN  — an explicit classic PAT with write:packages.
  #   2 an existing session — whatever `docker login ghcr.io` the operator
  #                           already made; left alone rather than replaced.
  #   3 gh auth token       — the historical default, still right on a host
  #                           whose gh token GHCR accepts.
  #
  # 2 exists because this step used to be unconditional, and overwriting a
  # working session is how a release loses its images: `gh auth token` is an
  # OAuth-app token, and GHCR refuses those for package writes on some
  # accounts — "denied: permission_denied: The token provided does not match
  # expected scopes" at push time, with the images already built and the
  # operator's own working PAT replaced by this login on the way in.
  if [ -n "${GOOSAR_GHCR_TOKEN:-}" ]; then
    info "logging in to ghcr.io as $GH_LOGIN (GOOSAR_GHCR_TOKEN)"
    if printf '%s' "$GOOSAR_GHCR_TOKEN" | docker login ghcr.io -u "$GH_LOGIN" --password-stdin >/dev/null 2>&1; then
      GHCR_OK=1
      GHCR_HELM_TOKEN="$GOOSAR_GHCR_TOKEN"
    else
      warn "ghcr.io login with GOOSAR_GHCR_TOKEN failed — continuing with the offline tar only"
      GHCR_SKIP_REASON="ghcr.io login failed"
    fi
  elif grep -q '"ghcr.io"' "${DOCKER_CONFIG:-$HOME/.docker}/config.json" 2>/dev/null; then
    info "ghcr.io: keeping the existing docker login (set GOOSAR_GHCR_TOKEN to override)"
    GHCR_OK=1
  else
    info "logging in to ghcr.io as $GH_LOGIN (gh auth token)"
    if printf '%s' "$GITHUB_TOKEN_VALUE" | docker login ghcr.io -u "$GH_LOGIN" --password-stdin >/dev/null 2>&1; then
      GHCR_OK=1
      GHCR_HELM_TOKEN="$GITHUB_TOKEN_VALUE"
    else
      warn "ghcr.io login failed — continuing with the offline tar only"
      GHCR_SKIP_REASON="ghcr.io login failed"
    fi
  fi
fi

# ghcr_tags <image>: the -t list release.yml's merge job would create.
ghcr_tags() {
  printf -- '-t %s:%s ' "$1" "$TAG"
  printf -- '-t %s:sha-%s ' "$1" "$SHORT_SHA"
  if [ "$IS_STABLE" = "1" ]; then printf -- '-t %s:latest ' "$1"; fi
}

BUILDX_LOG="$STAGE/buildx.log"

# classify_buildx_failure <exit-code> <log> -> builder | push | build
#
# #279: every buildx failure used to be reported as "GHCR unavailable?". On
# v0.8.1/v0.8.3 the real cause was the BUILD (corepack could not reach
# registry.npmjs.org) while GHCR was perfectly healthy, which sent the operator
# hunting a registry outage that did not exist. So: a failure is a BUILD failure
# unless it carries a known registry/transport signal.
classify_buildx_failure() {
  local rc="$1" log="$2"
  # Builder death: the buildkit container was OOM-killed or removed. buildx
  # surfaces this as a raw grpc transport error, or 137 straight from docker.
  if [ "$rc" -eq 137 ] \
     || grep -qEi 'closing transport|failed to list workers|no such container|oomkilled|signal: killed' "$log"; then
    printf builder; return
  fi
  # Push phase: the image was built, the registry rejected or dropped it.
  # Transport signals only the export/push step can produce.
  if grep -qEi 'failed to push|error writing manifest|blob upload|unexpected status from (post|put|patch|head)' "$log"; then
    printf push; return
  fi
  # denied / unauthorized / rate-limit words are NOT push-only: a build step
  # that cannot pull its base image (docker.io rate limit, a private base) or
  # that hits a filesystem "Permission denied: /x" prints them too, and calling
  # that a registry outage is #279 in reverse. They count as a push failure only
  # when the line names the registry this script pushes to, and never when the
  # line is buildx resolving a source image.
  if grep -Ei 'denied|unauthorized|toomanyrequests|insufficient_scope' "$log" \
     | grep -vi 'resolve source metadata' | grep -qi 'ghcr\.io'; then
    printf push; return
  fi
  printf build
}

builder_death_help() { # builder_death_help <exit-code>
  warn "the buildx builder '$BUILDER' died mid-build (exit $1). That is the buildkit container being OOM-killed or removed — NOT a registry problem and NOT your Dockerfile."
  warn "retry recipe: docker buildx rm $BUILDER && docker buildx create --name $BUILDER --driver docker-container, then re-run this script. If it repeats, raise Docker Desktop's memory limit (the web build needs several GB) or drop --platforms to one arch."
}

run_buildx() { # run_buildx <buildx args...> — runs buildx, tees its output to BUILDX_LOG
  local rc=0
  docker buildx build "$@" 2>&1 | tee "$BUILDX_LOG" || rc=$?
  return "$rc"
}

build_push() { # build_push <image> <dockerfile> <title> [extra build args...]
  local image="$1" dockerfile="$2" title="$3" rc=0 kind label
  shift 3
  label="ghcr ${image##*/}"
  info "buildx push $image ($PLATFORMS)"
  # shellcheck disable=SC2046
  run_buildx --builder "$BUILDER" \
      --platform "$PLATFORMS" \
      --file "$WORKTREE/$dockerfile" \
      --label "org.opencontainers.image.title=$title" \
      --label "org.opencontainers.image.revision=$SHA" \
      --label "org.opencontainers.image.version=$TAG" \
      "$@" $(ghcr_tags "$image") --push "$WORKTREE" || rc=$?
  if [ "$rc" -eq 0 ]; then
    record_ok "$label" "$image:$TAG"
    return 0
  fi
  kind="$(classify_buildx_failure "$rc" "$BUILDX_LOG")"
  case "$kind" in
    builder)
      builder_death_help "$rc"
      record_missing "$label" "buildx builder died (exit $rc) — recreate the builder and re-run" ;;
    push)
      warn "registry push failed for $image — the image BUILT fine; ghcr.io is unreachable, over quota or denying the token. The offline tar still covers deploy."
      GHCR_OK=0
      record_missing "$label" "registry push failed (image built; offline tar covers deploy)" ;;
    *)
      warn "IMAGE BUILD failed for $image (buildx exit $rc, before the push phase). This is a build failure, not a registry problem — read the buildx output above (a failing RUN step, an unreachable package mirror). Re-run the build; ghcr.io is not the suspect."
      record_missing "$label" "image build failed (buildx exit $rc) — re-run the build" ;;
  esac
  return 0
}

build_load_for_tar() { # build_load_for_tar <image> <dockerfile> [extra build args...]
  local image="$1" dockerfile="$2" rc=0 kind
  shift 2
  info "buildx load $image:$TAG ($TAR_PLATFORM) for the offline tar"
  run_buildx --builder "$BUILDER" \
    --platform "$TAR_PLATFORM" \
    --file "$WORKTREE/$dockerfile" \
    -t "$image:$TAG" \
    "$@" --load "$WORKTREE" || rc=$?
  [ "$rc" -eq 0 ] && return 0
  kind="$(classify_buildx_failure "$rc" "$BUILDX_LOG")"
  [ "$kind" = "builder" ] && builder_death_help "$rc"
  warn "offline-tar build failed for $image:$TAG (buildx exit $rc, classified as a $kind failure) — the offline images tar cannot be written without it."
  return 1
}

# One build date for every image, so the backend's bundled `goosar` and the
# CLI the web image serves under /cli/* report the same build. The web image
# stamps the short SHA because that is what goreleaser and the kit-cli build
# put into the GitHub Release / offline binaries (issue #525).
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BACKEND_ARGS=(--build-arg "VERSION=$TAG" --build-arg "COMMIT=$SHA" --build-arg "DATE=$BUILD_DATE")
WEB_ARGS=(--build-arg "NEXT_PUBLIC_APP_VERSION=$TAG" --build-arg "COMMIT=$SHORT_SHA" --build-arg "DATE=$BUILD_DATE")

# >>> hermes-dist-stage (extracted and executed by scripts/release-local.test.sh)
# stage_hermes_dist_for_web (T-27, #655): Dockerfile.web's cli stage reads
# GOOSAR_HERMES_DIST_DIR as a path RELATIVE TO THE BUILD CONTEXT, and the
# build context is $WORKTREE (the tag tree checked out above) — NOT this
# checkout. A sibling ../hermes-agent/dist, or any path load_hermes_dist_source
# resolved relative to the operator's checkout, is therefore invisible to the
# build. Copy the resolved dist's hermes-* entries into
# $WORKTREE/offline/agent-dist/ (gitignored) and point the build-arg there
# instead. Sets HERMES_DIST_ARG (empty when there is nothing to stage) and
# records the ledger line itself — run directly (not in a subshell/command
# substitution), so record_ok/record_skipped reach the real STATUS_LINES.
HERMES_DIST_ARG=""
stage_hermes_dist_for_web() { # stage_hermes_dist_for_web <worktree>
  local worktree="$1" staged entry base platforms=""
  if [ -z "${GOOSAR_HERMES_DIST_DIR:-}" ] || [ ! -d "$GOOSAR_HERMES_DIST_DIR" ]; then
    record_skipped "agent artifacts (web /cli)" "dist not found — set GOOSAR_HERMES_DIST_DIR (or ~/.goosar-release.env, or the ../hermes-agent/dist sibling); the web image still ships the goosar CLI"
    return 0
  fi
  staged="$worktree/offline/agent-dist"
  mkdir -p "$staged"
  for entry in "$GOOSAR_HERMES_DIST_DIR"/hermes-*; do
    [ -e "$entry" ] || continue
    cp -R "$entry" "$staged/"
    base="$(basename "$entry")"
    platforms="$platforms ${base%.tar.gz}"
  done
  if [ -z "$platforms" ]; then
    record_skipped "agent artifacts (web /cli)" "GOOSAR_HERMES_DIST_DIR=$GOOSAR_HERMES_DIST_DIR has no hermes-* entries"
    return 0
  fi
  record_ok "agent artifacts (web /cli)" "staged:${platforms}"
  HERMES_DIST_ARG="offline/agent-dist"
}
# <<< hermes-dist-stage
stage_hermes_dist_for_web "$WORKTREE"
[ -n "$HERMES_DIST_ARG" ] && WEB_ARGS+=(--build-arg "GOOSAR_HERMES_DIST_DIR=$HERMES_DIST_ARG")

if [ "$GHCR_OK" = "1" ]; then
  build_push "$BACKEND_IMAGE" Dockerfile "Goosar Backend" "${BACKEND_ARGS[@]}"
  build_push "$WEB_IMAGE" Dockerfile.web "Goosar Web" "${WEB_ARGS[@]}"
elif [ "$NO_GHCR" = "1" ]; then
  record_skipped "ghcr images" "--no-ghcr"
else
  record_missing "ghcr images" "${GHCR_SKIP_REASON:-registry push not attempted} — push the images manually"
fi

# The offline tar is written UNCONDITIONALLY: it is the transfer path that no
# registry quota can block (issue #68 lineage). Same layout as
# offline/save-release-images.sh so offline/load-release-images.sh accepts it.
TAR_OK=1
build_load_for_tar "$BACKEND_IMAGE" Dockerfile "${BACKEND_ARGS[@]}" || TAR_OK=0
build_load_for_tar "$WEB_IMAGE" Dockerfile.web "${WEB_ARGS[@]}" || TAR_OK=0

POSTGRES_IMAGE="$(awk '$1 == "image:" && $2 ~ /pgvector/ { print $2; exit }' "$WORKTREE/docker-compose.selfhost.yml")"
[ -n "$POSTGRES_IMAGE" ] || fail "could not resolve the postgres image from docker-compose.selfhost.yml"
if [ "$TAR_OK" = "1" ]; then
  info "pulling $POSTGRES_IMAGE ($TAR_PLATFORM)"
  if ! docker pull --platform "$TAR_PLATFORM" "$POSTGRES_IMAGE" >/dev/null; then
    warn "docker pull failed for $POSTGRES_IMAGE — the offline tar cannot be written without it"
    TAR_OK=0
  fi
fi

OUT_DIR="$ROOT_DIR/offline/kit-images"
if [ "$TAR_OK" = "1" ]; then
  mkdir -p "$OUT_DIR"
  info "saving offline images tar to offline/kit-images/"
  docker save -o "$OUT_DIR/release-images.tar" \
    "$POSTGRES_IMAGE" "$BACKEND_IMAGE:$TAG" "$WEB_IMAGE:$TAG"
  {
    printf '%s\n' "$POSTGRES_IMAGE"
    printf '%s\n' "$BACKEND_IMAGE:$TAG"
    printf '%s\n' "$WEB_IMAGE:$TAG"
  } > "$OUT_DIR/images.list"
  (cd "$OUT_DIR" && "${SHA256[@]}" release-images.tar images.list > checksums.txt)
  info "offline tar ready ($(du -h "$OUT_DIR/release-images.tar" | cut -f1))"
  record_ok "offline images tar" "offline/kit-images/release-images.tar (+ images.list, checksums.txt)"
else
  # The offline tar is the deploy path of last resort; losing it silently is
  # exactly the failure #279 is about.
  record_missing "offline images tar" "an image build for the tar failed — see the buildx output above"
fi

# --- 2b. SBOM + vulnerability scan (issue #385) ------------------------------
# Runs after the images exist locally (the offline-tar load above), so syft and
# trivy read the exact bytes that ship. The step never fails the release: its
# summary becomes ledger lines, and the HIGH/CRITICAL gate marks the run
# PARTIAL so the operator decides between fixing and a recorded exception.
SBOM_DIR="$OUT_DIR"
if [ "$NO_SBOM" = "1" ]; then
  warn "--no-sbom: skipping SBOM generation and the vulnerability scan"
  record_skipped "SBOM (syft)" "--no-sbom"
  record_skipped "vuln scan (trivy)" "--no-sbom"
else
  mkdir -p "$SBOM_DIR"
  SBOM_SUMMARY="$STAGE/sbom-summary.txt"
  # `|| true` under `set -e -o pipefail`: a step that cannot even run (script
  # missing, unreadable, killed) must not take the CLI, desktop and helm
  # artifacts down with it. It produces no status line, so the
  # `*)` branch below turns it into a MISSING ledger entry and the release is
  # reported PARTIAL — which is the whole point of the ledger (issue #279).
  bash "$ROOT_DIR/scripts/sbom-scan.sh" \
    --tag "$TAG" --out "$SBOM_DIR" \
    --image "$BACKEND_IMAGE:$TAG" --image "$WEB_IMAGE:$TAG" \
    --source-dir "$WORKTREE" | tee "$SBOM_SUMMARY" || true
  sbom_field() { sed -n "s/^$1=//p" "$SBOM_SUMMARY" | tail -n1; }
  case "$(sbom_field SBOM_STATUS)" in
    ok)      record_ok "SBOM (syft)" "$(sbom_field SBOM_FILES) CycloneDX file(s) in offline/kit-images/" ;;
    partial) record_missing "SBOM (syft)" "syft failed for at least one target — see the log above" ;;
    skipped-tool-missing) record_skipped "SBOM (syft)" "syft not installed (https://github.com/anchore/syft)" ;;
    *)       record_missing "SBOM (syft)" "the SBOM step produced no status" ;;
  esac
  case "$(sbom_field SCAN_STATUS)" in
    ok)       record_ok "vuln scan (trivy)" "no HIGH/CRITICAL findings" ;;
    findings) record_missing "vuln scan (trivy)" "$(sbom_field SCAN_FINDINGS) HIGH/CRITICAL finding(s) — fix them or record an exception before hand-over" ;;
    partial)  record_missing "vuln scan (trivy)" "trivy failed for at least one target — see the log above" ;;
    skipped-tool-missing) record_skipped "vuln scan (trivy)" "trivy not installed (https://trivy.dev/latest/docs/target/container_image/)" ;;
    *)        record_missing "vuln scan (trivy)" "the scan step produced no status" ;;
  esac
fi

# --- 3. CLI binaries + the GitHub Release ------------------------------------
if [ "$NO_CLI" = "1" ]; then
  warn "--no-cli: skipping goreleaser (desktop/helm uploads need an existing release)"
  record_skipped "GitHub Release (CLI)" "--no-cli"
else
  info "goreleaser release --clean (creates the GitHub Release for $TAG)"
  worktree_hygiene "$WORKTREE"
  if (cd "$WORKTREE" && GITHUB_TOKEN="$GITHUB_TOKEN_VALUE" goreleaser release --clean); then
    record_ok "GitHub Release (CLI)" "https://github.com/adanman/goosar/releases/tag/$TAG"
  else
    warn "goreleaser failed — no GitHub Release for $TAG; the desktop and helm uploads below have nothing to attach to"
    record_missing "GitHub Release (CLI)" "goreleaser failed — see the log above"
  fi
fi

# --- 4. desktop, one stage per platform (issue #401, ADR-0009) ---------------
# Functions, not inline: a desktop stage has no bearing on helm or on the
# OTHER platforms, and on v0.8.1 its failure
# took the rest down with it under `set -e` (#279). errexit does not apply
# inside a function used as an `if` condition, so every step here checks its
# own status explicitly.
#
# Everything produced here is unsigned and un-notarized until the security
# review (ADR-0003): the operating system warns on first launch, and
# SELF_HOSTING.md tells the customer how to allow the build.
DESKTOP_KIT_DIR="$ROOT_DIR/offline/kit-desktop"
CLI_KIT_DIR="$ROOT_DIR/offline/kit-cli"

# Artifact kinds a desktop build can produce, on any platform. Collected from
# the build output into offline/kit-desktop/ so the delivery manifest lists
# them and an air-gapped hand-over carries them; the GitHub Release gets the
# same files.
DESKTOP_ARTIFACT_EXTS="dmg zip exe AppImage deb rpm"

# smoke_cli_binary <binary> <goos> <goarch> — the cheapest check that the bytes
# are what the stage claims: file(1) always, plus `--version` wherever this host
# can actually execute the target. Prints one short line for the ledger; it
# never fails the stage, because a smoke that cannot run here is not the same
# as a broken binary.
smoke_cli_binary() {
  local bin="$1" goos="$2" goarch="$3" host_os host_arch desc out image
  host_os="$(uname -s)"
  host_arch="$(uname -m)"
  desc="$(file -b "$bin" 2>/dev/null | cut -c1-60)"

  case "$goos" in
    windows)
      # No Windows host and no wine here: the PE header is the whole check.
      case "$desc" in
        *PE32*|*"MS Windows"*) printf 'file(1): %s' "$desc" ;;
        *) printf 'file(1) does NOT report a Windows PE: %s' "$desc" ;;
      esac
      return 0 ;;
    darwin)
      if [ "$host_os" = "Darwin" ]; then
        # An arm64 host runs an amd64 binary through Rosetta; `arch` fails
        # cleanly when Rosetta is not installed.
        if [ "$goarch" = "amd64" ] && [ "$host_arch" = "arm64" ]; then
          if out="$(arch -x86_64 "$bin" --version 2>&1 | head -n1)"; then
            printf 'Rosetta --version: %s' "$out"; return 0
          fi
          printf 'file(1): %s; Rosetta run failed' "$desc"; return 0
        fi
        if out="$("$bin" --version 2>&1 | head -n1)"; then
          printf 'native --version: %s' "$out"; return 0
        fi
      fi
      printf 'file(1): %s; not runnable on this host' "$desc"
      return 0 ;;
    linux)
      # Only a base image ALREADY on this machine, and only one whose
      # architecture matches the binary — anything else proves nothing about it
      # and would reach for the network in the middle of a release.
      if command -v docker >/dev/null 2>&1; then
        for image in alpine:3.21 alpine:latest debian:stable-slim; do
          [ "$(docker image inspect "$image" --format '{{.Architecture}}' 2>/dev/null)" = "$goarch" ] || continue
          if out="$(docker run --rm --platform "linux/$goarch" \
              -v "$(cd "$(dirname "$bin")" && pwd):/smoke:ro" "$image" \
              "/smoke/$(basename "$bin")" --version 2>&1 | head -n1)"; then
            printf 'docker %s --version: %s' "$image" "$out"; return 0
          fi
        done
      fi
      printf 'file(1): %s; no local linux/%s base image to run it in' "$desc" "$goarch"
      return 0 ;;
  esac
  printf 'file(1): %s' "$desc"
}

# build_cli_for_platform <key> — the Go CLI/daemon for one target, into
# offline/kit-cli/. CGO_ENABLED=0 (what .goreleaser.yml and
# apps/desktop/scripts/bundle-cli.mjs already use, so every target
# cross-compiles from any host), ldflags mirroring goreleaser so
# `goosar --version` reports the release.
#
# goreleaser publishes the same binaries to the GitHub Release; this writes
# them into the OFFLINE kit, which carried no CLI at all and which is the only
# transport into a closed perimeter. Go's build cache makes the overlap cheap.
build_cli_for_platform() {
  local key="$1" goos goarch label bin_name bin_dir archive
  goos="$(spec_field "$key" 2)"
  goarch="$(spec_field "$key" 3)"
  label="$(spec_field "$key" 4)"
  bin_name=goosar
  [ "$goos" = "windows" ] && bin_name=goosar.exe
  bin_dir="$STAGE/cli-$key"
  mkdir -p "$bin_dir" "$CLI_KIT_DIR"
  info "CLI/daemon build → $label ($goos/$goarch, CGO_ENABLED=0)"
  (cd "$WORKTREE/server" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath \
      -ldflags "-s -w -X main.version=$TAG -X main.commit=$SHORT_SHA -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o "$bin_dir/$bin_name" ./cmd/goosar) || return 1
  archive="$CLI_KIT_DIR/goosar-cli-${TAG#v}-$goos-$goarch.tar.gz"
  tar -czf "$archive" -C "$bin_dir" "$bin_name" || return 1
  CLI_SMOKE="$(smoke_cli_binary "$bin_dir/$bin_name" "$goos" "$goarch")"
  return 0
}

# collect_desktop_artifacts — copies everything the build produced into
# offline/kit-desktop/ and prints the basenames. package.mjs wipes dist/ at the
# start of EVERY invocation, so this has to happen between platforms.
collect_desktop_artifacts() {
  mkdir -p "$DESKTOP_KIT_DIR"
  local ext found=0 file
  for ext in $DESKTOP_ARTIFACT_EXTS; do
    while IFS= read -r -d '' file; do
      cp "$file" "$DESKTOP_KIT_DIR/" || return 1
      found=1
      printf '%s ' "$(basename "$file")"
      # `-prune` on the app bundle and the unpacked tree: both are build
      # intermediates full of arbitrary files, and a stray .zip inside one of
      # them would be uploaded to the release as if it were a deliverable.
    done < <(find "$WORKTREE/apps/desktop/dist" \
      \( -name '*.app' -o -name '*unpacked*' \) -prune -o \
      -maxdepth 3 -type f -name "*.$ext" -print0 2>/dev/null)
  done
  [ "$found" = "1" ]
}
run_desktop_platform() { # run_desktop_platform <key>
  local key="$1" flags label artifacts file
  flags="$(spec_field "$key" 1)"
  label="$(spec_field "$key" 4)"
  info "desktop stage → $label (unsigned, see ADR-0003)"
  worktree_hygiene "$WORKTREE"
  build_cli_for_platform "$key" || return 1
  # No-op when the e2e gate or an earlier platform already installed them.
  ensure_worktree_deps || return 1
  # --thin: the on-prem default build profile (issue #172/#185) — the public
  # DMG ships without skills/MCP servers/shared Chromium; the client
  # provisions them from its own deployment on first launch instead. A fully
  # air-gapped internal build that must carry the fat payload passes
  # --require-mcp-servers/--require-skills to the apps/desktop
  # `pnpm package --` entry point directly, not through release-local.sh.
  # shellcheck disable=SC2086 — $flags is a deliberate multi-word flag list.
  # GOOSAR_RELEASE_BUILD (#565): bundle-cli.mjs skips quietly without `go`
  # or a pre-built binary and the package still succeeds — a DMG without the
  # CLI, whose daemon never starts because the runtime repair download hits a
  # private repo. In a release that skip is a failure.
  (cd "$WORKTREE/apps/desktop" && \
    GH_TOKEN="$GITHUB_TOKEN_VALUE" GITHUB_TOKEN="$GITHUB_TOKEN_VALUE" \
    GOOSAR_RELEASE_BUILD=1 \
    node scripts/package.mjs $flags --thin --publish always) || return 1
  # Belt and braces for the same gap: the staging dir electron-builder copied
  # into the package must hold the binary bundle-cli.mjs just placed there.
  if ! ls "$WORKTREE/apps/desktop/resources/bin"/goosar* >/dev/null 2>&1; then
    warn "$label package was built without the bundled goosar CLI (apps/desktop/resources/bin is empty)"
    return 1
  fi

  # electron-builder no longer builds (or publishes) the mac DMG: it sized the
  # image's HFS volume from a `du` estimate with no headroom, silently dropped
  # the ~172MB Electron Framework binary on a >2GB app, and still exited 0 —
  # v0.6.7 shipped a DMG that aborted at launch for every user (issue #186).
  # package.mjs now builds each DMG itself and FAILS CLOSED unless the image
  # matches the app byte for byte; a non-zero package.mjs returned above, so a
  # truncated DMG can never reach the release.
  artifacts="$(collect_desktop_artifacts)" || {
    warn "no desktop artifact produced for $label"
    return 1
  }
  info "attaching $label artifacts to the release"
  local dmg
  for file in $artifacts; do
    dmg="$DESKTOP_KIT_DIR/$file"
    info "  upload $file"
    GH_TOKEN="$GITHUB_TOKEN_VALUE" gh release upload "$TAG" "$dmg" --clobber || return 1
  done
  DESKTOP_ARTIFACTS="$artifacts"
  return 0
}

# Stale-artifact guard for the two kit directories this stage writes. Neither
# is cleaned between releases and both hold VERSIONED filenames, so a previous
# tag's DMG or CLI archive would still be sitting there when section 7 walks
# offline/ — and it would be listed, with its checksum, in THIS tag's
# DELIVERY-MANIFEST.md, the document the acceptance act is signed against. Same
# failure class as the images.list guard in offline/offline-manifest.sh, which
# exists because it already happened once. Files carrying this tag survive, so
# building the platforms across several runs of the same tag still accumulates.
# Runs even under --no-desktop: a skipped stage is not a licence to hand over
# the previous release's client.
for kit_dir in "$CLI_KIT_DIR" "$DESKTOP_KIT_DIR"; do
  [ -d "$kit_dir" ] || continue
  for stale_file in "$kit_dir"/*; do
    [ -f "$stale_file" ] || continue
    case "$(basename "$stale_file")" in *"${TAG#v}"*) continue ;; esac
    warn "removing an artifact left from an earlier release: ${stale_file#"$ROOT_DIR"/}"
    rm -f "$stale_file"
  done
done

if [ "$NO_DESKTOP" = "1" ]; then
  warn "--no-desktop: skipping the desktop build"
  record_skipped "desktop" "--no-desktop"
else
  [ -n "${APPLE_TEAM_ID:-}" ] || warn "APPLE_TEAM_ID not set — macOS builds are unsigned and not notarized (ADR-0003: signing follows the security review)"
  for platform_key in ${DESKTOP_PLATFORMS[@]+"${DESKTOP_PLATFORMS[@]}"}; do
    platform_label="$(spec_field "$platform_key" 4)"
    host_block="$(desktop_host_block "$platform_key" || true)"
    CLI_SMOKE=""
    DESKTOP_ARTIFACTS=""
    if [ -n "$host_block" ]; then
      # The CLI still cross-compiles from anywhere, so build and record it even
      # when the Electron package cannot be produced here — a delivery missing
      # only its Windows installer is a different gap from one missing the
      # daemon too.
      if build_cli_for_platform "$platform_key"; then
        record_ok "cli $platform_key" "offline/kit-cli/ — $CLI_SMOKE"
      else
        record_missing "cli $platform_key" "go build failed — see the log above"
      fi
      warn "desktop $platform_label cannot be built on this host: $host_block"
      record_missing "desktop $platform_key" "not buildable on this host: $host_block"
      continue
    fi
    if run_desktop_platform "$platform_key"; then
      record_ok "cli $platform_key" "offline/kit-cli/ — $CLI_SMOKE"
      record_ok "desktop $platform_key" "unsigned; $DESKTOP_ARTIFACTS"
    else
      warn "desktop $platform_label failed — continuing with the remaining platforms and the helm chart (neither depends on it)"
      if [ -n "$CLI_SMOKE" ]; then
        record_ok "cli $platform_key" "offline/kit-cli/ — $CLI_SMOKE"
      else
        record_missing "cli $platform_key" "the CLI cross-compile failed — see the log above"
      fi
      record_missing "desktop $platform_key" "build or upload failed — see the log above"
    fi
  done
  # One checksum file per kit directory, next to the artifacts, in the same
  # shape as offline/kit-images/checksums.txt.
  for kit_dir in "$CLI_KIT_DIR" "$DESKTOP_KIT_DIR"; do
    [ -d "$kit_dir" ] || continue
    (cd "$kit_dir" && find . -maxdepth 1 -type f ! -name checksums.txt -exec "${SHA256[@]}" {} + > checksums.txt) || true
  done
fi

# --- 5. helm chart -----------------------------------------------------------
# Two artifacts, tracked separately: the OCI chart in the registry and the .tgz
# attached to the GitHub Release. Losing one is not losing the other, and #279
# is precisely about a run that lost the OCI push without saying so.
# helm shares ghcr.io with docker but not the login step: it reads
# ~/.docker/config.json, so a `helm registry login` with a DIFFERENT token
# overwrites the session the images were just pushed with. That is how v0.10.1
# pushed both images and then lost the chart to a 403 on the blob HEAD — the
# images rode the operator's PAT, the chart rode `gh auth token`. So: log in
# only with the token that already worked, and otherwise not at all.
helm_registry_login() {
  [ -z "$GHCR_HELM_TOKEN" ] && return 0
  printf '%s' "$GHCR_HELM_TOKEN" | helm registry login ghcr.io --username "$GH_LOGIN" --password-stdin >/dev/null 2>&1
}

run_helm() {
  local chart_dir="$WORKTREE/deploy/helm/goosar" chart_version="${TAG#v}" chart_out chart_tgz
  info "helm chart $chart_version"
  # Portable in-place edit (BSD sed on macOS needs -i '', GNU sed doesn't).
  perl -pi -e "s/^version:.*/version: $chart_version/" "$chart_dir/Chart.yaml" || return 1
  perl -pi -e "s/^appVersion:.*/appVersion: \"$TAG\"/" "$chart_dir/Chart.yaml" || return 1
  helm lint "$chart_dir" || return 1
  chart_out="$STAGE/chart-packages"
  helm package "$chart_dir" --destination "$chart_out" || return 1
  chart_tgz="$chart_out/goosar-$chart_version.tgz"

  if [ "$GHCR_OK" != "1" ]; then
    warn "helm OCI push skipped: ${GHCR_SKIP_REASON:-the image push to ghcr.io failed earlier in this run}"
    if [ "$NO_GHCR" = "1" ]; then
      record_skipped "helm chart (OCI)" "--no-ghcr"
    else
      record_missing "helm chart (OCI)" "ghcr.io unusable this run (${GHCR_SKIP_REASON:-image push failed}) — push it manually with helm push"
    fi
  elif helm_registry_login && helm push "$chart_tgz" "oci://ghcr.io/adanman/charts"; then
    info "helm chart pushed to oci://ghcr.io/adanman/charts"
    record_ok "helm chart (OCI)" "oci://ghcr.io/adanman/charts:$chart_version"
  else
    warn "helm OCI push failed — the .tgz is still attached to the release"
    record_missing "helm chart (OCI)" "helm push to oci://ghcr.io/adanman/charts failed"
  fi

  if gh release view "$TAG" >/dev/null 2>&1 && gh release upload "$TAG" "$chart_tgz" --clobber; then
    info "helm chart attached to the GitHub Release"
    record_ok "helm chart (release asset)" "goosar-$chart_version.tgz"
  else
    warn "no GitHub Release for $TAG (or the upload failed) — the chart .tgz is not attached"
    record_missing "helm chart (release asset)" "no GitHub Release for $TAG, or the upload failed"
  fi
}

if [ "$NO_HELM" = "1" ]; then
  warn "--no-helm: skipping the helm chart"
  record_skipped "helm chart" "--no-helm"
elif ! run_helm; then
  warn "helm chart stage failed before packaging — continuing with the remaining stages"
  record_missing "helm chart" "packaging failed (lint/package/perl) — see the log above"
fi

# --- 6. delivery manifest (issue #437) ---------------------------------------
# The acceptance act refers to ONE list of what was handed over under this tag.
# Everything it needs already exists by now: the offline kit with its
# checksums, the image digests, the chart version, the migration range, the
# daemon floor and the SBOMs.
if gh release view "$TAG" >/dev/null 2>&1; then
  sbom_uploaded=0
  for sbom in "$SBOM_DIR"/sbom-*.cyclonedx.json; do
    [ -e "$sbom" ] || break
    gh release upload "$TAG" "$sbom" --clobber >/dev/null 2>&1 && sbom_uploaded=$((sbom_uploaded + 1))
  done
  [ "$sbom_uploaded" -gt 0 ] && info "$sbom_uploaded SBOM file(s) attached to the GitHub Release"
fi

# Digests are best-effort: they only exist for images this run actually pushed.
DIGESTS_FILE="$STAGE/image-digests.txt"
: > "$DIGESTS_FILE"
for image in "$BACKEND_IMAGE" "$WEB_IMAGE"; do
  digest="$(docker image inspect "$image:$TAG" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)"
  [ -n "$digest" ] || continue
  printf '%s %s\n' "$image:$TAG" "${digest##*@}" >> "$DIGESTS_FILE"
done

DELIVERY_MD="$ROOT_DIR/offline/DELIVERY-MANIFEST.md"
delivery_args=(delivery --tag "$TAG" --dir "$ROOT_DIR/offline" --out "$DELIVERY_MD")
[ -s "$DIGESTS_FILE" ] && delivery_args+=(--digests "$DIGESTS_FILE")
if bash "$ROOT_DIR/offline/offline-manifest.sh" "${delivery_args[@]}"; then
  record_ok "delivery manifest" "offline/DELIVERY-MANIFEST.md"
else
  warn "the delivery manifest could not be generated — the acceptance act has no hand-over list"
  record_missing "delivery manifest" "offline/offline-manifest.sh delivery failed — see the log above"
fi

# --- report ------------------------------------------------------------------
# One honest line per artifact. Anything MISSING makes this a PARTIAL release
# and the script exits non-zero — "release done" is unreachable unless every
# expected artifact exists (issue #279).
# #594: a `-dirty` artifact is never a deliverable — it was built from a
# modified tag tree (or is a leftover from such a run) and must not ride the
# kit into a customer's hands. Named and MISSING, whichever run made it.
dirty_artifacts="$(find "$DESKTOP_KIT_DIR" "$CLI_KIT_DIR" -maxdepth 1 -name '*-dirty*' 2>/dev/null | xargs -I{} basename {} | tr '\n' ' ')"
if [ -n "$dirty_artifacts" ]; then
  record_missing "dirty artifacts" "built from a modified tag tree — delete and rebuild: $dirty_artifacts"
fi

info "artifact ledger for $TAG:"
printf '\n'
for line in ${STATUS_LINES[@]+"${STATUS_LINES[@]}"}; do printf '  %s\n' "$line"; done
cat <<EOF

Deploy to a linux/amd64 server without any registry:

  scp offline/kit-images/release-images.tar offline/kit-images/images.list \\
      offline/kit-images/checksums.txt <server>:~/goosar-kit/
  ssh <server> 'cd <checkout> && bash offline/load-release-images.sh --kit-dir ~/goosar-kit'
  # then: GOOSAR_IMAGE_TAG=$TAG make selfhost

This script never pushes git refs or tags.
EOF

if [ "$MISSING_COUNT" -gt 0 ]; then
  warn "release $TAG is PARTIAL: $MISSING_COUNT artifact(s) MISSING (listed above). Publish them by hand or re-run the failed stages before announcing the release."
  exit 2
fi
info "release $TAG done — every expected artifact was published."
