#!/usr/bin/env bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/goosar-test-go.XXXXXX")
BIN_DIR="$TEST_DIR/bin"
CALLS_FILE="$TEST_DIR/go-calls.log"
OUTPUT_FILE="$TEST_DIR/output.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"
export GOOSAR_TEST_GO_CALLS="$CALLS_FILE"

cat >"$BIN_DIR/go" <<'EOF'
#!/usr/bin/env bash
set -eu

case "${1:-}" in
  list)
    if [ "$#" -ne 2 ] || [ "$2" != "./..." ]; then
      echo "unexpected go list arguments: $*" >&2
      exit 2
    fi
    printf '%s\n' \
      github.com/adanman/goosar/server \
      github.com/adanman/goosar/server/internal/daemon \
      github.com/adanman/goosar/server/pkg/agent \
      github.com/adanman/goosar/server/pkg/agent/internal/testutil
    ;;
  run)
    # #201: test-go.sh provisions the hermetic `<name>_test` database via
    # `go run` before testing; record the call plus the derived DATABASE_URL
    # so the rewrite itself is pinned.
    printf '%s %s\n' "$*" "${DATABASE_URL:-}" >>"$GOOSAR_TEST_GO_CALLS"
    ;;
  test)
    printf '%s\n' "$*" >>"$GOOSAR_TEST_GO_CALLS"
    ;;
  *)
    echo "unexpected go command: $*" >&2
    exit 2
    ;;
esac
EOF
chmod 755 "$BIN_DIR/go"

PATH="$BIN_DIR:$PATH" DATABASE_URL='postgres://u:p@h:9/base?sslmode=disable' \
  bash "$SCRIPT_DIR/test-go.sh" --race

expected_calls='run ./cmd/ensure_test_db postgres://u:p@h:9/base_test?sslmode=disable
run ./cmd/migrate up postgres://u:p@h:9/base_test?sslmode=disable
test -race github.com/adanman/goosar/server github.com/adanman/goosar/server/internal/daemon
test -race -p 2 -parallel 2 ./pkg/agent/...'
actual_calls=$(cat "$CALLS_FILE")
if [ "$actual_calls" != "$expected_calls" ]; then
  echo "unexpected go test calls:" >&2
  printf '%s\n' "$actual_calls" >&2
  exit 1
fi

: >"$CALLS_FILE"
set +e
PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" --unknown >"$OUTPUT_FILE" 2>&1
status=$?
set -e

if [ "$status" -ne 2 ]; then
  echo "unknown option returned status $status, want 2" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi
if [ -s "$CALLS_FILE" ]; then
  echo "unknown option invoked go:" >&2
  cat "$CALLS_FILE" >&2
  exit 1
fi
if ! grep -q '^usage: .*test-go.sh \[--race\]$' "$OUTPUT_FILE"; then
  echo "unknown option did not print usage" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi

echo "test-go.test.sh: PASS"
