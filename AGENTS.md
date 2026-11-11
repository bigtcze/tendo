# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=ship_v5_refined_reliable
stage=v5_refined_reliable_after_v4.0.0
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

always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,API,TESTING,CODE_QUALITY,FLOW,LEARNINGS,V5_SCOPE,V5_RECURRENCE_CONTRACT,V5_SECURITY_AND_MCP,V5_DEFINITION_OF_DONE,V5_ISSUES_WORKFLOW
when_frontend_UI_design_tokens_rendering_or_visual_baseline_change=V5_DESIGN_BRIEF,UX,V4_SCOPE,V4_DEFINITION_OF_DONE,V3_SCOPE,V3_DEFINITION_OF_DONE (V4_DESIGN_BRIEF/V3_DESIGN_BRIEF and `docs/design/reference/tendo-v4-*.png` only as historical context; they are not V5 visual targets or production evidence)
when_recurrence_completion_undo_schedule_or_item_lifecycle_change=V5_RECURRENCE_CONTRACT (always loaded) plus DEFINITION_OF_DONE
when_identity_household_membership_roles_admin_password_invitation_OIDC_session_or_MCP_change=V5_SECURITY_AND_MCP (always loaded) plus V4_SCOPE,V4_DEFINITION_OF_DONE,DEFINITION_OF_DONE
when_controller_systemd_maintenance_handoff_or_work_selection=FLOW "V5 controller and maintenance handoff", V5_ISSUES_WORKFLOW "Maintenance controller / systemd handoff"
when_dependency_or_renovate_PR_or_toolchain_or_security_update=FLOW "Dependency maintenance", then `docs/development/dependencies.md` "Dependency PR procedure"
when_docs_or_user_setup=DOCUMENTATION
when_platform_or_future_seam=ROADMAP
when_lists_search_filter_sort_pagination_counts_or_large_data_UI_or_API=V2_SCOPE,V2_DEFINITION_OF_DONE
when_test_harness_CI_local_gate_E2E_infrastructure_release_qualification_maintainability_refactoring_or_removing_skipping_relaxing_existing_tests=V2_1_SCOPE,V2_1_DEFINITION_OF_DONE
when_item_or_subject_forms_recurrence_lifecycle_history_auth_household_onboarding_security_deployment_or_backup=DEFINITION_OF_DONE
when_changing_documented_V1_V2_V2_1_V3_V4_product_or_API_semantics_including_V4_roles_identity_people_invitations_or_deployment=DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,V3_SCOPE,V3_DEFINITION_OF_DONE,V4_SCOPE,V4_DEFINITION_OF_DONE
when_release=FLOW "V5 release and completion" and "Maintenance release",DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,V4_SCOPE,V4_DEFINITION_OF_DONE, then `docs/development/release.md` and `docs/development/release-qualification.md`
when_historical_V3_or_V4_Issue_context_needed=V3_ISSUES_WORKFLOW or V4_ISSUES_WORKFLOW (read-only reference; see "Historical V3 and V4 Issues" below)
never_needed_for_routine_work=V5_CYCLE_PROMPT (already the V5 controller session prompt), V4_CYCLE_PROMPT (historical V4 controller prompt), CYCLE_PROMPT (historical V3 controller prompt), V3_ISSUES_MIGRATION_PROMPT (executed one-shot bootstrap), V3_ISSUES_READY (identifiers only), PROJECT_COMPLETE, V2_COMPLETE, V2_1_COMPLETE, V3_COMPLETE, V4_COMPLETE (historical markers; never edit or delete)

The historical V1/V2/V2.1/V3/V4 scope, design and DoD files stay locked and
binding on all work, except where the V5 precedence below lets an explicitly
authorized V5 contract replace them. They are loaded by concern (triggers above) because many
of their contracts are specific, e.g. full-dataset search and bounded fetching
(V2), native no-Docker integration/E2E (V2.1), visual regression, contrast,
light/dark and adaptive layout of the shipped UI (V3). A dependency update
that only changes pins and passes the unchanged gate needs no UI trigger; one
that changes rendered UI, frontend code or visual baselines does. Adding
ordinary tests inside the existing harness is governed by the always set
(TESTING, GUARDRAILS). If a change crosses concerns, or you are unsure whether
a trigger applies, read every applicable locked file before editing.

V5 precedence: the human-authorized locked V5_SCOPE, V5_RECURRENCE_CONTRACT,
V5_DESIGN_BRIEF, V5_SECURITY_AND_MCP, V5_DEFINITION_OF_DONE,
V5_ISSUES_WORKFLOW and V5_CYCLE_PROMPT govern V5 work.
- Visual direction: V5_DESIGN_BRIEF governs every NEW visual and supersedes
  the V4 and V3 briefs and the V4 reference images (historical context only).
- Recurrence: V5_RECURRENCE_CONTRACT governs schedule semantics, completion,
  undo and idempotency; a collision with an earlier invariant is handled as
  the contract says (failing test, reviewed compatibility/migration decision),
  never by silently overriding historical data.
- Identity, households, roles, password change, invitations and MCP:
  V5_SECURITY_AND_MCP governs.
- Earlier exclusions do not block explicitly authorized V5 deliverables:
  V4_SCOPE "Explicitly out of scope" (new integrations, unrelated
  authentication methods, new features), PRODUCT "v2plus" `multi_household_UI`
  and ROADMAP "Multi-household UI" yield to V5_SCOPE for exactly its
  deliverables (multi-household membership/switching/admin center, instance
  administrator, local password change and CLI reset, invitation cleanup,
  configurable Compose host IP, per-user OAuth-protected MCP). Everything else
  they exclude stays excluded, including V5_SCOPE "Non-negotiable boundaries"
  (no notifications/delivery, AI, telemetry, native app, off-topic
  integrations or framework rewrite).
- Every other V1/V2/V2.1/V3/V4 invariant (behavior, security, data,
  accessibility, CZ/EN, light/dark, test strength, two-service Compose) still
  binds. Where locked GUARDRAILS speaks of V3 Issues, its ownership, claim and
  status rules apply to V5 with the V5 milestone and `tendo:v5-autonomous`
  label.

## Tooling
toolchain=`bash scripts/dev-exec.sh -- <command>` runs with the exact `mise.toml` pins, checksum-verified against `mise.lock`, from a private user cache; needs user-level mise >= `min_version`; do not install or rely on host Go/Node/sqlc.
full_gate=`bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (needs `TENDO_TEST_POSTGRES_ADMIN_URL`; see docs/development/testing.md)

## Context rules
- English only in agent context/code/docs unless user-facing localization requires otherwise.
- Prefer `key=value`, tables, terse bullets, stable IDs.
- One canonical location per durable fact; elsewhere reference it.
- V5 work status, claims, handoff, evidence and blockers live ONLY in
  managed V5 GitHub Issues (V5_ISSUES_WORKFLOW) and their PRs; dependency
  status and triage in its PR and the Renovate Dependency Dashboard. No STATE.md or
  substitute state file, no new tracking labels, queues, leases or
  coordination frameworks.
- Legacy locked references to recording evidence/blockers "in STATE" (e.g.
  GUARDRAILS controller-tooling rule) now mean a comment on the owned managed
  V5 Issue (milestone + `tendo:v5-autonomous` rechecked first), else on the
  affected PR. Every other part of those rules, including the controller
  configuration ban, still applies.
- LEARNINGS=durable rules only; no session narrative.
- Do not duplicate locked specifications into PRs or docs; reference them.
- Clarity > compression. Never invent cryptic shorthand.

## Authority
may=plan,code,test,review,document,create_branch,push_branch,open_PR,update_Renovate_PR_branch,merge_when_required_checks_green,create_annotated_release_tag_after_green_main
may_self_improve=AGENTS,FLOW,LEARNINGS,dev_tooling,tests,CI,docs_ADRs
must_not_self_change=GUARDRAILS,CHARTER,PRODUCT,ROADMAP,UX,ARCHITECTURE,API,TESTING,DOCUMENTATION,DEFINITION_OF_DONE,V2_SCOPE,V2_DEFINITION_OF_DONE,V2_1_SCOPE,V2_1_DEFINITION_OF_DONE,CODE_QUALITY,systemd_runner,auth_credentials,global_opencode_config,V3_SCOPE,V3_DESIGN_BRIEF,V3_DEFINITION_OF_DONE,V3_ISSUES_WORKFLOW,CYCLE_PROMPT,V4_SCOPE,V4_DESIGN_BRIEF,V4_DEFINITION_OF_DONE,V4_ISSUES_WORKFLOW,V4_CYCLE_PROMPT,V4_reference_images,V5_SCOPE,V5_RECURRENCE_CONTRACT,V5_DESIGN_BRIEF,V5_SECURITY_AND_MCP,V5_DEFINITION_OF_DONE,V5_ISSUES_WORKFLOW,V5_CYCLE_PROMPT,autonomous_loop_script,v5_work_selection_policy,maintenance_controller
must_not=direct_commit_main,direct_push_main,force_push,history_rewrite,bypass_required_checks,merge_known_broken,feature_work_outside_V5_SCOPE,notification_settings_or_delivery,work_on_unmanaged_Issues,reactivate_V3_or_V4_backlog,claim_or_mutate_V3_or_V4_Issues,speculative_refactoring,casual_Renovate_policy_change,move_or_reuse_tags,empty_or_documentation_only_release,edit_historical_COMPLETE_markers,create_V5_COMPLETE_outside_V5-17

## Completion
V1, V2, V2.1, V3 and V4 are historical completed, released stages; preserve
all completion markers (`PROJECT_COMPLETE`, `V2_COMPLETE`, `V2_1_COMPLETE`,
`V3_COMPLETE`, `V4_COMPLETE`) and original release tags. The current stage is
V5 Refined & Reliable (release v5.0.0), human-authorized and activated by
managed Issue V5-00. Its backlog is exactly the Issues with BOTH milestone
`Tendo v5.0.0 - Refined & Reliable` AND label `tendo:v5-autonomous`. It
completes only via V5-17: `.autonomous/V5_COMPLETE` merged by a separate final
protected PR after the managed backlog, V5_DEFINITION_OF_DONE and the
published v5.0.0 release are verified. The controller
(`scripts/autonomous-loop.sh`) then exits and releases the shared maintenance
flock; the still-enabled `tendo-maintenance.timer` resumes post-V5 maintenance
on its next trigger without reconfiguration (FLOW "V5 controller and
maintenance handoff"). Historical markers, including `V4_COMPLETE`, never stop
the V5 controller.

## Historical V3 and V4 Issues
The V3 milestone `Tendo v3.0.0 - Everyday Elegance` and the V4 milestone
`Tendo v4.0.0 - Modern Everyday` are closed work. No agent queries them for
work, and never claims, labels, comments on, reopens or closes any V3 or V4
milestone Issue, managed or not. Do not create new `tendo:v3-autonomous` or
`tendo:v4-autonomous` Issues. Read V3_ISSUES_WORKFLOW or V4_ISSUES_WORKFLOW
only to interpret historical evidence; their rules still bind anyone who
touches those Issues.
