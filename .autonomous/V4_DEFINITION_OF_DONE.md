# Tendo V4 Definition of Done

status=locked_after_initial_governance_PR
- [ ] V3_COMPLETE and published v3.0.0 verified; previous tags/markers preserved.
- [ ] V4 milestone + exclusive label backlog bootstrapped; V4 workflow activated ONLY via reviewed protected governance PR; V3 maintenance reconciled and not allowed to claim V4 issues accidentally.
- [ ] V4 design system uses the approved desktop/mobile images copied byte-for-byte; Inter Variable is self-hosted, cool navy/petrol-teal/coral/amber palette and compact grouped layout are implemented throughout; real production screenshots are compared side by side using V4_DESIGN_BRIEF fidelity checklist, major deviations require human approval; light/dark/system no-flash behavior retained.
- [ ] Existing routes, interactions, messages and states receive consistent new styling; no fake or unrelated GitHub/task-manager UI; no UX regression.
- [ ] Household role change owner→admin migrated preserving all permissions/data; only admins access admin APIs/screens, can promote/demote, cannot remove last admin, cannot reach other household accounts.
- [ ] Admin-only secure single-use local-account password reset complete with state transitions, abuse protection, no plaintext leakage and tested expired/reuse/revoke/failure paths.
- [ ] OIDC JIT account creation and immutable issuer/subject binding; existing v3 schema lacks email and OIDC only fetches issuer/sub, so new email + verification provenance and provider email claims are implemented; same securely verified email resolves to same account automatically, legacy no-email accounts have a practical secure resolution path, unsafe/ambiguous cases blocked; explicit invite required for household membership.
- [ ] Legacy frontend manual OIDC account-linking removed without breaking existing linked identities or local password login.
- [ ] Existing registered members are first-class selectable people in task creation/edit/filters and People and things without a second manual entity; old subject IDs, linked persons, tasks/history migrate and remain stable; no name-only auto-merge.
- [ ] Expired invitations absent from all frontend lists, pages and counts even when backend cursor pagination includes expired rows; pending/accepted/revoked behavior and server audit remain correct.
- [ ] Canonical release Compose contains EXACTLY `postgres` and `app`: remove root `configs`, `postgres-init` and `migrate` container. New app startup idempotently ensures restricted role and performs existing migrations before opening HTTP; named PostgreSQL volume and privileges preserved for existing v3 installation and fresh install. Runtime DB connections stay restricted; privileged bootstrap connection is closed before serving, and retained admin credential exposure is documented as an accepted reduction in isolation, never misrepresented.
- [ ] Real clean install, populated published-v3 upgrade, repeated/concurrent startup, migration failure/dirty/newer schema, admin credential failure and recovery tested using published two-service Compose; no readiness on broken schema; release docs/overrides/CI updated.
- [ ] Meaningful Go, React, integration, security, API contract and production Playwright tests; 320–1440px, CZ/EN, keyboard, WCAG-oriented accessible contrast and controls, light/dark.
- [ ] Independent skeptical review of visuals, account takeover risk, privilege checks, migration reversibility, admin UX and performance; findings resolved, not merely recorded.
- [ ] README docs/release notes updated, screenshots from REAL running release candidate committed; no AI render labeled as genuine screenshot.
- [ ] Green PR and merged main CI, published GitHub v4.0.0 release, verified public GHCR amd64/arm64, published assets, fresh install and in-place v3.0.0→v4.0.0 upgrade without loss.
- [ ] Last isolated protected PR merges .autonomous/V4_COMPLETE, main clean, all managed V4 issues closed, preserve historical markers.
- [ ] No notifications settings/notifications service work in V4.
