-- name: GetMembership :one
SELECT role FROM household_memberships WHERE user_id=$1 AND household_id=$2;

-- name: LockHouseholdForMembership :one
SELECT h.id FROM households h
WHERE h.id=$1 AND EXISTS (SELECT 1 FROM household_memberships m WHERE m.household_id=h.id AND m.user_id=$2)
FOR NO KEY UPDATE OF h;

-- name: AddInvitedMember :exec
INSERT INTO household_memberships(user_id, household_id, role) VALUES($1, $2, 'member');

-- name: GetMembershipForInvitation :one
SELECT role FROM household_memberships WHERE user_id=$1 AND household_id=$2;

-- name: HasMembershipElsewhere :one
SELECT EXISTS(SELECT 1 FROM household_memberships WHERE user_id=$1 AND household_id<>$2);

-- name: ListMembers :many
SELECT user_id::text AS user_id, role
FROM household_memberships
WHERE household_id=$1 AND ($2::uuid IS NULL OR user_id > $2::uuid)
ORDER BY user_id LIMIT $3;
