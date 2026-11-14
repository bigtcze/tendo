-- +goose Up
-- V5-08: record how each session was established (ADR 0018).
--
-- The local password change flow accepts only a session created by a local
-- password sign-in. A session established through OIDC must not replace the
-- local password, even when the account also has one: the OIDC identity is a
-- different credential, and the browser never proved knowledge of the local
-- password to Tendo.
--
-- Rows that predate this migration cannot be classified, so they become
-- 'legacy'. That is fail closed: a session that may or may not have come from
-- a password sign-in must not authorize a password change, and the person
-- signs in again to obtain a classified session. New code never writes
-- 'legacy'.
--
-- The default only backfills existing rows and is dropped immediately, so
-- every later insert must name the method explicitly; without the default a
-- forgotten column is a failed insert instead of a silently mislabelled
-- session. The runtime role keeps its table-level SELECT, INSERT and DELETE on
-- user_sessions, which covers the new column; there is still no UPDATE grant.
ALTER TABLE user_sessions ADD COLUMN sign_in_method text NOT NULL DEFAULT 'legacy'
  CHECK (sign_in_method IN ('password', 'oidc', 'legacy'));
ALTER TABLE user_sessions ALTER COLUMN sign_in_method DROP DEFAULT;

-- +goose Down
-- Lossy by necessity: the recorded kind of every existing session is dropped.
-- Sessions stay valid, but the release that reads this column is also the one
-- that offers local password change, so a downgrade removes that endpoint
-- rather than re-permitting it. Restoring an upgraded backup re-derives
-- 'legacy' for the same sessions, so nobody gains a password-change right by
-- downgrading and upgrading.
ALTER TABLE user_sessions DROP COLUMN sign_in_method;
