# Agent context map

How the autonomous agent's instructions fit together, which files it loads and why. The read set itself lives in `AGENTS.md`; this page explains it for whoever maintains the read set; it is not cycle reading.

## How OpenCode loads context

- The systemd unit runs `scripts/autonomous-loop.sh`. Each cycle starts a fresh `opencode run --agent orchestrator` session whose first user message is `.autonomous/CYCLE_PROMPT.md`.
- OpenCode injects the repository `AGENTS.md` into every session's system prompt (it appears as "Instructions from: …/AGENTS.md"). Reading it again with a tool only duplicates it.
- In the verified configuration no other repository `.autonomous` document is loaded automatically. OpenCode and the installed plugin also supply their own agent and tool instructions, which this repository does not control. Every `.autonomous/*` file is loaded by explicit tool reads that the agent decides on from `AGENTS.md`. Child subagents do not inherit the parent's read history; they get their own system and project instructions plus whatever requirements the orchestrator puts in the packet.
- A file read with a tool stays in the model-visible input of every later step of the session, until compaction summarizes it. Prompt caching lowers the price of that repeated input but does not remove it from the context window. After compaction the exact text may be gone, so `AGENTS.md` tells the agent to re-read a governing file rather than rely on a summary.
- OpenCode Agent Skills (`SKILL.md`) are loaded on demand from global or `.opencode/skills` directories. They were evaluated and not used: locked rules must not be duplicated into skills, and an on-demand skill cannot make a locked rule more reliable than an explicit read-set entry.

## Authority

| File | Kind | Editable by agent | Loaded |
| --- | --- | --- | --- |
| `AGENTS.md` | entry point, read set, authority list | yes (navigation only) | injected |
| `CYCLE_PROMPT.md` | per-cycle controller prompt, human-authored | left unchanged: it is part of the controller input sent by `scripts/autonomous-loop.sh` | is the first message |
| GUARDRAILS, CODE_QUALITY, V3_DESIGN_BRIEF, V3_ISSUES_WORKFLOW | locked, `agent_must_not_edit=this_file` | no | always |
| CHARTER, PRODUCT, ARCHITECTURE, API, UX, TESTING, V3_SCOPE, V3_DEFINITION_OF_DONE | locked by `AGENTS.md` `must_not_self_change` | no | always |
| FLOW | agent-maintained procedure | yes, without changing locked policy | always |
| LEARNINGS | agent-maintained durable rules | yes | always |
| DOCUMENTATION | locked | no | docs/user setup, release |
| ROADMAP | locked direction | no | platform or future-seam work |
| V2_SCOPE, V2_DEFINITION_OF_DONE | locked, historical | no | lists, search, filter, sort, pagination, counts, large data; semantics changes; release/completion |
| V2_1_SCOPE, V2_1_DEFINITION_OF_DONE | locked, historical | no | tests, harness, CI, local gate, E2E infrastructure, release qualification, maintainability refactoring; semantics changes; release/completion |
| DEFINITION_OF_DONE (V1) | locked, historical | no | deployment, auth, household, onboarding, recurrence, lifecycle, history, backup; semantics changes; release/completion |
| V3_ISSUES_READY | issue identifiers, not status | no (migration output) | when an Issue ID is needed |
| V3_ISSUES_MIGRATION_PROMPT | executed one-shot bootstrap prompt | no | never for routine work |
| PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE | historical completion markers | never edit or delete | never for routine work |

Precedence when two sources seem to disagree: locked files over FLOW and AGENTS; V3_ISSUES_WORKFLOW over every other statement about Issues; the newer V3 documents refine but never relax the historical V1/V2/V2.1 invariants (GUARDRAILS "Scope safety").

## Why the always set contains API and UX

Locked `V3_SCOPE.md` "Execution constraints" requires reading V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE, CODE_QUALITY, GUARDRAILS, PRODUCT, UX, ARCHITECTURE, API and TESTING for V3 work. They were conditional before; they are now unconditional so that the read set satisfies that locked rule by itself.

## Why the historical V1/V2/V2.1 specifications are loaded by concern

They remain locked and binding. No locked file requires reading all of them every cycle (V2_1_SCOPE requires discovering the current specifications; V3_SCOPE "Execution constraints" names the current ones, which are in the always set, plus "all applicable existing docs"). Many of their contracts are specific to one concern and are not restated elsewhere, for example full-dataset search/filter and no unbounded fetching or DOM (V2_SCOPE journey 3, V2_DEFINITION_OF_DONE) or native no-Docker integration and E2E (V2_1_SCOPE, V2_1_DEFINITION_OF_DONE). So `AGENTS.md` loads each historical file when the work touches its concern, when documented V1/V2/V2.1 semantics would change, and for release and completion. A first version used only "changing tests or shipped behavior" as the trigger; it matched almost every task in the validation runs and under-specified the concern, so it was replaced by the concern map.

## Consolidated duplicates

| Topic | Canonical source | Other files now |
| --- | --- | --- |
| Issue ownership, claim, collision, status labels, closure, V3-18/V3-19 gates | V3_ISSUES_WORKFLOW (locked) | FLOW keeps the cycle steps and short reminders and points to it; AGENTS points to it; CYCLE_PROMPT and GUARDRAILS unchanged |
| Dependency PR procedure (wrapper, release notes, pin sync, root-cause fixes, Renovate branch rules) | `docs/development/dependencies.md` "Dependency PR procedure" | FLOW keeps the priority rules and a pointer |
| V2.1 release and completion protocol | executed; preserved in git history at tag `v2.1.0` (`.autonomous/FLOW.md`) and in `V2_1_COMPLETE` | removed from FLOW; the V3 release protocol supersedes it |
| V3 release protocol | FLOW "V3 release and completion protocol" | AGENTS "Completion" points to it |

Locked files still repeat each other (for example GUARDRAILS, V3_SCOPE, V3_DEFINITION_OF_DONE and V3_ISSUES_WORKFLOW all state the Issue ownership filter). Agents must not edit them, so those overlaps remain. The same applies to `CYCLE_PROMPT.md`: it restates the V3 brief, but it is the human-authored controller prompt that the loop sends unchanged, so it is left as is.

## Measurement method

Representative fresh sessions run `opencode run --agent orchestrator --format json` in a clean worktree of the commit under test, with a read-only prompt that asks the agent to load the context its instructions require for one scenario and then report the governing requirements and its next actions. Per-step token counts come from the `step_finish` events; prompt size is input plus cache read plus cache write. Scenarios: A frontend implementation, B backend authorization fix, C red main with an oversized component, D Issue claim recovery with an unmanaged look-alike Issue, E release readiness. Results for the change that introduced this page are recorded on Issue #374.
