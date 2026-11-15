-- +goose Up
-- V5-10: global account deletion by an instance administrator (ADR 0022).
--
-- An account is never removed as a row: completion and undo history, item and
-- subject creators, invitations, reset-link issuers and every membership audit
-- table reference user_accounts without ON DELETE, and that history must keep
-- its stable identifiers. Deletion therefore anonymizes the row in place:
-- login becomes the tombstone '~deleted-<32 hex of id>', which no login
-- pattern accepts, email facts and the default household are cleared, and
-- deleted_at is set. Credentials, sessions, OIDC links and grants are deleted.
ALTER TABLE user_accounts
 ADD COLUMN deleted_at timestamptz NULL,
 ADD CONSTRAINT user_accounts_deleted_tombstone CHECK (
   (deleted_at IS NULL) = (left(login, 9) <> '~deleted-')
   AND (deleted_at IS NULL OR (email IS NULL AND default_household_id IS NULL))
 );

-- The login stays unique among live accounts only, so a deleted account frees
-- its login for reuse. A partial unique index is not a key column for row
-- locking, so anonymizing the login keeps the row lock at FOR NO KEY UPDATE
-- (ADR 0019 global lock order: user_accounts is never locked FOR UPDATE).
-- The index keeps the constraint's name, which the adapters map to a login
-- conflict. Tombstones are unique by construction (they embed the id).
ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_login_key;
CREATE UNIQUE INDEX user_accounts_login_key ON user_accounts(login) WHERE deleted_at IS NULL;

-- Minimal append-only record of who deleted which account and when. No login,
-- email or household content is copied.
CREATE TABLE account_deletions (
 user_id uuid PRIMARY KEY REFERENCES user_accounts(id),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 deleted_at timestamptz NOT NULL,
 CHECK (user_id <> actor_user_id)
);
REVOKE ALL ON account_deletions FROM PUBLIC;
GRANT SELECT, INSERT ON account_deletions TO tendo;

-- Append-only revoke audit required by ADR 0016 before the runtime role may
-- delete an instance-administrator grant. The revoked grant's source and time
-- move here; instance_admins plus this table is the complete grant history.
CREATE TABLE instance_admin_revocations (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 user_id uuid NOT NULL REFERENCES user_accounts(id),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 grant_source text NOT NULL CHECK (grant_source IN ('setup','v4_bootstrap_migration','cli')),
 granted_at timestamptz NOT NULL,
 revoked_at timestamptz NOT NULL,
 reason text NOT NULL CHECK (reason IN ('account_deletion'))
);
REVOKE ALL ON instance_admin_revocations FROM PUBLIC;
GRANT SELECT, INSERT ON instance_admin_revocations TO tendo;

GRANT DELETE ON instance_admins TO tendo;
GRANT DELETE ON oidc_identities TO tendo;
-- user_accounts already carries table-level UPDATE for tendo (001_setup.sql).

-- +goose Down
-- Refuses while any account has been deleted: earlier releases do not know the
-- tombstone and would list it as a live, assignable account. Restore a backup
-- taken before the first deletion instead.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM user_accounts WHERE deleted_at IS NOT NULL) OR EXISTS (SELECT 1 FROM account_deletions) THEN
  RAISE EXCEPTION 'migration 027 cannot be reversed after an account was deleted; restore a backup instead';
 END IF;
END $$;
REVOKE DELETE ON oidc_identities FROM tendo;
REVOKE DELETE ON instance_admins FROM tendo;
DROP TABLE instance_admin_revocations;
DROP TABLE account_deletions;
DROP INDEX user_accounts_login_key;
ALTER TABLE user_accounts ADD CONSTRAINT user_accounts_login_key UNIQUE (login);
ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_deleted_tombstone, DROP COLUMN deleted_at;
