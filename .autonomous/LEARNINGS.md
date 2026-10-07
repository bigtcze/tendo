# Durable Learnings

Format:
`ID | rule | evidence/ref`

L001 | Preserve the primary task failure when cleanup/reporting also fails. | proven autonomous-flow safeguard
L002 | Verify local `main` after merge; command success alone is not proof of merged repository state. | proven autonomous-flow safeguard
L003 | Reconcile dependency/tool failures to root cause; do not paper over them with repeated reinstalls. | proven autonomous-flow safeguard
L004 | If the primary worktree holds uncommitted work and other OpenCode sessions on the shared server are still busy (`GET /session/status?directory=<repo>`), do not edit it; snapshot the diff + untracked files into a separate `git worktree` on a fresh branch and validate/deliver from there. | 2026-10-06 overlapping orphaned cycle writers on auth files

L005 | Triage multi-domain review feedback into bounded dependency-aware worker packets; orchestrator owns full validation/re-triage and cross-cutting design goes to oracle. | 2026-10-07 item-completions review expanded one fixer across API, persistence, HTTP, migrations, contracts and full release validation

Rules:
- Add only knowledge likely to matter across future cycles.
- No session diary, progress narrative, or duplicate policy.
- Prefer updating an existing rule over adding a near-duplicate.
