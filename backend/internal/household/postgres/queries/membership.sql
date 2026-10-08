-- name: GetMembership :one
SELECT role FROM household_memberships WHERE user_id=$1 AND household_id=$2;

-- name: AddInvitedMember :exec
INSERT INTO household_memberships(user_id, household_id, role) VALUES($1, $2, 'member');

-- name: GetMembershipForInvitation :one
SELECT role FROM household_memberships WHERE user_id=$1 AND household_id=$2;

-- name: ListMembers :many
SELECT u.id::text AS user_id, u.login, m.role
FROM household_memberships m JOIN user_accounts u ON u.id=m.user_id
WHERE m.household_id=$1 AND ($2::uuid IS NULL OR u.id > $2::uuid)
ORDER BY u.id LIMIT $3;
