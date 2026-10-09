# Development runtime

The Compose stack is for development and CI only. It runs:

- PostgreSQL and health endpoints
- first-owner setup, protected by an operator-generated setup code, used by the browser onboarding screen
- local login sessions (`POST`/`GET`/`DELETE /api/v1/session`)
- optional OIDC account linking and sign-in: API plus the sign-in button and **Your account** screen
- a session-authenticated household read (`GET /api/v1/households/{householdId}`, members only; malformed, nonexistent, and non-member IDs all return the same 404)
- an embedded web UI served by the app: first-run onboarding, a sign-in page, and an empty home screen (see [frontend development](frontend.md))

It is not a usable Tendo household application because item APIs and item screens are not implemented. PostgreSQL is private to the Compose network and the app port binds to host loopback.

## Prerequisites

- Docker Engine with the Compose plugin
- Go 1.26.8, Node.js 22.22.2+ (or 24+) with npm, Make, Python 3, curl, and OpenSSL for checks
- An unused local TCP port (default `8080`)

## Start and verify

```sh
cp .env.example .env
openssl rand -hex 32
openssl rand -hex 32
```

Set the two generated values independently in `.env`, then start the stack. Migrations run as a mandatory one-shot service before the app:

```sh
docker compose up -d --build --wait
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

Both health endpoints return JSON `{"status":"ok"}` with `Cache-Control: no-store` and `X-Request-ID`. Readiness returns HTTP 503 with an RFC 9457 `application/problem+json` response if PostgreSQL cannot be pinged or shutdown is draining; it contains no internal error details. Liveness checks only the HTTP process. Compose starts the one-shot migration before the app; stop with `docker compose down`. Add `--volumes` only when you intend to delete the development database.

For settings and their constraints, see [runtime configuration](../admin/configuration.md). `TENDO_PUBLIC_URL` must match the selected host port; the runtime smoke sets it to `http://127.0.0.1:<dynamic-port>`. See [reverse proxy deployment](../admin/reverse-proxy.md) for the separately tested proxy boundary, which also covers the HTTPS session cookie behavior. Set `TENDO_HOST_PORT` in `.env` to an available port if 8080 is occupied. For backup and restore steps, see [back up and restore](../admin/backup-restore.md). From the repository root, `bash scripts/household-backup-smoke.sh` runs a full household backup and restore into a fresh installation, and `bash scripts/backup-restore-smoke.sh` checks that restore refuses an existing target and rejects a truncated archive.

## Contract and checks

The setup, session, household read, OIDC, and health contract is `api/openapi.yaml` (OpenAPI 3.1). From `api/`, install pinned dependencies with `npm ci`, then run `npm run check`; this validates OpenAPI, contract examples and checker regressions, and drift for both generated Go outputs and TypeScript definitions. The same source schemas generate the setup models in identity/httpapi and household response models in household/httpapi, and health models in platform/httpx; the frontend API client is typed from the generated `api/generated/api.d.ts`. oapi-codegen v2.4.1 warns that OpenAPI 3.1 support is incomplete, so keep schemas simple and validate output coverage against the source contract.

From the repository root, run `bash scripts/runtime-smoke.sh` to build and exercise an isolated Compose project with generated credentials and a temporary host port. For reverse-proxy boundary verification, run `bash scripts/proxy-smoke.sh`; it executes Caddy, Nginx, and Traefik against the shared checked-in configs using a temporary local CA, real PostgreSQL, and no published database port. Set `PROXY=caddy`, `PROXY=nginx`, or `PROXY=traefik` to run only one configuration while debugging. It checks mismatched TLS SNI/Host handling, malformed forwarded-chain rejection, schema-valid live and unavailable health responses, media types, request IDs, cache headers, readiness failure/recovery when PostgreSQL stops, and query-secret absence from all three proxy logs during normal requests and upstream transport failures. The separate runtime smoke verifies container UID/read-only root, no published PostgreSQL port, and clean bounded SIGTERM shutdown. Compose readiness waits have explicit deadlines, and required CI bounds the overall job. Cleanup removes only the uniquely named project, its volume, and a private temporary directory.

Run `gofmt`, `go vet ./...`, and `go build ./...` from `backend/`. The unit-only race suite is `go test -race $(go list ./... | grep -v -e '/internal/identity/postgres$' -e '/internal/household/postgres$')`; the PostgreSQL-dependent integration suite is `bash scripts/setup-integration.sh` from the repository root, which provisions disposable PostgreSQL and runs the identity, household, and database packages with `-race` (one package at a time, because they share fixture tables). The first-owner, session, household read, persistence, and response fixtures are exercised against a disposable Compose stack by `bash scripts/setup-smoke.sh`. `bash scripts/oidc-smoke.sh` exercises configured OIDC against a test-only provider and real PostgreSQL, including link/login, replay, denial, and unknown-identity admission invariants. This provider proves protocol behavior only; it is not a live Pocket ID compatibility test.

Frontend checks (`npm run check` in `frontend/`) and the browser end-to-end smoke (`bash scripts/e2e-smoke.sh`) are described in [frontend development](frontend.md).
