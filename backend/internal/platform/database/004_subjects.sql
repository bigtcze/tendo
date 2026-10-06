-- +goose Up
CREATE TABLE subjects (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 type text NOT NULL CHECK (type IN ('person','home','vehicle','pet','custom')),
 name varchar(100) NOT NULL CHECK (name ~ '\S'),
 archived boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 version bigint NOT NULL DEFAULT 1 CHECK (version >= 1)
);
CREATE INDEX subjects_household_archived_id_idx ON subjects(household_id, archived, id);
REVOKE ALL ON subjects FROM PUBLIC;
GRANT SELECT, INSERT ON subjects TO tendo;
GRANT UPDATE (name, type, archived, version, updated_at) ON subjects TO tendo;

-- +goose Down
REVOKE ALL ON subjects FROM tendo;
DROP TABLE subjects;
