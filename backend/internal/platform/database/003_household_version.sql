-- +goose Up
ALTER TABLE households ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version >= 1);

-- +goose Down
ALTER TABLE households DROP COLUMN version;
