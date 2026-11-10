-- +goose Up
-- V4-08: admin-issued local-password reset links.
--
-- Only the SHA-256 digest of a link token is stored. Rows are retained as
-- audit history; the partial unique index keeps at most one outstanding link
-- per target account, and issuing again revokes the previous one first.
CREATE TABLE password_reset_tokens (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 target_user_id uuid NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
 created_by_user_id uuid NOT NULL REFERENCES user_accounts(id),
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
 used_at timestamptz NULL,
 revoked_at timestamptz NULL,
 CHECK (used_at IS NULL OR revoked_at IS NULL),
 CHECK (used_at IS NULL OR used_at >= created_at),
 CHECK (revoked_at IS NULL OR revoked_at >= created_at),
 CHECK (target_user_id <> created_by_user_id)
);
CREATE UNIQUE INDEX password_reset_tokens_outstanding_target_idx
 ON password_reset_tokens(target_user_id) WHERE used_at IS NULL AND revoked_at IS NULL;
CREATE INDEX password_reset_tokens_household_target_idx
 ON password_reset_tokens(household_id, target_user_id);
REVOKE ALL ON password_reset_tokens FROM PUBLIC;
GRANT SELECT, INSERT ON password_reset_tokens TO tendo;
GRANT UPDATE (used_at, revoked_at) ON password_reset_tokens TO tendo;

-- +goose Down
REVOKE ALL ON password_reset_tokens FROM tendo;
DROP TABLE password_reset_tokens;
