# Tendo Code Quality and Maintainability Policy

status=locked_human_authorized
agent_must_not_edit=this_file
applies=all_future_changes_and_V2_1_existing_debt_remediation
sources=go.dev/wiki/CodeReviewComments,google.github.io/styleguide/go/guide.html,react.dev/reference/rules,eslint.org/docs/latest/rules

## Core engineering principles
- Optimize for future maintainers: readable, correct, cohesive, explicit code
  with minimal moving parts. Prefer straightforward idiomatic code over clever
  abstractions, design-pattern ceremonies, deep indirection or micro-files.
- Single responsibility means one cohesive reason to change, not one line or
  one method per file. Keep boundaries grounded in the actual domain and usage.
- Minimize duplication of business rules and state transitions; centralize
  authoritative logic without creating generic, context-free helper buckets.
- Names explain intent; comments document rationale, invariants and tricky
  tradeoffs, not a narration of obvious syntax. Keep public contracts clear.
- Errors must remain visible and actionable; never swallow failures or use
  silent fallbacks that mask integrity/security faults. Test failure paths.
- Respect existing backend/frontend/service boundaries, contracts and data
  migrations. No circular dependencies or hidden global mutable state.
- Favor composition, explicit dependencies and testable pure logic; avoid
  premature frameworks, custom service locators or abstractions for one use.
- Do not introduce an additional library or linter without a clear need,
  maintenance/license check and comparison with existing tooling.

## Go conventions
- Follow Effective Go, Go Code Review Comments and the Google Go Style Guide
  where relevant. `gofmt` is mandatory; preserve `go vet`/existing checks.
- Prefer clear package ownership and domain-focused types/functions. Avoid
  vague `utils`, `common`, `manager`, `helper`, and `misc` grab-bags.
- Use small interfaces defined at consumers when actually needed, not
  interfaces for every concrete implementation or speculative mocking.
- Pass `context.Context` for cancellable operations; propagate meaningful
  errors with context and preserve errors.Is/As semantics when wrapping.
- Make transactional boundaries, authorization, concurrency and time handling
  obvious and testable. Do not change API/DB semantics merely to split code.
- Keep SQL/data access separate from HTTP transport and domain rules to the
  extent the existing architecture requires; do not duplicate generated code.

## React and TypeScript conventions
- Prefer focused UI components, cohesive hooks for reusable stateful behavior,
  and pure helpers for transformations. Split rendering, data fetching,
  validation and complex state transitions only at meaningful boundaries.
- Honor React Rules of Hooks and component purity. No effectful work during
  render, direct prop/state mutation, accidental dependency cycles or hidden
  duplicated state. Avoid unnecessary effects and memoization by default.
- Prefer explicit typed APIs and predictable error/loading/empty paths;
  avoid `any`, non-null assertions, unchecked casts and brittle giant components
  where a safer existing project pattern is feasible.
- Reuse the existing design system, error patterns, API client and generated
  types; do not add competing patterns while extracting components/hooks.

## Size and complexity: review triggers, NOT arbitrary hard caps
- Human-authored backend Go files around >500 LOC, React/TS/TSX >350 LOC,
  or functions/components >80 LOC deserve explicit cohesion/complexity review;
  >1000 LOC hand-written application files get priority audit.
- Deep nesting (>3-4 conceptual levels), many branches (roughly >15 paths),
  duplicate flows, high fan-in/fan-out and mixed responsibilities are stronger
  warning signals than LOC alone. Use existing lint complexity metrics when
  available; calibrate to baseline, don't blindly enforce default numbers.
- No rule demands splitting a coherent declaration, static table, generated
  output, a test suite, migration, or large schema just to meet a number.
- Prefer meaningful extraction (domain services, cohesive Go files in the same
  package, scoped React components/hooks, pure utilities), not renaming,
  moving lines or multiplying files to game metrics.
- For every justified exception record the technical rationale and owner.
  Do not silently exclude hand-written code or suppress a new regression.

## Permanent PR discipline
- Before implementation: identify module owner, test boundaries, existing
  oversized/complex hotspots being touched, and impact on coupling.
- For NEW code, keep modules and functions readable/cohesive from the start;
  do not add new giant multi-responsibility files or worsen existing hotspots.
- For touched legacy hotspots, improve structure within the PR when safe; for
  broader debt create/prioritize a focused refactoring PR in V2.1.
- Any nontrivial refactor gets behavioral characterization tests first as
  needed, then before/after verification (unit/integration/E2E where relevant).
- Every PR review explicitly assesses readability, coupling, duplication,
  error handling, boundaries, complexity, tests and maintainability impact.
- Add a lightweight repeatable CI check to prevent NEW/worsened structural
  debt; document baseline thresholds and named exceptions transparently.
  Checks must not bypass CI or permit blanket file suppressions.

## V2.1 remediation gate
- Inventory existing authored code; prioritize risk (complexity, churn,
  centrality, user/security impact), not mere sorting by file length.
- Actually refactor and verify significant high-risk code smells and tangled,
  oversized files; a report, TODO list or prettier formatting is not enough.
- Record ranked findings, fixes, PRs, before/after structure and evidence in
  `docs/development/v2-1-maintainability-audit.md`.
- Release-blocking maintainability findings must be FIXED, not relabeled as
  low severity to claim completion. Document justified remaining minor debt.
- Never let cleanup change intended behavior or weaken tests just to pass CI.
