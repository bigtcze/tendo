-- +goose Up
-- Briefly blocks writes to items while PostgreSQL builds the attention-order index.
CREATE INDEX items_household_archived_done_attention_id_idx
 ON items(household_id, archived, done, attention_on ASC NULLS FIRST, id ASC);

-- +goose Down
DROP INDEX items_household_archived_done_attention_id_idx;
