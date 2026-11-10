# ADR 0012: OIDC first sign-in and verified-email consolidation

Status: accepted (amends [ADR 0008](0008-oidc-account-linking.md))

## Context

ADR 0008 allowed OIDC sign-in only for identities already connected from the Account screen; an unknown identity was refused with `identity_not_linked`. V4 makes "Sign in with …" work on first use without letting a provider claim take over an existing account. The approved design, decision table, threat model and rejected alternatives are in section 5 of the [V4 baseline audit](../design/v4-baseline-audit.md#5-oidc-identity-and-email-v4-10-v4-11); this ADR records the decision only.

## Decision

- Request scopes `openid email`. An email from the ID token is usable only when `email_verified` is the JSON boolean `true` and the address passes one canonical function (`identity.CanonicalEmail`): trimmed, ASCII only, exactly one `@`, local part kept case-sensitive with no dot or `+tag` stripping, domain lowercased with LDH labels, at most 254 bytes. Anything else is treated as "no email". No userinfo call.
- Accounts gain an optional `email` with `email_verified_at`, `email_source` (`oidc_claim` or `account_confirmed`) and `email_issuer`, all set or all unset, unique when set (migration `019`). An email is trusted for automatic connection only under the issuer that proved it, so changing the configured provider never transfers trust by email. Existing accounts migrate with no email; email is never inferred, typed by an administrator, or overwritten automatically.
- The `(issuer, subject)` pair still owns sign-in. Each callback runs the audit's decision table in one transaction under a per-subject advisory lock, re-reading all state and retrying a unique violation once from a fresh read:
  - linked identity: sign in (and record the verified email only if the account has none and nobody else holds it);
  - unlinked, and exactly one account holds the verified email through `oidc_claim` or `account_confirmed`, proved by this issuer, and has no identity for this issuer: connect and sign in;
  - that account already has another subject for this issuer: generic `identity_conflict`, nothing written;
  - otherwise (unknown, unverified or missing email): nothing is attached; a server-side pending continuation is created.
- Every stored email is written in the same transaction as an identity for the issuer that proved it, so with one configured issuer the automatic connection applies only to an account that holds the address but has no identity for that issuer (no self-service unlink exists in this release, so this is rare); a new subject presenting an owned address otherwise meets the conflict row. Accounts without a stored email, including every account from before V4, consolidate through the password confirmation below, which then stores the email. Automatic connection across issuers is deliberately not offered.
- Pending continuation: 32 random bytes in an `HttpOnly`, `SameSite=Lax` cookie (`__Host-` prefixed and `Secure` under HTTPS), stored only as SHA-256 in `oidc_pending_logins`, bound to the issuer and client ID, valid for 10 minutes, consumed atomically with the write it authorizes. Continuation routes require same origin and reuse the sign-in throttle (status reads have their own bucket); all except cancel refuse an active session. A conflict consumes the continuation. From it the person either:
  - creates a new account: generated login `member-xxxxxxxx`, no password, no household, no role; or
  - confirms an existing account with its Tendo login and password, verified like ordinary sign-in; the credential row is locked and rechecked inside the binding transaction so a concurrent password change binds nothing. Five failed attempts delete the continuation. The verified email is then recorded as `account_confirmed`.
- Invitation carry-over: OIDC start may carry a pending invitation token; only its hash is stored on the flow and copied to the continuation. Membership is granted only by accepting that invitation in the same transaction as sign-in or account creation, with ordinary `member` semantics. An invitation that is no longer valid leaves sign-in completed and is reported generically.
- Recovery: an account created by mistake may discard itself while it has no household membership, no password and exactly one identity; this deletes it and returns to the continuation. After joining a household there is no self-service merge.
- OIDC never grants `admin` and never merges memberships or items. Local password sign-in is a separate path and keeps working while the provider is unreachable.

## Consequences

- `identity_not_linked` is no longer emitted; it stays in the documented callback pattern as a legacy value.
- An account created through OIDC has no password; its recovery depends on the provider or on an administrator.
- A provider that changes the case of a local part, or reissues an address to a new subject, cannot take over an account: such sign-ins reach the continuation or a conflict.
- The Account-screen connection flow from ADR 0008 still works in this release and is removed separately.
