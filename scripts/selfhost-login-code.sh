#!/usr/bin/env bash
# Print the latest [DEV] email verification code from the self-host backend
# logs (#295). When RESEND_API_KEY is unset the backend prints login codes to
# its stderr (stdout belongs to the audit journal, #434); `docker compose logs`
# shows both streams, so this saves the operator the `logs | grep` dance.
# Usage: make selfhost-code  (or: bash scripts/selfhost-login-code.sh)
set -euo pipefail

COMPOSE="${COMPOSE:-docker compose}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.selfhost.yml}"

line="$($COMPOSE -f "$COMPOSE_FILE" logs --no-color backend 2>/dev/null |
  grep -F '[DEV] Verification code' | tail -n1 || true)"

if [ -z "$line" ]; then
  echo "No verification code in the backend logs yet." >&2
  echo "Request a login code in the web UI first, then re-run: make selfhost-code" >&2
  echo "(With RESEND_API_KEY / SMTP_HOST configured, codes are emailed and never logged.)" >&2
  echo "With neither, this path needs APP_ENV=development in .env: on the stack default" >&2
  echo "APP_ENV=production the backend refuses sign-in (503 email_not_configured)." >&2
  exit 1
fi

echo "$line"
