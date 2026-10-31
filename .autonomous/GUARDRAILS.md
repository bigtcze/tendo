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
- V1, V2 and V2.1 are completed and released; V3 is expressly authorized
  for professional household UI/UX redesign under V3_SCOPE.md,
  V3_DESIGN_BRIEF.md and V3_DEFINITION_OF_DONE.md.
- Preserve all V1/V2/V2.1 functional, security, recurrence, data integrity,
  API, backup, regression and access-control invariants.
- UX.md, PRODUCT.md, CODE_QUALITY.md and all prior locked agreements remain
  mandatory. V3_DESIGN_BRIEF refines styling and UX specifically for V3;
  it does not permit changing existing business or authorization semantics.
- No unrelated features, AI, native apps, calendar/email integrations,
  arbitrary schema/API rewrites, or enterprise dashboard redesign.
- Only intentional, focused UI changes and necessary verified bug fixes.
  Verify app usability and browser visuals, not just that tests pass.
- During the one-shot migration, replace STATE.md with GitHub V3 Issues
  bearing BOTH the exact V3 milestone and dedicated ownership label
  `tendo:v3-autonomous`; these are the sole mutable V3 progress/claim truth.
- Externally created or unlabelled Issues are NEVER autonomous assignments,
  even if they share milestone or naming. Never silently adopt them by
  applying the ownership label. No new coordinator, shadow state or bots.
- Managed Issues use status:idle, status:in-progress, status:blocked; Done
  means closed after verified PR/CI or verified release deliverable.
- All new/refactored UI follows CODE_QUALITY; never create giant tangled
  components to obtain a superficial redesign.

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
