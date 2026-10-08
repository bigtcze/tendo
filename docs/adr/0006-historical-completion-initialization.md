# ADR 0006: Historical completion initializes the first recurring cycle

Status: accepted

This extends [ADR 0005](0005-completion-receipts.md): when a user explicitly reports a known historical completion while creating a recurring item, the system records it as an ordinary completion receipt in the same transaction as item creation. The receipt records the actor, submitted historical date, creation recurrence snapshot, null prior cycle anchor, and computed next attention date; item version moves from 1 to 2. No receipt is inferred from the current date.

The initialization receipt uses a fresh internal UUIDv7 idempotency key. This key only satisfies the existing receipt uniqueness/storage model; it is not POST /items creation idempotency and is not exposed as such. The historical date is explicit user input and is never replaced by household-local today.

The receipt is an ordinary latest completion and follows the existing undo path: undo restores the null prior attention anchor and open workflow state, increments item version, and preserves receipt history. No schema migration is required.
