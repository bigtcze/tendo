# Schedule domain development

This repository includes a pure Go schedule package and a persisted household-scoped items API with optional recurrence policies, completion history, and explicit historical completion initialization. See [API development](api.md), [runtime development](runtime.md), and [runtime configuration](../admin/configuration.md).

## Tooling and checks

The schedule package uses the Go standard library only. Requires Go 1.27.4 and a C compiler for race checks, or Docker. From repository root:

```sh
docker run --rm --user "$(id -u):$(id -g)" --tmpfs /tmp:rw,exec,size=1g --mount "type=bind,src=$PWD/backend,dst=/src" -w /src -e GOCACHE=/tmp/go-build golang:1.27.4-bookworm bash -c 'go version && test -z "$(gofmt -l internal/schedule/*.go)" && go vet ./... && go test -race ./... && go build ./...'
```

## Current schedule behavior

The canonical package is `backend/internal/schedule` (`internal/schedule` from the backend module). `Date` is a validated date-only value in the inclusive range 0001-01-01 through 9999-12-31; scheduling calculations do not represent dates as `time.Time`. `BusinessDate(at, zone)` converts an instant to the local calendar date in a loadable IANA timezone; it explicitly rejects empty and `Local`, while valid loadable names including `UTC` are allowed.

- A cycle is `Cycle{On Date, AnchorDay int}`: the planned attention date plus the day of month the recurrence is anchored to. The anchor is in 1..31, is never below `On.Day()`, and may exceed it only when `On` is the last day of its month (a clamped occurrence). `NextCycle` returns the next cycle and restores the anchor; there is no second arithmetic path.
- Repeat disabled yields no next cycle.
- Day/week units always set the next anchor to the next date's day of month.
- Month/year units with no current cycle, and fluid (`after_completion`) recurrence, add one interval to completion, clamp once, and anchor on the completion day.
- Fixed month/year recurrence never drifts. Occurrence `k` is month of current `On` plus `k*intervalMonths`, on day `min(AnchorDay, daysInMonth)`; the engine takes the smallest `k >= 1` whose occurrence is strictly after completion and keeps `AnchorDay` unchanged. The interval index is computed directly from the month difference, not by an unbounded loop.
- Month/year arithmetic clamps to the last valid day in the target month, per occurrence. Feb 29 + 1 year = Feb 28 in the following non-leap year; with an anchor of 31, Feb 28 clamps and the following occurrence is the 31st again.
- Historical initialization from an explicit `historicalCompletedOn` uses the no-anchor calculation in both recurrence modes and persists the resulting anchor. The resulting past attention date is retained.
- Missing or past/current attention anchors derive as `needs_attention`; future anchors derive as `upcoming`.

Examples (monthly interval): fixed anchor 2026-01-01 completed 2026-03-15 advances to 2026-04-01; fluid completion on 2026-03-15 advances to 2026-04-15. An anchor of 31 taken through Jan 31 → Feb 28 → completion advances to Mar 31, not Mar 28, because the original anchor day is restored rather than chained from the clamped date.

These functions remain pure calculations and do not persist schedule state or implement lifecycle, completion history, or completion transactions. The items API persists recurrence policy separately from the current attention date; the completion transaction that uses these calculations is described in [ADR 0005](../adr/0005-completion-receipts.md).
