# V4 autonomous execution contract

release=v4.0.0
milestone=Tendo v4.0.0 - Modern Everyday
managed_issue_label=tendo:v4-autonomous
progress_source=GitHub_Issues_only

1. An issue is V4-agent-managed only if it is NOT a PR AND has BOTH exact V4 milestone and exact V4 ownership label. Never adopt, label, comment on, edit, claim, close, or treat an unowned issue as a V4 task. V3 issues are historical and untouchable.
2. Bootstrapping work V4-00 is a deliberate human-authorized transition from post-V3 maintenance: update AGENTS/conditional read set and FLOW/controller as needed via protected PR; preserve current toolchain, maintenance guarantees and no surprise active controller changes. Do not start product features before this PR is merged and green.
3. Exactly one status:idle/status:in-progress/status:blocked label per open managed issue. Claim only dependency-ready idle issues, post CLAIM comment, change status, wait 3–5 seconds, reread and resolve collision by earliest active claim timestamp + ID. Claims/handoffs/evidence live in issue comments. No STATE.md, second ledger, lease service or custom framework.
4. Work in focused branch and one PR at a time, no direct push to protected main. Changes require meaningful tests, independent review and required CI; only close owned issue when PR merged and green main verified. Reconcile existing PRs/claims on interruption. Do not silently weaken checks or scope.
5. Repo governance/source files are instructions, not success evidence. Only actual frontend/backend, API, migration, security and E2E behavior can satisfy acceptance criteria. Human-approved image concepts are visual targets; only genuine browser captures validate implementation.
6. Finalization: all agent-owned V4 issues (except final marker ticket) closed, DoD evidenced, release docs and v4.0.0 tagged/published/verified with public multiarch GHCR, clean install and v3 upgrade; final V4_COMPLETE via separately reviewed protected PR only, then stop V4 feature loop.
7. New verified V4 release blockers get newly created V4-owned milestone+label issues at creation; unlabelled user/third-party issues remain outside autonomy. Never modify completed V3 governance files or historical tags/markers.
