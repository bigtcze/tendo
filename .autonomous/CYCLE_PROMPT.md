Work autonomously on Tendo V1.

Read `AGENTS.md` and all referenced `.autonomous` context first. Locked files are immutable policy.

Goal this cycle:
- advance the highest-value unproven V1 Definition-of-Done behavior;
- finish existing PR/blocker work before unrelated work;
- keep the change a small complete vertical slice;
- preserve platform boundaries without speculative infrastructure.

Hard requirements:
- never commit/push directly to `main`;
- branch + PR only;
- required checks green and branch current before merge;
- never bypass repository protection;
- no V2+ implementation;
- Repeat and Fluid recurrence are distinct concepts;
- fixed and after-completion recurrence must both remain correct;
- V1 UI exposes one named/default household only, while DB/auth design remains future multi-household-capable;
- responsive web only; no native mobile app architecture;
- preserve calm consumer UX;
- backend application/domain owns business/date/auth rules;
- public API follows locked API contract;
- behavior changes include meaningful tests at appropriate layers;
- documentation/config examples change with behavior;
- update STATE from reality before ending;
- LEARNINGS only for durable reusable knowledge;
- never modify controller/global OpenCode boundaries.

Use available tools/agents for implementation and independent review. Resolve failures rather than hiding them. If blocked, record exact evidence and choose safe independent V1 work.

Completion is not a direct-main action. When all V1 DoD items are proven on main, follow FLOW's final completion-PR protocol.
