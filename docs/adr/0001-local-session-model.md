# ADR 0001: Local browser session model

Status: accepted

## Context

Tendo needs local login that works without an external identity provider. Browser auth must use HttpOnly cookies, never long-lived bearer tokens in browser storage. Tendo serves plain HTTP behind a TLS-terminating reverse proxy, and `TENDO_PUBLIC_URL` is the canonical origin. Future OIDC login and API/service tokens must create the same transport-neutral principal instead of adding a second authorization path.

## Decision

- Sessions are opaque server-side records in PostgreSQL (`user_sessions`). We do not use signed or stateless tokens, so logout and revocation take effect immediately and need no signing-key management.
- The cookie value is 32 random bytes (base64url, 43 characters). The database stores only its SHA-256 digest, so a database read does not reveal usable cookies.
- Sessions have an absolute lifetime of 30 days with no sliding renewal. The runtime role may select, insert, and delete sessions, but may not update them.
- Cookie attributes: `HttpOnly`, `Path=/`, `SameSite=Lax`, no `Domain`. With an HTTPS public URL the name is `__Host-tendo_session` and the cookie is `Secure`. With a plain-HTTP development public URL it is `tendo_session` without `Secure`. The cookie policy follows `TENDO_PUBLIC_URL`, not the backend socket scheme.
- CSRF defense relies on the existing canonical-origin middleware. Every non-safe method must carry an `Origin` header that exactly matches the public origin. `SameSite=Lax` adds a second layer of protection. No separate CSRF token is issued.
- Login failures (unknown login, wrong password, malformed input) all return the same `401 invalid_credentials`. Unknown logins still run an Argon2id verification against a dummy hash to reduce timing differences. Per-client-IP and global concurrency limits run in-process; Redis is not used.
- The identity module produces a transport-neutral `Principal` (user ID, login, default household ID). The default household ID is a navigation hint only, never authorization. For every household resource request, the household module checks current membership, role, and capabilities at that resource boundary. HTTP adapters and future OIDC/service-token adapters create the principal; they do not grant household access.

## Consequences

- Every authenticated request needs one indexed PostgreSQL lookup. This is acceptable at household scale.
- Expired rows are pruned for a user when that user logs in again. Global cleanup can be added later without a contract change.
- Plain-HTTP deployments are for trusted local development only. Internet-facing deployments must use an HTTPS public URL so the `Secure` and `__Host-` protections apply.
