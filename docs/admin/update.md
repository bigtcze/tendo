# Update Tendo

Tendo is published as the container image `ghcr.io/bigtcze/tendo`. `TENDO_VERSION` in `.env` selects which tag Compose runs:

| `TENDO_VERSION` | What you get |
| --- | --- |
| `latest` (default) | The newest release, including a future major version. |
| `v1` | The newest 1.x release. A future 2.0 is never installed until you change this value. |
| `v1.0.0` | Exactly that release. Change the value to update. |

Every release is listed with its changes on the [releases page](https://github.com/bigtcze/tendo/releases). Read the notes before you update.

Run every command from the directory that holds Tendo's `compose.yaml` and `.env`.

## Update

1. [Back up](backup-restore.md#back-up) first. An update can change the database, and the backup is the only way back.

2. Get the Compose file of the new release. Releases can change it, for example to update PostgreSQL:

   ```sh
   curl -fsSLO https://github.com/bigtcze/tendo/releases/latest/download/compose.yaml
   ```

   This overwrites `compose.yaml`. Keep your own changes in `.env` or in a separate Compose override file, never in `compose.yaml`. If you pin `TENDO_VERSION` to a version that is not the newest, download the Compose file of that release instead: replace `latest/download` with `download/v1.0.0` (your version).

3. Pull the new images and restart:

   ```sh
   docker compose pull
   docker compose up -d --wait
   ```

   Compose runs the one-shot `migrate` container and starts the app only after migration succeeds. Migrations keep your data. The app is unavailable for a few seconds while it restarts.

4. Check that Tendo is ready and sign in:

   ```sh
   curl --fail --show-error http://localhost:8080/health/ready
   ```

   If you changed `TENDO_HOST_PORT`, replace `8080` in this command with that value. This checks the app's local HTTP port, not the reverse proxy's public HTTPS port. If the app is still starting, wait a few seconds and run it again.

5. Compare the release's [`example.env`](https://github.com/bigtcze/tendo/releases/latest/download/example.env) with your `.env` and add any new settings you want. The [configuration reference](configuration.md) describes each one and its default.

## Updating from 1.x to 2.0

Tendo 2.0 keeps all your data and adds no required settings. Take a [backup](backup-restore.md#back-up) first, then follow the update steps above. If `TENDO_VERSION` is `v1` or `v1.0.0`, change it to `v2` (or `latest`) first.

The 2.0 migration adds search and sorting support to the database:

- It installs the PostgreSQL extensions `unaccent` and `pg_trgm` and builds a search index. The published `compose.yaml` migrates as the PostgreSQL administrator, which is allowed to do this. If you run your own PostgreSQL with a less privileged migration role, that role must be allowed to install these two extensions.
- It rewrites every item row once to fill the search column. With many items this takes longer than an ordinary update; the app starts only after the migration finishes.

Going back to 1.x afterwards needs the backup you took before updating; see [Go back to an earlier version](#go-back-to-an-earlier-version).

## Moving from a Git checkout

Before version 1.0, Tendo was installed with `git clone` and built locally. Such a checkout switches to the published image with one last pull:

1. Check that `docker compose version` reports 2.23.1 or newer. The 1.0 Compose file does not work with older versions.
2. [Back up](backup-restore.md#back-up).
3. In the existing checkout, get version 1.0 and start it:

   ```sh
   git pull --ff-only
   docker compose pull
   docker compose up -d --wait
   ```

Your data stays where it is. Without `TENDO_VERSION` in `.env`, Compose uses `latest`. From now on, follow the update steps above in the same directory. Do not move the installation to a directory with a different name: Compose names the data volume after the directory, so a new name starts with an empty database.

## Go back to an earlier version

An older Tendo cannot use a database whose schema version is newer than it supports. If an update advanced the schema, the older app stops with `database schema validation failed`, and its migration stops with `database schema is newer than this application`.

To go back, set `TENDO_VERSION` to the earlier release, download that release's `compose.yaml`, and [restore the backup you took before updating](backup-restore.md#restore-onto-a-fresh-installation) into a fresh installation. Changes made after that backup are lost.

## Things an update does not change

- **Database passwords.** PostgreSQL stores the role passwords when its data volume is first created. Changing `POSTGRES_PASSWORD` or `TENDO_DATABASE_PASSWORD` in `.env` later does not change them, and the app then cannot connect. Keep the original values.
- **Your data volume.** `docker compose down` keeps it. Never add `--volumes` unless you mean to delete every household record.
- **Settings in `.env`.** Changed values apply when the app container is recreated, which `docker compose up -d` does when its configuration changed.
