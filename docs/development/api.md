# API conventions

The contract is `api/openapi.yaml` (OpenAPI 3.1). It is edited together with the code that implements it. The locked policy is `.autonomous/API.md`; this page describes what is implemented.

## Basics

- Base path `/api/v1`. JSON field and query parameter names are `camelCase`; timestamps are RFC 3339 UTC.
- Authentication is the session cookie. State-changing requests also need the canonical `Origin` header (missing or foreign origin returns `403`).
- Household resources live under `/api/v1/households/{householdId}/...`. The household ID is checked against membership on every request.
- Responses carry `X-Request-ID` and `Cache-Control: no-store`.

## Errors

Errors use `application/problem+json` with `type`, `title`, `status`, and a stable `code`. Clients must not parse `detail`.

| Status | `code` | Meaning |
| --- | --- | --- |
| 400 | `invalid_request` | Malformed JSON, unknown, duplicate or null keys, wrong value types, empty patch |
| 400 | `invalid_query` | Bad query parameter; `parameter` names `limit`, `cursor`, `archived`, `done`, `subjectId`, `responsibleUserId`, `workflowState`, `attention`, `recurring`, `sort`, or `attentionDate` |
| 401 | `unauthenticated` | No valid session |
| 404 | `not_found` | Malformed ID, nonexistent resource, resource of another household, or caller not a member. The bodies are identical |
| 404 | `invalid_invitation` | Malformed, unknown, expired, revoked, or already-used invitation token |
| 403 | `owner_required` | Caller is a current household member but not an owner managing invitations |
| 400 | `idempotency_key_required`, `invalid_idempotency_key` | `Idempotency-Key` is missing or malformed on invitation, subject, item, or completion create |
| 409 | `already_member`, `household_conflict` | Existing-account invitation acceptance conflicts with current membership/default household |
| 429 | `rate_limited` | Session or invitation acceptance limit; response includes `Retry-After` |
| 412 | `precondition_failed` | `If-Match` is malformed, weak, a list, or stale |
| 413 | `content_too_large` | Body over the limit (4 KiB for subjects; 64 KiB for items) |
| 415 | `unsupported_media_type` | Body is not `application/json` |
| 422 | `invalid_length`, `invalid_characters`, `invalid_type` | Validation failure; `field` names the property. Subject names are 1 to 100 characters after trimming and may not contain control, format (zero-width, bidirectional) or line and paragraph separator characters |
| 428 | `precondition_required` | `If-Match` missing on `PATCH` |
| 503 | `unavailable` | Persistence failure; no internals are exposed |

## Invitations and members

- Only a current household owner may create, list, or revoke invitations. Members receive `403 owner_required`; non-members and unknown households receive uniform `404 not_found`. `defaultHouseholdId` grants no access.
- Create with `POST /api/v1/households/{householdId}/invitations` and one visible-ASCII `Idempotency-Key`. The body is exactly `{}`. A first creation returns `201` and the raw bearer token once; same-key retry returns `200` without it. Both responses include `Location`. Missing key returns `400 idempotency_key_required`; invalid or duplicate keys return `400 invalid_idempotency_key`. Tokens are never included in list responses.
- Owner list uses `limit` and opaque `n1:` cursors. Members list uses opaque `m1:` cursors and is available to any household member. Both default to 50 and cap at 100.
- Revoke succeeds for terminal invitations and does not remove members. New-account acceptance is `POST /api/v1/auth/invitations/accept`, returns `{userId, login, householdId, role}`, and does not set a session cookie; sign in through `POST /api/v1/session`. Existing-account acceptance is `POST /api/v1/invitations/accept`, returns `{userId, householdId, role}`, and rejects an account with an existing default household or other household membership (`409 household_conflict`); an existing member gets `409 already_member`. Malformed, unknown, expired, revoked, and already-used tokens all return `404 invalid_invitation`.
- Request evaluation order for management routes: global Origin/authority middleware (400 for malformed trusted forwarded metadata, 403 Origin, 421 authority) → session authentication (401, clearing a stale single cookie) → Idempotency-Key syntax (create only) → strict body decode → service authorization/not-found. Thus forbidden Origins are rejected before session lookup; authenticated members with malformed body/key see syntax errors before owner authorization. All responses include `X-Request-ID`; application responses use `Cache-Control: no-store`.

## Subjects

- `linkedUserId` is always present in subject representations and is either a member `userId` or `null`. The association is descriptive only, grants no access, is person-only, and is not unique. Create omission and explicit `null` both mean unlinked; PATCH omission preserves the current value and `null` unlinks. Non-null UUIDs are canonicalized to lowercase. Invalid, malformed, or foreign-household references return `422 invalid_reference`; a non-person linked result returns `422 requires_person`; membership lookup outages return `503 unavailable`.
- For create idempotency, the lowercase linked UUID is appended to the intent fingerprint only when non-null; omitted and explicit null retain the original v1 fingerprint and replay equivalently. Key lookup remains before linked-reference validation, so replay after later link edits returns the current subject representation without rechecking the link. A different link using the same key returns `422 idempotency_key_reused`.
- Create with `POST /api/v1/households/{householdId}/subjects` and exactly one case-sensitive `Idempotency-Key`: 1–128 visible ASCII bytes (`0x21`–`0x7E`). Missing returns `400 idempotency_key_required`; empty, invalid, or repeated headers return `400 invalid_idempotency_key`. Evaluation order: global Origin/authority middleware (400/403/421) → session (401) → key syntax (400) → strict body decoding (415/413/400) → household membership (404) → replay lookup → name/type validation (422).
- Keys are scoped to the resource kind, household, authenticated actor, and key. Membership authorization is still required on every retry. Replay lookup occurs after membership and before name/type domain validation. The intent is the trimmed name and the exact type. A matching submitted intent returns `201` with the subject's current representation, ETag, and original Location; edits and archive state made since creation are reflected and no version is incremented. Reusing a key for a different intent returns `422 idempotency_key_reused` without a `field`. Invalid or rejected requests do not reserve a key.

## Items

- Create with `POST /api/v1/households/{householdId}/items` and exactly one case-sensitive `Idempotency-Key`: 1–128 visible ASCII bytes. Missing returns `400 idempotency_key_required`; empty, invalid, or repeated headers return `400 invalid_idempotency_key`. Evaluation order: Origin/authority → session → key syntax → strict body decode → household membership → replay lookup → domain validation.
- Keys are scoped by resource kind, household, authenticated actor, and key. Replay identity uses the decoded creation intent (including explicit null versus empty, canonical UUIDs and canonical recurrence). A matching retry returns `201` with current item representation/ETag and original Location; changed intent returns `422 idempotency_key_reused` without a field. Replay happens before validation, so later subject archival does not prevent it. Historical initialization creates one receipt only for the committed creation; replay after receipt undo does not create another.

## OIDC authentication

- `GET /api/v1/auth/oidc` reports whether the configured provider is enabled; `displayName` appears only when enabled. `GET /api/v1/auth/oidc/identity` requires a session and reports `{linked}`.
- Start with `POST /api/v1/auth/oidc/start` and `{ "purpose": "login" }`, or link with `{ "purpose": "link", "currentPassword": "..." }` while signed in. Linking reauthenticates the local password. The request requires canonical Origin; success returns an authorization URL and sets a ten-minute HttpOnly flow cookie.
- The provider callback is `GET /api/v1/auth/oidc/callback`. It consumes the single-use flow and always returns an empty `303` redirect to a fixed application route. Success sets a fresh session cookie; link success revokes the initiating session. Failures expose only stable `oidcError` codes.

## Cursor pagination

Collections return `{"items": [...], "nextCursor": string | null}`. `nextCursor` is always present and is `null` on the last page.

- `limit`: integer 1 to 100, default 50.
- `cursor`: the `nextCursor` of the previous page. It is opaque and versioned; do not build or parse it.
- Items default to `sort=created`, ordered by `id` ascending (UUIDv7, so creation order). `sort=attention_on` orders by `attentionOn` ascending, null first, then ID ascending; null means the item needs attention now. Sort accepts exactly one nonempty `created` or `attention_on` value. A keyset traversal guarantees unchanged rows keep their order and are not duplicated; concurrent edits to sort keys may move rows across the boundary.
- Invalid values return `400 invalid_query`. Unknown query parameters are ignored.
- Subjects: `archived=false` (default) lists active subjects; `archived=true` lists archived ones only.
- Items: `archived=false` (default) lists active items; `archived=true` lists archived items only.

## Evaluation order

Checks run in this order and the first failure is returned: 403/421 origin and authority (global middleware), 401 session, 428/412 `If-Match` syntax, 415/413/400 request body, 404 household membership and subject existence, 422 field validation, 412 stale version. The `PATCH` consequence: a non-member who omits `If-Match` gets 428, and one who sends a well-formed `If-Match` gets the uniform 404. `If-Match: *`, weak tags and lists are rejected with 412.

## Items

- Create and update accept a title of 1 to 200 Unicode characters after trimming; the stored title is trimmed. Notes, when present, contain 1 to 4,000 Unicode characters and are stored exactly as provided. Send `notes: null` to clear notes. Control, format, and line/paragraph separator characters are rejected in titles; notes also reject those characters except newline, carriage return, and tab.
- `subjectId` must identify an active subject in the same household. Invalid, archived, or foreign-household subjects return `422 invalid_reference` for `subjectId`.
- `responsibleUserId` is always present in item representations and is either a user UUID or `null`. On create, omit it or send `null` to leave the item unassigned; a string assigns it. On PATCH, omission preserves the assignment, `null` clears it, and a string assigns/replaces it. The candidate must be a member of the item's household in any role; any household member may assign any other member, and the ID matches `userId` in the members list. Malformed, unknown, or foreign-household IDs return `422 invalid_reference` on `responsibleUserId`; a subject-reference error takes precedence. A membership lookup outage returns `503 unavailable`.
- Completion and undo do not snapshot or restore the responsible member: they preserve the item's current assignment, including changes made after completion. The database uses a restrictive membership FK, so future member-removal flows must explicitly clear responsible assignments with normal item version bumps before deleting memberships.
- `attentionOn` is an optional date-only value. On creation, without it, the item is `needs_attention` immediately unless `historicalCompletedOn` supplies the first attention date; before the date it is `upcoming`; on or after the date it is `needs_attention`. On PATCH, omitted leaves the stored date unchanged; `null` clears it, making the item need attention immediately. The derived `attention` is evaluated per request using the household timezone and is not stored.
- Item responses use `Cache-Control: no-store`. The strong ETag identifies the stored item version used for `If-Match`; the derived `attention` value can change at household-local midnight without changing that version.
- `workflowState` is one of `open`, `in_progress`, `waiting`, or `paused`. Archive and unarchive with the `archived` boolean; archived items remain directly readable and editable.
- `recurrence` is null for Repeat OFF (one-off). On create, omitted or null means one-off. On PATCH, omitted leaves the policy unchanged, null turns Repeat off, and an object replaces the whole policy. A policy contains an integer interval value (1–999), unit (`day`, `week`, `month`, `year`), and mode. Non-integer JSON values, including fractions and exponent notation, are `400 invalid_request`; integer literals beyond signed 64-bit range are clamped for validation and return `422 invalid_interval`. Repeat and Fluid are distinct choices: `fixed` means Repeat ON / Fluid OFF; `after_completion` means Repeat ON / Fluid ON. The policy determines the next cycle when the item is completed. Setting, changing, or clearing recurrence does not reschedule `attentionOn`, except explicit historical initialization; `attention` remains derived from `attentionOn` and household-local today.
- On create only, optional non-null `historicalCompletedOn` is a real `YYYY-MM-DD` known date; explicit null is `400 invalid_request`, while omission means absent. It requires enabled recurrence, conflicts with non-null `attentionOn`, and cannot be after household-local today (today is allowed). It creates an explicit completion receipt by the authenticated actor and initializes `attentionOn` as that date plus the recurrence interval for both modes. It never uses today as the historical date. The creation Idempotency-Key reserves this intent and prevents duplicate item/receipt creation on retry. The initial creation returns version 2 / ETag `"2"`; without historical initialization it returns version 1 / ETag `"1"`. A same-key replay returns the current item representation and ETag, which may reflect a later version. A computed attention date already in the past stays past. Create validation order after auth/household checks: title, notes, recurrence, attentionOn, historical date syntax/calendar, recurrence requirement, attentionOn conflict, future date, computed date overflow, then subject reference. Validation returns 422 `invalid_date`, `requires_recurrence`, `conflicting_fields`, or `future_date` on `historicalCompletedOn`; arithmetic overflow returns `date_overflow` on `recurrence`. The field is not accepted on PATCH.
- Invalid recurrence values return 422 with `field: recurrence` and `invalid_interval`, `invalid_interval_unit`, or `invalid_mode`.
- List accepts `limit` (1–100, default 50), opaque `cursor`, `archived` and `done` (`true` or `false`; both default `false`), `sort` (`created` by default or `attention_on`), optional `attentionDate`, plus `subjectId` (UUID), `responsibleUserId` (UUID or exact `unassigned`), `workflowState` (`open`, `in_progress`, `waiting`, `paused`), `attention` (`needs_attention`, `upcoming`), and `recurring` (exact lowercase `true` or `false`). Each optional value is single-valued; filters combine with AND. Empty/repeated/comma-list values and invalid values return `400 invalid_query` naming the parameter. UUIDs are case-insensitive; well-formed unknown/foreign IDs return an empty list, and archived subjects are valid filter targets. Filter matches are applied before the page limit.
- `attentionDate` must be exactly one canonical, real `YYYY-MM-DD` date and match household-local today. It is a freshness guard only, never an as-of date. A mismatch returns `400 invalid_query` with `parameter: attentionDate`; clients should restart the list from page one after local midnight.
- Cursor selection/bindings: omitted or explicit `sort=created` with V1-only query parameters keeps the V1 cursor format unchanged. Any structured filter, `sort=attention_on`, or a supplied `attentionDate` uses an extended cursor bound to the household and the normalized query (filters, archived/done, sort, guard presence). Changing filters, sort, or guard presence invalidates continuation with `400 invalid_query`, normally naming `cursor`; a malformed or stale supplied guard names `attentionDate` before cursor validation. Changing `limit` is allowed. Cursors from requests with an attention filter or guard expire at household-local midnight; attention-sort-only cursors do not. Traversal is deterministic for unchanged matching rows; it is not a snapshot, so a row whose sort key or matching state changes between pages may move across the page boundary. Cursors are opaque and unsigned; unknown query parameters remain ignored.

## Completions

- Create a receipt with `POST /api/v1/households/{householdId}/items/{itemId}/completions`. `Idempotency-Key` is required, case-sensitive, 1–128 visible ASCII bytes with no normalization. `If-Match` must contain the current strong item ETag. The body may be `{}` (household-local today) or specify `completedOn` as a valid, non-future business date.
- Evaluation order is session/origin, If-Match presence/syntax, idempotency header, media/body validation, household membership/item existence, then idempotency lookup under the item lock before future-date, version, lifecycle, and recurrence-overflow checks. The fingerprint binds actor and submitted-date presence/value; it excludes If-Match and resolved today. Identical retries replay the same receipt and Location without a version bump, including across local midnight. A differing request with the same key returns `422 idempotency_key_reused`. Retrying the original key and body after undo returns the same receipt with undo metadata and does not change the item's version or re-complete it.
- Success is `201` with a durable receipt and no ETag; GET the item for its current ETag. A recurring item advances from the policy snapshot (fixed cadence or after-completion) and resets workflow state to open. A one-off becomes done while its receipt remains in history. Archived and done items cannot be completed; unarchive or enabling recurrence does not reopen a done item.
- GET `/completions` returns receipts in ascending ID order using `c1:` keyset cursors (base64url without padding); limit defaults to 50 and caps at 100. Membership and item existence are required; archived/done items remain readable, and undone receipts stay in history.
- Undo the latest active receipt with `PATCH /completions/{completionId}`, `If-Match` and `{"undone":true}`. Every request requires exactly one syntactically valid strong ETag: missing returns 428; malformed, weak, wildcard, or list returns 412. Evaluation order is authentication, origin/authority, If-Match presence/syntax, body, membership and item/receipt existence, item row lock, then receipt lookup. Already-undone receipts replay unchanged after syntax validation; only the stored-version comparison is skipped, otherwise stale version returns 412. Archived returns 409, and a receipt other than the greatest-version active receipt returns `409 completion_not_latest`. Missing `undone`, null, wrong type, unknown fields, and malformed JSON return 400 `invalid_request`; `undone:false` returns 422 for field `undone` with `invalid_value`, before resource existence checks. Undo restores the receipt's attention anchor and prior workflow and clears one-off `done`, incrementing the item version. This restoration intentionally overwrites an `attentionOn` edit made after completion. Recurrence policy, title, notes, and subject are user choices and remain unchanged. Undoing successive latest receipts naturally restores prior cycles. The receipt is marked with `undoneAt` and `undoneByUserId`, never deleted. Completion POST idempotency replay after undo returns that original undone receipt and does not complete again.
- Item responses include read-only `done` and nullable `lastCompletedOn`; it is the completed date for the greatest `item_version_before` among non-undone receipts, or null if none. Item listing filters exact-match `done` (default false), independently of `archived`.

## Concurrency

- Single-resource `GET`, item/subject `POST` (create), and `PATCH` responses return a strong `ETag` such as `"3"`, the resource version. Completion `POST` returns a receipt without an ETag; fetch the item afterward for its current ETag. The version is not in the body.
- `PATCH` requires `If-Match` with one strong ETag. Missing returns `428`; anything not matching `^"[1-9][0-9]*"$`, or a stale version, returns `412`.
- Every successful update increments the version.

## Regenerating and checking

- `make -C api generate` regenerates the TypeScript types (`api/generated/`) and the Go transport types (`*.gen.go`) using native Go and locked Node dependencies; it does not require Docker.
- `npm run check` in `api/` lints the contract, runs all three contract/health/OIDC checker test files, and fails if generated files differ from a fresh generation. Run `npm audit --audit-level=moderate` as part of verification.
- Migration 015 adds the attention-order index in a transaction. Creating it may briefly block writes to `items` while PostgreSQL builds the index.
- `bash scripts/setup-smoke.sh` records real responses and validates them against the contract.
