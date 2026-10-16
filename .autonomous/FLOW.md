# Autonomous Flow

cycle=fresh_opencode_session
goal=advance_highest_value_unproven_V2_DoD
work_unit=small_reviewable_PR
main=protected
parallel_PRs=avoid

## Start each cycle
1. Read context in AGENTS order.
2. Inspect git status/current branch, origin/main, open PRs, required CI, merge state.
3. Reconcile STATE with reality; reality wins.
4. If active Tendo PR exists, finish/review/fix it before unrelated work.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Select smallest high-value unproven V2 GUI/UX/scale DoD slice.

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

## Delegation discipline

- The orchestrator owns triage, dependency order, and final validation. Subagents own bounded work packets, not the whole PR.
- Before delegating review feedback, group findings by owning concern and dependency. Never forward a cross-domain review report wholesale to one writing subagent.
- A writing packet must have one coherent concern, explicit acceptance criteria, and targeted validation. It ends when those checks pass or a new blocker is evidenced.
- Use only one writing subagent at a time in the same worktree. Read-only review and analysis may overlap.
- A worker must not silently absorb newly discovered unrelated or cross-cutting failures. Return exact evidence to the orchestrator for re-triage.
- Consult `oracle` before implementation when a finding changes API/domain contracts, auth or household boundaries, transaction semantics, migration/schema invariants, concurrency/idempotency semantics, or another architecture boundary.
- The orchestrator owns full-suite/release validation after bounded implementation packets. A validation failure is re-triaged to its owning concern; the worker that happened to trigger the suite does not automatically own every failure.
- Keep packets few and dependency-aware. Do not split tightly coupled work merely to maximize agent count.

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

## Dependency maintenance
Dependency PRs (human, Renovate or self-opened) are ordinary open PRs: Start step 4 applies, so finish them before new feature work.
1. Run every tool through `scripts/dev-exec.sh`; it provisions the checked-out branch's `toolchain.json` pins in the user cache. Never install candidate versions globally, edit shell profiles, or change controller/OpenCode runtimes.
2. Read the upstream release notes for every bumped version; for a major, read the migration guide and record behavior changes in `docs/development/dependencies.md`.
3. Keep `toolchain.json` and all pins in sync (`scripts/test_toolchain_manifest.py`). New archive checksums come from the release asset digest.
4. Diagnose each failing required check to root cause: read the failing job log, reproduce locally through the wrapper, then fix code/tests/generated output. Never weaken tests, pin backwards silently, or retry to green.
5. Run the complete gate: `scripts/dev-exec.sh -- bash scripts/verify-local.sh`.
6. Merge only through normal protection; afterwards verify green `main` CI (Work step 15).

Renovate PRs:
- Patch/minor with green required CI: review the diff and release notes, then merge normally.
- Renovate stops updating a branch once it has non-Renovate commits. If a migration needs code changes, push fixes to the Renovate branch only when that PR is the single focused change; otherwise close it and open a focused migration PR that supersedes it, and reference it.
- Do not use rebase/recreate checkboxes or labels to hide failures; a fresh run must pass on its own.
- Do not create schedulers, bots or Renovate config changes without a separate justified PR.

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
- Choose safe independent V2 work if available.
- Never claim unverified success.

## V2 release and completion protocol
Only after every V2_DEFINITION_OF_DONE item is proven on current main:
1. run complete local and required CI/test/release validation;
2. review production screenshots/flows/large-backlog evidence and documentation;
3. via ordinary PR merge `docs/releases/v2.0.0.md` and any release-facing docs;
4. after green main, create annotated v2.0.0 tag on that exact main commit;
5. use the existing release workflow; verify GitHub Release, assets, public GHCR
   image, both architectures, clean install and upgrade with preserved data;
6. if GitHub permissions/visibility block release, record blocker and STOP
   claiming completion; never bypass protection or fabricate evidence;
7. from fresh main create a FINAL completion branch, add V2_COMPLETE with
   evidence and released tag, open PR, pass checks, merge normally;
8. sync clean local main and verify tracked V2_COMPLETE, then exit.

Never create/commit V2_COMPLETE directly on main. Keep V1 PROJECT_COMPLETE.
