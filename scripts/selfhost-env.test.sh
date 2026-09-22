#!/usr/bin/env bash
# Tests for scripts/selfhost-env.sh (issue #280): the shared .env bootstrap
# for `make selfhost` / `make selfhost-build` must generate secrets, pin
# GOOSAR_IMAGE_TAG to the newest release tag known to the clone (so a fresh
# self-host never silently runs a stale version), and warn when an existing
# .env pin has fallen behind. Also guards the repo-level invariant that no
# hardcoded version literal remains in .env.example or the compose defaults.
# Runs without docker and without network.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SCRIPT="$ROOT_DIR/scripts/selfhost-env.sh"

FAILURES=0

pass() { printf 'ok   %s\n' "$1"; }
env_val() { sed -n "s/^$2=//p" "$1" 2>/dev/null | tail -n1 || true; }
failed() {
  printf 'FAIL %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# Build a fixture checkout: the real .env.example plus a git repo carrying
# release tags. Tag order is deliberately non-lexicographic (v0.10.0 must
# beat v0.9.0) to prove version sort, not string sort.
make_fixture() {
  local dir="$1"
  shift
  mkdir -p "$dir"
  cp "$ROOT_DIR/.env.example" "$dir/.env.example"
  git -C "$dir" init -q
  git -C "$dir" -c user.email=t@t -c user.name=t commit -q --allow-empty -m x
  local tag
  for tag in "$@"; do
    git -C "$dir" tag "$tag"
  done
}

# --- Script parses ----------------------------------------------------------
if [ -f "$SCRIPT" ] && bash -n "$SCRIPT"; then
  pass "selfhost-env.sh exists and parses"
else
  failed "selfhost-env.sh missing or does not parse"
fi

# --- Fresh .env: secrets generated, tag pinned to newest release ------------
d="$TMP_ROOT/fresh"
make_fixture "$d" v0.6.5 v0.9.0 v0.10.0
if (cd "$d" && bash "$SCRIPT" >/dev/null 2>&1) && [ -f "$d/.env" ]; then
  pass "fresh run creates .env"
else
  failed "fresh run did not create .env"
fi
env_tag="$(env_val "$d/.env" GOOSAR_IMAGE_TAG)"
if [ "$env_tag" = "v0.10.0" ]; then
  pass "GOOSAR_IMAGE_TAG pinned to newest release tag (version sort)"
else
  failed "GOOSAR_IMAGE_TAG expected v0.10.0, got '$env_tag'"
fi
jwt="$(env_val "$d/.env" JWT_SECRET)"
pgpass="$(env_val "$d/.env" POSTGRES_PASSWORD)"
vcskey="$(env_val "$d/.env" GOOSAR_VCS_SECRET_KEY)"
if [ -n "$jwt" ] && [ -n "$pgpass" ] && [ -n "$vcskey" ]; then
  pass "JWT_SECRET, POSTGRES_PASSWORD, GOOSAR_VCS_SECRET_KEY generated"
else
  failed "generated secrets missing (jwt='$jwt' pg='$pgpass' vcs='$vcskey')"
fi
if grep -Eq "^DATABASE_URL=postgres://[^:]+:$pgpass@" "$d/.env" 2>/dev/null; then
  pass "DATABASE_URL password matches generated POSTGRES_PASSWORD"
else
  failed "DATABASE_URL password not synced with POSTGRES_PASSWORD"
fi

# --- Fresh .env: not world-readable ----------------------------------------
# It holds JWT_SECRET, POSTGRES_PASSWORD and both at-rest keys. cp inherits
# the example's mode masked by the umask, so without an explicit chmod this
# lands 0644 and every local account on the host reads the signing secret.
mode="$(ls -l "$d/.env" | cut -c1-10)"
if [ "$mode" = "-rw-------" ]; then
  pass ".env created with owner-only permissions"
else
  failed ".env mode is '$mode', want -rw------- (secrets readable by every local account)"
fi

# --- Fresh .env: MCP secret key generated and 32 decoded bytes (#428) -------
mcpkey="$(env_val "$d/.env" GOOSAR_MCP_SECRET_KEY)"
if [ -n "$mcpkey" ]; then
  pass "GOOSAR_MCP_SECRET_KEY generated"
else
  failed "GOOSAR_MCP_SECRET_KEY not generated (the server then stores MCP/VCS credentials as plaintext)"
fi
# secretbox.ValidateKeyEnv accepts exactly 32 base64-decoded bytes; a shorter
# key makes the server refuse to start, which is worse than not generating one.
decoded_len="$(printf '%s' "$mcpkey" | base64 -d 2>/dev/null | wc -c | tr -d ' ')"
if [ "$decoded_len" = "32" ]; then
  pass "GOOSAR_MCP_SECRET_KEY decodes to 32 bytes"
else
  failed "GOOSAR_MCP_SECRET_KEY decodes to $decoded_len bytes, want 32"
fi

# --- Fresh .env: self-host defaults to the perimeter profile (#428) ---------
profile="$(env_val "$d/.env" GOOSAR_DELIVERY_PROFILE)"
if [ "$profile" = "perimeter" ]; then
  pass "fresh self-host .env pins GOOSAR_DELIVERY_PROFILE=perimeter"
else
  failed "expected GOOSAR_DELIVERY_PROFILE=perimeter on a fresh self-host .env, got '$profile'"
fi

d="$TMP_ROOT/cloudprofile"
make_fixture "$d" v0.10.0
(cd "$d" && GOOSAR_DELIVERY_PROFILE=cloud bash "$SCRIPT" >/dev/null 2>&1) || true
profile="$(env_val "$d/.env" GOOSAR_DELIVERY_PROFILE)"
if [ "$profile" = "cloud" ]; then
  pass "explicit GOOSAR_DELIVERY_PROFILE=cloud wins over the perimeter default"
else
  failed "expected GOOSAR_DELIVERY_PROFILE=cloud, got '$profile'"
fi

d="$TMP_ROOT/badprofile"
make_fixture "$d" v0.10.0
if (cd "$d" && GOOSAR_DELIVERY_PROFILE=perimiter bash "$SCRIPT" >/dev/null 2>&1); then
  failed "a typo'd GOOSAR_DELIVERY_PROFILE was written into .env instead of failing"
else
  pass "typo'd GOOSAR_DELIVERY_PROFILE fails before .env is shipped"
fi
# "before .env is shipped" has to mean before it EXISTS: a rejected run that
# already copied .env.example leaves a full, profile-less .env, and the
# corrective re-run then takes the "existing .env" path and applies nothing.
if [ -e "$d/.env" ]; then
  failed "typo'd GOOSAR_DELIVERY_PROFILE left a half-written .env behind"
else
  pass "typo'd GOOSAR_DELIVERY_PROFILE leaves no .env behind"
fi

# --- Fresh .env: explicit GOOSAR_IMAGE_TAG env override wins ---------------
d="$TMP_ROOT/override"
make_fixture "$d" v0.6.5 v0.10.0
(cd "$d" && GOOSAR_IMAGE_TAG=v9.9.9 bash "$SCRIPT" >/dev/null 2>&1) || true
env_tag="$(env_val "$d/.env" GOOSAR_IMAGE_TAG)"
if [ "$env_tag" = "v9.9.9" ]; then
  pass "explicit GOOSAR_IMAGE_TAG override is pinned into new .env"
else
  failed "override expected v9.9.9, got '$env_tag'"
fi

# --- Fresh .env: no release tags -> no pin, compose falls back to latest ----
d="$TMP_ROOT/notags"
make_fixture "$d"
if (cd "$d" && bash "$SCRIPT" >/dev/null 2>&1); then
  pass "run succeeds in a clone without release tags"
else
  failed "run failed in a clone without release tags"
fi
env_tag="$(env_val "$d/.env" GOOSAR_IMAGE_TAG)"
if [ -z "$env_tag" ]; then
  pass "no release tags -> GOOSAR_IMAGE_TAG left empty (compose default applies)"
else
  failed "expected empty GOOSAR_IMAGE_TAG without release tags, got '$env_tag'"
fi

# --- Existing .env: untouched, stale pin warns ------------------------------
d="$TMP_ROOT/stale"
make_fixture "$d" v0.6.5 v0.10.0
printf 'GOOSAR_IMAGE_TAG=v0.6.5\n' >"$d/.env"
before="$(cat "$d/.env")"
stderr="$( (cd "$d" && bash "$SCRIPT" >/dev/null) 2>&1 || true )"
if [ "$(cat "$d/.env")" = "$before" ]; then
  pass "existing .env is never rewritten"
else
  failed "existing .env was modified"
fi
if printf '%s' "$stderr" | grep -q "v0.6.5" && printf '%s' "$stderr" | grep -q "v0.10.0"; then
  pass "stale pin warns, naming pinned and newest tags"
else
  failed "stale pin warning missing (stderr: $stderr)"
fi

# --- Existing .env: current pin does not warn -------------------------------
d="$TMP_ROOT/current"
make_fixture "$d" v0.10.0
printf 'GOOSAR_IMAGE_TAG=v0.10.0\n' >"$d/.env"
stderr="$( (cd "$d" && bash "$SCRIPT" >/dev/null) 2>&1 || true )"
if [ -z "$stderr" ]; then
  pass "up-to-date pin produces no warning"
else
  failed "unexpected output for up-to-date pin: $stderr"
fi

# --- Existing .env: non-release tags (offline-<rev>) do not warn ------------
d="$TMP_ROOT/offline"
make_fixture "$d" v0.10.0
printf 'GOOSAR_IMAGE_TAG=offline-abc123\n' >"$d/.env"
stderr="$( (cd "$d" && bash "$SCRIPT" >/dev/null) 2>&1 || true )"
if [ -z "$stderr" ]; then
  pass "offline-<rev> pin produces no staleness warning"
else
  failed "offline pin warned unexpectedly: $stderr"
fi

# --- PUBLIC_HOST derivation (#295) ------------------------------------------
# PUBLIC_HOST set in .env -> localhost/template defaults for the public-origin
# vars are replaced with the derived origin; explicit values always win.
d="$TMP_ROOT/pubhost-fresh"
make_fixture "$d" v0.10.0
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
# opt in on the generated .env, then re-run
printf 'PUBLIC_HOST=goosar.example.com\n' >>"$d/.env"
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
for key in FRONTEND_ORIGIN GOOSAR_APP_URL GOOSAR_PUBLIC_URL CORS_ALLOWED_ORIGINS; do
  got="$(env_val "$d/.env" "$key")"
  if [ "$got" = "https://goosar.example.com" ]; then
    pass "PUBLIC_HOST derives $key"
  else
    failed "PUBLIC_HOST: $key expected https://goosar.example.com, got '$got'"
  fi
done

# explicit http:// scheme in PUBLIC_HOST is kept as-is
d="$TMP_ROOT/pubhost-scheme"
make_fixture "$d" v0.10.0
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
printf 'PUBLIC_HOST=http://goosar.internal:8080\n' >>"$d/.env"
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
if [ "$(env_val "$d/.env" GOOSAR_PUBLIC_URL)" = "http://goosar.internal:8080" ]; then
  pass "PUBLIC_HOST with explicit scheme is used verbatim"
else
  failed "PUBLIC_HOST scheme: got '$(env_val "$d/.env" GOOSAR_PUBLIC_URL)'"
fi

# explicit non-default values win over derivation
d="$TMP_ROOT/pubhost-explicit"
make_fixture "$d" v0.10.0
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
printf 'PUBLIC_HOST=goosar.example.com\n' >>"$d/.env"
(cd "$d" && sed -i.bak "s#^CORS_ALLOWED_ORIGINS=.*#CORS_ALLOWED_ORIGINS=https://other.example.com#" .env && rm -f .env.bak)
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
if [ "$(env_val "$d/.env" CORS_ALLOWED_ORIGINS)" = "https://other.example.com" ]; then
  pass "explicit CORS_ALLOWED_ORIGINS wins over PUBLIC_HOST derivation"
else
  failed "explicit value clobbered: '$(env_val "$d/.env" CORS_ALLOWED_ORIGINS)'"
fi
if [ "$(env_val "$d/.env" GOOSAR_PUBLIC_URL)" = "https://goosar.example.com" ]; then
  pass "other vars still derived alongside an explicit one"
else
  failed "derivation skipped alongside explicit value"
fi

# a comma list is an explicit value even when it starts with localhost
d="$TMP_ROOT/pubhost-commalist"
make_fixture "$d" v0.10.0
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
printf 'PUBLIC_HOST=goosar.example.com\n' >>"$d/.env"
(cd "$d" && sed -i.bak "s#^CORS_ALLOWED_ORIGINS=.*#CORS_ALLOWED_ORIGINS=http://localhost:3001,https://other.example.com#" .env && rm -f .env.bak)
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
if [ "$(env_val "$d/.env" CORS_ALLOWED_ORIGINS)" = "http://localhost:3001,https://other.example.com" ]; then
  pass "comma-list CORS starting with localhost is not clobbered"
else
  failed "comma-list CORS clobbered: '$(env_val "$d/.env" CORS_ALLOWED_ORIGINS)'"
fi

# no PUBLIC_HOST -> a second run leaves the generated .env untouched
d="$TMP_ROOT/pubhost-off"
make_fixture "$d" v0.10.0
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
before="$(cat "$d/.env")"
(cd "$d" && bash "$SCRIPT" >/dev/null 2>&1)
if [ "$(cat "$d/.env")" = "$before" ]; then
  pass "without PUBLIC_HOST the .env stays untouched"
else
  failed "derivation ran without PUBLIC_HOST"
fi

if grep -q 'PUBLIC_HOST' .env.example; then
  pass ".env.example documents PUBLIC_HOST"
else
  failed ".env.example does not mention PUBLIC_HOST"
fi

# --- Repo invariants: the version literal lives nowhere ---------------------
if grep -Eq '^GOOSAR_IMAGE_TAG=$' .env.example; then
  pass ".env.example ships GOOSAR_IMAGE_TAG unpinned"
else
  failed ".env.example still pins GOOSAR_IMAGE_TAG: $(grep '^GOOSAR_IMAGE_TAG=' .env.example || true)"
fi
compose_defaults="$(grep -o 'GOOSAR_IMAGE_TAG:-[^}]*' docker-compose.selfhost.yml |
  sed 's/^GOOSAR_IMAGE_TAG:-//' | LC_ALL=C sort -u)"
if [ "$compose_defaults" = "latest" ]; then
  pass "compose GOOSAR_IMAGE_TAG default is 'latest' everywhere"
else
  failed "compose GOOSAR_IMAGE_TAG default(s): '$(printf '%s' "$compose_defaults" | tr '\n' ',')' (expected only 'latest')"
fi
if grep -Eq 'v[0-9]+\.[0-9]+\.[0-9]+' docker-compose.selfhost.yml; then
  failed "docker-compose.selfhost.yml carries a hardcoded release version literal"
else
  pass "docker-compose.selfhost.yml has no hardcoded release version literal"
fi
# --- install.sh delegates .env bootstrap to this script (#428) --------------
# The installer used to carry a half-copy that generated only two of the four
# secrets; the recommended install path must not diverge from `make selfhost`.
if grep -q 'bash scripts/selfhost-env.sh' scripts/install.sh; then
  pass "install.sh delegates .env bootstrap to scripts/selfhost-env.sh"
else
  failed "install.sh does not call scripts/selfhost-env.sh"
fi
if awk '/^setup_server\(\)/{f=1} /^}/{if(f)exit} f' scripts/install.sh | grep -q 'openssl rand'; then
  failed "install.sh still generates secrets itself instead of delegating"
else
  pass "install.sh no longer generates .env secrets itself"
fi

for target in selfhost selfhost-build; do
  if awk "/^$target:/{f=1;next} /^[a-zA-Z][a-zA-Z0-9_.-]*:/{f=0} f" Makefile | grep -q 'scripts/selfhost-env.sh'; then
    pass "make $target delegates .env bootstrap to scripts/selfhost-env.sh"
  else
    failed "make $target does not call scripts/selfhost-env.sh"
  fi
done

# --- Health loop reads the port from .env, not the shell (#428) ------------
# The Makefile derives PORT from BACKEND_PORT/API_PORT/SERVER_PORT in the
# included .env; a recipe that reads the SHELL variable $PORT instead polls
# 8081 on a stand configured for another port and always reports "still
# starting".
if grep -q '\${PORT:-8081}' Makefile; then
  failed "Makefile still polls the shell \$PORT in a health loop instead of \$(PORT) from .env"
else
  pass "Makefile health loops use the .env-derived \$(PORT)"
fi

# ---------------------------------------------------------------------------
if [ "$FAILURES" -gt 0 ]; then
  printf '\n%d selfhost-env test(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf '\nall selfhost-env tests passed\n'
