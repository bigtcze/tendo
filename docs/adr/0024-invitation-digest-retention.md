# ADR 0024: Invitation digest retention

Status: accepted

Amends [ADR 0007](0007-invitation-admission.md). Slice B of Issue #455 (V5-11).

## Context

An invitation is a bearer secret. Tendo stores only its SHA-256 digest
(`household_invitations.token_hash`), but up to v4.0.0 the digest stayed on the
row for ever: after acceptance, after revocation (including the revocation that
comes with member removal and account deletion), and after expiry. Each of those
rows still bore a value that identifies a once-valid token, in the live
database and in every backup. ADR 0007 said there was no cleanup job.

V5 requires that cancelled or revoked invitations do not persist as
secret-bearing rows, that expired invitations are cleaned up by a bounded
policy that is safe against concurrent acceptance and revocation, that the v4
migration purges the existing secret-bearing rows, and that history is kept
without secrets.

The rows themselves cannot simply be deleted. The owner's invitation list shows
accepted and revoked invitations; an idempotent create retry must find the
original invitation by its key and answer `200` without a token instead of
creating a second one; account deletion (ADR 0022) and member removal
(ADR 0019) revoke pending invitations in place; and `created_by_user_id`
references the account row that must keep its history.

## Decision

- **The secret is purged, the row is the redacted record.** Only a pending
  invitation holds a digest. Acceptance and revocation set `token_hash` to
  `NULL` in the same statement that records the transition, so no terminal row
  ever holds a digest, also during a race. What stays is the minimal redacted
  record: ID, household, creator, idempotency key, and the created, expiry,
  accepted and revoked times. None of these can admit anyone. No row is deleted
  and no separate audit table is added.
- **Storage enforces it.** Migration 031 makes `token_hash` nullable and adds
  `household_invitations_terminal_no_digest`:
  `CHECK (token_hash IS NULL OR (accepted_at IS NULL AND revoked_at IS NULL))`.
  The runtime role gets `UPDATE (token_hash)`, and a trigger lets an update keep
  the digest or clear it but never set a new value (SQLSTATE `42501`), so no
  code path can revive or re-key an invitation. The runtime role still has no
  `DELETE` and cannot change household, creator, key or expiry. The `UNIQUE`
  constraint stays; `NULL` digests do not collide.
- **Expired digests are swept.** An expired invitation can never be accepted,
  so its digest serves no purpose. A retention sweep clears the digests of
  invitations whose `expires_at` is at or before now. It runs once at startup
  and then every 10 minutes, the same lifecycle as the MCP sweep (ADR 0023):
  each run is bounded by `TENDO_DB_TIMEOUT`, works in batches of at most 1000
  rows that each commit on their own with `lock_timeout` 100 ms, and loops
  while a batch comes back full. A partial index on `expires_at` where a digest
  is present keeps each batch cheap. Logs carry only the cleared count, a
  `locked` flag and the fixed fault classes `timeout`, `canceled`,
  `step_failed` and `unknown`, never driver text. The bounded retention for an
  expired digest is therefore one sweep interval plus one run after expiry,
  as long as the app is running; a stopped app clears the backlog at its next
  start.
- **Races.** The sweep selects rows `FOR UPDATE SKIP LOCKED`, so it never waits
  on, and never overrides, an acceptance or revocation that holds the row. The
  holder clears the digest itself when it commits a transition; if it rolls
  back, the row is still expired and the next batch clears it. Acceptance
  re-checks expiry under its row lock and refuses an expired invitation, so a
  sweep can never clear the digest of an invitation that is still usable. The
  accept, revoke and sweep three-way race is tested against PostgreSQL.
- **v4 migration.** Migration 031 clears the digest of every accepted, revoked
  or already expired invitation and keeps every row with its timestamps.
  Pending invitations keep their digest and stay usable after the upgrade. Down
  is lossy by design: it gives every cleared row a fresh random digest whose
  token nobody holds, so `NOT NULL` can return without making any row usable;
  terminal rows stay terminal and expired rows stay expired.
- **Unchanged API.** Status stays derived from the timestamps, never from the
  digest. The list output, the `includeExpired` behaviour, the idempotent
  create retry (`200`, no token), the revoke no-op on a terminal invitation and
  the uniform `404 invalid_invitation` for every unusable token are unchanged.
  An unknown digest and a cleared one are indistinguishable to a caller.

## Consequences

- A database or backup taken after this migration holds digests of pending
  invitations only. Restoring an older v4 backup runs migration 031 on startup
  and clears its stale digests the same way.
- An OIDC sign-in that started from an invitation keeps the invitation digest
  in `oidc_pending_logins` for at most its 10-minute lifetime. If the
  invitation is revoked, accepted or swept meanwhile, the lookup finds no row
  with that digest and the sign-in gets the same invalid-invitation refusal as
  before.
- Redacted invitation rows are kept as long as their household. A row-count
  retention policy for them is out of scope; they hold no secret.
