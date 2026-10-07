# ADR 0005: Completion receipts

Status: accepted

## Decision

- A completion is a durable receipt and idempotency record. Its SHA-256 request fingerprint includes a versioned encoding of actor identity and the submitted date's presence/value, but excludes resolved household-local today and If-Match.
- After authentication, origin checks, If-Match syntax, Idempotency-Key validation, body/media validation, membership, and item existence, the repository locks the item row and checks the idempotency key before submitted-date validation/defaulting, version, and lifecycle checks. Identical requests replay the original receipt; a differing fingerprint is rejected. Receipts are preserved; undo marks a receipt undone and future correction adds a replacement rather than deleting history.
- Completion and item transition occur in one transaction. One-off completion uses a separate `done` state; repeat configuration does not itself reopen completed items.
- Receipt lists are ordered by ascending UUIDv7 ID and use versioned keyset cursors. `lastCompletedOn` is the completed date for the greatest `item_version_before` among non-undone receipts, or null if none; it does not use maximum business date.
- Undo marks the receipt with `undone_at` and actor rather than deleting it; it restores the cycle snapshot (attention anchor and prior workflow state) while leaving recurrence policy and other user choices untouched. The snapshot intentionally overwrites a later attention-anchor edit. Only the latest active receipt can be undone; the previous active receipt then becomes eligible.

## Consequences

- Clients can safely retry a completion request and receive the same resource/location without advancing the item twice.
- History reflects the actual completion order even when callers submit historical dates.
- Future undo and correction behavior must preserve receipt identity and history.
