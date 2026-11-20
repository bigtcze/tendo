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
  resource is `<TENDO_PUBLIC_URL>/mcp`. The RFC 8707 `resource` parameter is
  required and must equal the configured resource on authorization, code
  exchange and refresh. Every code, grant and token is bound to it.
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
  `cli_reset`, `account_deletion`, `replay`, `code_reuse`.
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
- **Disabled MCP** (`TENDO_MCP_ENABLED`, default off) is refused inside the
  service for issuance, exchange, refresh and verification, not only by
  routing.
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
- The HTTP surface, consent, client registration and listing or revoking
  grants from the UI are added by later slices on this store.
- The retention sweep (A2) removes expired secrets and never revokes. A grant
  past its absolute expiry is unusable; the sweep deletes its codes and token
  rows, keeps the grant row, and writes no event. Expiry is not revocation.
- Grantless, codeless clients older than 24 hours are deleted.

## Slice A2: dynamic client registration and retention

Slice A2 adds RFC 7591 registration, the grantless-client cap and the retention
sweep. The HTTP adapter, OpenAPI, CORS, reserved paths and the request limiters
are later slices, as is the sweep's scheduling goroutine in `cmd/tendo`.

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
- `client_name` is trimmed. Control (Cc) and format (Cf) characters are
  refused, which blocks right-to-left overrides and zero-width characters that
  disguise a consent prompt. It is 1 to 100 code points. An absent or empty name
  becomes `MCP client`.
- The metadata decoder ignores unknown keys, accepts one object of at most 16
  KiB, requires valid UTF-8, and refuses duplicate keys and wrong JSON types.
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
- **client_name is not NFC-normalized yet.** The decisions require NFC. The
  only implementation is `golang.org/x/text/unicode/norm`, and it is currently
  `// indirect` in `backend/go.mod`. Importing it makes `go mod tidy` edit the
  go.mod require block, which the slice brief does not allow without an
  owner-approved report. The rest of the name policy (trim, Cc and Cf refusal,
  1 to 100 code points) is enforced. NFC is a follow-up once go.mod may change.
- **Scope and auth method substitution.** The server does not honor a requested
  scope or auth method. Every client is public and receives the fixed policy
  above, so a client cannot widen its scope or select a secret method.

### Cap and residual risk

Registration is unauthenticated. The cap counts only clients that hold no grant
(1000). Registration is refused, not queued, once the cap is reached. The cap is
soft under concurrency: two registrations at the boundary may both succeed.
Grantless clients expire 24 hours after creation. A determined client can still
fill the cap every day; the residual denial-of-service risk is accepted here and
is bounded by the request limiters that a later slice adds.

### Retention sweep

The sweep runs regardless of `TENDO_MCP_ENABLED`, so a disabled installation
still clears expired secrets. Each step runs in its own transaction with
`lock_timeout = 100ms`, and deletes at most 1000 rows per statement by primary
key. A step that cannot get a lock yields for the run and the next run retries
it. Other storage failures are returned as fixed error classes, with no driver
text.

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
  `SKIP LOCKED` keep the sweep from ever stalling a revocation or an issuance.
- The client NOT EXISTS set is pinned by a test that reads `pg_constraint` for
  every foreign key into `mcp_clients`.

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
