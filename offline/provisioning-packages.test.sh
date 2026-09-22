#!/usr/bin/env bash
# Tests for offline/load-provisioning-packages.sh — the HTTP-index catalog
# loader. Hermetic: the index is served from a temp directory through file://
# URLs, and the bearer-token cases use a fake `curl` on PATH that maps URLs to
# files in that directory. No docker, no network, no jq/node.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOAD_SCRIPT="$ROOT_DIR/offline/load-provisioning-packages.sh"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() {
  printf 'FAIL %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

expect_success() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$name"; else failed "$name"; fi
}

if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi
sha256_of() { ($SHA256 "$1") | awk '{print $1}'; }

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# make_entry <serve-dir> <rel-blob-path> <name> <version> <type> <platform> <content> [requires-json]
# Writes the blob under <serve-dir>/<rel-blob-path> and prints one index entry.
make_entry() {
  local serve="$1" rel="$2" name="$3" version="$4" type="$5" platform="$6" content="$7" requires="${8:-[]}"
  mkdir -p "$serve/$(dirname "$rel")"
  printf '%s' "$content" > "$serve/$rel"
  printf '{"name":"%s","version":"%s","type":"%s","platform":"%s","sha256":"%s","size":%s,"requires":%s,"url":"%s"}' \
    "$name" "$version" "$type" "$platform" "$(sha256_of "$serve/$rel")" "$(wc -c < "$serve/$rel" | tr -d ' ')" "$requires" "$rel"
}

# write_index <file> <entry>... — pretty-printed index document.
write_index() {
  local file="$1"
  shift
  local sep="" body=""
  for e in "$@"; do
    body="$body$sep
    $e"
    sep=","
  done
  printf '{\n  "schemaVersion": 1,\n  "packages": [%s\n  ]\n}\n' "$body" > "$file"
}

# run_loader <args...> — runs the loader, captures output in $OUT, status in $STATUS.
run_loader() {
  OUT="$(bash "$LOAD_SCRIPT" "$@" 2>&1)" && STATUS=0 || STATUS=$?
}

# expect_load_failure <label> <expected substring> <args...>
expect_load_failure() {
  local label="$1" want="$2"
  shift 2
  run_loader "$@"
  if [ "$STATUS" -ne 0 ] && printf '%s' "$OUT" | grep -q -- "$want"; then
    pass "$label"
  else
    failed "$label (status=$STATUS, wanted '$want')"
    printf '%s\n' "$OUT" >&2
  fi
}

# --- Script syntax and portability -------------------------------------------
expect_success "load-provisioning-packages.sh parses" bash -n "$LOAD_SCRIPT"
if grep -q 'sha256sum' "$LOAD_SCRIPT" && grep -q 'shasum -a 256' "$LOAD_SCRIPT"; then
  pass "loader pairs sha256sum with a shasum -a 256 fallback"
else
  failed "loader must pair sha256sum with a 'shasum -a 256' fallback (breaks on macOS)"
fi
expect_success "--help prints usage" bash "$LOAD_SCRIPT" --help
help_out="$(bash "$LOAD_SCRIPT" --help 2>&1)"
if printf '%s' "$help_out" | grep -q -- '--index-url'; then
  pass "--help documents --index-url"
else
  failed "--help documents --index-url"
fi

# --- happy path --------------------------------------------------------------
serve="$TMP_ROOT/serve"
store="$TMP_ROOT/store"
mkdir -p "$serve"
e1="$(make_entry "$serve" "packages/office-docx-1.4.0-darwin-arm64.tar.zst" office-docx 1.4.0 skill darwin-arm64 "blob-one")"
e2="$(make_entry "$serve" "blobs/web-search-2.0.1.tar.zst" web-search 2.0.1 mcp-server '*' "blob-two" '["skill:office-docx@1.4.0"]')"
write_index "$serve/index.json" "$e1" "$e2"

run_loader --index-url "file://$serve/index.json" --store-dir "$store" --prefix provisioning
if [ "$STATUS" -eq 0 ]; then pass "loader succeeds on a well-formed index"; else failed "loader succeeds on a well-formed index"; printf '%s\n' "$OUT" >&2; fi

office_dir="$store/provisioning/office-docx/1.4.0"
web_dir="$store/provisioning/web-search/2.0.1"
if [ -f "$office_dir/manifest.json" ] && [ -f "$office_dir/office-docx-1.4.0-darwin-arm64.tar.zst" ] &&
   [ -f "$web_dir/manifest.json" ] && [ -f "$web_dir/web-search-2.0.1-*.tar.zst" ]; then
  pass "packages land under the backend's <prefix>/<name>/<version> keys with the literal platform in the blob name"
else
  failed "packages land under the backend's <prefix>/<name>/<version> keys"
  find "$store" >&2 || true
fi

if diff -q "$serve/packages/office-docx-1.4.0-darwin-arm64.tar.zst" "$office_dir/office-docx-1.4.0-darwin-arm64.tar.zst" >/dev/null 2>&1; then
  pass "stored blob matches the served bytes"
else
  failed "stored blob matches the served bytes"
fi

manifest="$(cat "$web_dir/manifest.json")"
for want in '"schemaVersion":1' '"name":"web-search"' '"version":"2.0.1"' '"type":"mcp-server"' '"platform":"*"' \
            "\"sha256\":\"$(sha256_of "$serve/blobs/web-search-2.0.1.tar.zst")\"" '"size":8' '"requires":["skill:office-docx@1.4.0"]'; do
  if printf '%s' "$manifest" | grep -qF -- "$want"; then
    pass "manifest.json carries $want"
  else
    failed "manifest.json carries $want: $manifest"
  fi
done

catalog_json="$(cat "$store/provisioning/catalog.json" 2>/dev/null || true)"
if printf '%s' "$catalog_json" | grep -q '"name":"office-docx","version":"1.4.0"' &&
   printf '%s' "$catalog_json" | grep -q '"name":"web-search","version":"2.0.1"'; then
  pass "catalog.json lists every installed package in the backend's schema"
else
  failed "catalog.json lists every installed package: $catalog_json"
fi

# --- second run reuses what is already in the store --------------------------
run_loader --index-url "file://$serve/index.json" --store-dir "$store" --prefix provisioning
if [ "$STATUS" -eq 0 ] && printf '%s' "$OUT" | grep -q 'already present office-docx@1.4.0'; then
  pass "a repeated load keeps packages whose digest already matches"
else
  failed "a repeated load keeps packages whose digest already matches (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

# --- env defaults ------------------------------------------------------------
env_store="$TMP_ROOT/env-store"
OUT="$(LOCAL_UPLOAD_DIR="$env_store" GOOSAR_PROVISIONING_LOCAL_PREFIX="custom-prefix" bash "$LOAD_SCRIPT" --index-url "file://$serve/index.json" 2>&1)" && STATUS=0 || STATUS=$?
if [ "$STATUS" -eq 0 ] && [ -f "$env_store/custom-prefix/office-docx/1.4.0/manifest.json" ]; then
  pass "LOCAL_UPLOAD_DIR / GOOSAR_PROVISIONING_LOCAL_PREFIX select the store directory"
else
  failed "LOCAL_UPLOAD_DIR / GOOSAR_PROVISIONING_LOCAL_PREFIX select the store directory (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

# --- a minified index reads the same as a pretty-printed one -----------------
tr -d '\n ' < "$serve/index.json" > "$serve/minified.json"
min_store="$TMP_ROOT/min-store"
run_loader --index-url "file://$serve/minified.json" --store-dir "$min_store"
if [ "$STATUS" -eq 0 ] && [ -f "$min_store/provisioning/web-search/2.0.1/manifest.json" ]; then
  pass "a minified index loads every package"
else
  failed "a minified index loads every package (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

# --- verification failures leave the store untouched -------------------------
bad_serve="$TMP_ROOT/bad-serve"
mkdir -p "$bad_serve"
g1="$(make_entry "$bad_serve" "good.tar.zst" good-pkg 1.0.0 skill '*' "good-content")"
b1="$(make_entry "$bad_serve" "bad.tar.zst" bad-pkg 1.0.0 skill '*' "original-content")"
printf 'tampered-content' > "$bad_serve/bad.tar.zst"
write_index "$bad_serve/index.json" "$g1" "$b1"
bad_store="$TMP_ROOT/bad-store"
expect_load_failure "a blob whose sha256 differs from the index is refused" "sha256 mismatch for bad-pkg@1.0.0" \
  --index-url "file://$bad_serve/index.json" --store-dir "$bad_store"
if [ ! -e "$bad_store/provisioning/catalog.json" ] && [ ! -e "$bad_store/provisioning/good-pkg" ]; then
  pass "nothing is written to the store when any package fails verification"
else
  failed "nothing is written to the store when any package fails verification"
fi

size_serve="$TMP_ROOT/size-serve"
mkdir -p "$size_serve"
s1="$(make_entry "$size_serve" "s.tar.zst" size-pkg 1.0.0 skill '*' "abcdef")"
s1="$(printf '%s' "$s1" | sed 's/"size":6/"size":7/')"
write_index "$size_serve/index.json" "$s1"
expect_load_failure "a blob whose size differs from the index is refused" "size mismatch for size-pkg@1.0.0" \
  --index-url "file://$size_serve/index.json" --store-dir "$TMP_ROOT/size-store"

# --- malformed indexes -------------------------------------------------------
mal="$TMP_ROOT/mal"
mkdir -p "$mal"

write_index "$mal/dup.json" "$e1" "$e1"
expect_load_failure "the same name@version listed twice is refused" "twice" \
  --index-url "file://$mal/dup.json" --store-dir "$TMP_ROOT/dup-store"

badtype="$(printf '%s' "$e1" | sed 's/"type":"skill"/"type":"plugin"/')"
write_index "$mal/type.json" "$badtype"
expect_load_failure "an unknown package type is refused" "type must be skill, mcp-server or runtime" \
  --index-url "file://$mal/type.json" --store-dir "$TMP_ROOT/type-store"

badplat="$(printf '%s' "$e1" | sed 's/"platform":"darwin-arm64"/"platform":"solaris-sparc"/')"
write_index "$mal/plat.json" "$badplat"
expect_load_failure "an unknown platform is refused" "invalid platform" \
  --index-url "file://$mal/plat.json" --store-dir "$TMP_ROOT/plat-store"

nosha="$(printf '%s' "$e1" | sed 's/"sha256":"[0-9a-f]*"/"sha256":"deadbeef"/')"
write_index "$mal/sha.json" "$nosha"
expect_load_failure "a short sha256 is refused" "sha256 must be 64 lowercase hex" \
  --index-url "file://$mal/sha.json" --store-dir "$TMP_ROOT/sha-store"

badname="$(printf '%s' "$e1" | sed 's/"name":"office-docx"/"name":"..\/evil"/')"
write_index "$mal/name.json" "$badname"
expect_load_failure "a path-like package name is refused" "invalid name" \
  --index-url "file://$mal/name.json" --store-dir "$TMP_ROOT/name-store"

badreq="$(printf '%s' "$e1" | sed 's/"requires":\[\]/"requires":["not a ref"]/')"
write_index "$mal/req.json" "$badreq"
expect_load_failure "a malformed requires entry is refused" "invalid requires entry" \
  --index-url "file://$mal/req.json" --store-dir "$TMP_ROOT/req-store"

printf '{"schemaVersion":2,"packages":[%s]}' "$e1" > "$mal/schema.json"
expect_load_failure "an unsupported schemaVersion is refused" "unsupported index schemaVersion" \
  --index-url "file://$mal/schema.json" --store-dir "$TMP_ROOT/schema-store"

printf '{"schemaVersion":1,"packages":[]}' > "$mal/empty.json"
expect_load_failure "an index with no packages is refused" "lists no packages" \
  --index-url "file://$mal/empty.json" --store-dir "$TMP_ROOT/empty-store"

expect_load_failure "a missing index file is reported" "could not fetch the package index" \
  --index-url "file://$mal/absent.json" --store-dir "$TMP_ROOT/absent-store"

expect_load_failure "--index-url is required" "--index-url is required" --store-dir "$TMP_ROOT/none-store"
expect_load_failure "an index URL with an unknown scheme is refused" "--index-url must start with" \
  --index-url "ftp://example.test/index.json" --store-dir "$TMP_ROOT/ftp-store"

# --- HTTP behaviour through a fake curl --------------------------------------
fake_bin="$TMP_ROOT/bin"
http_serve="$TMP_ROOT/http-serve"
mkdir -p "$fake_bin" "$http_serve"
CURL_LOG="$TMP_ROOT/curl.log"
cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
# Fake curl: maps https?://<host>/<path> onto $HTTP_SERVE_DIR/<path> and logs
# the request plus any config read from stdin (-K -).
out=""; url=""; cfg=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[$i]}" in
    -o) out="${args[$((i + 1))]}" ;;
    -K) cfg="$(cat)" ;;
  esac
  url="${args[$i]}"
done
printf 'GET %s cfg=[%s]\n' "$url" "$cfg" >> "$CURL_LOG"
rel="${url#*://*/}"
[ -f "$HTTP_SERVE_DIR/$rel" ] || exit 22
cp "$HTTP_SERVE_DIR/$rel" "$out"
EOF
chmod +x "$fake_bin/curl"

h1="$(make_entry "$http_serve" "catalog/a-1.0.0.tar.zst" pkg-a 1.0.0 skill '*' "http-a")"
h1="$(printf '%s' "$h1" | sed 's|"url":"catalog/a-1.0.0.tar.zst"|"url":"a-1.0.0.tar.zst"|')"
h2_blob="$(make_entry "$http_serve" "other-host/b-1.0.0.tar.zst" pkg-b 1.0.0 skill '*' "http-b")"
h2="$(printf '%s' "$h2_blob" | sed 's|"url":"other-host/b-1.0.0.tar.zst"|"url":"https://mirror.example.test/other-host/b-1.0.0.tar.zst"|')"
write_index "$http_serve/catalog/index.json" "$h1" "$h2"

http_store="$TMP_ROOT/http-store"
OUT="$(PATH="$fake_bin:$PATH" HTTP_SERVE_DIR="$http_serve" CURL_LOG="$CURL_LOG" GOOSAR_PACKAGE_INDEX_TOKEN="s3cret-token" \
  bash "$LOAD_SCRIPT" --index-url "https://catalog.example.test/catalog/index.json" --store-dir "$http_store" 2>&1)" && STATUS=0 || STATUS=$?
if [ "$STATUS" -eq 0 ] && [ -f "$http_store/provisioning/pkg-a/1.0.0/manifest.json" ] && [ -f "$http_store/provisioning/pkg-b/1.0.0/manifest.json" ]; then
  pass "an https index loads relative and absolute package URLs"
else
  failed "an https index loads relative and absolute package URLs (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
  cat "$CURL_LOG" >&2 || true
fi

if grep -q '^GET https://catalog.example.test/catalog/index.json cfg=\[header = "Authorization: Bearer s3cret-token"\]' "$CURL_LOG" &&
   grep -q '^GET https://catalog.example.test/catalog/a-1.0.0.tar.zst cfg=\[header = "Authorization: Bearer s3cret-token"\]' "$CURL_LOG"; then
  pass "the bearer token is sent to the index origin"
else
  failed "the bearer token is sent to the index origin"
  cat "$CURL_LOG" >&2 || true
fi
if grep -q '^GET https://mirror.example.test/.* cfg=\[\]$' "$CURL_LOG"; then
  pass "the bearer token is not sent to another origin"
else
  failed "the bearer token is not sent to another origin"
  cat "$CURL_LOG" >&2 || true
fi
if grep -q -- 's3cret-token' <<<"$OUT"; then
  failed "the token never appears in the loader output"
else
  pass "the token never appears in the loader output"
fi

OUT="$(PATH="$fake_bin:$PATH" HTTP_SERVE_DIR="$http_serve" CURL_LOG="$CURL_LOG" GOOSAR_PACKAGE_INDEX_TOKEN="s3cret-token" \
  bash "$LOAD_SCRIPT" --index-url "http://catalog.example.test/catalog/index.json" --store-dir "$TMP_ROOT/plain-store" 2>&1)" && STATUS=0 || STATUS=$?
if [ "$STATUS" -ne 0 ] && printf '%s' "$OUT" | grep -q 'plain http'; then
  pass "a token is never sent over plain http to a non-loopback host"
else
  failed "a token is never sent over plain http to a non-loopback host (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

: > "$CURL_LOG"
OUT="$(PATH="$fake_bin:$PATH" HTTP_SERVE_DIR="$http_serve" CURL_LOG="$CURL_LOG" \
  bash "$LOAD_SCRIPT" --index-url "http://catalog.example.test/catalog/index.json" --store-dir "$TMP_ROOT/anon-store" 2>&1)" && STATUS=0 || STATUS=$?
if [ "$STATUS" -eq 0 ] && ! grep -q 'Authorization' "$CURL_LOG"; then
  pass "without a token no Authorization header is sent"
else
  failed "without a token no Authorization header is sent (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

# A remote index may not point the loader at a local file.
h3="$(printf '%s' "$h1" | sed 's|"url":"a-1.0.0.tar.zst"|"url":"file:///etc/hosts"|')"
write_index "$http_serve/catalog/local.json" "$h3"
OUT="$(PATH="$fake_bin:$PATH" HTTP_SERVE_DIR="$http_serve" CURL_LOG="$CURL_LOG" \
  bash "$LOAD_SCRIPT" --index-url "https://catalog.example.test/catalog/local.json" --store-dir "$TMP_ROOT/local-store" 2>&1)" && STATUS=0 || STATUS=$?
if [ "$STATUS" -ne 0 ] && printf '%s' "$OUT" | grep -q 'an http(s) index may not point at'; then
  pass "an http(s) index may not reference file:// package URLs"
else
  failed "an http(s) index may not reference file:// package URLs (status=$STATUS)"
  printf '%s\n' "$OUT" >&2
fi

if [ "$FAILURES" -gt 0 ]; then
  echo "$FAILURES provisioning-packages test(s) failed" >&2
  exit 1
fi
echo "All provisioning-packages tests passed"
