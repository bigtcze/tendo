-- name: LockSetupState :one
SELECT setup_required FROM installation_state WHERE singleton = true FOR UPDATE;

-- name: CreateUser :one
INSERT INTO user_accounts (login) VALUES ($1) RETURNING id::text;

-- name: SetDefaultHousehold :exec
UPDATE user_accounts SET default_household_id = $2 WHERE id = $1;

-- name: CompleteSetup :exec
UPDATE installation_state SET setup_required = false WHERE singleton = true;

-- name: CreateLocalCredential :exec
INSERT INTO local_credentials (user_id, password_hash) VALUES ($1, $2);
