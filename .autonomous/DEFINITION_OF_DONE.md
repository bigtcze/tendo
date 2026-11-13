# Definition of Done (evergreen release gates)

status=canonical_version_independent
source_of_truth=this_file
scope=every_release_and_every_behavior_or_security_change
not_in_scope=stage_bootstrap_steps, controller_stop_conditions, completion_markers
historical_evidence=previous release artifacts, tags and `*_COMPLETE` markers are immutable evidence and are never edited, deleted or treated as a stop condition (AGENTS "Completion")

Requirements below are acceptance requirements. Passing a check here does not by itself prove the requirement; each item needs fresh evidence recorded in the release Issue or PR.

## Deployment and release artifacts
- [ ] Fresh clone, documented config and `docker compose up -d` start Tendo and PostgreSQL with exactly two services (ARCHITECTURE "Deployment and startup").
- [ ] The production image contains the built frontend. There is no separate frontend runtime container.
- [ ] Core app works with no mandatory SaaS or external IdP.
- [ ] App serves HTTP; the reverse proxy handles HTTPS. Caddy, Nginx and Traefik docs match tested configs.
- [ ] Trusted-proxy behavior and `TENDO_PUBLIC_URL` are implemented and documented.
- [ ] `TENDO_HOST_IP` defaults to `127.0.0.1`; all-interface bind is an explicit documented opt-in (ARCHITECTURE "Secure host IP").
- [ ] Health, readiness and graceful shutdown work.
- [ ] Backup and restore are documented and smoke-tested.
- [ ] Published release: annotated tag on green main, GitHub Release with human-written notes, public multi-arch GHCR image (amd64 and arm64), downloadable Compose and env assets, all verified after publication.
- [ ] Clean install and in-place upgrade from the previous published release succeed with existing data preserved, including OIDC login and backup/restore against published artifacts. Comprehensive release qualification is repeatable and enforced.
- [ ] Every outstanding PR, including dependency PRs, is explicitly reviewed in the release Issue/PR; no unexplained untriaged dependency PR remains. A blocked PR crosses release only with evidence and a justified decision (FLOW "Release from owned Issues").

## Identity, household and security
- [ ] First-run onboarding asks for the household name, confirms the timezone and creates an admin membership with the default household.
- [ ] Local login uses secure hashing, session handling and cookies (SECURITY.md "Local authentication").
- [ ] OIDC works with a deterministic test provider and documented PocketID setup. OIDC login alone never grants household membership.
- [ ] Invitation-based multi-user flow works without outbound email. Invitation cleanup meets SECURITY.md "Invitations".
- [ ] Authorization is enforced server-side for every mutation and read. Household isolation is enforced in storage and API.
- [ ] Instance admin and household admin scopes match SECURITY.md. Last-admin protection holds under concurrency.
- [ ] Local password change, admin reset and account deletion behave as SECURITY.md specifies, including session and token revocation.
- [ ] MCP OAuth, per-user scope and disable behavior match SECURITY.md "MCP".
- [ ] No secrets committed or logged. No telemetry or external tracking by default.
- [ ] Config validation fails clearly on unsafe production settings.
- [ ] Container runs non-root with explicit writable paths. The startup privilege tradeoff is documented as an accepted reduction, never as equal isolation (ARCHITECTURE "App startup privilege tradeoff").

## Items, recurrence and lifecycle
- [ ] Create, edit and archive one-off items. Archive preserves history.
- [ ] Repeat toggle exists and defaults OFF. Repeat OFF requires no recurrence fields.
- [ ] Repeat ON reveals the interval and a separate fluid toggle. Fluid OFF is fixed; Fluid ON is after-completion.
- [ ] Every golden case and mandatory coverage item in RECURRENCE.md passes at the pure domain, real-PostgreSQL, API, UI Done/Undo and MCP layers.
- [ ] Switching recurrence mode, turning Repeat off and editing policy preserve history.
- [ ] Optional attention date, optional historical completion and past computed dates behave as RECURRENCE.md defines.
- [ ] In progress, waiting, paused and resumed states never advance recurrence.
- [ ] Double submission and retries cannot create two completions or skip a cycle (API.md "Completion idempotency").
- [ ] Concurrent update and completion behavior is safe and tested.
- [ ] Household timezone and date-only behavior is tested, including DST and year boundaries.
- [ ] Same-date monthly Done regression is covered by a test that failed before the fix.

## User experience
- [ ] Responsive web works at 320/360/390/768/1024/1440/1920 px. No horizontal overflow at 200% and 400% zoom.
- [ ] Home answers "What needs my attention?" calmly. Other states remain reachable.
- [ ] Repeat and fluid are two distinct, understandable controls.
- [ ] Completion is fast and recoverable.
- [ ] Navigation includes Archive on phones and desktop; the household selector is accessible.
- [ ] Favicon, browser title and app icons are correct in all themes and languages.
- [ ] Czech and English paths work, including long Czech strings.
- [ ] Light, dark and system themes persist and do not flash on first paint.
- [ ] WCAG 2.2 AA-oriented checks cover contrast, keyboard, focus, semantics, forms, errors, touch targets and reduced motion.
- [ ] Empty, loading, error, permission and offline states work.
- [ ] EVERY route/state, including admin and MCP, is redesigned consistently under UX.md, existing unwanted Home/header marketing is removed, and independent skeptical senior-design review and real beginner journeys are completed with findings fixed. Visual baselines follow UX "Production screenshot workflow". No claimed human approval without a human action.
- [ ] Real production-build screenshots are captured for the release. AI renders and concept art are never labelled as product screenshots.

## Lists, scale and history
- [ ] Search, filters, counts and sorting operate on the full matching household dataset with cursor pagination (API.md "Collections").
- [ ] Practical behavior with 1000+ items: no unbounded fetch, no unbounded DOM, deterministic query and performance checks.
- [ ] Completion history is paginated and ordered stably; last completion and next attention are visible when applicable.
- [ ] Expired invitations are absent from every list, page and count.

## API
- [ ] `/api/v1` follows API.md conventions. OpenAPI is valid and complete for implemented endpoints.
- [ ] Go transport and TypeScript client are generated from the contract without drift.
- [ ] RFC 9457 problem responses are consistent.
- [ ] ETag and If-Match concurrency work as documented.
- [ ] Completion idempotency works as documented.
- [ ] Request IDs are propagated and returned.

## Tests and quality
- [ ] Required CI includes the layers in TESTING.md "Required CI target".
- [ ] Go race detector passes. Integration tests run against real PostgreSQL.
- [ ] Playwright E2E runs against the production build and real PostgreSQL, covering TESTING.md primary journeys.
- [ ] Test strength has not been reduced: no skipped, retried, weakened or over-mocked required test (TESTING.md "Quality bar").
- [ ] Independent review has challenged assertions. Critical assertions were shown to fail when the behavior is deliberately broken in an isolated environment.
- [ ] Maintainability check passes with no new or worsened debt (CODE_QUALITY.md).
- [ ] Full local verification passes with the pinned toolchain (AGENTS "Tooling").
- [ ] No known release-blocking defect, flaky test or ignored failure remains.

## Documentation
- [ ] README and user, admin and developer docs match the shipped behavior and version claims. No unimplemented claims.
- [ ] Install and first-run path is copy-pasteable and tested.
- [ ] Admin docs cover reverse proxy, configuration, OIDC, updates, backup and restore, troubleshooting, the startup privilege tradeoff and the host IP opt-in.
- [ ] `.env.example` matches implementation.
- [ ] Czech and English examples explain fixed vs after-completion recurrence (RECURRENCE.md).
- [ ] Release notes are human-written and professional.

## Scope and governance
- [ ] No unauthorized product scope: ROADMAP is direction only, and PRODUCT "Explicitly deferred or excluded" still holds.
- [ ] Every change lands through an independently reviewed protected PR with actual `required` green on its current head before merge, and verified green resulting main after merge.
- [ ] Migrations have a reviewed compatibility decision for every reinterpretation or destructive step, and a populated-upgrade test.
- [ ] Owned release Issues carry their evidence and claims as comments and PRs (AGENTS "Context rules"). Nothing else tracks status.
