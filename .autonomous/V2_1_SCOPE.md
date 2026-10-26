# V2.1: Comprehensive Quality Hardening

status=locked_human_authorized
release_target=v2.1.0
focus=exhaustive_behavior_verification_defect_remediation_codebase_refactoring_maintainability_regression_prevention

## Goal
Systematically find and FIX defects in the existing Tendo product. Test real user
behavior across every implemented function, screen, API boundary and relevant
state transition. Produce lasting regression coverage and documented evidence.
V2.1 is a quality, bug-fixing AND maintainability-hardening release, NOT a
feature-expansion release. Authorized behavior-preserving refactoring of
existing code debt is mandatory, not optional and not a speculative rewrite.
Passing the existing tests alone is never sufficient evidence of completion.

## Discovery first: independent product inventory
- Read current product/architecture/API/UX/security specifications, actual source,
  routes, components, user documentation, database schema, and existing tests.
- Inspect EVERY currently reachable UI screen and actionable control in real
  production-built browsers. Inspect supported API routes and persistent states.
- Build and maintain `docs/development/v2-1-coverage-matrix.md`:
  feature/requirement -> happy/negative/boundary/state/concurrency scenarios ->
  test IDs -> last verified outcome -> remaining gaps and residual risks.
- Capture a fresh baseline of existing behavior/tests before planning new coverage.
- Do not infer coverage from test count, HTTP 2xx, click completion or mocks.
- Prioritize user-facing and data/security-critical risks; divide into focused PRs.

## Maintainability audit and actual refactoring (required, not just a report)
- Follow the durable locked `.autonomous/CODE_QUALITY.md` in every future
  cycle and PR; implement the rules in existing code as part of V2.1.
- Independently inventory hand-written Go, React/TypeScript, SQL and scripts.
  Flag oversized files/functions, excessive branching/nesting, intertwined
  responsibilities, duplication, hidden coupling, unclear naming, weak error
  handling, fragile tests and boundary violations; omit generated/vendor files.
- Produce `docs/development/v2-1-maintainability-audit.md`: ranked existing
  hotspots with LOC/complexity where useful, ownership, concrete evidence,
  chosen remediation, PR/test IDs, and explicitly justified exceptions.
- Proactively FIX high-risk maintainability hotspots, including oversized
  multi-responsibility files, even without a currently reported bug. Split
  along coherent domain/component boundaries, reduce duplication and isolate
  effects while preserving documented behavior and existing contracts.
- Before refactoring, establish characterization/regression evidence for
  sensitive behavior; refactor in reviewable increments, run relevant tests
  before/after and record parity evidence. Fix any resulting behavior defects.
- Add or tune a pragmatic repeatable maintainability check in regular CI:
  human-written code only, transparent baseline/exceptions, meaningful signal
  on NEW or worsened debt, no mass grandfathered suppressions. Prefer existing
  linters/tooling; don't add a heavy platform just for metrics.
- Large test fixtures, generated code, migrations and cohesive large tables
  can legitimately remain large. A file-length warning is an audit trigger,
  not a reason to split a logical module into arbitrary tiny files.
- All material, actionable high-priority code-structure findings must be
  resolved by PR before V2.1 completes; residual low-risk exceptions must
  have specific technical justification and no deterioration vs baseline.

## Required scenario families (extend based on the actual implementation)
- First use, setup/onboarding, sign-in/out, session lifecycle/revocation,
  password errors, OIDC login/linking/cancellation/failure, redirects.
- Household isolation, member invitation/accept/revoke, roles/authorization,
  malicious or cross-household identifiers, permissions on every API mutation.
- Every UI route, navigation/back/forward/deep links, dialogs, menus, forms,
  validation/errors, focus, keyboard, touch, loading/empty/offline states.
- People/things, subjects and items: create/read/edit/delete-if-supported,
  complete/undo, start/wait/pause/resume, archive/restore, assignment, history.
- Repeat vs Fluid vs one-off; fixed vs completion-anchored cadence; late/early
  completion, undo, turn recurrence off/on, historical starts and migrations.
- Calendar edge conditions: month lengths, February 29, year boundaries, DST,
  Europe/Prague and other time zones, midnight, stale computed attention dates.
- Search, filters, summary counts, sort, cursors, pagination and preserved
  navigation context with empty/1/30/500/1000+ items and long/Unicode text.
- Multiple users/sessions editing simultaneously, optimistic conflicts, stale
  ETags, idempotency, duplicate requests, interrupted submissions/retries.
- Authentication failures, malformed inputs, 401/403/404/409/412/422/429,
  server errors, slow/failed network, reload and restart recovery.
- Responsive behavior at 320/360/390/768/1440px and zoom, Czech/English,
  long translated strings, accessibility/keyboard/focus and no console errors.
- Persistence after reload/relogin, database invariants, backup/restore,
  existing-data upgrades and production deployment/release smoke paths.
- When practical, browser interoperability beyond the mandatory Chromium gate.
- For combinatorial or huge/infinite state spaces, use risk-based boundaries,
  state-machine/model/property-based tests and pairwise selections; document
  uncovered risks instead of promising mathematical exhaustiveness.

## Execution: detect -> reproduce -> FIX -> verify
- Every found defect must be triaged and fixed within V2.1 unless a genuinely
  external blocker makes it impossible. Do NOT stop at filing a bug or report.
- Write a deterministic, meaningful FAILING regression test first whenever
  practical; prove it catches the bug, then fix root cause, prove GREEN and run
  relevant surrounding tests. Keep the regression test permanently.
- Validate user-visible outcomes AND persisted DB/API state; not toast messages,
  status codes, snapshots or UI mocks alone. Use actual backend + isolated
  PostgreSQL 18 + production-built frontend in Playwright E2E.
- Preserve native tests and the existing `scripts/dev-exec.sh` toolchain;
  DO NOT introduce Docker into local native integration/E2E test execution.
- Use unit, integration, contract, race, property/model and mutation checks
  when they are stronger/faster than browser testing. E2E is not a substitute
  for testing every invalid input and combination at the correct layer.
- Use isolated, reproducible seeded fixtures, controlled time, stable test data,
  distinct browser sessions and rigorous cleanup. No shared production data.
- Independently review both implementation and TEST QUALITY. On representative
  critical cases demonstrate assertions fail when behavior is deliberately
  broken in an isolated test environment; restore the code after verification.
- No flaky retries, arbitrary sleeps, skipped tests, weakened assertions,
  silenced errors, test-only application shortcuts or false green results.
- Maintain fast actionable per-PR checks and a comprehensive repeatable release
  qualification suite. Every required verification layer must run under a
  visible, enforced CI/release gate, not rely on undocumented local-only claims.
- Fix discovered defects in focused, reviewable PRs, including required API,
  docs, migration and test changes; obey protected main / green CI rules.

## Scope exclusions
Do not implement unrelated roadmap items, new integrations, AI, dashboards,
new product modules or architecture rewrites. Preserve V1 and V2 contracts,
existing stored data, localization, security and all product invariants.
Behavior-preserving maintainability refactoring and simplification of existing
code ARE explicitly authorized and required by V2.1; this is not permission
for speculative frameworks or rewrites. Only add functional changes necessary
to correct demonstrated defects or material usability/accessibility regressions
in existing supported behavior.

## Release
Completion requires traceable evidence for all supported implemented workflows,
a completed codebase maintainability audit plus verified hotspot refactoring,
lasting code-quality rules and regression controls, full green CI and
release-qualification tests, zero known release-blocking bugs,
release notes, verified published v2.1.0 artifacts and a final protected
`V2_1_COMPLETE` PR. Never assert absence of all possible defects.
