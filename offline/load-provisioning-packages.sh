#!/usr/bin/env bash
# Goosar — load a package catalog from an HTTP index into the backend store.
#
# The package catalog (skills, MCP servers, runtimes) is an extension point:
# it is not part of the product delivery. An operator points this script at an
# index served over HTTP(S); the script downloads every package the index
# lists, verifies each blob against the sha256 (and size) the index declares,
# and lays the verified packages into the on-disk store the backend reads.
# The index format is documented in docs/package-index.md.
#
# Store layout (must stay byte-for-byte aligned with
# server/internal/provisioning/local.go's LocalPackageStore, which reads from
# the SAME storage.Storage backend as attachments — $LOCAL_UPLOAD_DIR, default
# "./data/uploads" — under a prefix, $GOOSAR_PROVISIONING_LOCAL_PREFIX,
# default "provisioning"):
#
#   <base>/<prefix>/catalog.json                    — []{name,version}
#   <base>/<prefix>/<name>/<version>/manifest.json
#   <base>/<prefix>/<name>/<version>/<name>-<version>-<platform>.tar.zst
#
# Usage:
#   bash offline/load-provisioning-packages.sh --index-url URL [--store-dir DIR] [--prefix PREFIX]
#
# --index-url  URL of the index JSON (https://, http:// or file://). Relative
#              package URLs in the index resolve against this URL.
# --store-dir  defaults to $LOCAL_UPLOAD_DIR, or ./data/uploads if unset —
#              the SAME variable and default the backend's storage.Storage uses.
# --prefix     defaults to $GOOSAR_PROVISIONING_LOCAL_PREFIX, or "provisioning"
#              if unset — the SAME variable and default LocalPackageStore uses.
#
# Environment:
#   GOOSAR_PACKAGE_INDEX_TOKEN  optional bearer token. Sent as an
#       "Authorization: Bearer" header only to URLs on the same origin as the
#       index, and only over https (or http to a loopback host). It is passed
#       to curl through stdin, never on the command line.
#   GOOSAR_PACKAGE_INDEX_TIMEOUT  per-download time limit in seconds (default 600).
#
# Verification: every package is downloaded into a staging directory first and
# checked (64-hex sha256 of the blob bytes equals the index value; byte size
# equals the index value when present). Nothing is written into the store
# unless every package in the index verified.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STORE_DIR="${LOCAL_UPLOAD_DIR:-$ROOT_DIR/data/uploads}"
PREFIX="${GOOSAR_PROVISIONING_LOCAL_PREFIX:-provisioning}"
INDEX_URL=""
TOKEN="${GOOSAR_PACKAGE_INDEX_TOKEN:-}"
TIMEOUT="${GOOSAR_PACKAGE_INDEX_TIMEOUT:-600}"

while [ $# -gt 0 ]; do
  case "$1" in
    --index-url) INDEX_URL="${2:?--index-url requires a value}"; shift ;;
    --store-dir) STORE_DIR="${2:?--store-dir requires a value}"; shift ;;
    --prefix) PREFIX="${2:?--prefix requires a value}"; shift ;;
    --help|-h) sed -n '2,41p' "$ROOT_DIR/offline/load-provisioning-packages.sh" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $1 (see --help)" >&2; exit 1 ;;
  esac
  shift
done

info() { printf '==> %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

[ -n "$INDEX_URL" ] || fail "--index-url is required (see --help and docs/package-index.md)"
case "$INDEX_URL" in
  https://*|http://*|file://*) ;;
  *) fail "--index-url must start with https://, http:// or file:// (got '$INDEX_URL')" ;;
esac
command -v curl >/dev/null 2>&1 || fail "curl not found on PATH"

# Strip a leading/trailing slash from a user- or env-supplied prefix, mirroring
# NewLocalPackageStore's strings.Trim(prefix, "/") in local.go — a stray slash
# here must not silently produce a different store path than the backend.
PREFIX="${PREFIX#/}"
PREFIX="${PREFIX%/}"
[ -n "$PREFIX" ] || PREFIX="provisioning"
PREFIX_DIR="$STORE_DIR/$PREFIX"

# sha256 tool portability (macOS ships shasum, Linux sha256sum).
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi

# url_origin <url> — scheme://authority, lowercase. Empty for file://.
url_origin() {
  printf '%s' "$1" | sed -E 's|^([A-Za-z][A-Za-z0-9+.-]*://[^/?#]*).*|\1|' | tr 'A-Z' 'a-z'
}

# url_is_loopback <url> — true for localhost / 127.x / [::1] authorities.
url_is_loopback() {
  local host
  host="$(url_origin "$1" | sed -E 's|^[a-z]+://||; s|^[^@]*@||; s|:[0-9]+$||')"
  case "$host" in localhost|127.*|'[::1]') return 0 ;; esac
  return 1
}

INDEX_ORIGIN="$(url_origin "$INDEX_URL")"
INDEX_BASE="${INDEX_URL%%[?#]*}"
INDEX_BASE="${INDEX_BASE%/*}"

if [ -n "$TOKEN" ]; then
  case "$INDEX_URL" in
    https://*) ;;
    http://*) url_is_loopback "$INDEX_URL" ||
      fail "refusing to send GOOSAR_PACKAGE_INDEX_TOKEN over plain http to a non-loopback host; use an https:// index URL" ;;
    *) fail "GOOSAR_PACKAGE_INDEX_TOKEN is set but the index URL is not http(s)" ;;
  esac
fi

STAGE_DIR="$(mktemp -d)"
cleanup() { rm -rf "$STAGE_DIR"; }
trap cleanup EXIT

# fetch <url> <out-file> — downloads with curl; the bearer token (if any) is
# attached only for the index's own origin and travels through stdin.
fetch() {
  local url="$1" out="$2"
  local -a args=(-fsSL --retry 2 --connect-timeout 15 --max-time "$TIMEOUT" -o "$out")
  if [ -n "$TOKEN" ] && [ "$(url_origin "$url")" = "$INDEX_ORIGIN" ]; then
    printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" | curl -K - "${args[@]}" -- "$url"
  else
    curl "${args[@]}" -- "$url"
  fi
}

INDEX_FILE="$STAGE_DIR/index.json"
info "fetching index $INDEX_URL"
fetch "$INDEX_URL" "$INDEX_FILE" || fail "could not fetch the package index from $INDEX_URL"
[ -s "$INDEX_FILE" ] || fail "the package index at $INDEX_URL is empty"

# Flat-object reader. The index contract keeps every package object free of
# nested braces (its only nested value is the `requires` string array), so
# objects are the innermost {...} runs of the whitespace-flattened document —
# grep/sed only, no jq/node dependency, and a minified index reads the same as
# a pretty-printed one.
FLAT="$(tr '\n\r\t' '   ' < "$INDEX_FILE")"

schema="$(printf '%s' "$FLAT" | grep -o '"schemaVersion"[[:space:]]*:[[:space:]]*[0-9][0-9]*' | head -n1 | sed -E 's/.*:[[:space:]]*//' || true)"
[ "$schema" = "1" ] || fail "unsupported index schemaVersion '${schema:-missing}' (this loader reads schemaVersion 1; see docs/package-index.md)"

# entry_field <entry> <key> — string or number value of a top-level key.
entry_field() {
  local entry="$1" key="$2" line
  line="$(printf '%s' "$entry" | grep -o "\"${key}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"\|\"${key}\"[[:space:]]*:[[:space:]]*[0-9][0-9]*" | head -n1)" || true
  [ -n "$line" ] || return 1
  printf '%s' "$line" | sed -E "s/^\"${key}\"[[:space:]]*:[[:space:]]*//; s/^\"//; s/\"\$//"
}

# entry_requires <entry> — the raw JSON array of the `requires` key, or [].
entry_requires() {
  local raw
  raw="$(printf '%s' "$1" | grep -o '"requires"[[:space:]]*:[[:space:]]*\[[^]]*\]' | head -n1 | sed -E 's/^"requires"[[:space:]]*:[[:space:]]*//')" || true
  [ -n "$raw" ] || raw="[]"
  printf '%s' "$raw"
}

PLATFORMS=" * darwin-arm64 darwin-x64 win-x64 linux-x64 linux-arm64 "
IDENT_RE='^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$'
REQUIRE_RE='^(skill|mcp-server|runtime):[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?@[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$'

# resolve_url <url> — absolute URL for a package URL from the index.
resolve_url() {
  local u="$1"
  case "$u" in
    https://*|http://*|file://*) printf '%s' "$u" ;;
    //*|*://*) return 1 ;;
    /*) printf '%s%s' "$INDEX_ORIGIN" "$u" ;;
    *) printf '%s/%s' "$INDEX_BASE" "$u" ;;
  esac
}

names=(); versions=(); types=(); platforms=(); shas=(); sizes=(); requires_list=(); urls=()

while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  name="$(entry_field "$entry" name || true)"
  version="$(entry_field "$entry" version || true)"
  ptype="$(entry_field "$entry" type || true)"
  platform="$(entry_field "$entry" platform || true)"
  sha="$(entry_field "$entry" sha256 || true)"
  size="$(entry_field "$entry" size || true)"
  url="$(entry_field "$entry" url || true)"
  label="${name:-<unnamed>}@${version:-<no version>}"

  printf '%s' "$name" | grep -Eq "$IDENT_RE" || fail "index entry $label: invalid name '$name'"
  printf '%s' "$version" | grep -Eq "$IDENT_RE" || fail "index entry $label: invalid version '$version'"
  case "$ptype" in skill|mcp-server|runtime) ;; *) fail "index entry $label: type must be skill, mcp-server or runtime (got '$ptype')" ;; esac
  case "$PLATFORMS" in *" $platform "*) ;; *) fail "index entry $label: invalid platform '$platform'" ;; esac
  printf '%s' "$sha" | grep -Eq '^[0-9a-f]{64}$' || fail "index entry $label: sha256 must be 64 lowercase hex characters"
  printf '%s' "$size" | grep -Eq '^[1-9][0-9]*$' || fail "index entry $label: size must be a positive integer"
  [ -n "$url" ] || fail "index entry $label: missing url"
  printf '%s' "$url" | grep -Eq '^[^[:space:]"<>\\{}]+$' || fail "index entry $label: url has characters that are not allowed"

  req="$(entry_requires "$entry")"
  if [ "$req" != "[]" ]; then
    inner="$(printf '%s' "$req" | sed -E 's/^\[[[:space:]]*//; s/[[:space:]]*\]$//')"
    if [ -n "$inner" ]; then
      while IFS= read -r ref; do
        ref="$(printf '%s' "$ref" | sed -E 's/^[[:space:]]*"//; s/"[[:space:]]*$//')"
        printf '%s' "$ref" | grep -Eq "$REQUIRE_RE" || fail "index entry $label: invalid requires entry '$ref' (want type:name@version)"
      done < <(printf '%s\n' "$inner" | tr ',' '\n')
    fi
  fi

  for i in ${names[@]+"${!names[@]}"}; do
    if [ "${names[$i]}" = "$name" ] && [ "${versions[$i]}" = "$version" ]; then
      fail "index lists $name@$version twice — the store holds one package per name and version"
    fi
  done

  abs_url="$(resolve_url "$url")" || fail "index entry $label: unsupported url '$url'"
  case "$INDEX_URL" in
    http://*|https://*) case "$abs_url" in http://*|https://*) ;; *) fail "index entry $label: an http(s) index may not point at '$abs_url'" ;; esac ;;
  esac

  names+=("$name"); versions+=("$version"); types+=("$ptype"); platforms+=("$platform")
  shas+=("$sha"); sizes+=("$size"); requires_list+=("$req"); urls+=("$abs_url")
done < <(printf '%s' "$FLAT" | grep -o '{[^{}]*}' | grep '"sha256"' || true)

# Cross-check the object scanner against the raw key count: a package object it
# could not see (nested braces, a shape the contract does not cover) must stop
# the load instead of shortening the catalog.
sha_keys="$(printf '%s' "$FLAT" | grep -o '"sha256"[[:space:]]*:' | wc -l | tr -d '[:space:]')"
[ "${#names[@]}" -gt 0 ] || fail "the index lists no packages"
[ "${#names[@]}" = "$sha_keys" ] ||
  fail "read ${#names[@]} package(s) but the index has $sha_keys \"sha256\" key(s) — the index is malformed"

info "index lists ${#names[@]} package(s)"

# Phase 1: download + verify into staging. Nothing touches the store yet.
for i in "${!names[@]}"; do
  name="${names[$i]}"; version="${versions[$i]}"; platform="${platforms[$i]}"
  dest_dir="$PREFIX_DIR/$name/$version"
  blob_name="${name}-${version}-${platform}.tar.zst"
  staged="$STAGE_DIR/$i.tar.zst"

  # Resume: a package already in the store with the same digest is kept as is.
  if [ -f "$dest_dir/$blob_name" ] && [ -f "$dest_dir/manifest.json" ]; then
    existing="$( ($SHA256 "$dest_dir/$blob_name") | awk '{print $1}')"
    if [ "$existing" = "${shas[$i]}" ]; then
      info "already present $name@$version ($platform)"
      cp "$dest_dir/$blob_name" "$staged"
      continue
    fi
  fi

  info "downloading $name@$version ($platform)"
  fetch "${urls[$i]}" "$staged" || fail "could not download $name@$version from ${urls[$i]}"
  actual="$( ($SHA256 "$staged") | awk '{print $1}')"
  [ "$actual" = "${shas[$i]}" ] ||
    fail "sha256 mismatch for $name@$version: index declares ${shas[$i]}, downloaded bytes hash to $actual — refusing to load"
  actual_size="$(wc -c < "$staged" | tr -d '[:space:]')"
  [ "$actual_size" = "${sizes[$i]}" ] ||
    fail "size mismatch for $name@$version: index declares ${sizes[$i]}, downloaded $actual_size — refusing to load"
done

# Phase 2: install under the exact keys LocalPackageStore reads.
mkdir -p "$PREFIX_DIR"
for i in "${!names[@]}"; do
  name="${names[$i]}"; version="${versions[$i]}"; platform="${platforms[$i]}"
  dest_dir="$PREFIX_DIR/$name/$version"
  mkdir -p "$dest_dir"
  cp "$STAGE_DIR/$i.tar.zst" "$dest_dir/${name}-${version}-${platform}.tar.zst"
  printf '{"schemaVersion":1,"name":"%s","version":"%s","type":"%s","platform":"%s","sha256":"%s","size":%s,"requires":%s}\n' \
    "$name" "$version" "${types[$i]}" "$platform" "${shas[$i]}" "${sizes[$i]}" "${requires_list[$i]}" > "$dest_dir/manifest.json"
  info "loaded $name@$version ($platform) -> $dest_dir"
done

# Rebuild catalog.json from the store tree itself rather than trusting (or
# incrementally patching) a possibly-stale existing file: LocalPackageStore.List
# fails closed on any catalog entry with no matching manifest, so the catalog
# must always exactly mirror what is actually installed, including packages
# loaded by a previous run. Package names/versions are restricted to
# ValidPackageIdentifier's charset (checked above), so plain string
# interpolation into JSON needs no escaping.
catalog_body=""
sep=""
while IFS= read -r manifest_path; do
  rel="${manifest_path#"$PREFIX_DIR"/}"
  version_dir="$(dirname "$rel")"
  cat_version="$(basename "$version_dir")"
  cat_name="$(dirname "$version_dir")"
  catalog_body="${catalog_body}${sep}{\"name\":\"${cat_name}\",\"version\":\"${cat_version}\"}"
  sep=","
done < <(find "$PREFIX_DIR" -mindepth 3 -maxdepth 3 -type f -name manifest.json | LC_ALL=C sort)
printf '[%s]' "$catalog_body" > "$PREFIX_DIR/catalog.json"

info "Loaded ${#names[@]} package(s) into $PREFIX_DIR"
info "catalog.json now lists $(find "$PREFIX_DIR" -mindepth 3 -maxdepth 3 -type f -name manifest.json | wc -l | tr -d ' ') package(s) total"
info "Set GOOSAR_PROVISIONING_STORE=local, LOCAL_UPLOAD_DIR=$STORE_DIR, and"
info "GOOSAR_PROVISIONING_LOCAL_PREFIX=$PREFIX (or accept the defaults, which"
info "already match) for the backend to read them (see SELF_HOSTING.md, «Каталог пакетов (provisioning store)»)."
