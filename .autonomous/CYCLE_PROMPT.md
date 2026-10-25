Work autonomously on Tendo V2: a polished, professional, scalable household GUI.
Read AGENTS.md and its referenced files, especially V2_SCOPE and
V2_DEFINITION_OF_DONE. V1 is complete and released; preserve all its invariants.

Begin by reconciling git, open PRs, STATE, and required CI. Finish existing PR
work first. Choose the smallest high-value unproven V2 UX/scale/quality slice.
Audit real browser UX and realistic large backlogs before aesthetic changes.

Never directly commit/push main; use branch + PR, current branch and green
required checks, independent review, complete local test gate and meaningful
Playwright production-build browser tests. Never bypass protection, weaken tests,
change controller/systemd/OpenCode configuration, or implement unrelated ROADMAP
features. Keep user-facing Czech and English clear; protect recurrence, auth,
API, idempotency, household isolation, historical data and all V1 behavior.

Use subagents in bounded packets (one writing subagent per worktree), consult
oracle for cross-cutting API/DB/auth/concurrency decisions, keep STATE current
and LEARNINGS reusable. Fix real failure causes; never declare unproven success.

After every V2 DoD item passes on main, follow FLOW's V2 release and completion
protocol: publish and verify GitHub Release v2.0.0 and container, then add
V2_COMPLETE via final PR and end on clean main. PROJECT_COMPLETE is V1 history.

## Autonomous technical blocker resolution

Owner clarification (2026-10-25):

Tendo V2 development and release must operate autonomously.
Ordinary engineering, testing, infrastructure, accessibility,
and release decisions belong to the autonomous agent.

Do not request owner approval for technical waivers or
routine engineering decisions.

Historical E2E incident (CI run 39872577652):

- The original diagnostic artifacts are lost.
- Do not claim the historical root cause is known.
- Repair CI artifact retention so Playwright traces,
  screenshots, reports and logs survive failed jobs.
- Verify artifact retention with an intentionally failing test.
- Investigate relevant race conditions and reproduce failures.
- Fix identified defects through the normal PR workflow.
- Obtain at least three successful complete CI E2E runs
  and twenty targeted reproduction runs after hardening.
- If no new unexplained failures occur, evaluate residual
  risk and document an evidence-based technical closure.
- The historical unknown cause must remain documented.
- Do not request a human exception for missing historical
  evidence that can no longer be recovered.

Accessibility:

- Arrange independent AI-assisted review, separate from
  the implementation agent.
- Perform production-build keyboard, semantic, and
  automated accessibility verification.
- Provision and use screen-reader tools where feasible.
- If actual screen-reader testing is technically unavailable,
  document the limitation and independently inspect the
  accessibility tree and keyboard interaction.
- Fix all identified blocking accessibility defects.
- Never claim a human audit or screen-reader test occurred
  unless it actually occurred.

Autonomy and quality:

- Solve technical blockers instead of escalating them.
- Never weaken branch protection or disable required tests.
- Never hide errors using retries or skipped checks.
- Any new unexplained failure remains a release blocker.
- Document evidence and remaining uncertainty in STATE.
- Keep README release claims accurate until publication.
- Continue the release workflow autonomously.

This owner clarification takes precedence over earlier
instructions demanding human approval solely for routine
technical blocker resolution.

Escalate only genuine new product decisions or actions
outside existing authorization.
