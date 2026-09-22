#!/bin/sh
# Stages hermes-agent build output from GOOSAR_HERMES_DIST_DIR into
# /out/cli under the stable name Dockerfile.web / install.sh expect:
#   hermes-<os>-<arch>.tar.gz   (arch normalized: x86_64|amd64->amd64, arm64|aarch64->arm64)
#
# Accepts two shapes of dist entries, both produced by hermes-agent's own
# scripts/build-artifact.sh:
#   - hermes-<version>-<os>-<arch>/          (unpacked artifact directory;
#     packed here the same way build-artifact.sh's own instructions do:
#     `tar -C dist -czf NAME.tar.gz NAME`, keeping the wrapped top-level dir)
#   - hermes-<version>-<os>-<arch>.tar.gz     (already packed; copied as-is)
#
# If two entries normalize to the same os/arch, the highest version (sort -V)
# wins and a warning is printed for the discarded one.
#
# POSIX sh: this runs inside the alpine build stage, which has no bash.
#
# Usage: stage-agent-artifacts.sh <dist-dir> <out-dir>
set -eu

DIST_DIR="${1:?usage: stage-agent-artifacts.sh <dist-dir> <out-dir>}"
OUT_DIR="${2:?usage: stage-agent-artifacts.sh <dist-dir> <out-dir>}"

mkdir -p "$OUT_DIR"

normalize_arch() {
  case "$1" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "$1" ;;
  esac
}

# Dotted-version compare (no `sort -V`: busybox ash's sort lacks it).
# Prints "$1" if version $1 >= version $2, else prints "$2".
newer_version() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    n = split(a, av, ".");
    m = split(b, bv, ".");
    max = n > m ? n : m;
    for (i = 1; i <= max; i++) {
      x = (i <= n) ? av[i] + 0 : 0;
      y = (i <= m) ? bv[i] + 0 : 0;
      if (x > y) { print a; exit }
      if (x < y) { print b; exit }
    }
    print a;
  }'
}

[ -d "$DIST_DIR" ] || exit 0

CANDIDATES="$(mktemp)"
trap 'rm -f "$CANDIDATES"' EXIT

# Each line: version|os|arch|path|is_dir
for path in "$DIST_DIR"/hermes-*; do
  [ -e "$path" ] || continue
  base="$(basename "$path")"
  if [ -d "$path" ]; then
    is_dir=1
    name="$base"
  elif [ "${base%.tar.gz}" != "$base" ]; then
    is_dir=0
    name="${base%.tar.gz}"
  else
    continue
  fi
  rest="${name#hermes-}"
  os="$(printf '%s' "$rest" | awk -F- '{print $(NF-1)}')"
  arch_raw="$(printf '%s' "$rest" | awk -F- '{print $NF}')"
  version="${rest%-"$os"-"$arch_raw"}"
  [ -n "$os" ] && [ -n "$arch_raw" ] && [ -n "$version" ] || continue
  arch="$(normalize_arch "$arch_raw")"
  printf '%s|%s|%s|%s|%s\n' "$version" "$os" "$arch" "$path" "$is_dir" >>"$CANDIDATES"
done

[ -s "$CANDIDATES" ] || exit 0

PLATFORMS="$(awk -F'|' '{print $2"-"$3}' "$CANDIDATES" | sort -u)"

for platform in $PLATFORMS; do
  os="${platform%-*}"
  arch="${platform##*-}"

  best_version=""
  other_versions=""
  while IFS='|' read -r version p_os p_arch path is_dir; do
    [ "$p_os-$p_arch" = "$platform" ] || continue
    if [ -z "$best_version" ]; then
      best_version="$version"
    elif [ "$(newer_version "$version" "$best_version")" = "$version" ] && [ "$version" != "$best_version" ]; then
      other_versions="$other_versions $best_version"
      best_version="$version"
    else
      other_versions="$other_versions $version"
    fi
  done <"$CANDIDATES"

  for v in $other_versions; do
    echo "[cli] multiple hermes-agent versions found for $platform; using $best_version over $v" >&2
  done

  best_line="$(awk -F'|' -v os="$os" -v arch="$arch" -v ver="$best_version" \
    '$1==ver && $2==os && $3==arch {print; exit}' "$CANDIDATES")"
  path="$(printf '%s\n' "$best_line" | cut -d'|' -f4)"
  is_dir="$(printf '%s\n' "$best_line" | cut -d'|' -f5)"
  target="$OUT_DIR/hermes-$os-$arch.tar.gz"
  if [ "$is_dir" = "1" ]; then
    tar -C "$(dirname "$path")" -czf "$target" "$(basename "$path")"
  else
    cp "$path" "$target"
  fi
done

cd "$OUT_DIR" && sha256sum hermes-*.tar.gz >> checksums.txt
