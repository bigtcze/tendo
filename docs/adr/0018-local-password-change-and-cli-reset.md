# ADR 0018: Secure local password change and operator CLI reset

Status: accepted

Amends [ADR 0014](0014-admin-password-reset.md) (both now share one credential
replacement write). Builds on [ADR 0001](0001-local-session-model.md),
[ADR 0012](0012-oidc-jit-and-verified-email.md) and
[ADR 0017](0017-password-reset-account-authority.md).

## Context

Before 5.0 a person could not replace their own local password, and an operator
had no way to restore access from the host. Recovery went through an
administrator-issued reset link, which needs a second administrator: the only
administrator of a household could not recover a forgotten password at all, and
an account whose administrators no longer reach the person stayed locked out.
At the same time nothing recorded how a session had been established, so any
authenticated session could have been treated as proof that the browser knew
the local password.

Two new paths are therefore needed: a self-service change in **Your account**,
and an interactive operator command inside the application container. Both
replace the same account-wide credential that the reset link replaces, and all
three must leave the account in exactly the same state. If each path grew its
own "replace the password and sign the account out" code, the invariant would
drift and one path would eventually keep a session or a reset link alive.

## Decision

- **Session kind is recorded and authoritative.** `user_sessions.sign_in_method`
  is `password`, `oidc` or `legacy`, enforced by a `CHECK`; the migration
  backfills pre-existing rows as `legacy`, drops the backfill default, and never
  writes `legacy` again. The runtime role keeps its table-level `SELECT`,
  `INSERT` and `DELETE` grants, so the column needs no new grant. Every insert
  names the kind; a zero value is refused in the adapter
  (`ErrInvalidSignInMethod`) before it reaches the database, and a statement
  that forgets the column fails on `NOT NULL` instead of silently mislabelling a
  session. `legacy` is a fail-closed answer: an unclassifiable session may not
  change a password, because it might have come from OIDC. `Session` and
  `SessionInfo` expose `signInMethod`, so the browser is told what its session
  is and the login response is derived from the same stored value.
- **The self-service change is local-only.** `POST /api/v1/auth/password`
  `{currentPassword, newPassword}` requires the session cookie like every other
  authenticated route, and then requires `signInMethod == password`. An OIDC
  session is refused even when the account also has a local password: the OIDC
  identity is a different credential, and the browser never proved knowledge of
  the local password to Tendo. The refusal is `403 local_session_required`, and
  it is decided before the body is parsed so a malformed body cannot be turned
  into a decoding answer. The same refusal covers an account that has lost its
  local credential.
- **Refusal order inside the change.** Session kind and a well-formed token
  first, then the new-password policy, then the current password, then the
  transaction. The new-password policy is checked before the current password so
  a stronger password is never rejected with a misleading "current password is
  wrong". A wrong current password is `422 currentPassword: incorrect`, not
  `401`: the caller stays signed in and only the form shows the error. Both the
  wrong password and a corrupt stored hash are the same answer to the client,
  but the corrupt hash stays recognizable internally so the existing
  `password_hash_integrity_fault` operational record still fires. The new
  password follows the same 15–128 character policy as setup, invitation
  acceptance and reset redemption.
- **One write path for every replacement.** The identity domain defines one
  persistence protocol, `CredentialReplacementTransaction`
  (`RevokeOutstandingPasswordResets` → `LockLocalCredential` →
  `ReplaceLocalPassword`), implemented once in the PostgreSQL adapter. The
  self-service change extends it with the caller's own session operations
  (`ActiveSessionExists`, `CreateSession`), the operator CLI embeds it as-is, and
  reset redemption reuses `LockLocalCredential`/`ReplaceLocalPassword` while
  keeping the link it is redeeming usable. A successful replacement increments
  `local_credentials.credential_version`, deletes every session of the account,
  and reports which of the deleted sessions were still unexpired.
- **Reset links do not survive a replacement.** Every change and every operator
  reset revokes the account's outstanding admin-issued reset links in the same
  transaction, so a link handed out before the change cannot replace the new
  password afterwards. Redemption deliberately does not revoke links: it locks
  the token row it is consuming, then the credential row, then completes the
  token and replaces the password.
- **Lock order is `password_reset_tokens` before `local_credentials`, always.**
  Redemption takes the token row `FOR UPDATE` before the credential row, so the
  self-service change revokes reset links before locking the credential. The
  opposite order would let a change and a redemption deadlock against each
  other. This is stated in the protocol's own documentation because it is not
  visible from either call site alone.
- **The caller keeps exactly one fresh session.** The replacement deletes the
  caller's own session like every other one, so the transaction proves the
  caller's session was among the deleted ones (`ErrUnauthenticated` otherwise),
  inserts exactly one new `password` session, and the handler sets the cookie
  from that session. The rotated token appears only in `Set-Cookie`, never in
  the response body. The response is `200 Session` with the rotated expiry and
  `Cache-Control: no-store`. Deleting everything and re-inserting one is what
  makes "other sessions and tokens are invalidated" true without an `UPDATE`
  grant on `user_sessions`.
- **Concurrent change and sign-in.** The stored hash is read without a lock,
  because Argon2 is slow and must not run under a row lock; the re-read inside
  the transaction takes `FOR UPDATE` and must still equal what was verified. A
  change that loses that race answers `422 currentPassword: incorrect` while
  its own session still exists, and `401` once its session is gone. Sign-in
  keeps its existing `FOR SHARE` revalidation (ADR 0014), so a sign-in racing a
  change either commits first and has its session deleted or fails as a wrong
  password.
- **Throttles.** Per client address 10 per minute, spent before authentication
  so an unauthenticated flood cannot amplify into session lookups; per account
  5 per minute and 20 per hour after authentication. All three answer
  `429 rate_limited` with `Retry-After`, and the password-work limit shares that
  answer. The limits are in-process, like the rest of the authentication
  surface.
- **The operator command is `tendo users set-password --login <login>`.** It
  runs inside the application container with the runtime database role and no
  administrator credentials, so its authority boundary is host or container
  access, exactly like `instance-admin grant`. The password is read twice from
  the controlling terminal with echo disabled; a non-interactive stdin is
  refused rather than reading a password from a pipe that might be recorded.
  An interrupt or `SIGTERM` during the prompt restores the saved terminal mode
  before exiting with status 130, so an aborted reset never leaves the shell
  without echo. Nothing about the password can be passed in `argv`, so it cannot appear in
  `ps` or shell history, and it is never logged. The password policy is checked
  before the account is looked up, so a policy violation never depends on
  whether the login exists. An account without a local credential is refused
  (`ErrNoLocalPassword`) instead of silently gaining a password sign-in method,
  which keeps the OIDC-only account's provider as its only credential. On
  success the command prints the login and how many live sessions it signed
  out, and never any secret. The stored password is hashed with the same
  Argon2 implementation as every other path.
- **Success is recorded operationally, not in a new table.** Both paths emit one
  structured log line with the user ID and no secrets (`local password
  changed` / `local password reset`, the latter with the sign-out count). There
  is no authentication audit table today; adding one is not part of this
  decision.

## Consequences

- Everyone signed in as the account is signed out by a change or an operator
  reset, and the person must sign in again with the new password; the one
  exception is the browser that performed the change, which receives a fresh
  cookie. On other devices the next request answers `401` and the client returns
  to sign-in.
- The web client must offer the change only for a password-signed-in session.
  `signInMethod` on the session response is the field that decides it; a client
  must not assume a session is local because the account has a password.
- A downgrade to a release that does not read `sign_in_method` keeps the
  sessions but loses the recorded kind; re-upgrading re-derives `legacy` for
  them, so nobody gains the change right by downgrading and upgrading. The
  `Down` section drops the column and is lossy by necessity.
- A backup taken before a change still contains the previous hash and the
  outstanding reset links; restoring it restores both. The same applies to
  sessions deleted by the change.
- Every future password-replacement path must go through
  `CredentialReplacementTransaction` and keep the documented lock order. A path
  that writes `local_credentials` directly would break the reset-link
  revocation, the session sweep and the sign-in revalidation at once.
- The operator command needs an interactive terminal, so unattended automation
  cannot use it with a password. That is deliberate: the alternative is a
  secret in `argv`, an environment variable or a pipe.
- Outstanding follow-ups not decided here: an authentication audit trail for
  privileged identity events, MCP OAuth token revocation on credential
  replacement (the protocol comment names the place), and an email or
  notification when an operator resets a password.
