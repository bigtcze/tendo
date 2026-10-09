-- +goose Up
CREATE TABLE oidc_identities (
 issuer text COLLATE "C" NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 2048),
 subject text COLLATE "C" NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 255 AND subject !~ '[^!-~]'),
 user_id uuid NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL,
 PRIMARY KEY (issuer, subject),
 UNIQUE (user_id, issuer)
);
REVOKE ALL ON oidc_identities FROM PUBLIC;
GRANT SELECT, INSERT ON oidc_identities TO tendo;

CREATE TABLE oidc_flows (
 state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash)=32),
 browser_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(browser_token_hash)=32),
 issuer text COLLATE "C" NOT NULL,
 client_id text NOT NULL,
 nonce text NOT NULL,
 pkce_verifier text NOT NULL CHECK (octet_length(pkce_verifier)=43),
 purpose text NOT NULL CHECK (purpose IN ('login','link')),
 user_id uuid NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
 session_id uuid NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes'),
 CHECK ((purpose='login' AND user_id IS NULL AND session_id IS NULL) OR (purpose='link' AND user_id IS NOT NULL AND session_id IS NOT NULL))
);
CREATE INDEX oidc_flows_expires_at_idx ON oidc_flows(expires_at);
REVOKE ALL ON oidc_flows FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON oidc_flows TO tendo;

-- +goose Down
REVOKE ALL ON oidc_flows FROM tendo;
REVOKE ALL ON oidc_identities FROM tendo;
DROP TABLE oidc_flows;
DROP TABLE oidc_identities;
