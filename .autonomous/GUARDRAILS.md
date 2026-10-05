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
- Build V1 only.
- ROADMAP is direction, not implementation permission.
- Prefer the smallest complete solution over speculative framework-building.
- No AI, document ingestion, Nextcloud, calendar sync, email ingestion, chatbot, native mobile app, analytics dashboard, gamification, household ERP, or general-purpose autonomous agent runtime in V1.
- Platform-ready means stable boundaries/contracts, not empty plugin frameworks or unused abstractions.

## Controller boundary
- Do not upgrade/reconfigure OpenCode, Oh My OpenAgent Slim, provider auth, global OpenCode config, this systemd unit, or `scripts/autonomous-loop.sh`.
- If controller tooling is broken, record exact evidence in STATE and use a safe alternative when possible.
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
