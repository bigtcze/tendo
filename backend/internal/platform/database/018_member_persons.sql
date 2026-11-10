-- +goose Up
-- Registered household members are person subjects. This migration makes the
-- existing descriptive subject link canonical: at most one linked person per
-- membership, created and kept in sync with the membership by the application.
-- Ordinary old-application writes are excluded while the preflight and backfill
-- run, so the invariant cannot be violated halfway through.
LOCK TABLE household_memberships, subjects IN SHARE ROW EXCLUSIVE MODE;

-- Lossless audit of non-canonical duplicate links. Rows are never deleted or
-- merged; only the descriptive linked_user_id is cleared. Administrator-only.
CREATE TABLE member_person_migration_unlinks (
 subject_id uuid PRIMARY KEY REFERENCES subjects(id) ON DELETE CASCADE,
 household_id uuid NOT NULL,
 user_id uuid NOT NULL,
 unlinked_at timestamptz NOT NULL DEFAULT now()
);
REVOKE ALL ON member_person_migration_unlinks FROM PUBLIC;

-- Canonical link per (household, member) is the earliest-created subject.
-- Every other linked duplicate is unlinked and recorded; names, creation
-- metadata and item associations (items.subject_id) are untouched.
WITH ranked AS (
 SELECT id, household_id, linked_user_id,
   row_number() OVER (PARTITION BY household_id, linked_user_id ORDER BY created_at, id) AS position
 FROM subjects
 WHERE linked_user_id IS NOT NULL
), duplicates AS (
 SELECT id, household_id, linked_user_id FROM ranked WHERE position > 1
), logged AS (
 INSERT INTO member_person_migration_unlinks(subject_id, household_id, user_id)
 SELECT id, household_id, linked_user_id FROM duplicates
)
UPDATE subjects s
SET linked_user_id = NULL, version = version + 1, updated_at = now()
FROM duplicates d
WHERE s.id = d.id;

-- Emit one notice per unlinked subject so an operator running the migration by
-- hand or through a client that surfaces PostgreSQL notices can inspect the
-- lossless unlink log without querying the audit table.
DO $$
DECLARE entry record;
BEGIN
 FOR entry IN SELECT subject_id, household_id, user_id FROM member_person_migration_unlinks LOOP
  RAISE NOTICE 'member person migration unlinked subject % in household % from user %',
   entry.subject_id, entry.household_id, entry.user_id;
 END LOOP;
END
$$;

-- A member person is always selectable while the membership exists.
WITH canonical AS (
 SELECT DISTINCT ON (household_id, linked_user_id) id
 FROM subjects
 WHERE linked_user_id IS NOT NULL
 ORDER BY household_id, linked_user_id, created_at, id
)
UPDATE subjects s
SET archived = false, version = version + 1, updated_at = now()
FROM canonical c
WHERE s.id = c.id AND s.archived;

-- Backfill exactly one person per membership that has no linked person yet.
-- The display name starts as the account login and remains freely editable;
-- names are never matched to claim an existing unlinked person.
INSERT INTO subjects (household_id, type, name, linked_user_id)
SELECT m.household_id, 'person', u.login, m.user_id
FROM household_memberships m
JOIN user_accounts u ON u.id = m.user_id
WHERE NOT EXISTS (
 SELECT 1 FROM subjects s
 WHERE s.household_id = m.household_id AND s.linked_user_id = m.user_id
);

DROP INDEX subjects_linked_membership_idx;
CREATE UNIQUE INDEX subjects_member_person_unique ON subjects(linked_user_id, household_id) WHERE linked_user_id IS NOT NULL;

-- +goose Down
-- Non-destructive by design: this reverses only the schema (the uniqueness
-- guarantee and the audit table). Provisioned person rows and unlinked
-- duplicates keep their current values; restoring the exact pre-migration
-- link values requires a database backup. The old non-unique index from
-- migration 014 is recreated exactly.
DROP INDEX subjects_member_person_unique;
CREATE INDEX subjects_linked_membership_idx ON subjects(linked_user_id, household_id) WHERE linked_user_id IS NOT NULL;
DROP TABLE member_person_migration_unlinks;
