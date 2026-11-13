# Tendo Agent Entry

mode=autonomous
language=english
context_style=terse_structured
primary_goal=deliver_owned_release_Issues_and_keep_dependencies_security_CI_and_patch_releases_current
main_policy=PR_only
human_intervention=owner_authority_only

## Authority and ownership
- Work is owned ONLY when it is a real Issue in `bigtcze/tendo` with the exact `release` label. Milestones, titles and release numbers are not authority.
- Never adopt an existing Issue by adding `release`; never mutate unowned or closed historical Issues. New verified defects within authorized work may get a new owned Issue with labels set at creation. Recheck ownership before every mutation.
- Claims, collisions, dependencies, evidence and handoff follow FLOW "Release Issue execution" and "Dependency recovery". A blocked or old claim is not abandoned work. Never steal it.
- Permanent product/technical contracts live in the canonical files below. Issue bodies define authorized deliverables and acceptance; comments/PRs carry status and evidence. No STATE.md, acceptance database, shadow queue, leases, tracking labels or second scheduler.
- Routine engineering choices and recoverable ordering blockers are the agent's responsibility. Escalate only out-of-scope work, security exceptions, destructive/irreversible actions beyond an authorized safe migration, credentials/infrastructure, legal decisions or protection gates. Never bypass a gate to resolve a blocker.

## Read set
Paths are `.autonomous/<NAME>.md`. This entry is already injected; do not re-read it. Read each matching file once while its requirements remain verbatim in context and unchanged. After compaction, checkout or uncertainty, re-read. `docs/development/agent-context.md` explains navigation, not a second specification.

always=GUARDRAILS,CHARTER,PRODUCT,ARCHITECTURE,API,TESTING,CODE_QUALITY,FLOW,LEARNINGS
when_owned_Issue=its_acceptance_and_named_contracts_resolved_by_compatibility_table_below
when_UI_design_tokens_rendering_or_visual_baselines=UX,DEFINITION_OF_DONE
when_recurrence_completion_undo_schedule_or_item_lifecycle=RECURRENCE,DEFINITION_OF_DONE
when_identity_households_membership_roles_admin_password_invitation_OIDC_sessions_or_MCP=SECURITY,DEFINITION_OF_DONE
when_lists_search_filter_sort_pagination_counts_or_large_data=API,UX,DEFINITION_OF_DONE
when_test_harness_CI_local_gate_E2E_release_qualification_refactoring_or_test_strength=TESTING,CODE_QUALITY,DEFINITION_OF_DONE
when_dependency_toolchain_Renovate_or_security_update=FLOW "Dependency maintenance",docs/development/dependencies.md "Dependency PR procedure"
when_docs_or_user_setup=DOCUMENTATION
when_platform_or_future_seam=ROADMAP
when_forms_history_onboarding_deployment_backup=DEFINITION_OF_DONE
when_release=FLOW "Release from owned Issues" and "Maintenance release",DOCUMENTATION,DEFINITION_OF_DONE,docs/development/release.md,docs/development/release-qualification.md
when_controller_or_work_selection=FLOW "Autonomy controller" and "Scheduler order"
never_for_routine_work=historical_governance_from_Git_or_completed_Issues

If concerns overlap, read all matching contracts before editing.

## Canonical contracts and compatibility
These documents consolidate the human-authorized contracts without reopening completed stages. No historical version files are needed on the current branch. Historical texts remain in Git and original release tags; do not archive duplicates. References in existing open Issues resolve as follows, without editing their bodies, claims or acceptance:

| Legacy reference | Current binding location |
| --- | --- |
| V2_SCOPE, V2_DEFINITION_OF_DONE | PRODUCT; API "Collections"; UX "Scale"; DEFINITION_OF_DONE |
| V2_1_SCOPE, V2_1_DEFINITION_OF_DONE | TESTING; CODE_QUALITY; DEFINITION_OF_DONE |
| V3_SCOPE, V3_DEFINITION_OF_DONE, V3_DESIGN_BRIEF | UX; TESTING; DEFINITION_OF_DONE; old visual direction historical only |
| V4_SCOPE, V4_DEFINITION_OF_DONE | PRODUCT; SECURITY; ARCHITECTURE; DEFINITION_OF_DONE |
| V4_DESIGN_BRIEF and reference PNGs | historical design context only, never current targets or production evidence |
| V5_SCOPE | PRODUCT; RECURRENCE; SECURITY; UX; ARCHITECTURE; DEFINITION_OF_DONE |
| V5_RECURRENCE_CONTRACT | RECURRENCE (including the unchanged golden table and mandatory coverage) |
| V5_SECURITY_AND_MCP | SECURITY; ARCHITECTURE "Deployment and startup" |
| V5_DESIGN_BRIEF | UX |
| V5_DEFINITION_OF_DONE | DEFINITION_OF_DONE |
| Any former ISSUES_WORKFLOW or CYCLE_PROMPT | FLOW; current AUTONOMY_PROMPT |

- Technical requirements, explicit acceptance, migration safety, test strength and review gates survive consolidation. No blanket permission to reinterpret or drop acceptance. A genuine technical conflict requires a failing test and independent reviewed compatibility/migration decision; record evidence on the owned Issue/PR.
- Ownership is `release`, not legacy milestone/label text. The permanent controller replaces all per-stage loop/timer handoffs. Existing acceptance asking to stop a loop or enable maintenance after completion is satisfied by proving continued serialized dependency/security/patch maintenance after release; never stop the controller or re-enable a competing scheduler. A requested final `*_COMPLETE` PR is evidence only, still protected and only after its release gates.
- New releases use ordinary owned Issues and `docs/releases/` notes. Do not create another versioned governance packet, cycle prompt, controller or precedence chain.

## Runner independence
- These rules bind every model, coding agent, CLI, provider and host identically. They name capabilities (independent reviewer, delegated worker, architecture review; FLOW "Layers"), never products or agent profile names.
- How a concrete runtime is started, guarded and recovered lives only in FLOW "Runtime integration". Never add per-agent, per-provider or per-release governance copies, schedulers, status stores or queues. A runtime that does not load this file itself is given it explicitly; it gets no rule copy.

## Tooling and verification
- Use `bash scripts/dev-exec.sh -- <command>` for exact checksum-verified `mise.toml`/`mise.lock` pins; user-level mise must meet `min_version`. Never rely on host Go/Node/sqlc.
- Full gate: `bash scripts/dev-exec.sh -- bash scripts/verify-local.sh` (`TENDO_TEST_POSTGRES_ADMIN_URL`; see `docs/development/testing.md`). Tests use isolated resources; never share cleanup with another active run.
- Offline controller/selector/migration/installer suite: `bash scripts/autonomy-test.sh`. Never run the real installer `--apply`/`--rollback` as an agent.
- Independent review, actual green `required` on the current PR, protected merge and verified green main are mandatory. Never claim tests not executed.

## Change boundaries
may=plan,code,test,review,document,create_branch,push_branch,open_PR,update_Renovate_PR_branch,merge_after_protection_and_green_checks,create_new_annotated_release_tag_after_green_main
may_self_improve=AGENTS,FLOW,LEARNINGS,dev_tooling,tests,CI,docs_ADRs_within_authorized_scope
must_not_self_change=GUARDRAILS,CHARTER,PRODUCT,ROADMAP,UX,ARCHITECTURE,API,TESTING,DOCUMENTATION,DEFINITION_OF_DONE,RECURRENCE,SECURITY,CODE_QUALITY,AUTONOMY_PROMPT,autonomy_controller_script,autonomy_work_selection_policy,autonomy_installer,autonomy_migration_policy,systemd_runner,auth_credentials,global_runtime_config
must_not=direct_commit_main,direct_push_main,force_push,history_rewrite,bypass_required_checks,merge_known_broken,out_of_scope_feature_work,notification_settings_or_delivery,work_on_unowned_Issues,add_release_to_existing_Issues,relabel_closed_historical_Issues,run_autonomy_installer_apply_or_rollback,change_host_services_or_runtime_locks,speculative_refactoring,casual_Renovate_policy_change,move_or_reuse_tags,empty_or_documentation_only_release,edit_historical_COMPLETE_markers

Changing locked contracts or controller configuration requires explicit owner authorization through a reviewed protected governance PR, not autonomous self-amendment.

## Completion and history
- Preserve all existing completion markers and original tags unchanged. Completion markers never select work or stop the controller.
- Completed historical Issues are read-only evidence, not backlog. Never claim, label, comment on, reopen or close them; never reactivate old stage labels.
- `tendo-autonomous.service` runs `scripts/autonomy-controller.sh` permanently. When no owned work is actionable it continues dependency, security, CI recovery and eligible patch-release maintenance, otherwise idle polling. There is no stage end.
- Work status stays in owned Issues and their PRs; dependency triage stays on its PR and the Renovate Dashboard. LEARNINGS holds durable reusable rules only, never session narrative.
