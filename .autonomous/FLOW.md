# Autonomous Flow

cycle=fresh_opencode_session
goal=ship_highest_value_unproven_V3_design_DoD
work_unit=small_reviewable_PR
main=protected
parallel_PRs=avoid

## Start each cycle
1. Read context in AGENTS order.
2. Inspect git status/current branch, origin/main, open PRs, required CI, merge state.
3. Reconcile STATE with reality; reality wins.
4. If active Tendo PR exists, finish/review/fix it before unrelated work.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Select the highest-value unmet V3 audit, concept, design-system, UI or UX-validation DoD slice; one focused PR.

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

## Maintainability discipline
- Apply locked CODE_QUALITY policy on EVERY PR, including dependency updates.
- Audit existing source size/cohesion/complexity, track ranked remediation
  with verified tests, and actively refactor high-risk existing hotspots.
- Prefer cohesive boundaries and readability over arbitrary line-count limits;
  preserve contracts and behavior. Guard against NEW/worsened debt in CI.
- Reviewer must consider code health, architecture, coupling, duplicate rules,
  error handling and testability; fix meaningful findings before merge.

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
- Choose safe independent V3 design/UX validation, UI work or regression repair if available.
- Never claim unverified success.

## V2.1 release and completion protocol
Only after every V2_1_DEFINITION_OF_DONE item is proven against current main:
1. Audit the complete behavior/requirement matrix for untested routes, actions,
   permission boundaries, state transitions and unresolved defects. Fill gaps
   or record a strictly justified exclusion; no known release blockers.
   ALSO independently audit every human-authored production source file for
   CODE_QUALITY debt, verify high-risk oversized/tangled modules were ACTUALLY
   refactored via green PRs and the maintainability regression gate is active.
2. Run and record the exhaustive release qualification, full pinned native
   local verification, required GitHub CI, independent QA review, mutation
   evidence, real-browser E2E, production deployment and migration checks.
3. Merge docs/releases/v2.1.0.md and all release-facing documentation via PR.
4. On green main, create annotated v2.1.0 tag at that exact verified commit.
5. Use existing release workflow; verify GitHub Release/assets, public GHCR
   image both architectures, clean install and upgrade with existing data.
6. If any gate or external publication step fails, fix it or record the exact
   blocker; do not claim completion or bypass branch protections.
7. On fresh main, create FINAL completion branch with V2_1_COMPLETE containing
   detailed evidence and the released tag; merge using normal protected PR.
8. Sync clean local main, verify tracked V2_1_COMPLETE and exit successfully.
Preserve both historical completion markers PROJECT_COMPLETE and V2_COMPLETE.
Never add the completion marker directly to main.

## V3 GitHub issue execution (permanent)
- Every cycle read V3_ISSUES_WORKFLOW. Query ONLY Issues that have BOTH the
  exact V3 milestone AND ownership label `tendo:v3-autonomous`. Ignore ALL
  other Issues, even if they are in the V3 milestone or have V3-like titles.
  Never auto-adopt an outside Issue by applying the ownership label.
- Only agents creating NEW Issues may attach this label at creation time.
  Reconcile owned comments, claims, dependencies, linked PRs and required CI.
  Never re-create STATE.md.
- Work only on a claimed managed Issue; comment claim with stable identity, change
  idle -> in-progress, WAIT 3–5 seconds and re-read full issue/comment history.
  First still-active server-ordered claim wins, otherwise RELEASE and pick
  another ready issue. One focused PR at a time, no separate coordinator.
- Record tests/screenshots/decisions/blocked reasons only in owned Issue
  comments and linked PRs. Before every mutation recheck BOTH ownership
  label and milestone; close only after green merged main and proof.
- For release/finish, close V3-18 only after confirmed published public
  v3.0.0 and both GHCR platforms; then V3-19 covers V3_COMPLETE final PR.

## V3 design and visual validation discipline
- Before changing visuals, capture evidence of the real current UI on actual
  product routes with real build and representative synthetic household data.
- Explore two coherent implemented design concepts; independent critique,
  comparison and documented choice BEFORE large-scale redesign rollout.
- For each UI PR, assess authentic before/after browser screenshots for phone
  and desktop, Czech/English, dark/light, empty/populated/error states and
  accessibility. Fix design problems before merge; do not just "approve" CSS.
- Evaluate daily-user journeys: few understandable actions, no technical
  jargon, readable type, clear hierarchy and forgiving interactions.
- Commit stable deterministic visual regression baselines for relevant routes,
  viewports and themes; independently review legitimate screenshot updates.
- Apply CODE_QUALITY to every frontend slice. Preserve backend, auth, API,
  recurrence and V2.1 regression checks without weakening coverage.

## V3 release and completion protocol
Only after all agent-managed milestone Issues with `tendo:v3-autonomous`
and V3_DEFINITION_OF_DONE are fully and truthfully proven on green main:
1. Independently review the real implementation against V3_DESIGN_BRIEF,
   route/state inventory, mobile/tablet/desktop, actual screenshots,
   localization, contrast, keyboard and beginner household journeys.
2. Run complete pinned local verification, required CI, production-built
   Playwright E2E, visual regression and deployment/upgrade checks. Fix all
   release-blocking issues. Never skip or weaken existing V2.1 gates.
3. Update README with authentic committed v3.0.0 Playwright screenshot(s),
   user/docs version references and quality release-facing documentation.
4. Merge professionally written docs/releases/v3.0.0.md via protected PR.
   Notes must explain what visually changed, why users benefit, install/
   upgrade path, backwards compatibility, limitations and tested artifacts.
5. Only when main has green `required`, create annotated v3.0.0 tag pointing
   to that EXACT verified main commit, push once, trigger existing Release
   workflow. NEVER move a tag or bypass the normal publishing pipeline.
6. Verify GitHub Release v3.0.0 exists publicly with proper notes and exact
   compose.yaml/example.env; verify public ghcr.io/bigtcze/tendo:v3.0.0
   image manifest includes linux/amd64 and linux/arm64 and floating tags
   match release; verify released clean install AND upgrade from v2.1.0
   retaining existing household data.
7. If release workflow, GitHub visibility, artifact or upgrade fails, diagnose
   and FIX before completion. Record genuine external blockers and STOP
   claiming completion; do not fake a release.
8. From fresh main, create separate FINAL completion branch, add
   .autonomous/V3_COMPLETE documenting release and evidence; open PR, run
   required checks, merge under protection.
9. Sync clean local main and verify V3_COMPLETE tracked and true; exit.
Keep PROJECT_COMPLETE, V2_COMPLETE and V2_1_COMPLETE as historical markers.
Use ONLY V3 milestone Issues carrying `tendo:v3-autonomous`, not STATE.md,
for V3 progress, blockers and claims. Ignore unlabelled/external Issues.
Never directly write completion to main.
