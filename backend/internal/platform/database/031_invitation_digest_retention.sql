-- +goose Up
-- V5-11 invitation retention (ADR 0024). Only a pending invitation keeps the
-- SHA-256 digest of its bearer token. Acceptance, revocation (including the
-- revocation that comes with member removal and account deletion) and expiry
-- clear the digest. The row itself stays as the minimal redacted record:
-- household, creator, idempotency key and timestamps, never the secret. No row
-- is deleted, so history, list output and idempotent create retries are kept.
ALTER TABLE household_invitations ALTER COLUMN token_hash DROP NOT NULL;

-- Purge the digests a v4 database still holds for used, revoked and already
-- expired invitations. Still-pending invitations keep theirs and stay usable.
UPDATE household_invitations SET token_hash = NULL
WHERE accepted_at IS NOT NULL OR revoked_at IS NOT NULL OR expires_at <= now();

-- A terminal invitation never holds a digest again.
ALTER TABLE household_invitations ADD CONSTRAINT household_invitations_terminal_no_digest
 CHECK (token_hash IS NULL OR (accepted_at IS NULL AND revoked_at IS NULL));

-- The retention sweep finds expired digests through this index.
CREATE INDEX household_invitations_digest_expires_at_idx ON household_invitations(expires_at)
 WHERE token_hash IS NOT NULL;

-- The runtime role may clear a digest and nothing else: an update may keep
-- token_hash or set it to NULL, never to a new value, so no invitation can be
-- revived or re-keyed. It still cannot delete invitations or change their household,
-- creator, key or expiry.
CREATE FUNCTION public.tendo_invitation_digest_clear_only() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
 IF NEW.token_hash IS NOT NULL AND NEW.token_hash IS DISTINCT FROM OLD.token_hash THEN
  RAISE EXCEPTION 'invitation token digest can only be cleared' USING ERRCODE = '42501';
 END IF;
 RETURN NEW;
END
$$;
REVOKE ALL ON FUNCTION public.tendo_invitation_digest_clear_only() FROM PUBLIC;
CREATE TRIGGER household_invitations_digest_clear_only BEFORE UPDATE OF token_hash ON household_invitations
FOR EACH ROW EXECUTE FUNCTION public.tendo_invitation_digest_clear_only();
GRANT UPDATE (token_hash) ON household_invitations TO tendo;

-- +goose Down
-- A cleared digest cannot be restored. Each redacted row gets a random digest
-- whose token nobody holds, so NOT NULL can return and no row becomes usable:
-- terminal rows stay terminal and expired rows stay expired.
REVOKE UPDATE (token_hash) ON household_invitations FROM tendo;
DROP TRIGGER household_invitations_digest_clear_only ON household_invitations;
DROP FUNCTION public.tendo_invitation_digest_clear_only();
DROP INDEX household_invitations_digest_expires_at_idx;
ALTER TABLE household_invitations DROP CONSTRAINT household_invitations_terminal_no_digest;
UPDATE household_invitations SET token_hash = sha256(convert_to(gen_random_uuid()::text || id::text, 'UTF8'))
WHERE token_hash IS NULL;
ALTER TABLE household_invitations ALTER COLUMN token_hash SET NOT NULL;
