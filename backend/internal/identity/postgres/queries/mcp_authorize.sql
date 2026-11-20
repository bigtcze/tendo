-- MCP authorization code flow, slice A3a (ADR 0023). Pending consents are keyed
-- by the SHA-256 digests of the handle and of the browser binding; a consent is
-- single use and never visible past expires_at. The lock order is the one in
-- mcp_grants.sql: the client is held FOR KEY SHARE before a consent row, and a
-- consent row is held before the code it issues.

-- name: CreateMCPAuthorizationRequest :exec
-- An empty state is stored as NULL: the CHECK refuses a zero-length state.
INSERT INTO mcp_authorization_requests(request_hash, binding_hash, client_id, redirect_uri, code_challenge, scopes, resource, state, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF(sqlc.arg(state)::text, ''), $8, $9);

-- name: FindMCPAuthorizationRequest :one
-- The unlocked read. The client name comes from the joined client row; the
-- foreign key guarantees it exists.
SELECT r.client_id, c.client_name, r.redirect_uri, r.code_challenge, r.scopes, r.resource, r.state, r.created_at, r.expires_at
FROM mcp_authorization_requests r JOIN mcp_clients c ON c.client_id = r.client_id
WHERE r.request_hash = $1 AND r.binding_hash = $2 AND r.expires_at > $3;

-- name: ConsumeMCPAuthorizationRequest :one
-- Single use: the delete and the return are one statement. A wrong binding or an
-- expired row matches nothing and deletes nothing.
DELETE FROM mcp_authorization_requests
WHERE request_hash = $1 AND binding_hash = $2 AND expires_at > $3
RETURNING client_id, redirect_uri, code_challenge, scopes, resource, state, created_at, expires_at;

-- name: FindMCPTokenGrantOwner :one
-- The unlocked pre-read for revocation. An access token or a refresh token,
-- consumed or not, names its grant. Revoked grants still answer: the service
-- decides what a revoked grant means.
SELECT g.user_id, g.id AS grant_id FROM mcp_grants g JOIN (
  SELECT a.grant_id FROM mcp_access_tokens a WHERE a.token_hash = $1
  UNION ALL
  SELECT r.grant_id FROM mcp_refresh_tokens r WHERE r.token_hash = $1
) t ON t.grant_id = g.id
LIMIT 1;

-- name: SweepMCPAuthorizationRequestsExpired :execrows
-- One batch by primary key. The sweep loops while a batch comes back full.
DELETE FROM mcp_authorization_requests WHERE request_hash IN (
  SELECT request_hash FROM mcp_authorization_requests WHERE expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY request_hash LIMIT 1000
);
