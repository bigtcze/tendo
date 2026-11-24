-- name: RevokePendingInvitationsCreatedBy :exec
-- A removed member must not keep a usable invitation into the household. The
-- rows stay as redacted history; the token digest is cleared (ADR 0024).
UPDATE household_invitations SET revoked_at=$3, token_hash=NULL
WHERE household_id=$1 AND created_by_user_id=$2
  AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: RevokeMemberResetsInHousehold :exec
-- Reset links the removed member is party to inside this household stop
-- working: as a target they cannot be redeemed by an admin of a household they
-- left, and as an issuer they lose the admin role that redemption re-checks.
-- Links of the same account issued through another household are untouched.
UPDATE password_reset_tokens SET revoked_at=$3
WHERE household_id=$1 AND (target_user_id=$2 OR created_by_user_id=$2)
  AND used_at IS NULL AND revoked_at IS NULL;

-- name: ReassignDefaultHousehold :exec
-- The account's default is a navigation hint guarded by the deferred composite
-- foreign key user_default_membership_fk (ADR 0015). It must be moved off the
-- removed household in the same transaction and before commit, otherwise the
-- constraint fails the removal. The lowest-id remaining membership is
-- deterministic; no membership leaves NULL.
UPDATE user_accounts SET default_household_id = (
 SELECT m.household_id FROM household_memberships m
 WHERE m.user_id = sqlc.arg(user_id)::uuid AND m.household_id <> sqlc.arg(removed_household_id)::uuid
 ORDER BY m.household_id LIMIT 1
)
WHERE id = sqlc.arg(user_id)::uuid AND default_household_id = sqlc.arg(removed_household_id)::uuid;
