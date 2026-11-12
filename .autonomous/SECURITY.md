# Security and identity contract

status=canonical_version_independent
source_of_truth=this_file
sources_consolidated=V1 PRODUCT/DEFINITION_OF_DONE identity and security items; V2.1 identity scenario families; V4_SCOPE and V4_DEFINITION_OF_DONE identity, OIDC, password-reset and admin items; V5_SECURITY_AND_MCP; V5_SCOPE account and MCP items
authority=AGENTS "Authority" and GUARDRAILS remain binding for process; this file is the technical contract

## Identity model
- UserAccount is a GLOBAL identity. It may have a local login/password, an OIDC identity, or both.
- Household membership is many-to-many: user <-> household, one household-scoped role per membership. Roles are `admin` and `member`. The former V1 `owner` role is retired in favour of household admin (see "Migration").
- Instance administrator is an independent, explicitly granted, installation-wide capability. It is NOT implied by household admin and is never granted by invitation, OIDC claims or household role edits.
- Household admin has no implicit cross-household authority.
- A tracked person subject needs no login.
- Authenticated human accounts only; no shared accounts, no shared privileged key.

## Authorization invariants
- Every request resolves an explicit Principal (identity + memberships/roles + instance capabilities). Local session, OIDC session and MCP OAuth are adapters that create it (ARCHITECTURE "Durable architecture rules").
- The current household MUST be explicitly selected and validated on the server, regardless of frontend state.
- Route IDs, SQL filters, search, subject references, completion history and counts MUST enforce the household boundary.
- Stale sessions, stale roles and membership removal are tested and take effect on the next request.
- 404/403 policy is consistent and must not leak inaccessible resource details (API.md "Security").
- Protection is enforced in the backend. UI disabled buttons do not count as protection.
- Last-admin protection (atomic under concurrency):
  - the last active instance administrator cannot be demoted, removed or deleted;
  - the last active admin of each household cannot be demoted, removed or deleted;
  - self-removal, cross-household delete, invitation acceptance and revoked members are covered by tests.

## Local authentication
- Passwords use a modern password hash. Never reversible or plaintext.
- Browser auth uses secure HttpOnly cookies. No long-lived bearer token in localStorage.
- Local password sign-in keeps working when OIDC is unavailable.
- Local password change (V5):
  - requires a LOCAL authenticated session, the current password, CSRF/origin protection and rate limiting;
  - a session established via OIDC cannot use this flow, even if the account also has a local password;
  - on success, other sessions and tokens are invalidated as appropriate.
- Admin CLI password reset (V5):
  - target command shape `tendo users set-password --login ...`, interactive; the implemented command and its usage are documented in admin docs when shipped;
  - no password in argv, `ps`, shell history or logs; no cleartext in logs;
  - admin access is restricted through container/SSH access;
  - resets and revokes sessions and tokens.
- Admin-initiated password reset for local-password accounts (V4):
  - only instance/household admins acting within the same household; only accounts with a local password;
  - single-use, time-limited reset token stored hashed, shown ONCE; no email service;
  - throttled; audited; CSRF/authorization checked;
  - never reveals a password or lets the admin discover another user's password;
  - invalidates affected sessions/tokens after reset;
  - expired, reused, revoked and failure paths are tested.

## OIDC
- Optional. Works with PocketID and generic standards-compliant providers. Tested against a deterministic local test provider at the protocol boundary.
- Identity binding: after first login, identify by immutable (issuer, subject). Existing linked identities are authoritative by issuer+subject even if email changes.
- OIDC login alone never grants household membership. Membership requires an explicit invitation.
- OIDC never grants instance-admin or household-admin rights by default or from claims.
- Just-in-time (JIT) account creation: a first successful OIDC login creates an account idempotently, bound to (issuer, subject). The user then sees a working signed-in state and, if not yet a member, a "join household by invitation" screen.
- Email matching (V4 provenance model):
  - Email is an optional, provenance-tracked attribute with a verification flag.
  - Obtain `email` and `email_verified` claims, or validated userinfo with the identical `sub`.
  - When BOTH sides have established email ownership, the match is unique and no conflicting identity binding exists, verified matching email MUST automatically attach OIDC to the existing account instead of creating a duplicate. Automatic attachment is permitted ONLY under these conditions; JIT creation must not bypass this resolution or unsafe-case recovery.
  - Never infer ownership from display name or unverified strings.
  - Duplicate, conflicting, unverified or absent email means NO automatic attach and NO membership merge; present an understandable, actionable recovery route.
  - Legacy v3 accounts have no email column and no recorded email: the safe recovery path is a one-time verification or admin-mediated resolution, never blanket "all old accounts are mergeable by email".
  - Concurrent callbacks and email changes are handled safely.
  - Account takeover via unverified or mutable email, issuer mismatch, or race conditions must be blocked.
- A provider outage never breaks local password login.
- Legacy manual SSO account-linking/pairing UI and flow is removed from the frontend. Existing linked identities keep signing in. Legacy API compatibility is kept or removed only after a proven-safe audit.
- Callback/state/nonce/PKCE handling and public-origin/callback URLs follow ARCHITECTURE "Reverse proxy / networking".

## Sessions
- Local session and OIDC session are distinct session kinds; the kind is recorded and drives which flows are allowed (local password change is local-only).
- Logout, password reset, admin reset and account deletion revoke affected sessions.
- Session cookies follow the canonical public URL policy for `Secure`.

## Household operations and account lifecycle
- Instance admin creates households, assigns and removes members, changes scoped roles and can delete accounts.
- Household admin manages only their own household within granted permissions.
- User account deletion (V5):
  - an explicit, confirmed, destructive action;
  - revokes sessions, OIDC links and MCP credentials;
  - removes or anonymizes personal fields while retaining required non-identifying household completion history and references;
  - covers active items, ownership transfers, person links and multi-household members;
  - transaction and retention decisions are documented and tested.
- Stable historical task references are preserved.

## Invitations (V5 cleanup)
- Cancelled or revoked invitations must not persist as secret-bearing rows. Remove the active token and optionally record a minimal redacted audit event.
- Expired invitations are purged by bounded-retention cleanup that handles the concurrent accept/revoke race.
- Migration purges existing cancelled/revoked rows with secrets while preserving history separately from invite tokens.
- Expired invitations are hidden from all frontend lists, pages and counts, even when backend cursor pagination includes them. Audit history stays intact.
- Invitation acceptance is race-tested against revocation.

## MCP (per-user OAuth, V5)
- MCP runs over HTTPS (deployment terminates TLS at the reverse proxy) using Streamable HTTP. Use standard libraries; no speculative identity framework.
- Protocol: validate the MCP authorization revision appropriate to supported clients. Advertise Protected Resource Metadata and OAuth Authorization Server Metadata as required. Issue OAuth codes with PKCE. Use correct redirect and client-registration strategy. Access tokens are short-lived and audience-bound. Support refresh, rotation, revocation and replay protection where provided.
- OIDC login authenticates the human at the authorization step only. Never accept an upstream OIDC ID token as a Tendo MCP bearer token.
- Each MCP request resolves its own user identity and authorized households. No shared singleton user state, no globally privileged API key, no ambient cookie elevation, no token forwarding to third parties, no anonymous write endpoint, no silent PAT fallback.
- Disabled-MCP state is enforced at the endpoint, not only in admin UI.
- Effective permission:
  `effective = global_MCP_enabled AND user_MCP_enabled AND (admin_max_scopes ∩ user_selected_scopes ∩ OAuth_token_scopes ∩ active_household_permission)`
  - user override only reduces;
  - re-evaluated server-side on every request;
  - revocation takes effect on the next request;
  - both UI and backend expose and verify per-user grants; mutations are audited.
- Tool surface: small and useful (households, items list/search/detail/create/update, complete/undo, schedule/repeat, history, people and things as allowed). Excluded: instance-admin tools, user deletion, permission self-elevation, arbitrary database access.
- Every MCP tool has a guarded, testable input/output schema and explicit error results, calls the existing application services and honours idempotency, audit and household isolation.
- Required negative tests: rate limits, token audience/issuer, cross-household exfiltration, unauthorized tool calls, CSRF on browser flows, disabled-MCP endpoint refusal.
- OAuth login is validated locally with a deterministic test IdP plus protocol conformance checks.

## Logging, audit and secrets
- No secrets, passwords, OAuth tokens, session tokens, invitation tokens, password hashes or OIDC secrets in logs, audit records or API responses.
- Audit records for security-sensitive mutations (role changes, deletes, MCP grants, resets) hold minimal facts, not unnecessary household content.
- No telemetry or external tracking by default.

## Deployment security
- Two-service Compose only (see ARCHITECTURE "Deployment and startup").
- Runtime DB role is restricted; never a superuser. See ARCHITECTURE "App startup privilege tradeoff".
- Host bind defaults to localhost (ARCHITECTURE "Secure host IP").
- Trusted-proxy rules: forged `X-Forwarded-*` from untrusted clients are ignored; trusted proxy CIDRs/hops only (ARCHITECTURE "Reverse proxy / networking").
- Config validation fails clearly on unsafe production settings.
- Container runs non-root with explicit writable paths.

## Migration from v4 (V5)
- Preserve stable v4 user IDs, household IDs, OIDC issuer/sub bindings, login credentials, tasks, persons/subjects and ledger entries.
- Initial instance administrator: identify the existing v4 owner/bootstrap authority using documented evidence. Do NOT grant instance-wide rights to every household admin by assumption.
- If an unambiguous mapping is not possible, FAIL CLOSED and provide a safe explicit CLI setup/recovery path.
- The migration is reviewed; no silent data overrides.

## Required review
- A security design review is required before any schema or OAuth change. It documents trust boundaries, refusal cases and the no-surprise migration from released v4.0.0.

## Links
- Household scoping in storage and API: ARCHITECTURE "Identity and data scoping", API.md "Resource hierarchy".
- Session and CSRF HTTP rules: API.md "Security".
- Negative test requirements: TESTING.md "Security and identity tests".
- Acceptance: DEFINITION_OF_DONE.md "Identity, household and security".
