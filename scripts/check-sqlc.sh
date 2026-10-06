#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
image=sqlc/sqlc:1.30.0
command -v docker >/dev/null 2>&1 || { printf 'docker is required\n' >&2; exit 1; }
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image"
tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
if [[ -d "$root/backend/internal/identity/postgres/dbgen" ]]; then
  cp -a "$root/backend/internal/identity/postgres/dbgen" "$tmp/identity"
fi
if [[ -d "$root/backend/internal/household/postgres/dbgen" ]]; then
  cp -a "$root/backend/internal/household/postgres/dbgen" "$tmp/household"
fi
docker run --rm -v "$root/backend:/src" -w /src "$image" generate -f sqlc.yaml
for module in identity household; do
  target="$root/backend/internal/$module/postgres/dbgen"
  if [[ -d "$tmp/$module" ]]; then
    diff -ru "$tmp/$module" "$target"
  elif [[ -d "$target" ]]; then
    printf 'generated output unexpectedly appeared: %s\n' "$target" >&2
    exit 1
  fi
done
