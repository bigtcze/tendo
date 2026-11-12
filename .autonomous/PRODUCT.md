# Product contract

status=canonical_version_independent
source_of_truth=this_file
scope=household_life_backlog
multiuser=yes
household_scoped=yes
external_integrations_required=no
auth=local_plus_optional_oidc
oidc_target=PocketID_and_standards_compliant_OIDC
registration_default=closed_after_initial_admin; new access by explicit invitation
native_mobile_app=no
web=responsive
product_thesis=CHARTER.md
recurrence_rules=RECURRENCE.md (single source for all schedule, completion and undo semantics)
security_rules=SECURITY.md
visual_rules=UX.md

## Identity and households
UserAccount
- Authenticated human account. Global identity.
- Local login/password and/or OIDC identity. Both may coexist.
- A tracked person does not need an account.
- Optional `default_household_id` selects the household shown first. It is not an access control source.

Household
- Durable ownership and security boundary.
- Has a name chosen by the first user during onboarding (examples: "Veselí", "Novák family").
- IANA timezone; created metadata.

HouseholdMembership
- Many-to-many: a user may belong to several households, each with a household-scoped role.
- Roles: `admin` (manages the household within granted permissions) and `member`.
- The former V1 `owner` role is the household admin role after migration (SECURITY.md "Migration from v4").
- Instance administrator is an installation-wide capability, not a membership role. It is granted explicitly, never by invitation, OIDC claims or household role edits (SECURITY.md "Identity model").

Contract (v5 target; implementation status is evidence in release Issues, not claimed here):
- The app lets a user switch the active household. The active household is always validated on the server.
- Instance admin creates households, assigns and removes members, changes scoped roles and can delete accounts.
- Household admin manages only their own household.
- Last-admin protection applies per household and to the instance (SECURITY.md "Authorization invariants").

Subject
- Tracked entity; may exist without login. Household-scoped.
- Types: person, home, vehicle, pet, custom.
- Name; optional linked user account for a person subject; archived flag.

People and subjects
- An existing household member IS a person. The member appears as a selectable person in task creation and edit, filters, search and "People and things" without a second manual record.
- Existing person records may be linked to a member optionally. No auto-merge by name.
- Subject references, selections, member renames and login changes keep history and references stable.
- Migration of existing subject references is idempotent.

Examples:
- child = person subject without login
- spouse = user account + optional linked person subject
- car = vehicle subject
- apartment = home subject

## Onboarding
1. Create or authenticate the first user.
2. Ask for the household or family name.
3. Detect the browser timezone; the user confirms or changes it.
4. Create the household and an admin membership.
5. Set it as the user's default household.
6. Enter an empty home screen with an obvious first-item action.

Never ask about database concepts, household IDs, membership models or multi-household support.

## Item model
Item
- Household-scoped. Title. Subject. Optional responsible household member. Optional notes. Archived flag.
- Workflow state (see "Lifecycle").
- Optional current-cycle attention date and optional recurrence policy (RECURRENCE.md).
- History. Created and updated metadata. Opaque public ID. Resource version for optimistic concurrency.
- One-off items are first class.

## Lifecycle
derived_time_state=UPCOMING|NEEDS_ATTENTION
workflow_state=OPEN|IN_PROGRESS|WAITING|PAUSED
completion=explicit_event

Rules:
- NEEDS_ATTENTION is derived. It is never selected manually.
- IN_PROGRESS means someone started handling the item.
- WAITING means an action happened but the obligation is not complete.
- PAUSED suppresses normal active attention. The schedule clock keeps running.
- Resuming immediately reflects the current derived time state.
- IN_PROGRESS, WAITING and PAUSED never advance recurrence.
- Completion is the only action that advances a repeating item to its next cycle.
- An active item persists until explicit completion, archive or pause.
- Completion is atomic and idempotent against accidental double submission (RECURRENCE.md).
- The latest completion is undoable or correctable without corrupting schedule or history.
- Completing a non-repeating item keeps it in history and removes it from the active backlog.
- Archive preserves history.

## Recurrence
All schedule and completion semantics, golden cases and undo rules are in RECURRENCE.md. Product summary:
- Repeat is an explicit toggle, OFF by default. OFF = a fully valid one-off item.
- Repeat ON reveals the interval (day, week, month or year) and a second toggle: "Count the next repeat from when I complete this" (fluid). Fluid is OFF by default.
- Fluid OFF keeps the planned cadence even when completion is early or late.
- Fluid ON counts the next cycle from the actual completion.
- Switching repeat or fluid settings preserves completion history.
- Optional known historical completion initializes the first cycle (RECURRENCE.md).

## Current-cycle attention
- An optional attention date controls when the current item becomes visible for attention.
- No date means the item needs attention immediately.
- Before the date: UPCOMING. On or after: NEEDS_ATTENTION.
- Recurrence never forces an initial date.

## Time
- Household timezone is IANA. Business dates are date-only in that zone. Audit timestamps are UTC.
- Completion defaults to household-local today. The user may record the actual completion date.
- Fixed and fluid calculations are backend-only.

## Authentication and onboarding
- Local auth works without an external identity provider.
- Passwords use a modern hash; never reversible or plaintext.
- Browser auth uses secure HttpOnly cookies; no long-lived bearer token in localStorage.
- OIDC is optional. OIDC login alone never grants membership in a household.
- Invitations do not require outbound email.
- Full identity, OIDC, password-change, password-reset, invitation and MCP rules: SECURITY.md.

## Home and navigation
- Home answers "What needs my attention?". It shows what deserves attention now, plus compact in-progress and waiting groups. Upcoming items follow.
- Paused, completed, archived and the full backlog stay discoverable without noise. Never hide items to make a screen look clean.
- Navigation contains: Home, All items, Archive, People and things, Members, Account, and Admin center when authorized. Archive is discoverable on phones too.
- Layout and visual language: UX.md.

## Completion history
- Item detail shows a meaningful completion history. Last completion and next attention date are visible when applicable.
- History is paginated with stable ordering (UX.md "History").

## Search, filters and scale
- Search and filters operate on the full matching household collection, with stable cursor pagination and truthful counts.
- Practical performance with 1000+ items is required (TESTING.md, UX.md "Scale").

## Localization
- Czech and English UI from day one. Strings live in locale resources, not in backend business logic.
- Human wording only (UX.md "Language").

## MCP access
- Per-user, OAuth-protected MCP. Full rules in SECURITY.md "MCP (per-user OAuth, V5)".

## Deployment
- Docker Compose with exactly two services (ARCHITECTURE "Deployment and startup").
- Reverse-proxy-first. Tendo serves HTTP; the reverse proxy handles HTTPS.
- Backup and restore are documented and smoke-tested (DOCUMENTATION).

## Explicitly deferred or excluded
Not part of the product unless a future release explicitly authorizes it:
- AI provider, document upload and extraction, OCR, chat, automation rules, templates marketplace
- Nextcloud per-user storage, calendar sync, email inbox
- Notification configuration, delivery and channels, push
- Analytics, telemetry, gamification, charts, dashboards
- Unrelated authentication methods, native mobile app, framework rewrites
- Post-V1 directions: ROADMAP.md (direction only, not permission)
