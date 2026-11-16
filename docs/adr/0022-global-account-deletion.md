# ADR 0022: Global account deletion

Status: accepted

Builds on [ADR 0016](0016-instance-administrator.md),
[ADR 0019](0019-household-membership-removal.md),
[ADR 0020](0020-instance-admin-household-accounts.md) and
[ADR 0021](0021-instance-admin-household-creation.md). Slice C1 of the admin
center (Issue #454): backend and API contract. The CZ/EN deletion UI is slice
C2.

## Context

An instance administrator must be able to delete an installation account so it
can no longer sign in, appear in any household, or be assigned again. Removing
the `user_accounts` row is impossible: completion and undo history, item and
subject creators, invitations, reset-link issuers and every membership audit
table (`household_member_removals`, `household_member_assignments`,
`household_creations`) reference it without `ON DELETE`. That history must keep
its stable identifiers, and a cascade would destroy it.

A deletion also changes several households at once and may change the
installation-wide administrator set, so it must follow the ADR 0019 global lock
order and must not leave any household without an admin or the installation
without an instance administrator.

## Decision

- **Anonymize in place.** The account row stays. Its login becomes the
  tombstone `~deleted-<32 lowercase hex of the id>`, which no login pattern
  accepts and which is unique by construction. `email`, the email facts and
  `default_household_id` are cleared and `deleted_at` is set. A two-way `CHECK`
  ties `deleted_at` to the tombstone prefix and forbids a tombstone with email
  or a default household. Credentials, every session, OIDC identity links and
  link flows, and outstanding password-reset links (as target or issuer, in any
  household) are deleted or revoked.
- **Live logins only are unique.** `user_accounts_login_key` becomes a partial
  unique index `WHERE deleted_at IS NULL` with the same name, so a deleted
  login can be reused and the existing `23505` login-conflict mapping is
  unchanged. A partial index is not a key column for row locking, so the
  tombstone update stays inside `FOR NO KEY UPDATE`.
- **Deleted accounts are refused everywhere.** Every account lock, login
  lookup, session lookup, directory listing, assignment, invitation, reset and
  instance-administrator check adds `deleted_at IS NULL`. OIDC sign-in drops a
  stale link to a tombstone and never auto-attaches a verified email to a
  deleted account; the CLI grant path and installation counts exclude deleted
  accounts.
- **Route.** `POST /api/v1/admin/account-deletions` with body
  `{userId, confirmLogin}` answers `204`. It is an instance-administrator route
  under the ADR 0020 rules (origin/authority middleware, session, grant,
  `Cache-Control: no-store`, no MCP tokens). `confirmLogin` must equal the
  target's current login exactly; it guards against deleting the wrong account
  from a stale list. The route takes no `Idempotency-Key`: deletion is
  naturally idempotent, and a repeat after a lost response finds no live
  account and answers `404` without changing anything.
- **Evaluation order.** `403 instance_admin_required` → `422` `userId`
  `invalid_reference` (malformed id) → `409 cannot_delete_self` → `404`
  (unknown or already deleted) → `422` `confirmLogin` `mismatch` →
  `409 last_admin` (the target is the only admin of a household it belongs to)
  → `409 last_instance_admin` → `503 unavailable`. An instance administrator
  never deletes their own account; another administrator must do it.
- **Effects, in one `READ COMMITTED` transaction.** Grant revocation (audit
  first, into `instance_admin_revocations`, then the grant row is deleted);
  per household the ADR 0019 removal effects (item responsibility released,
  member person unlinked, household reset links revoked, removal audit with
  authority `instance_admin`, membership deleted); then account-wide reset
  links, the credential, sessions, OIDC links and flows, the
  `account_deletions` record (target, actor, time — no login, email or
  content), and finally the tombstone, immediately preceded by one more session
  delete.
- **Bounded retry.** The household set is read unlocked, locked ascending, and
  compared with the memberships locked `FOR UPDATE` after the account lock. If
  a membership appeared outside the locked set (a concurrent assignment or
  invitation acceptance), the attempt rolls back and a fresh transaction
  retries, at most three attempts in total; then `503 unavailable`. No other
  error retries.
- **Migration 027.** Adds `deleted_at`, the tombstone `CHECK`, the partial
  unique login index, `account_deletions` and `instance_admin_revocations`
  (runtime role `SELECT, INSERT`), and grants the runtime role `DELETE` on
  `instance_admins` and `oidc_identities`. Down refuses while any tombstone or
  `account_deletions` row exists: an earlier release would treat the tombstone
  as a live, assignable account. Restore a backup taken before the first
  deletion instead.

## Lock order

The deletion is the full ADR 0019 protocol, with one slot added for OIDC
advisory locks:

1. `installation_state FOR UPDATE` and the actor's live grant — deletion may
   change the administrator set, so deletions serialize with each other and
   with grants;
2. the target's `households FOR NO KEY UPDATE`, ascending;
3. `pg_advisory_xact_lock` for each OIDC identity linked to the target,
   ascending key, with the sign-in path's key derivation;
4. `household_invitations` (pending invitations the target created);
5. the target `user_accounts` row `FOR NO KEY UPDATE`;
6. the target's `household_memberships FOR UPDATE`, ascending household;
7. items, subjects and `password_reset_tokens`;
8. `local_credentials`, then sessions.

It never runs at `REPEATABLE READ`.

## Last instance administrator

`ErrLastInstanceAdmin` cannot be reached through this route today: the actor
must hold a live grant and cannot be the target, so at least one live
instance administrator other than the target always remains. The check is kept
as defense in depth inside the transaction, and the `409 last_instance_admin`
mapping documents the answer if a future path (for example a CLI deletion)
reuses the service without that precondition.

## Retained data

- History rows keep the anonymized account's id. Member and directory lookups
  never return a deleted account: it has no memberships and every live-account
  read excludes it. A future view that resolves historical actor ids to logins
  must render a deleted account without its tombstone login.
- A household person that was linked to the account is unlinked, not deleted,
  so the household keeps its item history. Its display name stays as the
  household entered it; a household admin can rename or archive that person.
- `account_deletions` and `instance_admin_revocations` are append-only.

## MCP tokens

The MCP credential store does not exist yet. Issue #456 must add deleting the
account's MCP tokens to the account-wide erase step, before the deletion
record.

## Consequences

- A deleted account cannot sign in by password, existing session, reset link
  or OIDC, and cannot be found, listed, assigned or invited.
- No household is left without an admin and the installation is never left
  without an instance administrator.
- Every history row stays referentially intact.
- Migration 027 is not reversible once used; operators downgrade by restoring
  a backup.
