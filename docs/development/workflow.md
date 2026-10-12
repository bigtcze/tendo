# Development workflow

## Before coding
For behavior/API/schema changes:
- identify the owning module
- identify user-visible behavior/invariant
- identify required unit/integration/E2E/contract coverage
- update OpenAPI/schema/migration in the same slice when required
- create an ADR only for a genuinely durable/non-obvious decision

## Work
1. Create a focused branch from current `main`.
2. Implement the smallest complete vertical slice.
3. Add/update meaningful tests per `docs/specs/TESTING.md`.
4. Run applicable local format/lint/typecheck/unit/integration/contract/E2E/build checks.
5. Independent review is required for every code PR before merge.
6. Fix findings; never hide/suppress failures.
7. Update docs/config examples with behavior.
8. Commit clearly.
9. Push branch.
10. Open/update a PR with behavior + tests summary.
11. Ensure stable `required` CI executes/gates every applicable release-blocking layer introduced by the PR; do not leave tests as local-only commands.
12. Fix failures before merge.
13. Resolve required conversations.
14. Enable auto-merge only when repository rules permit it.
15. After merge: checkout/sync `main`, verify expected content + green main CI, remove stale local branch when safe.

## Architecture discipline
- Do not bypass application services from frontend/integrations.
- Do not introduce generic abstraction until a real use needs it, except locked platform seams.
- Do not duplicate API/domain models manually when generation/derivation exists.
- Before dependency: justify ownership, maintenance, license, and simpler alternatives.
- Keep modular monolith boundaries intact.

## Test discipline
- Tests land with behavior, not later.
- If a test cannot fail for the intended regression, rewrite/remove it.
- Never reduce assertions to accommodate implementation.
- Regression -> reproducing failing test first when practical.
- Time/concurrency/idempotency paths require deterministic tests.

## Dependency maintenance
Dependency PRs (human, Renovate, or developer-opened) are ordinary open PRs; finish open dependency PRs before new feature work.
1. Run every tool through `scripts/dev-exec.sh`; it provisions the checked-out branch's `toolchain.json` pins in the user cache. Never install candidate versions globally, edit shell profiles, or change agent runtimes.
2. Read upstream release notes for every bumped version; for a major, read the migration guide and record behavior changes in `docs/development/dependencies.md`.
3. Keep `toolchain.json` and all pins in sync (`scripts/test_toolchain_manifest.py`). New archive checksums come from the release asset digest.
4. Diagnose each failing required check to root cause: read the failing job log, reproduce locally through the wrapper, then fix code/tests/generated output. Never weaken tests, pin backwards silently, or retry to green.
5. Run the complete gate: `scripts/dev-exec.sh -- bash scripts/verify-local.sh`.
6. Merge only through normal protection; afterwards verify green `main` CI (Work step 15).

Renovate PRs:
- Patch/minor with green required CI: review the diff and release notes, then merge normally.
- Renovate stops updating a branch once it has non-Renovate commits. If a migration needs code changes, push fixes to the Renovate branch only when that PR is the single focused change; otherwise close it and open a focused migration PR that supersedes it, and reference it.
- Do not use rebase/recreate checkboxes or labels to hide failures; a fresh run must pass on its own.
- Do not create schedulers, bots, or Renovate config changes without a separate justified PR.

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what `docs/specs/GUARDRAILS.md` permits.
- Verify version and rerun the originally failing task.
- Do not repeatedly reinstall tools; record durable requirements once in project documentation or the task tracker.
- Preserve the primary failure in the task record if cleanup/reporting also fails.

## Blockers
- Do not spin on quota/outage/missing external dependency.
- Record exact blocker/evidence in the task tracker.
- Choose safe independent V1 work if available.
- Never claim unverified success.
