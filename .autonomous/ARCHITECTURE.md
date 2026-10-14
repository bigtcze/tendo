# Architecture

style=modular_monolith
backend=Go
api=REST_contract_first
api_version=v1
api_contract=OpenAPI
frontend=React+TypeScript+Vite
ui=Tailwind+shadcn_ui_or_equivalent_consumer_starter
db=PostgreSQL
query_layer=sqlc
router=chi
deployment=Docker_Compose
production_frontend=embedded_into_Go_binary
runtime_containers=app+postgres
transport_to_app=HTTP
tls_termination=reverse_proxy
telemetry_default=none

## Durable architecture rules
- Modular monolith until measured evidence justifies splitting a service.
- Business/domain behavior must not depend on HTTP, React, PostgreSQL driver details, OIDC provider, or future integrations.
- Backend application services are the single write boundary for core domain behavior.
- Application authorization consumes a transport-neutral Principal/Actor (identity + household membership/role/capabilities); local session/OIDC are adapters that create it. Future API/service tokens must reuse the same boundary.
- Frontend and future integrations use API/application contracts; never duplicate domain rules.
- Future connectors do not write core tables directly.
- Prefer explicit dependency injection/construction over service locators/global state.
- No cyclic package dependencies.
- No generic `utils` dumping ground; shared code needs a clear platform/domain owner.
- Do not create speculative plugin frameworks, brokers, Redis, or microservices in V1.

## Backend module boundaries
backend/cmd/tendo/
- process wiring/config/startup only

backend/internal/platform/
- config
- database
- httpx
- logging
- clock
- ids
- security primitives
- no household business rules

backend/internal/identity/
- local/OIDC identities
- sessions
- invitations/authentication orchestration

backend/internal/household/
- households
- memberships
- roles/authorization policies

backend/internal/subject/
- people/home/vehicle/pet/custom subjects

backend/internal/item/
- backlog items
- lifecycle commands
- completion/history
- item-facing application services

backend/internal/schedule/
- optional attention scheduling
- optional after-completion recurrence
- pure deterministic date logic

backend/internal/integration/
- reserved namespace for real post-V1 connectors only
- may remain absent in V1; do not scaffold fake integrations

Per-module adapters:
- each business module owns its repository contract
- PostgreSQL implementation lives under that module, e.g. `internal/item/postgres/`
- HTTP adapter lives under that module or a thin routed adapter, e.g. `internal/item/httpapi/`
- platform/database owns only shared pool/migration/transaction primitives
- sqlc queries are grouped/owned by the business module they serve

Rules:
- modules expose narrow application/service interfaces
- parent business package must not import its adapter subpackages; composition happens in `cmd/tendo`
- cross-module calls use application/service interfaces or stable domain values, not another module's SQL tables
- HTTP handlers are adapters; they do not contain business rules

## Domain events / future platform seam
- Meaningful successful commands may emit typed in-process domain events, e.g. ItemCreated, ItemCompleted, ItemStateChanged.
- Event payloads use stable IDs/domain facts, not DB rows.
- V1 consumers may be synchronous/in-process.
- Do not add broker/outbox until required by a real async integration.
- Preserve event boundary so a transactional outbox can later be added without rewriting domain behavior.

## Identity/data scoping
- Public resource IDs are opaque UUIDv7 (or another documented sortable opaque UUID chosen before schema freeze); never expose sequential DB IDs as public API identity.
- Every household-owned aggregate is explicitly household-scoped in schema and authorization.
- UserAccount <-> Household uses membership relation so future multi-household use does not require schema/API redesign.
- UserAccount may store `default_household_id`; first-run sets it to the created household.
- V1 UI exposes only the default household and contains no household switcher/create-second-household flow.
- DB/schema/API authorization must remain capable of multiple memberships for future versions.
- Subject-person linkage to an account is optional.

## Time
- Audit timestamps=UTC RFC3339.
- Household timezone=IANA zone.
- Scheduling/completion business dates=date-only in household timezone.
- Inject a Clock interface into business logic/tests; no scattered direct `time.Now()` in domain code.
- Centralize month/year arithmetic in schedule module.

## Persistence/concurrency
- PostgreSQL is authoritative state store.
- Persist recurrence policy separately from current-cycle schedule state.
- Persist the current cycle's attention anchor; do not recompute it opportunistically on every read.
- Completion history stores enough cycle/policy facts to explain history and safely undo/correct the latest completion.
- A policy edit must not retroactively mutate historical completion facts.
- Migrations are ordered, reproducible, and tested from empty DB plus upgrade paths.
- Completion + history + next-cycle state changes are one DB transaction.
- Optimistic concurrency uses resource versions/ETags at API boundary.
- Accidental/retried completion requests are idempotent and cannot advance two cycles.
- Concurrent edits fail explicitly; never silently lose updates.
- No destructive schema change without explicit migration/data-preservation path.

## Frontend
- SPA communicates only through versioned REST API.
- API types/client generated or mechanically derived from OpenAPI.
- React contains presentation/client-state logic, never recurrence/authorization/date-policy rules.
- One responsive SPA serves phone/tablet/desktop; there is no native-mobile architecture in V1.
- Feature-oriented frontend structure; avoid global component dumping ground.
- Shared components remain generic presentation primitives.
- Localization from day one; Czech + English keys live in locale resources.

## Reverse proxy / networking
- Tendo itself serves plain HTTP only.
- No certificate issuance, ACME, TLS key handling, or HTTPS listener in app.
- Expected production topology: client HTTPS -> reverse proxy -> Tendo HTTP.
- `TENDO_PUBLIC_URL` is canonical external origin for links/OIDC callbacks/origin policy.
- Public URL may be HTTP for trusted local development; HTTPS expected for internet-facing deployment.
- Trust `Forwarded` / `X-Forwarded-*` only from explicitly configured trusted proxy CIDRs/hops.
- Never trust arbitrary forwarded client IP/proto headers from direct clients.
- Secure-cookie behavior follows canonical public URL/proxy-aware policy, not raw backend HTTP socket.
- Reverse proxy passes original host/proto/client information.
- CORS disabled/not needed for same-origin web app by default; future external clients get explicit allow-list configuration.
- V1 assumes URL-root deployment; hostname/subdomain reverse proxy is supported. Subpath hosting is not a V1 requirement.
- Health endpoints are proxy/container friendly and expose no private data.

## Operations
- Config through documented environment/config with safe defaults + startup validation.
- Structured logs + request/correlation ID; no secrets/private content in logs.
- `/health/live` + `/health/ready`.
- Graceful shutdown.
- Container runs non-root where practical and has explicit writable paths.
- Standard PostgreSQL backup/restore; no proprietary backup format.
- No outbound network requirement for core V1 runtime except optional configured OIDC.

## Repository
backend/
frontend/
api/
  openapi.yaml
  paths/
  components/
deploy/
docs/
  user/
  admin/
  development/
  adr/
.autonomous/
scripts/

## ADR policy
Use short ADRs only for decisions that would otherwise be rediscovered/reargued:
- schema/identity choices
- auth/session model
- API conventions
- UI starter/template
- unusual dependency
Do not create ADRs for routine implementation details.
