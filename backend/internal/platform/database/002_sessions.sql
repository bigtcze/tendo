-- +goose Up
CREATE TABLE user_sessions (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 user_id uuid NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK (expires_at > created_at)
);
CREATE INDEX user_sessions_user_id_idx ON user_sessions(user_id);
CREATE INDEX user_sessions_expires_at_idx ON user_sessions(expires_at);
REVOKE ALL ON user_sessions FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON user_sessions TO tendo;

-- +goose Down
REVOKE ALL ON user_sessions FROM tendo;
DROP TABLE user_sessions;
