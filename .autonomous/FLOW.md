# Autonomous Flow

cycle=fresh_opencode_session
goal=ship_highest_value_unproven_V3_design_DoD
work_unit=small_reviewable_PR
main=protected
parallel_PRs=avoid

## Start each cycle
1. Read the AGENTS read set (`always` + matching conditional files), each once.
2. Inspect git status/current branch, origin/main, open PRs, required CI, merge state.
3. Read locked V3_ISSUES_WORKFLOW. Query open managed Issues with BOTH the
   exact V3 milestone AND label `tendo:v3-autonomous`, e.g.
   `gh issue list --milestone "Tendo v3.0.0 - Everyday Elegance" --label tendo:v3-autonomous --state open`;
   reconcile their claims, comments, dependencies, linked PRs and CI with
   reality (reality wins). Never treat other Issues as work items.
4. If active Tendo PR exists, finish/review/fix it before unrelated work.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Select the highest-value ready managed `status:idle` Issue (prerequisites
   closed), claim it per V3_ISSUES_WORKFLOW, then deliver one focused PR.

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
Exception: routine Renovate PRs never take priority over an open managed `[P0]` Issue or a red `main`. Fix P0 first; leave routine PRs open (or to their native allowlisted automerge) and return to them afterwards. Security-alert PRs (`security` label) are P0 work. Never abandon a PR you already started without reconciling its state.
Follow `docs/development/dependencies.md` "Dependency PR procedure" for every dependency PR (wrapper-only tooling, release notes, pin sync, root-cause failures, full gate, Renovate branch rules).

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure evidence in the owned Issue if cleanup/reporting also fails.

## Context maintenance
read_set=AGENTS (always + conditional triggers); map and rationale in docs/development/agent-context.md
work_status=managed_V3_Issues_only_no_state_file
issue_body=stable_acceptance_contract_update_when_scope_changes
issue_comments=claims_decisions_evidence_blockers_PR_links_handoff
LEARNINGS=durable_reusable_rules_only
FLOW=may_improve_process_without_changing_locked_policy
AGENTS=may_improve_navigation_without_changing_locked_policy
docs_ADR=durable_decisions_only

## Blockers
- Do not spin on quota/outage/missing external dependency.
- Record exact blocker/evidence in a comment on the owned managed Issue and
  set `status:blocked` (recheck milestone + ownership label first).
- Choose safe independent V3 design/UX validation, UI work or regression repair if available.
- Never claim unverified success.

## V3 GitHub issue execution (permanent)
Locked V3_ISSUES_WORKFLOW is the single canonical rule set (ownership filter,
new-Issue creation, claim/collision, status labels, closure, V3-18/V3-19
gates); read it every cycle and apply it exactly. Procedure reminders:
- Every query and mutation filters BOTH milestone `Tendo v3.0.0 - Everyday Elegance`
  AND label `tendo:v3-autonomous`; recheck both immediately before each mutation.
- New Issue: `gh issue create --milestone "Tendo v3.0.0 - Everyday Elegance" --label tendo:v3-autonomous --label status:idle`.
  Never label an existing Issue.
- Evidence (tests, screenshots, decisions, blockers) goes only in owned Issue
  comments and linked PRs; never STATE.md or a substitute.

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
