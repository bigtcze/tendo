# Recurrence root-cause audit (V5-02)

Audit for Issue #446 against the locked `.autonomous/V5_RECURRENCE_CONTRACT.md`, taken on main `f229fd4`
(v4.0.0 code). It records what the code does today, which contract rows fail and why, and the decisions
the fix (#447) must make. No production behavior was changed by this audit. Every finding has a test that
asserts the contract value directly and therefore fails on current code; no assertion was inverted or weakened
to describe today's behavior.

## How Done works today

| Layer | Behavior | Source |
|---|---|---|
| Calendar | `Date` is date-only; month/year steps clamp to the target month's last day. | `backend/internal/schedule/schedule.go` `AddInterval` |
| Engine | `NextCycle(anchor, completion, policy)`. Fixed: start from `anchor`, add the interval and repeat until strictly after the completion date. Fluid: completion + interval. No anchor: completion + interval in both modes. | `schedule.go` `NextCycle` |
| Service | Completion date = submitted `completedOn` or household-local today from the injected clock and IANA zone. The engine is called with `anchor = items.attention_on`, the current (possibly clamped) due date. | `backend/internal/item/completion_plan.go` `completionDecider` |
| Storage | One transaction: `SELECT … FOR UPDATE` on the item, idempotency lookup by `(item_id, idempotency_key)`, insert receipt (snapshot of cycle date, policy, workflow state, next date), update `attention_on`/`done`/`version`. `UNIQUE (item_id, idempotency_key)` and `UNIQUE (item_id, item_version_before)`. | `backend/internal/item/postgres/completion.go`, migration `007_item_completions.sql` |
| Idempotency | Same key + same fingerprint (actor, submitted date) replays the stored receipt with 201 and does not advance, even when the item has moved on. | ADR 0005; `api/openapi.yaml` completion POST |
| Undo | Only the latest active receipt; restores `cycle_attention_on` and prior workflow state from the snapshot; receipt is kept and marked undone. An attention edit made after the completion is overwritten when the caller sends the latest ETag. | `completion.go` `UndoCompletion`, `postgres/completion_undo.go`, ADR 0005, `api/openapi.yaml` completion undo |
| Persisted schedule | `items.attention_on` plus the policy columns (`recurrence_interval_value/unit/mode`). There is no column for the original anchor day. | migrations `005`, `006` |
| Web UI | Never computes dates; sends `POST …/completions` with `If-Match` from a fresh GET and an `Idempotency-Key` kept per user/household/item in `sessionStorage` until an outcome handler clears it. | `frontend/src/features/items/pendingCompletions.ts`, `useItemDetailCompletion.ts`, `useCompletionActions.ts`, `completionOutcomes.ts` |
| MCP | Not implemented yet (V5 scope); no second date calculation exists. | — |
| Migration | v4 data has no anchor column. See R1 "Upgrade evidence" for what can and cannot be recovered. | migrations `005`–`008` |

## Contract result on current code

Every golden row (`Golden()`) and the mandatory calendar coverage list (`Coverage()`) run as chained Done
presses on three layers: the pure engine (`backend/internal/schedule/schedule_contract_test.go`), the item
service with returned and persisted dates (`backend/internal/item/completion_contract_test.go`) and real
PostgreSQL with returned, stored-receipt and item-row dates
(`backend/internal/item/postgres/completion_contract_test.go`). The shared table is
`backend/internal/schedule/scheduletest/contract.go`.

| Row | Result |
|---|---|
| one-off; monthly fixed; monthly fixed year-rollover; monthly fixed early; monthly fluid year-rollover; monthly fixed short month; leap year monthly; late fixed skips missed anchors; fixed 2-month interval; daily fixed; weekly fixed | pass |
| fixed anchor restoration (2027-01-31 → 2027-02-28 → want 2027-03-31) | **fail**: 2027-03-28 |
| yearly leap day (2028-02-29 → … → 2031-02-28 → want 2032-02-29) | **fail**: 2032-02-28 |
| coverage: 3-month and 6-month month-end, 2-month early, 12-month leap day, leap century 2000, leap year 2104 after non-leap 2100 | **fail**: day 28 instead of 30/31/29 |
| coverage: 2-month late, 7-day/14-day December crossing, daily into Feb 29; DST spring-forward/fall-back local completion date (Europe/Prague) | pass |

Red evidence: commit `fed8e89` on PR #464 asserted the contract values directly; its required CI run failed
on these rows on the engine and service layers (golden and coverage) and on PostgreSQL for the golden rows,
and on the reproducer R2 below, before any change. In `fed8e89` the PostgreSQL test iterated only `Golden()`;
it now iterates `Golden()` and `Coverage()`, so the PostgreSQL coverage failures are evidenced by the later
CI run of this PR, not by `fed8e89`.

## Findings

### R1 — fixed anchor drifts after a clamp (confirmed, contract violation)

Two causes, both required for a fix:

1. Data model. `completionDecider` passes the current due date as the fixed anchor, and no other column holds
   the original day. After a clamp the due date is the only stored date, so the next completion re-anchors on
   day 28 (or Feb 28) for ever: Jan 31 → Feb 28 → Mar 28 and Feb 29 → Feb 28 → … → Feb 28 in the next leap
   year.
2. Engine catch-up. `NextCycle` (`schedule.go`) adds the interval to the previous result inside its
   "until strictly after completion" loop, clamping after each addition. Even with a persisted anchor, a late
   completion that skips several occurrences would drift inside one call (Jan 31 → Feb 28 → Mar 28 when
   completed in early March). The fix must compute occurrence *n* as anchor + *n* × interval, clamped once.

The clamping in `AddInterval` is correct for a single step. The contract requires the original anchor to be
persisted independently and never drift.

This is a documented collision. The behavior was specified on purpose before V5: `.autonomous/PRODUCT.md`
"Time semantics" only defines single-step clamping, while `docs/development/schedule-domain.md` and the
`NextCycle` comment describe sequential clamping, and `docs/development/v2-1-coverage-matrix.md` resolved it as
"not a defect". Existing tests that pin the old policy: `schedule_test.go` `consecutive clamp`,
`leap February then consecutive clamp`, `leap clamp` (single step, still valid), and
`completion_test.go` `TestUndoThreeCompletionCyclesRestoresSnapshotsInReverse` (expects 2026-06-28), and the
property test `schedule_property_test.go`, whose fixed-mode oracle repeats the sequential clamp. V5 precedence
lets the contract replace that policy; the fix must update those tests and documents in the same PR, with a
reviewed compatibility decision (below).

Failing tests: the contract rows listed above on all three layers (`TestNextCycleContractGolden`,
`TestNextCycleContractCoverage`, `TestCompletionContractGoldenReturnedAndPersisted`,
`TestCompletionContractGoldenPostgres`).

#### Upgrade evidence (v4 data)

Sequential clamping only ever produces day 28, 29 or 30, and the drifted day then persists (Jan 31 → Feb 28 →
Mar 28). The engine changes `attention_on` only through a completion receipt; create, edit and undo set or
restore it.

| Case | Evidence available | Recoverable? |
|---|---|---|
| fluid items, one-off items, daily/weekly fixed items | anchor is not a day-of-month | not applicable |
| monthly fixed, `attention_on` day 1–27 or 31; yearly fixed, any date other than Feb 28 | no clamp or drift produces these days | yes, exactly |
| no active receipt (never completed, or every receipt undone) | `attention_on` was entered by a user (create or edit), or restored to such a value by undo | yes, exactly: the stored date |
| monthly fixed day 28–30, or yearly fixed Feb 28, with active receipts | may be the anchor or a drifted clamp of a larger anchor | only with full receipt continuity (below); otherwise ambiguous |

Full receipt continuity: the active receipts for the item, ordered by `item_version_before`, start at
`item_version_before = 1` (no change before the first Done), consecutive values differ by exactly 1, and
`items.version = last item_version_before + 1`. Then no create-time value was ever edited, and the original
anchor is the first receipt's `cycle_attention_on`, or its `completed_on` when `cycle_attention_on` is NULL (a
historical initialization or a first Done without a date; `item_test.go` historical initialization test:
2026-01-31 → 2026-02-28). `007_item_completions.sql` guarantees unique versions, not continuity; any gap is
rejected as ambiguous without inferring which field changed.

Proposed conservative migration (for review in #447): initialize the anchor from the exact cases above;
for ambiguous items, initialize it from `attention_on` as stored without claiming exact recovery. Never rewrite
`attention_on` or historical receipts, and never guess a later day. Ambiguous items keep today's date and only
drift-free behavior from then on.

### R2 — a later Done reuses a committed completion's key and keeps the same date (confirmed, user-visible)

This is the reproducible "monthly Done keeps the same date" path. The pending key is cleared only by the outcome
handler. On item detail that handler runs only while the screen's load generation is current, so if the user
leaves detail (or detail reloads) before the response arrives, a committed completion leaves its key in
`sessionStorage`. The next Done on that item, from detail or Home, reuses the key; the server correctly replays
the original receipt (201, no advance), and the UI reports "Next time: <the date the item already shows>". That
Done press is silently lost: no ledger entry, no schedule transition, the same date stays. The replay outcome then
clears the key, so only a further press advances. A retry of an uncertain request must keep its key (ADR 0005); a new Done after a committed completion must
not.

Failing test: `ItemDetailScreen.test.tsx` "advances a fixed/after_completion monthly item with a new
idempotency key … (#446 same-date reproducer)". The fake server models the documented replay; the test requires
a new key on the second Done and the next date Feb 28, 2027. Server replay semantics stay as documented.

This is the confirmed path found by the audit. It is not proven to be the only way a user can see a repeated
date; the engine finding R1 also produces a visibly wrong (but not identical) date.

### R3 — undo overwrites an attention edit made after the completion (collision)

`UndoCompletion` checks only the item version the caller sends, the archived state and that the receipt is the
latest active one. A caller holding the latest ETag after an intervening attention edit therefore undoes the
completion and restores `cycle_attention_on`, discarding the edit. This is documented on purpose
(`api/openapi.yaml` completion undo: "an attentionOn edit after completion is intentionally overwritten by
undo"; `completion_test.go` `TestUndoOverwritesAttentionEditedAfterCompletion`). The contract requires
"reject undo for an intervening changed state rather than corrupting history".

Failing test: `completion_contract_test.go` `TestCompletionContractUndoRejectsInterveningAttentionEdit`
(requires an error and the edited date to remain). The exact rule (which intervening changes reject, the error
code, and whether the old test is replaced) is a reviewed decision for #447.

### Checked, no defect found

- Year rollover: Dec 31 → Jan 31 and Dec 30 → Jan 6 are correct on all layers; the year boundary is not a cause.
- Early and late fixed completion consume the current occurrence and skip missed anchors without backlog.
- Fluid recurrence starts from the local completion date, not the previous due date.
- Household-local date across DST changes is derived from the injected instant and IANA zone; recurrence uses
  date arithmetic only (`TestCompletionContractDaylightSavingLocalDate`, `TestBusinessDateTimezone`).
- Completion is one transaction under a row lock; concurrent different keys yield one winner
  (`TestCompletionItemLockSerializesVersionDecisionsPostgres`, `TestCompletionIdempotencyReplayConflictAndConcurrentVersions`).
- Undo with no intervening change restores the exact receipt snapshot of due date and workflow state; with an
  independent anchor (R1 fix) the snapshot must also restore the anchor.

## Decisions required in #447

1. Persist the original anchor (day of month, and month for yearly) separately from `attention_on`, set it
   on create/edit/first completion, restore it on undo, and include it in the receipt snapshot. Compute fixed
   occurrences from the anchor, not from the previous clamped result (R1 cause 2).
2. v4 upgrade compatibility: adopt or replace the conservative migration under R1 "Upgrade evidence" as the
   reviewed migration decision.
3. Clear a completion's pending key once the server has committed it regardless of which screen is mounted,
   while retries of an uncertain outcome keep the key.
4. Replace the sequential-clamp text in `docs/development/schedule-domain.md` and the `NextCycle` comment, and
   update the old tests listed under R1, including the property-test oracle.
5. Decide the undo rule for intervening changes (R3) and update `api/openapi.yaml` and the old undo test with it.
