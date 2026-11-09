-- +goose Up
ALTER TABLE household_memberships DROP CONSTRAINT household_memberships_role_check;
UPDATE household_memberships SET role='admin' WHERE role='owner';
ALTER TABLE household_memberships ADD CONSTRAINT household_memberships_role_check CHECK (role IN ('admin','member'));
ALTER TABLE household_memberships ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version >= 1);

CREATE TABLE household_role_changes (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 target_user_id uuid NOT NULL REFERENCES user_accounts(id),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 previous_role text NOT NULL CHECK (previous_role IN ('admin','member')),
 new_role text NOT NULL CHECK (new_role IN ('admin','member')),
 changed_at timestamptz NOT NULL,
 CHECK (previous_role <> new_role)
);
CREATE INDEX household_role_changes_household_changed_at_idx ON household_role_changes(household_id, changed_at);
REVOKE ALL ON household_role_changes FROM PUBLIC;
GRANT SELECT, INSERT ON household_role_changes TO tendo;

-- A household with at least one membership must keep an administrator; otherwise
-- nobody could ever manage its members or invitations again. Households without
-- any membership are unreachable by the application, so no synthetic admin is
-- invented for them.
DO $$
BEGIN
 IF EXISTS (
   SELECT 1 FROM households h
   WHERE EXISTS (SELECT 1 FROM household_memberships m WHERE m.household_id = h.id)
     AND NOT EXISTS (SELECT 1 FROM household_memberships m WHERE m.household_id = h.id AND m.role = 'admin')
 ) THEN
   RAISE EXCEPTION 'household with memberships has no admin after role migration';
 END IF;
END
$$;

-- +goose Down
REVOKE ALL ON household_role_changes FROM tendo;
DROP TABLE household_role_changes;
ALTER TABLE household_memberships DROP COLUMN version;
-- The admin|member CHECK still holds admin rows, so it must be removed before
-- admin is mapped back to owner; otherwise this Down fails on any populated DB.
ALTER TABLE household_memberships DROP CONSTRAINT household_memberships_role_check;
UPDATE household_memberships SET role='owner' WHERE role='admin';
ALTER TABLE household_memberships ADD CONSTRAINT household_memberships_role_check CHECK (role IN ('owner','member'));
