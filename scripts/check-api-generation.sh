#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
command -v go >/dev/null 2>&1 || { printf 'go is required to check API generation\n' >&2; exit 1; }
command -v node >/dev/null 2>&1 || { printf 'node is required to check API generation\n' >&2; exit 1; }
command -v make >/dev/null 2>&1 || { printf 'make is required to check API generation\n' >&2; exit 1; }
if [[ ! -x "$root/api/node_modules/.bin/openapi-typescript" || ! -x "$root/api/node_modules/.bin/redocly" ]]; then
  printf 'API Node dependencies are missing; run npm ci in api/\n' >&2
  exit 1
fi
tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
mkdir -p "$tmp/api/generated" "$tmp/backend/internal/identity/httpapi" \
  "$tmp/backend/internal/platform/httpx" "$tmp/backend/internal/household/httpapi" \
  "$tmp/backend/internal/subject/httpapi" "$tmp/backend/internal/item/httpapi"
cp "$root/api/Makefile" "$root/api/openapi.yaml" "$root/api/oapi-codegen.yaml" "$tmp/api/"
ln -s "$root/api/node_modules" "$tmp/api/node_modules"
cp "$root/backend/go.mod" "$root/backend/go.sum" "$tmp/backend/"
make -C "$tmp/api" generate
for path in generated; do
  diff -ru "$root/api/$path" "$tmp/api/$path"
done
for path in \
  internal/identity/httpapi/setup.gen.go \
  internal/platform/httpx/health.gen.go \
  internal/household/httpapi/household.gen.go \
  internal/subject/httpapi/subject.gen.go \
  internal/item/httpapi/item.gen.go; do
  cmp "$root/backend/$path" "$tmp/backend/$path"
done
