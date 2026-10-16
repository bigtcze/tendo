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
