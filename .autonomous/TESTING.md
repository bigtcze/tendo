# Test strategy

status=canonical_version_independent
source_of_truth=this_file
principle=tests_prove_behavior_not_presence
required_layers=unit,integration,e2e,contract,build,security,migration
recurrence_cases=RECURRENCE.md "Mandatory test coverage" (single source; not repeated here)
toolchain=`bash scripts/dev-exec.sh -- <command>` (pinned mise toolchain; see docs/development/testing.md for setup)
full_gate=`bash scripts/dev-exec.sh -- bash scripts/verify-local.sh`

## Quality bar (strength is not relaxed)
- A test must be capable of failing when the behavior it protects is broken.
- Do not write tests only to raise coverage. Coverage is diagnostic, not a substitute for invariant review.
- Do not mock the thing under test. Do not over-mock.
- Domain tests assert state and invariants, not implementation call counts.
- HTTP tests assert the response contract AND the resulting persisted or domain state where relevant.
- A `200` assertion alone is not a meaningful API test.
- Every bug fix gets a regression test that FAILS on the pre-fix code before the fix. Document what failed first.
- Flaky tests are defects. Fix the root cause; never hide failures with retries.
- Required CI has zero ignored failures and no blanket skips. Never merge by temporarily removing a failing layer.
- Tests are not weakened to pass CI, to accommodate design, or to speed up a PR.
- Test code must not reimplement production logic.
- Independent review challenges assertions. For critical cases, demonstrate the assertion fails when the behavior is deliberately broken in an isolated environment, then restore the code.

## Native harness: real PostgreSQL, no Docker for native runs
- The native integration and E2E harness uses a real PostgreSQL (PostgreSQL 18 is the reference) started natively, through the pinned toolchain and existing harness scripts. Docker is not introduced into local native integration or E2E execution.
- Each run uses an isolated database and clean-up is rigorous. Shared production data is never used.
- Use `TENDO_TEST_POSTGRES_ADMIN_URL` for the admin connection (see docs/development/testing.md).
- Test-only application shortcuts are forbidden. The E2E backend is the real backend.
- Testcontainers or a CI service may be used for ephemeral DB in CI, but the native harness path stays Docker-free.

## Unit tests (pure domain and application)
- Fixed recurrence arithmetic, after-completion arithmetic, and month/year clamping and anchor restoration (RECURRENCE.md).
- Attention derivation, lifecycle transitions and repeat OFF / fixed / fluid transitions.
- Authorization policies, validation and idempotency helpers.
- Deterministic Clock and ID providers. Table-driven boundary cases. Explicit leap-year, end-of-month and timezone cases.
- Property and randomized tests for recurrence and invalid state transitions (RECURRENCE.md "Mandatory test coverage").
- Go race detector runs in the required backend path.

## Integration tests (real PostgreSQL)
Real PostgreSQL, never a mocked repository, for persistence and API integration.

Must cover:
- Migrations from an empty database and from populated upgrade fixtures (v1 through the current release, including v4).
- Household name and timezone first-run persistence.
- Many-to-many household memberships and household boundary enforcement.
- Repository constraints, transactions and rollback.
- Local auth and session behavior, including local-vs-OIDC session kinds.
- OIDC against a deterministic local test provider at the protocol boundary.
- CSRF and origin behavior.
- Idempotency-key persistence and semantics.
- ETag optimistic concurrency.
- Completion double-submit and concurrent completion; one ledger entry and one schedule transition per accepted command.
- Fixed and fluid recurrence persistence and transitions.
- Archive and history preservation.
- RFC 9457 problem contract.
- Last-admin protection under concurrent competing requests (SECURITY.md "Authorization invariants").
- Invitation acceptance versus revocation, and expiry cleanup races.
- Startup bootstrap and migrations: repeated and concurrent startup, dirty and newer-schema refusal, migration failure (see "Deployment, reverse-proxy and smoke tests").

## Security and identity tests (negative and adversarial)
Real backend. Each item needs a negative case.
- Household isolation: route IDs, search, subject references, completion history and counts, under parallel access by two users.
- Stale sessions and roles, membership removal, self-removal, cross-household delete, revoked members.
- Local password change: requires a local session and current password; OIDC session refused even if the account has a local password; CSRF and rate limiting.
- Admin CLI reset: no secret in argv or logs; sessions and tokens revoked.
- Admin-initiated reset token: single-use, expiry, reuse, revoke, failure paths; token stored hashed.
- OIDC: immutable issuer+sub binding, email-provenance attach only for verified unique matches, no attach for unverified, duplicate, absent or conflicting email, concurrent callbacks, email change, issuer mismatch, provider outage with local login still working.
- Invitations: cancelled and revoked rows purged with tokens invalidated; audit kept without secrets.
- MCP OAuth: PKCE, audience and issuer checks, token rotation and replay refusal, revocation effect on the next request, disabled-MCP refusal at the endpoint, per-user effective-scope intersection, cross-household exfiltration refusal, unauthorized tool calls refused, rate limits, no ID token accepted as a bearer token, two concurrent users isolated.
- Instance-admin tools are absent from MCP.
- Log and audit checks: no passwords, tokens or secrets in output.

## Migration tests
- Empty-database migrations and populated fixtures from every released version that is an upgrade source. Required for v5: a populated released v4.0.0 database.
- Data preservation: stable user, household, OIDC binding and item and subject IDs; ledger entries; history.
- Legacy accounts without email; V4 OIDC email provenance backfill; household role mapping from owner to admin.
- Invitation purge: existing cancelled and revoked secret-bearing rows removed; audit history retained.
- Subject and person reference migration is idempotent and does not auto-merge by name.
- Each destructive or reinterpreting migration has a reviewed compatibility decision recorded in the PR.

## Contract tests
- OpenAPI validates in CI.
- Generated Go and TypeScript artifacts are reproducible and drift-free.
- Every implemented public endpoint exists in OpenAPI.
- Representative success and error responses validate against schema.
- API examples remain valid.
- Date serialization follows API.md.

## Frontend tests
Protect meaningful interaction and state. Do not use whole-page snapshots as the only assertion.
- Onboarding asks for the household name and confirms the timezone.
- Repeat is OFF by default; Repeat ON reveals the interval and the separate fluid toggle; the fluid wording distinguishes fixed from after-completion.
- A one-off item does not require recurrence fields.
- Household switcher: present only where multiple memberships apply; validated on the server.
- Expired invitations are absent from every list, page and count.
- Error, empty and loading states; accessibility-critical form behavior; Czech and English smoke.
- Theme persistence and no first-paint flash.
- Visual regression: deterministic screenshot baselines across routes, breakpoints (320/390/768/1024/1440/1920), themes and key interaction states, with explicitly reviewed changes. Visual regression protects layout; it never approves design by itself.

## E2E tests
- Browser: Playwright.
- Runtime: production-built Tendo and real PostgreSQL. Mocks: none for Tendo backend or database.
- Time-sensitive flows use the deterministic test clock and configured timezone. No `sleep until tomorrow`, no arbitrary sleeps.
- Distinct browser sessions per user. Synthetic household fixtures only.
- E2E verifies actual persisted DB/API outcomes, not just toast messages, HTTP success or completed clicks. Stable paginated navigation must preserve user context across search/filter/sort, back/forward and concurrent dataset changes.

Primary journeys:
1. First-run owner onboarding: household name, timezone confirmation, empty home.
2. Create a person subject without a user account.
3. Create a one-off item with Repeat OFF; complete; verify active backlog and history.
4. Create a future one-off attention item; verify upcoming becomes needs-attention with a controlled clock.
5. Create a fixed repeating item (Repeat ON, Fluid OFF); complete late; verify the planned cadence is preserved.
6. Create a fluid repeating item (Repeat ON, Fluid ON); complete late; verify next cycle is completion plus interval.
7. In progress, then waiting, then complete; verify only completion advances recurrence.
8. Undo or correct the latest completion; verify schedule and history.
9. Invite a second user; verify role behavior.
10. Unauthorized household or resource access is blocked.
11. Responsive narrow and wide smoke for home, create and detail; 320/390/768/1440 widths.
12. Czech and English smoke.
13. Actual Done and Undo through the UI, and the same recurrence through the MCP tool path.
14. Household switching and cross-household isolation through the UI.
15. Admin center: role changes, last-admin refusal, password reset flow, account deletion confirmation.
16. Local password change; OIDC-session refusal.
17. MCP OAuth consent and token flow against the deterministic test IdP.

## Coverage discovery and risk boundaries
- Independently inventory every implemented route, actionable control, supported API boundary and persistent state against source, schema and real production browsers. Maintain the existing coverage matrix and maintainability audit as durable verification references, not a second work-status ledger; current claims/results remain in owned Issues and PRs.
- Include first-use and OIDC linking/cancellation/failure/redirects; navigation/back/forward/deep links; dialogs/forms; all subject and item lifecycle states; 401/403/404/409/412/422/429, malformed input, server errors, slow/offline network, interrupted submission, reload/relogin and restart recovery.
- Validate search/count/pagination/navigation with empty/1/10/30/100/500/1000+ records, Unicode and concurrent changes, using deterministic API/query budgets plus measured production browser evidence.
- Use risk-based boundaries, state-machine/model/property tests and pairwise combinations for large state spaces; document uncovered risks rather than claim mathematical exhaustiveness. Mutation checks on representative critical assertions strengthen independent test-quality review. Check browser interoperability beyond mandatory Chromium where practical and state unavailable coverage honestly.
- Accessibility requires automated checks and meaningful production keyboard/semantic review, real screen-reader checks where feasible, honest limitations and remediation of blocking defects. AI review is not human accessibility auditing.
- Preserve historical incident uncertainty: CI incident 39872577652 was subject to diagnostic-artifact retention proof, three complete successful E2E CI runs and twenty targeted reproduction runs after diagnostic hardening, with no new unexplained failures. Historical evidence stays historical; do not invent a root cause or require owner approval merely because an old artifact or human reviewer is unavailable. New unexplained failures remain blockers.

## Deterministic browser and security scope
- Seeded fixtures; controlled clock and timezone; isolated database per run; stable data.
- Negative browser cases are included for every security-sensitive flow.
- Playwright flakiness is a defect: no retries hiding it, no arbitrary waits.

## Deployment, reverse-proxy and smoke tests
- Compose: exactly two services; no `migrate` or `postgres-init` container; readiness gating; restricted runtime role; named PostgreSQL volume preserved on upgrade.
- Startup: fresh install; populated upgrade from published v3 and v4 fixtures; repeated and concurrent startup; migration failure, dirty and newer schema; admin credential failure and recovery. No readiness on a broken schema.
- Host IP: default binds to 127.0.0.1; explicit `TENDO_HOST_IP` opt-in is tested; OIDC callback and public origin tested with a non-default host IP.
- Reverse proxy: untrusted forged `X-Forwarded-*` ignored; configured trusted proxy headers produce correct public-origin behavior; secure session and OIDC callback follow `TENDO_PUBLIC_URL`; health endpoints work behind proxy assumptions.
- Backup and restore smoke.
- Production image build.

## Required CI target
- Stable aggregate check name: `required`.
- The PR that introduces a code layer also wires that layer's tests and checks into CI. Later PRs never remove or bypass them.
- `required` gates:
  - generated and contract drift
  - Go format, vet and static analysis
  - Go unit tests with race detector
  - Go integration tests with PostgreSQL
  - frontend lint, typecheck and unit tests
  - Playwright E2E
  - production Docker build
  - migration smoke and upgrade fixtures
  - security and identity negative suites
  - maintainability check (CODE_QUALITY.md)
- Tests must be stable `required` CI wired, not opt-in local suites only.

## Test review questions
Every behavior PR answers:
- Which user or domain behavior changed?
- Which test fails if it regresses?
- Are fixed and fluid recurrence effects considered where relevant?
- Are failure, edge, concurrency and security-negative paths covered?
- Did test code accidentally reimplement production logic?
- Is any assertion weaker than before? If yes, why, and is it justified?
