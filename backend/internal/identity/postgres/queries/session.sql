-- name: FindLogin :one
SELECT u.id::text AS user_id, u.login, COALESCE(u.default_household_id::text, '')::text AS default_household_id, c.password_hash
FROM user_accounts u JOIN local_credentials c ON c.user_id=u.id WHERE u.login=$1;

-- name: MemberLogins :many
SELECT id::text AS user_id, login FROM user_accounts WHERE id = ANY($1::uuid[]);

-- name: DeleteExpiredSessions :exec
DELETE FROM user_sessions WHERE user_id=$1 AND expires_at <= $2;

-- name: CreateSession :one
INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text AS id;

-- name: FindActiveSession :one
SELECT s.id::text AS id, u.id::text AS user_id, u.login, COALESCE(u.default_household_id::text, '')::text AS default_household_id, s.expires_at
FROM user_sessions s JOIN user_accounts u ON u.id=s.user_id
WHERE s.token_hash=$1 AND s.expires_at>$2;

-- name: DeleteSession :exec
DELETE FROM user_sessions WHERE token_hash=$1;
