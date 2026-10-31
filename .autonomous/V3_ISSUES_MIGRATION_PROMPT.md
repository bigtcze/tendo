ONE-TIME TENDO V3 GITHUB ISSUES MIGRATION / BOOTSTRAP. This is NOT a design
implementation cycle. The existing autonomous systemd loop is STOPPED and must
stay stopped until the calling bootstrap verifies the migration.

Read AGENTS.md, V3_SCOPE, V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE, FLOW,
GUARDRAILS, CODE_QUALITY, V3_ISSUES_WORKFLOW, STATE.md, and relevant release
and testing docs. Work as the existing OpenCode orchestrator, with existing
opencode-tendo-safe session safety and gh authenticated on host. Do NOT touch
controller/service configuration. Repository: bigtcze/tendo. Branch PR only;
never commit/push main directly. Complete this one-shot migration, then EXIT.

STEP 1: ACCURATELY MIGRATE EXISTING STATE, NOT BLINDLY DUMP THE DIARY.
- Inspect STATE.md and the actual repo, release v2.1.0, V2_1_COMPLETE,
  last PRs and existing issues. Preserve current useful facts, unresolved
  risks, historical references and decisions in appropriate canonical locked
  docs or a closed migration issue; do not copy stale facts or secrets.
- Do not erase unfinished work. Historical completed V1/V2/V2.1 work remains
  reachable from git and release pages; no replacement STATE.md is permitted.

STEP 2: CREATE COMPLETE GITHUB RELEASE BACKLOG, NOT ONE GIANT TICKET.
- Create/reuse a release milestone EXACTLY named:
  `Tendo v3.0.0 - Everyday Elegance`.
- First CREATE/VERIFY the dedicated ownership label `tendo:v3-autonomous`
  in `bigtcze/tendo` (color/description may be chosen appropriately); create
  or reuse `status:idle`, `status:in-progress`, `status:blocked` separately.
  The ownership label is distinct from mutable status:* labels.
- Create ONE new AGENT-OWNED issue `V3-00` for STATE.md handoff, with the V3
  milestone AND `tendo:v3-autonomous` at creation, recording accurate V2.1
  baseline/release/risks and canonical links. Close with evidence only after
  the migration is complete; NO progress journal file.
- Create exactly one execution Issue per ID below. Each must be created with
  BOTH the V3 milestone and ownership label `tendo:v3-autonomous`, plus
  `status:idle` at creation, and initially OPEN. Use `gh issue create` with
  `--label tendo:v3-autonomous --label status:idle --milestone ...`.
  Search existing managed Issues for idempotent recovery. If a V3-XX Issue
  already exists WITHOUT the ownership label, leave it alone: do NOT adopt,
  label, comment, or modify it. Record naming conflict and halt if needed.
  Never create duplicate managed IDs; search all milestone Issues for title
  collisions before creating the canonical issues.
  Each body must have specific deliverables, acceptance criteria, evidence,
  dependencies, impact and relevant DoD references. Fill out scope thoroughly.
  V3-01 Actual shipped V2.1 UX audit and route/state inventory
  V3-02 Baseline real-browser screenshot evidence, metrics, novice journeys
  V3-03 Interactive design direction A (real app prototype, phone/desktop)
  V3-04 Interactive design direction B (distinct real app prototype)
  V3-05 Independent critique, tradeoffs, chosen household-oriented direction
  V3-06 Tokens, typography, reusable design system and design guide
  V3-07 Responsive application shell, header, navigation and layout
  V3-08 Home and task/attention group layouts and interactions
  V3-09 All Items, filters/search/sort and 1000+ backlog presentation
  V3-10 Create/edit/detail, repeat/fixed/fluid, completion/undo/history
  V3-11 People and things, including empty/archived/large data views
  V3-12 Members, invitations, permissions and household UX
  V3-13 First-run, login, OIDC, account and recovery/error screens
  V3-14 Light/dark/system themes, preference, CZ/EN and microcopy
  V3-15 WCAG, keyboard, touch, zoom, responsive and cross-browser review
  V3-16 Visual regressions and V2.1 functional/security/E2E preservation
  V3-17 Final critical visual review, README authentic UI image, docs
  V3-18 Professional GitHub Release v3.0.0 and public GHCR verification
  V3-19 Final protected V3_COMPLETE PR and clean main shutdown
- Depend on obvious prerequisites, especially baseline -> directions ->
  direction choice -> design system -> rollout -> final validation -> release
  -> completion. Record dependencies as issue links/plain #IDs; do NOT
  install project managers, schedulers, bots, or new services.
- Explicitly cover every locked V3 DoD item in managed issue bodies. Create
  additional managed, labelled V3 issues only when justified; retain 01-19
  canonical IDs. Never include user/third-party Issues in this backlog.
- `V3-18` release must remain OPEN until the GitHub Release actually exists,
  assets are public and correct, both GHCR architectures work and released
  clean install and v2.1 upgrade keep user data intact.

STEP 3: MIGRATE THE AUTONOMOUS INSTRUCTIONS AND REMOVE STATE.md VIA PR.
- In ONE focused, protected bootstrap-migration PR, update AGENTS.md,
  .autonomous/FLOW.md, .autonomous/CYCLE_PROMPT.md, and if needed
  other agent-owned context to make GitHub Issues the ONLY ongoing source of
  work status, claims, handoff and progress. CRITICAL: update all required
  bootstrap file-checks in `.github/workflows/required.yml` and
  `scripts/verify-local.sh`: they CURRENTLY explicitly require STATE.md.
  Replace the old STATE requirement with V3_ISSUES_READY and
  V3_ISSUES_WORKFLOW, and update `scripts/test_verify_local.py` CONTEXT_FILES
  fixture accordingly. Inspect any other tests referencing STATE and adapt
  meaningfully, never weaken CI. Run verification to PROVE these updates.
  Remove STATE from the always-read
  list and from all current cycle reconciliation rules. Read the LOCKED
  V3_ISSUES_WORKFLOW at every cycle. Do NOT weaken GUARDRAILS, architecture,
  quality, product DoD or release requirements. Never self-edit locked files.
- `git rm .autonomous/STATE.md` only AFTER migrating important outstanding
  facts into appropriate durable sources/tickets. Do not create a substitute
  STATE file (README, JSON, YAML, another markdown, etc.). Documentation may
  contain stable decisions/test evidence, never mutable execution progress.
- Add a small permanent `.autonomous/V3_ISSUES_READY` marker through that PR,
  containing `managed_issue_label=tendo:v3-autonomous`, milestone URL/number,
  closed owned V3-00 and owned V3-01..V3-19 issue numbers (identifiers only,
  NOT mutable progress). DO NOT record status snapshots in this file.
- Ensure AGENTS/FLOW/CYCLE_PROMPT apply the strict issue ownership filter:
  BOTH milestone and `tendo:v3-autonomous`, never unlabelled/external Issues.
  Agent may create a new in-scope Issue ONLY with the label at creation.
  Then claim, set in-progress, wait/re-read 3–5 s and resolve collision, use
  focused PR/green main, close only after acceptance, continue to release.
- Preserve the original scripts/autonomous-loop.sh completion check on
  .autonomous/V3_COMPLETE and existing systemd/OpenCode settings.
- Run relevant complete local gates, open PR, wait for required CI, merge
  ONLY through protected main and verify green main. You may use existing
  reviewer/oracle appropriately. Do NOT start regular V3 design work yet.

STEP 4: VERIFY AND EXIT.
- Confirm clean main, tracked V3_ISSUES_READY declares the exact managed
  label, STATE.md absent, AGENTS/FLOW/CYCLE_PROMPT have strict milestone+label
  scope, all 19 managed V3 Issues exist OPEN/idle and V3-00 closed with proof.
  Verify that unlabelled user/third-party Issues remain untouched.
- Leave no open bootstrap PR, no broken CI, no pending migration cycle; do
  not mark V3_COMPLETE. Return factual summary with issue URLs and main SHA.
- Calling bash bootstrap, NOT you, will verify success and start the
  EXISTING tendo-autonomous.service. Do not start the service yourself.
