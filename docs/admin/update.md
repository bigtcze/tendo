# Update Tendo

Tendo has no tagged releases yet. An installation follows the `main` branch, and every change reaches `main` only after the full required test suite passes. See [release process](../development/release.md).

Run every command from the directory that holds Tendo's `compose.yaml` and `.env`.

## Update

1. [Back up](backup-restore.md#back-up) first. An update can change the database, and the backup is the only way back.

2. Get the new version:

   ```sh
   git pull --ff-only
   ```

   If Git refuses because you changed tracked files, keep your changes in `.env` or in a separate Compose override file instead, then pull again.

3. Rebuild and restart:

   ```sh
   docker compose up -d --build --wait
   ```

   Compose rebuilds the image, runs the one-shot `migrate` container, and starts the app only after migration succeeds. Migrations keep your data. The app is unavailable for a few seconds while it restarts.

4. Check that Tendo is ready and sign in:

   ```sh
   curl --fail --show-error http://localhost:8080/health/ready
   ```

   If you changed `TENDO_HOST_PORT`, replace `8080` in this command with that value. This checks the app's local HTTP port, not the reverse proxy's public HTTPS port. If the app is still starting, wait a few seconds and run it again.

5. Compare `.env.example` with your `.env` and add any new settings you want. The [configuration reference](configuration.md) describes each one and its default.

## Go back to an earlier version

An older Tendo cannot use a database whose schema version is newer than it supports. If an update advanced the schema, the older app stops with `database schema validation failed`, and its migration stops with `database schema is newer than this application`.

To go back, check out the earlier version and [restore the backup you took before updating](backup-restore.md#restore-onto-a-fresh-installation) into a fresh installation. Changes made after that backup are lost.

## Things an update does not change

- **Database passwords.** PostgreSQL stores the role passwords when its data volume is first created. Changing `POSTGRES_PASSWORD` or `TENDO_DATABASE_PASSWORD` in `.env` later does not change them, and the app then cannot connect. Keep the original values.
- **Your data volume.** `docker compose down` keeps it. Never add `--volumes` unless you mean to delete every household record.
- **Settings in `.env`.** Changed values apply when the app container is recreated, which `docker compose up -d` does when its configuration changed.
