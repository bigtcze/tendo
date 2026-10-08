#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-setup-integration-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
db_name="tendo_test"
admin_password=$(openssl rand -hex 32)
role_password=$(openssl rand -hex 32)
container="${project}-postgres"
network="${project}-network"
cache="${HOME}/.cache/tendo-go-build"
mkdir -p "$cache"
cleanup() {
  local result=$?
  trap - EXIT
  if ! docker rm -fv "$container" >/dev/null 2>&1; then
    printf 'PostgreSQL test container cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  if ! docker network rm "$network" >/dev/null 2>&1; then
    printf 'PostgreSQL test network cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT
docker network create "$network" >/dev/null
docker run -d --name "$container" --network "$network" \
  -e POSTGRES_USER=postgres -e "POSTGRES_PASSWORD=$admin_password" -e "POSTGRES_DB=$db_name" \
  postgres:18.6-bookworm >/dev/null
ready=0
for attempt in $(seq 1 60); do
  # TCP probe: the image's init-time temporary server listens only on the Unix socket.
  if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres -d "$db_name" >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if (( ready == 0 )); then
  printf 'Disposable PostgreSQL did not become ready\n' >&2
  docker logs "$container" >&2
  exit 1
fi
docker exec -i "$container" psql -U postgres -d "$db_name" -v ON_ERROR_STOP=1 -v role_password="$role_password" <<'SQL' >/dev/null
CREATE ROLE tendo LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD :'role_password';
GRANT CONNECT ON DATABASE tendo_test TO tendo;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO tendo;
SQL
admin_url="postgres://postgres:${admin_password}@${container}:5432/${db_name}?sslmode=disable"
role_url="postgres://tendo:${role_password}@${container}:5432/${db_name}?sslmode=disable"
docker run --rm --network "$network" \
  -e "TEST_DATABASE_ADMIN_URL=$admin_url" -e "TEST_DATABASE_URL=$role_url" \
  -e GOCACHE=/go-cache -v "$root/backend:/workspace/backend" -v "$cache:/go-cache" \
  -w /workspace/backend golang:1.26.8 go test -race -p 1 ./internal/identity/postgres ./internal/household/postgres ./internal/subject/postgres ./internal/item/postgres ./internal/platform/database -count=1
