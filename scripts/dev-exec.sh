#!/usr/bin/env bash
# Run a command with the tools pinned by mise.toml and verified against mise.lock.
#   bash scripts/dev-exec.sh -- bash scripts/verify-local.sh
#   bash scripts/dev-exec.sh -- mise ls --current
set -euo pipefail
if [[ "${1:-}" != "--" || $# -lt 2 ]]; then
  printf 'usage: bash scripts/dev-exec.sh -- COMMAND [ARG...]\n' >&2
  exit 2
fi
shift
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
command -v mise >/dev/null 2>&1 || { printf 'mise is required: https://mise.jdx.dev/installing-mise.html\n' >&2; exit 127; }

# Only this repository's mise.toml applies, and tools install into a Tendo-private directory. Inherited MISE_*
# variables, user/system/parent-directory configs and shared installs could add tools or skip verification,
# so they are ignored; nothing outside the private directory is read or changed.
while IFS= read -r name; do unset "$name"; done < <(compgen -e | grep '^MISE_' || true)
private="${XDG_CACHE_HOME:-$HOME/.cache}/tendo/mise"
for dir in data cache state config system; do mkdir -p "$private/$dir"; done
export MISE_DATA_DIR="$private/data" MISE_CACHE_DIR="$private/cache" MISE_STATE_DIR="$private/state" \
  MISE_CONFIG_DIR="$private/config" MISE_SYSTEM_CONFIG_DIR="$private/system" MISE_CEILING_PATHS="$(dirname "$root")"

mise -C "$root" install --locked --quiet
# Capture first: a failing `mise env` inside `eval "$(...)"` would not stop the script.
tool_env=$(cd "$root" && mise env --locked --shell bash)
eval "$tool_env"
# Go must not download another toolchain or apply settings saved with `go env -w`; explicit GO* variables still apply.
export GOTOOLCHAIN=local GOENV=off
exec "$@"
