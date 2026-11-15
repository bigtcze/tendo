-- name: LockMemberAccountForRemoval :one
-- FOR NO KEY UPDATE serializes account-level writes without conflicting with
-- the snapshot/key-share locks a session insert or foreign key takes on the
-- same row (ADR 0018, ADR 0019).
SELECT id::text AS id FROM user_accounts WHERE id=$1 FOR NO KEY UPDATE;

-- name: LockMembershipForRemoval :one
-- FOR UPDATE, never FOR NO KEY UPDATE: a concurrent foreign-key insert that
-- references this membership (item responsible, member person) takes a key
-- share and must block until the removal commits, so it can no longer succeed
-- against a membership that is about to disappear (ADR 0019).
SELECT role, version FROM household_memberships
WHERE household_id=$1 AND user_id=$2
FOR UPDATE;

-- name: CountHouseholdAdminsExcluding :one
SELECT count(*) FROM household_memberships
WHERE household_id=$1 AND role='admin' AND user_id<>$2;

-- name: DeleteMembership :exec
DELETE FROM household_memberships WHERE household_id=$1 AND user_id=$2;

-- name: InsertHouseholdMemberRemoval :exec
INSERT INTO household_member_removals(household_id,target_user_id,actor_user_id,previous_role,removed_at,actor_authority)
VALUES($1,$2,$3,$4,$5,$6);
