#!/usr/bin/env bash
# Tests for offline/offline-manifest.sh (issue #443, acceptance criterion 4).
# Hermetic: fixture files in a temp dir, no docker, no network, no jq/node.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/offline/offline-manifest.sh"

FAILURES=0
pass() { printf 'ok   %s\n' "$1"; }
failed() { printf 'FAIL %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# make_carry <dir> — assemble one artifact of every expected class.
make_carry() {
  local dir="$1"
  mkdir -p "$dir/kit-images" "$dir/desktop" "$dir/cli" "$dir/helm"
  printf 'fake docker save tarball\n' > "$dir/kit-images/release-images.tar"
  # Real image refs: the delivery mode reads this list to prove the tar in the
  # directory belongs to the tag being handed over.
  printf 'pgvector/pgvector:pg17\nghcr.io/adanman/goosar-backend:v1.0.0\nghcr.io/adanman/goosar-web:v1.0.0\n' > "$dir/kit-images/images.list"
  printf 'fake dmg\n' > "$dir/desktop/Goosar-1.0.0-arm64.dmg"
  printf 'fake cli archive\n' > "$dir/cli/goosar_1.0.0_darwin_arm64.tar.gz"
  printf 'fake helm chart\n' > "$dir/helm/goosar-1.0.0.tgz"
}

# assert_verify_fails <label> <dir> <manifest> <expected substring>
assert_verify_fails() {
  local label="$1" dir="$2" manifest="$3" want="$4" out
  if out="$(bash "$SCRIPT" verify --dir "$dir" --manifest "$manifest" 2>&1)"; then
    failed "$label — verify exited 0: $out"
  elif printf '%s' "$out" | grep -q "$want"; then
    pass "$label"
  else
    failed "$label — verify failed without naming $want: $out"
  fi
}

# --- generate: full carry directory -----------------------------------------
CARRY="$TMP_ROOT/carry"
make_carry "$CARRY"
MANIFEST="$TMP_ROOT/manifest.txt"

if out="$(bash "$SCRIPT" generate --tag v1.0.0 --dir "$CARRY" --out "$MANIFEST" 2>&1)"; then
  pass "generate exits 0 on a complete carry directory"
else
  failed "generate failed on a complete carry directory: $out"
fi

if [ -f "$MANIFEST" ]; then
  pass "generate writes the manifest file"
else
  failed "generate wrote no manifest at $MANIFEST"
fi

if grep -q '^tag=v1.0.0$' "$MANIFEST"; then
  pass "manifest records the tag"
else
  failed "manifest does not record tag=v1.0.0"
fi

for kind in images desktop cli helm; do
  if grep -q "kind=$kind " "$MANIFEST"; then
    pass "manifest classifies $kind artifacts"
  else
    failed "manifest has no kind=$kind row: $(cat "$MANIFEST")"
  fi
done

if grep -qE 'sha256=[0-9a-f]{64}' "$MANIFEST"; then
  pass "manifest records sha256 digests"
else
  failed "manifest has no 64-hex sha256 digests"
fi

# The manifest must never list itself — otherwise verify can never pass, since
# hashing the manifest is what produces the digest inside it.
if grep -q "$(basename "$MANIFEST")" "$MANIFEST"; then
  failed "manifest lists itself"
else
  pass "manifest does not list itself"
fi

# --- verify: happy path ------------------------------------------------------
if out="$(bash "$SCRIPT" verify --dir "$CARRY" --manifest "$MANIFEST" 2>&1)"; then
  pass "verify accepts an intact carry directory"
else
  failed "verify rejected an intact carry directory: $out"
fi

# --- verify: corrupted file --------------------------------------------------
CORRUPT="$TMP_ROOT/corrupt"
cp -R "$CARRY" "$CORRUPT"
# Same byte count on purpose, so the size check cannot mask the digest check.
printf 'FAKE DMG\n' > "$CORRUPT/desktop/Goosar-1.0.0-arm64.dmg"
if out="$(bash "$SCRIPT" verify --dir "$CORRUPT" --manifest "$MANIFEST" 2>&1)"; then
  failed "verify accepted a carry directory with a tampered artifact"
else
  if printf '%s' "$out" | grep -q 'CHECKSUM MISMATCH'; then
    pass "verify names the checksum mismatch"
  else
    failed "verify failed without naming CHECKSUM MISMATCH: $out"
  fi
fi

# --- verify: missing file ----------------------------------------------------
INCOMPLETE="$TMP_ROOT/incomplete"
cp -R "$CARRY" "$INCOMPLETE"
rm -f "$INCOMPLETE/desktop/Goosar-1.0.0-arm64.dmg"
if out="$(bash "$SCRIPT" verify --dir "$INCOMPLETE" --manifest "$MANIFEST" 2>&1)"; then
  failed "verify accepted a carry directory missing the DMG"
else
  if printf '%s' "$out" | grep -q 'MISSING'; then
    pass "verify names the missing artifact"
  else
    failed "verify failed without naming MISSING: $out"
  fi
fi

# --- generate --strict: incomplete carry directory ---------------------------
PARTIAL="$TMP_ROOT/partial"
mkdir -p "$PARTIAL/kit-images"
printf 'fake docker save tarball\n' > "$PARTIAL/kit-images/release-images.tar"
if out="$(bash "$SCRIPT" generate --tag v1.0.0 --dir "$PARTIAL" --out "$TMP_ROOT/partial.txt" --strict 2>&1)"; then
  failed "generate --strict accepted a carry directory with no desktop/cli/helm"
else
  if printf '%s' "$out" | grep -q 'desktop'; then
    pass "generate --strict names the artifact classes that are absent"
  else
    failed "generate --strict failed without naming the absent classes: $out"
  fi
fi

# Without --strict the same directory still produces a manifest, so an
# operator carrying only part of the release can still hand over a checkable
# list.
if bash "$SCRIPT" generate --tag v1.0.0 --dir "$PARTIAL" --out "$TMP_ROOT/partial.txt" >/dev/null 2>&1; then
  pass "generate without --strict tolerates a partial carry directory"
else
  failed "generate without --strict rejected a partial carry directory"
fi

# --- verify: manifest that lost its trailing newline -------------------------
# A manifest truncated at the last byte used to make `read` drop the final row
# entirely, so a genuinely missing artifact went unreported and verify passed.
NONEWLINE="$TMP_ROOT/manifest-nonewline.txt"
printf '%s' "$(cat "$MANIFEST")" > "$NONEWLINE"
LAST_ROW_DIR="$TMP_ROOT/last-row-missing"
cp -R "$CARRY" "$LAST_ROW_DIR"
LAST_FILE="$(sed -n '$s/.* file=\(.*\) size=.*/\1/p' "$MANIFEST")"
[ -n "$LAST_FILE" ] || failed "could not read the last artifact row out of $MANIFEST"
rm -f "$LAST_ROW_DIR/$LAST_FILE"
assert_verify_fails "verify still checks the last row of a manifest with no trailing newline" \
  "$LAST_ROW_DIR" "$NONEWLINE" 'MISSING'

# --- verify: manifest that lost rows in transit ------------------------------
# Every row left in a truncated manifest describes an intact file, so only the
# declared artifact count can catch this.
TRUNCATED="$TMP_ROOT/manifest-truncated.txt"
sed '$d' "$MANIFEST" > "$TRUNCATED"
assert_verify_fails "verify refuses a manifest with fewer rows than it declares" \
  "$CARRY" "$TRUNCATED" 'TRUNCATED MANIFEST'

# --- classify: the Windows CLI archive is cli, not desktop -------------------
WINDIR="$TMP_ROOT/windows"
mkdir -p "$WINDIR"
printf 'fake windows cli\n' > "$WINDIR/goosar_1.0.0_windows_amd64.zip"
printf 'fake windows desktop\n' > "$WINDIR/Goosar-Setup-1.0.0.exe"
bash "$SCRIPT" generate --tag v1.0.0 --dir "$WINDIR" --out "$TMP_ROOT/windows.txt" >/dev/null 2>&1
if grep -q '^kind=cli file=goosar_1.0.0_windows_amd64.zip ' "$TMP_ROOT/windows.txt"; then
  pass "the Windows CLI zip is classified as cli"
else
  failed "the Windows CLI zip was not classified as cli: $(cat "$TMP_ROOT/windows.txt")"
fi

# --- verify: empty manifest is not a pass ------------------------------------
: > "$TMP_ROOT/empty.txt"
if bash "$SCRIPT" verify --dir "$CARRY" --manifest "$TMP_ROOT/empty.txt" >/dev/null 2>&1; then
  failed "verify treated an empty manifest as a successful check"
else
  pass "verify refuses an empty manifest"
fi

# --- delivery: the hand-over list for the acceptance act (issue #437) --------
# The contract requires ONE document naming everything that was delivered under
# a tag. `delivery` renders it from the same carry directory `generate` walks,
# plus the facts an acceptance review asks for by name: image digests, chart
# version, migration range, the daemon floor, SBOM and notices files.
DELIVERY_CARRY="$TMP_ROOT/delivery"
make_carry "$DELIVERY_CARRY"
printf '{"bomFormat":"CycloneDX"}\n' > "$DELIVERY_CARRY/kit-images/sbom-goosar-backend-v1.0.0.cyclonedx.json"
printf '{"Results":[]}\n' > "$DELIVERY_CARRY/kit-images/scan-goosar-backend-v1.0.0.json"
DIGESTS="$TMP_ROOT/digests.txt"
cat > "$DIGESTS" <<'DIG'
ghcr.io/adanman/goosar-backend:v1.0.0 sha256:aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999
ghcr.io/adanman/goosar-web:v1.0.0 sha256:9999888877776666555544443333222211110000ffffeeeeddddccccbbbbaaaa
DIG
NOTICES="$TMP_ROOT/notices.md"
printf '# notices\n' > "$NOTICES"
DELIVERY_OUT="$TMP_ROOT/DELIVERY-MANIFEST.md"

if out="$(bash "$SCRIPT" delivery --tag v1.0.0 --dir "$DELIVERY_CARRY" --out "$DELIVERY_OUT" \
    --digests "$DIGESTS" --notices "$NOTICES" --chart-version 1.0.0 \
    --min-daemon-version 0.2.21 --migration-range 001..296 2>&1)"; then
  pass "delivery exits 0 on a complete carry directory"
else
  failed "delivery failed: $out"
fi

for want in \
  'v1.0.0' \
  'release-images.tar' \
  'sha256:aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999' \
  '1.0.0' \
  '0.2.21' \
  '001..296' \
  'sbom-goosar-backend-v1.0.0.cyclonedx.json' \
do
  if grep -qF "$want" "$DELIVERY_OUT"; then
    pass "delivery manifest names $want"
  else
    failed "delivery manifest is missing $want"
  fi
done

# Every artifact row carries its checksum: the act is what the customer signs,
# so "the file with this hash" has to be checkable years later.
if grep -qE '^\| `kit-images/release-images.tar` \|' "$DELIVERY_OUT" && \
   [ "$(grep -c '[0-9a-f]\{64\}' "$DELIVERY_OUT")" -ge 3 ]; then
  pass "delivery manifest lists artifacts with sha256"
else
  failed "delivery manifest does not list artifacts with sha256:"
  sed 's/^/    /' "$DELIVERY_OUT" >&2
fi

if grep -q '^## Покрытие платформ' "$DELIVERY_OUT"; then
  failed "delivery manifest carries a package coverage matrix the delivery does not have"
else
  pass "delivery manifest has no package coverage matrix"
fi

# It is Markdown, not the flat carry manifest: the two documents answer
# different questions and must not be confused for each other.
if head -n 1 "$DELIVERY_OUT" | grep -q '^# '; then
  pass "delivery manifest is a Markdown document"
else
  failed "delivery manifest is not Markdown: $(head -n 1 "$DELIVERY_OUT")"
fi

# Defaults: without the optional flags the generator derives what it can from
# the repository instead of leaving the act half-empty.
DELIVERY_DEFAULTS="$TMP_ROOT/DELIVERY-DEFAULTS.md"
if out="$(bash "$SCRIPT" delivery --tag v9.9.9 --dir "$DELIVERY_CARRY" --out "$DELIVERY_DEFAULTS" 2>&1)"; then
  pass "delivery exits 0 without the optional flags"
else
  failed "delivery failed without the optional flags: $out"
fi
if grep -q '9\.9\.9' "$DELIVERY_DEFAULTS"; then
  pass "delivery derives the chart version from the tag"
else
  failed "delivery did not derive the chart version from the tag"
fi
if grep -qE '[0-9]{3}\.\.[0-9]{3}' "$DELIVERY_DEFAULTS"; then
  pass "delivery derives the migration range from server/migrations"
else
  failed "delivery did not derive the migration range"
fi

# The hand-over list must name what THIS release produced and nothing else.
# Two ways the generator used to over-claim, both against a document a customer
# signs an acceptance act on:
#   - offline/kit/ is the perimeter BUILD kit, and its go-vendor.tar.gz / npm
#     .tgz packages were filed as `cli` and `helm` deliverables;
#   - kit-images/ is not cleaned between releases, so an earlier tag's SBOM and
#     trivy report were listed as this release's scan evidence.
STALE_CARRY="$TMP_ROOT/stale"
make_carry "$STALE_CARRY"
mkdir -p "$STALE_CARRY/kit/context"
printf 'go vendor tree\n' > "$STALE_CARRY/kit/go-vendor.tar.gz"
printf 'npm package\n' > "$STALE_CARRY/kit/context/typescript-5.9.2.tgz"
printf '{"bomFormat":"CycloneDX"}\n' > "$STALE_CARRY/kit-images/sbom-goosar-backend-v0.9.0.cyclonedx.json"
printf '{"Results":[]}\n' > "$STALE_CARRY/kit-images/scan-goosar-backend-v0.9.0.json"
printf '{"bomFormat":"CycloneDX"}\n' > "$STALE_CARRY/kit-images/sbom-goosar-backend-v1.0.0.cyclonedx.json"
STALE_OUT="$TMP_ROOT/DELIVERY-STALE.md"
bash "$SCRIPT" delivery --tag v1.0.0 --dir "$STALE_CARRY" --out "$STALE_OUT" >/dev/null 2>&1

if grep -q 'go-vendor.tar.gz' "$STALE_OUT" || grep -q 'typescript-5.9.2.tgz' "$STALE_OUT"; then
  failed "delivery lists the perimeter build kit as a deliverable"
else
  pass "delivery excludes offline/kit (the build kit, not the delivery)"
fi

if grep -q 'v0\.9\.0' "$STALE_OUT"; then
  failed "delivery lists another tag's SBOM/scan reports as this release's"
else
  pass "delivery ignores SBOM/scan reports from another tag"
fi

if grep -q 'sbom-goosar-backend-v1.0.0.cyclonedx.json' "$STALE_OUT"; then
  pass "delivery still lists this tag's SBOM"
else
  failed "delivery dropped this tag's SBOM"
fi

# images.list and the images tar are written together, so a list naming no
# image of this tag proves the tar is left over from an earlier release. The
# document has to say so rather than print its checksum silently.
MISMATCH_OUT="$TMP_ROOT/DELIVERY-MISMATCH.md"
mismatch_err="$(bash "$SCRIPT" delivery --tag v9.9.9 --dir "$STALE_CARRY" --out "$MISMATCH_OUT" 2>&1 >/dev/null)"
if grep -q 'предыдущего выпуска' "$MISMATCH_OUT" && printf '%s' "$mismatch_err" | grep -q 'EARLIER release'; then
  pass "delivery flags an images tar left over from an earlier release"
else
  failed "delivery signed off a stale images tar silently: $mismatch_err"
fi
if grep -q 'предыдущего выпуска' "$DELIVERY_OUT"; then
  failed "delivery flags a matching images tar as stale"
else
  pass "delivery does not cry stale on a matching images tar"
fi

# A missing carry directory is an error, not an empty document.
if bash "$SCRIPT" delivery --tag v1.0.0 --dir "$TMP_ROOT/nope" --out "$TMP_ROOT/x.md" >/dev/null 2>&1; then
  failed "delivery accepted a missing carry directory"
else
  pass "delivery refuses a missing carry directory"
fi

if [ "$FAILURES" -gt 0 ]; then
  printf '\n%d test(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf '\nAll offline-manifest tests passed\n'
