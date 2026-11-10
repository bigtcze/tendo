# ADR 0013: Two-service Compose and app-startup database initialization

Status: accepted

## Context

Before 4.0 the published deployment ran three Compose services. PostgreSQL created the restricted `tendo` role through an inline `postgres-init` config script, which PostgreSQL runs only when the data volume is empty. A one-shot `migrate` container then applied the embedded migrations as the administrator, and only after it completed successfully did the app start and serve through the restricted role. That layout made `docker compose up --wait` depend on a completed job, kept administrator credentials out of the long-lived app container, and required a separate service in every override, smoke and document. It also could not provision the role on an existing pre-4.0 volume, and a failed initialization left operators reading three services.

The 4.0 design simplifies the deployment to exactly two services, PostgreSQL and the app, without adding a sidecar, bootstrap job, manual SQL step or a new required secret. The detailed audit, ordering and failure requirements are in section 7 of the [V4 baseline audit](../design/v4-baseline-audit.md#7-deployment-three-compose-services--two-v4-16).

## Decision

- `compose.yaml` defines exactly `postgres` and `app`, with no root `configs:` block, no `postgres.configs`, and no `migrate` service. The app depends only on a healthy PostgreSQL service.
- Compose builds `TENDO_DATABASE_ADMIN_URL` from the existing `POSTGRES_PASSWORD` and passes it, plus the existing restricted `DATABASE_URL`, to the app. No new `.env` key exists.
- When `TENDO_DATABASE_ADMIN_URL` is set, normal app startup runs before any HTTP listener exists:
  1. open a single privileged connection and ping it;
  2. idempotently create role `tendo` with `LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS` and the password taken from `DATABASE_URL` when absent; when present, validate its attributes and reject any role membership, never rewrite its password;
  3. grant `CONNECT`/`USAGE` and revoke `CREATE` on schema `public` from `PUBLIC`;
  4. apply the existing embedded migrations with their advisory lock, ledger, transaction and dirty/newer refusal semantics;
  5. close the privileged connection;
  6. open the restricted pool, run the serving-role restriction check, validate the schema, and only then listen.
- Any failure exits non-zero with a safe message such as `database administrator connection failed`, `database runtime role tendo has elevated privileges or role memberships`, `database serving role has elevated privileges or role memberships`, `database has a dirty migration` or `database schema is newer than this application`.
- Without the admin URL, as in native tests and `go run`, no bootstrap or migration runs; the app performs the serving-role check and schema validation before serving.
- The image's `/tendo healthcheck` subcommand lets Compose prove readiness without a shell in the distroless image. Compose uses it to make `docker compose up -d --wait` wait until bootstrap and migrations have completed.
- Restoring a dump into an empty database uses `/tendo ensure-role`, which bootstraps only the role, before `pg_restore` and the normal app start.

## Consequences

- Fresh installs and upgrades both work with one `docker compose up -d --wait --remove-orphans`; existing volumes, accounts, memberships and data are preserved, and the app's migration ledger prevents double application.
- Startup is fail-closed. A dirty or newer schema, an elevated or member-connected runtime role, wrong administrator credentials, or a `DATABASE_URL` that names an administrator never reaches HTTP readiness, and recovery after an operator fix is a restart with no data loss.
- The long-lived app container holds the PostgreSQL administrator password in its environment even after the privileged connection closes. It is visible through `docker inspect` and to any code running in that container. This is a deliberate reduction in isolation compared with the former short-lived `migrate` container and must never be described as equivalent. Routine requests still use the restricted `tendo` role.
- Because no separate secret is introduced, operators keep the two existing passwords. The app never silently rotates an existing role's password, so changing `TENDO_DATABASE_PASSWORD` later still fails to connect rather than repairing the database.
- Every Compose override, smoke, backup/restore guide and release description must use only the two services; `docker compose run migrate` is no longer a supported or documented operation.
- A sidecar, a retained one-shot migration service, an entrypoint shell script outside the app, or an additional generated secret was rejected because each reintroduces a service, a manual step or a new credential for no runtime isolation benefit.
