#!/usr/bin/env bash
# Goosar — SBOM and vulnerability scanning for a release (issue #385).
#
# Called by scripts/release-local.sh after the images are built. Kept a
# separate script on purpose: this way the step is exercisable — and IS
# exercised, in scripts/release-local.test.sh — without docker, gh, goreleaser
# or a real tag.
#
# What it produces in --out:
#
#   sbom-<name>-<tag>.cyclonedx.json   syft SBOM per image, CycloneDX JSON
#   sbom-source-<tag>.cyclonedx.json   syft SBOM of the repository source
#   scan-<name>-<tag>.json             trivy report per image (vuln + misconfig)
#   scan-source-<tag>.json             trivy report of the repository source
#
# Severity gate: HIGH and CRITICAL are the threshold. Findings do NOT abort the
# release. They set SCAN_STATUS=findings, which release-local.sh turns into a
# MISSING ledger line — the run is reported as PARTIAL and the operator decides
# whether to ship with a recorded exception or to fix first. A release pipeline
# that dies on a fresh CVE in a base image strands a delivery for reasons the
# operator cannot fix in the same hour; one that stays silent about it is worse.
#
# A missing syft or trivy is likewise not a failure: the step reports
# `skipped-tool-missing`, prints the install page, and the ledger says so. It is
# never a silent skip.
#
# Summary written to stdout for the caller to parse:
#
#   SBOM_STATUS=ok|partial|skipped-tool-missing
#   SBOM_FILES=<count>
#   SCAN_STATUS=ok|findings|partial|skipped-tool-missing
#   SCAN_FINDINGS=<count of HIGH+CRITICAL>
#
# Exit code is always 0 — the caller owns the release verdict.
#
# Usage:
#   bash scripts/sbom-scan.sh --tag vX.Y.Z --out DIR [--image IMG]... [--source-dir DIR]
#
# Install pages (the only two references this step points at):
#   https://github.com/anchore/syft
#   https://trivy.dev/latest/docs/target/container_image/
#   https://trivy.dev/latest/docs/target/filesystem/
set -uo pipefail

info() { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33mWARN:\033[0m %s\n' "$1" >&2; }

TAG=""
OUT=""
SOURCE_DIR=""
IMAGES=()

while [ $# -gt 0 ]; do
  case "$1" in
    --tag) shift; TAG="${1:?--tag needs a value}" ;;
    --out) shift; OUT="${1:?--out needs a value}" ;;
    --image) shift; IMAGES+=("${1:?--image needs a value}") ;;
    --source-dir) shift; SOURCE_DIR="${1:?--source-dir needs a value}" ;;
    --help|-h) sed -n '2,/^set -uo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) printf 'ERROR: unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
  shift
done

[ -n "$TAG" ] || { printf 'ERROR: --tag is required\n' >&2; exit 1; }
[ -n "$OUT" ] || { printf 'ERROR: --out is required\n' >&2; exit 1; }
mkdir -p "$OUT"

# slug <image-ref> — the file-name stem: last path segment, tag stripped.
slug() {
  local s="${1##*/}"
  printf '%s' "${s%%:*}"
}

SBOM_STATUS=""
SBOM_FILES=0
SCAN_STATUS=""
SCAN_FINDINGS=0

# --- SBOM (syft) -------------------------------------------------------------
if ! command -v syft >/dev/null 2>&1; then
  warn "syft not installed — no SBOM for $TAG. Install it and re-run this step: https://github.com/anchore/syft"
  SBOM_STATUS="skipped-tool-missing"
else
  SBOM_STATUS="ok"
  for image in ${IMAGES[@]+"${IMAGES[@]}"}; do
    target="$OUT/sbom-$(slug "$image")-$TAG.cyclonedx.json"
    info "syft $image"
    if syft "$image" -o "cyclonedx-json=$target" && [ -s "$target" ]; then
      SBOM_FILES=$((SBOM_FILES + 1))
    else
      warn "syft failed for $image — no SBOM for that image"
      SBOM_STATUS="partial"
    fi
  done
  if [ -n "$SOURCE_DIR" ]; then
    target="$OUT/sbom-source-$TAG.cyclonedx.json"
    info "syft dir:$SOURCE_DIR"
    if syft "dir:$SOURCE_DIR" -o "cyclonedx-json=$target" && [ -s "$target" ]; then
      SBOM_FILES=$((SBOM_FILES + 1))
    else
      warn "syft failed for the repository source — no source SBOM"
      SBOM_STATUS="partial"
    fi
  fi
fi

# --- scan (trivy) ------------------------------------------------------------
# count_findings <report> — HIGH/CRITICAL entries in a trivy JSON report.
# grep, not jq: the release machine is not guaranteed to have jq, and this
# only ever needs a count.
count_findings() {
  [ -s "$1" ] || { printf '0'; return; }
  grep -o '"Severity":[[:space:]]*"\(HIGH\|CRITICAL\)"' "$1" | wc -l | tr -d '[:space:]'
}

# The accepted no-fix findings (#520) live next to the sources, not next to
# whatever directory the release was launched from, so name the file instead
# of relying on trivy picking one up from the current directory. The YAML form
# is what carries the path scope (source scan only) and the expiry.
#
# "Next to the sources" means SOURCE_DIR (the tag worktree release-local.sh
# scans), not wherever this script itself happens to live. The two are the
# same directory only when the operator's own checkout HEAD matches the tag
# under release; any drift there used to make trivy silently filter against a
# stale .trivyignore.yaml instead of the tag's committed one, re-reporting
# findings the tag had already fixed or accepted (#520 follow-up). Without
# --source-dir (the image-only test path) fall back to this script's own repo.
if [ -n "$SOURCE_DIR" ]; then
  IGNORE_FILE="$SOURCE_DIR/.trivyignore.yaml"
else
  IGNORE_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.trivyignore.yaml"
fi
# trivy exits fatally on an --ignorefile that does not exist ("ignore file not
# found"), which would record a FAILED scan for a reason that has nothing to do
# with vulnerabilities — this script gets copied around (offline kits, extracted
# tarballs). Scan unfiltered instead, and say so.
if [ ! -f "$IGNORE_FILE" ]; then
  warn "no .trivyignore.yaml at $IGNORE_FILE — scanning unfiltered; the accepted no-fix findings (#520) will be reported again"
  IGNORE_FILE=/dev/null
fi

if ! command -v trivy >/dev/null 2>&1; then
  warn "trivy not installed — images and source were NOT scanned for $TAG. Install it and re-run this step: https://trivy.dev/latest/docs/target/container_image/"
  SCAN_STATUS="skipped-tool-missing"
else
  SCAN_STATUS="ok"
  for image in ${IMAGES[@]+"${IMAGES[@]}"}; do
    report="$OUT/scan-$(slug "$image")-$TAG.json"
    info "trivy image $image (HIGH,CRITICAL; vuln + misconfig)"
    if trivy image --scanners vuln,misconfig --severity HIGH,CRITICAL \
        --ignorefile "$IGNORE_FILE" \
        --format json --output "$report" "$image"; then
      SCAN_FINDINGS=$((SCAN_FINDINGS + $(count_findings "$report")))
    else
      warn "trivy failed for $image — that image is unscanned"
      SCAN_STATUS="partial"
    fi
  done
  if [ -n "$SOURCE_DIR" ]; then
    report="$OUT/scan-source-$TAG.json"
    info "trivy fs $SOURCE_DIR (HIGH,CRITICAL; vuln + misconfig)"
    # https://trivy.dev/latest/docs/target/filesystem/
    if trivy fs --scanners vuln,misconfig --severity HIGH,CRITICAL \
        --ignorefile "$IGNORE_FILE" \
        --format json --output "$report" "$SOURCE_DIR"; then
      SCAN_FINDINGS=$((SCAN_FINDINGS + $(count_findings "$report")))
    else
      warn "trivy failed for the repository source — the source is unscanned"
      SCAN_STATUS="partial"
    fi
  fi
  if [ "$SCAN_STATUS" = "ok" ] && [ "$SCAN_FINDINGS" -gt 0 ]; then
    warn "$SCAN_FINDINGS HIGH/CRITICAL finding(s) for $TAG — reports are in $OUT. The release continues and is marked PARTIAL: fix them, or record an exception with a justification before handing the delivery over."
    SCAN_STATUS="findings"
  fi
fi

printf 'SBOM_STATUS=%s\n' "$SBOM_STATUS"
printf 'SBOM_FILES=%s\n' "$SBOM_FILES"
printf 'SCAN_STATUS=%s\n' "$SCAN_STATUS"
printf 'SCAN_FINDINGS=%s\n' "$SCAN_FINDINGS"
exit 0
