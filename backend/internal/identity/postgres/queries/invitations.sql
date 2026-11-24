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
FROM household_invitations
WHERE household_id=sqlc.arg('household_id') AND (sqlc.narg('after_id')::uuid IS NULL OR id > sqlc.narg('after_id')::uuid)
  AND (sqlc.narg('active_at')::timestamptz IS NULL OR accepted_at IS NOT NULL OR revoked_at IS NOT NULL OR expires_at > sqlc.narg('active_at')::timestamptz)
ORDER BY id LIMIT sqlc.arg('row_limit');

-- name: RevokeInvitation :execrows
-- A terminal invitation keeps no token digest (migration 031, ADR 0024).
UPDATE household_invitations SET revoked_at=$3, token_hash=NULL WHERE household_id=$1 AND id=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3;

-- name: AcceptInvitation :execrows
UPDATE household_invitations SET accepted_at=$2, token_hash=NULL WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2;

-- name: SweepExpiredInvitationDigests :execrows
-- One bounded batch. An invitation past expiry can never be accepted, so its
-- digest is cleared and the redacted row stays. SKIP LOCKED leaves a row that an
-- acceptance or revocation holds; that transaction clears the digest itself or
-- the next batch does.
UPDATE household_invitations SET token_hash=NULL WHERE id IN (
  SELECT id FROM household_invitations
  WHERE token_hash IS NOT NULL AND expires_at <= sqlc.arg(cutoff)::timestamptz
  ORDER BY id LIMIT 1000 FOR UPDATE SKIP LOCKED
);

-- name: InvitationExistsInHousehold :one
SELECT EXISTS(SELECT 1 FROM household_invitations WHERE household_id=$1 AND id=$2);

-- name: LockUserForInvitation :one
-- The accept path only serializes writes to this account row (joining, default
-- household) and never touches its credential or reset links, so it needs no
-- exclusion of the foreign-key KEY SHARE a session insert takes. FOR NO KEY
-- UPDATE keeps concurrent acceptances and account deletion serialized without
-- that conflict (ADR 0018). A deleted account is not found, so an invitation
-- never revives a tombstone.
SELECT login FROM user_accounts WHERE id=$1 AND deleted_at IS NULL FOR NO KEY UPDATE;

-- name: FindLoginForInvitation :one
-- A deleted account is not found: its tombstone login is never a valid
-- invitation target.
SELECT id::text FROM user_accounts WHERE login=$1 AND deleted_at IS NULL;
