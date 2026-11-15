-- +goose Up
-- V5-10: append-only audit of household membership removals (ADR 0019).
--
-- A removal takes effects that are not visible in household_memberships alone:
-- outstanding invitations and reset links are revoked, item responsibility is
-- released, the member person is unlinked and the account default may change.
-- This row records who removed whom and which role the removed membership had,
-- so the fact survives the deleted membership without keeping unnecessary
-- household content.
CREATE TABLE household_member_removals (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 target_user_id uuid NOT NULL REFERENCES user_accounts(id),
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 previous_role text NOT NULL CHECK (previous_role IN ('admin','member')),
 removed_at timestamptz NOT NULL
);
CREATE INDEX household_member_removals_household_removed_at_idx
 ON household_member_removals(household_id, removed_at);
REVOKE ALL ON household_member_removals FROM PUBLIC;
GRANT SELECT, INSERT ON household_member_removals TO tendo;

-- +goose Down
-- Lossy by necessity: this drops the audit rows. The memberships, item
-- responsibility releases and invitation/reset revocations performed by a
-- removal stay in effect; restoring the exact pre-removal state requires a
-- database backup.
DROP TABLE household_member_removals;
