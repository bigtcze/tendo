# REST API contract

status=canonical_version_independent
source_of_truth=this_file + api/openapi.yaml
style=resource_oriented
versioning=path_prefix
base=/api/v1
format=JSON
contract_source=api/openapi.yaml
openapi_version=3.1
errors=RFC_9457_application_problem_json

## Contract-first
- OpenAPI is edited with or just before implementation; code and spec land in the same PR.
- Go transport types and server interfaces are generated where practical with a maintained generator.
- TypeScript API types and client are generated from the same contract.
- CI fails on invalid OpenAPI or generated drift (TESTING "Contract tests").
- Every operation has a stable `operationId`, tags, auth requirements, request and response schemas, documented errors and useful examples.
- Breaking changes inside `/api/v1` are forbidden except unavoidable security or correctness fixes with migration guidance.
- Every implemented public endpoint is in OpenAPI.

## Naming
- JSON fields and query parameters use `camelCase`.
- Resource names are plural lowercase nouns.
- Enum wire values use lowercase `snake_case`.
- Date-only fields end in `On`; timestamps end in `At`; IDs end in `Id`.
- Null and omitted semantics are explicit in OpenAPI. No magic empty strings or zero dates.
- Tags are domain-oriented: Auth, Households, Members, Invitations, Subjects, Items, Completions, History, Mcp (admin and user MCP settings).

## Resource hierarchy
Global and session resources:
- `/api/v1/session`
- `/api/v1/auth/...`
- invitation acceptance and session bootstrap, only when not naturally household-nested.

Household resources (household ID is always the authorization context):
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
- Nouns and resources in URLs. No RPC endpoints such as `/doCompleteTask`.
- Completion is a resource event: create a completion; do not hide it in an unrelated endpoint.
- Workflow and state updates use documented item mutation semantics with concurrency protection.
- Archive is explicit and documented. Normal API and UI never hard-delete user history.
- Household ID is never trusted without membership validation (SECURITY.md "Authorization invariants").
- Household resource paths above are the target contract. Implementation status is recorded as evidence in release Issues, not here.
- Instance-administration resources are separate from household resources and are not exposed to MCP.

## HTTP semantics
- Standard status codes, used correctly.
- GET and HEAD are side-effect free.
- PUT, PATCH and DELETE are idempotent where used.
- Retryable non-idempotent POST operations, especially completion creation, support `Idempotency-Key`.
- Creation returns `201 Created` with `Location` where applicable.
- Empty successful mutation may return `204`.
- Validation errors are 4xx, never `200` with an error payload.
- Server failures are 5xx and do not leak internals.

## Errors
Use `application/problem+json` (RFC 9457):
- type, title, status, detail
- instance or request correlation when useful
- stable machine-readable extensions for validation fields and codes

Clients never parse human `detail` text for logic.

## Response shapes
- Single-resource success returns the resource object. No generic `{data: ...}` wrapper.
- Collection success returns `{items, nextCursor}`, plus documented metadata only when useful.
- Mutation responses return the changed resource when the client needs its new version or ETag. Otherwise an empty response.

## Collections
- Cursor pagination on every list endpoint. No unbounded collection responses.
- Request: `limit` and opaque `cursor`. Response: `items` and `nextCursor`, or one consistently documented equivalent.
- Enforce a sane maximum page size.
- Filtering and sorting names and semantics are consistent across resources.
- Search, filters and counts operate on the COMPLETE matching household dataset, not on a loaded page. Prefer server-side query and indexing (UX.md "Scale").
- Do not expose database offsets or cursors.
- Expired invitations are excluded from invitation lists and counts (SECURITY.md "Invitations").

## IDs and dates
- IDs are opaque strings.
- Audit timestamps are RFC3339 UTC.
- Household business dates are ISO `YYYY-MM-DD`.
- Recurrence and durations use structured fields; no free-form semantic encoding (RECURRENCE.md).

## Concurrency
- Single-resource GET exposes an ETag derived from the resource version.
- Mutable resource updates require or support `If-Match` per the documented policy.
- Stale updates return `412 Precondition Failed` with a consistent documented concurrency error.
- Never silently overwrite another member's newer edit.

## Completions
- `POST .../items/{itemId}/completions` creates or replays a Completion receipt, returns 201 with its canonical Location and no ETag. GET the item for its current state and ETag, as specified in `api/openapi.yaml`; consolidation does not change the public response shape.
- It is the only endpoint that advances a repeating item (RECURRENCE.md).
- Undo or correction of the latest completion uses a documented endpoint or mutation that restores recorded facts (RECURRENCE.md "Completion, idempotency, undo").
- Completion semantics for MCP are the same as for the API and call the same service.

## Completion idempotency
- Completion creation supports `Idempotency-Key` (API.md "HTTP semantics"). A retry with the same key returns the original result and never advances the item twice (RECURRENCE.md "Completion, idempotency, undo").
- Simultaneous conflicting requests never advance the item twice.
- Preserve the existing `api/openapi.yaml` completion contract: keys are item-scoped; fingerprints include actor and submitted-date presence/value, excluding If-Match and resolved today. Identical retries replay the original receipt/Location without incrementing item version across local midnight and after undo (the receipt then reports undone; no re-completion). Reuse with a different fingerprint returns 422 `idempotency_key_reused`. Keys are case-sensitive printable ASCII, 1–128 bytes, without normalization, and required alongside If-Match. OpenAPI specifies validation order and errors. Do not reopen these decisions in an ordinary implementation PR; any necessary compatibility change requires reviewed migration guidance and regression evidence. Any retention policy must preserve promised replay behavior.

## Security
- Cookie-authenticated state-changing web requests use CSRF and origin defenses.
- Authorization is enforced server-side per request and per resource.
- 404/403 policy is consistent and never leaks inaccessible resource details.
- Never expose password hashes, OIDC secrets, session tokens, OAuth tokens or invitation tokens (SECURITY.md "Logging, audit and secrets").
- Auth-sensitive rate limiting uses a simple in-process mechanism. No Redis dependency.

## Observability
- Accept, propagate and generate request IDs. Return the request ID response header.
- Structured logs include request ID, route template, status and latency. Never secrets or private content.

## Future compatibility
- Future integrations consume the same application and API boundaries.
- Webhooks and events may be added later without breaking current resource contracts.
- No endpoint named for a future provider.
