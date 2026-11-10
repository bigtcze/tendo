-- name: LockPasswordResetTarget :one
SELECT id::text AS id, login FROM user_accounts WHERE id=$1 FOR UPDATE;

-- name: FindUserLogin :one
SELECT login FROM user_accounts WHERE id=$1;

-- name: HasLocalCredential :one
SELECT EXISTS(SELECT 1 FROM local_credentials WHERE user_id=$1);

-- name: RevokeOutstandingPasswordResetsForTarget :execrows
UPDATE password_reset_tokens SET revoked_at=$2
WHERE target_user_id=$1 AND used_at IS NULL AND revoked_at IS NULL;

-- name: RevokeOutstandingPasswordResetsInHousehold :execrows
UPDATE password_reset_tokens SET revoked_at=$3
WHERE household_id=$1 AND target_user_id=$2 AND used_at IS NULL AND revoked_at IS NULL;

-- name: CreatePasswordReset :one
INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6)
RETURNING id::text, household_id::text, target_user_id::text, created_by_user_id::text, created_at, expires_at, used_at, revoked_at;

-- name: FindPasswordResetByToken :one
SELECT id::text, household_id::text, target_user_id::text, created_by_user_id::text, created_at, expires_at, used_at, revoked_at
FROM password_reset_tokens WHERE token_hash=$1;

-- name: FindPasswordResetByTokenForUpdate :one
SELECT id::text, household_id::text, target_user_id::text, created_by_user_id::text, created_at, expires_at, used_at, revoked_at
FROM password_reset_tokens WHERE token_hash=$1 FOR UPDATE;

-- name: CompletePasswordReset :execrows
UPDATE password_reset_tokens SET used_at=$2
WHERE id=$1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $2;

-- name: UpdateLocalCredential :exec
UPDATE local_credentials SET password_hash=$2 WHERE user_id=$1;

-- name: DeleteUserSessions :exec
DELETE FROM user_sessions WHERE user_id=$1;

-- name: OutstandingPasswordResetsByUserIDs :many
SELECT target_user_id::text AS user_id, created_at, expires_at
FROM password_reset_tokens
WHERE household_id=$1 AND target_user_id = ANY($2::uuid[])
  AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $3;
