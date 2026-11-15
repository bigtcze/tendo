-- +goose Up
-- V5-10: instance-administrator household account center (ADR 0020).
--
-- An instance administrator may assign an existing account into any household
-- and change or remove memberships there without being a member. Every
-- membership audit row now records which grant the actor used, so an instance
-- administrator acting outside their own households stays distinguishable from
-- a household admin. Rows written before this migration were household-admin
-- role changes and household-admin or self-leave removals; the self-leave rows
-- are recognisable because the actor is the target.
ALTER TABLE household_role_changes
 ADD COLUMN actor_authority text NOT NULL DEFAULT 'household_admin'
 CHECK (actor_authority IN ('household_admin','household_member','instance_admin'));
ALTER TABLE household_member_removals
 ADD COLUMN actor_authority text NOT NULL DEFAULT 'household_admin'
 CHECK (actor_authority IN ('household_admin','household_member','instance_admin'));
UPDATE household_member_removals r SET actor_authority = 'household_member'
WHERE r.actor_user_id = r.target_user_id AND r.previous_role = 'member';

-- Append-only audit of instance-administrator assignments. Like removals and
-- role changes, the row survives the household content it does not copy.
CREATE TABLE household_member_assignments (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 target_user_id uuid NOT NULL REFERENCES user_accounts(id),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 role text NOT NULL CHECK (role IN ('admin','member')),
 actor_authority text NOT NULL CHECK (actor_authority IN ('instance_admin')),
 assigned_at timestamptz NOT NULL
);
CREATE INDEX household_member_assignments_household_assigned_at_idx
 ON household_member_assignments(household_id, assigned_at);
REVOKE ALL ON household_member_assignments FROM PUBLIC;
GRANT SELECT, INSERT ON household_member_assignments TO tendo;

-- The instance-admin account directory pages accounts by case-folded login.
CREATE INDEX user_accounts_lower_login_id_idx ON user_accounts(lower(login), id);

-- +goose Down
-- Lossy by necessity: this drops the assignment audit rows and the recorded
-- actor authority. Memberships created by an assignment stay in effect;
-- restoring the exact pre-assignment state requires a database backup.
DROP INDEX user_accounts_lower_login_id_idx;
DROP TABLE household_member_assignments;
ALTER TABLE household_member_removals DROP COLUMN actor_authority;
ALTER TABLE household_role_changes DROP COLUMN actor_authority;
