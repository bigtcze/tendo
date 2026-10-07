-- +goose Up
ALTER TABLE subjects ADD CONSTRAINT subjects_household_id_id_key UNIQUE (household_id, id);
CREATE TABLE items (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 subject_id uuid NOT NULL,
 title varchar(200) NOT NULL CHECK (title ~ '\S'),
 notes varchar(4000) NULL CHECK (notes IS NULL OR notes <> ''),
 attention_on date NULL,
 workflow_state text NOT NULL DEFAULT 'open' CHECK (workflow_state IN ('open','in_progress','waiting','paused')),
 archived boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
 FOREIGN KEY (household_id, subject_id) REFERENCES subjects(household_id, id)
);
CREATE INDEX items_household_archived_id_idx ON items(household_id, archived, id);
REVOKE ALL ON items FROM PUBLIC;
GRANT SELECT, INSERT ON items TO tendo;
GRANT UPDATE (subject_id, title, notes, attention_on, workflow_state, archived, version, updated_at) ON items TO tendo;

-- +goose Down
REVOKE ALL ON items FROM tendo;
DROP TABLE items;
ALTER TABLE subjects DROP CONSTRAINT subjects_household_id_id_key;
