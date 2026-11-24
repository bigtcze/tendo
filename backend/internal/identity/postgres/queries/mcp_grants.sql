-- MCP OAuth grant store (ADR 0023). Every statement follows the lock order
-- user_accounts -> local_credentials -> user_sessions -> mcp_clients (KEY SHARE)
-- -> mcp_authorization_codes -> mcp_grants -> token rows, and the caller already
-- holds the account-side locks.

-- name: DeleteAccountMCPCodes :exec
-- Every authorization code of the account, outstanding or used: a code is a
-- secret and a revoked account keeps none.
DELETE FROM mcp_authorization_codes WHERE user_id = $1;

-- name: RevokeAccountMCPGrants :execrows
-- Revokes every live grant of the account and audits each one with its cause in
-- the same statement. The grant row stays (secret-free) as history.
-- revoked_at is clamped to the grant's created_at: a caller's clock (truncated
-- to seconds, read before lock waits, or skewed between instances) may be
-- earlier than a grant created moments ago, and an MCP CHECK must never block a
-- password replacement or an account deletion.
WITH revoked AS (
  UPDATE mcp_grants
  SET revoked_at = GREATEST(sqlc.arg(revoked_at)::timestamptz, mcp_grants.created_at),
      revoke_reason = sqlc.arg(revoke_reason)
  WHERE mcp_grants.user_id = sqlc.arg(user_id) AND mcp_grants.revoked_at IS NULL
  RETURNING mcp_grants.id, mcp_grants.user_id, mcp_grants.client_id, mcp_grants.revoked_at
)
INSERT INTO mcp_grant_events(grant_id, user_id, client_id, client_name, event, reason, actor_user_id, occurred_at)
SELECT r.id, r.user_id, r.client_id, c.client_name, 'revoked', sqlc.arg(revoke_reason), sqlc.narg(actor_user_id)::uuid, r.revoked_at
FROM revoked r JOIN mcp_clients c ON c.client_id = r.client_id;

-- name: DeleteAccountMCPAccessTokens :exec
DELETE FROM mcp_access_tokens a USING mcp_grants g WHERE g.id = a.grant_id AND g.user_id = $1;

-- name: DeleteAccountMCPRefreshTokens :exec
-- Consumed digests go too: once every grant is revoked there is nothing left to
-- detect replay against.
DELETE FROM mcp_refresh_tokens r USING mcp_grants g WHERE g.id = r.grant_id AND g.user_id = $1;

-- name: LockLiveMCPAccount :one
-- Every MCP issuance takes the live account FOR SHARE first. It conflicts with
-- account deletion's FOR NO KEY UPDATE, so no MCP secret row can be inserted
-- for an account whose deletion committed or is in progress.
SELECT id FROM user_accounts WHERE id = $1 AND deleted_at IS NULL FOR SHARE;

-- name: ShareLocalCredential :many
-- The credential row, when the account has one, FOR SHARE: a password
-- replacement holding it FOR UPDATE either finishes first (and the issuance
-- then sees the grant revoked) or waits and revokes what was issued.
SELECT user_id FROM local_credentials WHERE user_id = $1 FOR SHARE;

-- name: FindMCPClient :one
-- FOR KEY SHARE: an issuance that found the client keeps it until commit, so the
-- sweep's FOR UPDATE delete skips it rather than orphaning a code being inserted.
-- KEY SHARE does not block other registrations or the referencing-row inserts.
SELECT client_id, client_name, redirect_uris FROM mcp_clients WHERE client_id = $1 FOR KEY SHARE;

-- name: CreateMCPAuthorizationCode :exec
INSERT INTO mcp_authorization_codes(code_hash, user_id, client_id, redirect_uri, code_challenge, scopes, resource, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: FindMCPAuthorizationCodeOwner :one
-- The unlocked pre-read: the account is locked before the code row.
SELECT user_id FROM mcp_authorization_codes WHERE code_hash = $1;

-- name: LockMCPAuthorizationCode :one
SELECT code_hash, user_id, client_id, redirect_uri, code_challenge, scopes, resource,
  expires_at, used_at, issued_grant_id
FROM mcp_authorization_codes WHERE code_hash = $1 FOR UPDATE;

-- name: MarkMCPAuthorizationCodeUsed :execrows
-- Clamped to created_at against clock skew between instances.
UPDATE mcp_authorization_codes SET used_at = GREATEST(sqlc.arg(used_at)::timestamptz, created_at), issued_grant_id = sqlc.arg(issued_grant_id)
WHERE code_hash = sqlc.arg(code_hash) AND used_at IS NULL;

-- name: DeleteMCPAuthorizationCode :exec
DELETE FROM mcp_authorization_codes WHERE code_hash = $1;

-- name: CreateMCPGrant :one
INSERT INTO mcp_grants(user_id, client_id, scopes, resource, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;

-- name: RecordMCPGrantCreated :exec
INSERT INTO mcp_grant_events(grant_id, user_id, client_id, client_name, event, actor_user_id, occurred_at)
SELECT g.id, g.user_id, g.client_id, c.client_name, 'created', g.user_id, g.created_at
FROM mcp_grants g JOIN mcp_clients c ON c.client_id = g.client_id WHERE g.id = $1;

-- name: CreateMCPAccessToken :exec
INSERT INTO mcp_access_tokens(token_hash, grant_id, created_at, expires_at) VALUES ($1, $2, $3, $4);

-- name: CreateMCPRefreshToken :exec
INSERT INTO mcp_refresh_tokens(token_hash, grant_id, created_at, expires_at) VALUES ($1, $2, $3, $4);

-- name: FindMCPGrantOwner :one
-- The unlocked pre-read for a user's own grant revocation: the account is
-- locked before the grant row. An unknown grant and a foreign grant both answer
-- no rows, so the caller learns nothing about another account's grants.
SELECT user_id FROM mcp_grants WHERE id = $1;

-- name: ListLiveMCPGrants :many
-- A plain read for the connected-apps list: live means not revoked and not
-- expired. Keyset pagination on id (UUIDv7, creation order), newest first. A
-- null after_id is the first page.
SELECT g.id, c.client_name, g.scopes, g.created_at, g.expires_at
FROM mcp_grants g JOIN mcp_clients c ON c.client_id = g.client_id
WHERE g.user_id = sqlc.arg(user_id)
  AND g.revoked_at IS NULL
  AND g.expires_at > sqlc.arg(now)::timestamptz
  AND (sqlc.narg(after_id)::uuid IS NULL OR g.id < sqlc.narg(after_id)::uuid)
ORDER BY g.id DESC
LIMIT sqlc.arg(page_limit)::int;

-- name: FindMCPRefreshTokenOwner :one
-- The unlocked pre-read: the account is locked before the grant and token rows.
-- A refresh token never moves to another grant, so grant_id is stable.
SELECT g.user_id, g.id AS grant_id FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id = r.grant_id WHERE r.token_hash = $1;

-- name: LockMCPGrantCodes :exec
-- The used codes that issued the grant, FOR UPDATE, before the grant row: the
-- ADR 0023 order puts codes before grants, and revoking a grant deletes them.
SELECT code_hash FROM mcp_authorization_codes WHERE issued_grant_id = $1 ORDER BY code_hash FOR UPDATE;

-- name: LockMCPGrant :one
SELECT id, user_id, client_id, scopes, resource, expires_at, revoked_at
FROM mcp_grants WHERE id = $1 FOR UPDATE;

-- name: LockMCPRefreshToken :one
SELECT token_hash, grant_id, expires_at, consumed_at FROM mcp_refresh_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: ConsumeMCPRefreshToken :execrows
-- Clamped to created_at against clock skew between instances.
UPDATE mcp_refresh_tokens SET consumed_at = GREATEST(sqlc.arg(consumed_at)::timestamptz, created_at)
WHERE token_hash = sqlc.arg(token_hash) AND consumed_at IS NULL;

-- name: DeleteMCPGrantCodes :exec
-- Used codes that issued the grant: once it is revoked a replayed code finds
-- nothing and fails like any unknown code.
DELETE FROM mcp_authorization_codes WHERE issued_grant_id = $1;

-- name: RevokeMCPGrant :execrows
-- revoked_at is clamped to created_at exactly as in RevokeAccountMCPGrants: a
-- concurrent code-reuse loser read its clock before the winner created the
-- grant, and that revocation must still commit.
WITH revoked AS (
  UPDATE mcp_grants
  SET revoked_at = GREATEST(sqlc.arg(revoked_at)::timestamptz, mcp_grants.created_at),
      revoke_reason = sqlc.arg(revoke_reason)
  WHERE mcp_grants.id = sqlc.arg(grant_id) AND mcp_grants.revoked_at IS NULL
  RETURNING mcp_grants.id, mcp_grants.user_id, mcp_grants.client_id, mcp_grants.revoked_at
)
INSERT INTO mcp_grant_events(grant_id, user_id, client_id, client_name, event, reason, actor_user_id, occurred_at)
SELECT r.id, r.user_id, r.client_id, c.client_name, 'revoked', sqlc.arg(revoke_reason), sqlc.narg(actor_user_id)::uuid, r.revoked_at
FROM revoked r JOIN mcp_clients c ON c.client_id = r.client_id;

-- name: DeleteMCPGrantAccessTokens :exec
DELETE FROM mcp_access_tokens WHERE grant_id = $1;

-- name: DeleteMCPGrantRefreshTokens :exec
DELETE FROM mcp_refresh_tokens WHERE grant_id = $1;

-- name: FindActiveMCPAccessToken :one
-- Verification re-reads the grant, the account and the MCP policy (#457) on
-- every request, so a revocation, deletion or policy change takes effect on the
-- next request. A missing policy row comes back as NULL columns and evaluates
-- as inactive.
SELECT g.id, g.user_id, g.client_id, g.scopes, g.resource,
  i.enabled AS installation_enabled, i.default_account_enabled, i.default_max_scopes,
  p.user_id AS policy_user_id, p.admin_enabled, p.admin_max_scopes, p.user_enabled, p.user_scopes
FROM mcp_access_tokens a
JOIN mcp_grants g ON g.id = a.grant_id
JOIN user_accounts u ON u.id = g.user_id
LEFT JOIN mcp_installation_policy i ON i.singleton
LEFT JOIN mcp_account_policies p ON p.user_id = g.user_id
WHERE a.token_hash = $1 AND a.expires_at > $2
  AND g.revoked_at IS NULL AND g.expires_at > $2 AND u.deleted_at IS NULL;

-- name: ReadMCPPolicy :one
-- The installation and account policy (#457) as stored. Issuance reads it after
-- the account lock and takes no policy lock: a change that commits meanwhile is
-- enforced by the next verification and refresh, which re-read it. A missing
-- row comes back as NULL columns and evaluates as inactive.
SELECT i.enabled AS installation_enabled, i.default_account_enabled, i.default_max_scopes,
  p.user_id AS policy_user_id, p.admin_enabled, p.admin_max_scopes, p.user_enabled, p.user_scopes
FROM (SELECT sqlc.arg(user_id)::uuid AS user_id) a
LEFT JOIN mcp_installation_policy i ON i.singleton
LEFT JOIN mcp_account_policies p ON p.user_id = a.user_id;

-- name: CreateMCPClientWithinCap :execrows
-- One statement: the grantless-client count and the insert are not separate
-- round trips. Concurrent registrations may each see room under the cap, so the
-- cap is soft (ADR 0023).
INSERT INTO mcp_clients(client_id, client_name, redirect_uris, created_at)
SELECT sqlc.arg(client_id)::text, sqlc.arg(client_name)::text, sqlc.arg(redirect_uris)::text[], sqlc.arg(created_at)::timestamptz
WHERE (
  SELECT count(*) FROM mcp_clients c
  WHERE NOT EXISTS (SELECT 1 FROM mcp_grants g WHERE g.client_id = c.client_id)
) < sqlc.arg(client_cap)::bigint;

-- name: SweepMCPAccessTokensExpired :execrows
-- One batch by primary key. The sweep loops while a batch comes back full.
DELETE FROM mcp_access_tokens WHERE token_hash IN (
  SELECT token_hash FROM mcp_access_tokens WHERE expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY token_hash LIMIT 1000
);

-- name: SweepMCPRefreshTokensExpired :execrows
-- Unconsumed digests only: a consumed digest is kept until its grant ends, so a
-- replayed refresh token is still detected.
DELETE FROM mcp_refresh_tokens WHERE token_hash IN (
  SELECT token_hash FROM mcp_refresh_tokens
  WHERE consumed_at IS NULL AND expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY token_hash LIMIT 1000
);

-- name: SweepMCPEndedGrantCodes :execrows
-- Codes that issued a grant past its expiry. Expiry is not revocation: the grant
-- row stays and no event is written.
DELETE FROM mcp_authorization_codes WHERE code_hash IN (
  SELECT c.code_hash FROM mcp_authorization_codes c JOIN mcp_grants g ON g.id = c.issued_grant_id
  WHERE g.expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY c.code_hash LIMIT 1000
);

-- name: SweepMCPEndedGrantAccessTokens :execrows
DELETE FROM mcp_access_tokens WHERE token_hash IN (
  SELECT a.token_hash FROM mcp_access_tokens a JOIN mcp_grants g ON g.id = a.grant_id
  WHERE g.expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY a.token_hash LIMIT 1000
);

-- name: SweepMCPEndedGrantRefreshTokens :execrows
DELETE FROM mcp_refresh_tokens WHERE token_hash IN (
  SELECT r.token_hash FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id = r.grant_id
  WHERE g.expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY r.token_hash LIMIT 1000
);

-- name: SweepMCPUnusedCodesExpired :execrows
DELETE FROM mcp_authorization_codes WHERE code_hash IN (
  SELECT code_hash FROM mcp_authorization_codes
  WHERE used_at IS NULL AND expires_at <= sqlc.arg(cutoff)::timestamptz ORDER BY code_hash LIMIT 1000
);

-- name: SweepMCPGrantlessClients :execrows
-- A client is kept while any grant or authorization code references it. The
-- SKIP LOCKED skips a client an issuance holds (its FOR KEY SHARE from
-- FindMCPClient conflicts with FOR UPDATE), so the sweep never orphans a code
-- being inserted. The set of referencing tables is pinned by a test.
DELETE FROM mcp_clients WHERE client_id IN (
  SELECT c.client_id FROM mcp_clients c
  WHERE c.created_at <= sqlc.arg(cutoff)::timestamptz
    AND NOT EXISTS (SELECT 1 FROM mcp_grants g WHERE g.client_id = c.client_id)
    AND NOT EXISTS (SELECT 1 FROM mcp_authorization_codes k WHERE k.client_id = c.client_id)
    AND NOT EXISTS (SELECT 1 FROM mcp_authorization_requests q WHERE q.client_id = c.client_id)
  ORDER BY c.client_id LIMIT 1000 FOR UPDATE SKIP LOCKED
);
