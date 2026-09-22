#!/usr/bin/env bash
# Shared .env bootstrap for `make selfhost` / `make selfhost-build` and for
# `scripts/install.sh --with-server` (#280, #428).
#
# Single source of truth: the installer used to carry its own half-copy that
# generated two of the four secrets, so the *recommended* install path shipped
# stands with no VCS key (integration 503s), no MCP key (integration
# credentials stored as plaintext in the database and in every backup) and no
# image pin (:latest).
#
# The release version must not live as a hand-edited literal in the repo:
# .env.example shipped GOOSAR_IMAGE_TAG=v0.6.5 for five minor releases and
# every fresh self-host silently ran a stale stack. Instead, the newest
# release tag is derived from the clone's own git tags at .env creation time
# (a `git clone` always carries the release tags), so pushing a release tag
# IS the update — no release-script edit, no manual bump.
#
# Behavior:
#   .env missing  -> copy .env.example, generate secrets, pin
#                    GOOSAR_IMAGE_TAG to the newest vX.Y.Z tag in the clone
#                    (an explicit GOOSAR_IMAGE_TAG env var wins; with no
#                    release tags the pin stays empty and compose falls back
#                    to :latest).
#   .env present  -> never modified (except opt-in PUBLIC_HOST derivation
#                    below); if its pinned vX.Y.Z tag is older than
#                    the newest release tag in the clone, print a warning so
#                    the operator knows the stand is behind. Non-release pins
#                    (offline-<rev>, custom tags) are left alone silently.
#
# Operates on the current working directory (make runs it from the checkout
# root; tests run it from a fixture checkout).
set -euo pipefail

if [ ! -f .env.example ]; then
  echo "ERROR: no .env.example in $PWD — run from the checkout root." >&2
  exit 1
fi

# Portable in-place sed (BSD sed on macOS needs -i '').
sed_i() {
  if [ "$(uname)" = "Darwin" ]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

set_env_kv() {
  # Escape sed-replacement specials so a host containing `&`/`#`/`\` cannot
  # break or corrupt the substitution.
  local esc
  esc="$(printf '%s' "$2" | sed -e 's/[\\&#]/\\&/g')"
  if grep -q "^$1=" .env; then
    sed_i "s#^$1=.*#$1=$esc#" .env
  else
    printf '%s=%s\n' "$1" "$2" >> .env
  fi
}

# Newest release tag known to this clone, by version sort (v0.10.0 > v0.9.0).
# Filtered to strict vX.Y.Z: a suffixed local tag (v1.0.0-dirty, left over
# from a build or a test run) must never outrank a real release just because
# --sort=-v:refname treats "1.0.0" as newer than "0.11.1".
latest_release_tag() {
  git tag --list 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1 || true
}

# --- Profile validation, BEFORE anything is written --------------------------
# Both profiles are validated here rather than inside the .env-creation branch
# below: a rejected value used to exit AFTER `cp .env.example .env`, so the
# stand kept a full, profile-less .env — and the corrective re-run then took
# the "existing .env" path, applied nothing and exited 0. A typo must leave the
# directory exactly as it found it.
#
# The server treats an unknown profile as a hard startup error, so catching a
# typo here beats writing it into .env and shipping a crash loop.
DELIVERY_PROFILE="${GOOSAR_DELIVERY_PROFILE:-perimeter}"
case "$DELIVERY_PROFILE" in
  cloud|perimeter) ;;
  *) echo "ERROR: GOOSAR_DELIVERY_PROFILE must be 'cloud' or 'perimeter' (got '$DELIVERY_PROFILE')." >&2; exit 1 ;;
esac

# The server parser trims and lowercases (strings.TrimSpace + strings.ToLower),
# so accept exactly what it would accept. `tr -d '[:space:]'` also stripped
# INNER whitespace, which quietly turned 'de mo' into a valid 'demo' the Go
# side would refuse at startup.
DEPLOYMENT_PROFILE="$(printf '%s' "${GOOSAR_DEPLOYMENT_PROFILE:-}" | tr '[:upper:]' '[:lower:]' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
case "$DEPLOYMENT_PROFILE" in
  ""|perimeter|demo|dev|local) ;;
  *) echo "ERROR: GOOSAR_DEPLOYMENT_PROFILE must be 'perimeter', 'demo', 'dev' or 'local' (got '$DEPLOYMENT_PROFILE')." >&2; exit 1 ;;
esac

if [ ! -f .env ]; then
  echo "==> Creating .env from .env.example..."
  cp .env.example .env
  # Before a single secret is written: this file holds JWT_SECRET (forges any
  # session), POSTGRES_PASSWORD and the two at-rest keys. cp inherits the
  # example's 0644 masked by the operator's umask, so on a normal server it
  # would land world-readable — same treatment the TLS private keys get in
  # scripts/selfhost-pg-tls.sh.
  chmod 600 .env
  JWT=$(openssl rand -hex 32)
  PGPASS=$(openssl rand -hex 24)
  VCSKEY=$(openssl rand -base64 32)
  # secretbox.ValidateKeyEnv requires exactly 32 base64-decoded bytes.
  MCPKEY=$(openssl rand -base64 32)
  # Grafana admin password for the optional monitoring overlay (#396).
  # Generated unconditionally: the overlay refuses to start on an empty value,
  # and a stand that turns monitoring on months later should not have to know
  # which secret is still missing.
  GRAFANAPASS=$(openssl rand -hex 24)
  sed_i "s/^JWT_SECRET=.*/JWT_SECRET=$JWT/" .env
  sed_i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$PGPASS/" .env
  sed_i -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$PGPASS\2#" .env
  sed_i "s#^GOOSAR_VCS_SECRET_KEY=.*#GOOSAR_VCS_SECRET_KEY=$VCSKEY#" .env
  sed_i "s#^GOOSAR_MCP_SECRET_KEY=.*#GOOSAR_MCP_SECRET_KEY=$MCPKEY#" .env
  sed_i "s#^GRAFANA_ADMIN_PASSWORD=.*#GRAFANA_ADMIN_PASSWORD=$GRAFANAPASS#" .env
  echo "==> Generated random JWT_SECRET, POSTGRES_PASSWORD, GOOSAR_VCS_SECRET_KEY, GOOSAR_MCP_SECRET_KEY, and GRAFANA_ADMIN_PASSWORD"
  echo "    Back GOOSAR_VCS_SECRET_KEY and GOOSAR_MCP_SECRET_KEY up with the database: losing them loses every stored integration credential."

  # Delivery profile (#428). Self-hosting IS the on-prem product, so a fresh
  # .env is pinned to `perimeter` — no self-updates, no clawhub.ai/github.com
  # skill egress — unless the operator asks for cloud behavior explicitly
  # (GOOSAR_DELIVERY_PROFILE=cloud). See SELF_HOSTING.md →
  # «Профиль доставки и исходящий трафик» for the full destination/switch table.
  sed_i "s#^GOOSAR_DELIVERY_PROFILE=.*#GOOSAR_DELIVERY_PROFILE=$DELIVERY_PROFILE#" .env
  echo "==> Set GOOSAR_DELIVERY_PROFILE=$DELIVERY_PROFILE"

  # Deployment profile (ADR-0015, #530). Optional: with the variable unset a
  # fresh .env keeps today's stock values byte-for-byte (the server then reads
  # the empty value as `perimeter`). Naming a profile applies the ADR table's
  # variable set, so an operator does not hand-edit six variables and miss one.
  #
  # Three rows of the table are deliberately NOT written here, because their
  # value is a fact about the host that no installer can guess. All three are
  # listed as operator steps in SELF_HOSTING.md and runbook §2:
  #
  #   POSTGRES_SSLMODE        =require needs the TLS overlay's certificates;
  #                           writing it blindly gives a stack that cannot
  #                           reach its own database.
  #   ALLOWED_EMAIL_DOMAINS   the customer's own domain. On `perimeter` this
  #   / ALLOWED_EMAILS        is the variable that lets the FIRST account in:
  #                           ALLOW_SIGNUP=false with no allowlist is a stand
  #                           nobody — including the operator — can sign into.
  #   GOOSAR_MAC_DMG_URL     and the sibling build links: they are full URLs
  #                           of files the operator uploads to /downloads, so
  #                           a guessed filename would render a broken link on
  #                           /download instead of no link.
  #
  # One row is not a variable at all: role `open_join`. The server derives it
  # from the signup posture (open registration + no allowlist => roles are
  # provisioned CLOSED, because "any authenticated user" is then unbounded),
  # so a demo/dev/local operator who wants the ADR's open roles either sets an
  # allowlist or opens each role in the UI. Documented in SELF_HOSTING.md.
  if [ -n "$DEPLOYMENT_PROFILE" ]; then
    set_env_kv GOOSAR_DEPLOYMENT_PROFILE "$DEPLOYMENT_PROFILE"
    # Common to all four: roles provisioned automatically, no GitHub release
    # lookup from the stand (builds are served from /downloads), packages
    # delivered from the stand's own object storage rather than a registry
    # (with the variable unset the provisioning endpoints answer 503).
    set_env_kv GOOSAR_ROLE_WORKSPACES auto
    set_env_kv GOOSAR_DOWNLOAD_GITHUB_RELEASES off
    set_env_kv GOOSAR_PROVISIONING_STORE local
    if [ "$DEPLOYMENT_PROFILE" = "perimeter" ]; then
      set_env_kv ALLOW_SIGNUP false
      set_env_kv GOOSAR_EXTERNAL_IMAGES block
      echo "==> Applied deployment profile perimeter (ADR-0015)"
      echo "    Registration is closed. Set ALLOWED_EMAIL_DOMAINS (or ALLOWED_EMAILS) before"
      echo "    the first start, or NOBODY can sign in — including you: those two variables are"
      echo "    the only way past ALLOW_SIGNUP=false. GOOSAR_DEPLOYMENT_ADMIN_EMAILS grants the"
      echo "    administrator role to an account that already exists; it does not create one."
    else
      set_env_kv ALLOW_SIGNUP true
      if [ "$DEPLOYMENT_PROFILE" = "dev" ]; then
        # The one row that separates `dev` from `demo`/`local`: a load run
        # against stock limits is throttled and measures the limiter.
        set_env_kv RATE_LIMIT_API 6000
        set_env_kv RATE_LIMIT_AUTH 100
      fi
      echo "==> Applied deployment profile $DEPLOYMENT_PROFILE (ADR-0015)"
      echo "    Role workspaces are provisioned CLOSED while registration is open with no"
      echo "    allowlist. Open the roles you want joinable in the UI, or set ALLOWED_EMAIL_DOMAINS."
    fi
  fi

  TAG="${GOOSAR_IMAGE_TAG:-$(latest_release_tag)}"
  if [ -n "$TAG" ]; then
    sed_i "s/^GOOSAR_IMAGE_TAG=.*/GOOSAR_IMAGE_TAG=$TAG/" .env
    echo "==> Pinned GOOSAR_IMAGE_TAG=$TAG (newest release tag in this clone)"
  else
    echo "==> No release tag found in this clone; images default to :latest"
  fi
else
  # A profile is only ever applied while .env is being created. Saying so is
  # the difference between "nothing to do" and an operator who believes the
  # stand just switched profiles.
  if [ -n "$DEPLOYMENT_PROFILE" ]; then
    echo "WARN: .env already exists — GOOSAR_DEPLOYMENT_PROFILE=$DEPLOYMENT_PROFILE was NOT applied." >&2
    echo "      Edit GOOSAR_DEPLOYMENT_PROFILE (and the rest of the profile's variables) in .env and restart the backend." >&2
  fi
  current="$(sed -n 's/^GOOSAR_IMAGE_TAG=//p' .env | tail -n1 || true)"
  latest="$(latest_release_tag)"
  case "$current" in
    v[0-9]*)
      if [ -n "$latest" ] && [ "$current" != "$latest" ] &&
         [ "$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n1)" = "$latest" ]; then
        echo "WARN: .env pins GOOSAR_IMAGE_TAG=$current but the newest release tag here is $latest." >&2
        echo "      Edit .env, or run: GOOSAR_IMAGE_TAG=$latest make selfhost" >&2
      fi
      ;;
  esac
fi

# --- PUBLIC_HOST derivation (#295) ------------------------------------------
# One PUBLIC_HOST in .env (opt-in; bare hostname or full origin) derives the
# public-origin variables that historically had to be set by hand — forgetting
# any of them ends in a silent WS 403. A variable is only derived while it
# still holds its stock localhost/template default (empty, contains `${`, or
# starts with http://localhost); an explicit operator value always wins.
# GOOSAR_TRUSTED_PROXIES is NOT derivable from a hostname (it is a CIDR list
# of the reverse proxy) and stays manual.
env_current() {
  sed -n "s/^$1=//p" .env | tail -n1
}

PUBLIC_HOST_VALUE="$(env_current PUBLIC_HOST)"
if [ -n "$PUBLIC_HOST_VALUE" ]; then
  case "$PUBLIC_HOST_VALUE" in
    http://*|https://*) PUBLIC_ORIGIN="$PUBLIC_HOST_VALUE" ;;
    *) PUBLIC_ORIGIN="https://$PUBLIC_HOST_VALUE" ;;
  esac
  PUBLIC_ORIGIN="${PUBLIC_ORIGIN%/}"
  derived=""
  for key in FRONTEND_ORIGIN GOOSAR_APP_URL GOOSAR_PUBLIC_URL CORS_ALLOWED_ORIGINS; do
    cur="$(env_current "$key")"
    case "$cur" in
      *,*)
        # A comma list (e.g. CORS with several origins) is always an explicit
        # operator value, even if it starts with a localhost origin.
        ;;
      ""|*'${'*|http://localhost*)
        set_env_kv "$key" "$PUBLIC_ORIGIN"
        derived="$derived $key"
        ;;
    esac
  done
  if [ -n "$derived" ]; then
    echo "==> Derived$derived = $PUBLIC_ORIGIN from PUBLIC_HOST (explicit values win)"
  fi
fi
