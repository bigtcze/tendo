# Development and verification

## Prerequisites

The complete local gate targets Go 1.26.8, a C compiler (`gcc`) for race-enabled tests, Python 3, curl, tar, sha256sum, Node.js 22.22.3 (as in CI and the production image) or 24.x with npm, native PostgreSQL `psql` 18, native `sqlc` 1.30.0, and Playwright 1.63.0 with its matching Chromium browser. Repository-pinned JavaScript tools include oapi-codegen v2.4.1, openapi-typescript 7.6.1 and `@playwright/test` 1.63.0. Both Node versions satisfy the package engine ranges.

Install the local Playwright browser before running the complete gate:

```sh
cd frontend
npm ci
npx --no-install playwright install chromium
cd ..
```

Install Go/Node/native PostgreSQL/sqlc tools using your platform's trusted package sources. The gate does not use Docker for its native integration or E2E layers. CI installs the pinned sqlc release archive only after SHA-256 verification.

## Isolated PostgreSQL service prerequisite

Use a dedicated PostgreSQL 18 test instance, never a production/shared application database. It needs a maintenance database named `postgres` and a pre-provisioned shared runtime grant role:

```sql
CREATE ROLE tendo NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
```

Provision that prerequisite once as the dedicated service administrator. The test harness validates that `tendo` is NOLOGIN, has no elevated role attributes and inherits no other roles; it never creates, alters, grants to, changes the password of, or drops this shared role. Per-run login roles/databases and upgrade fixtures are unique. Runtime test connections are random login users inheriting `tendo`; migrations preserve the existing `GRANT ... TO tendo` permissions.

Export the service admin URL privately in the shell (replace host/password with your dedicated service values; do not put real credentials in repository files or print the variable):

```sh
read -rsp 'Dedicated PostgreSQL 18 test admin password: ' PG_TEST_ADMIN_PASSWORD; printf '\n'
PG_TEST_ADMIN_PASSWORD_ENC=$(PG_TEST_ADMIN_PASSWORD="$PG_TEST_ADMIN_PASSWORD" python3 -c 'import os, urllib.parse; print(urllib.parse.quote(os.environ["PG_TEST_ADMIN_PASSWORD"], safe=""))')
export TENDO_TEST_POSTGRES_ADMIN_URL="postgresql://postgres:${PG_TEST_ADMIN_PASSWORD_ENC}@127.0.0.1:5432/postgres?sslmode=verify-full&sslrootcert=/path/to/test-service-ca.pem"
unset PG_TEST_ADMIN_PASSWORD PG_TEST_ADMIN_PASSWORD_ENC
```

The password is percent-encoded so URI-reserved characters (`@`, `/`, `?`, `#`, `%`) remain valid. This section lists the prerequisites of an existing dedicated service; it is not a recipe for starting or stopping one.

Use the service's actual CA path and TLS settings; for a strictly local disposable service, `sslmode=disable` is acceptable. The URL must connect to that service's `postgres` maintenance database. It is a high-privilege bootstrap credential: never print it, pass it to child applications, or point it at production. Setup scripts fail closed if the URL or service prerequisite is unavailable. A reused test cluster may contain colliding names; collisions fail without adopting/deleting those resources. SIGKILL cannot run cleanup; uniquely named resources may remain and must only be cleaned after verifying their ownership/OIDs. Do not broad-drop roles/databases or use `DROP OWNED`.

## Targeted checks and complete gate

After setting up the dedicated test service and tools, useful targeted checks include:

```sh
(cd backend && go vet ./...)
(cd backend && go test -race $(go list ./... | grep -v -e '/internal/identity/postgres$' -e '/internal/household/postgres$' -e '/internal/subject/postgres$' -e '/internal/item/postgres$'))
bash scripts/setup-integration.sh
bash scripts/check-sqlc.sh
(cd api && npm ci && npm audit --audit-level=moderate && npm run check)
(cd frontend && npm ci && npm audit --audit-level=moderate && npm run check)
python3 -m unittest scripts/test_postgres_test_service.py scripts/test_e2e_native_runner.py scripts/test_verify_local.py
python3 scripts/test_postgres_live.py
python3 -m unittest scripts/test_generation_drift.py
python3 scripts/test_e2e_interrupt.py
bash scripts/e2e-smoke.sh
```

The required pre-PR command is the complete, mandatory sequence:

```sh
bash scripts/verify-local.sh
```

There are no skip flags. It fails fast and reports stage status/duration and downstream NOT RUN. A local pass does not replace CI; do not open/update a PR with a known failing required check. The complete verifier includes bootstrap/controller syntax only, Go formatting/vet/race unit/build, real PostgreSQL integration, sqlc drift, API install/audit/check, frontend install/audit/check, native deterministic/live/generator/interruption harness tests and full native production E2E.

## CI stage map

The required workflow remains one serial `required` job and keeps every deployment smoke. Its order is:

1. checkout and Go 1.26.8 setup;
2. native psql 18 + sqlc 1.30.0 verified release install;
3. one-time `tendo` NOLOGIN bootstrap on the PostgreSQL 18.6 service container (started before the steps);
4. autonomous bootstrap, Go format/vet/race unit tests;
5. PostgreSQL integration, sqlc drift, Go build;
6. Node 22.22.3 setup, API npm-ci/audit/check, frontend npm-ci/audit/check;
7. pinned Chromium installation, then PostgreSQL/process/generator/verifier harness tests;
8. unchanged production runtime, setup, OIDC, proxy, backup/restore, and full-household-backup Docker smokes;
9. native production-binary browser E2E.

Retained E2E artifacts (`frontend/test-results/`, Playwright traces, screenshots and logs) contain disposable fixture credentials, setup/session/invitation tokens and OIDC callback material. They are not redacted: treat them as sensitive, and sanitize them before sharing or uploading.

The local native E2E is not the runtime/setup/OIDC/proxy/backup deployment suite. Those Docker-based deployment checks remain mandatory in CI and targeted local debugging.

## Coverage and baseline

| Layer | Protected assertions |
|---|---|
| Go static/unit | formatting, vet, all unit packages, race detector |
| PostgreSQL | empty/upgrade migrations; privilege and SQLSTATE 42501 restrictions; rollback/concurrency/persistence/auth/session/household/item/OIDC/invitations |
| SQL generation | identity, household, subject and item complete dbgen trees; changed/missing/extra artifacts |
| API | recommended OpenAPI lint; all 3 Node checker files/78 checks; health/source checks; exact 2 TS and 5 Go outputs |
| Frontend | eslint, typecheck, Vitest, production build, npm audit |
| Runtime | production-image UI embedding, migration startup, health/outage recovery, request IDs/problems, non-root/read-only, no published DB, bounded shutdown |
| Setup | real response/DB graph, setup/origin/validation/rate limits, session digest/cookies, household isolation, CRUD/ETags/recurrence/completion/idempotency/undo/responsible, invitations/contracts |
| OIDC | real provider link/login, callback/replay/password gate/session replacement, unknown identity unchanged DB, denial/origin/log privacy/contracts |
| Proxy | Caddy/Nginx/Traefik TLS, SNI/Host, trusted forwarding, forgery rejection, Origin, health/SPA headers, cookies, outage recovery, secret-free logs |
| Backup | archive round trip, Unicode/null rows, ownership/FK/CHECK/runtime permissions, existing-target/truncation safety |
| Household backup | API-populated restore with fresh credentials, counts/ledger/API bodies/ETags/session/password/idempotency/undo/new writes/invitations/restrictions |
| Browser | 27 serial production journeys; onboarding, CRUD, authorization, OIDC, viewports/keyboard/localization/security; persisted item across app-only midnight restart |

Baseline evidence: five successful required runs on 2026-10-09; median total workflow duration 11m19s (range 10m50s–11m45s), median job duration 11m17s. Median stage timings: Go vet 25s; unit race 60s; PostgreSQL integration 93s; sqlc 2s; API install/audit/contract/generation 162s; frontend install/check 39s; runtime 52s; setup 20s; OIDC 25s; proxy variants 81s; backup safety 9s; household backup 19s; browser 78s. Detailed GitHub job log downloads returned HTTP 403, so per-command sub-timing/cache-hit analysis was unavailable; five samples describe the recent release family, not a long-term performance guarantee.
