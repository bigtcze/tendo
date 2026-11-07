# Agent context map

How the autonomous agent's instructions fit together, which files it loads and why. The read set itself lives in `AGENTS.md`; this page explains it for whoever maintains the read set; it is not cycle reading.

## How OpenCode loads context

- Since v3.0.0 the project is in post-V3 maintenance. A daily user systemd timer (`tendo-maintenance.timer`, outside this repository) triages releases and open Renovate PRs and, when there is work, starts one fresh `opencode run --agent orchestrator` session with the owner's maintenance prompt. The V3 controller (`scripts/autonomous-loop.sh` with `.autonomous/CYCLE_PROMPT.md` as first message) exits immediately because `.autonomous/V3_COMPLETE` is tracked on `main`; both are kept unchanged as history.
- OpenCode injects the repository `AGENTS.md` into every session's system prompt (it appears as "Instructions from: …/AGENTS.md"). Reading it again with a tool only duplicates it.
- In the verified configuration no other repository `.autonomous` document is loaded automatically. OpenCode and the installed plugin also supply their own agent and tool instructions, which this repository does not control. Every `.autonomous/*` file is loaded by explicit tool reads that the agent decides on from `AGENTS.md`. Child subagents do not inherit the parent's read history; they get their own system and project instructions plus whatever requirements the orchestrator puts in the packet.
- A file read with a tool stays in the model-visible input of every later step of the session, until compaction summarizes it. Prompt caching lowers the price of that repeated input but does not remove it from the context window. After compaction the exact text may be gone, so `AGENTS.md` tells the agent to re-read a governing file rather than rely on a summary.
- OpenCode Agent Skills (`SKILL.md`) are loaded on demand from global or `.opencode/skills` directories. They were evaluated and not used: locked rules must not be duplicated into skills, and an on-demand skill cannot make a locked rule more reliable than an explicit read-set entry.

## Authority

| File | Kind | Editable by agent | Loaded |
| --- | --- | --- | --- |
| `AGENTS.md` | entry point, read set, authority list | yes (navigation only) | injected |
| `CYCLE_PROMPT.md` | historical V3 controller prompt, human-authored | no: controller input of `scripts/autonomous-loop.sh` | never in maintenance |
| GUARDRAILS, CODE_QUALITY | locked, `agent_must_not_edit=this_file` | no | always |
| CHARTER, PRODUCT, ARCHITECTURE, API, TESTING | locked by `AGENTS.md` `must_not_self_change` | no | always |
| UX, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE | locked (V3_DESIGN_BRIEF also `agent_must_not_edit`) | no | frontend UI, rendering or visual-baseline changes; V3 semantics changes |
| V3_ISSUES_WORKFLOW | locked, `agent_must_not_edit=this_file`; historical | no | only to interpret historical V3 Issue evidence |
| FLOW | agent-maintained procedure | yes, without changing locked policy | always |
| LEARNINGS | agent-maintained durable rules | yes | always |
| DOCUMENTATION | locked | no | docs/user setup, release |
| ROADMAP | locked direction | no | platform or future-seam work |
| V2_SCOPE, V2_DEFINITION_OF_DONE | locked, historical | no | lists, search, filter, sort, pagination, counts, large data; semantics changes; release/completion |
| V2_1_SCOPE, V2_1_DEFINITION_OF_DONE | locked, historical | no | test harness, CI, local gate, E2E infrastructure, release qualification, maintainability refactoring, removing/skipping/relaxing existing tests; semantics changes; release/completion |
| DEFINITION_OF_DONE (V1) | locked, historical | no | item or subject forms, recurrence, lifecycle, history, auth, household, onboarding, security, deployment, backup; semantics changes; release/completion |
| V3_ISSUES_READY | issue identifiers, not status | no (migration output) | when an Issue ID is needed |
| V3_ISSUES_MIGRATION_PROMPT | executed one-shot bootstrap prompt | no | never for routine work |
| PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE, V3_COMPLETE | historical completion markers | never edit or delete | never for routine work |
| `docs/development/dependencies.md`, `release.md`, `release-qualification.md` | agent-maintained development docs | yes | dependency/toolchain/security work; releases |

Precedence when two sources seem to disagree: locked files over FLOW and AGENTS; V3_ISSUES_WORKFLOW over every other statement about Issues; the newer V3 documents refine but never relax the historical V1/V2/V2.1 invariants (GUARDRAILS "Scope safety").

## Post-V3 maintenance read set

During V3 the always set also held UX, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE and V3_ISSUES_WORKFLOW, because locked `V3_SCOPE.md` "Execution constraints" requires reading them for V3 work. V3 work ended with v3.0.0 and `V3_COMPLETE`; routine maintenance is dependency, toolchain, security and compatibility updates, so those files moved to concern triggers (about 26 KB less per session):

- UX and the V3 design files load for any frontend UI, rendering or visual-baseline change, including a dependency migration that touches frontend code. A pin-only bump that passes the unchanged gate (visual regression included) changes no shipped UI and does not need them.
- V3_ISSUES_WORKFLOW is not needed: maintenance never queries or mutates V3 milestone Issues. Status lives in PRs, the Renovate Dependency Dashboard and releases.
- GUARDRAILS, CHARTER, PRODUCT, ARCHITECTURE, API, TESTING and CODE_QUALITY stay always loaded: every dependency change can affect security, behavior, boundaries, the API contract and tests.
- Releases load the V1/V2/V2.1 release-relevant files and DOCUMENTATION, as before, plus FLOW "Maintenance release".

The V3 cycle steps, Issue procedure, design-validation discipline and release/completion protocol were removed from FLOW. They are preserved verbatim at tag `v3.0.0` (`.autonomous/FLOW.md`), next to the unchanged locked V3 files and `V3_COMPLETE`.

## Why the historical V1/V2/V2.1 specifications are loaded by concern

They remain locked and binding. No locked file requires reading all of them every cycle (V2_1_SCOPE requires discovering the current specifications; V3_SCOPE "Execution constraints" names the current ones, which are in the always set, plus "all applicable existing docs"). Many of their contracts are specific to one concern and are not restated elsewhere, for example full-dataset search/filter and no unbounded fetching or DOM (V2_SCOPE journey 3, V2_DEFINITION_OF_DONE) or native no-Docker integration and E2E (V2_1_SCOPE, V2_1_DEFINITION_OF_DONE). So `AGENTS.md` loads each historical file when the work touches its concern, when documented V1/V2/V2.1 semantics would change, and for release and completion. A first version used only "changing tests or shipped behavior" as the trigger; it matched almost every task in the validation runs and under-specified the concern, so it was replaced by the concern map.

## Consolidated duplicates

| Topic | Canonical source | Other files now |
| --- | --- | --- |
| Issue ownership, claim, collision, status labels, closure, V3-18/V3-19 gates | V3_ISSUES_WORKFLOW (locked, historical) | AGENTS "V3 Issues" forbids maintenance from touching V3 Issues; CYCLE_PROMPT and GUARDRAILS unchanged |
| Dependency PR procedure (wrapper, release notes, pin sync, root-cause fixes, Renovate branch rules) | `docs/development/dependencies.md` "Dependency PR procedure" | FLOW keeps the priority rules, the migrate-don't-retry rule and a pointer |
| Release mechanics (tagging, workflow, assets) | `docs/development/release.md` "Cut a release" | FLOW "Maintenance release" holds cadence, eligibility and verification steps and points to it |
| V2.1 release and completion protocol | executed; preserved in git history at tag `v2.1.0` (`.autonomous/FLOW.md`) and in `V2_1_COMPLETE` | removed from FLOW; the V3 release protocol supersedes it |
| V3 release and completion protocol | executed; preserved at tag `v3.0.0` (`.autonomous/FLOW.md`) and in `V3_COMPLETE` | removed from FLOW; FLOW "Maintenance release" governs later releases |

Locked files still repeat each other (for example GUARDRAILS, V3_SCOPE, V3_DEFINITION_OF_DONE and V3_ISSUES_WORKFLOW all state the Issue ownership filter). Agents must not edit them, so those overlaps remain. The same applies to `CYCLE_PROMPT.md`: it restates the V3 brief, but it is the human-authored controller prompt that the loop sends unchanged, so it is left as is.

## Measurement method

Representative fresh sessions run `opencode run --agent orchestrator --format json` in a clean worktree of the commit under test, with a read-only prompt that asks the agent to load the context its instructions require for one scenario and then report the governing requirements and its next actions. Per-step token counts come from the `step_finish` events; prompt size is input plus cache read plus cache write. Scenarios: A frontend implementation, B backend authorization fix, C red main with an oversized component, D Issue claim recovery with an unmanaged look-alike Issue, E release readiness. Results for the change that introduced this page are recorded on Issue #374.
