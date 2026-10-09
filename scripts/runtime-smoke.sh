#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-runtime-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-runtime-smoke.XXXXXX")
export COMPOSE_PROJECT_NAME=$project
export POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_OIDC_ISSUER TENDO_OIDC_CLIENT_ID TENDO_OIDC_CLIENT_SECRET TENDO_OIDC_CLIENT_SECRET_FILE TENDO_OIDC_DISPLAY_NAME
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
TENDO_OIDC_ISSUER=
TENDO_OIDC_CLIENT_ID=
TENDO_OIDC_CLIENT_SECRET=
TENDO_OIDC_CLIENT_SECRET_FILE=
TENDO_OIDC_DISPLAY_NAME=
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://127.0.0.1:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
compose=(docker compose --project-name "$project" -f "$root/compose.yaml")
migrate=(docker compose --profile migration --project-name "$project" -f "$root/compose.yaml")
cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    if ! "${compose[@]}" logs --no-color >"$temp_dir/logs" 2>&1; then
      printf 'Could not collect Compose logs for failed project %s\n' "$project" >&2
    else
      python3 - "$temp_dir/logs" <<'PY'
import os, re, sys
text = open(sys.argv[1], encoding="utf-8").read()
for name in ("POSTGRES_PASSWORD", "TENDO_DATABASE_PASSWORD"):
    value = os.environ[name]
    text = text.replace(value, "[REDACTED]")
text = text.replace(os.environ.get("TENDO_SETUP_TOKEN", ""), "[REDACTED]") if os.environ.get("TENDO_SETUP_TOKEN") else text
text = re.sub(r"(?i)(POSTGRES_PASSWORD|TENDO_DATABASE_PASSWORD|TENDO_SETUP_TOKEN)(=|%3[dD])[^\s&]+", r"\1\2[REDACTED]", text)
print(text, end="", file=sys.stderr)
PY
    fi
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans; then
    printf 'Compose cleanup failed for project %s\n' "$project" >&2
    (( result != 0 )) || result=1
  fi
  if ! rm -rf -- "$temp_dir"; then
    printf 'Could not remove smoke temporary directory %s\n' "$temp_dir" >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT

poll_status() {
  local path=$1 expected=$2 deadline=$((SECONDS + 60)) status
  while (( SECONDS < deadline )); do
    if status=$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 2 "http://127.0.0.1:${TENDO_HOST_PORT}$path"); then
      if [[ "$status" == "$expected" ]]; then return 0; fi
    else
      status=connection-failed
    fi
    sleep 1
  done
  printf 'Timed out waiting for %s HTTP %s; last status=%s\n' "$path" "$expected" "$status" >&2
  return 1
}

"${compose[@]}" up -d --build --wait --wait-timeout 90
poll_status /health/live 200
poll_status /health/ready 200
RUNTIME_HEALTH_ORIGIN="http://127.0.0.1:${TENDO_HOST_PORT}" node "$root/api/check-health.mjs"
curl --silent --show-error --max-time 5 -D "$temp_dir/headers" -o "$temp_dir/body" \
  -H 'X-Request-ID: smoke_assertion-01' "http://127.0.0.1:${TENDO_HOST_PORT}/health/ready"
python3 - "$temp_dir/headers" "$temp_dir/body" <<'PY'
import json, re, sys
with open(sys.argv[1], encoding="utf-8") as source:
    headers = source.read().lower()
with open(sys.argv[2], encoding="utf-8") as source:
    body = json.load(source)
assert re.search(r"^x-request-id: [a-z0-9._-]{1,64}\r?$", headers, re.M), headers
assert re.search(r"^cache-control: no-store\r?$", headers, re.M), headers
assert body == {"status": "ok"}, body
PY
"${compose[@]}" stop postgres
poll_status /health/ready 503
poll_status /health/live 200
RUNTIME_HEALTH_ORIGIN="http://127.0.0.1:${TENDO_HOST_PORT}" RUNTIME_HEALTH_EXPECT=unavailable node "$root/api/check-health.mjs"
"${compose[@]}" start postgres
"${compose[@]}" up -d --wait --wait-timeout 90 postgres
poll_status /health/ready 200

container=$("${compose[@]}" ps -q app)
[[ $(docker inspect --format '{{.Config.User}}' "$container") == 65532:65532 ]]
[[ $(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$container") == true ]]
[[ $(docker inspect --format '{{json .NetworkSettings.Ports}}' "$("${compose[@]}" ps -q postgres)") == *'null'* ]]

"${compose[@]}" stop app >/dev/null
"${compose[@]}" start app >/dev/null
"${compose[@]}" up -d --wait --wait-timeout 90 app
container=$("${compose[@]}" ps -q app)
started=$SECONDS
docker kill --signal=TERM "$container" >/dev/null
deadline=$((SECONDS + 15))
while [[ $(docker inspect --format '{{.State.Running}}' "$container") == true ]]; do
  if (( SECONDS >= deadline )); then
    printf 'app did not exit within 15 seconds after SIGTERM (elapsed %d seconds)\n' "$((SECONDS - started))" >&2
    exit 1
  fi
  sleep 1
done
exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$container")
[[ "$exit_code" == 0 ]]
