#!/usr/bin/env bash
# Measure Go statement coverage of the production binary's own packages
# (everything ./cmd/tendo links from this module) across the unit and the
# real-PostgreSQL integration suites, excluding generated code, and fail below
# the floor. Needs TENDO_TEST_POSTGRES_ADMIN_URL (see docs/development/testing.md).
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
floor=80
: "${TENDO_TEST_POSTGRES_ADMIN_URL:?Set TENDO_TEST_POSTGRES_ADMIN_URL to the dedicated PostgreSQL 18 test service admin URL}"

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
cd "$root/backend"

module=$(go list -m)
packages=$(go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./cmd/tendo | grep "^${module}/")
coverpkg=$(paste -sd, - <<<"$packages")
integration=(./internal/identity/postgres ./internal/household/postgres ./internal/subject/postgres ./internal/item/postgres ./internal/platform/database)
unit=$(go list ./... | grep -v -e '/internal/identity/postgres$' -e '/internal/household/postgres$' -e '/internal/subject/postgres$' -e '/internal/item/postgres$')
[[ -n "$unit" ]]

echo "--> unit suite"
# shellcheck disable=SC2086 # $unit is a newline-separated package list
go test -count=1 -covermode=set -coverpkg="$coverpkg" -coverprofile="$out/unit.out" $unit >"$out/unit.log" 2>&1 || { cat "$out/unit.log"; exit 1; }
echo "--> PostgreSQL integration suite"
python3 "$root/scripts/postgres-test-service.py" \
  go test -p 1 -count=1 -covermode=set -coverpkg="$coverpkg" -coverprofile="$out/integration.out" "${integration[@]}" >"$out/integration.log" 2>&1 || { cat "$out/integration.log"; exit 1; }

# Generated sqlc and oapi-codegen files carry the standard header; their
# statements are not hand-written and are left out of the measurement.
find . -name '*.go' -type f -exec awk 'FNR == 1 { header = 1 } /^package / { header = 0 } header && /^\/\/ Code generated .* DO NOT EDIT\.$/ { print FILENAME; header = 0 }' {} + |
  sed "s|^\./|${module}/|; s|\$|:|" >"$out/generated"
[[ -s "$out/generated" ]]
{
  echo 'mode: set'
  tail -q -n +2 "$out/unit.out" "$out/integration.out" | grep -v -F -f "$out/generated"
} >"$out/merged.out"

total=$(go tool cover -func="$out/merged.out" | awk '$1 == "total:" { sub("%", "", $3); print $3 }')
[[ $total =~ ^[0-9]+(\.[0-9]+)?$ ]]
if [[ -n "${TENDO_GO_COVERAGE_PROFILE:-}" ]]; then
  cp "$out/merged.out" "$TENDO_GO_COVERAGE_PROFILE"
fi
printf 'Go statement coverage (unit + PostgreSQL integration, %s production packages, generated code excluded): %s%% (floor %s%%)\n' \
  "$(wc -l <<<"$packages")" "$total" "$floor"
awk -v t="$total" -v f="$floor" 'BEGIN { exit !(t >= f) }' || {
  printf 'Go statement coverage %s%% is below the %s%% floor\n' "$total" "$floor" >&2
  exit 1
}
