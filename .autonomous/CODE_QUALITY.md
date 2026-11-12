# Code quality and maintainability policy

status=canonical_version_independent
source_of_truth=this_file
applies=all_future_changes
sources=go.dev/wiki/CodeReviewComments, google.github.io/styleguide/go/guide.html, react.dev/reference/rules, eslint.org/docs/latest/rules
historical_evidence=docs/development/v2-1-maintainability-audit.md (V2.1 findings; read-only history, not an ongoing authority)

## Core engineering principles
- Optimize for future maintainers: readable, correct, cohesive, explicit code with minimal moving parts.
- Prefer straightforward idiomatic code over clever abstractions, design-pattern ceremony, deep indirection or micro-files.
- Single responsibility means one cohesive reason to change, not one line or one method per file. Boundaries follow the actual domain and usage.
- Minimize duplication of business rules and state transitions. Centralize authoritative logic without creating generic, context-free helper buckets.
- Names explain intent. Comments document rationale, invariants and tradeoffs, not obvious syntax. Public contracts stay clear.
- Errors stay visible and actionable. Never swallow failures or use silent fallbacks that mask integrity or security faults. Test failure paths.
- Respect backend, frontend and service boundaries, contracts and data migrations. No circular dependencies and no hidden global mutable state.
- Favor composition, explicit dependencies and testable pure logic. Avoid premature frameworks, custom service locators and abstractions for a single use.
- Do not add a library or linter without a clear need, a maintenance and license check, and a comparison with existing tooling.

## Go conventions
- Follow Effective Go, Go Code Review Comments and the Google Go Style Guide where relevant. `gofmt` is mandatory. Keep `go vet` and existing checks.
- Clear package ownership and domain-focused types and functions. Avoid vague `utils`, `common`, `manager`, `helper` and `misc` grab-bags.
- Small interfaces defined at the consumer, only when actually needed. No interface for every implementation and no speculative mocking.
- Pass `context.Context` to cancellable operations. Propagate errors with context. Preserve `errors.Is` and `errors.As` semantics when wrapping.
- Make transactional boundaries, authorization, concurrency and time handling obvious and testable. Do not change API or DB semantics merely to split code.
- Keep SQL and data access separate from HTTP transport and domain rules, to the extent the architecture requires (ARCHITECTURE "Backend module boundaries"). Do not duplicate generated code.

## React and TypeScript conventions
- Prefer focused UI components, cohesive hooks for reusable stateful behavior, and pure helpers for transformations. Split rendering, data fetching, validation and complex state transitions only at meaningful boundaries.
- Honor the React Rules of Hooks and component purity. No effectful work during render, no direct prop or state mutation, no accidental dependency cycles, no hidden duplicated state. Avoid unnecessary effects and default memoization.
- Prefer explicit typed APIs and predictable error, loading and empty paths. Avoid `any`, non-null assertions, unchecked casts and giant components where a safer existing project pattern is feasible.
- Reuse the existing design system, error patterns, API client and generated types. Do not add competing patterns while extracting components or hooks.
- The React layer contains presentation and client state only. Recurrence, authorization and date-policy rules stay in the backend (ARCHITECTURE "Frontend").

## Size and complexity: review triggers, not hard caps
- Hand-written Go files over about 500 LOC, React/TS/TSX files over about 350 LOC, or functions and components over about 80 LOC deserve an explicit cohesion and complexity review. Hand-written application files over 1000 LOC get priority audit.
- Deep nesting (more than 3–4 conceptual levels), many branches (roughly more than 15 paths), duplicated flows, high fan-in or fan-out, and mixed responsibilities are stronger warning signals than LOC alone. Use existing lint complexity metrics where available, and calibrate to the baseline instead of blindly enforcing defaults.
- No rule demands splitting a coherent declaration, static table, generated output, test suite, migration or large schema just to meet a number.
- Prefer meaningful extraction: domain services, cohesive Go files in the same package, scoped React components and hooks, pure utilities. Do not rename, move lines or multiply files to game metrics.
- Each justified exception records its technical rationale and owner. Do not silently exclude hand-written code or suppress a new regression.

## Permanent PR discipline
- Before implementation, identify the module owner, the test boundaries, any existing oversized or complex hotspots being touched, and the impact on coupling.
- New code stays readable and cohesive from the start. Do not add new giant multi-responsibility files or worsen existing hotspots.
- For touched legacy hotspots, improve the structure inside the PR when it is safe. For broader debt, create or prioritize a focused refactoring PR.
- Any nontrivial refactor gets behavioral characterization tests first where needed, then before-and-after verification (unit, integration and E2E where relevant).
- Every PR review explicitly assesses readability, coupling, duplication, error handling, boundaries, complexity, tests and maintainability impact.
- Maintainability regression check: a lightweight repeatable CI check prevents NEW or worsened structural debt. Document baseline thresholds and named exceptions transparently. The check must not bypass CI or permit blanket file suppressions.

## Structural debt remediation
- Inventory existing authored code by risk: complexity, churn, centrality and user or security impact. Do not sort by file length alone.
- Significant high-risk smells and tangled oversized files are actually refactored and verified through reviewed PRs. A report, TODO list or formatting pass is not enough.
- Release-blocking maintainability findings are fixed, not relabeled as low severity to claim completion. Remaining minor debt is documented with a justification.
- Cleanup never changes intended behavior and never weakens tests to pass CI.
- Existing contracts, migrations and user outcomes are preserved during refactoring (TESTING.md "Quality bar").

## Links
- Test strength and layers: TESTING.md
- Release gates including maintainability: DEFINITION_OF_DONE.md "Tests and quality"
- Architecture boundaries: ARCHITECTURE.md
