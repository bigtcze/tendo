-- +goose Up
ALTER TABLE items ADD COLUMN responsible_user_id uuid NULL;
ALTER TABLE items ADD CONSTRAINT items_responsible_membership_fk FOREIGN KEY (responsible_user_id, household_id) REFERENCES household_memberships(user_id, household_id) ON DELETE NO ACTION;
CREATE INDEX items_responsible_membership_idx ON items(responsible_user_id, household_id) WHERE responsible_user_id IS NOT NULL;
GRANT UPDATE (responsible_user_id) ON items TO tendo;

-- +goose Down
REVOKE UPDATE (responsible_user_id) ON items FROM tendo;
DROP INDEX items_responsible_membership_idx;
ALTER TABLE items DROP CONSTRAINT items_responsible_membership_fk;
ALTER TABLE items DROP COLUMN responsible_user_id;
