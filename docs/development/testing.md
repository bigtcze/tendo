# Development and verification

## Prerequisites

The complete local gate targets Go 1.27.3, a C compiler (`gcc`) for race-enabled tests, Python 3, curl, tar, sha256sum, Node.js 24.21.0 with npm, native PostgreSQL `psql` 18, native `sqlc` 1.31.1, and Playwright 1.64.1 with its matching Chromium browser. Repository-pinned JavaScript tools include oapi-codegen v2.8.0, openapi-typescript 7.13.0 and `@playwright/test` 1.64.1. Node 22.22.2+ remains compatible with package engines; CI and the Docker web build use Node 24.21.0. See [dependency and toolchain inventory](dependencies.md); `toolchain.json` is the canonical pin list.

## Pinned toolchain wrapper

`scripts/dev-exec.sh` runs any command with the exact Go, Node.js/npm, sqlc and Playwright Chromium versions from the checked-out `toolchain.json`. The host still needs Python 3, `gcc`, `make`, `curl`, `tar`, `psql` 18 and Chromium's system libraries (on Debian/Ubuntu: `sudo npx --no-install playwright install-deps chromium` once from `frontend/`). Nothing is installed system-wide and your shell, global profiles and other mise installations are unchanged.

```sh
bash scripts/dev-exec.sh --versions                         # provision, then print versions and executable paths
bash scripts/dev-exec.sh -- bash scripts/verify-local.sh    # complete pre-PR gate with the pins
bash scripts/dev-exec.sh -- bash -c 'cd backend && go test ./...'
```

How it provisions each tool (linux x86_64 only):

| Tool | Source and verification |
|---|---|
| mise (installer only) | Official release archive, SHA-256 from `toolchain.json` checked before extraction. Runs with `MISE_NO_CONFIG=1` and private data/cache/state/config/system directories and no shared install directories, so no `mise.toml`, global config, shims or shared installs are read or changed; an install path outside the private directory is refused. |
| Go, Node.js | `mise install go@X.Y.Z node@X.Y.Z` from the official mirrors with Go checksum and Node signature/checksum verification forced on and mise's npm reshim hook off; the wrapper then checks `go version`/`node --version` report the exact pin. |
| sqlc | Official release archive, SHA-256 from `toolchain.json` checked before extraction, then `sqlc version` checked. |
| oapi-codegen | Not installed: `api/Makefile` runs `go run …@<pin>` with the pinned Go. |
| Playwright Chromium | `playwright-core` installed (scripts disabled, user/global `.npmrc` and `npm_config_*` ignored) and accepted only with the exact version and integrity of `frontend/package-lock.json`; its `browsers.json` decides the Chromium revision. |

The command replaces the wrapper process, so exit status and signals are the command's own. A missing or mismatched tool is an error; the wrapper never falls back to a system binary. Cached mise and sqlc binaries are re-checked against their recorded SHA-256 before each use. `GOTOOLCHAIN=local` stops Go from downloading another toolchain and `GOENV=off` ignores settings saved with `go env -w`; explicit `GO*` variables in your environment still apply. Interrupting provisioning stops the installer before the cache lock is released.

**Cache.** Everything lives under `${XDG_CACHE_HOME:-~/.cache}/tendo/toolchain` (override with an absolute `TENDO_TOOLCHAIN_CACHE`; CI uses the runner temp directory). Versions are kept side by side, so switching branches or testing an upgrade branch reuses existing downloads and works offline once provisioned. Expect roughly 2 GB for one set of pins. Old versions are not pruned automatically; deleting the whole directory is always safe and the next run re-downloads. Long-running agent hosts (for example Paperclip/OpenCode workers) should keep this directory on persistent storage, not tmpfs, and must not add it to the controller's own `PATH`.

The existing PostgreSQL service is not managed by the wrapper; set `TENDO_TEST_POSTGRES_ADMIN_URL` as described below. CI jobs keep provisioning via `actions/setup-go`/`setup-node`; the `backend` job additionally provisions through the wrapper to prove it works from a clean cache.

The complete local entrypoint does not use Docker for its native integration or E2E layers. Without the wrapper, install the same versions from trusted sources and run `npx --no-install playwright install chromium` in `frontend/`.

## Dedicated PostgreSQL service setup

Use a dedicated PostgreSQL 18 test instance, never a production/shared application database. It needs a `postgres` maintenance database and this pre-provisioned shared role:

```sql
CREATE ROLE tendo NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
```

Run the prerequisite SQL once as the administrator of that isolated test instance. The harness validates the role is NOLOGIN, has no elevated attributes and inherits no other roles. It never changes shared-role attributes/password or grants membership to shared `tendo`; it only grants the fixed role to each freshly-created per-run runtime login. Migrations retain existing permissions granted to `tendo`.

Export the service admin URL privately in the shell, replacing host, password and CA path with values for the dedicated service. This example uses a TLS-verifying connection and does not echo the password or URL:

```sh
read -rsp 'Dedicated test-service PostgreSQL admin password: ' PG_TEST_ADMIN_PASSWORD; printf '\n'
PG_TEST_ADMIN_PASSWORD_ENC=$(PG_TEST_ADMIN_PASSWORD="$PG_TEST_ADMIN_PASSWORD" python3 -c 'import os, urllib.parse; print(urllib.parse.quote(os.environ["PG_TEST_ADMIN_PASSWORD"], safe=""))')
export TENDO_TEST_POSTGRES_ADMIN_URL="postgresql://postgres:${PG_TEST_ADMIN_PASSWORD_ENC}@127.0.0.1:5432/postgres?sslmode=verify-full&sslrootcert=/path/to/test-service-ca.pem"
unset PG_TEST_ADMIN_PASSWORD PG_TEST_ADMIN_PASSWORD_ENC
```

For a local disposable PostgreSQL instance only, `sslmode=disable` is acceptable. The URI must select the dedicated server's `postgres` maintenance database; the value is a high-privilege service admin connection, must not target production, must not be stored in repo config, and must never be printed. The test helpers remove the admin URL from child environments. Service setup is operator-specific and deliberately does not create/alter/drop the shared grant role. Per-run migrator/runtime roles, databases and upgrade fixtures are unique; OID/owner-checked cleanup deletes only positively-owned resources. Collisions fail without adoption/deletion. SIGKILL cannot perform cleanup, so uniquely-named resources can remain for later ownership-verified cleanup. Never broad-drop cluster roles/databases or use `DROP OWNED`.

## Targeted checks and complete pre-PR gate

After the dedicated service and pinned tools are available, targeted commands for debugging are:

```sh
(cd backend && go vet ./...)
(cd backend && go test -race $(go list ./... | grep -v -e '/internal/identity/postgres$' -e '/internal/household/postgres$' -e '/internal/subject/postgres$' -e '/internal/item/postgres$'))
bash scripts/setup-integration.sh
bash scripts/check-sqlc.sh
(cd api && npm ci && npm audit --audit-level=moderate && npm run check)
(cd frontend && npm ci && npm audit --audit-level=moderate && npm run check)
python3 -m unittest scripts/test_verify_local.py
python3 -m unittest scripts/test_postgres_test_service.py scripts/test_e2e_native_runner.py scripts/test_verify_local.py scripts/test_ci_workflow.py scripts/test_toolchain_manifest.py scripts/test_dev_toolchain.py scripts/test_quickstart_docs.py
python3 scripts/test_postgres_live.py
python3 -m unittest scripts/test_generation_drift.py
python3 scripts/test_e2e_interrupt.py
bash scripts/e2e-smoke.sh
```

The mandatory complete pre-PR command has no skip switches:

```sh
bash scripts/dev-exec.sh -- bash scripts/verify-local.sh
```

It fails fast and reports stage status/duration, primary failure, and downstream NOT RUN. A local pass never replaces required GitHub checks. Do not request review with a known failing required check or use retries to hide a failure.

## CI job map

The required workflow retains one stable branch-protection aggregate check named `required`. Backend, API, frontend, PostgreSQL, native E2E and eight deployment-smoke jobs run independently. The aggregate uses `if: always()` and passes only if every dependency result is exactly `success`; failure, cancellation and skipped all fail the gate.

| Job | Commands and prerequisites |
|---|---|
| `backend` | Go 1.27.3/setup-go cache; autonomous bootstrap; gofmt, `go vet ./...`, fail-closed package discovery and race unit suite excluding exactly the four PostgreSQL adapters; `go build ./...`; `scripts/test_verify_local.py`, `scripts/test_ci_workflow.py` (workflow structure: mandatory commands, all seven smokes, in-job prerequisite order, aggregate needs/count), and `scripts/test_toolchain_manifest.py` (every pin matches `toolchain.json`), `scripts/test_dev_toolchain.py` (wrapper behavior), `scripts/test_quickstart_docs.py` (shell extraction, section fences, ordered README consistency); real `scripts/dev-exec.sh` provisioning into an empty cache plus a pinned `go build` |
| `api` | Go 1.27.3, Node 24.21.0, API npm cache; `npm ci`, moderate audit, `npm run check` |
| `frontend` | Node 24.21.0/frontend npm cache; `npm ci`, moderate audit, full `npm run check` |
| `postgres` | PostgreSQL 18.6 service, native psql18, official sqlc 1.31.1 release tarball verified against SHA256 before extraction, fixed `tendo` NOLOGIN bootstrap; exact race/P=1 integration suite, sqlc drift; API npm dependencies, frontend npm dependencies and matching Chromium for generator/process harness cases; PostgreSQL/live/process/generator/verifier tests |
| `e2e` | Separate PostgreSQL 18.6 service, native psql18 and `tendo` bootstrap; Go setup, Node 24.21.0, frontend `npm ci`, Playwright 1.64.1 Chromium install, full native production-build E2E |
| `deployment` matrix, `fail-fast: false` | Eight entries: runtime, setup, OIDC, proxy, backup/restore, household backup, V1-to-current upgrade, and quick start. Each checks out and installs Go/Node/API dependencies before running its Docker smoke. The upgrade smoke starts published v1.0.0 with its release image, populates only through the V1 public API, then upgrades the same PostgreSQL volume using the checkout build; it compares table counts and captured response bodies/ETags and exercises V2 APIs. The quick-start smoke extracts and executes the install commands from the documentation. Each matrix job builds its own images; image transfer is YAGNI absent measured evidence for artifact handoff. Proxy and household-backup/upgrade retain their 12/15 minute bounds using per-matrix-step `timeout` values. |
| `required` aggregate | `if: always()`, `needs` every other job, prints results and jq-requires exactly six job successes (the deployment matrix is one job). |

The native browser suite is not a replacement for deployment smokes. Production runtime, setup/API, OIDC API, all reverse proxies, backup/restore, household backup, published V1 data upgrade, and the copy-paste quick start remain mandatory CI jobs. Run the upgrade check locally with `bash scripts/upgrade-smoke.sh`; it uses a unique Compose project and volume, pulls public `ghcr.io/bigtcze/tendo:v1.0.0`, and removes its containers, volume, and private temporary data on exit. To stress browser synchronization under slower reads, set `TENDO_E2E_READ_DELAY_MS` to a positive integer number of milliseconds when running `scripts/e2e-smoke.sh`; the runner delays ordinary household API GETs from the main journey browser context before forwarding them to the server (separately created member and accessibility contexts, and requests a test fulfils or routes itself, are not delayed). In this mode the default per-test timeout rises from 30 to 90 seconds because the slowed reads accumulate; tests that set their own timeout keep it. The option defaults off and does not affect required CI runs.

### Refresh the README screenshot

With the dedicated PostgreSQL service and pinned tools available, run:

```sh
TENDO_README_SCREENSHOT=$PWD/docs/images/home.png bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

The opt-in capture builds and runs the production app against real PostgreSQL, uses the fixed test clock `2026-05-14T08:00:00Z`, and creates realistic household data through the public API. It is separate from the required E2E journeys and is not part of CI.

### UX/scale audit capture

With the dedicated PostgreSQL service, pinned tools, and frontend dependencies available:

```sh
TENDO_UX_AUDIT_DIR=/tmp/opencode/ux-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

This opt-in production-build run captures staged home screenshots and viewport-fold screenshots, plus `metrics.json` (request counts/bytes, render timing, DOM size, rows, overflow, and seeding details). It is deliberately excluded from the default browser suite and is not part of required CI.

## Assertion coverage map

| Layer | Assertions retained |
|---|---|
| Go static/unit | formatting, vet, all unit packages, race detector |
| PostgreSQL | empty/upgrade migrations; privilege and SQLSTATE 42501 restrictions; rollback/concurrency/persistence/auth/session/household/item/OIDC/invitations |
| SQL generation | identity, household, subject, item complete dbgen trees; changed/missing/extra artifacts |
| API | recommended OpenAPI lint; all three Node files/78 checks; health/source checkers; exact 2 TypeScript and 5 Go output drift |
| Frontend | eslint, typecheck, Vitest, production build, npm audit |
| Runtime | production-image UI embedding, migration startup, health/outage recovery, request IDs/problems, non-root/read-only, no published DB, bounded shutdown |
| Setup | real response and DB graph; setup/origin/validation/rate limits; session digest/cookies; household isolation; subject/item CRUD/ETags/recurrence/completion/idempotency/undo/responsible; invitations/contracts |
| OIDC | real link/login, callback/replay/password gate/session replacement, unknown identity unchanged DB, denial/origin/log privacy/contracts |
| Proxy | Caddy/Nginx/Traefik TLS, SNI/Host, trusted forwarding, forged-header rejection, Origin, health/SPA headers, OIDC callback/link/login and `__Host-` cookies through HTTPS, outage recovery, secret-free logs |
| Backup | archive round trip, Unicode/null rows, ownership/FK/CHECK/runtime permissions, existing target/truncation safety |
| Household backup | API-populated restore with fresh credentials; counts/ledger/API bodies/ETags/session/password/idempotency/undo/new writes/invitations/restrictions |
| Quick start | Exact install-guide shell blocks and README command consistency; release-asset install into an empty directory with a locally built image, settings permissions/secrets, Compose startup, HTTP first-owner setup/login/household/UI, then token-disabled restart |
| Browser | 39 serial production journeys; onboarding, CRUD, authorization, OIDC, viewports/keyboard/localization/security, axe WCAG 2.2 A/AA scans, 200 % zoom-equivalent reflow and reduced motion; persisted item across app-only midnight restart |

The E2E runner seeds an unowned foreign household as migrator; browser and API assertions verify cross-household denial, and a post-suite database check confirms the seed is unchanged. Home/create/detail layouts are checked at 360, 768 and 1280px widths.

## Historical CI baseline

Five successful required runs on 2026-10-09: median full run 11m19s (range 10m50s–11m45s), median job 11m17s. Median stage times: Go vet 25s; unit race 60s; PostgreSQL integration 93s; sqlc 2s; API install/audit/contract/generation 162s; frontend install/check 39s; production runtime 52s; setup 20s; OIDC 25s; proxy variants 81s; backup safety 9s; household backup 19s; browser E2E 78s. Detailed GitHub logs returned HTTP 403, so per-command/build/cache subtimings are unknown. Five runs describe the recent release family, not a long-term estimate.

Last serial run after the native-tool change (main, 2026-10-09): 11m09s.

**Parallel workflow (first successful PR run, 2026-10-09):** 3m58s wall clock from run creation to completion (jobs 3m54s from first start to last finish). Job durations: `postgres` 229s (critical path), `proxy-smoke` 148s, `e2e` 141s, `oidc-smoke` 101s, `backend` 98s, `api` 98s, `setup-smoke` 84s, `household-backup-smoke` 81s, `runtime-smoke` 76s, `backup-restore-smoke` 71s, `frontend` 52s, `required` aggregate 2s. A single sample: queueing and runner availability vary, so treat it as indicative rather than a guarantee. Total billed runner minutes increase because each job repeats checkout and tool setup.
