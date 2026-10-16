-- +goose Up
ALTER TABLE subjects ADD COLUMN linked_user_id uuid NULL;
ALTER TABLE subjects ADD CONSTRAINT subjects_linked_membership_fk FOREIGN KEY (linked_user_id, household_id) REFERENCES household_memberships(user_id, household_id) ON DELETE NO ACTION;
ALTER TABLE subjects ADD CONSTRAINT subjects_linked_user_person_only CHECK (linked_user_id IS NULL OR type = 'person');
CREATE INDEX subjects_linked_membership_idx ON subjects(linked_user_id, household_id) WHERE linked_user_id IS NOT NULL;
GRANT UPDATE (linked_user_id) ON subjects TO tendo;

-- +goose Down
REVOKE UPDATE (linked_user_id) ON subjects FROM tendo;
DROP INDEX subjects_linked_membership_idx;
ALTER TABLE subjects DROP CONSTRAINT subjects_linked_user_person_only;
ALTER TABLE subjects DROP CONSTRAINT subjects_linked_membership_fk;
ALTER TABLE subjects DROP COLUMN linked_user_id;
