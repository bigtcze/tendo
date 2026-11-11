# V5 recurring item semantics — locked acceptance contract

status=human_authorized
priority=P0
scope=domain,application,storage,API,frontend,MCP

## Definitions
- Household timezone is an IANA zone. A task's date recurrence uses household-local CALENDAR DATES, not UTC-hour arithmetic. Completion has an instant and a household-local completion date derived from an injected clock.
- The original recurrence anchor (day-of-month or month/day where applicable) and interval are persisted independently of adjusted due dates. Never drift the anchor from 31 to 28 or 29, or from Feb 29 permanently to Feb 28.
- Fixed/anchored: one completion consumes the CURRENT scheduled occurrence and chooses the earliest strictly subsequent anchor that is also after the local completion date. Completing early still consumes the current occurrence; completing late never recreates months of overdue backlog. Never produce the same date on successful completion.
- After-completion/fluid: next date is completion-local calendar date + one configured interval, respecting documented end-of-month/leap-day policy. Not calculated from prior due date.
- One-off: completion records history and leaves no next active occurrence.
- A completion requires a stable item/version/idempotency key contract. Accepted command writes exactly one ledger entry and exactly one schedule transition in one DB transaction; duplicate identical requests return original result, conflicting simultaneous requests do not advance twice.
- Undo restores the EXACT previous due/attention state, recurrence anchor and completion ledger semantics. Undo must be tested for next scheduled cycle, late/early and concurrency paths; reject undo for an intervening changed state rather than corrupting history.

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

These sample dates are REQUIRED behavioral decisions for v5, not unverified claims about the currently implemented Tendo engine. If a collision with an existing critical invariant is found, document it with failing test and a reviewed compatibility/migration decision; do not silently override historical data.

## Additional mandatory coverage
- Early/exact/late completion; repeated intervals >1; end-of-month and leap years; daylight saving offset changes; household timezone transitions; no-date first cycle; recurring task toggled on/off or switched fixed/fluid; edits after completion and undo; archived/paused/waiting transitions; old populated v4 data migration; date serialization and API contracts.
- Tests enumerate 1, 2, 3, 6, 12-month and 1, 7, 14-day intervals with randomized calendar/property tests ensuring strict progress and no anchor drift. Explicit January/December crossing and Gregorian leap-century cases (2000, 2100) are mandatory.
- Separate pure calendar function table-driven tests, API/service tests, real-PostgreSQL transactional/idempotency/concurrent tests, Playwright actual Done/Undo and MCP tool path. Inject clock/timezone; do not rely on real time or sleeps.
- FIRST create a regression test that reproduces the current same-date completion bug and fails on the pre-fix code. Do not change implementation before identifying what failed. All tests must be wired into stable `required` CI, not only local opt-in suites.
- Document user-facing CZ/EN examples explaining fixed vs after-completion recurrence. No jargon in simple task form.
