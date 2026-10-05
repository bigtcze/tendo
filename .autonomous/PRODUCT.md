# Product V1

scope=household_life_backlog
multiuser=yes
household_scoped=yes
external_integrations_required=no
auth=local_plus_optional_oidc
oidc_target=PocketID_and_standards_compliant_OIDC
registration_default=closed_after_initial_owner
native_mobile_app=no
web=responsive

## Identity model
UserAccount
- authenticated human account
- local auth and/or OIDC identity
- a tracked person does not need an account
- optional `default_household_id` for future multi-household support

Household
- durable ownership/security boundary
- user chooses household/family name during first-run onboarding
- name examples: "Veselí", "Novák family"
- IANA timezone
- created metadata

HouseholdMembership
- schema is many-to-many-capable from day one
- user -> household
- role=owner|member
- V1 UI supports exactly one active/default household and has no household switcher
- V1 UI does not expose creating a second household
- schema/API authorization must not hardcode "one household forever"
- if future versions expose multiple memberships, `default_household_id` gives a migration-free default selection path
- V1 invite acceptance rejects/defers joining a second household when the account already has a different active/default household; schema remains many-to-many for a future UI that can safely switch

Subject
- tracked entity; may exist without login
- household-scoped
- type=person|home|vehicle|pet|custom
- name
- optional linked user account for a person subject
- archived flag

Examples:
- child=person subject without login
- spouse=user account + optional linked person subject
- car=vehicle subject
- apartment=home subject

## First-run onboarding
1=create/authenticate first user
2=ask for household/family name
3=detect browser timezone and let user confirm/change it
4=create household + owner membership
5=set it as user's default household
6=enter empty home/backlog screen with obvious first-item action

Do not ask the user about database concepts, household IDs, membership models, or future multi-household support.

## Item model
Item
- household-scoped
- title
- subject
- responsible household member optional
- notes optional
- archived flag
- workflow state
- optional current-cycle attention date
- optional recurrence configuration
- history
- created/updated metadata
- opaque public ID
- resource version for optimistic concurrency

## Recurrence model
repeat_enabled=explicit_UI_toggle
repeat_default=off
fluid_recurrence=second_toggle_visible_only_when_repeat_enabled

## Schedule policy vs cycle state
Keep policy separate from current-cycle state.

RecurrencePolicy
- enabled
- interval_value
- interval_unit=day|week|month|year
- mode=fixed|after_completion

CurrentCycle
- attention_on nullable date
- represents the current cycle's planned attention anchor
- persists independently of later policy edits until explicitly rescheduled or advanced by completion

CompletionEvent
- completed_on
- completed_by
- snapshot of the completed cycle's `attention_on`
- snapshot of recurrence policy relevant to that completion
- immutable history except explicit latest-completion correction/undo workflow

Rules:
- editing interval/mode affects what happens at the next completion; it does not silently rewrite the current cycle's attention date
- editing current attention date is a separate explicit action
- completion transaction records history and computes/persists the next current-cycle attention date atomically
- undo/correct latest completion restores/recomputes state from recorded completion/cycle facts; never guess from current UI values

There are two independent decisions:
1. `Repeat` ON/OFF = should a new cycle appear after completion?
2. `Fluid recurrence` ON/OFF = if repeating, what anchors the next cycle?

### Repeat OFF
- completion ends the active item
- history remains available
- fluid recurrence control is hidden/irrelevant

### Repeat ON + Fluid OFF = fixed cadence
internal_mode=fixed

Meaning:
- preserve the planned cadence even if the current cycle is completed early/late
- current cycle has a scheduled attention anchor (`attention_on`)
- next anchor is based on the current scheduled anchor, not completion date
- if completion occurs after one or more future anchors would already be in the past, advance by whole intervals until the first anchor strictly after completion
- missed intervals do not create a pile of duplicate active items in V1

Examples:
- yearly item scheduled 2026-09-01, completed 2026-09-20 -> next 2027-09-01
- monthly item scheduled 2026-01-01, completed 2026-03-15 -> next 2026-04-01
- yearly item scheduled 2026-09-01, completed early 2026-08-20 -> next 2027-09-01

If no scheduled anchor exists for the first cycle:
- completion establishes the initial cadence anchor
- next anchor = completion date + interval

### Repeat ON + Fluid ON = after-completion cadence
internal_mode=after_completion

Meaning:
- next cycle is counted from when the user actually completes the current one
- next anchor = completion date + interval
- late/early completion intentionally shifts future cadence

Example:
- 12-month item scheduled 2026-09-01, completed 2026-10-20 -> next 2027-10-20

### UI
- `Repeat` is a normal toggle.
- When Repeat turns ON, reveal interval controls.
- Also reveal a second toggle with human wording such as:
  "Count the next repeat from when I complete this"
- Do not make users understand the term `after_completion`.
- Fluid toggle default is OFF unless later user research justifies changing it.
- Switching repeat/fluid settings must preserve existing completion history.

## Current-cycle attention
- optional `attention_on` date controls when the current item becomes visible for attention
- if absent, item needs attention immediately
- before date: UPCOMING
- on/after date: NEEDS_ATTENTION
- recurrence controls what happens after completion; it does not force an initial date

## Time semantics
household_timezone=IANA_timezone
business_dates=date_only_in_household_timezone
audit_timestamps=UTC

Rules:
- Completion defaults to household-local today; user may record actual completion date.
- Fixed and fluid calculations are backend-only.
- Interval=value+unit(day|week|month|year).
- Month/year arithmetic clamps to last valid target-month day.
- Jan 31 + 1 month -> Feb 28/29.
- Feb 29 + 1 year -> Feb 28 in a non-leap year.
- Optional known historical completion initializes the first recurring cycle: `attention_on = historical_completed_on + interval` for both modes; mode begins to differ only after the next real completion.
- If that computed attention date is already past, the item is immediately NEEDS_ATTENTION; do not skip it forward merely because time passed.
- Never fabricate a historical completion only to make scheduling work.
- Backend is sole authority for schedule calculations.

## Lifecycle
derived_time_state=UPCOMING|NEEDS_ATTENTION
workflow_state=OPEN|IN_PROGRESS|WAITING|PAUSED
completion=explicit_event

Rules:
- NEEDS_ATTENTION is derived; never manually selected.
- IN_PROGRESS means somebody started handling it.
- WAITING means action happened but obligation is not complete.
- PAUSED suppresses normal active attention; schedule clock continues.
- Resuming immediately reflects current derived time state.
- IN_PROGRESS/WAITING/PAUSED never advance recurrence.
- Completion is the only action that advances a repeating item to its next cycle.
- Active item persists until explicit completion/archive/pause.
- Completion is atomic/idempotent against accidental double submission.
- Latest completion is undoable/correctable without corrupting schedule/history.
- Non-repeating completion remains in history but leaves active backlog.

## Authentication/onboarding
- Local auth exists so Tendo works without an external IdP.
- Passwords use modern password hashing; never reversible/plaintext.
- Browser auth uses secure HttpOnly cookies; no long-lived bearer token in localStorage.
- Optional OIDC works with PocketID and generic standards-compliant providers.
- Initial user creates/names the first household as owner.
- New access requires explicit invitation/authorization by default.
- OIDC login alone never grants membership to an existing household.
- Invite flow does not require outbound email in V1.

## Required V1 capabilities
- First-run onboarding that asks for household/family name.
- Local multiuser auth.
- Optional OIDC.
- Household owner/member roles and invitations.
- DB schema capable of multiple households/memberships; no V1 household switcher.
- Subject create/edit/archive.
- Item create/edit/archive.
- First-class one-off items.
- Optional future attention date.
- `Repeat` toggle.
- Repeat interval.
- Separate `Fluid recurrence` toggle.
- Both fixed and after-completion recurrence semantics.
- Optional known historical completion for initialization.
- OPEN / IN_PROGRESS / WAITING / PAUSED.
- Completion + undo/correct latest completion.
- Completion/history timeline.
- Optional responsible member.
- Calm home view: attention first, in-progress/waiting second, upcoming later.
- Responsive web UI for phone/tablet/desktop.
- Professional versioned REST API/OpenAPI.
- PostgreSQL persistence.
- Docker Compose deployment.
- Reverse-proxy-first deployment; Tendo serves HTTP, not TLS/certificates.
- Backup/restore documentation.
- Czech + English UI architecture; strings localizable and not embedded in backend business logic.

## Explicitly deferred
v2plus=AI_provider,document_upload,document_extraction,Nextcloud_per_user,calendar_optional_sync,email_inbox,notifications,push,OCR,chat,automation_rules,templates_marketplace,multi_household_UI
