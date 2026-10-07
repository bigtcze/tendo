-- +goose Up
ALTER TABLE items ADD COLUMN done boolean NOT NULL DEFAULT false;
ALTER TABLE items ADD CONSTRAINT items_household_id_id_key UNIQUE (household_id, id);
DROP INDEX items_household_archived_id_idx;
CREATE INDEX items_household_archived_done_id_idx ON items(household_id, archived, done, id);
GRANT UPDATE (done) ON items TO tendo;

CREATE TABLE item_completions (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL,
 item_id uuid NOT NULL,
 completed_on date NOT NULL,
 completed_by_user_id uuid NOT NULL REFERENCES user_accounts(id),
 cycle_attention_on date NULL,
 recurrence_interval_value integer NULL CHECK (recurrence_interval_value BETWEEN 1 AND 999),
 recurrence_interval_unit text NULL CHECK (recurrence_interval_unit IN ('day','week','month','year')),
 recurrence_mode text NULL CHECK (recurrence_mode IN ('fixed','after_completion')),
 prior_workflow_state text NOT NULL CHECK (prior_workflow_state IN ('open','in_progress','waiting','paused')),
 next_attention_on date NULL,
 item_version_before bigint NOT NULL CHECK (item_version_before >= 1),
 idempotency_key varchar(128) NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key !~ '[^!-~]'),
 request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((recurrence_interval_value IS NULL AND recurrence_interval_unit IS NULL AND recurrence_mode IS NULL) OR (recurrence_interval_value IS NOT NULL AND recurrence_interval_unit IS NOT NULL AND recurrence_mode IS NOT NULL)),
 FOREIGN KEY (household_id, item_id) REFERENCES items(household_id, id) ON DELETE CASCADE,
 UNIQUE (item_id, idempotency_key),
 UNIQUE (item_id, item_version_before)
);
CREATE INDEX item_completions_household_item_id_idx ON item_completions(household_id, item_id, id);
REVOKE ALL ON item_completions FROM PUBLIC;
GRANT SELECT, INSERT ON item_completions TO tendo;

-- +goose Down
REVOKE ALL ON item_completions FROM tendo;
DROP TABLE item_completions;
REVOKE UPDATE (done) ON items FROM tendo;
DROP INDEX items_household_archived_done_id_idx;
CREATE INDEX items_household_archived_id_idx ON items(household_id, archived, id);
ALTER TABLE items DROP CONSTRAINT items_household_id_id_key;
ALTER TABLE items DROP COLUMN done;
