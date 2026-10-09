# ADR 0008: OIDC account linking and login

Status: accepted

## Context

Tendo needs an optional external sign-in method without weakening local account admission or granting household membership through provider claims. Existing local credentials remain the recovery path.

## Decision

- Configure one OIDC provider per deployment. Use Authorization Code flow with PKCE, and request only the `openid` scope.
- An account connects an identity explicitly from an active local session after reauthenticating with its current local password. The initiating session must still be active at callback time.
- Identity ownership is the exact validated `(issuer, subject)` pair. Never link or provision accounts by email, display name, preferred username, or other profile claims.
- OIDC login succeeds only for an already-linked identity. Unknown identities create no account, session, or membership; OIDC never adds household membership.
- Persist only single-use, expiring flow state in PostgreSQL. Consume it atomically before token exchange; bind it to the browser cookie, state, provider, and client. Do not persist provider tokens or profile claims.
- Issue a fresh ordinary Tendo session on success. Linking atomically revokes the initiating session as the identity is linked.

## Consequences

- A provider outage disables OIDC start/completion but does not affect local sign-in.
- The API and provider configuration can ship before browser controls; UI must not be claimed until implemented.
- Operators register the callback derived from canonical `TENDO_PUBLIC_URL`. Provider discovery is lazy and does not gate startup or local login.
- The local password remains necessary for account recovery and explicit linking.
