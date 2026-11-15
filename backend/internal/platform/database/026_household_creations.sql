-- +goose Up
-- V5-10: instance-administrator household creation (ADR 0021).
--
-- A household is never created empty: it is created together with its first
-- admin membership, so the "at least one admin" invariant holds from the first
-- instant. This table records which instance administrator created the
-- household and under which Idempotency-Key, so a retried create replays the
-- original household instead of creating a second one.
--
-- Keys are scoped to (actor, key) for the whole installation and kept for the
-- household's lifetime. The row is deleted with the household; the actor
-- reference is NO ACTION like the other membership audit tables, so account
-- deletion (slice C) must anonymize actor_user_id in place rather than cascade.
CREATE TABLE household_creations (
 household_id uuid PRIMARY KEY REFERENCES households(id) ON DELETE CASCADE,
 actor_user_id uuid NOT NULL REFERENCES user_accounts(id),
 creation_key text NOT NULL CHECK (octet_length(creation_key) BETWEEN 1 AND 128 AND creation_key !~ '[^!-~]'),
 request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
 created_at timestamptz NOT NULL,
 CONSTRAINT household_creations_actor_key_unique UNIQUE (actor_user_id, creation_key)
);
REVOKE ALL ON household_creations FROM PUBLIC;
GRANT SELECT, INSERT ON household_creations TO tendo;

-- +goose Down
-- Lossy by necessity: this drops the creation audit and replay records. The
-- households and their memberships stay in effect. A same-key retry after a
-- downgrade and re-upgrade creates a new household, because the replay key no
-- longer exists; restoring the exact records requires a database backup.
DROP TABLE household_creations;
