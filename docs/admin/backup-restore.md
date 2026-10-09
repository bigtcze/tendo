# Back up and restore Tendo

All Tendo data lives in PostgreSQL: accounts, the household, members, invitations, people and things, items, and completion history. A standard PostgreSQL dump is a complete backup. There is no Tendo-specific backup format.

Run every command from the directory that holds Tendo's `compose.yaml` and `.env`.

## What a backup contains

Store the archive privately. It contains every household record, plus:

- password hashes;
- digests of login sessions;
- digests of pending invitation links.

Digests are not the original secrets, but they still let a restored copy accept them. Login sessions that had not expired when you took the backup work again after a restore. An invitation link that was still pending also works again until it expires. Keep backups off the server in encrypted, access-controlled storage.

The archive does **not** contain your `.env` file, PostgreSQL role passwords, or the OIDC client secret. Keep a private copy of your configuration separately.

## Back up

1. Stop the app so nothing changes during the backup. PostgreSQL keeps running:

   ```sh
   docker compose stop app
   ```

2. Write the backup to a private file:

   ```sh
   umask 077
   docker compose exec -T postgres pg_dump -U postgres -d tendo --format=custom > tendo-backup.dump
   ```

3. Check that the file is not empty, then start the app again:

   ```sh
   test -s tendo-backup.dump && docker compose up -d
   ```

Do not publish PostgreSQL on a host or network port to make a backup.

## Restore onto a fresh installation

Use this after losing a server, or when moving Tendo to another one. The target must be a **new, empty** installation, with a new data volume. Never restore over a database that already holds Tendo data.

1. Prepare the new installation as in the [quick start](../../README.md), but **do not start it yet**. Use the same Tendo version, and a `.env` with the same `TENDO_PUBLIC_URL` and OIDC settings you had before. You can generate new database passwords; the backup does not depend on them. Leave `TENDO_SETUP_TOKEN` empty, because the restored data already has an owner.

2. Start only PostgreSQL. This creates the empty `tendo` database and the restricted `tendo` role:

   ```sh
   docker compose up -d --wait postgres
   ```

3. Confirm that the database is empty. The command must print `0`:

   ```sh
   docker compose exec -T postgres psql -U postgres -d tendo -At -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'"
   ```

   If it prints anything else, stop: this is not a fresh installation.

4. Restore the backup. On any error, nothing is written:

   ```sh
   docker compose exec -T postgres pg_restore --exit-on-error --single-transaction -U postgres -d tendo < tendo-backup.dump
   ```

5. Start Tendo:

   ```sh
   docker compose up -d --build --wait
   ```

6. Sign in with your usual login and check your household, people and things, items, and history.

To keep the old installation's data volume untouched while you check the restore on the same machine, give the new installation its own Compose project name, for example `docker compose -p tendo-restored …` in every command above. Stop the old app first, so both installations never use the same port.

Restore with the same PostgreSQL image version that `compose.yaml` uses for your Tendo release. PostgreSQL cannot restore into an older major version.

## Test a backup without touching your installation

You can restore a backup into a separate database next to the live one. Tendo does not use that database:

```sh
docker compose exec -T postgres sh -ec 'createdb -U postgres tendo_restore_test && pg_restore --exit-on-error --single-transaction -U postgres -d tendo_restore_test' < tendo-backup.dump
```

`createdb` fails if the name already exists, so the restore never runs over an existing database. Inspect the result with `psql`, then remove the test database:

```sh
docker compose exec -T postgres dropdb -U postgres tendo_restore_test
```

Double-check the database name before running `dropdb`. Never drop `tendo`.

## How this is tested

Two automated checks run on every change:

- `bash scripts/household-backup-smoke.sh` follows the steps above end to end. It builds a household through the app: owner, member, people and things, one-off, fixed, and after-completion items, completion history including an undone completion, archived records, and a pending invitation. It backs that household up with the backup command and restores it into a second installation with new database passwords. It then checks that every household, member, subject, item, and history response is identical, that every table has the same number of rows, and that the restored app still works. Specifically, it checks that sign-in, an existing session, a repeated completion request, undo, new records, and the pending invitation all work, and that the restricted database role still cannot delete history.
- `bash scripts/backup-restore-smoke.sh` checks restore safety. Restoring into an existing database is refused without changing it, and a truncated archive fails without leaving partial data behind.

Neither check covers point-in-time recovery (WAL archiving) or backups of the host itself.
