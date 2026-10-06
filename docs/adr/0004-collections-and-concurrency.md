# ADR 0004: Collections and concurrency

Status: accepted

## Context

Subjects are the first collection and the first mutable resource. Later resources (items, history) need the same conventions.

## Decision

- Collections use keyset pagination on the UUIDv7 `id`, ordered ascending (`id > $cursor`). The query fetches `limit + 1` rows to decide whether a next page exists.
- The cursor is opaque to clients: base64url without padding of `s1:<uuid>`. The `s1` prefix versions the format so it can change. Encoding and decoding live in the module that owns the collection.
- `limit` defaults to 50 and is at most 100. Invalid `limit`, `cursor`, or filter values return `400` with code `invalid_query` and a `parameter` field.
- `PATCH` requires `If-Match`. A missing header returns `428 precondition_required`. A malformed, weak, or stale ETag returns `412 precondition_failed`.
- The update is one statement, `UPDATE ... WHERE household_id = $1 AND id = $2 AND version = $3`, which increments `version`. When no row matches, a household-scoped lookup distinguishes not found from a version mismatch. Every successful update increments the version.
- Audit timestamps come from the database clock.
- Request evaluation order is fixed: 401 (session) → 403/421 (origin and authority, global middleware) → 428/412 (`If-Match` syntax) → 415/413/400 (body) → 404 (membership and existence) → 422 (validation) → 412 (stale version). A non-member who omits `If-Match` therefore sees 428; with a well-formed `If-Match` the response is the uniform 404.
- The subject module does not read membership tables. It receives an `Authorizer` function, composed in `cmd/tendo` from `household.Service.Get`, and every subject query also filters by household ID.

## Consequences

- Order is stable by `id`, and a row appears at most once across the pages of one traversal. A row created concurrently may sort before an already-issued cursor (UUIDv7 ids from different connections are not strictly monotonic with commit order); it then appears only when paging restarts from the first page.
- Clients must read the ETag before editing.
- Sorting by anything other than creation order needs a new cursor version.
- Cross-module authorization is one injected function, so changing the access policy does not touch subject SQL.
