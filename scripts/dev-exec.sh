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
# variables, user/system/parent-directory configs, system or shared installs and default-package files could
# add tools, substitute unverified installs or skip verification, so they are all redirected or disabled.
while IFS= read -r name; do unset "$name"; done < <(compgen -e | grep '^MISE_' || true)
private="${XDG_CACHE_HOME:-$HOME/.cache}/tendo/mise"
for dir in data cache state config system system-data; do mkdir -p "$private/$dir"; done
export MISE_DATA_DIR="$private/data" MISE_CACHE_DIR="$private/cache" MISE_STATE_DIR="$private/state" \
  MISE_CONFIG_DIR="$private/config" MISE_SYSTEM_CONFIG_DIR="$private/system" \
  MISE_SYSTEM_DATA_DIR="$private/system-data" MISE_SYSTEM_INSTALLS_DIR="$private/system-data/installs" \
  MISE_CEILING_PATHS="$(dirname "$root")"
# mise installs these into every fresh Node.js/Go runtime; only override when present (overriding prints a deprecation).
for source in "${NODE_DEFAULT_PACKAGES_FILE:-}" "$HOME/.default-nodejs-packages" "$HOME/.default-node-packages" "$HOME/.default-npm-packages"; do
  [[ -z "$source" || ! -e "$source" ]] || { export MISE_NODE_DEFAULT_PACKAGES_FILE=/dev/null; break; }
done
[[ ! -e "$HOME/.default-go-packages" ]] || export MISE_GO_DEFAULT_PACKAGES_FILE=/dev/null

# Supervise installation so cancelling the wrapper also stops an in-progress download or extraction.
mise -C "$root" install --locked --quiet &
installer=$!
trap 'kill -TERM "$installer" 2>/dev/null || true; wait "$installer" || true; exit 143' TERM HUP
trap 'kill -TERM "$installer" 2>/dev/null || true; wait "$installer" || true; exit 130' INT
status=0
wait "$installer" || status=$?
trap - TERM HUP INT
(( status == 0 )) || exit "$status"

# Capture first: a failing `mise env` inside `eval "$(...)"` would not stop the script.
tool_env=$(cd "$root" && mise env --locked --shell bash)
eval "$tool_env"
# Go must not download another toolchain or apply settings saved with `go env -w`; explicit GO* variables still apply.
export GOTOOLCHAIN=local GOENV=off

# mise verifies checksums when it installs; afterwards it trusts the install directory. Confirm that every tool
# resolves inside the private install tree, not a host binary later in PATH, and reports the pinned version.
check() {
  local tool=$1 want=$2 got path
  path=$(command -v "$tool" || true)
  [[ "$path" == "$private/data/installs/"* ]] || { printf 'dev-exec: %s resolves to %s, not the pinned install\n' "$tool" "${path:-nothing}" >&2; exit 1; }
  [[ -z "$want" ]] && return
  got=$("${@:3}" 2>/dev/null || true)
  [[ "$got" == "$want" ]] || { printf 'dev-exec: %s reports %s, expected %s; remove %s and retry\n' "$tool" "${got:-nothing}" "$want" "$private/data/installs" >&2; exit 1; }
}
current() { (cd "$root" && mise current "$1"); }
check go "go$(current go)" go env GOVERSION
check node "v$(current node)" node --version
check npm ""
check sqlc "v$(current aqua:sqlc-dev/sqlc)" sqlc version
exec "$@"
