#!/usr/bin/env bash
# Prove the two-service Compose stack initializes the database from the app container:
# fresh start, no-op restart, concurrent starts, and every startup failure is fail-closed
# and recoverable without losing data.
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-compose-startup-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD
export TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT
export TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN
export TENDO_OIDC_ISSUER TENDO_OIDC_CLIENT_ID TENDO_OIDC_CLIENT_SECRET
export TENDO_OIDC_CLIENT_SECRET_FILE TENDO_OIDC_DISPLAY_NAME
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=
TENDO_OIDC_ISSUER=
TENDO_OIDC_CLIENT_ID=
TENDO_OIDC_CLIENT_SECRET=
TENDO_OIDC_CLIENT_SECRET_FILE=
TENDO_OIDC_DISPLAY_NAME=
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml")
admin_url="postgres://postgres:${POSTGRES_PASSWORD}@postgres:5432/tendo?sslmode=disable"
origin=$TENDO_PUBLIC_URL
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-compose-startup-smoke.XXXXXX")
chmod 700 "$temp_dir"
cleanup() {
  local result=$? name
  trap - EXIT
  if (( result != 0 )); then
    if ! "${compose[@]}" logs --no-color >"$temp_dir/logs" 2>&1; then
      printf 'Could not collect Compose logs for failed project %s\n' "$project" >&2
    else
      python3 - "$temp_dir/logs" <<'REDACT'
import os, re, sys
text = open(sys.argv[1], encoding="utf-8", errors="replace").read()
for name in ("POSTGRES_PASSWORD", "TENDO_DATABASE_PASSWORD"):
    if os.environ.get(name):
        text = text.replace(os.environ[name], "[REDACTED]")
text = re.sub(r"(?i)(POSTGRES_PASSWORD|TENDO_DATABASE_PASSWORD)(=|%3[dD])[^\s&]+", r"\1\2[REDACTED]", text)
print(text, end="", file=sys.stderr)
REDACT
    fi
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    printf 'Compose cleanup failed for project %s\n' "$project" >&2
    (( result != 0 )) || result=1
  fi
  # `compose run` one-off containers are not always removed by `down`.
  mapfile -t leftovers < <(docker ps -aq --filter "label=com.docker.compose.project=$project")
  if (( ${#leftovers[@]} )); then
    if ! docker rm -f "${leftovers[@]}" >/dev/null; then
      printf 'Could not remove leftover containers for project %s\n' "$project" >&2
      (( result != 0 )) || result=1
    fi
  fi
  if ! rm -rf -- "$temp_dir"; then
    printf 'Could not remove private smoke directory\n' >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT

compose_config() {
  "${compose[@]}" config --format json | python3 -c 'import json,sys; c=json.load(sys.stdin); assert set(c["services"])=={"postgres","app"}, sorted(c["services"]); assert "configs" not in c, sorted(c); assert "migrate" not in c["services"]'
}
compose_config

psql_at() {
  "${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "$1"
}
wait_ready() {
  local _ deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  printf 'App did not become ready: %s/health/ready\n' "$origin" >&2
  return 1
}
app_health() {
  local container
  container=$("${compose[@]}" ps -q app)
  [[ -n "$container" ]] || { printf 'No app container\n' >&2; return 1; }
  docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container"
}
row_counts() {
  psql_at "SELECT (SELECT count(*) FROM installation_state)||'|'||(SELECT count(*) FROM user_accounts)||'|'||(SELECT count(*) FROM households)||'|'||(SELECT count(*) FROM subjects)||'|'||(SELECT count(*) FROM items)||'|'||(SELECT count(*) FROM item_completions)||'|'||(SELECT count(*) FROM tendo_schema_migrations)"
}
ledger() {
  psql_at "SELECT COALESCE(max(version),0)::text||':'||COALESCE(bool_or(dirty),false)::text FROM tendo_schema_migrations"
}
expect_failure() {
  local label=$1 expected=$2 log status
  shift 2
  log="$temp_dir/failure-$label.log"
  set +e
  "${compose[@]}" run -T --rm --no-deps "$@" app >"$log" 2>&1
  status=$?
  set -e
  (( status != 0 )) || { printf '%s startup unexpectedly exited 0\n' "$label" >&2; cat "$log" >&2; exit 1; }
  grep -Fq -- "$expected" "$log" || {
    printf '%s startup log is missing %s\n' "$label" "$expected" >&2
    cat "$log" >&2
    exit 1
  }
  printf 'Failure case %s refused startup as expected.\n' "$label"
}
restart_and_wait() {
  "${compose[@]}" restart app >/dev/null
  wait_ready
}
wait_oneoff_healthy() {
  local name=$1 deadline=$((SECONDS + 180)) state health
  while (( SECONDS < deadline )); do
    state=$(docker inspect --format '{{.State.Status}}' "$name" 2>/dev/null || true)
    health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>/dev/null || true)
    [[ "$health" == healthy ]] && return 0
    if [[ "$state" == exited || "$state" == dead ]]; then
      printf 'One-off app %s exited before becoming healthy\n' "$name" >&2
      docker logs "$name" 2>&1 >&2 || true
      return 1
    fi
    sleep 1
  done
  printf 'One-off app %s did not become healthy\n' "$name" >&2
  docker logs "$name" 2>&1 >&2 || true
  return 1
}

migration_count=$(find "$root/backend/internal/platform/database" -maxdepth 1 -type f -regextype posix-extended -regex '.*/[0-9]+_.*[.]sql' -printf '%f\n' | wc -l)
(( migration_count > 0 )) || { printf 'No embedded migrations found\n' >&2; exit 1; }

# Fresh empty volume: the single app container provisions role and schema, then serves.
"${compose[@]}" up -d --build --wait --wait-timeout 240
wait_ready
[[ "$(app_health)" == healthy ]] || { printf 'Fresh app container is not healthy\n' >&2; exit 1; }
[[ "$(ledger)" == "${migration_count}:false" ]] || { printf 'Fresh ledger is %s, expected %s:false\n' "$(ledger)" "$migration_count" >&2; exit 1; }
role_flags=$(psql_at "SELECT rolsuper||'|'||rolcreatedb||'|'||rolcreaterole||'|'||rolbypassrls||'|'||rolreplication||'|'||rolinherit FROM pg_roles WHERE rolname='tendo'")
[[ "$role_flags" == 'false|false|false|false|false|false' ]] || { printf 'Role tendo flags are %s\n' "$role_flags" >&2; exit 1; }
[[ "$(psql_at "SELECT count(*) FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member WHERE r.rolname='tendo'")" == 0 ]] \
  || { printf 'Role tendo has role memberships\n' >&2; exit 1; }
# Mark data inside the database so recovery checks below prove it survives.
psql_at "UPDATE installation_state SET setup_required=false" >/dev/null
counts_before=$(row_counts)
ledger_before=$(ledger)

# A restart runs the same idempotent bootstrap: no duplicate ledger rows, no data change.
restart_and_wait
[[ "$(app_health)" == healthy ]] || { printf 'Restarted app container is not healthy\n' >&2; exit 1; }
[[ "$(ledger)" == "$ledger_before" ]] || { printf 'Restart changed the ledger: %s -> %s\n' "$ledger_before" "$(ledger)" >&2; exit 1; }
[[ "$(row_counts)" == "$counts_before" ]] || { printf 'Restart changed row counts\n' >&2; exit 1; }
[[ "$(psql_at "SELECT setup_required FROM installation_state")" == f ]] || { printf 'Restart lost database data\n' >&2; exit 1; }
printf 'Fresh start, role flags and no-op restart passed.\n'

# Wrong administrator password: the privileged connection fails before serving.
wrong_admin="postgres://postgres:$(openssl rand -hex 32)@postgres:5432/tendo?sslmode=disable"
expect_failure wrong-admin 'database administrator connection failed' -e "TENDO_DATABASE_ADMIN_URL=$wrong_admin"
wrong_secret=${wrong_admin#postgres://postgres:}
wrong_secret=${wrong_secret%%@*}
if grep -Fq -- "$wrong_secret" "$temp_dir/failure-wrong-admin.log"; then
  printf 'Wrong administrator password appeared in startup logs\n' >&2
  exit 1
fi

# Dirty migration: refuse, never repair automatically, then recover after an operator fix.
psql_at "UPDATE tendo_schema_migrations SET dirty=true WHERE version=(SELECT max(version) FROM tendo_schema_migrations)" >/dev/null
expect_failure dirty 'dirty migration'
psql_at "UPDATE tendo_schema_migrations SET dirty=false WHERE version=(SELECT max(version) FROM tendo_schema_migrations)" >/dev/null
restart_and_wait
[[ "$(ledger)" == "$ledger_before" && "$(row_counts)" == "$counts_before" ]] || { printf 'Dirty recovery lost data\n' >&2; exit 1; }
printf 'Dirty schema refusal and recovery passed.\n'

# Newer schema: refuse until the extra ledger row is removed.
newer=$(( ${ledger_before%%:*} + 1 ))
psql_at "INSERT INTO tendo_schema_migrations(version,dirty) VALUES($newer,false)" >/dev/null
expect_failure newer 'database schema is newer than this application'
psql_at "DELETE FROM tendo_schema_migrations WHERE version=$newer" >/dev/null
restart_and_wait
[[ "$(ledger)" == "$ledger_before" ]] || { printf 'Newer-schema recovery did not restore the ledger\n' >&2; exit 1; }
printf 'Newer schema refusal and recovery passed.\n'

# An existing role with elevated attributes is refused, not repaired.
psql_at "ALTER ROLE tendo CREATEROLE" >/dev/null
expect_failure elevated-role 'database runtime role tendo has elevated privileges or role memberships'
psql_at "ALTER ROLE tendo NOCREATEROLE" >/dev/null
restart_and_wait
printf 'Elevated existing role refusal and recovery passed.\n'

# An existing role with any membership is refused (NOINHERIT alone still allows SET ROLE).
psql_at "GRANT pg_write_all_data TO tendo" >/dev/null
expect_failure role-membership 'database runtime role tendo has elevated privileges or role memberships'
psql_at "REVOKE pg_write_all_data FROM tendo" >/dev/null
restart_and_wait
printf 'Existing role membership refusal and recovery passed.\n'

# Serving as the administrator is refused: with both URLs naming the same user configuration
# validation rejects it, and without the admin URL the serving-role check refuses the superuser.
expect_failure admin-same-user 'TENDO_DATABASE_ADMIN_URL must use a different user than DATABASE_URL' -e "DATABASE_URL=$admin_url"
expect_failure admin-serving-role 'database serving role has elevated privileges or role memberships' -e "DATABASE_URL=$admin_url" -e TENDO_DATABASE_ADMIN_URL=
[[ "$(psql_at "SELECT setup_required FROM installation_state")" == f ]] || { printf 'Failure cases lost database data\n' >&2; exit 1; }

# Concurrent starts on a fresh volume: the advisory lock serializes the ledger; both serve.
"${compose[@]}" down --volumes --remove-orphans >/dev/null
"${compose[@]}" up -d --build --wait --wait-timeout 120 postgres
name_a="${project}-concurrent-a"
name_b="${project}-concurrent-b"
"${compose[@]}" run -d --no-deps --name "$name_a" app >/dev/null
"${compose[@]}" run -d --no-deps --name "$name_b" app >/dev/null
wait_oneoff_healthy "$name_a"
wait_oneoff_healthy "$name_b"
concurrent_ledger=$(psql_at "SELECT count(*)::text||':'||max(version)::text||':'||COALESCE(bool_or(dirty),false)::text FROM tendo_schema_migrations")
[[ "$concurrent_ledger" == "${migration_count}:${migration_count}:false" ]] \
  || { printf 'Concurrent ledger is %s, expected %s\n' "$concurrent_ledger" "${migration_count}:${migration_count}:false" >&2; exit 1; }
for name in "$name_a" "$name_b"; do
  docker logs "$name" >"$temp_dir/$name.log" 2>&1
  if grep -Fq 'runtime failed' "$temp_dir/$name.log"; then
    printf 'Concurrent app %s logged a startup failure\n' "$name" >&2
    cat "$temp_dir/$name.log" >&2
    exit 1
  fi
done
docker rm -f "$name_a" "$name_b" >/dev/null
printf 'Concurrent app starts serialized and both became healthy.\n'
printf 'Two-service database startup smoke passed.\n'
