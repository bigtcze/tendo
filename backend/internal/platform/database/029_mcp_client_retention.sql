-- +goose Up
-- V5-12 slice A2: MCP client registration privileges and retention indexes
-- (ADR 0023). No table or column is added or changed, so an upgrade keeps every
-- existing row byte-for-byte.
--
-- Registration (RFC 7591) inserts client rows and the retention sweep deletes
-- them after 24 hours without a grant or code. The sweep and expiry deletes
-- need the expiry indexes below to stay short under lock_timeout.
--
-- Lock grants on mcp_clients: PostgreSQL requires UPDATE privilege on at least
-- one column for every row-locking clause, including FOR KEY SHARE. UPDATE
-- (last_used_at) is therefore granted so the runtime role can take the FOR KEY
-- SHARE lock in code issuance and the FOR UPDATE SKIP LOCKED lock in the client
-- sweep. No other client column is writable by the runtime role.

GRANT INSERT, DELETE ON mcp_clients TO tendo;
GRANT UPDATE (last_used_at) ON mcp_clients TO tendo;

CREATE INDEX mcp_clients_created_at_idx ON mcp_clients(created_at);
CREATE INDEX mcp_authorization_codes_client_id_idx ON mcp_authorization_codes(client_id);
CREATE INDEX mcp_access_tokens_expires_at_idx ON mcp_access_tokens(expires_at);
CREATE INDEX mcp_refresh_tokens_expires_at_idx ON mcp_refresh_tokens(expires_at) WHERE consumed_at IS NULL;
CREATE INDEX mcp_authorization_codes_expires_at_idx ON mcp_authorization_codes(expires_at) WHERE used_at IS NULL;
CREATE INDEX mcp_grants_expires_at_idx ON mcp_grants(expires_at) WHERE revoked_at IS NULL;

-- +goose Down
-- Reverses only what this migration added. Client rows, grants and tokens are
-- left in place, so the reversal never loses registrations.
DROP INDEX mcp_grants_expires_at_idx;
DROP INDEX mcp_authorization_codes_expires_at_idx;
DROP INDEX mcp_refresh_tokens_expires_at_idx;
DROP INDEX mcp_access_tokens_expires_at_idx;
DROP INDEX mcp_authorization_codes_client_id_idx;
DROP INDEX mcp_clients_created_at_idx;
REVOKE UPDATE (last_used_at) ON mcp_clients FROM tendo;
REVOKE INSERT, DELETE ON mcp_clients FROM tendo;
