# Recurrence root-cause audit (V5-02)

Audit for Issue #446 against the locked `.autonomous/V5_RECURRENCE_CONTRACT.md`, taken on main `f229fd4`
(v4.0.0 code). It records what the code does today, which contract rows fail and why, and the decisions
the fix (#447) must make. No production behavior was changed by this audit; every finding is locked by a
test that fails if the behavior changes.

## How Done works today

| Layer | Behavior | Source |
|---|---|---|
| Calendar | `Date` is date-only; month/year steps clamp to the target month's last day. | `backend/internal/schedule/schedule.go` `AddInterval` |
| Engine | `NextCycle(anchor, completion, policy)`. Fixed: start from `anchor`, add the interval and repeat until strictly after the completion date. Fluid: completion + interval. No anchor: completion + interval in both modes. | `schedule.go` `NextCycle` |
| Service | Completion date = submitted `completedOn` or household-local today from the injected clock and IANA zone. The engine is called with `anchor = items.attention_on`, the current (possibly clamped) due date. | `backend/internal/item/completion_plan.go` `completionDecider` |
| Storage | One transaction: `SELECT … FOR UPDATE` on the item, idempotency lookup by `(item_id, idempotency_key)`, insert receipt (snapshot of cycle date, policy, workflow state, next date), update `attention_on`/`done`/`version`. `UNIQUE (item_id, idempotency_key)` and `UNIQUE (item_id, item_version_before)`. | `backend/internal/item/postgres/completion.go`, migration `007_item_completions.sql` |
| Idempotency | Same key + same fingerprint (actor, submitted date) replays the stored receipt with 201 and does not advance, even when the item has moved on. | ADR 0005; `api/openapi.yaml` completion POST |
| Undo | Only the latest active receipt; restores `cycle_attention_on` and prior workflow state from the snapshot; receipt is kept and marked undone. | `completion_undo.go`, ADR 0005 |
| Persisted schedule | `items.attention_on` plus the policy columns (`recurrence_interval_value/unit/mode`). There is no column for the original anchor day. | migrations `005`, `006` |
| Web UI | Never computes dates; sends `POST …/completions` with `If-Match` from a fresh GET and an `Idempotency-Key` kept per user/household/item in `sessionStorage` until an outcome handler clears it. | `frontend/src/features/items/pendingCompletions.ts`, `useItemDetailCompletion.ts`, `useCompletionActions.ts`, `completionOutcomes.ts` |
| MCP | Not implemented yet (V5 scope); no second date calculation exists. | — |
| Migration | v4 data has only `attention_on`; any v4 item whose due date is already clamped (for example 2027-02-28 from a day-31 cadence) has lost its original day. | migrations `005`–`008` |

## Contract result on current code

Every golden row and the calendar coverage list run as chained Done presses on three layers: the pure engine
(`backend/internal/schedule/schedule_contract_test.go`), the item service with returned and persisted dates
(`backend/internal/item/completion_contract_test.go`) and real PostgreSQL with returned, stored-receipt and
item-row dates (`backend/internal/item/postgres/completion_contract_test.go`). The shared table is
`backend/internal/schedule/scheduletest/contract.go`.

| Row | Result |
|---|---|
| one-off; monthly fixed; monthly fixed year-rollover; monthly fixed early; monthly fluid year-rollover; monthly fixed short month; leap year monthly; late fixed skips missed anchors; fixed 2-month interval; daily fixed; weekly fixed | pass |
| fixed anchor restoration (2027-01-31 → 2027-02-28 → want 2027-03-31) | **fail**: 2027-03-28 |
| yearly leap day (2028-02-29 → … → 2031-02-28 → want 2032-02-29) | **fail**: 2032-02-28 |
| coverage: 3-month and 6-month month-end, 2-month early, 12-month leap day, leap century 2000, leap year 2104 after non-leap 2100 | **fail**: day 28 instead of 30/31/29 |
| coverage: 2-month late, 7-day/14-day December crossing, daily into Feb 29; DST spring-forward/fall-back local completion date (Europe/Prague) | pass |

Red evidence: commit `fed8e89` on PR #464 asserted the contract values directly; its required CI run failed
on exactly these rows (engine, service and PostgreSQL) and on the reproducer R2 below, before any change.

## Findings

### R1 — fixed anchor drifts after a clamp (confirmed, contract violation)

`completionDecider` passes the current due date as the fixed anchor. After a clamp the due date is the only
stored date, so the next step re-anchors on day 28 (or Feb 28) for ever: Jan 31 → Feb 28 → Mar 28 and
Feb 29 → Feb 28 → … → Feb 28 in the next leap year. The contract requires the original anchor to be persisted
independently and never drift. Root cause is the data model (no persisted anchor), not the arithmetic: the
clamping in `AddInterval` is correct for a single step.

This is a documented collision. The behavior was specified on purpose before V5: `.autonomous/PRODUCT.md`
"Time semantics" only defines single-step clamping, while `docs/development/schedule-domain.md` and the
`NextCycle` comment describe sequential clamping, and `docs/development/v2-1-coverage-matrix.md` resolved it as
"not a defect". Existing tests that pin the old policy: `schedule_test.go` `consecutive clamp`,
`leap February then consecutive clamp`, `leap clamp` (single step, still valid), and
`completion_test.go` `TestUndoThreeCompletionCyclesRestoresSnapshotsInReverse` (expects 2026-06-28). V5
precedence lets the contract replace it; the fix must update those tests and documents in the same PR, with a
reviewed compatibility decision (below).

Locked by: `PreFix` entries in `scheduletest/contract.go`. Each is a strict expected failure: the test passes
only while the engine produces exactly the recorded wrong date, so the #447 fix fails it until the entry is
deleted and the contract value is enforced.

### R2 — a later Done reuses a committed completion's key and keeps the same date (confirmed, user-visible)

This is the reproducible "monthly Done keeps the same date" path. The pending key is cleared only by the outcome
handler. On item detail that handler runs only while the screen's load generation is current, so if the user
leaves detail (or detail reloads) before the response arrives, a committed completion leaves its key in
`sessionStorage`. The next Done on that item, from detail or Home, reuses the key; the server correctly replays
the original receipt (201, no advance), and the UI reports "Next time: <the date the item already shows>". That
Done press is silently lost: no ledger entry, no schedule transition, the same date stays. The replay outcome then
clears the key, so only a further press advances. A retry of an uncertain request must keep its key (ADR 0005); a new Done after a committed completion must
not.

Locked by: `ItemDetailScreen.test.tsx` "KNOWN DEFECT R2 (#447)…" (strict expected failure: asserts the reused
key and the repeated Jan 31, 2027 date; the fix flips it). Server replay semantics stay as documented.

### Checked, no defect found

- Year rollover: Dec 31 → Jan 31 and Dec 30 → Jan 6 are correct on all layers; the year boundary is not a cause.
- Early and late fixed completion consume the current occurrence and skip missed anchors without backlog.
- Fluid recurrence starts from the local completion date, not the previous due date.
- Household-local date across DST changes is derived from the injected instant and IANA zone; recurrence uses
  date arithmetic only (`TestCompletionContractDaylightSavingLocalDate`, `TestBusinessDateTimezone`).
- Completion is one transaction under a row lock; concurrent different keys yield one winner
  (`TestCompletionItemLockSerializesVersionDecisionsPostgres`, `TestCompletionIdempotencyReplayConflictAndConcurrentVersions`).
- Undo restores the exact receipt snapshot of due date and workflow state; with an independent anchor (R1 fix)
  the snapshot must also restore the anchor.

## Decisions required in #447

1. Persist the original anchor (day of month, and month for yearly) separately from `attention_on`, set it
   on create/edit/first completion, restore it on undo, and include it in the receipt snapshot.
2. v4 upgrade compatibility: an existing `attention_on` already clamped (day 28/29/30 at a short month end) cannot
   reveal its original day. Initialize the anchor from `attention_on` as stored and never rewrite historical
   receipts; record this as the reviewed migration decision.
3. Clear a completion's pending key once the server has committed it regardless of which screen is mounted,
   while retries of an uncertain outcome keep the key.
4. Replace the sequential-clamp text in `docs/development/schedule-domain.md` and the `NextCycle` comment, and
   update the old tests listed under R1.
