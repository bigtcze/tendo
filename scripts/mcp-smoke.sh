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
dump=$(mktemp)
# Recreating the app deletes the old container and its logs, so the logs of
# every app container are appended here before it goes away.
applogs=$(mktemp)
save_app_logs() {
  "${compose[@]}" logs --no-color app >>"$applogs"
}
cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    save_app_logs 2>/dev/null || true
    python3 -I - "$applogs" "$secrets" <<'PY' >&2 || true
import pathlib,sys
text=pathlib.Path(sys.argv[1]).read_text(errors='replace')
for secret in sorted(filter(None, pathlib.Path(sys.argv[2]).read_text().splitlines()), key=len, reverse=True):
  text=text.replace(secret,'[REDACTED]')
print(text)
PY
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    echo 'MCP Compose cleanup failed' >&2
    (( result != 0 )) || result=1
  fi
  rm -f "$fixtures" "$secrets" "$state" "$dump" "$applogs" || { echo 'MCP smoke temporary cleanup failed' >&2; (( result != 0 )) || result=1; }
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
  save_app_logs
  TENDO_MCP_ENABLED=$1
  "${compose[@]}" up -d --no-deps --wait --wait-timeout 180 app >/dev/null
  wait_ready
}
# The psql argv as JSON, so a checkout path with spaces survives.
psql_argv=$(python3 -I -c 'import json,sys;print(json.dumps(sys.argv[1:]))' \
  "${compose[@]}" exec -T postgres psql -U postgres -d tendo -At)
drive() {
  MCP_SMOKE_ORIGIN="$origin" MCP_SMOKE_SETUP_TOKEN="$TENDO_SETUP_TOKEN" MCP_SMOKE_FIXTURES="$fixtures" \
    MCP_SMOKE_SECRETS="$secrets" MCP_SMOKE_PSQL="$psql_argv" \
    python3 -I "$root/scripts/mcp_smoke_driver.py" "$1" "$state"
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
# No collected secret appears in any MCP table, neither as text nor as the raw
# decoded bytes pg_dump prints for a bytea column (\x<hex>): hash columns are
# 32 bytes, like a raw 43-character token. This proves the plaintext is absent,
# not which digest is stored.
"${compose[@]}" exec -T postgres pg_dump -U postgres -d tendo --data-only \
  -t mcp_clients -t mcp_grants -t mcp_authorization_codes -t mcp_access_tokens \
  -t mcp_refresh_tokens -t mcp_authorization_requests -t mcp_grant_events >"$dump"
python3 -I - "$dump" "$secrets" <<'PY'
import base64,binascii,pathlib,re,sys
dump=pathlib.Path(sys.argv[1]).read_text(errors='replace')
lower=dump.lower()
for secret in filter(None, pathlib.Path(sys.argv[2]).read_text().splitlines()):
  if secret in dump: sys.exit('An MCP secret is stored in plaintext in PostgreSQL')
  if re.fullmatch(r'[A-Za-z0-9_-]{43}', secret):
    raw=binascii.hexlify(base64.urlsafe_b64decode(secret+'=')).decode()
    if raw in lower: sys.exit('An MCP secret is stored as raw bytes in PostgreSQL')
PY
save_app_logs
while IFS= read -r secret; do
  [[ -z "$secret" ]] && continue
  if grep -F -- "$secret" "$applogs" >/dev/null; then echo 'An MCP token, code, handle, verifier or cookie value appeared in app logs' >&2; exit 1; fi
done <"$secrets"
if grep -E 'code=|#request=|code_verifier|refresh_token=' "$applogs" >/dev/null; then echo 'MCP OAuth parameter marker found in app logs' >&2; exit 1; fi
printf 'MCP discovery, registration, consent, token, revocation, endpoint, kill-switch and log-privacy smoke passed.\n'
