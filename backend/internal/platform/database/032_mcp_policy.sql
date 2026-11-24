-- +goose Up
-- V5-13 slice B1a: MCP access policy (Issue #457, ADR 0023 "Slice B1a").
--
-- Effective MCP rights are evaluated on every request as
--   TENDO_MCP_ENABLED AND installation.enabled AND account enabled AND user
--   enabled AND (account maximum ∩ user scopes ∩ grant scopes)
-- with household membership enforced by the canonical services.
--
-- New tables only, plus the 'admin' revoke reason. No existing row changes. The
-- installation row is inserted here with the permissive defaults, so an upgrade
-- keeps every existing grant's effective scopes: TENDO_MCP_ENABLED stays the
-- operator's opt-in.
--
-- A missing installation or account row evaluates as inactive (fail closed),
-- never as a default. Every account therefore has exactly one account row: this
-- migration backfills one per existing account, and a trigger inserts one with
-- every new account, whichever path creates it. A fresh account row follows the
-- installation defaults (admin columns NULL) with unrestricted user settings.

-- The installation-wide policy. Exactly one row; the runtime role cannot insert
-- or delete it.
CREATE TABLE mcp_installation_policy (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 enabled boolean NOT NULL DEFAULT true,
 default_account_enabled boolean NOT NULL DEFAULT true,
 default_max_scopes text[] NOT NULL DEFAULT ARRAY['tendo:read','tendo:write']::text[] CHECK (
   cardinality(default_max_scopes) >= 1 AND default_max_scopes <@ ARRAY['tendo:read','tendo:write']::text[]
 ),
 version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
 updated_at timestamptz NULL
);
INSERT INTO mcp_installation_policy(singleton) VALUES (true);
REVOKE ALL ON mcp_installation_policy FROM PUBLIC;
GRANT SELECT ON mcp_installation_policy TO tendo;
GRANT UPDATE (enabled, default_account_enabled, default_max_scopes, version, updated_at) ON mcp_installation_policy TO tendo;

-- One account's policy. The admin columns are NULL while the account follows the
-- installation default. The user columns are the account owner's own settings;
-- they can only restrict, because the effective set is an intersection.
CREATE TABLE mcp_account_policies (
 user_id uuid PRIMARY KEY REFERENCES user_accounts(id) ON DELETE CASCADE,
 admin_enabled boolean NULL,
 admin_max_scopes text[] NULL CHECK (
   admin_max_scopes IS NULL OR (
     cardinality(admin_max_scopes) >= 1 AND admin_max_scopes <@ ARRAY['tendo:read','tendo:write']::text[]
   )
 ),
 user_enabled boolean NOT NULL DEFAULT true,
 user_scopes text[] NOT NULL DEFAULT ARRAY['tendo:read','tendo:write']::text[] CHECK (
   cardinality(user_scopes) >= 1 AND user_scopes <@ ARRAY['tendo:read','tendo:write']::text[]
 ),
 version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
 updated_at timestamptz NOT NULL DEFAULT now()
);
-- Accounts are tombstoned, never deleted, by the application. The cascade only
-- lets an operator's manual row removal take the policy with it.
INSERT INTO mcp_account_policies(user_id) SELECT id FROM user_accounts;
REVOKE ALL ON mcp_account_policies FROM PUBLIC;
GRANT SELECT, INSERT ON mcp_account_policies TO tendo;
GRANT UPDATE (admin_enabled, admin_max_scopes, user_enabled, user_scopes, version, updated_at) ON mcp_account_policies TO tendo;

-- Every new account gets its default account row in the same transaction.
CREATE FUNCTION public.tendo_mcp_account_policy_default() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
 INSERT INTO public.mcp_account_policies(user_id) VALUES (NEW.id);
 RETURN NULL;
END
$$;
REVOKE ALL ON FUNCTION public.tendo_mcp_account_policy_default() FROM PUBLIC;
CREATE TRIGGER user_accounts_mcp_account_policy AFTER INSERT ON user_accounts
FOR EACH ROW EXECUTE FUNCTION public.tendo_mcp_account_policy_default();

-- Append-only policy audit. Each row is the policy as it stood after one change
-- and the human who made it. No secret and no household content is stored.
--   installation: enabled, default_account_enabled and scopes (the default
--                 maximum) are all set; there is no target account.
--   account:      an instance administrator's change; NULL enabled or scopes
--                 means the account follows the installation default.
--   user:         the account owner's own settings; actor and target are the
--                 same account.
CREATE TABLE mcp_policy_events (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 target_user_id uuid NULL REFERENCES user_accounts(id),
 policy text NOT NULL CHECK (policy IN ('installation','account','user')),
 enabled boolean NULL,
 default_account_enabled boolean NULL,
 scopes text[] NULL CHECK (
   scopes IS NULL OR (cardinality(scopes) >= 1 AND scopes <@ ARRAY['tendo:read','tendo:write']::text[])
 ),
 occurred_at timestamptz NOT NULL,
 CHECK ((policy = 'installation') = (target_user_id IS NULL)),
 CHECK ((policy = 'installation') = (default_account_enabled IS NOT NULL)),
 CHECK (policy = 'account' OR (enabled IS NOT NULL AND scopes IS NOT NULL)),
 CHECK (policy <> 'user' OR actor_user_id = target_user_id)
);
CREATE INDEX mcp_policy_events_actor_user_id_idx ON mcp_policy_events(actor_user_id);
CREATE INDEX mcp_policy_events_target_user_id_idx ON mcp_policy_events(target_user_id);
CREATE INDEX mcp_policy_events_occurred_at_idx ON mcp_policy_events(occurred_at);
REVOKE ALL ON mcp_policy_events FROM PUBLIC;
GRANT SELECT, INSERT ON mcp_policy_events TO tendo;

-- An instance administrator's revocation of another account's grant. The
-- constraint names are the ones migration 030 set.
ALTER TABLE mcp_grants DROP CONSTRAINT mcp_grants_revoke_reason_check;
ALTER TABLE mcp_grants ADD CONSTRAINT mcp_grants_revoke_reason_check CHECK (revoke_reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client','admin'
 ));
ALTER TABLE mcp_grant_events DROP CONSTRAINT mcp_grant_events_reason_check;
ALTER TABLE mcp_grant_events ADD CONSTRAINT mcp_grant_events_reason_check CHECK (reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client','admin'
 ));

-- +goose Down
-- Refuses once the policy was used: an earlier release has no policy, so a
-- rollback would silently widen access, and it cannot record an administrator
-- revocation without falsifying its audited cause. Restore a backup taken
-- before the first policy change or administrator revocation instead.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM mcp_grants WHERE revoke_reason = 'admin')
   OR EXISTS (SELECT 1 FROM mcp_grant_events WHERE reason = 'admin')
   OR EXISTS (SELECT 1 FROM mcp_policy_events)
   OR EXISTS (SELECT 1 FROM user_accounts u WHERE u.deleted_at IS NULL
     AND NOT EXISTS (SELECT 1 FROM mcp_account_policies p WHERE p.user_id = u.id))
   OR EXISTS (SELECT 1 FROM mcp_account_policies
     WHERE admin_enabled IS NOT NULL OR admin_max_scopes IS NOT NULL OR NOT user_enabled
       OR NOT (user_scopes @> ARRAY['tendo:read','tendo:write']::text[]))
   OR NOT EXISTS (SELECT 1 FROM mcp_installation_policy
     WHERE enabled AND default_account_enabled
       AND default_max_scopes @> ARRAY['tendo:read','tendo:write']::text[])
 THEN
  RAISE EXCEPTION 'migration 032 cannot be reversed after the MCP policy was changed or an administrator revoked a grant; restore a backup instead';
 END IF;
END $$;
ALTER TABLE mcp_grant_events DROP CONSTRAINT mcp_grant_events_reason_check;
ALTER TABLE mcp_grant_events ADD CONSTRAINT mcp_grant_events_reason_check CHECK (reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client'
 ));
ALTER TABLE mcp_grants DROP CONSTRAINT mcp_grants_revoke_reason_check;
ALTER TABLE mcp_grants ADD CONSTRAINT mcp_grants_revoke_reason_check CHECK (revoke_reason IN (
   'user','password_change','admin_reset','cli_reset','account_deletion','replay','code_reuse','client'
 ));
DROP TRIGGER user_accounts_mcp_account_policy ON user_accounts;
DROP FUNCTION public.tendo_mcp_account_policy_default();
REVOKE ALL ON mcp_policy_events, mcp_account_policies, mcp_installation_policy FROM tendo;
DROP TABLE mcp_policy_events;
DROP TABLE mcp_account_policies;
DROP TABLE mcp_installation_policy;
