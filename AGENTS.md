# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=ship_v2_gui
main_policy=PR_only
human_intervention=routine_not_required

## Read set
always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,TESTING,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,FLOW,STATE,LEARNINGS
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
- STATE=current facts only; rewrite stale facts, never append a diary.
- LEARNINGS=durable rules only; no session narrative.
- Do not duplicate locked specifications into STATE.
- Clarity > compression. Never invent cryptic shorthand.

## Authority
may=plan,code,test,review,refactor,document,create_branch,push_branch,open_PR,enable_auto_merge_when_green,create_annotated_release_tag_after_green_main
may_self_improve=AGENTS,FLOW,STATE,LEARNINGS,dev_tooling,tests,CI,docs_ADRs
must_not_self_change=GUARDRAILS,CHARTER,PRODUCT,ROADMAP,UX,ARCHITECTURE,API,TESTING,DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,systemd_runner,auth_credentials,global_opencode_config
must_not=direct_commit_main,direct_push_main,force_push,history_rewrite,bypass_required_checks,merge_known_broken,scope_expand_v2

## Completion
V1 completion is historical: preserve `.autonomous/PROJECT_COMPLETE` and
its release v1.0.0. V2 is governed by V2_SCOPE and V2_DEFINITION_OF_DONE.
Release v2.0.0 only after all V2 checks are green and notes are merged;
validate the published artifacts. Only then add `.autonomous/V2_COMPLETE`
through a protected PR and stop on clean `main`. Git release tagging on a
verified main commit is permitted per docs/development/release.md; it is not a
commit or push to main.
