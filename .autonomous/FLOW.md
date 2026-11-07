# Autonomous Flow

cycle=fresh_opencode_session_daily_maintenance
goal=keep_released_tendo_secure_current_and_green
work_unit=small_reviewable_PR
per_invocation=at_most_one_maintenance_change_and_one_release
main=protected
parallel_PRs=avoid

V3 is complete (`.autonomous/V3_COMPLETE`). The V3 cycle steps, Issue
procedure, design-validation discipline and release/completion protocol
were executed; they are preserved verbatim in git history at tag `v3.0.0`
(`.autonomous/FLOW.md`) and are not maintenance procedure.

## Start each cycle
1. Read the AGENTS read set (`always` + matching conditional files), each once.
2. Inspect git status/current branch, origin/main, open PRs, required CI on
   `main`, merge state, open security advisories (`gh api
   repos/bigtcze/tendo/dependabot/alerts`), the Renovate Dependency
   Dashboard (Issue titled "Dependency Dashboard") and the latest published
   release (`gh release view`).
3. If an active Tendo PR exists, finish/review/fix it before unrelated work,
   subject to the priority order below. Never abandon a PR you already
   started without reconciling its state.
4. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
5. Pick the single highest-priority actionable item:
   1. red `main` CI;
   2. security vulnerabilities (advisories, `security`-labelled PRs, audit failures);
   3. open Renovate/dependency PRs;
   4. toolchain/runtime updates;
   5. necessary compatibility fixes.
   Deliver it as one focused PR per "Work", then evaluate "Maintenance release".
6. Exit promptly when nothing is actionable. No feature work, speculative
   refactoring, new coordination frameworks or casual Renovate policy changes.

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
Dependency PRs (human, Renovate or self-opened) are ordinary open PRs and the core maintenance work; priority order is Start step 5.
Routine Renovate PRs never take priority over a red `main` or a security fix; leave them open (or to their native allowlisted automerge) and return afterwards. Security-alert PRs (`security` label) and advisories with a fix are top priority after a red `main`.
A Renovate PR that fails CI or cannot merge because the new version changes behavior is engineering work, not a merge request: read upstream release notes and migration guides, diagnose the real incompatibility and modify Tendo (Go/TypeScript code, API contract and generated clients, npm/Go constraints, toolchain pins and checksums, Docker images/build config, SQL generation, tests) to support it. Never merely retry CI, suppress failures, delete meaningful tests, silently downgrade or merge broken code. When an update is genuinely blocked, follow "Blockers".
Follow `docs/development/dependencies.md` "Dependency PR procedure" for every dependency PR (wrapper-only tooling, release notes, pin sync, root-cause failures, full gate, Renovate branch rules).

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure evidence in the affected PR (or the session report) if cleanup/reporting also fails.

## Context maintenance
read_set=AGENTS (always + conditional triggers); map and rationale in docs/development/agent-context.md
work_status=open_PRs_Renovate_Dependency_Dashboard_and_releases_no_state_file
pr_description=change_release_notes_reviewed_tests_and_gate_evidence
pr_comments=decisions_evidence_blockers_handoff
V3_Issues=closed_historical_never_touched_by_maintenance
LEARNINGS=durable_reusable_rules_only
FLOW=may_improve_process_without_changing_locked_policy
AGENTS=may_improve_navigation_without_changing_locked_policy
docs_ADR=durable_decisions_only

## Blockers
- Do not spin on quota/outage/missing external dependency.
- For a genuinely incompatible update that cannot be safely resolved, record
  the exact technical blocker, evidence (failing check, log excerpt, local
  reproduction) and upstream issue in a comment on that PR; leave it unmerged
  and continue with independent safe work.
- Before working an open PR again, read its existing blocker comments; do not
  re-run or retry an unchanged blocker. Resume only when upstream, the
  dependency version or Tendo code relevant to it has changed.
- Never claim unverified success.

## Maintenance release
Evaluate daily, after the cycle's maintenance change (if any) is merged and
`main` is green.
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
  1. Select the next SemVer version (patch increment for maintenance).
  2. Write user-facing `docs/releases/vX.Y.Z.md`: what changed for
     self-hosters, update steps, compatibility, known limitations; no
     commit dump.
  3. Submit it through one focused protected PR; run the full gate and
     required CI; merge; verify green `main`.
  4. Create an annotated tag on that exact verified `main` commit and push it
     once; the Release workflow runs qualification and publishes.
  5. Verify the GitHub Release (notes, `compose.yaml`, `example.env`) and the
     public `ghcr.io/bigtcze/tendo:vX.Y.Z` manifest for `linux/amd64` and
     `linux/arm64`, pulled anonymously; floating tags match.
  6. If qualification or publication fails, diagnose and fix through a new
     PR and a new patch version. Never move or reuse tags or bypass a failed
     qualification.
- Release evidence goes in the release-notes PR and the session report.
