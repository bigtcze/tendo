-- +goose Up
ALTER TABLE subjects
 ADD COLUMN created_by_user_id uuid REFERENCES user_accounts(id),
 ADD COLUMN creation_key text,
 ADD COLUMN creation_request_fingerprint bytea,
 ADD CONSTRAINT subjects_creation_metadata_all_or_none CHECK (
  (created_by_user_id IS NULL AND creation_key IS NULL AND creation_request_fingerprint IS NULL)
  OR (created_by_user_id IS NOT NULL AND creation_key IS NOT NULL AND creation_request_fingerprint IS NOT NULL)
 ),
 ADD CONSTRAINT subjects_creation_key_valid CHECK (
  creation_key IS NULL OR (octet_length(creation_key) BETWEEN 1 AND 128 AND creation_key !~ '[^!-~]')
 ),
 ADD CONSTRAINT subjects_creation_fingerprint_length CHECK (
  creation_request_fingerprint IS NULL OR octet_length(creation_request_fingerprint) = 32
 );
ALTER TABLE subjects ADD CONSTRAINT subjects_household_creator_creation_key_unique
 UNIQUE (household_id, created_by_user_id, creation_key);

-- +goose Down
ALTER TABLE subjects DROP CONSTRAINT subjects_household_creator_creation_key_unique;
ALTER TABLE subjects
 DROP CONSTRAINT subjects_creation_fingerprint_length,
 DROP CONSTRAINT subjects_creation_key_valid,
 DROP CONSTRAINT subjects_creation_metadata_all_or_none,
 DROP COLUMN creation_request_fingerprint,
 DROP COLUMN creation_key,
 DROP COLUMN created_by_user_id;
