#!/bin/sh
set -eu
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=application_password="$TENDO_DATABASE_PASSWORD" <<'SQL'
CREATE ROLE tendo LOGIN PASSWORD :'application_password';
GRANT CONNECT ON DATABASE tendo TO tendo;
SQL
