# Autonomous Flow

cycle=fresh_opencode_session_per_V5_controller_cycle
goal=ship_v5_refined_reliable_via_managed_V5_Issues
controller=scripts/autonomous-loop.sh(tendo-autonomous.service)+.autonomous/V5_CYCLE_PROMPT.md+scripts/v5_work_selection.py
milestone=Tendo v5.0.0 - Refined & Reliable
managed_issue_label=tendo:v5-autonomous
work_unit=small_reviewable_PR
per_cycle=at_most_one_claimed_managed_V5_Issue_or_one_dependency_PR
main=protected
parallel_PRs=avoid

V4 is complete (`.autonomous/V4_COMPLETE`, v4.0.0); its cycle, Issue
procedure and release/completion protocol are preserved at tag `v4.0.0`
(`.autonomous/FLOW.md`). `V4_COMPLETE` and every older marker are historical
and never stop or steer the V5 controller.

## V5 controller and maintenance handoff
- `tendo-autonomous.service` runs `scripts/autonomous-loop.sh` in the
  canonical checkout; every cycle is one fresh `opencode run --agent
  orchestrator` session whose prompt is the byte-exact
  `.autonomous/V5_CYCLE_PROMPT.md` followed by the controller's advisory work
  selection (`scripts/v5_work_selection.py`, the priority below computed from
  live GitHub state). Re-verify live state before acting on it.
- The loop takes the post-release maintenance flock
  `$XDG_RUNTIME_DIR/tendo-maintenance-$UID.lock` (the one
  `~/.local/bin/tendo-maintenance.sh` takes) before its first cycle and holds
  it until it exits. `tendo-maintenance.timer` may stay enabled: a scheduled
  maintenance run finds the lock held and exits "already running". A
  maintenance run that already holds the lock is real competing work, so the
  loop refuses to start (exit 3, no session). Unit enabled/active state is not
  consulted.
- The loop exits 0 only when `.autonomous/V5_COMPLETE` is tracked on clean
  `main`, which releases the lock; the next scheduled maintenance run then
  resumes post-V5 maintenance without manual reconfiguration.
- Behavior tests: `bash scripts/autonomous-loop-test.sh` (local gate and
  required CI) runs the real loop and `scripts/test_v5_work_selection.py`.
- The loop, selection policy and systemd units are controller configuration
  (AGENTS `must_not_self_change`); change them only through a human-authorized
  governance PR.

## Start each cycle
1. Read the AGENTS read set (`always` + matching conditional files), each once.
2. Inspect git status/current branch and uncommitted work, origin/main,
   ALL open PRs including bot/Renovate PRs (`gh pr list --state open`; never
   filter by V5 Issue), their CI and merge state, required CI on `main`, open
   security advisories (`gh api repos/bigtcze/tendo/dependabot/alerts`), the
   Renovate Dependency Dashboard (Issue titled "Dependency Dashboard"), recent
   merged PR history and the latest published release (`gh release view`).
3. Query owned V5 Issues with BOTH the exact milestone AND label:
   `gh issue list --milestone "Tendo v5.0.0 - Refined & Reliable" --label tendo:v5-autonomous --state all`;
   reconcile their claims, comments, dependencies ("Blocked by"), linked PRs
   and CI with reality (reality wins). Never treat other Issues as work items.
4. Reconcile an active PR first: finish/review/fix it before unrelated work.
   Never abandon a PR you already started without reconciling its state.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Pick the single highest-priority actionable item (locked
   V5_ISSUES_WORKFLOW order; `scripts/v5_work_selection.py` computes it and
   the controller appends its result to the prompt):
   a. red `main` CI;
   b. urgent security: open `security`-labelled PR, or an open advisory with a
      patched version;
   c. the active PR (one that `Refs` an open owned V5 Issue, or a Renovate PR
      already carrying agent repair commits), or an owned Issue left
      `status:in-progress` without an open PR;
   d. any untriaged Renovate PR older than 48 h (oldest first) — triage it
      this cycle (see "Renovate triage and fairness");
   e. an actionable dependency PR when at least TWO feature PRs merged since
      the last merged dependency PR;
   f. the highest-value dependency-ready `status:idle` owned V5 Issue (every
      "Blocked by" Issue closed); recurrence Issues rank first (P0), then by
      V5 code; claimed per "V5 GitHub issue execution";
   g. remaining actionable dependency PRs.
   Deliver it as one focused PR per "Work", then exit.

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
4. Run applicable focused checks while iterating, then the complete gate before opening or merging: `bash scripts/dev-exec.sh -- bash scripts/verify-local.sh`.
5. For every code or dependency PR, run an independent reviewer subagent when the controller provides one; review the diff, upstream release notes against Tendo usage, regression coverage, test quality, auth boundary, API contract, architecture boundary, UX simplicity. If no reviewer tool is available, perform and record the same checklist explicitly before merge.
6. Fix findings; never hide/suppress failures.
7. Update docs/config examples with behavior.
8. Commit clearly.
9. Push branch.
10. Open/update PR with behavior + tests summary.
11. Ensure stable `required` CI actually executes/gates every applicable release-blocking layer introduced by the PR; do not leave tests as local-only commands.
12. Fix failures before merge.
13. Resolve required conversations.
14. Merge only through normal protection once `required` is green on the current branch (repository auto-merge is disabled; Renovate's own allowlisted PR automerge is unaffected).
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
- Maintenance does not start refactors. Code changed for a dependency
  migration follows CODE_QUALITY "Permanent PR discipline" (improve touched
  hotspots only when safe and in scope of the migration).
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
Dependency PRs (human, Renovate or self-opened) are ordinary open PRs; their
priority is Start step 6. They are never ignored because V5 feature Issues
remain open. Routine Renovate PRs never take priority over a red `main` or a
security fix.
A Renovate PR that fails CI or cannot merge because the new version changes behavior is engineering work, not a merge request: read upstream release notes and migration guides, diagnose the real incompatibility and modify Tendo (Go/TypeScript code, API contract and generated clients, npm/Go constraints, toolchain pins and checksums, Docker images/build config, SQL generation, tests) to support it. Never merely retry CI, suppress failures, delete meaningful tests, silently downgrade or merge broken code. When an update is genuinely blocked, follow "Blockers".
Follow `docs/development/dependencies.md` "Dependency PR procedure" for every dependency PR (wrapper-only tooling, release notes, pin sync, root-cause failures, full gate, Renovate branch rules).

### Renovate triage and fairness
All state is GitHub state; there is no counter file, queue or cache.
- Triage record: a PR comment whose FIRST line is exactly
  `TRIAGE agent=<identity> time=<UTC> decision=<actionable|blocked> target=<exact current PR title>`
  followed by the evidence (release notes read, compatibility/migration
  needs, checks run, blocker and upstream link when `blocked`). The latest
  such comment counts only while `target` equals the current PR title (the
  title names dependency and version); a new version needs triage again.
- Aged: a Renovate PR open longer than 48 h without a valid triage record is
  priority d. Triage is time-bounded and is not an exemption: an `actionable`
  PR is then processed to merge in the dependency slot (e).
- Dependency slot: count merged PRs on `main` since the most recent merged
  dependency PR (Renovate author, `renovate/` branch or `dependencies`/
  `renovate` label); every other merged PR counts as a feature PR. At two or
  more, the next cycle processes the oldest actionable dependency PR.
  Merging it resets the count.
- `blocked` with an unchanged target is skipped until the title (version),
  upstream facts or relevant Tendo code change; do not re-run an unchanged
  blocker. Record blockers on the PR, never close it without evidence.
- PRs #404 (Vite), #405 (go-jose) and #406 (Playwright) were open at V5
  authorization; reconcile them against live state like any other PR (V5-01).
  Renovate PRs never receive the V5 ownership label.

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure evidence in the owned V5 Issue or the affected PR if cleanup/reporting also fails.

## Context maintenance
read_set=AGENTS (always + conditional triggers); map and rationale in docs/development/agent-context.md
work_status=managed_V5_Issues_plus_open_PRs_PR_triage_comments_and_Renovate_Dependency_Dashboard_no_state_file
issue_body=stable_acceptance_contract_update_when_scope_changes
issue_comments=claims_decisions_evidence_blockers_PR_links_handoff
pr_description=change_reviewed_tests_and_gate_evidence
V3_V4_Issues=closed_historical_never_touched
LEARNINGS=durable_reusable_rules_only
FLOW=may_improve_process_without_changing_locked_policy
AGENTS=may_improve_navigation_without_changing_locked_policy
docs_ADR=durable_decisions_only

## Blockers
- Do not spin on quota/outage/missing external dependency.
- For a managed V5 Issue: record the exact blocker and evidence in a comment
  on it and set `status:blocked` (recheck milestone + ownership label first);
  continue with independent safe work.
- For a genuinely incompatible update that cannot be safely resolved, record
  the exact technical blocker, evidence (failing check, log excerpt, local
  reproduction) and upstream issue in a comment on that PR; leave it unmerged
  and continue with independent safe work.
- Before working an open PR again, read its existing blocker comments; do not
  re-run or retry an unchanged blocker. Resume only when upstream, the
  dependency version or Tendo code relevant to it has changed.
- Never claim unverified success.

## V5 GitHub issue execution
Locked V5_ISSUES_WORKFLOW is the canonical rule set (ownership filter, claim,
collision, status labels, closure, finalization). Procedure reminders:
- Every query and mutation filters BOTH milestone `Tendo v5.0.0 - Refined & Reliable`
  AND label `tendo:v5-autonomous`; recheck both immediately before each
  mutation, and STOP if either selector disappears.
- Each open owned Issue carries exactly one of `status:idle`,
  `status:in-progress`, `status:blocked`.
- Claim (only idle, dependency-ready Issues): comment
  `CLAIM agent=<identity> time=<UTC>`, swap `status:idle` for
  `status:in-progress`, wait 3-5 s, re-read labels, milestone and all
  comments; earliest active claim (server timestamp, then comment ID) wins;
  the loser releases.
- New managed Issue (verified V5 defect only):
  `gh issue create --milestone "Tendo v5.0.0 - Refined & Reliable" --label tendo:v5-autonomous --label status:idle`.
  Never label an existing Issue.
- Reference the managed Issue from its PR with `Refs #N`, never `Closes`/
  `Fixes`/`Resolves` (GitHub would auto-close it at merge, before main CI is
  verified). Close it explicitly only after merge, verified green `main` and a
  DONE comment with evidence (milestone + label rechecked).
- Dependency order is each Issue body's "Blocked by" line. V5-16 (release)
  blocks V5-17 (`V5_COMPLETE`).

## V5 recurrence priority
Recurrence is release-critical (V5_RECURRENCE_CONTRACT, P0): its Issues rank
first among ready feature work. Reproduce the same-date monthly Done failure
with a failing regression test before changing implementation; one canonical
application service for UI/API/MCP; required-CI coverage at pure, real
PostgreSQL, browser and MCP layers.

## V5 design and visual validation
- Before UI, token, rendering or visual-baseline work, read
  `.autonomous/V5_DESIGN_BRIEF.md`. The V4 reference PNGs and earlier briefs
  are historical, never V5 targets or evidence.
- Evidence is real production-build browser captures (widths, CZ/EN, themes
  per the brief); never claim human approval without a recorded human action.
- Keep the visual-regression gate meaningful: baseline updates are
  intentional, reviewed and explained in the PR.

## V5 release and completion
V5-16 releases v5.0.0 per "Maintenance release" procedure steps 1-6 (major
version, published two-service Compose, populated v4.0.0 upgrade) and
V5_DEFINITION_OF_DONE; V5-17 merges `.autonomous/V5_COMPLETE` through a
separate final protected PR, after which the controller exits and releases the
maintenance lock. Preserve all earlier markers and tags.

## Maintenance release
Not evaluated by V5 cycles; it applies again after `V5_COMPLETE` and for an
urgent security patch. Evaluate on green `main`, after any change has merged.
- Interval: normally one maintenance patch release every 14 days, measured
  from the latest published release's `publishedAt`.
- Release only when meaningful dependency, security or compatibility changes
  merged since the latest release tag (`gh api
  repos/bigtcze/tendo/compare/<tag>...main`): changes to what self-hosters
  run or how it is built (backend, frontend, API, migrations, runtime/build
  toolchain, Dockerfiles, `compose.yaml`, `.env.example`). Never publish an
  empty or documentation-only release; CI-, test- or dev-tool-only bumps
  alone do not justify one.
- A merged security fix is released promptly without waiting for the interval.
- SemVer: maintenance releases are patch releases. Anything that needs manual
  self-hoster action or breaks `/api/v1` clients is not a patch
  (`docs/development/release.md`); do not hide breaking behavior behind a
  patch version.
- Procedure (`docs/development/release.md` "Cut a release" is canonical):
  1. Select the next SemVer version (patch increment for maintenance). It
     must be higher than every existing `v*` tag (`git ls-remote --tags
     origin`), including tags whose release failed; a failed tag is never
     reused.
  2. Write user-facing `docs/releases/vX.Y.Z.md`: what changed for
     self-hosters, update steps, compatibility, known limitations; no
     commit dump.
  3. Submit it through one focused protected PR; run the full gate and
     required CI; merge; verify green `main`.
  4. Create an annotated tag on that exact verified `main` commit and push it
     once; the Release workflow runs qualification and publishes.
  5. Verify the GitHub Release (notes, `compose.yaml`, `example.env`) and the
     public `ghcr.io/bigtcze/tendo:vX.Y.Z` manifest for `linux/amd64` and
     `linux/arm64`, pulled anonymously; floating tags match. Verify a clean
     install from the published release assets and an in-place upgrade from
     the previous published release that preserves existing household data
     (`docs/development/release-qualification.md`, published-artifact row).
  6. If qualification or publication fails, diagnose and fix through a new
     PR and a new patch version. Never move or reuse tags or bypass a failed
     qualification.
- Release evidence goes in the release-notes PR and the session report.
