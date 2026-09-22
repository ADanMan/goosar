#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_config() {
  local config=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$config"; then
    echo "Missing expected docker compose config value:"
    echo "  $expected"
    exit 1
  fi
}

require_env() {
  local output=$1
  local expected=$2

  if ! grep -Fxq "$expected" <<<"$output"; then
    echo "Missing expected derived env value:"
    echo "  $expected"
    echo "Observed:"
    echo "$output"
    exit 1
  fi
}

tmp_env="$(mktemp)"
trap 'rm -f "$tmp_env"' EXIT
sed 's/^FRONTEND_PORT=.*/FRONTEND_PORT=3100/' .env.example >"$tmp_env"
printf '\nBACKEND_PORT=9100\n' >>"$tmp_env"
# JWT_SECRET is a required compose variable (no default since the silent-default
# hardening); the example env deliberately ships without a value, so the config
# interpolation needs a synthetic one. Test-only, clearly not a real secret.
printf 'JWT_SECRET=ci-synthetic-jwt-secret-for-compose-config-test\n' >>"$tmp_env"
# Rate limits the `dev` deployment profile writes into .env (ADR-0015).
printf 'RATE_LIMIT_API=6000\nRATE_LIMIT_AUTH=100\n' >>"$tmp_env"

config="$(
  docker compose \
    --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml \
    config
)"

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOSAR_APP_URL: http://localhost:3100'

for script in scripts/dev.sh scripts/check.sh; do
  if ! grep -Fq '. scripts/local-env.sh' "$script"; then
    echo "$script must source scripts/local-env.sh for shared local env derivation."
    exit 1
  fi
done

local_env="$(
  env -i PATH="$PATH" bash -c '
    set -euo pipefail
    env_file=$1
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
    # shellcheck disable=SC1091
    . scripts/local-env.sh
    printf "%s\n" \
      "PORT=${PORT}" \
      "FRONTEND_PORT=${FRONTEND_PORT}" \
      "FRONTEND_ORIGIN=${FRONTEND_ORIGIN}" \
      "GOOSAR_APP_URL=${GOOSAR_APP_URL}" \
      "GOOSAR_SERVER_URL=${GOOSAR_SERVER_URL}" \
      "LOCAL_UPLOAD_BASE_URL=${LOCAL_UPLOAD_BASE_URL}" \
      "PLAYWRIGHT_BASE_URL=${PLAYWRIGHT_BASE_URL}"
  ' _ "$tmp_env"
)"

require_env "$local_env" 'PORT=9100'
require_env "$local_env" 'FRONTEND_PORT=3100'
require_env "$local_env" 'FRONTEND_ORIGIN=http://localhost:3100'
require_env "$local_env" 'GOOSAR_APP_URL=http://localhost:3100'
require_env "$local_env" 'GOOSAR_SERVER_URL=ws://localhost:9100/ws'
require_env "$local_env" 'LOCAL_UPLOAD_BASE_URL=http://localhost:9100'
require_env "$local_env" 'PLAYWRIGHT_BASE_URL=http://localhost:3100'

echo "self-host env derivation ok"

# #231 guard: every GOOSAR_PROVISIONING_* variable the backend reads must be
# forwarded by docker-compose.selfhost.yml — compose only passes variables it
# lists, so a new store variable that skips this file silently never reaches
# the container (the manifest endpoint then 503s with a loaded catalog).
compose_file="$ROOT_DIR/docker-compose.selfhost.yml"
while IFS= read -r var; do
  case "$var" in *_) continue ;; esac  # prefix constants, not variables
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var (read by server/)"
    exit 1
  fi
done < <(grep -rhoE 'GOOSAR_PROVISIONING_[A-Z0-9_]+' "$ROOT_DIR/server" --include='*.go' | sort -u)

# Same #231 guard for the deployment-admin variables (#213): the bootstrap
# seed reads GOOSAR_DEPLOYMENT_ADMIN_EMAILS at startup, and an .env entry
# compose does not forward silently never seeds anyone.
while IFS= read -r var; do
  case "$var" in *_) continue ;; esac  # prefix constants, not variables
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var (read by server/)"
    exit 1
  fi
done < <(grep -rhoE 'GOOSAR_DEPLOYMENT_[A-Z0-9_]+' "$ROOT_DIR/server" --include='*.go' | sort -u)

# #428: the delivery profile and the MCP encryption key must reach the
# container — an .env entry compose does not forward is the same silent gap
# #231 shipped for the provisioning store.
# #484: role-workspace provisioning is a startup hook — a stand whose
# compose file does not forward the switch cannot turn it off at all.
for var in GOOSAR_DELIVERY_PROFILE GOOSAR_MCP_SECRET_KEY GOOSAR_VCS_SECRET_KEY GOOSAR_ROLE_WORKSPACES; do
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var"
    exit 1
  fi
done

# 0.11.1 regression: router.go reads GOOSAR_LLM_API_KEY/BASE_URL/DEFAULT_MODEL
# for GET /api/deployment/client-secrets (deployment_secrets.go). The compose
# file only forwarded the unrelated GOOSAR_DEPLOYMENT_LLM_* pair, so every
# stand's client-secrets response had llm: null and clients showed the AI
# gateway as unavailable even with a key in .env.
for var in GOOSAR_LLM_API_KEY GOOSAR_LLM_BASE_URL GOOSAR_LLM_DEFAULT_MODEL; do
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var (read by server/cmd/server/router.go)"
    exit 1
  fi
done

# #434: unbounded json-file logs fill the operator's disk, and LOG_LEVEL /
# LOG_FORMAT that .env sets must actually reach the backend container.
for var in LOG_LEVEL LOG_FORMAT GOOSAR_LOG_LEVEL GOOSAR_LOG_FORMAT; do
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var (read by server/internal/logger)"
    exit 1
  fi
done

# Same guard for the rate limiters (#530). The `dev` deployment profile writes
# RATE_LIMIT_API/RATE_LIMIT_AUTH into .env, and a limit compose does not
# forward is a stand that silently keeps the stock 600/5 while SELF_HOSTING.md
# says otherwise. Every RATE_LIMIT_* the server reads must be forwarded.
while IFS= read -r var; do
  [ "$var" = "RATE_LIMITED" ] && continue  # a GraphQL error type, not an env var
  if ! grep -q "$var:" "$compose_file"; then
    echo "docker-compose.selfhost.yml does not forward $var (read by server/)"
    exit 1
  fi
done < <(grep -rhoE '"RATE_LIMIT[A-Z_]*"' "$ROOT_DIR/server" --include='*.go' | tr -d '"' | sort -u)

# ...and the forwarding must survive interpolation, not merely appear in the
# file: this is the exact value the `dev` profile writes.
require_config "$config" 'RATE_LIMIT_API: "6000"'
require_config "$config" 'RATE_LIMIT_AUTH: "100"'

# json is the recommended production format, and the stack is production.
require_config "$config" 'LOG_FORMAT: json'

# Every service gets the same capped driver: three containers, one disk.
log_driver_services="$(grep -c 'driver: json-file' <<<"$config" || true)"
if [ "$log_driver_services" -lt 3 ]; then
  echo "expected a capped json-file log driver on all three services, found $log_driver_services"
  exit 1
fi
require_config "$config" 'max-size: 50m'
require_config "$config" 'max-file: "5"'

# The caps are operator-overridable from .env without editing the compose file.
override_env="$(mktemp)"
trap 'rm -f "$tmp_env" "$override_env"' EXIT
cp "$tmp_env" "$override_env"
printf '\nLOG_MAX_SIZE=10m\nLOG_MAX_FILE=2\nLOG_FORMAT=text\n' >>"$override_env"
override_config="$(
  docker compose \
    --env-file "$override_env" \
    -f docker-compose.selfhost.yml \
    config
)"
require_config "$override_config" 'max-size: 10m'
require_config "$override_config" 'max-file: "2"'
require_config "$override_config" 'LOG_FORMAT: text'

echo "self-host log configuration ok"

# --- ADR-0015 deployment profiles (#530) ------------------------------------
# Every profile must produce a valid .env: the profile name itself plus the
# variables the ADR table pins.
profile_env_val() { sed -n "s/^$2=//p" "$1" | tail -n1; }

profile_fixture() {
  local dir="$1"
  mkdir -p "$dir"
  cp "$ROOT_DIR/.env.example" "$dir/.env.example"
  git -C "$dir" init -q
  git -C "$dir" -c user.email=t@t -c user.name=t commit -q --allow-empty -m x
}

require_profile_env() {
  local file="$1" key="$2" want="$3" got
  got="$(profile_env_val "$file" "$key")"
  if [ "$got" != "$want" ]; then
    echo "profile .env: $key=$got, want $key=$want ($file)"
    exit 1
  fi
}

profile_root="$(mktemp -d)"
trap 'rm -f "$tmp_env" "$override_env"; rm -rf "$profile_root"' EXIT

for profile in perimeter demo dev local; do
  d="$profile_root/$profile"
  profile_fixture "$d"
  if ! (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE="$profile" bash "$ROOT_DIR/scripts/selfhost-env.sh" >"$d/$profile.out" 2>&1); then
    echo "selfhost-env.sh failed for profile $profile"
    exit 1
  fi
  require_profile_env "$d/.env" GOOSAR_DEPLOYMENT_PROFILE "$profile"
  require_profile_env "$d/.env" GOOSAR_ROLE_WORKSPACES auto
  require_profile_env "$d/.env" GOOSAR_DOWNLOAD_GITHUB_RELEASES off
  # Unset means the provisioning endpoints answer 503, so every profile pins it.
  require_profile_env "$d/.env" GOOSAR_PROVISIONING_STORE local
  # Self-hosting is the on-prem product: no profile opts back into cloud egress.
  require_profile_env "$d/.env" GOOSAR_DELIVERY_PROFILE perimeter
  if [ "$profile" = "perimeter" ]; then
    require_profile_env "$d/.env" ALLOW_SIGNUP false
    require_profile_env "$d/.env" GOOSAR_EXTERNAL_IMAGES block
    # ALLOW_SIGNUP=false with no allowlist locks everyone out, the operator
    # included, so the preset must say which variable actually opens the door.
    if ! grep -q 'ALLOWED_EMAIL_DOMAINS' "$d/perimeter.out"; then
      echo "perimeter preset does not tell the operator to set ALLOWED_EMAIL_DOMAINS"
      exit 1
    fi
  else
    require_profile_env "$d/.env" ALLOW_SIGNUP true
    # The ADR pins external images to the stock default outside perimeter:
    # silently blocking them on a demo stand is a product regression.
    if [ "$(profile_env_val "$d/.env" GOOSAR_EXTERNAL_IMAGES)" = "block" ]; then
      echo "profile $profile must not block external images (ADR-0015: stock default)"
      exit 1
    fi
  fi
  # Relaxed limits are the row that separates `dev` from `demo`/`local`.
  if [ "$profile" = "dev" ]; then
    require_profile_env "$d/.env" RATE_LIMIT_API 6000
    require_profile_env "$d/.env" RATE_LIMIT_AUTH 100
  else
    for limit in RATE_LIMIT_API RATE_LIMIT_AUTH; do
      if [ -n "$(profile_env_val "$d/.env" "$limit")" ]; then
        echo "profile $profile must leave $limit at the stock default"
        exit 1
      fi
    done
  fi
done

# The server parser trims and lowercases (deploymentprofile.Parse), so the
# installer must not reject a value the backend would happily run.
d="$profile_root/mixedcase"
profile_fixture "$d"
if ! (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE="  Demo  " bash "$ROOT_DIR/scripts/selfhost-env.sh" >/dev/null 2>&1); then
  echo "selfhost-env.sh rejected '  Demo  ', which the server accepts"
  exit 1
fi
require_profile_env "$d/.env" GOOSAR_DEPLOYMENT_PROFILE demo

# ...but only the OUTER whitespace: the Go parser trims, it does not squeeze,
# so 'de mo' is a typo the backend refuses at startup and the installer must
# refuse here.
d="$profile_root/innerspace"
profile_fixture "$d"
if (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE="de mo" bash "$ROOT_DIR/scripts/selfhost-env.sh" >/dev/null 2>&1); then
  echo "selfhost-env.sh accepted 'de mo' as 'demo'; the server would refuse to start"
  exit 1
fi

# A typo must fail loudly instead of writing an unknown profile the backend
# then refuses to start on.
d="$profile_root/bogus"
profile_fixture "$d"
if (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE=prod bash "$ROOT_DIR/scripts/selfhost-env.sh" >/dev/null 2>&1); then
  echo "selfhost-env.sh accepted GOOSAR_DEPLOYMENT_PROFILE=prod"
  exit 1
fi
# ...and it must leave NOTHING behind. A rejected run that has already copied
# .env.example turns the corrective re-run into a no-op (the script never
# touches an existing .env), so the stand silently ends up with an empty
# profile and open registration — exit 1 followed by a clean stand.
if [ -e "$d/.env" ]; then
  echo "selfhost-env.sh rejected the profile but left a half-written .env behind"
  exit 1
fi
if ! (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE=perimeter bash "$ROOT_DIR/scripts/selfhost-env.sh" >/dev/null 2>&1); then
  echo "corrective re-run after a rejected profile failed"
  exit 1
fi
require_profile_env "$d/.env" GOOSAR_DEPLOYMENT_PROFILE perimeter
require_profile_env "$d/.env" ALLOW_SIGNUP false

# A re-run against an existing .env applies nothing (by design). It must say
# so instead of exiting 0 in silence, or the operator walks away believing the
# profile switched.
if ! (cd "$d" && GOOSAR_DEPLOYMENT_PROFILE=demo bash "$ROOT_DIR/scripts/selfhost-env.sh" >"$d/rerun.out" 2>&1); then
  echo "re-run over an existing .env must not fail"
  exit 1
fi
if ! grep -q "GOOSAR_DEPLOYMENT_PROFILE" "$d/rerun.out"; then
  echo "re-run over an existing .env silently ignored GOOSAR_DEPLOYMENT_PROFILE=demo"
  exit 1
fi
require_profile_env "$d/.env" GOOSAR_DEPLOYMENT_PROFILE perimeter

# Both installers must offer the same four values. A bare `grep -q "$profile"`
# is vacuous here — `dev` matches /dev/null and `local` the shell keyword — so
# assert on the parser constructs themselves. install.sh's --profile flag is
# additionally covered behaviorally in scripts/install.test.sh.
if ! grep -qE '^\s*perimeter\|demo\|dev\|local\|cloud\)' "$ROOT_DIR/scripts/install.sh"; then
  echo "install.sh --profile does not accept perimeter|demo|dev|local|cloud"
  exit 1
fi
if ! grep -qE '^\s*(""\|)?perimeter\|demo\|dev\|local\)' "$ROOT_DIR/scripts/selfhost-env.sh"; then
  echo "selfhost-env.sh does not validate the four deployment profiles"
  exit 1
fi
if ! grep -q '"perimeter", "demo", "dev", "local"' "$ROOT_DIR/scripts/install.ps1"; then
  echo "install.ps1 does not validate the four deployment profiles"
  exit 1
fi
for script in scripts/install.sh scripts/install.ps1; do
  if ! grep -q "GOOSAR_DEPLOYMENT_PROFILE" "$script"; then
    echo "$script does not write GOOSAR_DEPLOYMENT_PROFILE"
    exit 1
  fi
done

# The compose stack must forward the variable, or the backend never sees the
# profile the installer just wrote (#231 guard, same shape).
if ! grep -q "GOOSAR_DEPLOYMENT_PROFILE:" "$compose_file"; then
  echo "docker-compose.selfhost.yml does not forward GOOSAR_DEPLOYMENT_PROFILE"
  exit 1
fi

echo "deployment profiles ok"
