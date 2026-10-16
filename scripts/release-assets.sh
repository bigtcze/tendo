#!/usr/bin/env bash
# Stage the files attached to a GitHub release. The quick-start smoke installs from the same set.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out=${1:?usage: release-assets.sh OUTPUT_DIRECTORY}
mkdir -p "$out"
install -m 0644 "$root/compose.yaml" "$out/compose.yaml"
# GitHub rewrites release asset names that start with a dot, so the example settings ship as example.env.
install -m 0644 "$root/.env.example" "$out/example.env"
