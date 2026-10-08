-- +goose Up
CREATE TABLE household_invitations (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 created_by_user_id uuid NOT NULL REFERENCES user_accounts(id),
 creation_key text NOT NULL CHECK (octet_length(creation_key) BETWEEN 1 AND 128 AND creation_key !~ '[^!-~]'),
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
 accepted_at timestamptz NULL,
 revoked_at timestamptz NULL,
 CHECK (accepted_at IS NULL OR revoked_at IS NULL),
 CHECK (accepted_at IS NULL OR accepted_at >= created_at),
 CHECK (revoked_at IS NULL OR revoked_at >= created_at),
 UNIQUE (household_id, created_by_user_id, creation_key)
);
CREATE INDEX household_invitations_household_id_id_idx ON household_invitations(household_id, id);
REVOKE ALL ON household_invitations FROM PUBLIC;
GRANT SELECT, INSERT ON household_invitations TO tendo;
GRANT UPDATE (accepted_at, revoked_at) ON household_invitations TO tendo;

-- +goose Down
REVOKE ALL ON household_invitations FROM tendo;
DROP TABLE household_invitations;
