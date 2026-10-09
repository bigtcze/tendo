#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
backend="$root/backend"
command -v sqlc >/dev/null 2>&1 || { printf 'sqlc 1.31.1 is required (native executable); source the pinned native tools environment or install it and retry\n' >&2; exit 1; }
version=$(sqlc version)
if [[ "$version" != "v1.31.1" ]]; then
  printf 'sqlc version mismatch: expected v1.31.1, found %s\n' "$version" >&2
  exit 1
fi
tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
mkdir -p "$tmp/internal"
cp "$backend/sqlc.yaml" "$tmp/"
cp -a "$backend/internal/platform" "$tmp/internal/"
for module in identity household subject item; do
  mkdir -p "$tmp/internal/$module/postgres"
  cp -a "$backend/internal/$module/postgres/queries" "$tmp/internal/$module/postgres/"
  if [[ -d "$backend/internal/$module/postgres/dbgen" ]]; then
    cp -a "$backend/internal/$module/postgres/dbgen" "$tmp/internal/$module/postgres/expected-dbgen"
  fi
done
(cd "$tmp" && sqlc -f sqlc.yaml generate)
for module in identity household subject item; do
  source="$backend/internal/$module/postgres/dbgen"
  generated="$tmp/internal/$module/postgres/dbgen"
  expected="$tmp/internal/$module/postgres/expected-dbgen"
  if [[ -d "$expected" ]]; then
    if [[ ! -d "$generated" ]]; then
      printf 'generated output missing: %s\n' "$source" >&2
      exit 1
    fi
    diff -ru "$expected" "$generated"
  elif [[ -d "$generated" ]]; then
    printf 'generated output unexpectedly appeared: %s\n' "$source" >&2
    exit 1
  fi
done
