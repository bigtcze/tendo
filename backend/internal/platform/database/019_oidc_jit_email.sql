-- +goose Up
-- V4-10: verified account email and the server-side pending continuation for
-- OIDC just-in-time provisioning. Email is never inferred for existing rows.

ALTER TABLE user_accounts
 ADD COLUMN email text COLLATE "C" NULL,
 ADD COLUMN email_verified_at timestamptz NULL,
 ADD COLUMN email_source text NULL,
 -- The issuer whose verified claim (or confirmed local sign-in) proved the
 -- address. Row 2 only trusts an address proved by the *same* issuer.
 ADD COLUMN email_issuer text COLLATE "C" NULL,
 ADD CONSTRAINT user_accounts_email_source_check CHECK (email_source IN ('oidc_claim','account_confirmed')),
 ADD CONSTRAINT user_accounts_email_all_or_none CHECK (
   (email IS NULL AND email_verified_at IS NULL AND email_source IS NULL AND email_issuer IS NULL)
   OR (email IS NOT NULL AND email_verified_at IS NOT NULL AND email_source IS NOT NULL AND email_issuer IS NOT NULL)
 ),
 -- Structural invariants only: the canonical form is owned by the Go function.
 ADD CONSTRAINT user_accounts_email_canonical CHECK (
   email IS NULL OR (
     email = btrim(email)
     AND position('@' in email) > 1
     AND split_part(email,'@',2) = lower(split_part(email,'@',2))
     AND octet_length(email) <= 254
   )
 );

-- The unique index compares canonical strings byte-for-byte; a second account
-- can never claim an address another account proved first.
CREATE UNIQUE INDEX user_accounts_email_unique ON user_accounts(email) WHERE email IS NOT NULL;

-- Invitation carry-over: only the SHA-256 of a pending invitation token is
-- stored with the flow, and only for the login purpose.
ALTER TABLE oidc_flows
 ADD COLUMN invite_token_hash bytea NULL CHECK (octet_length(invite_token_hash)=32),
 ADD CONSTRAINT oidc_flows_invite_login_only CHECK (invite_token_hash IS NULL OR purpose='login');

-- A continuation that needs a further user step is stored server-side. The
-- browser only ever holds the random handle; the table stores its digest.
CREATE TABLE oidc_pending_logins (
 handle_hash bytea PRIMARY KEY CHECK (octet_length(handle_hash)=32),
 issuer text COLLATE "C" NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 2048),
 subject text COLLATE "C" NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 255 AND subject !~ '[^!-~]'),
 email text COLLATE "C" NULL CHECK (email IS NULL OR (
   email = btrim(email)
   AND position('@' in email) > 1
   AND split_part(email,'@',2) = lower(split_part(email,'@',2))
   AND octet_length(email) <= 254
 )),
 email_verified boolean NOT NULL DEFAULT false,
 client_id text NOT NULL,
 invite_token_hash bytea NULL CHECK (octet_length(invite_token_hash)=32),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes'),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 CHECK (NOT email_verified OR email IS NOT NULL)
);
CREATE INDEX oidc_pending_logins_expires_at_idx ON oidc_pending_logins(expires_at);
REVOKE ALL ON oidc_pending_logins FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON oidc_pending_logins TO tendo;
GRANT UPDATE (attempts) ON oidc_pending_logins TO tendo;

-- +goose Down
-- Lossy by necessity: this drops every recorded email address with its
-- verification provenance and all pending OIDC continuations. OIDC identities
-- (issuer, subject) are kept, so linked accounts still sign in, but automatic
-- verified-email attachment starts again from no recorded addresses. Restore a
-- pre-downgrade backup if the recorded emails matter.
REVOKE ALL ON oidc_pending_logins FROM tendo;
DROP TABLE oidc_pending_logins;
ALTER TABLE oidc_flows DROP CONSTRAINT oidc_flows_invite_login_only;
ALTER TABLE oidc_flows DROP COLUMN invite_token_hash;
DROP INDEX user_accounts_email_unique;
ALTER TABLE user_accounts
 DROP CONSTRAINT user_accounts_email_canonical,
 DROP CONSTRAINT user_accounts_email_all_or_none,
 DROP CONSTRAINT user_accounts_email_source_check,
 DROP COLUMN email_issuer,
 DROP COLUMN email_source,
 DROP COLUMN email_verified_at,
 DROP COLUMN email;
