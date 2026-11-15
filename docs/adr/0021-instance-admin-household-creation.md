# ADR 0021: Instance-admin household creation

Status: accepted

Builds on [ADR 0016](0016-instance-administrator.md) and
[ADR 0020](0020-instance-admin-household-accounts.md). Slice B3a of the admin
center (Issue #454).

## Context

ADR 0020 let an instance administrator manage memberships of an existing
household but not create a household. Creating one still required the ordinary
setup or invitation flow, and a newly created household must never exist even
briefly without an admin: the at-least-one-admin invariant is enforced by the
role-change and removal cores, so an empty household would be unfixable except
by assignment.

The create endpoint is also the first admin-center write that must be
idempotent. A client that retries after a lost response must get the household
that was created, not a second one, and a key reused for a different intent must
fail rather than silently return an unrelated household.

## Decision

- **One resource, two operations.** A household is created with
  `POST /api/v1/admin/households` (`createAdminHousehold`) and read back with
  `GET /api/v1/admin/households/{householdId}` (`getAdminHousehold`). Both are
  instance-administrator routes under the ADR 0020 rules: the global
  origin/authority middleware, then the session, then the grant
  (`403 instance_admin_required`), then the request. Every response is
  `Cache-Control: no-store`; MCP tokens are never accepted. Neither route
  carries household content.
- **Creation includes the first admin.** The request is
  `{name, timezone, adminUserId}`. `adminUserId` names an existing installation
  account that becomes the household's first membership with role `admin`. The
  household row, the creation record and the membership are written in one
  `READ COMMITTED` transaction, so the at-least-one-admin invariant holds from
  the first commit. The create response is `201` with the household
  representation, a `Location` header and a strong quoted-decimal
  household-version `ETag`. The same assignment effects as ADR 0020 run: the
  member person is provisioned, the account default is set only when unset, and
  the `household_member_assignments` audit row records authority
  `instance_admin`.
- **Evaluation order.** Origin/authority middleware (`400`, code-less `403`
  origin rejection, `421`) → session (`401`) → grant
  (`403 instance_admin_required`, before the key and the body) →
  Idempotency-Key (`400 idempotency_key_required` / `invalid_idempotency_key`)
  → body (`400`/`413`/`415`) → replay lookup → field validation → persistence
  (`503`). Field validation runs in the order `name`, `timezone`,
  `adminUserId` and answers `422` with those field names. A malformed
  `adminUserId` is `422 invalid_reference`; a well-formed but unknown or
  inaccessible administrator account is `503 unavailable`, never `404`, so the
  create route reveals nothing about account existence.
- **Idempotency is scoped to the actor.** One `Idempotency-Key` is 1–128
  visible ASCII bytes. The stored intent fingerprint is SHA-256 over
  length-prefixed fields with the domain tag `tendo.admin.household.create.v1`:
  the lowercased actor, the trimmed name exactly as stored, the timezone
  exactly as submitted, and the lowercased `adminUserId`. An identical retry
  returns `201` with the current household representation, the original
  `Location` and its current version `ETag`; the same key with a different
  name/timezone/adminUserId returns `422 idempotency_key_reused` without a
  `field`. Keys live for the household's lifetime.
- **Race handling.** Replay lookup runs inside the create transaction before
  validation. A concurrent create with the same actor and key makes the second
  `household_creations` insert fail on
  `household_creations_actor_key_unique`; the adapter maps only that constraint
  and SQLSTATE `23505` to an internal raced sentinel. The service then runs
  exactly one fresh transaction that re-checks the grant, reads the winner's
  creation row and applies the fingerprint comparison. It never retries the
  insert and never loops.
- **Migration 026.** `household_creations` stores `household_id` (primary key,
  cascade with the household), `actor_user_id` (reference, `NO ACTION`),
  `creation_key` (1–128 visible ASCII, `CHECK`), `request_fingerprint`
  (exactly 32 bytes, `CHECK`) and `created_at`, with the unique constraint
  above. The runtime role gets `SELECT, INSERT` only. The Down section drops the
  table and is lossy: a same-key retry after a downgrade and re-upgrade creates
  a new household.

## Lock order

The create transaction extends the ADR 0019/0020 global order without locking
any existing household:

1. `installation_state` `FOR SHARE` (grant gate);
2. the new `households` row (invisible to others until commit);
3. the `household_creations` actor/key unique index — a same-key loser waits
   here while holding nothing the winner needs, so the wait cannot deadlock;
4. the target `user_accounts` row `FOR NO KEY UPDATE`;
5. the membership insert;
6. the member-person `subjects` row.

## Slice C note

Global account deletion (slice C) must anonymize `actor_user_id` **in place**
rather than delete or cascade the creation record, because the actor reference
is `NO ACTION` like the other membership audit tables. The household and its
membership stay in effect; only the actor identity is replaced.

## Consequences

- An instance administrator can stand up a new household and its first admin
  atomically, without a setup or invitation round trip, and a retry is safe.
- The create path cannot produce an admin-less household, even if the response
  is lost: the invariant is committed with the household.
- The replay record is an audit fact as well as an idempotency record; it names
  which administrator created which household under which key.
- Account deletion must respect the creation reference (slice C), and the
  existing last-instance-administrator guard continues to apply to the actor.
