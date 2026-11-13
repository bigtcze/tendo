# Guardrails

status=locked
agent_must_not_edit=this_file

## Git safety
- `main` changes only through pull requests.
- Never commit or push directly to `main`.
- Never force-push shared branches or rewrite published history.
- One focused work branch/PR at a time unless a blocker makes independent work necessary.
- Required checks must pass and branch must be current before merge.
- Human approval is not required unless repository rules require it.
- Auto-merge is allowed only after required checks/rules permit it.
- Never bypass branch protection, required checks, or conversation-resolution rules.
- Never merge known-broken code.
- After merge, verify resulting `main`; command success alone is not proof.

## Scope safety
- Only owned `release` Issues authorize feature work; milestones, titles and versions do not. Preserve published data and all canonical product, security, recurrence, API, backup, regression and access-control contracts.
- PRODUCT, RECURRENCE, SECURITY, UX, ARCHITECTURE, API, TESTING, DEFINITION_OF_DONE and CODE_QUALITY remain binding. Design work does not authorize unrelated behavior or security changes.
- No unrelated features, AI, native apps, calendar/email integrations, notifications, arbitrary schema/API rewrites or enterprise dashboard redesign.
- Only intentional focused changes and necessary verified fixes. Verify real usability/browser visuals, not tests alone.
- Never adopt an existing Issue by adding the ownership label. Recheck `release` before every mutation; stop if it disappears. Historical completed Issues remain untouched.
- Owned Issues use exactly one of status:idle, status:in-progress, status:blocked. Done means closed after complete acceptance evidence, protected merge and green main, or independently verified release deliverable.
- GitHub owned Issues and PRs are the only work/claim/status ledger. No STATE.md, additional coordinator, tracking labels, shadow state or sync bot.
- Dependency ordering may be reconciled only under FLOW "Dependency recovery" among owned work. Never weaken acceptance, tests, review or merge gates or take over another active claim.
- All new/refactored UI follows CODE_QUALITY; never create giant tangled components to obtain a superficial redesign.

## Controller boundary
- Do not upgrade/reconfigure the coding-agent runtime (FLOW "Runtime integration"), agent plugins, provider auth, global runtime config, systemd units, the autonomy controller, selector, prompt, installer or ownership-migration policy without explicit owner-authorized governance work.
- If controller tooling is broken, record exact evidence on the owned Issue (ownership rechecked) or affected PR and use a safe alternative when possible. Never change host services or runtime locks autonomously.
- Never edit unrelated repositories or host services.

## Tooling autonomy
Allowed when directly required to build/test Tendo:
- project-local dependencies;
- user-space development tools;
- Docker images/tooling used by this project;
- noninteractive apt package installation only when already permitted by host policy.

For apt:
- use `sudo -n`;
- allowed operations are update/install of required development/build/test packages;
- never remove packages, dist-upgrade, alter repositories, or wait for a sudo password;
- verify installed version and rerun the blocked task;
- record durable requirements once; do not repeatedly reinstall the same tool.

## Secrets/privacy
- Never expose/commit credentials, tokens, private keys, cookies, session secrets, or `.env` contents.
- No telemetry, analytics, crash reporting, or external tracking by default.
- Do not send household data to external services in V1.
- Use least privilege and secure defaults.

## Product invariants
- Tendo is a household organization tool, not a medical/legal/financial authority.
- It may record user-defined intervals; it must not invent professional schedules/recommendations.
- Time passing alone never silently deletes/completes an item.
- Time may derive UPCOMING -> NEEDS_ATTENTION; only explicit user actions may complete, archive, pause, or otherwise remove an item from normal attention.
- Never shame users for lateness.
- Recurrence is optional; a one-off item is a first-class V1 use case.
- Turning recurrence off must never be treated as an exceptional/secondary path.

## Quality
- No placeholder tests, tests that only prove mocks, or assertions that merely check a request returned `200`.
- Do not weaken/delete meaningful tests to make CI green.
- Every fixed behavior regression gets a regression test.
- No `|| true`, ignored failures, broad test skips, or flaky-test retries in required CI.
- A PR is not done until behavior, tests, docs, migrations, and API contract agree.

## Legal/dependencies
- Project license is a human decision; do not add/change the root project license without explicit human instruction.
- Prefer mature, actively maintained dependencies with licenses compatible with an open-source project.
- Record third-party UI starter/template source and license.
