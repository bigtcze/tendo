# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=ship_v3_everyday_elegance
main_policy=PR_only
human_intervention=routine_not_required

## Read set
OpenCode already injects this file into every session; do not re-read it.
Paths are `.autonomous/<NAME>.md`. Read each file once per session; re-read
only after it changes (new branch content, merge, edit). Do not read files
outside the matching set for routine work (rationale for maintainers of this
read set only: `docs/development/agent-context.md`).

always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,API,UX,TESTING,CODE_QUALITY,FLOW,LEARNINGS,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE,V3_ISSUES_WORKFLOW
when_docs_or_user_setup=DOCUMENTATION
when_platform_or_future_seam=ROADMAP
when_release_or_completion=DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE
when_removing_skipping_relaxing_or_relocating_existing_tests_CI_gates_or_release_qualification=DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE
when_intentionally_changing_documented_V1_V2_V2_1_product_or_API_semantics=DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE
when_dependency_or_renovate_PR=FLOW "Dependency maintenance", then `docs/development/dependencies.md` "Dependency PR procedure"
never_needed_for_routine_work=CYCLE_PROMPT (already the session prompt), V3_ISSUES_MIGRATION_PROMPT (executed one-shot bootstrap), PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE (historical markers; never edit or delete)

The historical V1/V2/V2.1 scope and DoD files stay locked and binding: their
invariants are already restated in the always set and enforced by the
required test suite. Routine V3 UI work, bug fixes that restore documented
behavior, and adding or repairing tests without relaxing them do not trigger
them. If a change crosses concerns, or you are unsure whether a conditional
trigger applies, read every applicable locked file before editing.

## Tooling
toolchain=`bash scripts/dev-exec.sh -- <command>` runs with the exact `mise.toml` pins, checksum-verified against `mise.lock`, from a private user cache; needs user-level mise >= `min_version`; do not install or rely on host Go/Node/sqlc.
full_gate=`bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (needs `TENDO_TEST_POSTGRES_ADMIN_URL`; see docs/development/testing.md)

## Context rules
- English only in agent context/code/docs unless user-facing localization requires otherwise.
- Prefer `key=value`, tables, terse bullets, stable IDs.
- One canonical location per durable fact; elsewhere reference it.
- Work status, claims, handoff, blockers and progress live ONLY in managed V3
  GitHub Issues (V3_ISSUES_WORKFLOW). No STATE.md or substitute state file.
- Legacy locked references to recording evidence/blockers "in STATE" (e.g.
  GUARDRAILS controller-tooling rule) now mean a comment on the owned managed
  Issue (milestone + `tendo:v3-autonomous` rechecked first). Every other
  part of those rules, including the controller configuration ban, still applies.
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
Managed V3 work = Issues in `bigtcze/tendo`, milestone
`Tendo v3.0.0 - Everyday Elegance` AND label `tendo:v3-autonomous` (both
filters on every query and mutation; identifiers in `.autonomous/V3_ISSUES_READY`).
Never adopt or touch other Issues. Rules: V3_ISSUES_WORKFLOW; procedure: FLOW.
V3_COMPLETE only via the FLOW "V3 release and completion protocol", after the
published and verified v3.0.0 release.
