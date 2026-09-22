#!/usr/bin/env bash
# Refresh the vendored agent-template skills under
# server/internal/agenttmpl/vendored/ (issue #66).
#
# Built-in agent templates reference upstream skills by github.com tree URL
# (server/internal/agenttmpl/templates/*.json). Creating an agent from a
# template must work with zero network, so a snapshot of every referenced
# skill is checked into the repo and embedded into the server binary. This
# script rebuilds that snapshot from the current upstream HEADs:
#
#   1. Collects every skill source_url from the template JSON files.
#   2. Shallow-clones each referenced repository once.
#   3. Copies each skill directory: binary assets are dropped, SKILL.md is
#      kept as the primary content, and the upstream LICENSE/NOTICE/COPYING
#      files are kept next to the skill (#435). Those files are the
#      redistribution condition of the upstream Apache-2.0 / MIT bundles: the
#      snapshot is embedded into the server binary and shipped in the DMG and
#      in provisioning packages, so their text has to travel with it. They are
#      NOT skill content — agenttmpl/vendored.go keeps them out of the
#      imported file set, so nothing of them reaches an agent prompt.
#   4. Rewrites vendored/skills/** and vendored/manifest.json (with the
#      upstream commit recorded per skill for provenance).
#
# Requirements: bash, git, jq, network access to github.com.
# Usage: scripts/sync-vendored-template-skills.sh
# Then review the diff, run the Go tests, and commit the result:
#   (cd server && go test ./internal/agenttmpl/... ./internal/handler/ -run 'Vendored|Template' -count=1)
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
templates_dir="$repo_root/server/internal/agenttmpl/templates"
vendored_dir="$repo_root/server/internal/agenttmpl/vendored"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

# Mirrors isLikelyBinaryFilePath in server/internal/handler/skill.go: the live
# importer silently drops these, so the vendored snapshot must too.
is_binary_path() {
  case "$(printf '%s' "${1##*.}" | tr '[:upper:]' '[:lower:]')" in
    png|jpg|jpeg|gif|webp|bmp|tiff|ico|heic) return 0 ;;
    ttf|otf|woff|woff2|eot) return 0 ;;
    zip|gz|tar|bz2|7z|rar) return 0 ;;
    pdf|docx|xlsx|pptx|doc|xls|ppt) return 0 ;;
    mp3|mp4|wav|avi|mov|webm|m4a|flac) return 0 ;;
    exe|dll|so|dylib|class|jar|wasm) return 0 ;;
    db|sqlite|sqlite3|pyc) return 0 ;;
    *) return 1 ;;
  esac
}

# Licence-bearing files, kept next to the skill (#435). Matches the pattern
# agenttmpl/vendored.go uses to keep the same files out of the imported skill
# content, so "shipped" and "not in the prompt" cannot drift apart.
is_license_basename() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    license|licence|notice|copying) return 0 ;;
    license.*|licence.*|notice.*|copying.*) return 0 ;;
    *) return 1 ;;
  esac
}

# Copies one upstream skill directory into the vendored snapshot. Returns
# nothing; prints the number of copied files. Kept a function so
# scripts/sync-vendored-template-skills.test.sh can drive it on a fixture
# without a network clone.
copy_skill_tree() {
  local src_dir="$1" dest_dir="$2" file rel base copied=0
  while IFS= read -r file; do
    rel="${file#"$src_dir"/}"
    base="$(basename "$rel")"
    if [ "$rel" != "SKILL.md" ] && ! is_license_basename "$base"; then
      is_binary_path "$rel" && continue
    fi
    mkdir -p "$dest_dir/$(dirname "$rel")"
    cp "$file" "$dest_dir/$rel"
    copied=$((copied + 1))
  done < <(find "$src_dir" -type f | sort)
  printf '%s' "$copied"
}

# Copies the licence files that sit at the CLONE ROOT into the vendored skill
# directory. Upstream keeps one LICENSE for the whole repository, not one per
# skill (anthropics/skills, vercel-labs/agent-skills and obra/superpowers-skills
# all do), so walking only the skill directory vendors no licence at all — which
# already present in the skill directory wins: it is the more specific one.
# Prints the number of copied files.
copy_root_licenses() {
  local clone_dir="$1" dest_dir="$2" file base copied=0
  for file in "$clone_dir"/*; do
    [ -f "$file" ] || continue
    base="$(basename "$file")"
    if is_license_basename "$base" && [ ! -e "$dest_dir/$base" ]; then
      cp "$file" "$dest_dir/$base"
      copied=$((copied + 1))
    fi
  done
  printf '%s' "$copied"
}

# Everything below performs the sync; sourcing the script (the test does) stops
# here with only the helpers defined.
(return 0 2>/dev/null) && return 0

urls="$(jq -r '.skills[]?.source_url' "$templates_dir"/*.json | sort -u)"
[ -n "$urls" ] || { echo "no skill source_urls found under $templates_dir" >&2; exit 1; }

rm -rf "$vendored_dir/skills"
mkdir -p "$vendored_dir/skills"
manifest_entries="$work_dir/entries.jsonl"
: > "$manifest_entries"

while IFS= read -r url; do
  # Expected form: https://github.com/{owner}/{repo}/tree/{ref}/{path...}
  rest="${url#https://github.com/}"
  if [ "$rest" = "$url" ]; then
    echo "unsupported source_url (expected https://github.com/...): $url" >&2
    exit 1
  fi
  owner="${rest%%/*}"; rest="${rest#*/}"
  repo="${rest%%/*}"; rest="${rest#*/}"
  kind="${rest%%/*}"; rest="${rest#*/}"
  if [ "$kind" != "tree" ]; then
    echo "unsupported source_url form (expected /tree/{ref}/{path}): $url" >&2
    exit 1
  fi
  ref="${rest%%/*}"
  skill_path="${rest#*/}"
  if [ -z "$owner" ] || [ -z "$repo" ] || [ -z "$ref" ] || [ -z "$skill_path" ] || [ "$skill_path" = "$ref" ]; then
    echo "could not parse source_url: $url" >&2
    exit 1
  fi

  clone_dir="$work_dir/$owner--$repo--$ref"
  if [ ! -d "$clone_dir" ]; then
    echo "cloning $owner/$repo@$ref ..." >&2
    git clone --quiet --depth 1 --branch "$ref" "https://github.com/$owner/$repo" "$clone_dir"
  fi
  commit="$(git -C "$clone_dir" rev-parse HEAD)"

  src_dir="$clone_dir/$skill_path"
  if [ ! -f "$src_dir/SKILL.md" ]; then
    echo "SKILL.md missing at $skill_path in $owner/$repo@$ref ($url)" >&2
    exit 1
  fi

  dir_name="$(basename "$skill_path")"
  dest_dir="$vendored_dir/skills/$dir_name"
  if [ -e "$dest_dir" ]; then
    echo "vendored dir name collision: $dir_name (from $url)" >&2
    exit 1
  fi

  copied="$(copy_skill_tree "$src_dir" "$dest_dir")"
  copied=$((copied + $(copy_root_licenses "$clone_dir" "$dest_dir")))
  if [ -z "$(find "$dest_dir" \( -iname 'LICEN[SC]E*' -o -iname 'COPYING*' \) -type f)" ]; then
    echo "no licence file for $dir_name in $owner/$repo@$ref — vendoring it would ship an unlicensed bundle" >&2
    exit 1
  fi
  echo "vendored $dir_name: $copied files from $owner/$repo@${commit:0:12}" >&2

  jq -n \
    --arg source_url "$url" \
    --arg dir "$dir_name" \
    --arg owner "$owner" \
    --arg repo "$repo" \
    --arg ref "$ref" \
    --arg path "$skill_path" \
    --arg commit "$commit" \
    '{source_url: $source_url, dir: $dir, owner: $owner, repo: $repo, ref: $ref, path: $path, commit: $commit}' \
    >> "$manifest_entries"
done <<< "$urls"

jq -s '{skills: sort_by(.source_url)}' "$manifest_entries" > "$vendored_dir/manifest.json"
echo "wrote $vendored_dir/manifest.json" >&2
