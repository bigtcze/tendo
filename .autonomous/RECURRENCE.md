# Recurrence contract

status=canonical_version_independent
source_of_truth=this_file
consolidates=V1 PRODUCT/UX/TESTING/DEFINITION_OF_DONE recurrence content; V5_RECURRENCE_CONTRACT (migrated here); V2.1/V3/V4 recurrence and item-lifecycle scope and DoD bullets
priority=P0
scope=domain,application,storage,API,frontend,MCP

## Definitions
- Household timezone is an IANA zone. Recurrence uses household-local CALENDAR DATES, never UTC-hour arithmetic.
- Completion has an instant and a household-local completion date derived from an injected Clock.
- Business dates are date-only in the household timezone. Audit timestamps are UTC RFC3339.
- Policy and cycle state are separate:
  - RecurrencePolicy: enabled, interval_value, interval_unit=day|week|month|year, mode=fixed|after_completion.
  - CurrentCycle: attention_on (nullable date), the planned attention anchor of the current cycle.
  - CompletionEvent: completed_on, completed_by, snapshot of the completed cycle's attention_on, snapshot of the policy relevant to that completion.
- Original recurrence anchor (day-of-month, or month/day where applicable) and interval are persisted INDEPENDENTLY of adjusted due dates. Never drift the anchor from 31 to 28/29, or from Feb 29 permanently to Feb 28.
- Two independent user decisions:
  1. Repeat ON/OFF: should a new cycle appear after completion?
  2. Fluid recurrence ON/OFF (only visible when Repeat is ON): what anchors the next cycle?
- Repeat OFF: completion ends the active item; history remains; fluid control is hidden.
- Repeat ON + Fluid OFF = internal mode `fixed`. Repeat ON + Fluid ON = internal mode `after_completion`. UI never shows these internal terms.

## Calculation rules
- Fixed: one completion consumes the CURRENT scheduled occurrence and chooses the earliest strictly subsequent anchor that is also after the local completion date. Completing early still consumes the current occurrence; completing late never recreates overdue backlog. A successful completion never yields the same date.
- If the first cycle has no scheduled anchor, completion establishes the initial cadence: next = completion local date + interval.
- After-completion (fluid): next date = completion-local calendar date + one configured interval, respecting the end-of-month/leap-day policy. Never calculated from the prior due date.
- Month/year arithmetic clamps to the last valid day of the target month (Jan 31 + 1 month = Feb 28/29). The clamp applies to the adjusted due date only; the persisted anchor is restored on the next fixed occurrence (see golden cases).
- One-off: completion records history and leaves no next active occurrence.
- Optional known historical completion initializes the first cycle: attention_on = historical_completed_on + interval, for both modes. Modes differ only after the next real completion. If the computed date is already past, the item is NEEDS_ATTENTION immediately; never skip it forward. Never fabricate a historical completion to make scheduling work.
- Attention derivation: no attention_on means NEEDS_ATTENTION immediately; before the date means UPCOMING; on or after means NEEDS_ATTENTION. Recurrence does not force an initial date.
- Backend is the sole authority for schedule calculation. Frontend and MCP do no date arithmetic.
- Editing policy (interval/mode) affects the NEXT completion only. It does not rewrite the current attention_on. Editing attention_on is a separate explicit action.
- Switching fixed <-> fluid does not rewrite completion history or silently move the current attention date.

## Completion, idempotency, undo
- A completion requires a stable item/version/idempotency-key contract (API.md "Completion idempotency").
- An accepted command writes exactly one CompletionEvent and exactly one schedule transition in ONE DB transaction.
- Duplicate identical requests return the original result. Conflicting simultaneous requests must not advance twice.
- Undo/correct restores the EXACT previous due/attention state, recurrence anchor and completion-ledger semantics, recomputed from recorded facts, never from current UI values.
- Undo is rejected for an intervening changed state rather than corrupting history.
- In-progress, waiting, paused and resumed transitions never advance recurrence. Only completion advances it.
- Pause keeps the schedule clock running; resuming reflects current derived time state immediately.

## Golden cases (tests assert exact returned AND persisted dates)

| Rule | Before / completion | Expected after |
|---|---|---|
| one-off | date 2026-12-31, complete 2026-12-11 | no next date; completed |
| monthly fixed | due 2026-11-08, complete 2026-11-08 | 2026-12-08 |
| monthly fixed year-rollover | due 2026-12-31, complete 2026-12-31 | 2027-01-31 |
| monthly fixed early | due 2026-12-31, complete 2026-12-11 | 2027-01-31 |
| monthly fluid year-rollover | due 2026-12-31, complete 2026-12-11 | 2027-01-11 |
| monthly fixed short month | due 2027-01-31, complete 2027-01-31 | 2027-02-28 |
| fixed anchor restoration | due 2027-02-28 (original anchor=31), complete 2027-02-28 | 2027-03-31 |
| leap year monthly | due 2028-01-31, complete 2028-01-31 | 2028-02-29 |
| yearly leap day | due 2028-02-29, complete 2028-02-29 | 2029-02-28, then 2030-02-28, then 2031-02-28, then 2032-02-29 |
| late fixed skips missed anchors | due 2026-09-08, complete 2026-12-11 | 2027-01-08 |
| fixed 2-month interval | due 2026-11-30, complete 2026-11-30 | 2027-01-30 |
| daily fixed | due 2026-11-11, complete 2026-11-11 | 2026-11-12 |
| weekly fixed | due 2026-12-30, complete 2026-12-30 | 2027-01-06 |

These sample dates are REQUIRED behavioral decisions for v5, not unverified claims about the currently implemented engine. If a collision with an existing critical invariant is found, document it with a failing test and a reviewed compatibility/migration decision; never silently override historical data.

Additional examples (from product semantics, kept as tests):
- Yearly item scheduled 2026-09-01, completed 2026-09-20 -> next 2027-09-01 (fixed).
- Monthly item scheduled 2026-01-01, completed 2026-03-15 -> next 2026-04-01 (fixed).
- Yearly item scheduled 2026-09-01, completed early 2026-08-20 -> next 2027-09-01 (fixed).
- 12-month item scheduled 2026-09-01, completed 2026-10-20 -> next 2027-10-20 (fluid).
- Fluid late and early completion both shift the next cycle from the completion date.

## Mandatory test coverage
- Early, exact and late completion.
- Repeated intervals >1.
- End-of-month and leap years, including Gregorian leap centuries 2000 and 2100.
- Daylight-saving offset changes; household timezone transitions; midnight boundaries.
- No-date first cycle.
- Recurring task toggled on/off or switched fixed <-> fluid.
- Edits after completion, and undo: explicitly test undo for the next scheduled cycle, early/late completion and concurrency paths, asserting exact restored schedule, anchor and ledger semantics.
- Archived, paused and waiting transitions.
- Old populated v4 data migration.
- Date serialization and API contracts.
- Property/randomized calendar tests enumerating intervals 1, 2, 3, 6, 12 months and 1, 7, 14 days, asserting strict progress and no anchor drift.
- Explicit January/December year crossing.
- Layers, all required in stable `required` CI (TESTING "Required CI target"):
  - pure calendar function table-driven tests;
  - API/service tests;
  - real-PostgreSQL transactional, idempotency and concurrency tests;
  - Playwright actual Done/Undo flows;
  - MCP tool path.
- Inject clock and timezone. No real time, no sleeps.
- Property-based coverage per TESTING "Unit tests".

## Regression rule
- The same-date monthly Done failure (completion retained the same date instead of advancing) is reproduced by a regression test that FAILS on pre-fix code before any implementation change. The fix is at the canonical application service, shared by UI, API and MCP.

## User-facing copy
- CZ/EN examples explain fixed vs after-completion cadence in simple household terms. No "policy", "anchor", "fluid", "after_completion" or ETag vocabulary in UI (UX.md "Language").

## Links
- Lifecycle states and item states: see PRODUCT.md "Lifecycle".
- Idempotency and ETag wire rules: API.md "Concurrency" and "Completions".
- Persistence transaction rules: ARCHITECTURE.md "Persistence and concurrency".
- Test layers: TESTING.md.
- Done/undo acceptance: DEFINITION_OF_DONE.md "Items, recurrence and lifecycle".
