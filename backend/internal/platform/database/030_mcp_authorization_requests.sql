-- +goose Up
-- V5-12 slice A3a: pending MCP consent requests and the client revocation
-- reason (ADR 0023).
--
-- A pending consent is the browser-facing authorization request between
-- /oauth/authorize and the signed-in human's decision. Only SHA-256 digests of
-- its handle and of its browser binding are stored, never the plaintext. A row
-- is single use: a decision deletes it (DELETE ... RETURNING), and the sweep
-- deletes rows past expires_at. The runtime role has no UPDATE on this table.
--
-- The revoke reason 'client' (RFC 7009 token revocation by the holding client)
-- widens the two reason CHECK sets. The constraint names are the ones PostgreSQL
-- generated for migration 028, verified in a migrated database; they are named
-- explicitly here so the Down section restores the same names.

CREATE TABLE mcp_authorization_requests (
 request_hash bytea PRIMARY KEY CHECK (octet_length(request_hash) = 32),
 binding_hash bytea NOT NULL CHECK (octet_length(binding_hash) = 32),
 client_id text COLLATE "C" NOT NULL REFERENCES mcp_clients(client_id),
 redirect_uri text NOT NULL CHECK (octet_length(redirect_uri) BETWEEN 1 AND 2048),
 code_challenge text COLLATE "C" NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
 scopes text[] NOT NULL CHECK (
   cardinality(scopes) >= 1 AND scopes <@ ARRAY['tendo:read','tendo:write']::text[]
 ),
 resource text NOT NULL CHECK (octet_length(resource) BETWEEN 1 AND 2048),
 state text NULL CHECK (state IS NULL OR (octet_length(state) BETWEEN 1 AND 1024 AND state ~ '^[ -~]+$')),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes')
);
CREATE INDEX mcp_authorization_requests_client_id_idx ON mcp_authorization_requests(client_id);
CREATE INDEX mcp_authorization_requests_expires_at_idx ON mcp_authorization_requests(expires_at);
REVOKE ALL ON mcp_authorization_requests FROM PUBLIC;
-- No UPDATE: a decision consumes the row with DELETE ... RETURNING.
GRANT SELECT, INSERT, DELETE ON mcp_authorization_requests TO tendo;

ALTER TABLE mcp_grants DROP CONSTRAINT mcp_grants_revoke_reason_check;
ALTER TABLE mcp_grants ADD CONSTRAINT mcp_grants_revoke_reason_check CHECK (revoke_reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client'
 ));
ALTER TABLE mcp_grant_events DROP CONSTRAINT mcp_grant_events_reason_check;
ALTER TABLE mcp_grant_events ADD CONSTRAINT mcp_grant_events_reason_check CHECK (reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client'
 ));

-- +goose Down
-- Lossy by necessity: the 'client' reason does not exist before this migration,
-- so every client revocation is recorded as 'user' (the grant's owner revoked
-- it through the client). The mapping is applied to mcp_grants and to the
-- append-only mcp_grant_events audit, which keeps its rows. Pending consents are
-- dropped; they are short-lived (at most 10 minutes) and no earlier release
-- serves them.
UPDATE mcp_grants SET revoke_reason = 'user' WHERE revoke_reason = 'client';
UPDATE mcp_grant_events SET reason = 'user' WHERE reason = 'client';
ALTER TABLE mcp_grant_events DROP CONSTRAINT mcp_grant_events_reason_check;
ALTER TABLE mcp_grant_events ADD CONSTRAINT mcp_grant_events_reason_check CHECK (reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse'
 ));
ALTER TABLE mcp_grants DROP CONSTRAINT mcp_grants_revoke_reason_check;
ALTER TABLE mcp_grants ADD CONSTRAINT mcp_grants_revoke_reason_check CHECK (revoke_reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse'
 ));
DROP TABLE mcp_authorization_requests;
