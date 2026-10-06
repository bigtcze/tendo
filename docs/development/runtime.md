# Development runtime

The Compose stack is for development and CI only. It runs PostgreSQL, health endpoints, and an operator-authorized first-owner setup API; it is not a usable Tendo household application because login, household/item APIs, and UI are not implemented. PostgreSQL is private to the Compose network and the app port binds to host loopback.

## Prerequisites

- Docker Engine with the Compose plugin
- Go 1.26.8, Node.js 22+ with npm, Make, Python 3, curl, and OpenSSL for checks
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

For settings and their constraints, see [runtime configuration](../admin/configuration.md). `TENDO_PUBLIC_URL` must match the selected host port; the runtime smoke sets it to `http://127.0.0.1:<dynamic-port>`. See [reverse proxy deployment](../admin/reverse-proxy.md) for the separately tested proxy boundary, not as a claim that this setup-only runtime has login/session behavior. Set `TENDO_HOST_PORT` in `.env` to an available port if 8080 is occupied. For the bounded PostgreSQL logical backup/restore evidence and safe operational guidance, see [backup and restore](../admin/backup-restore.md); run `bash scripts/backup-restore-smoke.sh` from the repository root.

## Contract and checks

The setup and health contract is `api/openapi.yaml` (OpenAPI 3.1). From `api/`, install pinned dependencies with `npm ci`, then run `npm run check`; this validates OpenAPI, contract examples and checker regressions, and drift for both generated Go outputs and TypeScript definitions. The same source schemas generate the setup models in identity/httpapi and health models in platform/httpx; generated TS definitions remain outside the frontend runtime until a real API client exists. oapi-codegen v2.4.1 warns that OpenAPI 3.1 support is incomplete, so keep schemas simple and validate output coverage against the source contract.

From the repository root, run `bash scripts/runtime-smoke.sh` to build and exercise an isolated Compose project with generated credentials and a temporary host port. For reverse-proxy boundary verification, run `bash scripts/proxy-smoke.sh`; it executes Caddy, Nginx, and Traefik against the shared checked-in configs using a temporary local CA, real PostgreSQL, and no published database port. Set `PROXY=caddy`, `PROXY=nginx`, or `PROXY=traefik` to run only one configuration while debugging. It checks mismatched TLS SNI/Host handling, malformed forwarded-chain rejection, schema-valid live and unavailable health responses, media types, request IDs, cache headers, readiness failure/recovery when PostgreSQL stops, and query-secret absence from all three proxy logs during normal requests and upstream transport failures. The separate runtime smoke verifies container UID/read-only root, no published PostgreSQL port, and clean bounded SIGTERM shutdown. Compose readiness waits have explicit deadlines, and required CI bounds the overall job. Cleanup removes only the uniquely named project, its volume, and a private temporary directory.

Run `gofmt`, `go vet ./...`, and `go build ./...` from `backend/`. The unit-only race suite is `go test -race $(go list ./... | grep -v '/internal/identity/postgres$')`; the PostgreSQL-dependent integration suite is `bash scripts/setup-integration.sh` from the repository root, which provisions disposable PostgreSQL and runs the database packages with `-race`. The first-owner API, persistence, and response fixtures are exercised against a disposable Compose stack by `bash scripts/setup-smoke.sh`.
