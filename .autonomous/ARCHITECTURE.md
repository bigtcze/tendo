# Architecture

status=canonical_version_independent
source_of_truth=this_file
recurrence_rules=RECURRENCE.md
security_rules=SECURITY.md
api_rules=API.md

## Stack and style
style=modular_monolith
backend=Go
api=REST_contract_first (API.md)
api_version=v1
db=PostgreSQL
query_layer=sqlc
router=chi
frontend=React+TypeScript+Vite
ui=Tailwind+shadcn_ui_or_equivalent_consumer_starter, extended not replaced
production_frontend=embedded_into_Go_binary
deployment=Docker_Compose
runtime_containers=app+postgres (exactly two required services)
transport_to_app=HTTP
tls_termination=reverse_proxy
telemetry_default=none
mcp_transport=Streamable_HTTP, hosted inside app (or a deliberately justified component; see SECURITY.md "MCP")

## Durable architecture rules
- Modular monolith until measured evidence justifies splitting a service.
- Business and domain behavior must not depend on HTTP, React, PostgreSQL driver details, OIDC provider, MCP transport or future integrations.
- Backend application services are the single write boundary for core domain behavior. UI, API and MCP call the SAME services. No parallel recurrence engine and no frontend date arithmetic.
- Application authorization consumes a transport-neutral Principal (identity + household memberships/roles + instance capabilities). Local session, OIDC session and MCP OAuth are adapters that create it.
- Frontend and integrations use API/application contracts; they never duplicate domain rules.
- Future connectors do not write core tables directly.
- Prefer explicit dependency injection over service locators or global state.
- No cyclic package dependencies. No generic `utils` dumping ground.
- No speculative plugin frameworks, brokers, Redis or microservices. Do not create empty tables/services for future features (ROADMAP "Platform constraint").

## Backend module boundaries
backend/cmd/tendo/ - process wiring, config, startup only
backend/internal/platform/ - config, database, httpx, logging, clock, ids, security primitives; no household business rules
backend/internal/identity/ - local/OIDC identities, sessions, invitation and authentication orchestration, MCP OAuth
backend/internal/household/ - households, memberships, roles, authorization policies, instance capabilities
backend/internal/subject/ - person/home/vehicle/pet/custom subjects
backend/internal/item/ - backlog items, lifecycle commands, completion/history, item application services
backend/internal/schedule/ - attention scheduling and recurrence; pure deterministic date logic; all month/year arithmetic is centralized here (RECURRENCE.md)
backend/internal/integration/ - reserved for real post-V1 connectors only; do not scaffold fake integrations

Per-module adapters:
- Each business module owns its repository contract.
- PostgreSQL implementation lives under that module, e.g. `internal/item/postgres/`.
- HTTP adapter lives under that module or a thin routed adapter, e.g. `internal/item/httpapi/`.
- platform/database owns only shared pool, migration and transaction primitives.
- sqlc queries are grouped by the business module they serve.

Rules:
- Modules expose narrow application/service interfaces.
- A parent business package must not import its adapter subpackages; composition happens in `cmd/tendo`.
- Cross-module calls use application/service interfaces or stable domain values, never another module's SQL tables.
- HTTP handlers and MCP tools are adapters; they contain no business rules.

## Domain events / future platform seam
- Meaningful successful commands may emit typed in-process domain events (e.g. ItemCreated, ItemCompleted, ItemStateChanged).
- Payloads use stable IDs and domain facts, never DB rows.
- Consumers may be synchronous and in-process. No broker or outbox until a real async integration requires it; preserve the boundary so an outbox can be added later.

## Identity and data scoping
- Public resource IDs are opaque UUIDv7 (or another documented sortable opaque UUID). Never expose sequential DB IDs.
- Every household-owned aggregate is explicitly household-scoped in schema and authorization.
- UserAccount <-> Household is a membership relation with a role per membership. Schema, API and authorization support many households.
- UserAccount may store `default_household_id` as a selection hint; it is not an access control source.
- Subject-person linkage to an account is optional.
- Persons who are existing household members are first-class people: the member IS the person. No second manual identity; no auto-merge by name (see PRODUCT "Identity and households").

## Time
- Audit timestamps are UTC RFC3339.
- Household timezone is an IANA zone. Business dates are date-only in that zone.
- Inject a Clock interface into business logic and tests. No scattered direct `time.Now()` in domain code.
- Centralize month/year arithmetic in the schedule module (RECURRENCE.md).

## Persistence and concurrency
- PostgreSQL is the authoritative state store.
- Persist recurrence policy separately from the current-cycle schedule state.
- Persist the current cycle's attention anchor; do not recompute it opportunistically on every read.
- Completion history stores enough cycle and policy facts to explain history and to undo or correct the latest completion safely.
- A policy edit never mutates historical completion facts.
- Completion, history and next-cycle state changes are ONE DB transaction (RECURRENCE.md "Completion, idempotency, undo").
- Optimistic concurrency uses resource versions/ETags at the API boundary (API.md "Concurrency").
- Accidental or retried completion requests are idempotent and cannot advance two cycles.
- Concurrent edits fail explicitly; updates are never silently lost.
- Migrations are ordered, reproducible and tested from an empty DB and from upgrade fixtures. No destructive schema change without an explicit migration and data-preservation path.
- Migration paths include: V1-V4 populated data, household membership backfill, V4 OIDC email provenance, invitation purge (SECURITY.md "Migration from v4").
- Stable public IDs, OIDC issuer/sub bindings and history survive migrations.

## Deployment and startup
- Compose services are exactly two: `postgres` and `app`. Root `configs:` blocks and separate `migrate` or `postgres-init` containers are NOT part of the canonical install.
- PostgreSQL data uses a named volume that is never mutated by migration logic.
- Readiness gating: the app starts serving HTTP only after the following succeed, in order:
  1. privileged bootstrap connection creates or verifies the restricted `tendo` runtime role and grants;
  2. embedded schema migrations run under the existing migration lock, version and dirty checks;
  3. the privileged connection is closed;
  4. only then the HTTP listener opens and `/health/ready` reports ready.
- Migration failure, a dirty schema or a newer-than-binary schema fails closed. The app must never report ready on a broken schema.
- Repeated and concurrent startups are safe through the migration lock.
- Ordinary serving uses the restricted runtime role. Never a superuser; never the privileged credential.

### App startup privilege tradeoff (disclosed; see also "Deployment and startup")
- The privileged bootstrap connection is short-lived, but the privileged DB credential remains in the long-lived app container environment, because the app itself performs bootstrap and migrations.
- This is an accepted REDUCTION in isolation versus the former one-shot `migrate`/`postgres-init` containers, which were separate from the serving process. It is NOT equal isolation to the V3 install. Docs and release notes must say so; they must never present the two-service deployment as equally isolated.
- Reviewed in isolation: the privileged connection closes before serving; the restricted runtime role never receives extra privileges; no new sidecar, bootstrap job, manual SQL step or extra required secret is introduced.

## Frontend
- The SPA communicates only through the versioned REST API.
- API types and client are generated or mechanically derived from OpenAPI.
- React contains presentation and client-state logic only. It never contains recurrence, authorization or date-policy rules.
- One responsive SPA serves phone, tablet and desktop. No native-mobile architecture.
- Feature-oriented frontend structure. Shared components are generic presentation primitives only.
- Localization from day one; Czech and English strings live in locale resources (UX.md "Language").

## Reverse proxy / networking
- Tendo serves plain HTTP only. No certificate issuance, ACME, TLS key handling or HTTPS listener in app.
- Production topology: client HTTPS -> reverse proxy -> Tendo HTTP.
- `TENDO_PUBLIC_URL` is the canonical external origin for links, OIDC callbacks and origin policy. HTTP is allowed only for trusted local development; HTTPS is expected for internet-facing deployments.
- Trust `Forwarded` and `X-Forwarded-*` only from explicitly configured trusted proxy CIDRs or hops. Never trust forwarded client IP or proto from direct clients.
- Secure-cookie behavior follows the canonical public URL and proxy-aware policy, not the raw socket.
- Reverse proxy passes original host, proto and client information.
- CORS is disabled by default for the same-origin app. Future external clients get explicit allow-lists.
- Deployment assumes URL-root hosting. Hostname or subdomain reverse proxies are supported. Subpath hosting is not required.
- Health endpoints are proxy and container friendly and expose no private data.

### Secure host IP (V5)
- Compose publishes the app port as `${TENDO_HOST_IP:-127.0.0.1}:${TENDO_HOST_PORT:-8080}:8080`.
- Default is localhost only. Network-wide bind is an explicit documented opt-in (`TENDO_HOST_IP=0.0.0.0` or a specific interface) in `.env.example`, installation docs and tests.
- Upgrading must never expose a previously local app to the network by accident.
- No hardcoded published bind address in any published Compose file.
- Verify proxy behaviour, public origin and OIDC callback with a non-default host IP.

## Operations
- Config through documented environment variables with safe defaults and startup validation.
- Structured logs with request/correlation ID. No secrets or private content in logs.
- `/health/live` and `/health/ready`.
- Graceful shutdown.
- Container runs non-root where practical with explicit writable paths.
- Standard PostgreSQL backup and restore; no proprietary backup format (DOCUMENTATION).
- No outbound network requirement for core runtime except optional configured OIDC.
- Favicon, browser title and app icons come from Tendo's own brand assets, correct in all themes and languages (UX.md).

## Repository layout
backend/
frontend/
api/openapi.yaml, api/paths/, api/components/
deploy/
docs/ (user/, admin/, development/, adr/, design/, releases/)
.autonomous/
scripts/

## ADR policy
Short ADRs only for decisions that would otherwise be re-argued:
- schema and identity choices
- auth/session model
- API conventions
- UI starter or template
- unusual dependency
No ADRs for routine implementation details.
