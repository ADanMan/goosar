#!/usr/bin/env bash
set -euo pipefail

# ==========================================================================
# Full verification pipeline: formatting → typecheck → unit tests → Go tests → E2E
# Usage: bash scripts/check.sh
# ==========================================================================

ENV_FILE="${ENV_FILE:-.env}"
if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

# shellcheck disable=SC1091
. scripts/local-env.sh

BACKEND_PID=""
FRONTEND_PID=""
STARTED_BACKEND=false
STARTED_FRONTEND=false
EXIT_CODE=0

# --------------------------------------------------------------------------
# Cleanup: kill only services this script started
# --------------------------------------------------------------------------
cleanup() {
  echo ""
  if [ "$STARTED_BACKEND" = true ] && [ -n "$BACKEND_PID" ]; then
    kill "$BACKEND_PID" 2>/dev/null && wait "$BACKEND_PID" 2>/dev/null || true
    echo "    Stopped backend (PID $BACKEND_PID)"
  fi
  if [ "$STARTED_FRONTEND" = true ] && [ -n "$FRONTEND_PID" ]; then
    kill "$FRONTEND_PID" 2>/dev/null && wait "$FRONTEND_PID" 2>/dev/null || true
    echo "    Stopped frontend (PID $FRONTEND_PID)"
  fi
  echo ""
  if [ "$EXIT_CODE" -eq 0 ]; then
    echo "✓ All checks passed."
  else
    echo "✗ Checks FAILED."
  fi
  exit "$EXIT_CODE"
}
trap cleanup EXIT

# --------------------------------------------------------------------------
# Utility: wait until a port responds
# --------------------------------------------------------------------------
wait_for_port() {
  local port=$1 name=$2 max_wait=${3:-60} path=${4:-/}
  local elapsed=0
  echo "    Waiting for $name on :$port..."
  while ! curl -sf "http://localhost:${port}${path}" > /dev/null 2>&1; do
    sleep 1
    elapsed=$((elapsed + 1))
    if [ "$elapsed" -ge "$max_wait" ]; then
      echo "    ERROR: $name did not start within ${max_wait}s"
      EXIT_CODE=1
      exit 1
    fi
  done
  echo "    $name ready (${elapsed}s)"
}

# --------------------------------------------------------------------------
# Step 0: Ensure DB
# --------------------------------------------------------------------------
echo "==> Using env file: $ENV_FILE"
echo "==> Checking PostgreSQL..."
bash scripts/ensure-postgres.sh "$ENV_FILE"

# --------------------------------------------------------------------------
# Step 1: House formatting (gofumpt + gci, Prettier)
# --------------------------------------------------------------------------
echo ""
echo "==> [1/6] Formatting (gofumpt + gci, Prettier)..."
bash scripts/check-go-format.sh || { EXIT_CODE=1; exit 1; }
pnpm format:check || { EXIT_CODE=1; exit 1; }

# --------------------------------------------------------------------------
# Step 2: TypeScript typecheck
# --------------------------------------------------------------------------
echo ""
echo "==> [2/6] TypeScript typecheck..."
pnpm typecheck || { EXIT_CODE=1; exit 1; }

# --------------------------------------------------------------------------
# Step 3: TypeScript unit tests (Vitest)
# --------------------------------------------------------------------------
echo ""
echo "==> [3/6] TypeScript unit tests..."
pnpm test || { EXIT_CODE=1; exit 1; }

# --------------------------------------------------------------------------
# Step 4: Go tests
# --------------------------------------------------------------------------
echo ""
echo "==> [4/6] Go tests..."
echo "==> Verifying Go test wrapper..."
bash scripts/test-go.test.sh || { EXIT_CODE=1; exit 1; }
echo "==> Verifying Windows installer parity (#447)..."
bash scripts/install-ps1.test.sh || { EXIT_CODE=1; exit 1; }
echo "==> Verifying the client acceptance script (#582)..."
bash scripts/acceptance-client.test.sh || { EXIT_CODE=1; exit 1; }
echo "==> Verifying the served Windows CLI installer parity (#567)..."
bash apps/web/public/install-ps1.test.sh || { EXIT_CODE=1; exit 1; }
echo "==> Running database migrations..."
(cd server && go run ./cmd/migrate up) || { EXIT_CODE=1; exit 1; }
bash scripts/test-go.sh || { EXIT_CODE=1; exit 1; }

# --------------------------------------------------------------------------
# Steps 5-6: E2E (delegated to scripts/test-e2e.sh — issue #212). It starts
# or reuses backend+frontend, runs Playwright, and tears down only what it
# started, so check.sh no longer manages E2E services itself.
# --------------------------------------------------------------------------
echo ""
echo "==> [5/6 + 6/6] E2E gate (scripts/test-e2e.sh)..."
ENV_FILE="$ENV_FILE" bash scripts/test-e2e.sh || { EXIT_CODE=1; exit 1; }
