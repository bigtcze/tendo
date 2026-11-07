# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=post_v3_dependency_security_and_compatibility_maintenance
stage=maintenance_after_v3.0.0
main_policy=PR_only
human_intervention=routine_not_required

## Read set
OpenCode already injects this file into every session; do not re-read it.
Paths are `.autonomous/<NAME>.md`. Avoid redundant reads: a file already
read in this session and unchanged since (no checkout, merge or edit) need not
be read again. DO re-read when context was compacted or truncated, when a
governing requirement is no longer verbatim in view, or when unsure what it
says; avoiding redundant reads never overrides having the rules available.
Do not load files outside the matching set for routine work (rationale, for
maintainers of this read set only: `docs/development/agent-context.md`).

always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,API,TESTING,CODE_QUALITY,FLOW,LEARNINGS
when_dependency_or_renovate_PR_or_toolchain_or_security_update=FLOW "Dependency maintenance", then `docs/development/dependencies.md` "Dependency PR procedure"
when_frontend_UI_rendering_or_visual_baseline_change=UX,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE
when_docs_or_user_setup=DOCUMENTATION
when_platform_or_future_seam=ROADMAP
when_lists_search_filter_sort_pagination_counts_or_large_data_UI_or_API=V2_SCOPE,V2_DEFINITION_OF_DONE
when_test_harness_CI_local_gate_E2E_infrastructure_release_qualification_maintainability_refactoring_or_removing_skipping_relaxing_existing_tests=V2_1_SCOPE,V2_1_DEFINITION_OF_DONE
when_item_or_subject_forms_recurrence_lifecycle_history_auth_household_onboarding_security_deployment_or_backup=DEFINITION_OF_DONE
when_changing_documented_V1_V2_V2_1_V3_product_or_API_semantics=DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,V3_SCOPE,V3_DEFINITION_OF_DONE
when_release=FLOW "Maintenance release",DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE, then `docs/development/release.md` and `docs/development/release-qualification.md`
when_historical_V3_Issue_context_needed=V3_ISSUES_WORKFLOW (read-only reference; see "V3 Issues" below)
never_needed_for_routine_work=CYCLE_PROMPT (historical V3 controller prompt), V3_ISSUES_MIGRATION_PROMPT (executed one-shot bootstrap), V3_ISSUES_READY (identifiers only), PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE, V3_COMPLETE (historical markers; never edit or delete)

The historical V1/V2/V2.1/V3 scope, design and DoD files stay locked and
binding on all work. They are loaded by concern (triggers above) because many
of their contracts are specific, e.g. full-dataset search and bounded fetching
(V2), native no-Docker integration/E2E (V2.1), visual regression, contrast,
light/dark and adaptive layout of the shipped UI (V3). A dependency update
that only changes pins and passes the unchanged gate needs no UI trigger; one
that changes rendered UI, frontend code or visual baselines does. Adding
ordinary tests inside the existing harness is governed by the always set
(TESTING, GUARDRAILS). If a change crosses concerns, or you are unsure whether
a trigger applies, read every applicable locked file before editing.

## Tooling
toolchain=`bash scripts/dev-exec.sh -- <command>` runs with the exact `mise.toml` pins, checksum-verified against `mise.lock`, from a private user cache; needs user-level mise >= `min_version`; do not install or rely on host Go/Node/sqlc.
full_gate=`bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (needs `TENDO_TEST_POSTGRES_ADMIN_URL`; see docs/development/testing.md)

## Context rules
- English only in agent context/code/docs unless user-facing localization requires otherwise.
- Prefer `key=value`, tables, terse bullets, stable IDs.
- One canonical location per durable fact; elsewhere reference it.
- Maintenance status, decisions, evidence and blockers live in the PR
  itself (description and comments), the Renovate Dependency Dashboard, and
  published releases. No STATE.md or substitute state file, no new tracking
  labels, Issues queues or coordination frameworks.
- Legacy locked references to recording evidence/blockers "in STATE" (e.g.
  GUARDRAILS controller-tooling rule) now mean a comment on the affected
  maintenance PR, or the session report when no PR exists. Every other part
  of those rules, including the controller configuration ban, still applies.
- LEARNINGS=durable rules only; no session narrative.
- Do not duplicate locked specifications into PRs or docs; reference them.
- Clarity > compression. Never invent cryptic shorthand.

## Authority
may=plan,code,test,review,document,create_branch,push_branch,open_PR,update_Renovate_PR_branch,merge_when_required_checks_green,create_annotated_release_tag_after_green_main
may_self_improve=AGENTS,FLOW,LEARNINGS,dev_tooling,tests,CI,docs_ADRs
must_not_self_change=GUARDRAILS,CHARTER,PRODUCT,ROADMAP,UX,ARCHITECTURE,API,TESTING,DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,CODE_QUALITY,systemd_runner,auth_credentials,global_opencode_config,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE,V3_ISSUES_WORKFLOW,CYCLE_PROMPT,autonomous_loop_script,maintenance_controller
must_not=direct_commit_main,direct_push_main,force_push,history_rewrite,bypass_required_checks,merge_known_broken,feature_development,reactivate_V3_backlog,claim_or_mutate_V3_Issues,speculative_refactoring,casual_Renovate_policy_change,move_or_reuse_tags,empty_or_documentation_only_release

## Completion
V1, V2, V2.1 and V3 are historical completed, released stages; preserve all
completion markers (`PROJECT_COMPLETE`, `V2_COMPLETE`, `V2_1_COMPLETE`,
`V3_COMPLETE`) and original release tags. The current stage is ongoing
maintenance of the released product: dependency, toolchain, security and
compatibility updates plus maintenance patch releases, per FLOW. It has no
completion marker and no backlog.

## V3 Issues
The V3 milestone `Tendo v3.0.0 - Everyday Elegance` is closed work.
Maintenance never queries it for work, and never claims, labels, comments
on, reopens or closes any V3 milestone Issue, managed or not. Do not create new
`tendo:v3-autonomous` Issues. Read V3_ISSUES_WORKFLOW only to interpret
historical evidence; its rules still bind anyone who touches those Issues.
