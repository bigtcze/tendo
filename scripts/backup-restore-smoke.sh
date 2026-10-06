#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-backup-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-backup-smoke.XXXXXX")
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD
# Compose requires a public origin even when the app is disabled in this smoke test.
export TENDO_PUBLIC_URL=http://localhost
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
cleanup() {
  local result=$?
  trap - EXIT
  if ! "${compose[@]}" down --volumes --remove-orphans; then
    printf 'Compose cleanup failed for project %s\n' "$project" >&2
    (( result != 0 )) || result=1
  fi
  if ! rm -rf -- "$temp_dir"; then
    printf 'Could not remove private smoke directory\n' >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT

# A temporary profile override excludes the app; only private PostgreSQL is started.
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$temp_dir/compose.yaml")
cat >"$temp_dir/compose.yaml" <<'YAML'
services:
  app:
    profiles: [disabled-for-backup-smoke]
YAML
"${compose[@]}" up -d --wait --wait-timeout 90 postgres
psql() { "${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres "$@"; }
psql -d tendo <<'SQL'
CREATE SCHEMA smoke AUTHORIZATION tendo;
GRANT USAGE, CREATE ON SCHEMA smoke TO tendo;
SET ROLE tendo;
CREATE TABLE smoke.people (
  id integer PRIMARY KEY,
  name text NOT NULL,
  born_on date,
  related_id integer REFERENCES smoke.people(id),
  CONSTRAINT name_nonblank CHECK (length(name) > 0)
);
INSERT INTO smoke.people VALUES (1, 'Žofie', '2020-02-29', NULL), (2, '家族', NULL, 1);
RESET ROLE;
SQL
"${compose[@]}" exec -T postgres pg_dump -U postgres -d tendo --format=custom >"$temp_dir/backup.dump"
test -s "$temp_dir/backup.dump"
"${compose[@]}" exec -T postgres sh -ec 'createdb -U postgres tendo_restore && pg_restore --exit-on-error --single-transaction -U postgres -d tendo_restore' <"$temp_dir/backup.dump"
"${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres -c 'CREATE DATABASE tendo_existing_restore;'
"${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo_existing_restore -c "CREATE TABLE sentinel (value text); INSERT INTO sentinel VALUES ('preserved');"
if "${compose[@]}" exec -T postgres sh -ec 'createdb -U postgres tendo_existing_restore && pg_restore --exit-on-error --single-transaction -U postgres -d tendo_existing_restore' <"$temp_dir/backup.dump" >"$temp_dir/existing.out" 2>&1; then
  printf 'Restore unexpectedly proceeded against an existing database\n' >&2
  exit 1
fi
psql -d tendo_existing_restore <<'SQL'
DO $$ BEGIN
  IF (SELECT value FROM sentinel) <> 'preserved' THEN RAISE EXCEPTION 'sentinel changed'; END IF;
  IF to_regnamespace('smoke') IS NOT NULL THEN RAISE EXCEPTION 'restore added fixture schema to existing database'; END IF;
END $$;
SQL
psql -d tendo_restore <<'SQL'
DO $$
DECLARE r record;
BEGIN
  IF (SELECT count(*) FROM smoke.people) <> 2 THEN RAISE EXCEPTION 'row count mismatch'; END IF;
  IF NOT EXISTS (SELECT 1 FROM smoke.people WHERE id=1 AND name='Žofie' AND born_on='2020-02-29' AND related_id IS NULL) THEN RAISE EXCEPTION 'first fixture row mismatch'; END IF;
  IF NOT EXISTS (SELECT 1 FROM smoke.people WHERE id=2 AND name='家族' AND born_on IS NULL AND related_id=1) THEN RAISE EXCEPTION 'related fixture row mismatch'; END IF;
  SELECT pg_get_userbyid(c.relowner) AS owner INTO r FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='smoke' AND c.relname='people';
  IF r.owner <> 'tendo' THEN RAISE EXCEPTION 'table owner mismatch: %', r.owner; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='smoke.people'::regclass AND contype='f') OR NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='smoke.people'::regclass AND conname='name_nonblank') THEN RAISE EXCEPTION 'constraints missing'; END IF;
  IF (SELECT rolsuper OR rolcreatedb OR rolcreaterole FROM pg_roles WHERE rolname='tendo') THEN RAISE EXCEPTION 'tendo role is privileged'; END IF;
END $$;
SQL
"${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U tendo -d tendo_restore <<'SQL'
SELECT name FROM smoke.people ORDER BY id;
INSERT INTO smoke.people (id, name) VALUES (3, 'write-check');
DO $$
BEGIN
  BEGIN
    INSERT INTO smoke.people (id, name) VALUES (4, '');
    RAISE EXCEPTION 'empty name unexpectedly accepted';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
  BEGIN
    INSERT INTO smoke.people (id, name, related_id) VALUES (5, 'orphan', 999);
    RAISE EXCEPTION 'orphan reference unexpectedly accepted';
  EXCEPTION WHEN foreign_key_violation THEN NULL;
  END;
END $$;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM smoke.people WHERE id IN (4, 5)) THEN RAISE EXCEPTION 'rejected rows persisted'; END IF;
END $$;
SQL
"${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U tendo -d tendo_restore <<'SQL'
DO $$ BEGIN
  BEGIN
    CREATE ROLE tendo_smoke_denied;
    RAISE EXCEPTION 'restricted role unexpectedly created a role';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
END $$;
SQL
# A truncated archive must fail restore; isolate its target and explicitly assert expected failure.
"${compose[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres -c 'CREATE DATABASE tendo_corrupt_restore;'
python3 - "$temp_dir/backup.dump" "$temp_dir/corrupt.dump" <<'PY'
import sys
payload = open(sys.argv[1], 'rb').read()
if len(payload) < 100:
    raise SystemExit('archive unexpectedly small')
open(sys.argv[2], 'wb').write(payload[:len(payload)//2])
PY
if "${compose[@]}" exec -T postgres pg_restore --exit-on-error --single-transaction -U postgres -d tendo_corrupt_restore <"$temp_dir/corrupt.dump" >"$temp_dir/corrupt.out" 2>&1; then
  printf 'Truncated archive unexpectedly restored successfully\n' >&2
  exit 1
fi
if ! python3 - "$temp_dir/corrupt.out" <<'PY'
import sys
error = open(sys.argv[1], encoding='utf-8', errors='replace').read().lower()
if not any(token in error for token in ('unexpected end', 'could not read', 'could not find header', 'truncated', 'corrupt')):
    raise SystemExit('truncated archive failed for an unexpected reason')
PY
then
  printf 'Truncated archive failure was not identified as corruption/read failure\n' >&2
  exit 1
fi
psql -d tendo_corrupt_restore <<'SQL'
DO $$ BEGIN
  IF to_regnamespace('smoke') IS NOT NULL THEN RAISE EXCEPTION 'failed restore left fixture schema'; END IF;
END $$;
SQL
printf 'Backup/restore smoke passed, including truncated-archive rejection.\n'
