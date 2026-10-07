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
| 400 | `invalid_query` | Bad query parameter; the `parameter` field names `limit`, `cursor`, or `archived` |
| 401 | `unauthenticated` | No valid session |
| 404 | `not_found` | Malformed ID, nonexistent resource, resource of another household, or caller not a member. The bodies are identical |
| 412 | `precondition_failed` | `If-Match` is malformed, weak, a list, or stale |
| 413 | `content_too_large` | Body over the limit (4 KiB for subjects; 64 KiB for items) |
| 415 | `unsupported_media_type` | Body is not `application/json` |
| 422 | `invalid_length`, `invalid_characters`, `invalid_type` | Validation failure; `field` names the property. Subject names are 1 to 100 characters after trimming and may not contain control, format (zero-width, bidirectional) or line and paragraph separator characters |
| 428 | `precondition_required` | `If-Match` missing on `PATCH` |
| 503 | `unavailable` | Persistence failure; no internals are exposed |

## Cursor pagination

Collections return `{"items": [...], "nextCursor": string | null}`. `nextCursor` is always present and is `null` on the last page.

- `limit`: integer 1 to 100, default 50.
- `cursor`: the `nextCursor` of the previous page. It is opaque and versioned; do not build or parse it.
- Items are ordered by `id` ascending (UUIDv7, so creation order). The cursor is a keyset position, not an offset, so a row appears at most once across the pages of one traversal. A row created concurrently can sort before an already-issued cursor and then shows up only when paging restarts from the first page.
- Invalid values return `400 invalid_query`. Unknown query parameters are ignored.
- Subjects: `archived=false` (default) lists active subjects; `archived=true` lists archived ones only.
- Items: `archived=false` (default) lists active items; `archived=true` lists archived items only.

## Evaluation order

Checks run in this order and the first failure is returned: 401 session, 403/421 origin and authority (global middleware), 428/412 `If-Match` syntax, 415/413/400 request body, 404 household membership and subject existence, 422 field validation, 412 stale version. The `PATCH` consequence: a non-member who omits `If-Match` gets 428, and one who sends a well-formed `If-Match` gets the uniform 404. `If-Match: *`, weak tags and lists are rejected with 412.

## Items

- Create and update accept a title of 1 to 200 Unicode characters after trimming; the stored title is trimmed. Notes, when present, contain 1 to 4,000 Unicode characters and are stored exactly as provided. Send `notes: null` to clear notes. Control, format, and line/paragraph separator characters are rejected in titles; notes also reject those characters except newline, carriage return, and tab.
- `subjectId` must identify an active subject in the same household. Invalid, archived, or foreign-household subjects return `422 invalid_reference` for `subjectId`.
- `attentionOn` is an optional date-only value. On creation, without it, the item is `needs_attention` immediately; before the date it is `upcoming`; on or after the date it is `needs_attention`. On PATCH, omitted leaves the stored date unchanged; `null` clears it, making the item need attention immediately. The derived `attention` is evaluated per request using the household timezone and is not stored.
- Item responses use `Cache-Control: no-store`. The strong ETag identifies the stored item version used for `If-Match`; the derived `attention` value can change at household-local midnight without changing that version.
- `workflowState` is one of `open`, `in_progress`, `waiting`, or `paused`. Archive and unarchive with the `archived` boolean; archived items remain directly readable and editable.
- `recurrence` is null for Repeat OFF (one-off). On create, omitted or null means one-off. On PATCH, omitted leaves the policy unchanged, null turns Repeat off, and an object replaces the whole policy. A policy contains an integer interval value (1–999), unit (`day`, `week`, `month`, `year`), and mode. Non-integer JSON values, including fractions and exponent notation, are `400 invalid_request`; integer literals beyond signed 64-bit range are clamped for validation and return `422 invalid_interval`. Repeat and Fluid are distinct choices: `fixed` means Repeat ON / Fluid OFF; `after_completion` means Repeat ON / Fluid ON. The policy only changes what would happen at the next completion; completion behavior is not implemented yet. Setting, changing, or clearing recurrence does not reschedule `attentionOn`; `attention` remains derived from `attentionOn` and household-local today.
- Invalid recurrence values return 422 with `field: recurrence` and `invalid_interval`, `invalid_interval_unit`, or `invalid_mode`.
- List accepts `limit` (1–100, default 50), opaque `cursor`, and `archived` (`true` or `false`; default `false`). Unknown query parameters are ignored. Results use ascending ID keyset pagination.

## Concurrency

- Single-resource `GET`, `POST` (create) and `PATCH` responses return a strong `ETag` such as `"3"`, the resource version. The version is not in the body.
- `PATCH` requires `If-Match` with one strong ETag. Missing returns `428`; anything not matching `^"[1-9][0-9]*"$`, or a stale version, returns `412`.
- Every successful update increments the version.

## Regenerating and checking

- `make -C api generate` regenerates the TypeScript types (`api/generated/`) and the Go transport types (`*.gen.go`). It needs Docker.
- `npm run check` in `api/` lints the contract, runs the contract tests, and fails if generated files differ from a fresh generation.
- `bash scripts/setup-smoke.sh` records real responses and validates them against the contract.
