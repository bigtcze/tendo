# PostgreSQL backup and restore

This operational guide covers PostgreSQL data in the development/CI Compose stack. The backup smoke uses a test-only fixture, not a Tendo runtime schema. It does not prove recovery of owner/household records or a complete household product; application-schema recovery remains unproven.

## Back up

Prerequisites: Docker Compose, configured `.env` credentials (see [runtime configuration](configuration.md)), and a running PostgreSQL 18 Compose service. Keep PostgreSQL's major version the same for restore; PostgreSQL does not support downgrading a data directory or restoring into an older major version.

Stop or quiet the app during the backup to avoid writes during capture. Store the archive privately: it can contain all database data. Keep it off-host in secure storage and protect it with appropriate access controls/encryption. The custom-format archive does not include PostgreSQL roles, role passwords, server configuration, or `.env`; securely retain configuration/credentials separately and recreate the `tendo` role from fresh Compose credentials before restore.

From the repository root, the evidence smoke runs the equivalent dump using PostgreSQL's bundled tools:

```sh
docker compose exec -T postgres pg_dump -U postgres -d tendo --format=custom > tendo-backup.dump
```

Use a private destination with restrictive permissions (for example, set `umask 077` first). Do not publish PostgreSQL to a host/network port to make a backup.

## Restore safely

Keep the app stopped throughout recovery. Prefer restoring to a new named database first; do not overwrite or drop the current database as an initial recovery step. Ensure the Compose PostgreSQL service is running the same major version and has the intended fresh Compose credentials/role bootstrap. The standard dump preserves object owners, so the required roles must exist at restore time.

The target must be new and must not already exist. Stop if it exists; do not drop or reuse it. Run creation and restore as one fail-fast sequence so restore never runs if database creation fails:

```sh
docker compose exec -T postgres sh -ec 'createdb -U postgres tendo_restore && pg_restore --exit-on-error --single-transaction -U postgres -d tendo_restore' < tendo-backup.dump
```

Before any cutover, verify expected rows, owners, constraints, and application access against the restored database. Only after verification, plan an explicit configuration/cutover with a tested rollback. This guide does not guess a `DATABASE_URL` recipe or prescribe destructive commands. Never run `dropdb`, `DROP DATABASE`, or restore over a live database without an independently verified backup, explicit target confirmation, and a recovery plan. Resume the app only when the intended data source is confirmed.

Run the isolated evidence check from the repository root with `bash scripts/backup-restore-smoke.sh`. It uses a uniquely named Compose project and temporary volume, generated throwaway credentials, a private temporary custom-format archive, and a test-only fixture; it removes only those resources. It proves restore into a fresh database, fixture owner/constraints/data and restricted role behavior, refuses an existing target without changing its sentinel data, and rejects a truncated archive without leaving fixture schema behind. It does not claim PITR/WAL recovery, platform infrastructure coverage, or restore of a household application schema.
