#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${TENDO_TEST_POSTGRES_ADMIN_URL:?Set TENDO_TEST_POSTGRES_ADMIN_URL to the dedicated PostgreSQL 18 test service admin URL}"
exec python3 "$root/scripts/postgres-test-service.py" bash -c 'cd "$1/backend" && go test -race -p 1 -count=1 ./internal/identity/postgres ./internal/household/postgres ./internal/subject/postgres ./internal/item/postgres ./internal/platform/database' _ "$root"
