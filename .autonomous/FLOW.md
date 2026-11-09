# Autonomous Flow

cycle=fresh_opencode_session_per_V4_controller_cycle
goal=ship_v4_modern_everyday_via_managed_V4_Issues
controller=scripts/autonomous-loop.sh(tendo-autonomous.service)+.autonomous/V4_CYCLE_PROMPT.md
work_unit=small_reviewable_PR
per_cycle=at_most_one_claimed_managed_V4_Issue
main=protected
parallel_PRs=avoid

V3 is complete (`.autonomous/V3_COMPLETE`); its cycle, Issue procedure and
release/completion protocol are preserved at tag `v3.0.0`
(`.autonomous/FLOW.md`). Post-V3 maintenance (the daily
`tendo-maintenance.timer`, outside this repository) is paused while V4 runs:
the V4 controller refuses to start while that timer or service is enabled or
active, and holds the maintenance flock during every cycle. Dependency,
security and red-`main` work continues inside the V4 cycle (priority below).
Maintenance releases are not cut during V4; security fixes ship in v4.0.0 or,
if urgent, through a patch release per "Maintenance release".

## Start each cycle
1. Read the AGENTS read set (`always` + matching conditional files), each once.
2. Inspect git status/current branch, origin/main, open PRs, required CI on
   `main`, merge state, open security advisories (`gh api
   repos/bigtcze/tendo/dependabot/alerts`), the Renovate Dependency
   Dashboard (Issue titled "Dependency Dashboard") and the latest published
   release (`gh release view`).
3. Query open managed V4 Issues with BOTH the exact milestone AND label:
   `gh issue list --milestone "Tendo v4.0.0 - Modern Everyday" --label tendo:v4-autonomous --state open`;
   reconcile their claims, comments, dependencies ("Blocked by"), linked PRs
   and CI with reality (reality wins). Never treat other Issues as work items.
4. If an active Tendo PR exists, finish/review/fix it before unrelated work.
   Never abandon a PR you already started without reconciling its state.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Pick the single highest-priority actionable item:
   1. red `main` CI;
   2. security vulnerabilities (advisories, `security`-labelled PRs, audit failures);
   3. the highest-value dependency-ready managed `status:idle` V4 Issue
      (every "Blocked by" Issue closed), claimed per "V4 GitHub issue execution";
   4. open Renovate/dependency PRs that do not conflict with active V4 work.
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
Dependency PRs (human, Renovate or self-opened) are ordinary open PRs; priority order is Start step 6.
Routine Renovate PRs never take priority over a red `main` or a security fix; leave them open (or to their native allowlisted automerge) and return afterwards. Security-alert PRs (`security` label) and advisories with a fix are top priority after a red `main`.
A Renovate PR that fails CI or cannot merge because the new version changes behavior is engineering work, not a merge request: read upstream release notes and migration guides, diagnose the real incompatibility and modify Tendo (Go/TypeScript code, API contract and generated clients, npm/Go constraints, toolchain pins and checksums, Docker images/build config, SQL generation, tests) to support it. Never merely retry CI, suppress failures, delete meaningful tests, silently downgrade or merge broken code. When an update is genuinely blocked, follow "Blockers".
Follow `docs/development/dependencies.md` "Dependency PR procedure" for every dependency PR (wrapper-only tooling, release notes, pin sync, root-cause failures, full gate, Renovate branch rules).

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure evidence in the owned V4 Issue or the affected PR if cleanup/reporting also fails.

## Context maintenance
read_set=AGENTS (always + conditional triggers); map and rationale in docs/development/agent-context.md
work_status=managed_V4_Issues_plus_open_PRs_and_Renovate_Dependency_Dashboard_no_state_file
issue_body=stable_acceptance_contract_update_when_scope_changes
issue_comments=claims_decisions_evidence_blockers_PR_links_handoff
pr_description=change_reviewed_tests_and_gate_evidence
V3_Issues=closed_historical_never_touched
LEARNINGS=durable_reusable_rules_only
FLOW=may_improve_process_without_changing_locked_policy
AGENTS=may_improve_navigation_without_changing_locked_policy
docs_ADR=durable_decisions_only

## Blockers
- Do not spin on quota/outage/missing external dependency.
- For a managed V4 Issue: record the exact blocker and evidence in a comment
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

## V4 GitHub issue execution
Locked V4_ISSUES_WORKFLOW is the canonical rule set (ownership filter, claim,
collision, status labels, closure, finalization). Procedure reminders:
- Every query and mutation filters BOTH milestone `Tendo v4.0.0 - Modern Everyday`
  AND label `tendo:v4-autonomous`; recheck both immediately before each mutation.
- Claim: comment `CLAIM agent=<identity> time=<UTC>`, swap `status:idle` for
  `status:in-progress`, wait 3-5 s, re-read labels, milestone and all
  comments; earliest active claim (server timestamp, then comment ID) wins.
- New managed Issue (verified V4 blocker only):
  `gh issue create --milestone "Tendo v4.0.0 - Modern Everyday" --label tendo:v4-autonomous --label status:idle`.
  Never label an existing Issue.
- Link PRs with `Closes #N` only for a fully satisfied managed Issue. Close it
  only after merge, verified green `main` and a DONE comment with evidence.
- V4-16 (two-service Compose) blocks V4-13; V4-13 blocks V4-14; V4-14 blocks
  V4-15 (`V4_COMPLETE`).

## V4 design and visual validation
- Before UI, token, rendering or visual-baseline work, read tracked
  `.autonomous/V4_DESIGN_BRIEF.md`, view both `docs/design/reference/tendo-v4-*.png`
  and verify their SHA-256 against the brief header. They are visual targets
  only; never commit them or derivatives as product screenshots.
- Validate with real production Playwright captures (fixture, widths, themes
  and side-by-side review per the brief's "Required visual comparison").
  Major deviations need human approval, recorded on the owned Issue.
- Keep the V3 visual-regression gate meaningful: baseline updates are
  intentional, reviewed and explained in the PR.

## V4 release and completion
V4-14 releases v4.0.0 per "Maintenance release" procedure steps 1-6 (major
version, published two-service Compose) and V4_DEFINITION_OF_DONE; V4-15 merges
`.autonomous/V4_COMPLETE` through a separate final protected PR, after which
the controller exits. Preserve all earlier markers and tags.

## Maintenance release
Not evaluated by V4 cycles; it applies again after `V4_COMPLETE` and for an
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
