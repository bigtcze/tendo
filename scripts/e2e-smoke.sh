#!/usr/bin/env bash
# Browser E2E against the production-built Tendo image and real PostgreSQL.
# Requires `npm ci` in frontend/ beforehand (Playwright test runner comes from node_modules).
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
playwright_image_tag=v1.63.0-noble
playwright_image=mcr.microsoft.com/playwright:$playwright_image_tag
if [[ ! -x "$root/frontend/node_modules/.bin/playwright" ]]; then
  printf 'frontend/node_modules is missing; run `npm ci` in frontend/ first\n' >&2
  exit 1
fi
# The browser image and the @playwright/test runner must be the same version.
installed_playwright=$(node -p "require('$root/frontend/node_modules/@playwright/test/package.json').version")
if [[ "v${installed_playwright}-noble" != "$playwright_image_tag" ]]; then
  printf 'Playwright version mismatch: node_modules has %s but image tag is %s\n' "$installed_playwright" "$playwright_image_tag" >&2
  exit 1
fi
project="tendo-e2e-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_TEST_CLOCK_ACK TENDO_TEST_CLOCK_NOW
TENDO_TEST_CLOCK_ACK=isolated-e2e-only
TENDO_TEST_CLOCK_NOW=2026-01-15T22:59:59Z
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
E2E_OWNER_PASSWORD=$(openssl rand -hex 24)
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.e2e.yaml")
cleanup() {
  local result=$?
  trap - EXIT
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    printf 'Compose cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT
origin=$TENDO_PUBLIC_URL
timeout 600 "${compose[@]}" up -d --build --wait --wait-timeout 120
ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null; then ready=1; break; fi
  sleep 1
done
if (( ready == 0 )); then printf 'App did not become ready: %s/health/ready\n' "$origin" >&2; exit 1; fi
mkdir -p "$root/frontend/test-results/$project"
# Secrets and clock settings are passed by name only (-e NAME) so values stay out of argv.
export E2E_BASE_URL=$origin E2E_SETUP_TOKEN=$TENDO_SETUP_TOKEN E2E_OWNER_PASSWORD E2E_CLOCK_NOW=$TENDO_TEST_CLOCK_NOW E2E_CLOCK_FIXTURE_PATH="test-results/$project/attention-clock.json"
run_playwright() {
  timeout 600 docker run --rm --network host --ipc=host \
    --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e E2E_BASE_URL -e E2E_SETUP_TOKEN -e E2E_OWNER_PASSWORD -e E2E_CLOCK_NOW -e E2E_CLOCK_PHASE -e E2E_CLOCK_FIXTURE_PATH -e CI \
    -v "$root":/repo -w /repo/frontend \
    "$playwright_image" npx --no-install playwright test "$@"
}
unset E2E_CLOCK_PHASE
run_playwright
export E2E_CLOCK_PHASE=before
run_playwright e2e/attention-clock.spec.ts
TENDO_TEST_CLOCK_NOW=2026-01-15T23:00:00Z
E2E_CLOCK_NOW=$TENDO_TEST_CLOCK_NOW
"${compose[@]}" up -d --no-deps --force-recreate --wait --wait-timeout 120 app
ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null; then ready=1; break; fi
  sleep 1
done
if (( ready == 0 )); then printf 'App did not become ready after clock recreation: %s/health/ready\n' "$origin" >&2; exit 1; fi
export E2E_CLOCK_NOW E2E_CLOCK_PHASE=after
run_playwright e2e/attention-clock.spec.ts
printf 'Browser E2E smoke passed.\n'
