# Schedule domain development

This repository includes a pure Go schedule package and a persisted household-scoped items API with optional recurrence policies, completion history, and explicit historical completion initialization. See [API development](api.md), [runtime development](runtime.md), and [runtime configuration](../admin/configuration.md).

## Tooling and checks

The schedule package uses the Go standard library only. Requires Go 1.27.2 and a C compiler for race checks, or Docker. From repository root:

```sh
docker run --rm --user "$(id -u):$(id -g)" --tmpfs /tmp:rw,exec,size=1g --mount "type=bind,src=$PWD/backend,dst=/src" -w /src -e GOCACHE=/tmp/go-build golang:1.27.2-bookworm bash -c 'go version && test -z "$(gofmt -l internal/schedule/*.go)" && go vet ./... && go test -race ./... && go build ./...'
```

## Current schedule behavior

The canonical package is `backend/internal/schedule` (`internal/schedule` from the backend module). `Date` is a validated date-only value in the inclusive range 0001-01-01 through 9999-12-31; scheduling calculations do not represent dates as `time.Time`. `BusinessDate(at, zone)` converts an instant to the local calendar date in a loadable IANA timezone; it explicitly rejects empty and `Local`, while valid loadable names including `UTC` are allowed.

- Repeat disabled yields no next cycle.
- Fixed recurrence advances from the planned anchor, repeatedly adding one interval until the date is strictly after completion. If no anchor exists, completion is the starting anchor. It does not skip historical intervals when initializing from a historical completion. On item creation, explicit `historicalCompletedOn` invokes this no-anchor calculation for the initial cycle in both recurrence modes; the resulting past attention date is retained.
- Fluid (`after_completion`) recurrence adds one interval to completion.
- Month/year arithmetic clamps to the last valid day in the target month. Each step is clamped independently, so Jan 31 + 1 month = Feb 28 (or Feb 29 in leap years), then another monthly step = Mar 28/29. Feb 29 + 1 year = Feb 28 in the following non-leap year.
- Missing or past/current attention anchors derive as `needs_attention`; future anchors derive as `upcoming`.

Examples (monthly interval): fixed anchor 2026-01-01 completed 2026-03-15 advances to 2026-04-01; fluid completion on 2026-03-15 advances to 2026-04-15. The sequential clamping choice deliberately preserves each consecutive interval step rather than reapplying the original day-of-month after a clamp.

These functions remain pure calculations and do not persist schedule state or implement lifecycle, completion history, or completion transactions. The items API persists recurrence policy separately from the current attention date; completions are not yet implemented. See [the V1 plan](../../README.md) for project status.
