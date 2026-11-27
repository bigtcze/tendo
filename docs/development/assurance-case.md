# Security assurance case

This document argues why Tendo meets its security requirements and points to the tests that show it. It covers the threat model, the trust boundaries, and one section for each security property, linking the code that enforces the property to the tests that check it. It ends with the static analysis Tendo runs and the gaps that are known.

The requirements themselves are defined in [`.autonomous/SECURITY.md`](../../.autonomous/SECURITY.md) and [`.autonomous/ARCHITECTURE.md`](../../.autonomous/ARCHITECTURE.md). The decisions behind them are in the [ADRs](../adr/). This document does not restate them. To report a vulnerability, follow the [security policy](../../.github/SECURITY.md).

Test paths are relative to `backend/` unless shown otherwise. Layers: **unit** runs in `go test`; **PG** runs against real PostgreSQL in `scripts/setup-integration.sh`; **E2E** is the Playwright suite run by `scripts/e2e-smoke.sh`; **smoke** is a deployment script that runs in the `deployment` job of required CI or in release qualification ([CI job map](testing.md)).

## Threat model

Tendo is a self-hosted household app. One installation serves one or more households. Each household's data is private to its members.

Assets:

- household data: people, things, items, completions and history;
- account credentials: local password hashes, session tokens, OIDC identity bindings;
- delegated access: invitation tokens, password-reset links, MCP OAuth codes, access tokens and refresh tokens;
- administrative authority: the household admin role and the instance administrator.

Attackers considered:

| Attacker | Can do | Main defenses |
|---|---|---|
| Anonymous network client | Send any HTTP request to the public URL | Origin and authority checks, authentication on every API route, rate limits, request size and time limits |
| Malicious website visited by a signed-in user | Make the browser send cross-site requests with its cookies | `SameSite=Lax` cookies, canonical `Origin` required on unsafe methods, `frame-ancestors 'none'` |
| Signed-in member of another household | Use a valid session to probe other households | Server-side membership check on every household-scoped query, identical 404s |
| Household member without the admin role | Try admin-only actions | Server-side role checks, last-admin protection |
| Holder of a stolen or leaked token | Replay a session, invitation, reset or MCP credential | Tokens stored only as digests, single use, expiry, revocation on password change, refresh-replay detection |
| Malicious or compromised OIDC provider response | Send forged or replayed ID tokens | State, nonce and PKCE, issuer and audience checks, issuer-scoped email trust |
| Malicious MCP client | Request more scope than granted, replay codes | PKCE S256, exact redirect and resource, scope intersection, refresh rotation |
| Untrusted client claiming to be the proxy | Forge `Forwarded` or `X-Forwarded-*` | Forwarded headers trusted only from `TENDO_TRUSTED_PROXY_CIDRS` |

Out of scope, as in the [security policy](../../.github/SECURITY.md#scope): an attacker who already controls the host, the Docker daemon, the app container or the database, and weaknesses in a reverse proxy or identity provider that the operator runs.

## Trust boundaries

1. **Browser or MCP client to reverse proxy.** Untrusted. The proxy terminates TLS and must forward exactly one sanitized hop ([reverse proxy trust boundary](../admin/reverse-proxy.md#trust-boundary)).
2. **Reverse proxy to app.** Trusted only for peers inside `TENDO_TRUSTED_PROXY_CIDRS`. Any other peer is treated as a direct client, and its forwarded headers are ignored. The published app port binds to `127.0.0.1` unless the operator opts in ([configuration](../admin/configuration.md#make-the-app-port-reachable-from-other-devices)).
3. **App to PostgreSQL.** The app serves with a restricted runtime role and refuses to start with a superuser or a role that has memberships. Only the startup phase uses the admin connection, for role bootstrap and migrations. PostgreSQL publishes no host port.
4. **App to OIDC provider.** The provider is trusted to authenticate users, not to name Tendo accounts. Its responses are checked as described in [OIDC](#oidc).
5. **Frontend to API.** The frontend is untrusted input to the API. Every authorization decision is made on the server.

## Properties and evidence

### Household isolation

Enforcement: every household-scoped repository query filters by household and by the caller's membership; services map "not a member" and "does not exist" to the same error ([ADR 0002](../adr/0002-household-access-policy.md), [ADR 0015](../adr/0015-multi-household-membership.md)).

| Claim | Test | Layer |
|---|---|---|
| Non-member, malformed and missing household IDs give one identical 404 | `internal/household/httpapi/household_test.go` `TestNotFoundResponsesAreIdentical` | unit |
| Completions refuse a foreign household | `internal/item/httpapi/completion_test.go` `TestCreateCompletionHouseholdIsolation`, `TestUndoCompletionHouseholdIsolation` | unit |
| A user in two households sees each household's items, summary and subjects separately | `internal/item/postgres/item_test.go` `TestItemServiceHouseholdIsolationForDualMembers`; `internal/item/postgres/summary_test.go` `TestSummaryIsolatesDualMemberHouseholds`; `internal/subject/postgres/subject_test.go` `TestSubjectDualAdminHouseholdIsolation`; `internal/household/postgres/accounts_dual_member_test.go` `TestAccountsDualMemberHouseholdIsolation` | PG |
| Invitations of another household cannot be listed or revoked | `internal/identity/postgres/invitations_test.go` `TestInvitationPostgresNonOwnerAndForeignHouseholdIsolation` | PG |
| A foreign household looks like a missing one in the browser | `frontend/e2e/journey.spec.ts` `n10` | E2E |

### Roles and administration

Enforcement: `internal/household` services check the household admin role and the instance administrator grant before any write; removals and demotions take the global lock order in [ADR 0019](../adr/0019-household-membership-removal.md) so a household can never lose its last admin.

| Claim | Test | Layer |
|---|---|---|
| Members are refused admin actions against real data, including after an in-flight demotion | `internal/household/postgres/accounts_test.go` `TestAccountsAgainstPostgres`, `TestInFlightDemotionForbidsAdminWrites` | PG |
| The refusal is 403 `admin_required` over HTTP | `internal/household/httpapi/removal_test.go` `TestRemoveMemberHTTPContractAndErrors`; `internal/household/httpapi/accounts_test.go` `TestGetAccountHTTPETagAndNotFound` | unit |
| Instance-admin routes refuse others with 403 `instance_admin_required` before validating the request | `internal/household/httpapi/admin_test.go` `TestAdminRoutesInstanceAdminGatePrecedesValidation` | unit |
| Concurrent demotions and self-leaves never leave a household without an admin | `internal/household/postgres/admin_test.go` `TestInstanceAdminCenterRacesAgainstPostgres` | PG |
| Invitations and OIDC sign-in never grant instance administrator | `internal/identity/postgres/instance_admin_test.go` `TestInstanceAdminNotGrantedByInvitationOrOIDC` | PG |
| Deleting an account ends its sessions and memberships | `internal/household/postgres/account_deletion_test.go` `TestAccountDeletionAgainstPostgres`, `TestAccountDeletionRacesAgainstPostgres` | PG |

### Cross-site request forgery and origin

Enforcement: `internal/platform/httpx/origin.go` refuses a wrong scheme or authority with 421 and an unsafe method without the canonical `Origin` with 403, before authentication runs. The only exemption is the MCP endpoint list, which uses bearer tokens instead of cookies.

| Claim | Test | Layer |
|---|---|---|
| Authority, `Origin` and forwarded-header rules | `internal/platform/httpx/origin_test.go` `TestOriginMiddleware`, `TestOriginRejectsUnicodeFoldedBrowserHost` | unit |
| Every unsafe `/api/v1/` operation in the OpenAPI contract needs a valid `Origin` before authentication | `cmd/tendo/security_contract_test.go` `TestContractUnsafeOperationsRequireOriginBeforeAuthentication`, `TestComposedRoutesEnforceContractSecurity` | unit |
| The exemption covers only the listed MCP method and path pairs | `internal/platform/httpx/origin_crossorigin_test.go` `TestCrossOriginAnyExemptsOnlyListedMethodAndPath`; `cmd/tendo/mcp_composition_test.go` `TestProductionOriginExemptionIsExactlyTheMCPList` | unit |
| The real proxies pass a foreign `Origin` through to a 403 | `scripts/proxy-smoke.sh` (`Origin: https://attacker.test`) | smoke |

### Sessions and cookies

Enforcement: `internal/identity/httpapi/session.go`, `oidc.go`, `oidc_pending.go` and `mcp_authorize.go` set every cookie `HttpOnly` and `SameSite=Lax`. When `TENDO_PUBLIC_URL` is https they also set `Secure`, the session and OIDC cookies use the `__Host-` prefix, and the path-scoped MCP consent cookie uses `__Secure-`. The session token is stored only as a SHA-256 digest ([ADR 0001](../adr/0001-local-session-model.md)).

| Claim | Test | Layer |
|---|---|---|
| Session cookie attributes on https and http; token never in the body | `internal/identity/httpapi/session_test.go` `TestLoginCookiePolicyFollowsPublicURL` | unit |
| OIDC flow and pending cookies | `internal/identity/httpapi/oidc_test.go` `TestOIDCStartCookiePolicy`; `internal/identity/httpapi/oidc_pending_test.go` `TestOIDCPendingCancelAndDiscard` | unit |
| MCP consent cookie | `internal/identity/httpapi/mcp_authorize_test.go` `TestAuthorizeSuccessSetsBindingCookieAndNeverReadsSession`, `TestAuthorizePlainHTTPUsesUnprefixedCookie` | unit |
| Attributes survive the real proxies, with no `Domain` | `scripts/proxy_smoke_oidc.py` `assert_host_cookie_attrs` | smoke |
| Only the digest is stored; sessions expire after 30 days | `internal/identity/session_test.go` `TestLoginStoresOnlyDigestAndExpiresInThirtyDays` | unit |
| Password reset and password change end the target's other sessions | `internal/identity/postgres/password_reset_test.go` `TestPasswordResetPostgresRedeemSuccessAndFailures`; `internal/identity/postgres/password_change_test.go` `TestPasswordChangePostgresRotatesCallerAndSignsOutEverywhere`; `internal/identity/postgres/session_credential_race_test.go` `TestLoginRacingPasswordResetKeepsNoOldPasswordSession` | PG |

### Local passwords and rate limits

Enforcement: `internal/platform/security/password.go` hashes with Argon2id (64 MiB, 3 iterations, 1 lane, 16-byte salt) and bounds concurrent hashing. Unknown logins are hashed against a fixed dummy hash so they take as long as a wrong password. Limiters in `internal/identity/httpapi` answer 429 with `Retry-After`.

| Claim | Test | Layer |
|---|---|---|
| Hash format, verification and refusal of malformed hashes | `internal/platform/security/password_test.go` `TestPasswordHashAndVerify`, `TestVerifyPasswordRejectsMalformedOrUnsupportedHashes`, `TestPasswordLengthLimit` | unit |
| Hashing concurrency is bounded | `internal/platform/security/password_test.go` `TestPasswordGateBoundsConcurrentHoldersAndAdmitsAfterRelease` | unit |
| Login failures are indistinguishable and write nothing | `internal/identity/session_test.go` `TestLoginFailuresAreIndistinguishableAndCreateNothing` | unit |
| Login, password reset, password change and invitation acceptance are rate limited | `internal/identity/httpapi/session_test.go` `TestLoginRateLimits`; `internal/identity/httpapi/password_reset_test.go` `TestPasswordResetIssueStrictBodyAndLimiter`, `TestPasswordResetIssueTargetThrottle`, `TestPasswordResetRedeemHTTPContract`; `internal/identity/httpapi/password_change_test.go` `TestPasswordChangePerAccountLimit`, `TestPasswordChangeHourlyLimit`, `TestPasswordChangePerAddressLimitPrecedesAuthentication`; `internal/identity/httpapi/invitations_test.go` `TestPerClientAcceptanceRateLimits` | unit |
| A wrong current password changes nothing; concurrent changes have one winner | `internal/identity/postgres/password_change_test.go` `TestPasswordChangePostgresRejectsWithoutWriting`, `TestPasswordChangePostgresConcurrentChangesHaveOneWinner` | PG |

### OIDC

Enforcement: `internal/identity/oidc_stages.go` creates random state, browser binding, nonce and PKCE verifier; `internal/identity/oidcprovider/provider.go` validates the ID token; `internal/identity/postgres/oidc_jit.go` binds accounts to the issuer and subject pair and trusts a verified email only for the issuer that verified it ([ADR 0008](../adr/0008-oidc-account-linking.md), [ADR 0012](../adr/0012-oidc-jit-and-verified-email.md)).

| Claim | Test | Layer |
|---|---|---|
| Bad signature, wrong issuer, audience or `azp`, expiry, nonce mismatch, bad subject and disallowed algorithms are refused | `internal/identity/oidcprovider/provider_test.go` `TestProviderValidation`; `exchange_order_test.go` `TestExchangeValidationOrder` | unit |
| PKCE S256 challenge, nonce and state are sent; the verifier is checked by the provider at exchange | `internal/identity/oidcprovider/provider_test.go` `TestProviderSuccessAndAuthorizationParameters` (the test provider in `internal/testoidc` refuses a wrong verifier) | unit |
| The callback is single use and strict | `internal/identity/oidc_test.go` `TestOIDCCallbackConsumeReplayCancelAndStrictParams` | unit |
| Email trust is scoped to the issuer; one account per subject under concurrency | `internal/identity/postgres/oidc_jit_test.go` `TestOIDCJITEmailTrustIsIssuerScoped`, `TestOIDCJITConcurrentCallbacksSameSubjectCreateOneUser` | PG |
| A provider outage does not affect local sign-in | `internal/identity/postgres/oidc_outage_test.go` `TestOIDCProviderOutageDoesNotAffectLocalLogin` | PG |
| The authorization URL ignores forged forwarded headers | `scripts/proxy_smoke_oidc.py` (`forged=True`) | smoke |

### MCP OAuth

Enforcement: `internal/identity/mcp_authorize.go`, `mcp_token.go` and `mcp_policy.go` implement per-user OAuth with PKCE S256, an exact resource indicator, scope intersection with the instance policy, and refresh-token rotation with replay revocation ([ADR 0023](../adr/0023-mcp-oauth-grant-store.md)).

| Claim | Test | Layer |
|---|---|---|
| PKCE is S256 with a constant-time comparison; failed exchanges create nothing | `internal/identity/mcp_oauth_test.go` `TestMCPPKCES256Matches`, `TestMCPExchangeCodeRefusalsCreateNothing` | unit |
| A missing or other resource indicator is refused with `invalid_target` | `internal/identity/mcp_authorize_test.go` `TestMCPBeginAuthorizationResourceAbsentOrConfiguredOnly`; `internal/identity/httpapi/mcp_authorize_test.go` `TestAuthorizeErrorsAlwaysRedirectToConnect` | unit |
| Refresh rotates; a replayed refresh token or code revokes the grant and deletes its secrets | `internal/identity/mcp_oauth_test.go` `TestMCPRefreshRotatesAndReplayRevokesCommitted`; `internal/identity/postgres/mcp_grants_test.go` `TestMCPCodeReuseRevokesGrantAndDeletesEverySecret`, `TestMCPConcurrentRefreshReplayExactlyOneWinner` | unit, PG |
| Only access tokens work as bearer: refresh tokens, codes, session tokens and JWT-shaped strings get 401 | `internal/household/postgres/mcp_endpoint_tokens_test.go` `TestMCPEndpointRejectsNonAccessTokensAsBearer` | PG |
| Revocation and password change stop access on the next request | `internal/household/postgres/mcp_endpoint_revocation_test.go` `TestMCPEndpointRejectsRevokedAndEndedGrantsOnTheNextRequest` | PG |
| The MCP endpoint refuses browser origins and a wrong host | `cmd/tendo/mcp_enabled_composition_test.go` `TestEnabledMCPEndpointRefusesBrowserOriginsAndWrongHost` | unit |

The consent handle is a bearer secret for one pending consent. The browser keeps it in the address fragment, which is never sent to a server, and in `sessionStorage` only while the person finishes the consent (`frontend/src/features/mcp/connectEntry.ts`). It is also bound to the `HttpOnly` consent cookie, so the handle alone cannot approve a consent.

### Invitations and password-reset links

Enforcement: invitation and reset tokens are stored as SHA-256 digests, are single use and expire; terminal rows keep no digest ([ADR 0007](../adr/0007-invitation-admission.md), [ADR 0024](../adr/0024-invitation-digest-retention.md), [ADR 0017](../adr/0017-password-reset-account-authority.md)).

| Claim | Test | Layer |
|---|---|---|
| Only the digest is stored; a retry returns no token | `internal/identity/postgres/invitations_test.go` `TestInvitationPostgresDigestIdempotencyConcurrencyAndAdmission` | PG |
| Accept against revoke, and concurrent accepts, have one outcome | `internal/identity/postgres/invitations_test.go` `TestInvitationPostgresConcurrentAcceptAndRevoke`; `invitations_concurrency_test.go` `TestInvitationPostgresConcurrentAcceptOneWinner` | PG |
| Terminal rows hold no digest; the sweep clears only expired digests | `internal/identity/postgres/invitations_retention_test.go` `TestInvitationRetentionTerminalRowsHoldNoDigest`, `TestInvitationRetentionSweepClearsOnlyExpiredDigests` | PG |
| A reset link from another household stays hidden from non-admins | `internal/identity/postgres/password_reset_cross_household_test.go` `TestPasswordResetPostgresLinkFromNonAdminOfHouseholdStaysHidden` | PG |

### Secrets in logs

Enforcement: the request log in `internal/platform/httpx` records route templates without query strings, replaces unsafe request IDs and logs faults with a fixed class. Errors that leave the database layer carry no connection details.

| Claim | Test | Layer |
|---|---|---|
| Query strings and unsafe request IDs are not logged | `internal/platform/httpx/server_test.go` `TestMiddlewareRequestIDAndSafeLogging`, `TestRecordFaultRaisesAccessLogToErrorWithFixedClass` | unit |
| OIDC callback logs contain no code, state or cookie | `internal/identity/httpapi/oidc_test.go` `TestOIDCCallbackRequestLogOmitsSecrets` | unit |
| MCP logs contain no code, token or verifier | `internal/identity/httpapi/mcp_authorize_log_test.go` `TestMCPRequestLogsCarryNoSecrets` | unit |
| Database errors contain no URLs or role names | `internal/platform/database/serving_role_test.go` `TestValidateServingRoleAcceptsRuntimeAndRejectsMigrator` | PG |

### Reverse proxy trust

Enforcement: `internal/platform/httpx/origin.go` reads `Forwarded` or `X-Forwarded-*` only from peers in `TENDO_TRUSTED_PROXY_CIDRS` and refuses conflicting or repeated values; `internal/platform/config` requires a canonical `TENDO_PUBLIC_URL`.

| Claim | Test | Layer |
|---|---|---|
| Forged headers from an untrusted peer are ignored; conflicting or repeated headers from a trusted peer are refused | `internal/platform/httpx/origin_test.go` `TestOriginMiddleware` | unit |
| `TENDO_PUBLIC_URL` and proxy CIDRs are validated | `internal/platform/config/config_test.go` `TestCanonicalPublicURL`, `TestLoadFromProxyCIDRs` | unit |
| Caddy, Nginx and Traefik strip client forwarding and reject a host mismatch; a direct client with forged headers gets 421 | `scripts/proxy-smoke.sh` | smoke |
| The published port binds to `127.0.0.1` unless `TENDO_HOST_IP` opts in | `scripts/compose-startup-smoke.sh` | smoke |

### Database role and startup

Enforcement: `cmd/tendo/startup.go` bootstraps the runtime role and migrates with the admin connection, then serves with the runtime role only after `internal/platform/database/serving_role.go` confirms it is not elevated ([ADR 0013](../adr/0013-app-startup-database-initialization.md)). SQL is written in sqlc query files and runs as parameterized statements; the only concatenated SQL is the bootstrap `GRANT`, whose identifiers are quoted with `pgx.Identifier.Sanitize`.

| Claim | Test | Layer |
|---|---|---|
| The serving role must not be elevated or hold memberships | `internal/platform/database/serving_role_test.go` `TestValidateServingRoleAcceptsRuntimeAndRejectsMigrator` | PG |
| Bootstrap is idempotent, serialized, and refuses other roles | `internal/platform/database/bootstrap_test.go` `TestBootstrapRoleIsIdempotentAndAppliesGrants`, `TestBootstrapRoleSerializesConcurrentInstances`, `TestBootstrapRoleRefusesOtherRolesAndMissingPassword` | PG |
| Dirty, newer, older or missing migration state is refused | `internal/identity/postgres/setup_test.go` `TestSetupPostgresAtomicAndSingleWinner` | PG |
| No listener opens when the privileged phase or the role check fails | `cmd/tendo/startup_test.go` `TestStartupDoesNotListenWhenPrivilegedPhaseFails`, `TestStartupDoesNotListenWhenRuntimeRoleViolatesRestriction` | unit |

### HTTP hardening

Enforcement: `cmd/tendo/main.go` `newRuntimeServer` sets header, read, write and idle timeouts and a 16 KiB header limit. `internal/platform/httpx/jsonapi.go` caps request bodies and answers 413. `internal/platform/webui/webui.go` serves only embedded files with a strict Content-Security-Policy, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

| Claim | Test | Layer |
|---|---|---|
| Header timeout, idle timeout and header size limit | `cmd/tendo/runtime_test.go` `TestRuntimeServerBoundsHeadersAndIdleConnections` | unit |
| The write timeout covers the read and service budgets | `cmd/tendo/runtime_test.go` `TestRuntimeServerWriteBudgetCoversReadAndServiceBudgets` | unit |
| Body size and media type limits | `internal/platform/httpx/jsonapi_test.go` `TestReadObjectMediaTypeAndLimit` | unit |
| Exact CSP; framing, sniffing and referrer headers; no path traversal | `internal/platform/webui/webui_test.go` `TestDocumentCSPIsExactlyUnchanged`, `TestIndexFallback`, `TestConsentPageCannotBeFramed`, `TestTraversalNeverEscapes` | unit |

### Container

Enforcement: the `Dockerfile` builds a static binary on a distroless `nonroot` base pinned by digest. `compose.yaml` runs the app as `65532:65532` with a read-only root filesystem, a small `noexec` `/tmp`, `no-new-privileges` and all capabilities dropped.

| Claim | Test | Layer |
|---|---|---|
| Non-root user, read-only root filesystem, no capabilities, `no-new-privileges`, PostgreSQL without a host port | `scripts/setup-smoke.sh` (`docker inspect` checks) | smoke |

### Frontend

Enforcement: the React frontend renders text through JSX escaping; production code uses no `dangerouslySetInnerHTML`, `innerHTML`, `eval` or `new Function`. The CSP forbids inline and foreign scripts. Authentication lives only in the `HttpOnly` session cookie; `localStorage` holds only theme and language.

| Claim | Test | Layer |
|---|---|---|
| An unknown OIDC error code containing markup renders as a fixed message, never as markup | `frontend/src/features/auth/Oidc.test.tsx` (`/login#oidcError=%s shows a calm notice and scrubs the fragment`) | unit |
| The CSP forbids inline scripts | `internal/platform/webui/webui_test.go` `TestDocumentCSPIsExactlyUnchanged` | unit |

## Static analysis

Every pull request and the local gate run these checks; a finding fails the build ([CI job map](testing.md)):

- `golangci-lint` with `go vet`, `staticcheck`, `errcheck` and [gosec](https://github.com/securego/gosec) for security-oriented analysis of production Go code (`backend/.golangci.yml`);
- ESLint and TypeScript strict type checking for the frontend;
- `npm audit --audit-level=moderate` for the API tooling and the frontend;
- Dependabot alerts and Renovate updates for dependencies ([dependencies](dependencies.md)).

gosec does not check tests or the test-only OIDC provider (`internal/testoidc`, `test/oidc-provider`), which are never linked into `cmd/tendo`. Each accepted production finding is an exclusion rule in `backend/.golangci.yml` that names the gosec rule, the file and, where possible, the exact source line, with the reason it cannot be exploited and the owning module. `exclusions.warn-unused` reports a rule that no longer matches anything.

| Rule | Where | Why it is accepted |
|---|---|---|
| G704 (SSRF) | `cmd/tendo/startup.go` healthcheck | Dials the app's own readiness endpoint on loopback or the operator's `TENDO_LISTEN_ADDR`; no request input reaches the URL |
| G705 (XSS) | `internal/platform/webui/webui.go` | Writes files embedded in the binary at build time, never request input |
| G115 (integer conversion) | `internal/identity/postgres/oidc_jit.go` | Intended bit-for-bit reinterpretation of a SHA-256 prefix as a PostgreSQL advisory-lock key |
| G115 | `internal/{item,subject,household}` create fingerprints | Length prefixes of fields from bodies capped far below 2^32 |
| G115 | `internal/*/postgres` page limits and recurrence intervals | Services clamp limits to at most 100 and validate intervals to 1..999 before the repository |
| G115 | `internal/item/postgres/item_rows.go` | Anchor days are validated to 1..31 |
| G124 (cookie attributes) | `internal/identity/httpapi` cookie writers | Cookies are `HttpOnly` and `SameSite=Lax`, `Secure` on https; see [sessions and cookies](#sessions-and-cookies) |
| G101 (hardcoded credentials) | `internal/identity/session.go`, `httpapi/mcp_token_endpoint.go` | A public dummy-password constant used for timing equalization, and the token endpoint path |

## Known gaps

These are not covered by an automated test today. None is a known vulnerability.

- No global test scans every log line for passwords and session tokens; the log tests above cover the request log, OIDC and MCP paths.
- No test runs SQL injection payloads; protection rests on sqlc's parameterized queries and the review rule that SQL is never concatenated with input.
- Bootstrap identifier quoting is not tested with an unusual database name.
- No end-to-end browser test sends a cross-origin POST; the origin rules are covered by unit and proxy smoke tests.
- No end-to-end test renders hostile text in item or subject names; rendering relies on JSX escaping and the CSP.
