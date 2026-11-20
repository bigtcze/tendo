# ADR 0023: MCP OAuth grant store

Status: accepted

Builds on [ADR 0018](0018-local-password-change-and-cli-reset.md),
[ADR 0019](0019-household-membership-removal.md) and
[ADR 0022](0022-global-account-deletion.md). Slice A1 of per-user MCP OAuth
(Issue #456): the backend core with no HTTP surface. Discovery metadata, client
registration, the consent UI and the `/mcp` endpoint are later slices. Scope
intersection with admin, user and household limits is Issue #457.

## Context

SECURITY "MCP (per-user OAuth, V5)" requires per-user OAuth with PKCE,
short-lived audience-bound access tokens, refresh rotation, revocation and
replay protection, re-evaluated on every request. ADR 0022 requires account
deletion to erase the account's MCP credentials before the deletion record.
A password change, an admin reset and the operator CLI reset must sign the
account out everywhere (ADR 0018), and an MCP token is a sign-in.

The Go MCP SDK only provides the client side and resource-server token
verification. A general OAuth framework (for example fosite) would add a large
surface for the few flows Tendo needs, against SECURITY "no speculative
identity framework".

## Decision

- **Hand-written minimal authorization server** in `internal/identity`
  (`MCPService`). Public clients only (`token_endpoint_auth_method=none`), the
  authorization code grant with PKCE `S256` only, and the refresh token grant.
  No implicit, password or client credentials grant, and no `plain` PKCE.
- **Issuer and resource.** The issuer is `TENDO_PUBLIC_URL` and the protected
  resource is `<TENDO_PUBLIC_URL>/mcp`. The RFC 8707 `resource` parameter
  must equal the configured resource on authorization, code exchange and
  refresh; slice A3a lets an absent parameter default to it (see "Slice A3a").
  Every code, grant and token is bound to it.
- **Secrets.** Codes, access tokens and refresh tokens are 32 random bytes,
  base64url without padding (43 characters). Only their SHA-256 digests are
  stored, as for sessions. Plaintext is returned once and never logged.
- **Lifetimes.** Code: 60 seconds, single use. Access token: 10 minutes.
  Refresh token: rotated on every use, 30 days idle. Grant: 90 days absolute;
  no token outlives its grant. A pending consent request lasts 5 minutes and
  unused registered clients without grants expire after 24 hours (slice A2).
- **Grant at exchange.** Approving consent stores only a code carrying user,
  client, scopes, resource, PKCE challenge and redirect URI. The grant is
  created when the code is redeemed, and the code keeps `issued_grant_id`.
- **Replay.** Presenting a used code revokes the grant it issued
  (`code_reuse`). Presenting a consumed refresh token revokes its grant
  (`replay`). Consumed refresh digests are kept until the grant ends (revoked, or
  past its absolute expiry, when the sweep removes them) so replay stays
  detectable while the grant is live. The revocation commits even though the request
  fails with `invalid_grant`. Every refusal is the same `invalid_grant`.
- **Revocation keeps history.** A revoked grant keeps its row with
  `revoked_at` and `revoke_reason`; its codes and token rows are deleted, so a
  revoked grant holds no secret. `mcp_grant_events` is append-only and records
  creation and every revocation with its cause, a client snapshot and the
  human actor (empty for the CLI and automatic revocations).
  Presenting a code or refresh token of an already revoked grant changes
  nothing and writes no further event: its secrets are gone, so later replay
  attempts are refused but not audited. This is an accepted audit gap.
- **Revocation never fails on time.** `revoked_at`, `used_at` and
  `consumed_at` are clamped to the row's `created_at` in SQL (`GREATEST`).
  Callers truncate their clock (password replacement to the second), read it
  before lock waits, or run on another instance with a skewed clock. A
  `revoked_at >= created_at` CHECK must never roll back a password
  replacement, an account deletion or a code-reuse revocation. Exchange and
  refresh read the clock before their lock waits, so a code may be redeemed
  up to one lock wait past its 60-second expiry; this is accepted.
- **Fixed-length spans.** Lifetime CHECKs use `interval '2160 hours'`, not
  `'90 days'`, because day arithmetic on `timestamptz` follows the session
  TimeZone and a daylight-saving change would shorten the limit by an hour.
- **Revocation causes.** `user`, `password_change`, `admin_reset`,
  `cli_reset`, `account_deletion`, `replay`, `code_reuse`, and `client`
  (RFC 7009 revocation by the holding client, added by migration 030).
  `CredentialReplacementTransaction.ReplaceLocalPassword` revokes every grant
  of the account after it deletes the sessions, so self-service change, admin
  reset redemption and the CLI reset all end MCP access. Account deletion does
  the same in `EraseAccountAccess` after the credential and sessions and before
  the deletion record and tombstone. An account without a local password still
  loses its MCP grants on deletion.
- **Verification** re-reads token, grant and account on every request: the
  access token must be unexpired, the grant unrevoked and unexpired, the
  account not deleted, and the grant's resource the configured one. A
  revocation or deletion takes effect on the next request. An upstream OIDC ID
  token is never a bearer token.
- **Disabled MCP** is refused inside the service for issuance, exchange, refresh
  and verification, not only by routing. Production composition is hard-disabled
  until the flow is complete; no runtime setting enables it yet.
- **No CIMD initially.** Client ID Metadata Documents need outbound fetches,
  which ARCHITECTURE does not allow beyond optional OIDC. Dynamic client
  registration (slice A2) covers the supported clients.
- **Migration 028** creates new tables only and changes no existing row. Its
  Down drops them; that is lossy by design because earlier releases have no
  MCP endpoint, and the grant audit is recovered from a backup if needed.

## Lock order

Every MCP write runs at `READ COMMITTED` with explicit row locks, never
`REPEATABLE READ`, and takes them in the ADR 0019 global order, which this ADR
extends:

1. `user_accounts`: issuance (code, exchange, refresh) takes the live account
   `FOR SHARE`, which conflicts with deletion's `FOR NO KEY UPDATE`. An
   issuance waiting on a deletion then finds the account deleted, and a
   deletion waiting on an issuance erases what it committed.
2. `local_credentials`: issuance takes the row `FOR SHARE` when it exists,
   which conflicts with a password replacement's `FOR UPDATE` in the same way.
3. `user_sessions` (code issuance reads the approving session, which must still
   exist). The read takes no lock: logout takes neither the account nor the
   credential lock, so a code may be issued from a session deleted
   milliseconds earlier. That session approved the consent, so this is
   accepted. Password replacement and deletion do serialize, through steps 1
   and 2.
4. `mcp_authorization_codes`, then `mcp_grants`, then
   `mcp_access_tokens` / `mcp_refresh_tokens`. Refresh locks the grant's used
   codes before the grant row, because revoking the grant deletes them.
5. `mcp_clients` (A2): code issuance reads the client `FOR KEY SHARE`, after the
   user row and session and before `mcp_authorization_codes`. The retention
   sweep deletes a client only `FOR UPDATE SKIP LOCKED`, so it skips a client an
   issuance holds rather than orphaning a code being inserted.
6. `mcp_authorization_requests` (A3a): after `mcp_clients` and before
   `mcp_authorization_codes`. Authorization and the consent decision take the
   client `FOR KEY SHARE` before they insert or delete a pending request.

Every MCP path reaches an MCP row only after its account lock, so the account
and credential locks serialize issuance with deletion and with password
replacement. Locks taken earlier in ADR 0019 (`installation_state`,
`households`, advisory locks, invitations) are never taken by an MCP path.

## Consequences

- No MCP code or token row survives an account deletion or a password
  replacement, including one issued by a request that raced with it.
- A leaked refresh token is useful at most until the legitimate client next
  refreshes; after that both copies are dead and the grant is revoked.
- Grants and their events stay as minimal, secret-free history.
- The consent flow, the token endpoints, and listing or revoking grants from
  the UI are added by later slices on this store. Registration, the HTTP
  surface, and the retention sweep are complete in A2 (below).
- The retention sweep (A2) removes expired secrets and never revokes. A grant
  past its absolute expiry is unusable; the sweep deletes its codes and token
  rows, keeps the grant row, and writes no event. Expiry is not revocation.
- Grantless, codeless clients older than 24 hours are deleted.

## Slice A2: dynamic client registration and retention

Slice A2 adds RFC 7591 registration, the grantless-client cap, the registration
HTTP adapter, the RFC 9728 and RFC 8414 metadata documents, the OpenAPI
contract, CORS, the reserved paths, the request limiters, and the retention
sweep with its scheduling goroutine in `cmd/tendo`. Production composition keeps
MCP disabled: the routes answer the unknown-path 404 and the sweep still runs.
Nothing in A2 issues a grant, so no client can complete the flow yet.

### Registration policy

- A registration has 1 to 5 redirect URIs. One invalid URI refuses the whole
  registration. Duplicates are refused. Each URI is at most 2048 bytes, uses
  only RFC 3986 characters, has no `*`, `#` or userinfo, and round-trips
  through `net/url` byte for byte. The stored and returned value is the
  registered string.
- `https` may use any port and any host except Tendo's own host (a self-redirect
  would hand the code back to Tendo).
- `http` is accepted only for the exact hostnames `127.0.0.1`, `[::1]` and
  `localhost` (lowercase, no trailing dot), with any port or none (RFC 8252
  loopback). Loopback matching is port-insensitive.
- Private-use schemes are lowercase, hierarchical (`scheme:/path` or
  `scheme://host/...`), have no port, and are not on a denylist (`javascript`,
  `data`, `file`, `blob`, the browser and intent schemes, and the `chrome` and
  `ms-` prefixes).
- `client_name` is trimmed and NFC-normalized, then measured. Control (Cc),
  format (Cf), and line or paragraph separator (Zl, Zp: U+2028, U+2029)
  characters are refused, which blocks right-to-left overrides, zero-width
  characters, and line breaks that disguise a consent prompt. It is 1 to 100
  code points after normalization. An absent or empty name becomes `MCP client`.
- The metadata decoder ignores unknown keys, accepts one object of at most 16
  KiB, requires valid UTF-8, and refuses duplicate keys and wrong JSON types.
  Keys are matched exactly, so `REDIRECT_URIS` is an unknown key and a
  registration without `redirect_uris` is refused.
  `scope` is ignored and the response scope is `tendo:read tendo:write`. Any
  `token_endpoint_auth_method` is answered with `none`. `grant_types` must
  contain `authorization_code` and the response lists `authorization_code` and
  `refresh_token`. `response_types` must contain `code` and the response lists
  `code`.

### Deviations from the #456 review text

- **Dotless private-use schemes.** The review text asked for a dot in the
  scheme. Cursor registers `cursor://anysphere.cursor-mcp/oauth/callback`, which
  has no dot in the scheme, so the scheme regex is `^[a-z][a-z0-9+.-]{1,63}$`.
- **`http://localhost`.** Loopback http redirects are accepted as RFC 8252 §7.3
  requires for native clients (Claude Code uses `http://localhost:<port>`).
- **Scope and auth method substitution.** The server does not honor a requested
  scope or auth method. Every client is public and receives the fixed policy
  above, so a client cannot widen its scope or select a secret method.

### HTTP surface

The registration and metadata endpoints are `POST /oauth/register`, `GET
/.well-known/oauth-protected-resource/mcp` and `GET
/.well-known/oauth-authorization-server`, each with an `OPTIONS` preflight. The
paths are exact, so the root protected-resource document is not served.

- **Errors.** Every error uses the RFC 6749 §5.2 and RFC 7591 §3.2.2 shape,
  `{"error": ..., "error_description": ...}`, with fixed descriptions only.
  Malformed or refused metadata is 400 (`invalid_client_metadata`, or
  `invalid_redirect_uri` for a redirect). A non-JSON `Content-Type` is 415. A
  body over the 16 KiB limit is 413. Rate limiting is 429 with `Retry-After: 60`.
  A full cap is 503 (`temporarily_unavailable`) with no `Retry-After`. Storage
  faults are the same 503 and are recorded as a fixed fault class.
- **Order.** The 415 check runs before rate limiting, because it reads only the
  header and touches no state. Rate limiting runs before the body is read, so a
  flood cannot cost body I/O.
- **Rate limits.** Registration allows 10 per minute per client IP and 60 per
  minute globally. Each limit has its own instance and key space, shared with
  neither login nor the other limit.
- **Caching and cookies.** Every response is `Cache-Control: no-store`. The
  endpoints never read or set cookies, and they ignore a session cookie.
- **CORS.** Responses send `Access-Control-Allow-Origin: *` without
  `Access-Control-Allow-Credentials`, and expose `Retry-After`. Each path has an
  `OPTIONS` preflight answered with a 600 second max-age. The browser Origin
  check is exempt for these exact routes, so a client can call them from any
  origin; the canonical-authority check still runs.
- **Disabled.** While MCP is disabled, each path answers the same 404 as an
  unknown reserved path, the `WriteNotFound` problem body, so the disabled state
  is not visible. The routes stay mounted, so a request with another method
  answers 405. That is a known minor disclosure, accepted for A2 and covered by
  the contract test that checks each declared route.
- **Reserved prefixes.** `/oauth/`, `/mcp` and `/.well-known/` are reserved. An
  unknown path under them answers the API 404 and never the SPA.

### CrossOriginRoute design

`httpx.CrossOriginRoute` exempts one exact method and path from the browser
Origin check. The design has these rules:

- The match is exact on the path. A request whose `RawPath` is non-empty (any
  percent-encoded form, such as `%2F`) is never exempt, so it is checked like
  any other route.
- Build-time validation fails closed. An invalid entry, such as a non-absolute
  path, a wildcard, or an `OPTIONS` entry, makes the policy invalid, and every
  request then answers 421 rather than being served with a partial exemption
  list.
- The canonical-authority check always runs first. An exemption skips only the
  browser Origin check, never the Host check.

### Retention sweep cadence and timeout

The sweep runs once at startup and then every 10 minutes until shutdown. Each
run is bounded by the database timeout (`TENDO_DB_TIMEOUT`) as a whole, not per
step. Each batch commits on its own, so progress survives a timeout: a run that
ends early leaves the completed batches in place, and the next run continues.
The sweep logs counts and fixed fault classes (`timeout`, `canceled`,
`step_failed`, `unknown`), never driver text. On shutdown, the sweeper is
stopped and awaited after the HTTP server drains and before the pool closes.

### Declared deviations (A2)

- **Whole-run sweep timeout.** The sweep timeout bounds the whole run, not each
  step. Per-step bounds belong in `MCPService.Sweep` and are not in A2.
- **Smoke-scope exclusion.** `api/check-health.test.mjs` excludes the three
  MCP operationIds from the smoke-scope partition, because no live smoke can
  enable MCP. **A3/A4 requirement:** remove this exclusion once MCP can be
  enabled, so the smoke covers the MCP operations.
- **OPTIONS preflight allowlist.** The contract forbids `options:` operations,
  so the three preflight routes are named in an allowlist in the contract
  security test. Their disabled 404 is pinned by
  `TestProductionCompositionAnswersMCPDisabled404`.
- **415 before rate limiting.** The media-type check runs first. It reads only
  the header and changes no state, so the limiter does not count a request that
  is refused for its media type.
- **Disabled mount keeps the routes.** A disabled mount keeps the routes mounted,
  so 405 for other methods is accepted, as described under Disabled above.

### Cap and residual risk

Registration is unauthenticated. The cap counts only clients that hold no grant
(1000). Registration is refused, not queued, once the cap is reached. The cap is
soft under concurrency: two registrations at the boundary may both succeed.
Grantless clients expire 24 hours after creation. A determined client can still
fill the cap every day; the residual denial-of-service risk is accepted here and
is bounded by the request limiters described under HTTP surface (10 per minute per
client IP and 60 per minute globally). Those limits slow a flood but do not
prevent a determined client from filling the cap each day.

### Retention sweep

The sweep runs regardless of whether MCP is enabled, so a disabled installation
still clears expired secrets. Production composition hard-disables MCP
(`Enabled: false`) until the flow is complete. No `TENDO_MCP_ENABLED` setting
exists. Each step runs in its own transaction with `lock_timeout = 100ms`, and
deletes at most 1000 rows per statement by primary key. A step that cannot get
a lock yields for the run and the next run retries it. Other storage failures
are returned as fixed error classes, with no driver text.

`lock_timeout` means the sweep itself never waits for a lock. Other
transactions may still wait briefly on a sweep batch that holds a lock, so the
sweep bounds its own waiting but does not promise that issuance or revocation
is never delayed.

- Expiry is not revocation. The sweep deletes expired access tokens, unconsumed
  expired refresh tokens, and the codes and token rows of grants past their
  absolute expiry. It keeps each grant row and writes no `mcp_grant_events`.
- Consumed refresh digests are kept until the grant ends, so replay of a
  rotated token is still detected while the grant is live.
- Unused expired authorization codes are deleted. A used code of a live grant
  is kept, because it is the replay evidence for that grant.
- A client is deleted only when it is older than 24 hours and holds no grant and
  no authorization code. The delete uses `FOR UPDATE SKIP LOCKED`: an issuance
  that found the client holds `FOR KEY SHARE` until commit, so the sweep skips
  it rather than orphaning a code being inserted. `lock_timeout` and
  `SKIP LOCKED` mean the sweep does not wait on a lock; other transactions may
  briefly wait on a sweep batch.
- The client NOT EXISTS set is pinned by a test that parses the
  `SweepMCPGrantlessClients` SQL and compares its tables with every foreign key
  into `mcp_clients` in `pg_constraint`.

### Requirements for slice A3

- An error redirect to a registered URI is sent only after user interaction on
  the consent page. Without it an attacker can use Tendo as an open redirector
  (RFC 9700 §4.11.2).
- The consent SPA route is not under `/oauth/`, `/mcp` or `/.well-known/`.
- Loopback redirects match exactly, ignoring the port (RFC 8252 §8.3).
- A client that is unauthorized for 24 hours must register again, because its
  row may have been swept.
- The RFC snake_case wire names of the OAuth endpoints are an exception to the
  camelCase rule of API.md.

## Slice A3a: authorization, consent decision, token and revocation

Slice A3a adds the backend half of the consent flow: migration 030, the pending
consent store, `GET /oauth/authorize`, the session-authenticated consent API
`GET /api/v1/mcp/authorization-requests/{requestHandle}` and
`POST .../{requestHandle}/decision`, `POST /oauth/token` and
`POST /oauth/revoke`. The consent screen (`/connect`) is slice A3b, and `/mcp`
is A4. Production composition stays hard-disabled, so every new route answers
the unknown-path 404 before authentication or body reads. The decisions below
follow the independent review recorded on #456.

### Pending consent

- `/oauth/authorize` reads no cookie and never redirects to the client. Every
  outcome is `303` to `/connect#request=<handle>` or `/connect#error=<code>`,
  including a registered redirect URI with a bad `code_challenge`. An error
  goes to the client's redirect URI only after the human decides (the
  decision's `redirectTo`), which keeps Tendo from being an open redirector
  (RFC 9700 §4.11.2).
- A valid request is stored in `mcp_authorization_requests` for 5 minutes (the
  CHECK allows at most 10). Only the SHA-256 digests of the 43-character handle
  and of a browser binding are stored. The handle goes to the SPA in the URL
  fragment. The binding goes to the browser in a cookie
  (`__Secure-tendo_mcp_consent` on HTTPS, `tendo_mcp_consent` otherwise) that
  is HttpOnly, SameSite=Lax, Secure on HTTPS, `Max-Age=300`, and scoped to
  `/api/v1/mcp/authorization-requests/<handle>`, so parallel consents do not
  collide. Reading or deciding a request needs both the handle and the binding,
  compared by digest equality in SQL. A mismatch answers the same 404 as an
  unknown, expired or consumed request. A leaked handle alone cannot be
  approved from another browser. The cookie uses `__Secure-`, not `__Host-`,
  because `__Host-` requires `Path=/`; a sibling subdomain could therefore
  plant a binding cookie. That gains an attacker nothing over sending the
  victim an ordinary authorize link, so the residual risk is accepted.
- Authorization runs in one transaction: client `FOR KEY SHARE`, then the
  redirect match, then the INSERT. A foreign-key failure (the sweep won) is
  `invalid_client`.
- The decision locks the account and checks the session (ADR lock steps 1-3),
  reads the client `FOR KEY SHARE`, then consumes the request with
  `DELETE ... RETURNING`. Approval and denial both consume it, so of two
  concurrent decisions exactly one wins and at most one code exists. A
  refusal rolls back and consumes nothing. The runtime role has `SELECT`,
  `INSERT`, `DELETE` on the table and no `UPDATE`.
- Expired requests answer 404 and are left to the sweep step
  `authorization_requests_expired`, which runs before `grantless_clients`. The
  client sweep keeps a client that has a pending request.

### Redirect matching

One matcher, `MCPRedirectURIMatches`, serves authorization, the decision and
code issuance. A registered URI matches by byte equality. Otherwise only an
`http` loopback URI matches, with a byte-equal host (`localhost` never equals
`127.0.0.1`), path and query, and any port on either side (RFC 8252 §7.3,
§8.3). The token endpoint still requires the presented `redirect_uri` to equal
the one the code was issued for.

### Parameters

- Unknown parameters are ignored. A repeated recognized parameter is
  `invalid_request`; a repeated `resource` is `invalid_target`.
- An absent `resource` means the configured resource. A present one must equal
  it exactly, else `invalid_target`. This amends the "required" wording of the
  Decision section.
- `code_challenge_method` must be present and `S256`.
- An empty `scope` counts as absent (both Tendo scopes). Unknown scopes are
  dropped and the known ones granted; an empty result is `invalid_scope`. On
  refresh a present scope must equal the grant's scopes as a set; narrowing is
  not supported and is `invalid_scope`, with nothing consumed.
- `state` is optional, at most 1024 printable ASCII bytes.
- The token and revocation endpoints read parameters from the body only, take
  `application/x-www-form-urlencoded` with optional parameters such as
  `charset`, bound the body to 8 KiB, and never read or set a cookie. They use
  the A2 CORS policy (any Origin, no credentials) and `CrossOriginRoute`
  exemptions.

### Revocation

`POST /oauth/revoke` (RFC 7009) revokes the whole grant of an access token or
refresh token, consumed or not, when `client_id` names the grant's client. It
writes reason `client` with no actor and exactly one event; a consumed refresh
token presented here is a revocation, not a `replay`. A malformed, unknown,
foreign-client or already revoked token answers 200 and changes nothing.

Migration 030 widens `mcp_grants_revoke_reason_check` and
`mcp_grant_events_reason_check` under the same names. Its Down maps `client` to
`user` in both tables before it restores the 028 sets; the mapping is lossy and
accepted, because a downgraded release has no `client` cause.

### Limits

- Authorize: 20 per minute per client IP and 120 per minute globally; a refusal
  lands on `/connect#error=temporarily_unavailable`.
- Decision: 30 per minute per user.
- Token: 60 per minute per client IP and `client_id`, and 600 per minute
  globally.
- Revoke: 30 per minute per client IP.

### Declared deviations (A3a)

- An unknown client at the token endpoint is `invalid_grant` (400), not
  `invalid_client` (401, RFC 6749 §5.2), so the endpoint is no client-existence
  oracle.
- `OAuthError.error` is widened to the RFC 6749, RFC 7591 and RFC 8707 codes
  that the token and revocation endpoints emit.
- The consent API under `/api/v1/mcp/` uses camelCase and problem+json; only
  `/oauth/*` uses the RFC snake_case wire names.
- Request logs carry the route template only, never a handle, binding, code,
  token or query string.

### Requirements for slice A3b

- The login return path is restricted to `/connect` plus a handle matching the
  handle pattern.
- The consent screen shows "signed in as" and the unverified client name.
- A test keeps `frame-ancestors 'none'` on `/connect`.

## Slice A3b: consent screen

Slice A3b adds the web page that `/oauth/authorize` sends the browser to. It
changes no backend behaviour; production MCP stays hard-disabled until A4, so
the page can show only its error and "no longer valid" states there. The
decisions below follow the independent design review recorded on #456.

- `/connect` is an ordinary SPA document with the same CSP
  (`frame-ancestors 'none'`), `X-Frame-Options: DENY`,
  `Referrer-Policy: same-origin` and `Cache-Control: no-cache` as every page.
  Tests pin this in the web UI handler and the production composition.
- The fragment is read once and removed from the address bar. It holds exactly
  one key: `request` (43 base64url characters) or `error` (a fixed set of codes).
  Anything else is an invalid request. Fragment text is never shown.
- The handle is kept in memory and in `sessionStorage` (`tendo.mcpConnect`) for
  at most 5 minutes, so a reload or an identity-provider round trip keeps it.
  The handle is useless without the HttpOnly binding cookie, so this adds no
  exposure. The stored entry is revalidated on every read.
- Return after sign-in: only the sign-in action started from the `/connect`
  sign-in screen sets the return flag. A successful sign-in consumes it once
  and opens `/connect`. On boot it is consumed only at `/`, `/login` and
  `/connect`. The literal `/connect` with a stored, pattern-valid handle is the
  only possible target. A sign-in started anywhere else (Home, an invitation,
  onboarding) and a plain sign-out drop the stored entry. A signed-out visit
  to `/connect` without a usable stored request shows the problem state, not
  a sign-in. If the stored entry expires while the sign-in form is open, the
  sign-in ends on Home without a message; the app has to start again anyway.
- The page shows the client name marked as unverified and the redirect target:
  the punycode host name for https, "an app on this computer" for loopback, and
  "an app on this device" plus the scheme for a private-use scheme (RFC 8252
  §8.6). It also shows "signed in as" with a sign-out that returns to the
  `/connect` sign-in screen.
- Allow is never focused automatically and is enabled only after the page has
  been visible and focused for one second. Losing visibility or focus restarts
  the delay, against click-jacking through pop-ups and double clicks.
- A decision is sent at most once. The browser follows `redirectTo` only when
  it extends the registered redirect URI from the request (loopback port
  ignored) and is not a script-capable scheme. Then the page shows a final
  "returning to the app" state that never offers the buttons again.

Deferred to A4, where a real pending request exists without mocking the
backend: the visual baselines of a live consent, the consent screen in E2E
journey 17, and the user documentation.
