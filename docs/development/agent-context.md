# Agent context map

How the autonomous agent's instructions fit together, which files it loads and why. The read set itself lives in `AGENTS.md`; this page explains it for whoever maintains the read set; it is not cycle reading.

## How OpenCode loads context

- V4 Modern Everyday is the current stage (human-authorized, activated by managed Issue V4-00). The controller is the existing user unit `tendo-autonomous.service`, which runs `scripts/autonomous-loop.sh`; each cycle is one fresh `opencode run --agent orchestrator` session whose first message is `.autonomous/V4_CYCLE_PROMPT.md`. The loop exits successfully once `.autonomous/V4_COMPLETE` is tracked on clean `main`. `.autonomous/CYCLE_PROMPT.md` (V3) is kept unchanged as history and is no longer sent.
- Post-V3 maintenance (daily user timer `tendo-maintenance.timer` → `~/.local/bin/tendo-maintenance.sh` with the owner's prompt, outside this repository) is paused during V4. It is a second controller that would otherwise start its own sessions, so the V4 loop refuses to start a cycle (exit 3, no session) while `tendo-maintenance.timer` or `tendo-maintenance.service` is enabled or active, or while the maintenance flock `$XDG_RUNTIME_DIR/tendo-maintenance-$UID.lock` is held; it rechecks before every cycle and holds that flock for its whole run, so a manually started maintenance run exits as "already running". Dependency and security work continues inside V4 cycles (FLOW). Behavior tests: `bash scripts/autonomous-loop-test.sh` (local gate and required CI).
- OpenCode injects the repository `AGENTS.md` into every session's system prompt (it appears as "Instructions from: …/AGENTS.md"). Reading it again with a tool only duplicates it.
- In the verified configuration no other repository `.autonomous` document is loaded automatically. OpenCode and the installed plugin also supply their own agent and tool instructions, which this repository does not control. Every `.autonomous/*` file is loaded by explicit tool reads that the agent decides on from `AGENTS.md`. Child subagents do not inherit the parent's read history; they get their own system and project instructions plus whatever requirements the orchestrator puts in the packet.
- A file read with a tool stays in the model-visible input of every later step of the session, until compaction summarizes it. Prompt caching lowers the price of that repeated input but does not remove it from the context window. After compaction the exact text may be gone, so `AGENTS.md` tells the agent to re-read a governing file rather than rely on a summary.
- OpenCode Agent Skills (`SKILL.md`) are loaded on demand from global or `.opencode/skills` directories. They were evaluated and not used: locked rules must not be duplicated into skills, and an on-demand skill cannot make a locked rule more reliable than an explicit read-set entry.

## Authority

| File | Kind | Editable by agent | Loaded |
| --- | --- | --- | --- |
| `AGENTS.md` | entry point, read set, authority list | yes (navigation only) | injected |
| `V4_CYCLE_PROMPT.md` | V4 controller prompt, human-authorized through V4-00 | no: controller input of `scripts/autonomous-loop.sh` | as the session prompt |
| V4_SCOPE, V4_DEFINITION_OF_DONE, V4_ISSUES_WORKFLOW | locked, human-authorized V4 contract (imported byte-for-byte by V4-00) | no | always during V4 |
| V4_DESIGN_BRIEF + `docs/design/reference/tendo-v4-{desktop,mobile}.png` | locked visual direction; images are references, not screenshots; SHA-256 in the brief header | no | frontend UI, design tokens, rendering or visual-baseline changes |
| `CYCLE_PROMPT.md` | historical V3 controller prompt, human-authored | no | never |
| GUARDRAILS, CODE_QUALITY | locked, `agent_must_not_edit=this_file` | no | always |
| CHARTER, PRODUCT, ARCHITECTURE, API, TESTING | locked by `AGENTS.md` `must_not_self_change` | no | always |
| UX, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE | locked (V3_DESIGN_BRIEF also `agent_must_not_edit`) | no | with the V4 brief for UI work; V3 semantics changes |
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
| V4_COMPLETE (future) | V4 completion marker, only via V4-15 final protected PR | created once by V4-15 | stops the controller |
| `docs/development/dependencies.md`, `release.md`, `release-qualification.md` | agent-maintained development docs | yes | dependency/toolchain/security work; releases |

Precedence when two sources seem to disagree: locked files over FLOW and AGENTS; V4_ISSUES_WORKFLOW over every other statement about V4 Issues and V3_ISSUES_WORKFLOW over every statement about V3 Issues; V4_DESIGN_BRIEF over V3_DESIGN_BRIEF for visual direction only; the newer V3 documents refine but never relax the historical V1/V2/V2.1 invariants (GUARDRAILS "Scope safety").

## V4 activation and controller runbook

V4-00 (#408) imported the four V4 specifications and both reference images byte-for-byte from the human-approved packet (`v4-design-identity-deployment-2026-11-09-r2`), switched this read set, FLOW and the loop to V4, and did not touch any systemd unit (GUARDRAILS controller boundary). The human owner switches controllers once, after the V4-00 PR is merged and no maintenance run is active:

```bash
systemctl --user is-active tendo-maintenance.service   # must print inactive
systemctl --user disable --now tendo-maintenance.timer
git -C ~/repositories/tendo switch main && git -C ~/repositories/tendo pull --ff-only
~/.local/bin/opencode-tendo-safe --check                 # must print GUARD CHECK PASSED
systemctl --user start tendo-autonomous.service
journalctl --user -u tendo-autonomous.service -f
```

Inside the loop, a failed or timed-out OpenCode cycle is retried with backoff. The loop process itself exits on a refusal (exit 3: maintenance controller enabled, active or holding its lock), a missing tool or governance file (exit 2), or completion (exit 0); because of the unit's `Restart=no` drop-in it then stays stopped until the owner starts it again. The loop always uses the canonical checkout `/home/tomas/repositories/tendo`, the same one the host wrapper runs and recovers sessions in. After V4-15 merges `V4_COMPLETE` the loop exits; re-enable maintenance with `systemctl --user enable --now tendo-maintenance.timer` (its prompt is the owner's file and is not changed by this repository).

Known limitation: the host wrapper `~/.local/bin/opencode-tendo-safe` (outside this repository) embeds a recovery prompt that still mentions `.autonomous/STATE.md`; under AGENTS "Context rules" that means the owned managed V4 Issue.

## Post-V3 maintenance read set (historical, before V4)

During V3 the always set also held UX, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE and V3_ISSUES_WORKFLOW, because locked `V3_SCOPE.md` "Execution constraints" requires reading them for V3 work. V3 work ended with v3.0.0 and `V3_COMPLETE`; routine maintenance is dependency, toolchain, security and compatibility updates, so those files moved to concern triggers (about 26 KB less per session):

- UX and the V3 design files load for any frontend UI, rendering or visual-baseline change, including a dependency migration that touches frontend code. A pin-only bump that passes the unchanged gate (visual regression included) changes no shipped UI and does not need them.
- V3_ISSUES_WORKFLOW is not needed: maintenance never queries or mutates V3 milestone Issues. Status lives in PRs, the Renovate Dependency Dashboard and releases.
- GUARDRAILS, CHARTER, PRODUCT, ARCHITECTURE, API, TESTING and CODE_QUALITY stay always loaded: every dependency change can affect security, behavior, boundaries, the API contract and tests.
- Releases load the V1/V2/V2.1 release-relevant files and DOCUMENTATION, as before, plus FLOW "Maintenance release".

The V3 cycle steps, Issue procedure, design-validation discipline and release/completion protocol were removed from FLOW. They are preserved verbatim at tag `v3.0.0` (`.autonomous/FLOW.md`); the locked V3 files are unchanged, and `V3_COMPLETE` was added after the tag by the final V3 completion PR (#402).

## Why the historical V1/V2/V2.1 specifications are loaded by concern

They remain locked and binding. No locked file requires reading all of them every cycle (V2_1_SCOPE requires discovering the current specifications; V3_SCOPE "Execution constraints" names the V3 ones, now concern-triggered by UI/visual work after V3 completion, plus "all applicable existing docs"). Many of their contracts are specific to one concern and are not restated elsewhere, for example full-dataset search/filter and no unbounded fetching or DOM (V2_SCOPE journey 3, V2_DEFINITION_OF_DONE) or native no-Docker integration and E2E (V2_1_SCOPE, V2_1_DEFINITION_OF_DONE). So `AGENTS.md` loads each historical file when the work touches its concern, when documented V1/V2/V2.1 semantics would change, and for releases. A first version used only "changing tests or shipped behavior" as the trigger; it matched almost every task in the validation runs and under-specified the concern, so it was replaced by the concern map.

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
