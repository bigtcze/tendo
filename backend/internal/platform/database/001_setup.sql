-- +goose Up
CREATE TABLE installation_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 setup_required boolean NOT NULL DEFAULT true
);
INSERT INTO installation_state(singleton, setup_required) VALUES (true, true);
CREATE TABLE households (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 name varchar(100) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
 timezone varchar(128) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_accounts (
 id uuid PRIMARY KEY DEFAULT uuidv7(),
 login varchar(64) NOT NULL UNIQUE,
 default_household_id uuid,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE local_credentials (
 user_id uuid PRIMARY KEY REFERENCES user_accounts(id) ON DELETE CASCADE,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE household_memberships (
 user_id uuid NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
 household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
 role text NOT NULL CHECK (role IN ('owner','member')),
 PRIMARY KEY(user_id, household_id)
);
ALTER TABLE user_accounts ADD CONSTRAINT user_default_membership_fk FOREIGN KEY(id, default_household_id) REFERENCES household_memberships(user_id, household_id) DEFERRABLE INITIALLY DEFERRED;

DO $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tendo') THEN
  RAISE EXCEPTION 'required runtime role tendo does not exist';
 END IF;
END
$$;
REVOKE ALL ON installation_state, households, user_accounts, local_credentials, household_memberships FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO tendo;
GRANT SELECT ON installation_state TO tendo;
GRANT UPDATE (setup_required) ON installation_state TO tendo;
GRANT SELECT, INSERT, UPDATE, DELETE ON households, user_accounts, local_credentials, household_memberships TO tendo;
GRANT SELECT ON tendo_schema_migrations TO tendo;

-- +goose Down
REVOKE ALL ON installation_state, households, user_accounts, local_credentials, household_memberships FROM tendo;
ALTER TABLE user_accounts DROP CONSTRAINT user_default_membership_fk;
DROP TABLE household_memberships;
DROP TABLE local_credentials;
DROP TABLE user_accounts;
DROP TABLE households;
DROP TABLE installation_state;
