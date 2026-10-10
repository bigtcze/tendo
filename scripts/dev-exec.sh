#!/usr/bin/env bash
# Run a command with the exact toolchain.json pins from a project-scoped user cache.
#   bash scripts/dev-exec.sh -- bash scripts/verify-local.sh
#   bash scripts/dev-exec.sh --versions
# See docs/development/testing.md#pinned-toolchain-wrapper.
set -euo pipefail
exec python3 "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dev_toolchain.py" "$@"
