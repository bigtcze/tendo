# Test Strategy

status=locked
principle=tests_prove_behavior_not_presence
required_layers=unit,integration,e2e,contract,build

## Quality bar
- A test must be capable of failing when the behavior it claims to protect is broken.
- Do not write tests solely to increase coverage percentage.
- Do not mock the thing being tested.
- Core domain tests assert state/invariants, not implementation call counts.
- HTTP tests assert response contract AND resulting persistent/domain state where relevant.
- A `200` assertion alone is not a meaningful API test.
- Every bug fix gets a regression test that fails before the fix.
- Flaky tests are defects; fix root cause, never hide with retries.
- Required CI has zero ignored failures and no blanket skips.
- Coverage is diagnostic, not a substitute for behavior/invariant review.

## Unit tests
Target:
- fixed recurrence arithmetic
- fluid/after-completion recurrence arithmetic
- transitions between repeat OFF / fixed / fluid settings
- current-cycle attention derivation
- lifecycle transitions
- authorization policies
- validation
- idempotency helpers
- pure domain/application behavior

Rules:
- deterministic Clock/ID providers.
- table-driven boundary cases.
- explicit leap-year/end-of-month/timezone cases.
- property/fuzz tests for recurrence arithmetic/invalid state transitions where valuable.
- Go race detector in required backend path.

Required recurrence cases:
- repeat OFF completion creates no next cycle
- fixed cadence late completion preserves planned cadence
- fixed cadence early completion preserves planned cadence
- fixed cadence completed after multiple missed anchors advances to first future anchor without duplicate backlog items
- fluid late completion shifts next cycle from completion
- fluid early completion shifts next cycle from completion
- switching fixed <-> fluid does not rewrite completion history or silently move the current cycle's attention date
- changing interval/mode affects the next completion calculation, not historical facts
- turning Repeat OFF does not delete history
- first cycle with no explicit anchor behaves deterministically
- historical completion initializes the first attention date; a past computed date remains NEEDS_ATTENTION rather than being silently skipped

## Integration tests
Use real PostgreSQL, not a mocked repository, for persistence/API integration behavior.
Prefer ephemeral isolated PostgreSQL via testcontainers or CI service.

Must cover:
- migrations from empty database
- household name + timezone first-run persistence
- schema supports many-to-many household memberships
- V1 onboarding/UI application path establishes one default household
- repository constraints/transactions/rollback
- household authorization boundaries
- local auth/session behavior
- OIDC integration against deterministic local test provider/stub at protocol boundary
- CSRF/origin behavior
- idempotency-key persistence/semantics
- ETag optimistic concurrency
- completion double-submit/concurrent completion
- fixed and fluid recurrence persistence/transition semantics
- archive/history preservation
- RFC 9457 problem contract

## Contract tests
- OpenAPI validates in CI.
- Generated Go/TS artifacts are reproducible/drift-free.
- Every implemented public endpoint exists in OpenAPI.
- Representative success/error responses validate against schema.
- API examples remain valid.

## Frontend tests
Protect meaningful interaction/state:
- first-run asks household/family name
- no household switcher in V1 UI
- Repeat toggle OFF by default
- Repeat ON reveals interval
- Repeat ON reveals separate fluid toggle
- fluid toggle wording clearly distinguishes fixed vs completion-based cadence
- one-off item creation does not require recurrence fields
- error/empty/loading states
- accessibility-critical form behavior
- Czech/English localization smoke

Do not use whole-page snapshots as the only assertion.

## E2E tests
Browser=Playwright
Runtime=production_built_Tendo+real_PostgreSQL
Mocks=none_for_Tendo_backend_or_DB

Primary V1 journeys:
1. first-run owner onboarding -> enter household name -> confirm timezone -> empty home
2. create child/person subject without user account
3. create one-off item with Repeat OFF; complete; verify active backlog/history
4. create future one-off attention item; verify upcoming -> attention with controlled clock
5. create fixed repeating item (Repeat ON, Fluid OFF); complete late; verify original cadence is preserved
6. create fluid repeating item (Repeat ON, Fluid ON); complete late; verify next cycle is completion+interval
7. in-progress -> waiting -> complete; verify only completion advances recurrence
8. undo/correct latest completion; verify schedule/history
9. invite second user; verify role behavior
10. unauthorized household/resource access blocked
11. responsive narrow + wide viewport smoke for home/create/detail
12. Czech/English smoke

Time-sensitive E2E uses deterministic test clock/config; never `sleep until tomorrow`.

## Reverse-proxy tests
Verify:
- untrusted forged `X-Forwarded-*` ignored
- configured trusted proxy headers produce correct public-origin behavior
- secure session/OIDC callback behavior follows `TENDO_PUBLIC_URL`
- health endpoints work behind proxy assumptions

## Required CI target
Stable aggregate check name=`required`.
- The PR that first introduces a code layer also wires that layer's required tests/checks into CI.
- Once a required layer exists, later PRs do not remove/bypass it.

As implementation exists, `required` gates:
- generated/contract drift
- Go format/vet/static analysis
- Go unit tests + race
- Go integration tests + PostgreSQL
- frontend lint/typecheck/unit
- Playwright E2E
- production Docker build
- migration smoke/upgrade fixtures as they appear

Never merge by temporarily removing a failing layer.

## Review questions
Every behavior PR answers:
- what user/domain behavior changed?
- which test fails if it regresses?
- are fixed + fluid recurrence effects considered when relevant?
- are failure/edge/concurrency paths covered?
- did test code accidentally reimplement production logic?
