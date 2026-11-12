# Agent context map

How the autonomous agent's instructions fit together, which files it loads and why. The read set itself lives in `AGENTS.md`; this page explains it for whoever maintains the read set; it is not cycle reading.

## How OpenCode loads context

- The controller is evergreen. The existing user unit `tendo-autonomous.service` runs `scripts/autonomy-controller.sh`, a single serialized process. Each unit of work is one fresh `opencode run --agent orchestrator` session whose first message is `.autonomous/AUTONOMY_PROMPT.md`, followed by the advisory work selection of `scripts/autonomy-work-selection.py`. That selection is FLOW "Scheduler order" computed from live GitHub state; the selector keeps no state of its own. Ownership is the `release` label alone, so future releases need no new prompt, milestone filter, label or controller code. No `*_COMPLETE` marker stops it. `V5_CYCLE_PROMPT.md`, `scripts/autonomous-loop.sh` and `scripts/v5_work_selection.py` are retained unchanged and unused, so that the installer `--rollback` can restore the V5 loop. `V4_CYCLE_PROMPT.md` and `CYCLE_PROMPT.md` are history.
- The separate daily maintenance timer (`tendo-maintenance.timer` → `~/.local/bin/tendo-maintenance.sh` with the owner's out-of-repo prompt) is disabled at cutover; its duties (dependency PRs, security, CI recovery, 14-day patch releases) are scheduler priorities 1–8. The controller holds the singleton lock `$XDG_RUNTIME_DIR/tendo-autonomous-$UID.lock` and the legacy maintenance lock `tendo-maintenance-$UID.lock` for its lifetime, so a manually started maintenance run exits "already running". Behavior tests: `bash scripts/autonomy-test.sh` (local gate and required CI).
- OpenCode injects the repository `AGENTS.md` into every session's system prompt (it appears as "Instructions from: …/AGENTS.md"). Reading it again with a tool only duplicates it.
- In the verified configuration no other repository `.autonomous` document is loaded automatically. OpenCode and the installed plugin also supply their own agent and tool instructions, which this repository does not control. Every `.autonomous/*` file is loaded by explicit tool reads that the agent decides on from `AGENTS.md`. Child subagents do not inherit the parent's read history; they get their own system and project instructions plus whatever requirements the orchestrator puts in the packet.
- A file read with a tool stays in the model-visible input of every later step of the session, until compaction summarizes it. Prompt caching lowers the price of that repeated input but does not remove it from the context window. After compaction the exact text may be gone, so `AGENTS.md` tells the agent to re-read a governing file rather than rely on a summary.
- OpenCode Agent Skills (`SKILL.md`) are loaded on demand from global or `.opencode/skills` directories. They were evaluated and not used: locked rules must not be duplicated into skills, and an on-demand skill cannot make a locked rule more reliable than an explicit read-set entry.

## Authority

| File | Kind | Editable by agent | Loaded |
| --- | --- | --- | --- |
| `AGENTS.md` | entry point, read set, authority list | yes (navigation only) | injected |
| `AUTONOMY_PROMPT.md` | evergreen controller prompt, owner-authorized governance PR | no: controller input of `scripts/autonomy-controller.sh` | as the session prompt |
| `V5_CYCLE_PROMPT.md` | retained V5 controller prompt (rollback only) | no | never |
| V5_SCOPE, V5_RECURRENCE_CONTRACT, V5_SECURITY_AND_MCP, V5_DEFINITION_OF_DONE, V5_ISSUES_WORKFLOW | locked, human-authorized V5 contract (imported byte-for-byte by V5-00); ownership selector and controller handoff superseded by AGENTS "Evergreen precedence" | no | working a migrated V5 `release` Issue; recurrence/security/MCP concerns |
| V5_DESIGN_BRIEF | locked V5 visual direction (no reference image) | no | frontend UI, design tokens, rendering or visual-baseline changes |
| `scripts/autonomy-controller.sh`, `scripts/autonomy-work-selection.py`, `scripts/install-autonomy-service.sh`, `scripts/autonomy_migration.py` | controller, work-selection policy, one-time host installer and label migration | no: human-authorized governance PR only; agents never run the installer's `--apply`/`--rollback` | executed by the controller / owner |
| `scripts/autonomous-loop.sh`, `scripts/v5_work_selection.py` | retained V5 loop and selector (rollback only) | no | never |
| `V4_CYCLE_PROMPT.md` | historical V4 controller prompt | no | never |
| V4_SCOPE, V4_DEFINITION_OF_DONE | locked, historical V4 contract | no | V4 semantics changes, UI work, release |
| V4_ISSUES_WORKFLOW | locked, historical | no | only to interpret historical V4 Issue evidence |
| V4_DESIGN_BRIEF + `docs/design/reference/tendo-v4-{desktop,mobile}.png` | historical V4 visual direction; superseded by V5_DESIGN_BRIEF | no | optional historical context only |
| `CYCLE_PROMPT.md` | historical V3 controller prompt, human-authored | no | never |
| GUARDRAILS, CODE_QUALITY | locked, `agent_must_not_edit=this_file` | no | always |
| CHARTER, PRODUCT, ARCHITECTURE, API, TESTING | locked by `AGENTS.md` `must_not_self_change` | no | always |
| UX, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE | locked (V3_DESIGN_BRIEF also `agent_must_not_edit`) | no | UX, V3_SCOPE and V3_DEFINITION_OF_DONE with the V5 brief for UI work; V3 semantics changes |
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
| PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE, V3_COMPLETE, V4_COMPLETE, V5_COMPLETE (if added) | historical completion markers / evidence | never edit or delete | never for routine work; never stop the controller |
| `docs/development/dependencies.md`, `release.md`, `release-qualification.md` | agent-maintained development docs | yes | dependency/toolchain/security work; releases |

Precedence when two sources seem to disagree: locked files over FLOW and AGENTS, except where AGENTS "Evergreen precedence" (owner-authorized) replaces the V5 ownership selector, controller handoff and stop rule; V5_ISSUES_WORKFLOW over other statements about V5 acceptance and Renovate triage, V4_ISSUES_WORKFLOW and V3_ISSUES_WORKFLOW over every statement about historical V4 and V3 Issues; V5_DESIGN_BRIEF over the V4 and V3 briefs for visual direction; V5_RECURRENCE_CONTRACT and V5_SECURITY_AND_MCP over earlier statements on their concerns, and the V5 precedence in `AGENTS.md` for earlier exclusions of authorized V5 deliverables; the newer V3 documents refine but never relax the historical V1/V2/V2.1 invariants (GUARDRAILS "Scope safety").

## Evergreen controller runbook

The owner-authorized evergreen governance PR added the controller, selector, prompt, one-time installer and their tests. It changed no host state. The owner performs cutover once, after the PR merges and required CI on `main` is green, from the canonical checkout synced to `origin/main`:

```bash
bash scripts/install-autonomy-service.sh --check   # redacted dry run; refuses on active work, pending marker, dirty/unsynced checkout, red CI, unknown drop-ins
bash scripts/install-autonomy-service.sh --apply   # backup (0700) → quiesce → labels add release / remove tendo:v5-autonomous → new unit → timer disabled → start + verify
bash scripts/install-autonomy-service.sh --rollback  # restore units, drop-ins, timer state and labels from the latest backup
```

`--apply` is idempotent: a completed migration exits "already complete", and an interrupted one resumes from its backup. A pending safe-wrapper marker (`~/.local/state/tendo-cycle.pending`) blocks `--check`; the printed remediation explains how the owner inspects it and, only if that cycle's work is verifiably finished, archives it. Monitoring:

```bash
systemctl --user status tendo-autonomous.service
journalctl --user -u tendo-autonomous.service -f        # "[autonomy] ..." lines; one DECISION per cycle
systemctl --user is-enabled tendo-maintenance.timer     # disabled after cutover
```

The unit runs with `Restart=on-failure`, `RestartSec=60`, `KillMode=control-group` and `TimeoutStopSec=120`, and survives reboot via user linger. A busy lock exits 75 and is retried. Cycle failures, selection errors and rate limits back off inside the process. The legacy `20-safe.conf` `Restart=no` drop-in is removed at cutover; it was why the V5 loop stayed failed after a stuck wrapper marker.

Known limitation: the host wrapper `~/.local/bin/opencode-tendo-safe` (outside this repository) embeds a recovery prompt that still mentions `.autonomous/STATE.md`; under AGENTS "Context rules" that means the owned `release` Issue.

## V5 activation (historical)

V5-00 (#444) imported the seven human-approved V5 specifications, switched the read set, FLOW and the loop to V5 and kept the maintenance timer enabled with a shared flock. The V5 loop stopped at `V5_COMPLETE`. The evergreen governance PR superseded that handoff; the V5 loop files are retained for rollback.

## V4 activation (historical)

V4-00 (#408) imported the V4 specifications and reference images and ran V4 with the maintenance timer disabled; the V4 loop refused to start while the timer was enabled. That rule ended with V4; the V4 loop and runbook are preserved at tag `v4.0.0`.

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
