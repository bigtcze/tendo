-- +goose Up
ALTER TABLE item_completions ADD COLUMN undone_at timestamptz NULL;
ALTER TABLE item_completions ADD COLUMN undone_by_user_id uuid NULL REFERENCES user_accounts(id);
ALTER TABLE item_completions ADD CONSTRAINT item_completions_undo_actor_pair_check CHECK ((undone_at IS NULL) = (undone_by_user_id IS NULL));
GRANT UPDATE (undone_at, undone_by_user_id) ON item_completions TO tendo;

-- +goose Down
REVOKE UPDATE (undone_at, undone_by_user_id) ON item_completions FROM tendo;
ALTER TABLE item_completions DROP CONSTRAINT item_completions_undo_actor_pair_check;
ALTER TABLE item_completions DROP COLUMN undone_by_user_id;
ALTER TABLE item_completions DROP COLUMN undone_at;
