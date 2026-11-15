-- The identity-owned side of a global account deletion (ADR 0022). Every
-- statement runs inside the household-owned deletion transaction and follows
-- the ADR 0019 global lock order.

-- name: FindDeletableAccount :one
-- The unlocked pre-read of a live account's login. A missing or already
-- tombstoned account is not found, so it can never be a deletion target.
SELECT login FROM user_accounts WHERE id = $1 AND deleted_at IS NULL;

-- name: ListAccountIdentityKeys :many
-- The identity pairs whose advisory lock the deletion must take, so a
-- concurrent OIDC sign-in either finishes before the transaction or sees the
-- identity unlinked (ADR 0019 step 4).
SELECT issuer, subject FROM oidc_identities WHERE user_id = $1;

-- name: LockDeletableAccount :one
-- FOR NO KEY UPDATE, never FOR UPDATE: a session insert takes KEY SHARE here
-- and must not be blocked into a deadlock (ADR 0018, ADR 0019 step 4).
SELECT login FROM user_accounts WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE;

-- name: RevokeAccountPasswordResets :exec
-- Every outstanding reset link the account is party to, as target or issuer, in
-- any household, stops working. The per-household revocations already ran; this
-- catches links in households the account was never a member of.
UPDATE password_reset_tokens SET revoked_at = $2
WHERE (target_user_id = $1 OR created_by_user_id = $1)
  AND used_at IS NULL AND revoked_at IS NULL;

-- name: DeleteAccountCredentials :exec
-- The local password goes first, in the ADR 0018 replacement order, so no
-- password sign-in can begin before the sessions are gone.
DELETE FROM local_credentials WHERE user_id = $1;

-- name: DeleteAccountSessions :exec
-- Every session of the account, including one issued by an OIDC sign-in that
-- committed just before this lock.
DELETE FROM user_sessions WHERE user_id = $1;

-- name: DeleteAccountOIDCFlows :exec
-- Link flows of the account carry its session; they are useless once the
-- identity rows and sessions are gone.
DELETE FROM oidc_flows WHERE user_id = $1;

-- name: DeleteAccountIdentities :exec
-- The links themselves, so the identity signs in as unlinked next time.
DELETE FROM oidc_identities WHERE user_id = $1;

-- name: AnonymizeAccount :execrows
-- The tombstone keeps the row and its identifier so every history row still
-- references a real account. The login becomes '~deleted-<32 hex>', which no
-- login pattern accepts and which is unique by construction, so the partial
-- unique index frees the old login. Email facts, the default household and
-- every session are cleared; exactly one live row must change.
UPDATE user_accounts
SET login = '~deleted-' || replace(id::text, '-', ''),
    email = NULL,
    email_verified_at = NULL,
    email_source = NULL,
    email_issuer = NULL,
    default_household_id = NULL,
    deleted_at = $2
WHERE id = $1 AND deleted_at IS NULL;
