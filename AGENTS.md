# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=ship_v3_everyday_elegance
main_policy=PR_only
human_intervention=routine_not_required

## Read set
always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,TESTING,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,CODE_QUALITY,FLOW,LEARNINGS,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE,V3_ISSUES_WORKFLOW
when_frontend=UX
when_http_or_client=API
when_docs_or_user_setup=DOCUMENTATION
when_platform_or_future_seam=ROADMAP

Paths are `.autonomous/<NAME>.md`.
If a change crosses concerns, read every applicable locked file before editing.

## Tooling
toolchain=`bash scripts/dev-exec.sh -- <command>` runs with the exact `toolchain.json` pins from the user cache; do not install or rely on host Go/Node/sqlc/Playwright.
full_gate=`bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (needs `TENDO_TEST_POSTGRES_ADMIN_URL`; see docs/development/testing.md)
dependency_or_renovate_PR=FLOW "Dependency maintenance"

## Context rules
- English only in agent context/code/docs unless user-facing localization requires otherwise.
- Prefer `key=value`, tables, terse bullets, stable IDs.
- One canonical location per durable fact.
- Work status, claims, handoff, blockers and progress live ONLY in managed V3
  GitHub Issues (V3_ISSUES_WORKFLOW). No STATE.md or substitute state file.
- LEARNINGS=durable rules only; no session narrative.
- Do not duplicate locked specifications into Issues; reference them.
- Clarity > compression. Never invent cryptic shorthand.

## Authority
may=plan,code,test,review,refactor,document,create_branch,push_branch,open_PR,enable_auto_merge_when_green,create_annotated_release_tag_after_green_main
may_self_improve=AGENTS,FLOW,LEARNINGS,dev_tooling,tests,CI,docs_ADRs
must_not_self_change=GUARDRAILS,CHARTER,PRODUCT,ROADMAP,UX,ARCHITECTURE,API,TESTING,DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,CODE_QUALITY,systemd_runner,auth_credentials,global_opencode_config,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE,V3_ISSUES_WORKFLOW
must_not=direct_commit_main,direct_push_main,force_push,history_rewrite,bypass_required_checks,merge_known_broken,scope_expand_v3

## Completion
V1, V2 and V2.1 are historical completed, released stages; preserve all
completion markers and original release tags. V3 is governed by V3_SCOPE,
V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE, UX, and permanent CODE_QUALITY.
Reconcile V3 work solely through Issues in `bigtcze/tendo` that are in the
exact milestone `Tendo v3.0.0 - Everyday Elegance` AND carry ownership label
`tendo:v3-autonomous` (both filters, every query and mutation), as locked
V3_ISSUES_WORKFLOW requires; identifiers are in `.autonomous/V3_ISSUES_READY`.
Ignore all unlabelled, external or other user Issues; never adopt them. Create
new V3 Issues only with milestone + ownership label + `status:idle` at creation.
Only after the managed V3 issues, design, functional, accessibility and release
checks are proven,
publish and VERIFY GitHub Release v3.0.0 with public multi-arch GHCR images,
real release assets and clean-install / v2.1.0-upgrade evidence. Only then
merge V3_COMPLETE through protected PR and stop on clean main.
