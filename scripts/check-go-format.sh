#!/usr/bin/env bash
# House Go formatting gate: gofumpt profile plus gci import order
# (std -> third-party -> home module). Both tools are pinned as `tool`
# dependencies in server/go.mod, so `go tool <name>` resolves them without a
# separate install step.
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root/server"

status=0

fumpt_files=$(go tool gofumpt -l .)
if [ -n "$fumpt_files" ]; then
  echo "gofumpt: the following files are not formatted (run 'make fmt-go'):"
  echo "$fumpt_files"
  status=1
fi

gci_files=$(go tool gci list -s standard -s default -s localmodule --skip-generated .)
if [ -n "$gci_files" ]; then
  echo "gci: the following files have unsorted imports (run 'make fmt-go'):"
  echo "$gci_files"
  status=1
fi

exit "$status"
