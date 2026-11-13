# ADR 0005: Completion receipts

Status: accepted

## Decision

- A completion is a durable receipt and idempotency record. Its SHA-256 request fingerprint includes a versioned encoding of actor identity and the submitted date's presence/value, but excludes resolved household-local today and If-Match.
- After authentication, origin checks, If-Match syntax, Idempotency-Key validation, body/media validation, membership, and item existence, the repository locks the item row and checks the idempotency key before submitted-date validation/defaulting, version, and lifecycle checks. Identical requests replay the original receipt; a differing fingerprint is rejected. Receipts are preserved; undo marks a receipt undone and future correction adds a replacement rather than deleting history.
- Completion and item transition occur in one transaction. One-off completion uses a separate `done` state; repeat configuration does not itself reopen completed items.
- Receipt lists are ordered by ascending UUIDv7 ID and use versioned keyset cursors. `lastCompletedOn` is the completed date for the greatest `item_version_before` among non-undone receipts, or null if none; it does not use maximum business date.
- Undo marks the receipt with `undone_at` and actor rather than deleting it; it restores the cycle snapshot (attention date, cycle anchor, and prior workflow state) while leaving recurrence policy and other user choices untouched. The receipt also stores the next anchor, so undo restores the anchor that was current before completion. Undo is refused with `completion_superseded` when the current schedule state no longer matches the receipt snapshot: for a recurring receipt `attentionOn` must still equal `nextAttentionOn`, the item must not be done, and (unless the receipt predates anchors) the anchor must still equal `nextAnchorDay`; for a one-off receipt the item must still be done and `attentionOn` must still equal `cycleAttentionOn`. A later attention edit therefore blocks undo instead of being overwritten. Only the latest active receipt can be undone; the previous active receipt then becomes eligible.

## Consequences

- Clients can safely retry a completion request and receive the same resource/location without advancing the item twice.
- History reflects the actual completion order even when callers submit historical dates.
- Future undo and correction behavior must preserve receipt identity and history.
- Migration 021 adds the `attention_anchor_day` / `cycle_anchor_day` / `next_anchor_day` columns and backfills anchors, including an exact-recovery path for clamped fixed-month/year chains. Its Down migration drops the columns: anchors are lost and recurrence falls back to the old sequential-clamp drift behavior until anchors are rebuilt.
