-- +goose Up
-- V5-09: explicit installation-wide administrator capability (ADR 0016).
--
-- Instance administrator is independent of household roles. A row is created
-- only by the setup transaction, by the one-time evidence rule below, or by the
-- `tendo instance-admin grant` command run inside the app container. It is
-- never created by invitations, OIDC claims, household role edits or the API.
--
-- The runtime role gets SELECT and INSERT only. There is no revoke path yet:
-- the admin center (#454) must add an append-only grant/revoke audit and lock
-- the instance-admin set before any DELETE exists. ON DELETE RESTRICT keeps
-- account deletion from silently removing the last instance administrator.
CREATE TABLE instance_admins (
 user_id uuid PRIMARY KEY REFERENCES user_accounts(id) ON DELETE RESTRICT,
 granted_at timestamptz NOT NULL,
 grant_source text NOT NULL CHECK (grant_source IN ('setup','v4_bootstrap_migration','cli'))
);
REVOKE ALL ON instance_admins FROM PUBLIC;
GRANT SELECT, INSERT ON instance_admins TO tendo;

-- One-time conversion of the released v1-v4 bootstrap authority. Those
-- releases never recorded which account ran setup, but setup was the only code
-- path that ever created a household, and it inserted the account, its local
-- credential and the household in ONE transaction, where every created_at
-- defaults to now() (the transaction start time). Accounts created by
-- invitation acceptance or OIDC sign-in come from other transactions.
--
-- The setup account is therefore granted only when ALL of these hold:
--   * setup is complete;
--   * exactly one household exists;
--   * exactly one account was created at that household's created_at;
--   * that account has a local credential created at the same instant that
--     was never replaced (credential_version 1: the only path that rewrites a
--     password, the v4 admin reset, increments it, so a co-admin who reset the
--     setup account's password cannot inherit the installation authority);
--   * that account is still an admin member of that household.
-- Anything else is ambiguous and grants NOBODY. The upgrade still succeeds and
-- the operator uses the documented recovery command. No household admin,
-- earliest admin or default-household holder is ever granted by assumption.
WITH bootstrap_household AS (
 SELECT h.id, h.created_at
 FROM households h
 WHERE (SELECT count(*) FROM households) = 1
), setup_account AS (
 SELECT u.id
 FROM user_accounts u
 JOIN bootstrap_household h ON u.created_at = h.created_at
 WHERE (SELECT count(*) FROM user_accounts a JOIN bootstrap_household b ON a.created_at = b.created_at) = 1
)
INSERT INTO instance_admins (user_id, granted_at, grant_source)
SELECT s.id, now(), 'v4_bootstrap_migration'
FROM setup_account s
JOIN bootstrap_household h ON true
JOIN local_credentials c ON c.user_id = s.id AND c.created_at = h.created_at AND c.credential_version = 1
JOIN household_memberships m ON m.user_id = s.id AND m.household_id = h.id AND m.role = 'admin'
WHERE NOT (SELECT setup_required FROM installation_state WHERE singleton);

-- +goose Down
-- Lossy by necessity: this drops every instance-administrator grant, including
-- grants made through the recovery command. Upgrading again re-derives only the
-- bootstrap grant from unchanged evidence; restore a backup or re-run the
-- recovery command for anything else.
REVOKE ALL ON instance_admins FROM tendo;
DROP TABLE instance_admins;
