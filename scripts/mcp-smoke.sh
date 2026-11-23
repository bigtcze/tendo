#!/usr/bin/env bash
# Live MCP OAuth smoke (ADR 0023, slice A4c of #456): a Compose app with
# TENDO_MCP_ENABLED=true on a loopback origin, driven by a scripted public
# client. The app is recreated with MCP off and on again to prove the kill
# switch. Captured responses cover every MCP operationId in the checker.
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-mcp-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_MCP_ENABLED
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
TENDO_HOST_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
# The driver connects to this exact authority, so Host and Origin always equal
# the configured public origin (a mismatch is 421).
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
TENDO_MCP_ENABLED=true
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml")
fixtures=$(mktemp)
secrets=$(mktemp)
state=$(mktemp)
logs=$(mktemp)
cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    "${compose[@]}" logs --no-color app >"$logs" 2>/dev/null || true
    python3 - "$logs" "$secrets" <<'PY' >&2 || true
import pathlib,sys
text=pathlib.Path(sys.argv[1]).read_text(errors='replace')
for secret in pathlib.Path(sys.argv[2]).read_text().splitlines():
 if secret: text=text.replace(secret,'[REDACTED]')
print(text)
PY
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    echo 'MCP Compose cleanup failed' >&2
    (( result != 0 )) || result=1
  fi
  rm -f "$fixtures" "$secrets" "$state" "$logs" || { echo 'MCP smoke temporary cleanup failed' >&2; (( result != 0 )) || result=1; }
  exit "$result"
}
trap cleanup EXIT
origin=$TENDO_PUBLIC_URL
wait_ready() {
  for _ in $(seq 1 60); do
    curl -fsS --max-time 3 "$origin/health/ready" >/dev/null && return 0
    sleep 1
  done
  echo 'MCP smoke app failed readiness' >&2
  return 1
}
# Recreates only the app with the exported TENDO_MCP_ENABLED. A plain restart
# would keep the old container environment; --no-deps leaves PostgreSQL alone.
recreate_app() {
  TENDO_MCP_ENABLED=$1
  "${compose[@]}" up -d --no-deps --wait --wait-timeout 180 app >/dev/null
  wait_ready
}
drive() {
  MCP_SMOKE_ORIGIN="$origin" MCP_SMOKE_SETUP_TOKEN="$TENDO_SETUP_TOKEN" MCP_SMOKE_FIXTURES="$fixtures" \
    MCP_SMOKE_SECRETS="$secrets" MCP_SMOKE_PSQL="${compose[*]} exec -T postgres psql -U postgres -d tendo -At" \
    python3 "$root/scripts/mcp_smoke_driver.py" "$1" "$state"
}
"${compose[@]}" up -d --build --wait --wait-timeout 180
wait_ready
drive enabled
recreate_app false
drive disabled
recreate_app true
drive reenabled
API_REQUIRE_OPERATION_COVERAGE=1 \
  API_OPERATION_COVERAGE_SCOPE=registerMcpClient,getMcpProtectedResourceMetadata,\
getMcpAuthorizationServerMetadata,authorizeMcpClient,getMcpAuthorizationRequest,\
decideMcpAuthorizationRequest,exchangeMcpToken,revokeMcpToken,callMcp \
  node "$root/api/check-health.mjs" "$fixtures"
# Secrets live only as digests in PostgreSQL: no plaintext token, code, handle or
# binding appears in any MCP table.
"${compose[@]}" exec -T postgres pg_dump -U postgres -d tendo --data-only \
  -t mcp_clients -t mcp_grants -t mcp_authorization_codes -t mcp_access_tokens \
  -t mcp_refresh_tokens -t mcp_authorization_requests -t mcp_grant_events >"$logs"
while IFS= read -r secret; do
  [[ -z "$secret" ]] && continue
  if grep -F -- "$secret" "$logs" >/dev/null; then echo 'An MCP secret is stored in plaintext in PostgreSQL' >&2; exit 1; fi
done <"$secrets"
"${compose[@]}" logs --no-color app >"$logs"
while IFS= read -r secret; do
  [[ -z "$secret" ]] && continue
  if grep -F -- "$secret" "$logs" >/dev/null; then echo 'An MCP token, code, handle, verifier or cookie value appeared in app logs' >&2; exit 1; fi
done <"$secrets"
if grep -E 'code=|#request=|code_verifier|refresh_token=' "$logs" >/dev/null; then echo 'MCP OAuth parameter marker found in app logs' >&2; exit 1; fi
printf 'MCP discovery, registration, consent, token, revocation, endpoint, kill-switch and log-privacy smoke passed.\n'
