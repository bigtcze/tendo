# Tendo v5.0.0 — Refined & Reliable

status=human_authorized_v5_bootstrap_packet
release_target=v5.0.0
product=self_hosted_household_organizer
baseline=published_v4.0.0
managed_milestone=Tendo v5.0.0 - Refined & Reliable
managed_issue_label=tendo:v5-autonomous

## North star
Make Tendo a reliable, polished, household-first product. This release has two equally important outcomes: correct recurring-item scheduling and a distinctly professional, compact user interface. Add multi-household account administration and per-user, OAuth-protected MCP without compromising the existing user data, authorization, or simple two-container install.

## Deliverables

### Recurring items — release-critical, implement early
- Audit actual backend/domain/persistence/API/React behavior first. Reproduce the user's observed failure: clicking Done on a monthly repeating item may retain the same date instead of advancing.
- Implement one canonical deterministic recurrence calculation and atomically persist the state transition and completion ledger. UI, API and MCP use the SAME application service. No parallel recurrence engine or frontend date arithmetic.
- Support both anchored/fixed cadence and after-completion cadence. Preserve calendar anchors for month ends and leap days, advance exactly once per accepted completion, correctly cross month/year boundaries, skip missed fixed occurrences according to the recurrence contract. Preserve early and late completion semantics, undo, idempotency, concurrency safety and historical records.
- Put recurrence boundary/negative/concurrency/property tests in REQUIRED CI with real PostgreSQL integration and browser flows; do not weaken existing tests. Read V5_RECURRENCE_CONTRACT.md.

### Visual product overhaul
- Perform evidence-based senior product-design critique of current production UI at 320/390/768/1024/1440/1920px, CZ/EN and light/dark. Build at least two implemented, interactive directions using real screens (not AI marketing mockups), evaluate them against usability, family-first visual maturity and responsive behavior; choose one through documented evidence without falsely claiming human approval.
- Use selected ideas inspired by mature consumer task apps and well-disciplined admin UI, without copying proprietary design or creating a generic AI dashboard. Define deliberate tokens, typography, density, accessibility and layout; apply across EVERY route/state, including admin and MCP.
- Replace the framed/narrow header with a truly full-viewport top bar. Keep only Tendo branding and practical navigation: accessible active-household selector, identity/settings. Remove "DOMOV POD KONTROLOU.", "VÁŠ DOMOV · PŘEHLED", the oversized household heading, decorative period and marketing tagline from the Home content. Compact page title and primary action.
- Consistent spacing and insets; avoid edge-hugging buttons and random huge voids; usable task rows with aligned title, assignee, dates, cadence and Done. Keep responsive small-screen navigation and accessible controls.
- Move Archive into discoverable top-level navigation (mobile-accessible), add Tendo favicon/app icons, use straightforward paginated completion history rather than overengineered timeline/infinite scroll.
- Preserve practical 1000+ item performance, fully meaningful metadata, keyboard, contrast, touch, localization and reduced-motion behavior. Read V5_DESIGN_BRIEF.md.

### Account and household model
- A user account is a global identity; memberships are many-to-many across households, each with a household-scoped role. Add explicit instance administrator (installation-level authority) distinct from household admin and member. A member can belong to multiple households with different permissions, and switch active household in the application.
- Instance admin creates households, assigns/removes members, changes scoped roles and can delete accounts. Household admin manages only their own household within granted permissions. All operations are BACKEND authorized, with strict ownership filters and stable historical task references.
- Prevent demoting/removing/deleting the last active instance administrator and last active administrator of each household (including concurrent operations). No accidental instance-admin grants via invitations, OIDC or household role edits. Existing v4 users/households/roles survive migration with a documented, reviewed safe bootstrap-admin promotion and recovery path.
- Locally authenticated active-session users can change their own password by presenting the current password. Sessions established via OIDC cannot use the local password-change flow. Provide an interactive SSH/container CLI for resetting the instance administrator password without passing secrets as process arguments or writing cleartext to logs; reset/revoke sessions and tokens where appropriate.
- Physically remove cancelled/revoked invitations and invalidate their tokens; expire/purge stale invitations by bounded retention/cleanup. Security audit events may be preserved without revoked secret-bearing invitation records. Verify migrations and concurrent acceptance/revocation.

### Self hosting
- Preserve released two-service Compose (`postgres` + `app`), PostgreSQL data volume, readiness gating and restricted runtime role. Make host IP configurable, e.g. `${TENDO_HOST_IP:-127.0.0.1}:${TENDO_HOST_PORT:-8080}:8080`; show secure localhost default and explicit opt-in to network-wide bind in `.env.example`, installation docs and tests. No added migrate service or new required reverse proxy.
- Make favicon/browser title and install assets correct in all themes and languages.

### Per-user MCP
- Implement MCP Streamable HTTP in the existing app or a deliberately justified component. Multi-user identity is mandatory: one MCP endpoint authenticates each request as a real Tendo user, never one shared privileged key or cross-request user context.
- Standards-compliant OAuth authorization for MCP clients, appropriate resource metadata/discovery, PKCE, audience-bound access tokens, rotation/revocation and secure association with existing local/OIDC sign-in. OIDC login alone is not proof of MCP OAuth compatibility; implement/validate the actual protocol. No anonymous write tools or silent PAT fallback.
- Instance admin globally enables/disables MCP, allows/disallows particular users and grants per-user maximum tool/role scopes; the user can override/restrict their allowed set, never elevate it. Effective permission is intersection of admin grants, user selection, token scopes and household membership/role, re-evaluated server-side. Revocation immediately blocks future requests.
- Tools for accessible households, list/search/detail/create/update items, Done/Undo, scheduling, completion history, people and things, with guarded, testable schema and explicit error results. No MCP global instance administration or user deletion in v5. All tools call existing application services and honor idempotency, audit and household isolation.
- Read V5_SECURITY_AND_MCP.md.

## Non-negotiable boundaries
- Keep the Go/PostgreSQL/React architecture, generated OpenAPI and frontend types, V1–V4 data, history, OIDC/local login, backup/restore and test strength. No speculative framework rewrite, new native mobile app, notification/delivery subsystem, AI features, telemetry or off-topic integrations.
- All V1, V2, V2.1, V3 and V4 release markers/specifications/tags are historical and MUST NOT be rewritten or deleted. Old style references remain historical; V5 design brief governs the new visual direction.
- No direct commit to protected main. Every code/dependency/governance change through independent-reviewed PR, mandatory actual `required` CI, green merged main. Release only after published artifacts and real populated v4->v5 upgrade succeed.
- Issues are owned ONLY with BOTH exact milestone and exact label `tendo:v5-autonomous`. Never adopt or change unowned issues. Status evidence in issues/PR, no STATE.md or second work ledger.
- V5's active controller must process Renovate PRs throughout development and return to automatic dependency maintenance after release; see V5_ISSUES_WORKFLOW.
