-- name: LockAssignableAccount :one
-- Same lock as invitation acceptance (LockUserForInvitation): an instance-admin
-- assignment and an acceptance or removal of the same account serialize on the
-- account row without conflicting with session foreign-key key shares.
SELECT login FROM user_accounts WHERE id=$1 FOR NO KEY UPDATE;

-- name: SetDefaultHouseholdIfUnset :exec
-- An instance-admin assignment never moves an existing navigation hint.
UPDATE user_accounts SET default_household_id = sqlc.arg(household_id)::uuid
WHERE id = sqlc.arg(user_id)::uuid AND default_household_id IS NULL;

-- name: ListDirectoryAccounts :many
-- Keyset on (lower(login), id), served by user_accounts_lower_login_id_idx.
-- The search pattern arrives already escaped for LIKE with backslash.
SELECT id::text AS id, login
FROM user_accounts
WHERE (sqlc.arg(pattern)::text = '' OR lower(login) LIKE '%' || lower(sqlc.arg(pattern)::text) || '%' ESCAPE '\')
  AND (sqlc.narg(after_id)::uuid IS NULL OR (lower(login), id) > (lower(sqlc.arg(after_login)::text), sqlc.narg(after_id)::uuid))
ORDER BY lower(login), id
LIMIT sqlc.arg(row_limit);
