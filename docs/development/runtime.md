# Development runtime

The Compose stack is for development and CI only. It runs a health-only Go HTTP process and PostgreSQL; it is not a usable Tendo household application and must not be exposed publicly. PostgreSQL is private to the Compose network and the app port binds to host loopback.

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

Set the two generated values independently in `.env`, then:

```sh
docker compose up -d --build --wait
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

Both endpoints return JSON `{"status":"ok"}` with `Cache-Control: no-store` and `X-Request-ID`. Readiness returns HTTP 503 with an RFC 9457 `application/problem+json` response (`type: about:blank`, `title: Service Unavailable`, `status: 503`) if PostgreSQL cannot be pinged within the configured timeout or shutdown is draining; it contains no internal error details. Liveness checks only that the HTTP process responds. Stop with `docker compose down`; add `--volumes` only when you intend to delete this development database.

For settings and their constraints, see [runtime configuration](../admin/configuration.md). Set `TENDO_HOST_PORT` in `.env` to an available port if 8080 is occupied.

## Contract and checks

The health contract is `api/openapi.yaml` (OpenAPI 3.1). From `api/`, install pinned development dependencies with `npm ci`, then run `npm run check`; this validates examples, response-checker regressions, and generated Go/TypeScript drift. Redocly CLI v2.58.1 validates the source contract with telemetry disabled. The Go generator, oapi-codegen v2.4.1, does not fully support OpenAPI 3.1; this slice uses simple object models and independently validates live responses against the source schemas. Reassess generator support before introducing more complex schema constructs. The generated Go models are consumed by `httpx`; generated TS definitions are kept outside the frontend runtime until a real API client exists.

From the repository root, run `bash scripts/runtime-smoke.sh` to build and exercise an isolated Compose project with generated credentials and a temporary host port. It checks schema-valid live and unavailable health responses, media types, request IDs, cache headers, readiness failure/recovery when PostgreSQL stops, container UID/read-only root, no published PostgreSQL port, and clean bounded SIGTERM shutdown. Compose readiness waits have explicit deadlines, and required CI bounds the overall job. Cleanup removes only the uniquely named project, its volume, and a private temporary directory.

Go checks run from `backend/`: `gofmt`, `go vet ./...`, `go test -race ./...`, and `go build ./...`. The required CI job retains those checks and runs contract validation plus the production-container runtime smoke.
