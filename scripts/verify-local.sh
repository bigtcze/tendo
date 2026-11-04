#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

stage() {
  echo "==> $1"
  shift
  "$@"
}

stage "Go lint (gofmt, go vet, staticcheck, structure)" bash -c 'cd backend && golangci-lint run ./...'
stage "Go unit tests with race detector" bash -c 'set -euo pipefail; cd backend; packages=$(go list ./... | grep -v -e "/internal/identity/postgres$" -e "/internal/household/postgres$" -e "/internal/subject/postgres$" -e "/internal/item/postgres$"); [[ -n "$packages" ]]; go test -race $packages'
stage "PostgreSQL integration tests" bash scripts/setup-integration.sh
stage "sqlc generated output" bash scripts/check-sqlc.sh
stage "Go build" bash -c 'cd backend && go build ./...'
stage "API checks" bash -c 'cd api && npm ci && npm audit --audit-level=moderate && npm run check'
stage "Frontend checks" bash -c 'cd frontend && npm ci && npm audit --audit-level=moderate && npm run check'
stage "Playwright Chromium for the locked @playwright/test" bash -c 'cd frontend && npx --no-install playwright install chromium'
stage "Production E2E" bash scripts/e2e-smoke.sh
