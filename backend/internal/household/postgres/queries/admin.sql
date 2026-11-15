-- name: LockInstallationForInstanceAdmin :one
-- FOR SHARE: every instance-admin command takes the installation row in share
-- mode so a future grant revoke, which must take it FOR UPDATE, serializes with
-- in-flight commands (ADR 0020). Concurrent instance-admin commands do not
-- block each other.
SELECT setup_required FROM installation_state WHERE singleton = true FOR SHARE;

-- name: IsInstanceAdmin :one
SELECT EXISTS (SELECT 1 FROM instance_admins WHERE user_id=$1) AS granted;

-- name: LockHouseholdByID :one
-- The instance-admin grant replaces the membership gate, so the household row
-- is locked by its ID alone, in the same mode as LockHouseholdForMembership.
SELECT id FROM households WHERE id=$1 FOR NO KEY UPDATE;

-- name: HouseholdExists :one
SELECT EXISTS (SELECT 1 FROM households WHERE id=$1) AS present;

-- name: ListAllHouseholds :many
SELECT id::text AS id, name, timezone, created_at
FROM households
WHERE (sqlc.narg(after_id)::uuid IS NULL OR id > sqlc.narg(after_id)::uuid)
ORDER BY id LIMIT sqlc.arg(row_limit);

-- name: InsertAssignedMembership :one
INSERT INTO household_memberships(user_id, household_id, role) VALUES($1, $2, $3)
RETURNING version;

-- name: InsertHouseholdMemberAssignment :exec
INSERT INTO household_member_assignments(household_id,target_user_id,actor_user_id,role,actor_authority,assigned_at)
VALUES($1,$2,$3,$4,$5,$6);
