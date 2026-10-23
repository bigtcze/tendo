# Durable Learnings

Format:
`ID | rule | evidence/ref`

L001 | Preserve the primary task failure when cleanup/reporting also fails. | proven autonomous-flow safeguard
L002 | Verify local `main` after merge; command success alone is not proof of merged repository state. | proven autonomous-flow safeguard
L003 | Reconcile dependency/tool failures to root cause; do not paper over them with repeated reinstalls. | proven autonomous-flow safeguard
L004 | If the primary worktree holds uncommitted work and other OpenCode sessions on the shared server are still busy (`GET /session/status?directory=<repo>`), do not edit it; snapshot the diff + untracked files into a separate `git worktree` on a fresh branch and validate/deliver from there. | 2026-10-06 overlapping orphaned cycle writers on auth files

L005 | Triage multi-domain review feedback into bounded dependency-aware worker packets; orchestrator owns full validation/re-triage and cross-cutting design goes to oracle. | 2026-10-07 item-completions review expanded one fixer across API, persistence, HTTP, migrations, contracts and full release validation
L006 | When delegating writes into a non-primary git worktree, require every subagent shell command to set workdir or `cd` to that worktree and to confirm the primary checkout is clean before finishing; default cwd is the primary checkout. | 2026-10-07 undo-completion fixer wrote api/openapi.yaml into primary checkout
L007 | Playwright `journey.spec.ts` is one serial shared-state journey: each new test must navigate to its start screen itself and use titles unique across the whole file; verify with the full `scripts/e2e-smoke.sh`, never an isolated test. | 2026-10-08 item-edit n5 failed twice on prior test's detail screen and a reused title
L008 | Playwright `getByRole` name matching is substring by default; use `exact: true` for short accessible names (e.g. `Sign in`) that later controls may extend. | 2026-10-09 OIDC `Sign in with …` button broke journey n7 strict-mode locator
L009 | Host lacks native psql and a PG test service: run disposable `postgres:<toolchain pin>` container on 127.0.0.1 with `CREATE ROLE tendo NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`, keep the admin URL in a private 0600 env file outside the repo, and set `PSQL` to a Docker `psql` shim forwarding `PG*` env; prefer `docker exec -i <pg container> psql` (with `PGHOST=127.0.0.1 PGPORT=5432`) over `docker run --rm`, because per-call container start can exceed the harness 10-second psql deadline; then `verify-local.sh` and `setup-integration.sh` run unchanged. | 2026-10-14 subject create idempotency PR
L010 | Visual/scale evidence captures must set the viewport before every capture and assert `window.innerWidth`, stop timing before screenshots/`networkidle`, and apply fixture mutations in generator order (not concurrent completion order); record claims only for what was actually captured. | 2026-10-16 V2 audit PR 101 oracle round 1 found mislabeled 1440 px "390 px" captures and inflated timings
L011 | Shared chrome (nav, header) adds links and listitems to every screen: scope row/list/link queries in Vitest and Playwright to `main` or a named landmark, never count unscoped `listitem`; and update the opt-in `ux-audit.spec.ts` in the same PR as any UI change it drives, since it is not in required CI and silently drifts. | 2026-10-17 slice 8a nav broke n12 counts, the audit history count and a Subjects 401 test; audit had stalemated on the pre-6a single Show more
L012 | Never run two `verify-local.sh`/`e2e-smoke.sh`/ux-audit runs concurrently against the shared PostgreSQL test cluster: harness tests snapshot cluster-wide `tendo_m_/tendo_r_` roles and fail on another run's per-run roles; serialize gates (one script looping worktrees) and re-run alone before treating such a failure as real. | 2026-10-23 PR 136 gate failed only while a designer E2E/audit ran in parallel; passed alone
L013 | When Renovate reports an npm "artifact update problem" (e.g. EALLOWREMOTE), `npm ci` fails on the stale lockfile: sync only the bumped packages' lock entries via the wrapper (`npm install --package-lock-only`, then drop unrelated churn such as newly materialized optional bundle deps), confirm `npm ci` and `npm ls`, run the full gate, push to the Renovate branch. | 2026-10-23 PR 145 react 19.3.1 lockfile left at 19.3.0; local install also added unrelated oxide-wasm32-wasi bundle entries

Rules:
- Add only knowledge likely to matter across future cycles.
- No session diary, progress narrative, or duplicate policy.
- Prefer updating an existing rule over adding a near-duplicate.
