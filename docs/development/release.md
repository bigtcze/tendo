# Release process

`main` is always releasable. A release is a `vX.Y.Z` tag on a commit of `main` that passed the `required` workflow. Self-hosters install from the release: the app image `ghcr.io/bigtcze/tendo` plus the `compose.yaml` and `example.env` attached to the GitHub release.

## How a change reaches `main`

1. Work on a focused branch; `main` is protected and accepts changes only through pull requests.
2. Run the complete local gate: `bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (see [testing](testing.md)).
3. Open a pull request. The `required` GitHub Actions workflow (`.github/workflows/required.yml`) must pass, and the branch must be current with `main`, before merge. It covers backend unit, race, and real-PostgreSQL integration tests; sqlc and API generation drift; OpenAPI validation; frontend checks; and production-image smokes for runtime, setup, OIDC, all three reverse proxies, backup and restore, the documented quick start, and the Playwright browser journeys.
4. After merge, the same workflow runs on `main`. A red `main` is fixed before other work.

The deployment smokes build the app from the checkout with `deploy/compose.build.yaml` layered over `compose.yaml`. Use the same override to run a local build: `docker compose -f compose.yaml -f deploy/compose.build.yaml up -d --build --wait`.

## Cut a release

1. Choose the version with [semantic versioning](https://semver.org/). A change that needs manual action from self-hosters, or breaks `/api/v1` clients, is a new major version.
2. In a pull request, add the release notes as `docs/releases/vX.Y.Z.md`. Write them for the people who run Tendo: what changed for them, anything they must do when updating, and known limitations. Do not paste the commit list.
3. After that pull request is merged and `main` is green, tag the merge commit and push the tag:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a vX.Y.Z -m "Tendo vX.Y.Z"
   git push origin vX.Y.Z
   ```

4. The `Release` workflow (`.github/workflows/release.yml`) then:
   - refuses the tag unless its commit is on `main`, passed the `required` check, and has release notes;
   - builds the image for `linux/amd64` and `linux/arm64` and pushes it as `vX.Y.Z`, `vX.Y`, `vX`, and `latest`;
   - creates the GitHub release with the notes and attaches `compose.yaml` and `example.env` (`.env.example` under a name GitHub keeps), staged by `scripts/release-assets.sh`.

5. Check the result: `docker pull ghcr.io/bigtcze/tendo:vX.Y.Z` works without signing in, and the release page lists both files.

The first push creates the `tendo` package on GitHub Container Registry as private. Once, in the package settings, change its visibility to public and link it to this repository.

Tags are never moved or reused. A fix is a new patch release.

## Compatibility rules for every change

- Database migrations are forward-only and must upgrade an existing database without data loss. Each new migration ships with an upgrade test from the previous schema version.
- The app refuses to start on a database that is older, newer, or partially migrated (see [troubleshooting](../admin/troubleshooting.md#the-database-is-not-ready)).
- Configuration changes update `.env.example`, `compose.yaml`, and the [configuration reference](../admin/configuration.md) in the same pull request. New settings default to safe behavior when unset.
- `compose.yaml` is a release asset that self-hosters run unchanged. It must not reference files from the repository; the PostgreSQL role setup is inlined for that reason.
- Public API changes follow `api/openapi.yaml` and the [API conventions](api.md).
- Behavior changes update the user or admin guide that describes them.
