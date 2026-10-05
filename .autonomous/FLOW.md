# Autonomous Flow

cycle=fresh_opencode_session
goal=advance_highest_value_unproven_DoD
work_unit=small_reviewable_PR
main=protected
parallel_PRs=avoid

## Start each cycle
1. Read context in AGENTS order.
2. Inspect git status/current branch, origin/main, open PRs, required CI, merge state.
3. Reconcile STATE with reality; reality wins.
4. If active Tendo PR exists, finish/review/fix it before unrelated work.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Select smallest high-value unproven V1 behavior/DoD slice.

## Before coding
For behavior/API/schema changes:
- identify owning module
- identify user-visible behavior/invariant
- identify required unit/integration/E2E/contract coverage
- update OpenAPI/schema/migration in same slice when required
- create ADR only for genuinely durable/non-obvious decision

## Work
1. Create focused branch from current main.
2. Implement smallest complete vertical slice.
3. Add/update meaningful tests per TESTING.
4. Run applicable local format/lint/typecheck/unit/integration/contract/E2E/build checks.
5. For every code PR, run an independent reviewer subagent when the controller provides one; review test quality, auth boundary, API contract, architecture boundary, UX simplicity. If no reviewer tool is available, perform and record the same checklist explicitly before merge.
6. Fix findings; never hide/suppress failures.
7. Update docs/config examples with behavior.
8. Commit clearly.
9. Push branch.
10. Open/update PR with behavior + tests summary.
11. Ensure stable `required` CI actually executes/gates every applicable release-blocking layer introduced by the PR; do not leave tests as local-only commands.
12. Fix failures before merge.
13. Resolve required conversations.
14. Enable auto-merge only when repository rules permit.
15. After merge: checkout/sync main, verify expected content + green main CI, remove stale local branch when safe.

## Architecture discipline
- Do not bypass application services from frontend/integrations.
- Do not introduce generic abstraction until a real use needs it, except locked platform seams.
- Do not duplicate API/domain models manually when generation/derivation exists.
- Before dependency: justify ownership, maintenance, license, simpler alternatives.
- Keep modular monolith boundaries intact.

## Test discipline
- Tests land with behavior, not later.
- If a test cannot fail for intended regression, rewrite/remove it.
- Never reduce assertions to accommodate implementation.
- Regression -> reproducing failing test first when practical.
- Time/concurrency/idempotency paths require deterministic tests.

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure in STATE if cleanup/reporting also fails.

## Context maintenance
STATE=replace_stale_snapshot_concise_facts_only
LEARNINGS=durable_reusable_rules_only
FLOW=may_improve_process_without_changing_locked_policy
AGENTS=may_improve_navigation_without_changing_locked_policy
docs_ADR=durable_decisions_only

## Blockers
- Do not spin on quota/outage/missing external dependency.
- Record exact blocker/evidence in STATE.
- Choose safe independent V1 work if available.
- Never claim unverified success.

## Completion protocol
Only after every DoD item is proven on current `main`:
1. run complete clean CI/test/release validation;
2. verify docs/config examples against actual release;
3. create final branch from main;
4. add `.autonomous/PROJECT_COMPLETE` with V1 completion evidence/commit;
5. open final PR and pass normal required checks;
6. merge through protection;
7. checkout/pull main and verify completion file is tracked there;
8. end cleanly.

Never create/commit PROJECT_COMPLETE directly on main.
