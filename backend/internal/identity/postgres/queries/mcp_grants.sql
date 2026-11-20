-- MCP OAuth grant store (ADR 0023). Every statement follows the lock order
-- user_accounts -> local_credentials -> mcp_authorization_codes -> mcp_grants
-- -> token rows, and the caller already holds the account-side locks.

-- name: DeleteAccountMCPCodes :exec
-- Every authorization code of the account, outstanding or used: a code is a
-- secret and a revoked account keeps none.
DELETE FROM mcp_authorization_codes WHERE user_id = $1;

-- name: RevokeAccountMCPGrants :execrows
-- Revokes every live grant of the account and audits each one with its cause in
-- the same statement. The grant row stays (secret-free) as history.
WITH revoked AS (
  UPDATE mcp_grants
  SET revoked_at = sqlc.arg(revoked_at), revoke_reason = sqlc.arg(revoke_reason)
  WHERE mcp_grants.user_id = sqlc.arg(user_id) AND revoked_at IS NULL
  RETURNING id, mcp_grants.user_id, client_id
)
INSERT INTO mcp_grant_events(grant_id, user_id, client_id, client_name, event, reason, actor_user_id, occurred_at)
SELECT r.id, r.user_id, r.client_id, c.client_name, 'revoked', sqlc.arg(revoke_reason), sqlc.narg(actor_user_id)::uuid, sqlc.arg(revoked_at)
FROM revoked r JOIN mcp_clients c ON c.client_id = r.client_id;

-- name: DeleteAccountMCPAccessTokens :exec
DELETE FROM mcp_access_tokens a USING mcp_grants g WHERE g.id = a.grant_id AND g.user_id = $1;

-- name: DeleteAccountMCPRefreshTokens :exec
-- Consumed digests go too: once every grant is revoked there is nothing left to
-- detect replay against.
DELETE FROM mcp_refresh_tokens r USING mcp_grants g WHERE g.id = r.grant_id AND g.user_id = $1;
