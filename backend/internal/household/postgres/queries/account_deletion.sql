-- The global account deletion (ADR 0022). Its statements are household-owned
-- because the same transaction reuses the membership removal statements of
-- ADR 0019; the identity-owned eraser has its own file.

-- name: LockInstallationForInstanceAdminExclusive :one
-- FOR UPDATE, not FOR SHARE: a deletion changes the installation-wide
-- administrator set, so it must serialize with grants and with other deletions
-- (ADR 0019 global lock order, step 1). The runtime role holds
-- UPDATE(setup_required) on this table, which is enough to take the row lock
-- and exactly the column the command must not change.
SELECT setup_required FROM installation_state WHERE singleton = true FOR UPDATE;

-- name: ListUserHouseholds :many
-- The unlocked pre-read of the target's households, in the ascending order the
-- household locks are taken in. Membership rows are read without a lock: the
-- locked re-read after the account lock decides whether the set changed.
SELECT household_id::text AS household_id
FROM household_memberships
WHERE user_id = $1
ORDER BY household_id;

-- name: LockUserMemberships :many
-- FOR UPDATE in ascending household order, so the last-admin counts and the
-- deletion of each membership are answered under the same lock as a concurrent
-- removal of the same member (ADR 0019 step 5).
SELECT household_id::text AS household_id, role
FROM household_memberships
WHERE user_id = $1
ORDER BY household_id
FOR UPDATE;

-- name: FindInstanceAdminGrant :one
-- The target's live grant, read after installation_state is locked, so the
-- last-instance-administrator decision cannot race a grant or revoke.
SELECT grant_source, granted_at FROM instance_admins WHERE user_id = $1;

-- name: CountLiveInstanceAdminsExcluding :one
-- Only grants of live accounts count: a tombstoned account confers no
-- authority (ADR 0019, defense in depth against a left-behind grant row).
SELECT count(*)
FROM instance_admins g
JOIN user_accounts u ON u.id = g.user_id
WHERE g.user_id <> $1 AND u.deleted_at IS NULL;

-- name: InsertInstanceAdminRevocation :exec
-- Append-only revoke audit (ADR 0016): the grant's source and time move here
-- before the grant row is deleted, so the history stays complete.
INSERT INTO instance_admin_revocations(user_id, actor_user_id, grant_source, granted_at, revoked_at, reason)
VALUES($1, $2, $3, $4, $5, $6);

-- name: DeleteInstanceAdminGrant :execrows
-- Exactly one grant row must disappear; the adapter refuses any other count.
DELETE FROM instance_admins WHERE user_id = $1;

-- name: InsertAccountDeletion :exec
-- Who deleted which account and when. No login, email or household content is
-- copied (migration 027).
INSERT INTO account_deletions(user_id, actor_user_id, deleted_at)
VALUES($1, $2, $3);
