-- Shared local-credential replacement protocol (ADR 0018). One implementation
-- of "a password changed, sign the account out everywhere" is used by
-- self-service change, admin reset redemption and the operator CLI.

-- name: FindUserAccountByLogin :one
SELECT id::text AS user_id FROM user_accounts WHERE login=$1;

-- name: ReadLocalCredential :one
SELECT password_hash, credential_version FROM local_credentials WHERE user_id=$1;

-- name: ReplaceLocalPasswordHash :execrows
UPDATE local_credentials SET password_hash=$2, credential_version=credential_version+1 WHERE user_id=$1;

-- name: DeleteAllUserSessions :many
DELETE FROM user_sessions WHERE user_id=$1 RETURNING token_hash, expires_at;

-- name: UserSessionActive :one
SELECT EXISTS(SELECT 1 FROM user_sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>$3) AS active;
