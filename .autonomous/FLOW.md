# Autonomous Flow

cycle=fresh_opencode_session_per_autonomy_controller_cycle
goal=continuous_release_dependency_security_and_CI_maintenance_via_GitHub
controller=scripts/autonomy-controller.sh(tendo-autonomous.service)+.autonomous/AUTONOMY_PROMPT.md+scripts/autonomy-work-selection.py
owned_issue_label=release
work_unit=small_reviewable_PR
per_cycle=at_most_one_selected_work_item(one_owned_release_Issue_or_one_PR_or_one_patch_release)
main=protected
parallel_PRs=avoid

The controller is evergreen: it serves every future release, dependency,
security and patch-release cycle without per-version prompts, labels or code.
Completed stages (V1-V4, and V5 once released) are historical; their
completion markers (`*_COMPLETE`) are evidence only and never stop or steer the
controller. Earlier per-stage cycle procedures are preserved at their release
tags (`.autonomous/FLOW.md` at `v3.0.0`, `v4.0.0`).

## Autonomy controller
- `tendo-autonomous.service` runs `scripts/autonomy-controller.sh` in the
  canonical checkout `/home/tomas/repositories/tendo`. It is a single,
  serialized, long-lived process: per loop iteration it either runs ONE
  fresh `opencode run --agent orchestrator` session or pauses. The session
  prompt is `.autonomous/AUTONOMY_PROMPT.md` followed by the advisory work
  selection of `scripts/autonomy-work-selection.py` (computed from live
  GitHub state only; no state of its own). Re-verify live state before
  acting on it.
- Before any GitHub poll the controller checks local wrapper state
  (`opencode-tendo-safe --check`): a pending cycle marker is recovered through
  the wrapper first, and an active Tendo session or an unready checkout means
  wait, never select new work. GitHub is not polled while a cycle or any of
  its children is alive. A selection error or rate limit backs off; it is
  never treated as "nothing to do". Idle polls are spaced
  `IDLE_POLL_SECONDS` (300 s).
- Locks (held for the whole process lifetime, never inherited by children):
  singleton `$XDG_RUNTIME_DIR/tendo-autonomous-$UID.lock` and the legacy
  maintenance lock `$XDG_RUNTIME_DIR/tendo-maintenance-$UID.lock`. Either
  busy → exit 75 and systemd retries (`Restart=on-failure`). Each cycle runs
  under a timeout in its own process group; stop signals terminate the whole
  group (TERM, then KILL) before the locks are released.
- The controller never exits because of a completion marker. It replaces the
  separate daily maintenance timer: dependency, security, CI recovery and
  patch releases are its permanent duties (see "Scheduler order").
- Host installation is a one-time owner action:
  `bash scripts/install-autonomy-service.sh --check` (redacted dry run, no
  mutation), then `--apply` (backup to
  `~/.local/state/tendo-autonomy-migration/<UTC>` mode 0700, quiesce,
  migrate owned Issue labels add-`release`-then-remove-`tendo:v5-autonomous`,
  rewrite the unit, disable `tendo-maintenance.timer`, start and verify);
  `--rollback` restores units, drop-ins, timer state and labels from the
  backup. It refuses (fails closed) on any active work, pending wrapper
  marker, dirty or unsynced checkout, red required CI or unknown unit
  configuration. Agents never run `--apply` or `--rollback`.
- Obsolete per-stage loops, selectors and prompts are removed from the current tree. Their original revisions remain in Git. Owner-only rollback refuses BEFORE service/label/unit mutation if the backed-up runtime is unavailable. Recover a compatible historical checkout at the backup's `root_head` as an explicit owner action; merely restoring an old unit cannot restore deleted executable files. The installer never changes repository history or silently installs a legacy launcher.
- Behavior tests: `bash scripts/autonomy-test.sh` (first stage of the local gate and required CI) runs selector, permanent controller, label-migration, governance-contract and installer suites offline.
- The controller, selector, installer, prompt and systemd units are
  controller configuration (AGENTS `must_not_self_change`); change them only
  through a human-authorized governance PR.

## Start each cycle
1. Read the AGENTS read set (`always` + matching conditional files), each once.
2. Inspect git status/current branch and uncommitted work, origin/main,
   ALL open PRs including bot/Renovate PRs (`gh pr list --state open`; never
   filter by Issue), their CI and merge state, required CI on `main`, open
   security advisories (`gh api repos/bigtcze/tendo/dependabot/alerts`), the
   Renovate Dependency Dashboard (Issue titled "Dependency Dashboard"), recent
   merged PR history and the latest published release (`gh release view`).
3. Query owned Issues by the exact label only:
   `gh issue list --label release --state all`; reconcile their claims,
   comments, dependencies ("Blocked by"), linked PRs and CI with reality
   (reality wins). Milestones and titles never decide ownership. Never treat
   other Issues as work items.
4. Reconcile an active PR first: finish/review/fix it before unrelated work.
   Never abandon a PR you already started without reconciling its state.
5. If no active PR, safely sync local main (`fetch` + `pull --ff-only`) before branching.
6. Do the single highest-priority actionable item ("Scheduler order";
   `scripts/autonomy-work-selection.py` computes it and the controller
   appends it to the prompt). Deliver it as one focused PR per "Work", then
   end the session.

## Scheduler order
The first matching rule wins:
1. red `main` CI (latest completed required run on `main`);
2. urgent security: open `security`-labelled PR, or an open advisory with a
   patched version;
3. unfinished work: an in-flight PR (one that `Refs` an open owned Issue that
   is not `status:blocked`, a Renovate PR already carrying commits by a
   non-Renovate GitHub login, or an open `release/v*` release-notes PR), an
   owned Issue left `status:in-progress` without an open PR, or a pending
   safe-wrapper cycle (recovered by the wrapper);
4. any untriaged Renovate PR older than 48 h (oldest first) — triage it this
   cycle (see "Renovate triage and fairness");
5. an actionable dependency PR when at least TWO feature PRs merged since the
   last merged dependency PR;
6. the highest-value dependency-ready `status:idle` owned Issue (every
   "Blocked by" Issue closed); recurrence Issues rank first (P0), then an
   explicit `Priority: P<n>` body line, then the Issue code; a release
   coordinator Issue ranks last; claimed per "Release Issue execution". If none is ready, an eligible owned dependency cycle or atomic-delivery blocker gets read-only `review-owned-dependencies` advice under "Dependency recovery", never automatic claim authority;
7. remaining actionable dependency PRs;
8. an eligible release: a merged security fix with runtime-meaningful changes
   since the latest release, or the 14-day patch window with
   runtime-meaningful changes ("Maintenance release"); suspended while a
   release coordinator Issue or release PR is in flight;
9. idle: end the session without changes; the controller polls again after
   300 s.
Open PRs are discovered completely (paginated; a truncated list is an error)
and merged history is ordered by merge time.

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
priority is "Scheduler order". They are never ignored because owned release
Issues remain open. Routine Renovate PRs never take priority over a red `main` or a
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
  Comments by Renovate itself never count.
- Aged: a Renovate PR open longer than 48 h without a valid triage record is
  priority 4. Triage is time-bounded and is not an exemption: an `actionable`
  PR is then processed to merge in the dependency slot (priority 5).
- Dependency slot: ordered by merge time, count feature PRs merged into
  `main` since the most recent merged dependency PR (Renovate author,
  `renovate/` branch or `dependencies`/`renovate` label). A feature PR is a
  merged non-dependency PR whose body `Refs` an owned Issue (every release
  work PR does, per "Release Issue execution"); other merges (e.g. unrelated
  docs or human PRs) neither count nor reset. At two or more, the next cycle
  processes the oldest actionable dependency PR. Merging it resets the count.
- `blocked` with an unchanged target is skipped for 7 days, then counts as
  untriaged (priority 4) so the blocker is re-checked: compare upstream
  releases/issues and the relevant Tendo code against the recorded blocker;
  if nothing changed, post a fresh `blocked` record citing the earlier one
  instead of re-running the failing work. A title (version) change needs
  triage at once. Record blockers on the PR, never close it without evidence.
- Renovate and other dependency PRs never receive the `release` label.

## Dependency/tool failures
- Identify root cause before workaround.
- Install only what GUARDRAILS permits.
- Verify version and rerun originally failing task.
- Do not repeatedly reinstall tools across cycles; record durable requirement once.
- Preserve primary failure evidence in the owned Issue or the affected PR if cleanup/reporting also fails.

## Context maintenance
read_set=AGENTS (always + conditional triggers); map and rationale in docs/development/agent-context.md
work_status=owned_release_Issues_plus_open_PRs_PR_triage_comments_and_Renovate_Dependency_Dashboard_no_state_file
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
- For an owned Issue: record the exact blocker and evidence in a comment on
  it and set `status:blocked` (recheck the `release` label first);
  continue with independent safe work.
- For a genuinely incompatible update that cannot be safely resolved, record
  the exact technical blocker, evidence (failing check, log excerpt, local
  reproduction) and upstream issue in a comment on that PR; leave it unmerged
  and continue with independent safe work.
- Before working an open PR again, read its existing blocker comments; do not
  re-run or retry an unchanged blocker. Resume only when upstream, the
  dependency version or Tendo code relevant to it has changed.
- Never claim unverified success.

## Dependency recovery
Execution order is not acceptance. Agents may repair self-imposed ordering and deliver tightly coupled owned work atomically without an owner decision, but may never reduce scope, weaken assertions, bypass review/CI or claim someone else's work.

1. Re-read complete live Issue/PR comments, claim ownership, current heads, failed tests and dependency edges. The selector is read-only advice, never permission to mutate or bypass a dependency. Unknown evidence fails closed.
2. Diagnose declared cycles (including self-dependency), a test-first prerequisite whose red tests need the dependent implementation to merge green, stale/self-imposed ordering or another recoverable engineering blocker. A cycle alone is not proof combining work is safe. Verify all participating Issues are owned, have valid lifecycle labels and no unresolved outside prerequisite.
3. Claims remain binding until explicit RELEASE/HANDOFF by the claimant. Only the existing claimant may continue its branch; another agent must obtain explicit handoff and re-run the collision protocol. Claim age, `status:blocked`, a draft PR, an idle controller or an advisory review are never abandonment evidence. Do not comment or adjust ordering on an active claimant's Issues to take them over.
4. Record a narrow evidence-backed resolution on each affected owned Issue (ownership rechecked): exact dependency edges, why they obstruct delivery, unchanged acceptance, combined PR or safe reordered entry, red commit/run and intended green checks. Prefer existing PR/branch after claim reconciliation; preserve red evidence and all intervening commits. Independent review must challenge the resolution before merge. No premature closure just to satisfy `Blocked by:`.
5. For new blocker records use ordinary comments, not another state store: `BLOCKED agent=<identity> time=<UTC> kind=owned-dependency needs=#N recovery=combined` followed by evidence; genuine external/owner waits use `kind=external recovery=none` with exact reason and relevant change required. An advisory legacy prose review may identify an ordering proposal, but only the claimant can verify it and record the explicit resolution; prose never authorizes bypass. Issue-body acceptance stays stable; an ordering-line adjustment is allowed only after evidence and claim reconciliation, preserving the old edge and rationale in the comment.
6. For test-first atomic delivery, demonstrate the regression fails before changing implementation, then keep the same contract assertions and merge tests plus fix in one coherent green PR referencing every participating owned Issue. Close each only after its own acceptance and verified green main. Never invert assertions, expected-failure-lock a known bug or merge deliberately red tests.
7. Genuine external blockers (upstream outage, unavailable credentials/infrastructure, legal or scope decision, security exception, destructive/irreversible authority or protection gate) are not engineering-ordering recovery. Record the exact blocking fact, required relevant change and safe next action, then skip unchanged blockers. Time, unrelated comments, arbitrary main commits and restarts are not changed evidence. Recheck read-only facts when appropriate; never repeat the unchanged failing work.
8. Concurrent Git changes: fetch and inspect before every push; a changed remote head or dirty/foreign work means stop and reconcile. No force push, reset, clean, rebase of published work or silent overwrites. Preserve all work; use a fresh isolated integration branch only after ownership reconciliation. A rejected normal push is a safety stop, not a reason to force it.

Escalate only matters outside agent authority: scope expansion, security exception, destructive/irreversible action without an authorized safe path, credentials/infrastructure, legal decisions or branch-protection gates. Ordinary technical diagnosis, dependency adjustment among owned work and green atomic delivery do not require owner approval.

## Release Issue execution
Owner-authorized ownership model (replaces the V5 milestone + label selector;
see AGENTS "Ownership"):
- An Issue is agent-owned ONLY when it is an Issue in `bigtcze/tendo` with the
  exact label `release`. Milestones, titles and version names never grant or
  remove ownership. Recheck `release` immediately before each mutation and
  STOP touching the Issue if it disappears. Never adopt an Issue by adding
  `release` to it; never relabel closed historical Issues.
- Each open owned Issue carries exactly one of `status:idle`,
  `status:in-progress`, `status:blocked`.
- Claim (only idle, dependency-ready Issues): comment
  `CLAIM agent=<identity> time=<UTC>`, swap `status:idle` for
  `status:in-progress`, wait 3-5 s, re-read labels and all comments; earliest
  active claim (server timestamp, then comment ID) wins; the loser releases.
- New owned Issue (only a verified defect in owned release work):
  `gh issue create --label release --label status:idle`, labels set at
  creation; never label an existing Issue.
- Dependencies: an Issue body line `Blocked by: #a, #b` (or `none`); a
  dependency is satisfied when the referenced Issue is closed.
- Reference the owned Issue from its PR with `Refs #N`, never `Closes`/
  `Fixes`/`Resolves` (GitHub would auto-close it at merge, before main CI is
  verified). Close it explicitly only after merge, verified green `main` and a
  DONE comment with evidence (`release` rechecked).
- A future major/minor release is planned by the owner as ordinary Issues with
  `release` + `status:idle`, one of them a release coordinator (body line
  `Release: vX.Y.Z` or a `vX.Y.Z` token in the title). No milestone, version
  label, cycle prompt or controller change is needed. Its scope and
  definition-of-done references resolve through the AGENTS compatibility table. Do not create new versioned governance files or copy acceptance into another database.
- Existing open Issues retain all their acceptance through the canonical contracts and AGENTS compatibility table, without editing in-progress Issue bodies. Legacy instructions to exit a stage loop or resume a separate timer are replaced by proof that the permanent controller continues serialized maintenance after release. A requested final completion marker is evidence only, still delivered through a separately reviewed protected PR after release verification; never a stop condition.

## Recurrence priority
Recurrence is product-critical (RECURRENCE, P0): owned recurrence
Issues rank first among ready Issues. Reproduce a recurrence defect with a
failing regression test before changing implementation; one canonical
application service for UI/API/MCP; required-CI coverage at pure, real
PostgreSQL, browser and MCP layers.

## Design and visual validation
- Before UI, token, rendering or visual-baseline work, read
  `.autonomous/UX.md` (the current visual direction). The V4
  reference PNGs and earlier briefs are historical, never targets or evidence.
- Evidence is real production-build browser captures (widths, CZ/EN, themes
  per the brief); never claim human approval without a recorded human action.
- Keep the visual-regression gate meaningful: baseline updates are
  intentional, reviewed and explained in the PR.

## Release from owned Issues
A major or minor release is the release coordinator Issue's deliverable: cut
it per "Maintenance release" procedure steps 1-6 with the version the
coordinator names (the matching SemVer bump, published two-service Compose,
populated upgrade from the previous published release) and the applicable
definition of done. While a coordinator Issue or a `release/v*` PR is open,
automatic patch releases are suspended. Preserve all earlier markers and tags.
Before release, explicitly review ALL outstanding PRs, including dependency PRs, on the release coordinator Issue (or release-notes PR for maintenance). No unexplained untriaged dependency PR or unverified queue may be published around. A genuinely blocked PR may remain open only with exact evidence and a justified recorded decision; unchanged external blockers need not be retried.

## Maintenance release
Scheduler priority 8 (`security-release` / `patch-release`); never while a
release coordinator Issue or release PR is in flight. Evaluate on green
`main`, after any change has merged.
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
