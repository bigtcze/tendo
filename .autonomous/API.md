# REST API Contract

status=locked_for_v1
style=resource_oriented
versioning=path_prefix
base=/api/v1
format=JSON
contract_source=api/openapi.yaml
openapi_version=3.1
errors=RFC_9457_application_problem_json

## Contract-first
- OpenAPI is edited with/just before implementation; code and spec land in the same PR.
- Generate Go transport types/server interfaces where practical using a maintained OpenAPI generator.
- Generate TypeScript API types/client from the same contract.
- CI fails on invalid OpenAPI or generated drift.
- Every operation has stable `operationId`, tags, auth requirements, request/response schemas, documented errors, and useful examples.
- Breaking changes inside `/api/v1` are forbidden after V1 release except unavoidable security/correctness fixes with migration guidance.

## Naming
- JSON fields/query parameters use `camelCase`.
- Resource names are plural lowercase nouns.
- Enum wire values use lowercase `snake_case`.
- Date-only fields end in `On`; timestamps end in `At`; IDs end in `Id`.
- Null/omitted semantics are explicit in OpenAPI; never use magic empty strings/zero dates.
- OpenAPI tags are domain-oriented: Auth, Households, Members, Subjects, Items, Completions, History.

## Resource hierarchy
Global/session resources:
- `/api/v1/session`
- `/api/v1/auth/...`
- invitation acceptance/session bootstrap endpoints only when not naturally household-nested

Household resources:
- API remains household-scoped even though V1 UI exposes only the user's default household.
- V1 does not need a household-switcher UI or a public create-second-household flow.
- `/api/v1/households`
- `/api/v1/households/{householdId}`
- `/api/v1/households/{householdId}/members`
- `/api/v1/households/{householdId}/invitations`
- `/api/v1/households/{householdId}/subjects`
- `/api/v1/households/{householdId}/items`
- `/api/v1/households/{householdId}/items/{itemId}`
- `/api/v1/households/{householdId}/items/{itemId}/completions`
- `/api/v1/households/{householdId}/items/{itemId}/history`

Rules:
- nouns/resources in URLs; no RPC soup such as `/doCompleteTask`.
- completion is a resource/event: create a completion rather than hiding it in unrelated endpoint.
- workflow/state updates use documented item mutation semantics with concurrency protection.
- archive is explicit/documented; normal API/UI does not hard-delete user history.
- household ID is authorization context, never trusted without membership validation.

## HTTP semantics
- Use standard HTTP status codes correctly.
- GET/HEAD are side-effect free.
- PUT/PATCH/DELETE semantics are idempotent where used.
- Non-idempotent POST operations that can be retried, especially completion creation, support `Idempotency-Key`.
- Creation returns `201 Created` + Location when applicable.
- Empty successful mutation may return `204`.
- Validation errors are 4xx, never `200` with error payload.
- Server failures are 5xx and do not leak internals.

## Errors
Use `application/problem+json` following RFC 9457:
- type
- title
- status
- detail
- instance/request correlation when useful
- stable machine-readable extensions for validation fields/codes

Clients never parse human `detail` text for logic.

## Response shapes
- Single-resource success returns the resource object; no generic `{data: ...}` wrapper.
- Collection success returns `{items, nextCursor}` (plus explicitly documented metadata only when useful).
- Mutation responses return the changed resource when the client needs its new version/ETag; otherwise use an appropriate empty response.

## Collections
- Cursor pagination from the first list endpoint; no unbounded collection responses.
- Request: `limit` + opaque `cursor`.
- Response: `items` + `nextCursor` (or one consistently documented equivalent).
- Enforce sane max page size.
- Filtering/sorting names/semantics consistent across resources.
- Do not expose database offsets/cursors.

## IDs/dates
- IDs are opaque strings in API.
- Audit timestamps use RFC3339 UTC.
- Household business dates use ISO `YYYY-MM-DD`.
- Recurrence/durations use structured fields; no free-form semantic encoding.

## Concurrency
- Single-resource GET exposes ETag derived from resource version.
- Mutable resource update requires/supports `If-Match` according to documented policy.
- Stale update returns a consistent documented concurrency error, normally `412 Precondition Failed`.
- Completion additionally uses idempotency semantics.
- Never silently overwrite another household member's newer edit.

## Security
- Cookie-authenticated state-changing web requests use CSRF/origin defenses.
- Authorization server-side per request/resource.
- 404/403 policy is consistent and does not leak inaccessible resource details.
- Never expose password hashes/OIDC secrets/session tokens.
- Auth-sensitive rate limiting may use simple in-process mechanism; no Redis dependency in V1.

## Observability
- Accept/propagate/generate request ID.
- Return request ID response header.
- Structured logs include request ID, route template, status, latency; never secrets/private content.

## Future compatibility
- Future integrations consume same application/API boundaries.
- Webhooks/events may be added later without breaking current resource contracts.
- No V1 endpoint named for a future provider such as Nextcloud/OpenAI/Google.
