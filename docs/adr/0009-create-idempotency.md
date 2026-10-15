# ADR 0009: Subject and item creation idempotency

Status: accepted

Scope: implemented for subjects (migration 12) and items (migration 13).

## Context

`POST` creation of subjects and items is retryable: a lost response followed by a retry must not create a duplicate (locked API contract: retryable non-idempotent POSTs support `Idempotency-Key`). Completions (ADR 0005) and invitations (ADR 0007) already require a key. Subjects and items are never hard-deleted, so the resource row itself can carry its creation record.

## Decision

- `Idempotency-Key` is **required** on `POST .../subjects` and `POST .../items`, with the same syntax as completions: exactly one header value, 1–128 visible ASCII bytes (`0x21`–`0x7E`), case-sensitive, no normalization. Missing returns `400 idempotency_key_required`; empty, repeated, or invalid returns `400 invalid_idempotency_key`.
- Scope is (resource kind, household, authenticated actor, key). The same key used for the other resource kind, by another actor, or in another household is an independent creation.
- Creation metadata lives on the resource row: `created_by_user_id`, `creation_key`, `creation_request_fingerprint` (32-byte SHA-256), all null or all non-null, unique on (`household_id`, `created_by_user_id`, `creation_key`). Rows created before this decision keep null metadata; no creator or fingerprint is invented. The columns are private, immutable (no runtime `UPDATE` grant), and never appear in public representations.
- The fingerprint is SHA-256 over a versioned, fixed-field encoding of the actor and the submitted creation intent after canonicalization (trimmed name/title, canonical UUIDs, explicit null for omitted optional fields). JSON field order and formatting do not matter. It never includes the current date, generated IDs, timestamps, or derived values. Fingerprints are compared against the stored metadata, never against current (possibly edited) fields.
- Evaluation order: Origin/authority, session, key syntax, strict body decoding, household membership, then **replay lookup before domain validation**. A stored key with the same fingerprint returns `201` with the resource's **current** representation, current `ETag`, and original `Location`, and performs no write. A stored key with a different fingerprint returns `422 idempotency_key_reused` (no `field`). Replay is idempotency of identity and effect, not a byte-for-byte replay of the first response: later edits, archive, or completions are visible.
- Rejected requests do not reserve a key; only a committed creation does.
- Concurrency: insert with `ON CONFLICT (household_id, created_by_user_id, creation_key) DO NOTHING`; on no row, a separate read resolves replay or reuse. No locks or shared idempotency table.
- For items initialized with a historical completion (ADR 0006), only the transaction that inserts the item creates the initialization receipt; a replay never re-enters that path, including after the receipt was undone.
- The SPA generates one random key per deliberate submit and persists the unresolved attempt (key + exact submitted body) per user, household, and resource kind in `sessionStorage` with memory fallback, so that an explicit retry after a lost response, navigation, or reload resends the same key and body.

## Consequences

- Third-party clients must send a key on every create; generating a random UUID is enough.
- A reused key with a changed body is an explicit client error, never a silent second creation.
- When storage is unavailable, retry still works within the page, but recovery after reload cannot be guaranteed.
