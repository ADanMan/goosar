#!/usr/bin/env bash
# Goosar — offline delivery manifest (issue #443, criterion 4).
#
# One list of everything that has to cross the air gap for a given release,
# with a sha256 per file, plus a checker that validates a carried directory on
# the other side. Answers the two questions an operator actually has:
#
#   "did I put everything on the drive?"   -> generate --strict
#   "did everything arrive intact?"        -> verify
#
# Artifact classes (every air-gapped install needs all four):
#
#   images    deployment images exported by offline/save-release-images.sh
#             (*.tar, images.list)
#   desktop   the desktop client build (*.dmg / *.zip / *.exe / *.AppImage /
#             *.deb / *.rpm)
#   cli       goosar CLI archives (goosar_*.tar.gz / *.zip)
#   helm      the packaged Helm chart (*.tgz) — only for Kubernetes installs
#
# A third mode, `delivery`, renders DELIVERY-MANIFEST.md — the single hand-over
# list an acceptance act refers to (issue #437). It answers a different question
# from the two above: not "did the drive arrive intact" but "what, exactly, was
# delivered under this tag" — artifacts with checksums, image digests, chart
# version, migration range, the daemon version floor, the SBOM files and the
# third-party notices.
#
# Usage:
#   bash offline/offline-manifest.sh generate --tag vX.Y.Z --dir DIR [--out FILE] [--strict]
#   bash offline/offline-manifest.sh verify   --dir DIR --manifest FILE
#   bash offline/offline-manifest.sh delivery --tag vX.Y.Z --dir DIR [--out FILE]
#                                             [--digests FILE] [--notices FILE]
#                                             [--chart-version V]
#                                             [--min-daemon-version V]
#                                             [--migration-range A..B]
#
# --dir                 the staging/carry directory holding the artifacts.
# --out                 manifest path (default: <dir>/offline-manifest.txt, or
#                       <dir>/DELIVERY-MANIFEST.md for `delivery`).
# --strict              generate exits non-zero when an artifact class has no files.
# --digests FILE        `delivery`: lines of "<image ref> <sha256:digest>".
# --notices FILE        `delivery`: the third-party notices file to name
#                       (optional).
# --chart-version       `delivery`: default is the tag without its leading v.
# --min-daemon-version  `delivery`: default is read from server/pkg/agent/version.go.
# --migration-range     `delivery`: default is derived from server/migrations.
#
# The manifest is a flat text file on purpose: it has to be readable and
# checkable inside a perimeter with no jq, no node and no network. The delivery
# document is Markdown because a human signs it.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODE="${1:-}"
[ -n "$MODE" ] && shift || true

TAG=""
DIR=""
OUT=""
MANIFEST=""
STRICT=0
DIGESTS=""
NOTICES=""
CHART_VERSION=""
MIN_DAEMON_VERSION=""
MIGRATION_RANGE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="${2:?--tag requires a value}"; shift ;;
    --dir) DIR="${2:?--dir requires a value}"; shift ;;
    --out) OUT="${2:?--out requires a value}"; shift ;;
    --manifest) MANIFEST="${2:?--manifest requires a value}"; shift ;;
    --strict) STRICT=1 ;;
    --digests) DIGESTS="${2:?--digests requires a value}"; shift ;;
    --notices) NOTICES="${2:?--notices requires a value}"; shift ;;
    --chart-version) CHART_VERSION="${2:?--chart-version requires a value}"; shift ;;
    --min-daemon-version) MIN_DAEMON_VERSION="${2:?--min-daemon-version requires a value}"; shift ;;
    --migration-range) MIGRATION_RANGE="${2:?--migration-range requires a value}"; shift ;;
    # Stop at the `set -euo` sentinel instead of a line range: a line range
    # truncated this same help block in release-local.sh once already.
    --help|-h) sed -n '2,/^set -euo/p' "$ROOT_DIR/offline/offline-manifest.sh" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $1 (see --help)" >&2; exit 1 ;;
  esac
  shift
done

info() { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
warn_stale() {
  warn "images.list names no image tagged $1 — the images tar in this directory is from an EARLIER release; the delivery manifest says so and must not be signed as is"
}

# sha256 tool portability: macOS ships shasum, Linux sha256sum (issue #141).
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
else
  SHA256="shasum -a 256"
fi
sha256_of() { ($SHA256 "$1") | awk '{print $1}'; }

size_of() {
  # BSD stat (macOS) and GNU stat disagree on flags; wc -c works on both and
  # needs no branching.
  wc -c < "$1" | tr -d '[:space:]'
}

# classify <relative-path> — map a file onto an artifact class, or print
# nothing for a file that is not part of the delivery (checksums, notes, the
# manifest itself).
classify() {
  case "$1" in
    *offline-manifest*) return ;;
    */checksums.txt|checksums.txt) return ;;
    *.tar) printf 'images' ;;
    */images.list|images.list) printf 'images' ;;
    # .deb and .rpm join the list with the multi-platform desktop builds
    # (issue #401): a Linux hand-over the acceptance act does not list is a
    # hand-over nobody signed for.
    *.dmg|*.exe|*.AppImage|*.deb|*.rpm) printf 'desktop' ;;
    # The Windows CLI archive is a .zip too, so it has to be matched before
    # the desktop catch-all — otherwise a Windows carry set files the CLI
    # under `desktop` and --strict reports an empty `cli` class.
    goosar_*.zip|*/goosar_*.zip) printf 'cli' ;;
    *.zip) printf 'desktop' ;;
    *.tgz) printf 'helm' ;;
    *.tar.gz) printf 'cli' ;;
    *) return ;;
  esac
}

KINDS="images desktop cli helm"

generate() {
  [ -n "$DIR" ] || fail "generate needs --dir DIR"
  [ -d "$DIR" ] || fail "carry directory not found: $DIR"
  [ -n "$TAG" ] || TAG="$(git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null || true)"
  [ -n "$TAG" ] || fail "no --tag given and no release tag found via git describe"
  [ -n "$OUT" ] || OUT="$DIR/offline-manifest.txt"

  local tmp rows
  tmp="$(mktemp)"
  rows="$(mktemp)"

  local count=0
  # -print0/read -d '' keeps paths with spaces intact.
  while IFS= read -r -d '' file; do
    local rel kind
    rel="${file#"$DIR"/}"
    kind="$(classify "$rel")"
    [ -n "$kind" ] || continue
    printf 'kind=%s file=%s size=%s sha256=%s\n' \
      "$kind" "$rel" "$(size_of "$file")" "$(sha256_of "$file")" >> "$rows"
    count=$((count + 1))
  done < <(find "$DIR" -type f -print0 | LC_ALL=C sort -z)

  [ "$count" -gt 0 ] || { rm -f "$tmp" "$rows"; fail "no deliverable artifacts found under $DIR"; }

  # `artifacts=` is what makes verify fail closed on a TRUNCATED manifest: a
  # manifest that lost rows in transit still describes intact files, so
  # without a declared total, verify would happily pass on a short list.
  {
    printf '# goosar offline delivery manifest\n'
    printf '# verify with: bash offline/offline-manifest.sh verify --dir <dir> --manifest <this file>\n'
    printf 'tag=%s\n' "$TAG"
    printf 'generated=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'artifacts=%s\n' "$count"
    cat "$rows"
  } > "$tmp"
  rm -f "$rows"

  local missing=""
  for kind in $KINDS; do
    grep -q "^kind=$kind " "$tmp" || missing="$missing $kind"
  done

  mkdir -p "$(dirname "$OUT")"
  mv "$tmp" "$OUT"
  info "wrote $count artifact(s) for $TAG to $OUT"

  if [ -n "$missing" ]; then
    printf 'WARNING: no artifacts of class(es):%s\n' "$missing" >&2
    if [ "$STRICT" = "1" ]; then
      fail "--strict: the carry directory is incomplete —$missing"
    fi
  fi
}

verify() {
  [ -n "$DIR" ] || fail "verify needs --dir DIR"
  [ -d "$DIR" ] || fail "carry directory not found: $DIR"
  [ -n "$MANIFEST" ] || MANIFEST="$DIR/offline-manifest.txt"
  [ -f "$MANIFEST" ] || fail "manifest not found: $MANIFEST"

  local checked=0 problems=0 declared="" line rel expected_sha expected_size actual_sha actual_size
  # `|| [ -n "$line" ]` keeps the last row when the manifest lost its trailing
  # newline in transit: without it, `read` drops that row and a genuinely
  # missing artifact goes unreported.
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      artifacts=*) declared="${line#artifacts=}"; continue ;;
      kind=*) ;;
      *) continue ;;
    esac
    rel="$(printf '%s' "$line" | sed -n 's/.* file=\(.*\) size=.*/\1/p')"
    expected_size="$(printf '%s' "$line" | sed -n 's/.* size=\([0-9]*\) .*/\1/p')"
    expected_sha="$(printf '%s' "$line" | sed -n 's/.* sha256=\([0-9a-f]*\).*/\1/p')"
    [ -n "$rel" ] && [ -n "$expected_sha" ] || {
      printf 'MALFORMED: %s\n' "$line" >&2
      problems=$((problems + 1))
      continue
    }

    if [ ! -f "$DIR/$rel" ]; then
      printf 'MISSING: %s\n' "$rel" >&2
      problems=$((problems + 1))
      continue
    fi
    actual_size="$(size_of "$DIR/$rel")"
    if [ "$actual_size" != "$expected_size" ]; then
      printf 'SIZE MISMATCH: %s (%s bytes, manifest says %s)\n' "$rel" "$actual_size" "$expected_size" >&2
      problems=$((problems + 1))
      continue
    fi
    actual_sha="$(sha256_of "$DIR/$rel")"
    if [ "$actual_sha" != "$expected_sha" ]; then
      printf 'CHECKSUM MISMATCH: %s (%s, manifest says %s)\n' "$rel" "$actual_sha" "$expected_sha" >&2
      problems=$((problems + 1))
      continue
    fi
    checked=$((checked + 1))
  done < "$MANIFEST"

  [ "$checked" -gt 0 ] || [ "$problems" -gt 0 ] || fail "manifest $MANIFEST lists no artifacts — nothing was verified"

  # A manifest that arrived short is itself a delivery failure: the rows that
  # went missing name artifacts nobody would ever look for.
  if [ -n "$declared" ] && [ "$((checked + problems))" -ne "$declared" ]; then
    printf 'TRUNCATED MANIFEST: %s lists %s row(s) but declares artifacts=%s\n' \
      "$MANIFEST" "$((checked + problems))" "$declared" >&2
    problems=$((problems + 1))
  fi

  if [ "$problems" -gt 0 ]; then
    fail "$problems problem(s) across $((checked + problems)) artifact(s) — do not install from this media"
  fi
  info "verified $checked artifact(s) against $MANIFEST"
}

# --- delivery -----------------------------------------------------------------

# derive_migration_range prints e.g. 001..296 from the checkout's migration
# files. The names are zero-padded, so a lexical sort is a numeric sort.
derive_migration_range() {
  local dir="$ROOT_DIR/server/migrations" first last
  [ -d "$dir" ] || return 0
  first="$(ls "$dir"/*.up.sql 2>/dev/null | head -n1)"
  last="$(ls "$dir"/*.up.sql 2>/dev/null | tail -n1)"
  [ -n "$first" ] && [ -n "$last" ] || return 0
  printf '%s..%s' "$(basename "$first" | cut -d_ -f1)" "$(basename "$last" | cut -d_ -f1)"
}

# derive_min_daemon_version reads the floor from its single source of truth
# instead of repeating a number in a document that would drift from it.
derive_min_daemon_version() {
  local f="$ROOT_DIR/server/pkg/agent/version.go"
  [ -f "$f" ] || return 0
  grep -o 'MinQuickCreateCLIVersion = "[^"]*"' "$f" | head -n1 | cut -d'"' -f2
}

delivery() {
  [ -n "$DIR" ] || fail "delivery needs --dir DIR"
  [ -d "$DIR" ] || fail "carry directory not found: $DIR"
  [ -n "$TAG" ] || TAG="$(git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null || true)"
  [ -n "$TAG" ] || fail "no --tag given and no release tag found via git describe"
  [ -n "$OUT" ] || OUT="$DIR/DELIVERY-MANIFEST.md"
  [ -n "$CHART_VERSION" ] || CHART_VERSION="${TAG#v}"
  [ -n "$MIGRATION_RANGE" ] || MIGRATION_RANGE="$(derive_migration_range)"
  [ -n "$MIN_DAEMON_VERSION" ] || MIN_DAEMON_VERSION="$(derive_min_daemon_version)"

  local tmp rows sboms rel kind count=0
  tmp="$(mktemp)"; rows="$(mktemp)"; sboms="$(mktemp)"
  while IFS= read -r -d '' file; do
    rel="${file#"$DIR"/}"
    case "$rel" in
      # `offline/kit/` is the perimeter BUILD kit (offline/make-kit.sh): a Go
      # vendor tarball and npm .tgz packages used to compile inside a closed
      # network. It is not part of a release hand-over, and left in the walk it
      # files go-vendor.tar.gz under `cli` and every npm .tgz under `helm` — in
      # the one document a customer signs an acceptance act against.
      kit/*) continue ;;
      # SBOM and scan reports are part of the delivery but not of the carry set
      # `generate`/`verify` walk, so they get their own section rather than a
      # made-up artifact class. They are matched ON THIS TAG only: kit-images/
      # is not cleaned between releases, and an earlier tag's report listed here
      # would claim a scan this release never ran (worse still when syft/trivy
      # were missing and the ledger said `skipped: tool missing`).
      sbom-*-"$TAG".cyclonedx.json|*/sbom-*-"$TAG".cyclonedx.json)
        printf -- '- `%s` — SBOM (CycloneDX)\n' "$rel" >> "$sboms"; continue ;;
      scan-*-"$TAG".json|*/scan-*-"$TAG".json)
        printf -- '- `%s` — отчёт сканирования (trivy, HIGH/CRITICAL)\n' "$rel" >> "$sboms"; continue ;;
      *.cyclonedx.json|scan-*.json|*/scan-*.json) continue ;;
    esac
    kind="$(classify "$rel")"
    [ -n "$kind" ] || continue
    printf '| `%s` | %s | %s | `%s` |\n' "$rel" "$kind" "$(size_of "$file")" "$(sha256_of "$file")" >> "$rows"
    count=$((count + 1))
  done < <(find "$DIR" -type f -print0 | LC_ALL=C sort -z)

  [ "$count" -gt 0 ] || { rm -f "$tmp" "$rows" "$sboms"; fail "no deliverable artifacts found under $DIR"; }

  # Stale-artifact guard. The images tar and its images.list are written by the
  # same step, so a list that names no `:$TAG` image proves the tar sitting in
  # the directory is left over from an earlier release — which is exactly how a
  # customer ends up installing the wrong build off a signed hand-over list.
  # Say so in the document instead of listing the checksum silently.
  local stale_images=0 list
  for list in "$DIR/images.list" "$DIR/kit-images/images.list"; do
    [ -f "$list" ] || continue
    grep -q ":$TAG\$" "$list" || stale_images=1
  done
  [ "$stale_images" = "0" ] || warn_stale "$TAG"

  {
    printf '# Перечень поставки %s\n\n' "$TAG"
    printf 'Документ сформирован автоматически: `offline/offline-manifest.sh delivery`.\n'
    printf 'Дата: %s. Артефактов: %s.\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$count"

    printf '## Артефакты\n\n'
    printf '| Файл | Класс | Размер, байт | sha256 |\n| --- | --- | --- | --- |\n'
    cat "$rows"
    printf '\n'

    printf '## Образы\n\n'
    if [ "$stale_images" = "1" ]; then
      printf '> **Внимание.** `images.list` в каталоге поставки не содержит ни одного образа с тегом `%s`:\n' "$TAG"
      printf '> архив образов остался от предыдущего выпуска. Пересоберите поставку — подписывать этот перечень нельзя.\n\n' 
    fi
    if [ -n "$DIGESTS" ] && [ -f "$DIGESTS" ]; then
      printf '| Образ | Digest |\n| --- | --- |\n'
      while IFS= read -r line || [ -n "$line" ]; do
        [ -n "$line" ] || continue
        printf '| `%s` | `%s` |\n' "${line%% *}" "${line##* }"
      done < "$DIGESTS"
    else
      printf 'Digest-ы образов не переданы (`--digests`): сверяйте образы по `images.list` и контрольным суммам выше.\n'
    fi
    printf '\n'

    printf '## Версии и совместимость\n\n'
    printf '| Параметр | Значение |\n| --- | --- |\n'
    printf '| Тег релиза | `%s` |\n' "$TAG"
    printf '| Версия Helm-чарта | `%s` |\n' "$CHART_VERSION"
    printf '| Диапазон миграций | `%s` |\n' "${MIGRATION_RANGE:-не определён}"
    printf '| Минимальная версия демона | `%s` |\n' "${MIN_DAEMON_VERSION:-не определена}"
    printf '\n'
    printf 'Окно поддержки, каденция выпусков и правила совместимости — `docs/support/version-policy.md`.\n\n'

    printf '## SBOM и сканирование\n\n'
    if [ -s "$sboms" ]; then
      cat "$sboms"
    else
      printf 'SBOM в каталоге поставки нет. Соберите их шагом `scripts/sbom-scan.sh` и приложите к релизу.\n'
    fi
    printf '\n'

    printf '## Лицензии\n\n'
    if [ -n "$NOTICES" ] && [ -f "$NOTICES" ]; then
      printf -- '- `%s`\n' "$(basename "$NOTICES")"
    else
      printf 'Файл уведомлений о сторонних лицензиях не найден.\n'
    fi
    printf '\n'

    printf '## Проверка на стороне получателя\n\n'
    printf '```bash\n'
    printf 'bash offline/offline-manifest.sh verify --dir <каталог> --manifest offline-manifest.txt\n'
    printf '```\n'
  } > "$tmp"
  rm -f "$rows" "$sboms"

  mkdir -p "$(dirname "$OUT")"
  mv "$tmp" "$OUT"
  info "wrote the delivery manifest for $TAG to $OUT ($count artifact(s))"
}

case "$MODE" in
  generate) generate ;;
  verify) verify ;;
  delivery) delivery ;;
  ""|--help|-h) sed -n '2,/^set -euo/p' "$ROOT_DIR/offline/offline-manifest.sh" | sed '$d' | sed 's/^# \{0,1\}//' ;;
  *) fail "unknown mode: $MODE (expected generate, verify or delivery)" ;;
esac
