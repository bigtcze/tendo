# Release process

Tendo has no tagged releases, published container images, or changelog yet. `main` is the release line: self-hosters build the image from a checkout with `docker compose up -d --build` and update with `git pull --ff-only` (see [update](../admin/update.md)).

## How a change reaches `main`

1. Work on a focused branch; `main` is protected and accepts changes only through pull requests.
2. Run the complete local gate: `bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (see [testing](testing.md)).
3. Open a pull request. The `required` GitHub Actions workflow (`.github/workflows/required.yml`) must pass, and the branch must be current with `main`, before merge. It covers backend unit, race, and real-PostgreSQL integration tests; sqlc and API generation drift; OpenAPI validation; frontend checks; and production-image smokes for runtime, setup, OIDC, all three reverse proxies, backup and restore, and the Playwright browser journeys.
4. After merge, the same workflow runs on `main`. A red `main` is fixed before other work.

## Compatibility rules for every change

- Database migrations are forward-only and must upgrade an existing database without data loss. Each new migration ships with an upgrade test from the previous schema version.
- The app refuses to start on a database that is older, newer, or partially migrated (see [troubleshooting](../admin/troubleshooting.md#the-database-is-not-ready)).
- Configuration changes update `.env.example`, `compose.yaml`, and the [configuration reference](../admin/configuration.md) in the same pull request. New settings default to safe behavior when unset.
- Public API changes follow `api/openapi.yaml` and the [API conventions](api.md).
- Behavior changes update the user or admin guide that describes them.
