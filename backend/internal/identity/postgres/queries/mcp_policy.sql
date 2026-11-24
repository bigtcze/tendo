-- name: ShareInstallationForMCPPolicy :one
-- FOR SHARE, the first lock of every instance-administrator policy command
-- (ADR 0020, ADR 0023 "Slice B1b1"): a future instance-admin grant revoke takes
-- it FOR UPDATE and serializes with in-flight commands.
SELECT setup_required FROM installation_state WHERE singleton = true FOR SHARE;

-- name: IsMCPPolicyInstanceAdmin :one
-- Only a grant whose account is still live confers authority.
SELECT EXISTS (
  SELECT 1 FROM instance_admins g JOIN user_accounts u ON u.id = g.user_id
  WHERE g.user_id = $1 AND u.deleted_at IS NULL
) AS granted;

-- name: ReadLiveMCPAccountPolicy :one
-- A live account's stored policy without a lock. No row means the account is
-- unknown or deleted. A NULL policy column means the account row is missing; a
-- NULL installation column means the installation row is missing. Both
-- evaluate as inactive.
SELECT i.enabled AS installation_enabled, i.default_account_enabled, i.default_max_scopes,
  p.user_id AS policy_user_id, p.admin_enabled, p.admin_max_scopes, p.user_enabled, p.user_scopes,
  p.version AS policy_version
FROM user_accounts u
LEFT JOIN mcp_installation_policy i ON i.singleton
LEFT JOIN mcp_account_policies p ON p.user_id = u.id
WHERE u.id = $1 AND u.deleted_at IS NULL;

-- name: EnsureMCPAccountPolicy :execrows
-- Repairs a missing account row with the defaults the migration 032 trigger
-- inserts. It affects one row only when the row was missing; the caller then
-- always writes the row, which sets updated_at from the service clock.
INSERT INTO mcp_account_policies(user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING;

-- name: LockMCPAccountPolicy :one
-- The account row FOR UPDATE, after the live account lock (ADR 0023 lock
-- order). Administrator and owner writes of one account serialize here.
SELECT admin_enabled, admin_max_scopes, user_enabled, user_scopes, version
FROM mcp_account_policies WHERE user_id = $1 FOR UPDATE;

-- name: UpdateMCPAccountPolicy :execrows
-- The caller holds the row FOR UPDATE; the version guard makes a lost lock a
-- zero-row update, which the caller refuses, instead of a silent overwrite.
UPDATE mcp_account_policies SET
  admin_enabled = sqlc.narg(admin_enabled)::boolean,
  admin_max_scopes = sqlc.narg(admin_max_scopes)::text[],
  user_enabled = sqlc.arg(user_enabled)::boolean,
  user_scopes = sqlc.arg(user_scopes)::text[],
  version = sqlc.arg(version)::bigint,
  updated_at = sqlc.arg(updated_at)::timestamptz
WHERE user_id = sqlc.arg(user_id)::uuid AND version = sqlc.arg(version)::bigint - 1;

-- name: AppendMCPPolicyEvent :exec
-- Append-only. The target is NULL for the installation level, which also sets
-- default_account_enabled; the table checks keep the two shapes apart.
INSERT INTO mcp_policy_events(actor_user_id, target_user_id, policy, enabled, default_account_enabled, scopes, occurred_at)
VALUES (sqlc.arg(actor_user_id)::uuid, sqlc.narg(target_user_id)::uuid, sqlc.arg(policy)::text,
  sqlc.narg(enabled)::boolean, sqlc.narg(default_account_enabled)::boolean, sqlc.narg(scopes)::text[],
  sqlc.arg(occurred_at)::timestamptz);

-- name: ReadMCPInstallationPolicy :one
-- The installation row without a lock. No row is a missing installation, which
-- the caller refuses rather than defaulting.
SELECT enabled, default_account_enabled, default_max_scopes, version
FROM mcp_installation_policy WHERE singleton;

-- name: ReadMCPInstallationSwitch :one
-- The installation switch alone, read unlocked on every MCP route.
SELECT enabled FROM mcp_installation_policy WHERE singleton;

-- name: LockMCPInstallationPolicy :one
-- The installation row FOR UPDATE, after the instance-admin FOR SHARE lock.
SELECT enabled, default_account_enabled, default_max_scopes, version
FROM mcp_installation_policy WHERE singleton FOR UPDATE;

-- name: UpdateMCPInstallationPolicy :execrows
-- The caller holds the row FOR UPDATE. The version guard turns a lost lock into
-- a zero-row update, which the caller refuses.
UPDATE mcp_installation_policy SET
  enabled = sqlc.arg(enabled)::boolean,
  default_account_enabled = sqlc.arg(default_account_enabled)::boolean,
  default_max_scopes = sqlc.arg(default_max_scopes)::text[],
  version = sqlc.arg(version)::bigint,
  updated_at = sqlc.arg(updated_at)::timestamptz
WHERE singleton AND version = sqlc.arg(version)::bigint - 1;

-- name: ListMCPPolicyEvents :many
-- The audit list across all levels, newest first by uuidv7 id. A null after_id
-- is the first page.
SELECT id, policy, actor_user_id, target_user_id, enabled, default_account_enabled, scopes, occurred_at
FROM mcp_policy_events
WHERE (sqlc.narg(after_id)::uuid IS NULL OR id < sqlc.narg(after_id)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(page_limit)::int;
