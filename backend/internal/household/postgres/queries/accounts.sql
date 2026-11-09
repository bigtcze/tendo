-- name: GetMembershipAccount :one
SELECT user_id::text AS user_id, role, version
FROM household_memberships
WHERE household_id=$1 AND user_id=$2;

-- name: ListMembershipAccounts :many
SELECT user_id::text AS user_id, role, version
FROM household_memberships
WHERE household_id=$1 AND ($2::uuid IS NULL OR user_id > $2::uuid)
ORDER BY user_id LIMIT $3;

-- name: UpdateMembershipRole :one
UPDATE household_memberships SET role=$3, version=version+1
WHERE household_id=$1 AND user_id=$2
RETURNING version;

-- name: CountHouseholdAdmins :one
SELECT count(*) FROM household_memberships WHERE household_id=$1 AND role='admin';

-- name: InsertHouseholdRoleChange :exec
INSERT INTO household_role_changes(household_id,target_user_id,actor_user_id,previous_role,new_role,changed_at)
VALUES($1,$2,$3,$4,$5,$6);
