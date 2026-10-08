-- name: CreateInvitation :one
INSERT INTO household_invitations(household_id, created_by_user_id, creation_key, token_hash, created_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (household_id, created_by_user_id, creation_key) DO NOTHING
RETURNING id::text, household_id::text, created_by_user_id::text, creation_key, token_hash, created_at, expires_at, accepted_at, revoked_at;

-- name: FindInvitationByCreationKey :one
SELECT id::text, household_id::text, created_by_user_id::text, creation_key, token_hash, created_at, expires_at, accepted_at, revoked_at
FROM household_invitations WHERE household_id=$1 AND created_by_user_id=$2 AND creation_key=$3;

-- name: FindInvitationByToken :one
SELECT id::text, household_id::text, created_by_user_id::text, creation_key, token_hash, created_at, expires_at, accepted_at, revoked_at
FROM household_invitations WHERE token_hash=$1;

-- name: FindInvitationByTokenForUpdate :one
SELECT id::text, household_id::text, created_by_user_id::text, creation_key, token_hash, created_at, expires_at, accepted_at, revoked_at
FROM household_invitations WHERE token_hash=$1 FOR UPDATE;

-- name: ListInvitations :many
SELECT id::text, household_id::text, created_by_user_id::text, creation_key, token_hash, created_at, expires_at, accepted_at, revoked_at
FROM household_invitations WHERE household_id=$1 AND ($2::uuid IS NULL OR id > $2::uuid)
ORDER BY id LIMIT $3;

-- name: RevokeInvitation :execrows
UPDATE household_invitations SET revoked_at=$3 WHERE household_id=$1 AND id=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3;

-- name: AcceptInvitation :execrows
UPDATE household_invitations SET accepted_at=$2 WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2;

-- name: InvitationExistsInHousehold :one
SELECT EXISTS(SELECT 1 FROM household_invitations WHERE household_id=$1 AND id=$2);

-- name: LockUserForInvitation :one
SELECT id::text, login, COALESCE(default_household_id::text, '')::text AS default_household_id FROM user_accounts WHERE id=$1 FOR UPDATE;

-- name: FindLoginForInvitation :one
SELECT id::text FROM user_accounts WHERE login=$1;

-- name: HasMembershipElsewhere :one
SELECT EXISTS(SELECT 1 FROM household_memberships WHERE user_id=$1 AND household_id<>$2);
