-- +goose Up
-- V5-12 slice A1: per-user MCP OAuth grant store (ADR 0023).
--
-- New tables only; no existing row changes, so an upgrade from v4.0.0 keeps
-- every pre-existing table byte-for-byte. Secrets (authorization codes, access
-- and refresh tokens) are stored only as SHA-256 digests. A grant row holds no
-- secret: revoking it sets revoked_at/revoke_reason and deletes its codes and
-- tokens, and mcp_grant_events keeps the append-only audit (SECURITY "Audit
-- records ... MCP grants"). Grants are never deleted.
--
-- Lock order (ADR 0023, ADR 0019 "Global lock order"): user_accounts ->
-- local_credentials -> user_sessions -> mcp_authorization_codes -> mcp_grants
-- -> token rows. Every MCP issuance path re-reads the live account FOR SHARE,
-- which conflicts with account deletion's FOR NO KEY UPDATE.

-- Registered OAuth public clients. Registration (RFC 7591), the pending consent
-- requests and their write privileges arrive with slice A2; until then the
-- runtime role only reads clients.
CREATE TABLE mcp_clients (
 client_id text COLLATE "C" PRIMARY KEY CHECK (client_id ~ '^[A-Za-z0-9_-]{16,128}$'),
 client_name text NOT NULL CHECK (char_length(client_name) BETWEEN 1 AND 100),
 redirect_uris text[] NOT NULL CHECK (
   cardinality(redirect_uris) BETWEEN 1 AND 5 AND array_position(redirect_uris, NULL) IS NULL
 ),
 created_at timestamptz NOT NULL,
 last_used_at timestamptz NULL CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);
REVOKE ALL ON mcp_clients FROM PUBLIC;
GRANT SELECT ON mcp_clients TO tendo;

-- A user's authorization of one client for one resource. Created at code
-- exchange; revoked, never deleted.
CREATE TABLE mcp_grants (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 user_id uuid NOT NULL REFERENCES user_accounts(id),
 client_id text COLLATE "C" NOT NULL REFERENCES mcp_clients(client_id),
 scopes text[] NOT NULL CHECK (
   cardinality(scopes) >= 1 AND scopes <@ ARRAY['tendo:read','tendo:write']::text[]
 ),
 resource text NOT NULL CHECK (octet_length(resource) BETWEEN 1 AND 2048),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '2160 hours'),
 revoked_at timestamptz NULL CHECK (revoked_at IS NULL OR revoked_at >= created_at),
 revoke_reason text NULL CHECK (revoke_reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse'
 )),
 CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL))
);
CREATE INDEX mcp_grants_user_id_idx ON mcp_grants(user_id);
CREATE INDEX mcp_grants_client_id_idx ON mcp_grants(client_id);
REVOKE ALL ON mcp_grants FROM PUBLIC;
GRANT SELECT, INSERT ON mcp_grants TO tendo;
GRANT UPDATE (revoked_at, revoke_reason) ON mcp_grants TO tendo;

-- Single-use PKCE authorization codes issued by an approved consent. A used
-- code keeps issued_grant_id so presenting it again revokes that grant.
CREATE TABLE mcp_authorization_codes (
 code_hash bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
 user_id uuid NOT NULL REFERENCES user_accounts(id),
 client_id text COLLATE "C" NOT NULL REFERENCES mcp_clients(client_id),
 redirect_uri text NOT NULL CHECK (octet_length(redirect_uri) BETWEEN 1 AND 2048),
 code_challenge text COLLATE "C" NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
 scopes text[] NOT NULL CHECK (
   cardinality(scopes) >= 1 AND scopes <@ ARRAY['tendo:read','tendo:write']::text[]
 ),
 resource text NOT NULL CHECK (octet_length(resource) BETWEEN 1 AND 2048),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes'),
 used_at timestamptz NULL CHECK (used_at IS NULL OR used_at >= created_at),
 issued_grant_id uuid NULL REFERENCES mcp_grants(id),
 CHECK ((used_at IS NULL) = (issued_grant_id IS NULL))
);
CREATE INDEX mcp_authorization_codes_user_id_idx ON mcp_authorization_codes(user_id);
CREATE INDEX mcp_authorization_codes_issued_grant_id_idx ON mcp_authorization_codes(issued_grant_id);
REVOKE ALL ON mcp_authorization_codes FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON mcp_authorization_codes TO tendo;
GRANT UPDATE (used_at, issued_grant_id) ON mcp_authorization_codes TO tendo;

CREATE TABLE mcp_access_tokens (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
 grant_id uuid NOT NULL REFERENCES mcp_grants(id),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '1 hour')
);
CREATE INDEX mcp_access_tokens_grant_id_idx ON mcp_access_tokens(grant_id);
REVOKE ALL ON mcp_access_tokens FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON mcp_access_tokens TO tendo;

-- Rotated on every use. A consumed digest is kept until its grant is revoked
-- so presenting it again is detected as replay and revokes the grant.
CREATE TABLE mcp_refresh_tokens (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
 grant_id uuid NOT NULL REFERENCES mcp_grants(id),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '2160 hours'),
 consumed_at timestamptz NULL CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX mcp_refresh_tokens_grant_id_idx ON mcp_refresh_tokens(grant_id);
REVOKE ALL ON mcp_refresh_tokens FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON mcp_refresh_tokens TO tendo;
GRANT UPDATE (consumed_at) ON mcp_refresh_tokens TO tendo;

-- Append-only grant audit: creation and every revocation with its cause. The
-- client is a snapshot (id and display name), no secret is copied.
-- actor_user_id is the human who caused the event; it is NULL for the
-- automatic replay and code-reuse revocations and for the operator CLI reset.
CREATE TABLE mcp_grant_events (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 grant_id uuid NOT NULL REFERENCES mcp_grants(id),
 user_id uuid NOT NULL REFERENCES user_accounts(id),
 client_id text COLLATE "C" NOT NULL,
 client_name text NOT NULL,
 event text NOT NULL CHECK (event IN ('created','revoked')),
 reason text NULL CHECK (reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse'
 )),
 actor_user_id uuid NULL REFERENCES user_accounts(id),
 occurred_at timestamptz NOT NULL,
 CHECK ((event = 'created') = (reason IS NULL))
);
CREATE INDEX mcp_grant_events_grant_id_idx ON mcp_grant_events(grant_id);
CREATE INDEX mcp_grant_events_user_id_idx ON mcp_grant_events(user_id);
REVOKE ALL ON mcp_grant_events FROM PUBLIC;
GRANT SELECT, INSERT ON mcp_grant_events TO tendo;

-- +goose Down
-- Lossy by necessity: drops every MCP client registration, grant, outstanding
-- code and token, and the grant audit. Earlier releases have no MCP endpoint,
-- so nothing can use them after a downgrade. Restore a pre-downgrade backup if
-- the grant audit matters.
REVOKE ALL ON mcp_grant_events, mcp_refresh_tokens, mcp_access_tokens, mcp_authorization_codes, mcp_grants, mcp_clients FROM tendo;
DROP TABLE mcp_grant_events;
DROP TABLE mcp_refresh_tokens;
DROP TABLE mcp_access_tokens;
DROP TABLE mcp_authorization_codes;
DROP TABLE mcp_grants;
DROP TABLE mcp_clients;
