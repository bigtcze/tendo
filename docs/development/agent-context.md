# Agent context map

`AGENTS.md` is the entry and canonical read-set/compatibility map. This document explains the layout; it is not another authority or work ledger.

## Constant-size governance

| Concern | Canonical location |
| --- | --- |
| Authority, ownership, historical-reference resolution | `AGENTS.md`, `.autonomous/GUARDRAILS.md` |
| Product intent and supported behavior | `.autonomous/CHARTER.md`, `.autonomous/PRODUCT.md` |
| Calendar recurrence, golden cases, completion and undo | `.autonomous/RECURRENCE.md` |
| Identity, OIDC, households, invitations, MCP | `.autonomous/SECURITY.md` |
| Module boundaries, deployment and API conventions | `.autonomous/ARCHITECTURE.md`, `.autonomous/API.md` |
| Current visual direction and accessibility | `.autonomous/UX.md` |
| Verification and release quality | `.autonomous/TESTING.md`, `.autonomous/CODE_QUALITY.md`, `.autonomous/DEFINITION_OF_DONE.md` |
| Human documentation and future seams | `.autonomous/DOCUMENTATION.md`, `.autonomous/ROADMAP.md` |
| Execution, scheduling, claims, recovery, releases | `.autonomous/FLOW.md` |
| Permanent cycle prompt | `.autonomous/AUTONOMY_PROMPT.md` |
| Durable reusable experience | `.autonomous/LEARNINGS.md` |

Read only the always set plus concern triggers. Dependency pin changes with unchanged rendered behavior do not require visual contracts; a frontend migration or visual-baseline change does. If a change crosses concerns, all matching contracts bind. A file need not be reloaded while its verbatim requirements remain available and unchanged; compaction or uncertainty requires a fresh read.

Technical contracts are locked against routine autonomous amendment. The owner may authorize a protected governance PR; consolidating their location does not relax their behavior. FLOW and AGENTS may improve navigation/process but cannot grant themselves new product, security or infrastructure authority.

## Existing and future release Issues

Ownership is only the exact `release` label on an Issue in `bigtcze/tendo`. Milestones, labels from old stages and title versions are not authority. Existing Issue references to retired versioned files resolve through the compatibility table in AGENTS. Their acceptance survives intact, including recurrence golden cases, OIDC provenance, migration safety, real-DB testing and production visual evidence. No edit to an in-progress Issue is needed to resolve a document name.

New release deliverables and acceptance live in ordinary owned Issues; release notes live in `docs/releases/`. Do not add another release-specific scope packet, prompt, workflow or acceptance database. Historical specifications remain available through Git and original release tags rather than copied into an archive. Historical audit/design documents link those original tagged contracts and do not claim their measurements describe current main.

Claims, blockers, review and test evidence live only in owned Issues and PRs. Dependency triage lives on its PR and Renovate Dashboard. Repository docs are contracts or durable references, never a mirror of mutable work status.

## Permanent controller and recovery

`tendo-autonomous.service` runs `scripts/autonomy-controller.sh`; one serialized process starts one fresh session per actionable selection. `.autonomous/AUTONOMY_PROMPT.md` plus live read-only selection advice is the prompt. No completion marker stops the controller. A future release number requires no code or host reconfiguration.

`scripts/autonomy-work-selection.py` derives fairness from merged PR history and readiness from live owned Issues, dependencies and complete comments. `scripts/autonomy_dependencies.py` parses claims/blockers and diagnoses narrowly eligible owned dependency recovery. Advice is not mutation or authority: the claimant must reconcile live evidence and follow FLOW before any ordering adjustment. Genuine external blockers remain skipped until relevant facts change.

`bash scripts/autonomy-test.sh` runs offline tests with fakes and disposable repositories. It is part of local and required CI gates; it never calls the real host installer or systemd. Tests cover normal selection, fairness, ownership, claims, recovery, restart, markers and rollback readiness. Protected PR review and green required CI still gate delivery.

Obsolete per-stage runtime files are removed from current main. An owner-only installer rollback checks that the backed-up executable and required files are available before stopping services or changing labels/units. If not, it refuses and identifies the backup's historical recovery revision. The owner must deliberately recover a compatible historical checkout; restoring unit text alone is not a runnable rollback. Agents never run `--apply` or `--rollback`, change host configuration or interfere with active work.
