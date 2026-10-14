# V1 Definition of Done

A=deploy
- [ ] Fresh clone + documented config + `docker compose up -d` starts Tendo + PostgreSQL.
- [ ] Production image contains built frontend; no separate frontend runtime container.
- [ ] Core app works with no mandatory SaaS/external IdP.
- [ ] App serves HTTP; reverse proxy handles HTTPS.
- [ ] Caddy/Nginx/Traefik reverse-proxy docs exist and match tested config.
- [ ] Trusted-proxy behavior and `TENDO_PUBLIC_URL` are implemented/documented.
- [ ] Health/readiness + graceful shutdown work.
- [ ] Backup AND restore is documented and smoke-tested.

B=auth_household
- [ ] First-run owner onboarding asks for household/family name.
- [ ] Browser timezone is proposed and can be corrected.
- [ ] First-run creates household + owner membership + default household.
- [ ] Local login works with secure hashing/session/cookie handling.
- [ ] Optional OIDC works with standards-compliant test provider and PocketID docs.
- [ ] OIDC login alone does not silently grant household membership.
- [ ] Invite-based multiuser flow works without outbound email.
- [ ] Owner/member authorization enforced server-side.
- [ ] Household-scoped storage/API prevents cross-household access.
- [ ] DB membership model supports multiple households in future.
- [ ] V1 UI has no household switcher/create-second-household flow.

C=subjects
- [ ] Create/edit/archive subject.
- [ ] Types: person, home, vehicle, pet, custom.
- [ ] Person subject does not require login account.
- [ ] UI remains simple regardless of type.

D=items_recurrence
- [ ] Create/edit/archive one-off item.
- [ ] Repeat toggle exists and defaults OFF.
- [ ] Repeat OFF path requires no recurrence fields.
- [ ] Repeat ON reveals interval.
- [ ] Repeat ON reveals separate Fluid recurrence toggle.
- [ ] Fluid OFF implements fixed cadence.
- [ ] Fluid ON implements after-completion cadence.
- [ ] Switching recurrence mode preserves history/invariants.
- [ ] Turning Repeat OFF preserves history.
- [ ] Optional future one-time attention date works.
- [ ] Assign subject + optional responsible member.
- [ ] Notes supported.
- [ ] Optional known historical completion initializes the first cycle exactly as PRODUCT defines, including already-past attention dates.

E=lifecycle_schedule
- [ ] Immediate one-off starts needs-attention.
- [ ] Future one-off transitions upcoming -> needs-attention.
- [ ] In-progress/waiting/pause/resume work.
- [ ] Fixed cadence uses planned anchor, not completion.
- [ ] Fixed cadence late/early completion semantics match PRODUCT.
- [ ] Fixed cadence skips missed historical anchors without duplicate active backlog items.
- [ ] Fluid cadence anchors next cycle to actual completion.
- [ ] In-progress/waiting/paused never advance recurrence.
- [ ] Non-repeating completion ends active item but preserves history.
- [ ] Latest completion safely undoable/correctable.
- [ ] Double-submit/retry cannot create two completions/skip cycle.
- [ ] Concurrent update/completion behavior safe/tested.
- [ ] Month/year end-of-month/leap semantics tested.
- [ ] Household timezone/date-only behavior tested.

F=ux
- [ ] Responsive web works at common phone/tablet/desktop widths.
- [ ] No native mobile app dependency/architecture required.
- [ ] Needs-attention obvious without dashboard clutter.
- [ ] First-run household naming is short/clear.
- [ ] No household switcher in V1 UI.
- [ ] Create-item default flow is short.
- [ ] Repeat and Fluid recurrence are two distinct understandable controls.
- [ ] Completion fast + recoverable.
- [ ] No enterprise-dashboard/gamification/shaming UI.
- [ ] Keyboard/accessibility basics verified.
- [ ] Czech + English UI paths work.
- [ ] UI starter/template source/license documented.

G=history
- [ ] Item detail shows meaningful timeline/completion history.
- [ ] Last completion/next attention visible when applicable.
- [ ] Archive preserves history.

H=api
- [ ] `/api/v1` resource API follows locked API conventions.
- [ ] OpenAPI valid/complete for implemented public endpoints.
- [ ] Go transport + TS client/types generated/mechanically derived without drift.
- [ ] RFC 9457 problem responses consistent.
- [ ] Cursor pagination used for collections.
- [ ] ETag/If-Match concurrency policy works.
- [ ] Completion idempotency works.
- [ ] Request IDs propagated/returned.
- [ ] Household-scoped resource paths remain future-compatible with multi-household support.
- [ ] No future-provider-specific endpoint leaks into V1 core.

I=tests_quality
- [ ] Required CI includes unit, integration, contract, frontend, E2E, build, migration checks from TESTING.
- [ ] Go race detector passes.
- [ ] Integration tests use real PostgreSQL.
- [ ] Playwright E2E uses production-built app + real PostgreSQL.
- [ ] Both fixed and fluid recurrence are covered at unit/integration/E2E levels.
- [ ] Primary V1 journeys from TESTING covered.
- [ ] No known flaky/ignored release-blocking tests.
- [ ] No known release-blocking defects.
- [ ] Fixed regressions have regression tests.
- [ ] Test review confirms assertions protect behavior/invariants, not just execution.

J=security_ops
- [ ] No secrets committed/logged.
- [ ] No telemetry/external tracking by default.
- [ ] CSRF/origin/session protections tested.
- [ ] Forged forwarded headers from untrusted clients ignored.
- [ ] Database migrations work from clean DB + applicable upgrade fixtures.
- [ ] Container non-root/read-write boundaries reviewed.
- [ ] Config validation fails clearly on unsafe/invalid production settings.

K=documentation
- [ ] README follows DOCUMENTATION style and targets humans/non-developer self-hosters.
- [ ] README contains no AI-slop/architecture dump/unimplemented claims.
- [ ] Install/first-run path copy-pasteable and tested.
- [ ] Admin docs cover reverse proxy, config, OIDC, update, backup/restore, troubleshooting.
- [ ] Developer docs cover local setup, API, tests.
- [ ] `.env.example`/config reference matches implementation.

L=scope
- [ ] No V2+ integration became a V1 dependency.
- [ ] ROADMAP remains future direction only.
- [ ] PRODUCT/UX/ARCHITECTURE/API/TESTING/DOCUMENTATION constraints remain satisfied.
