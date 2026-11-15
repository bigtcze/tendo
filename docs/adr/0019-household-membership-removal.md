# ADR 0019: Household membership removal and leave

Status: accepted

Builds on [ADR 0011](0011-household-admin-role.md),
[ADR 0015](0015-multi-household-membership.md),
[ADR 0017](0017-password-reset-account-authority.md) and
[ADR 0018](0018-local-password-change-and-cli-reset.md).

## Context

A household could gain members through setup and invitation acceptance, and an
admin could change a member's role, but no path removed a membership. A person
who wanted to leave, or an admin who needed to revoke someone's access, had to
be handled manually in the database. Doing that correctly is not one `DELETE`:
the membership is referenced by the canonical member person and by item
responsibility, a removed member may still hold invitations and reset links into
the household, the account default may point at it, and the household must never
be left without an admin. Sessions are account-wide, not household-scoped, so
signing the account out everywhere would also sign it out of households the
removal does not touch.

The removal must therefore be one transaction that visits every affected module
in a fixed order, refuses to break the last-admin invariant, and is uniform
about what it reveals to a caller who is not authorized.

## Decision

- **One endpoint serves removal and leave.**
  `DELETE /api/v1/households/{householdId}/accounts/{userId}`
  (`removeHouseholdAccount`) removes one membership and answers `204` with no
  body and `Cache-Control: no-store`. The required `If-Match` is the target
  membership version, exactly as for PATCH: missing is `428`, malformed, weak,
  listed or stale is `412`.
- **A plain member reads their version from the household.** The account read
  that carries the membership `ETag` is admin-only (`RequireAdmin`), so a plain
  member calling it for their own leave received `403` and never sent the
  `DELETE`. Rather than relax that route and its uniform authorization story,
  the `Household` representation gains `viewerMembershipVersion`: the caller's
  own membership version, read in the same household query as `viewerRole`. The
  client re-reads the household immediately before the `DELETE` and quotes
  `"<viewerMembershipVersion>"` as `If-Match`. An admin removing another member
  keeps the existing account read. The field is a navigation hint only; the
  removal still re-checks the version and the caller's authority under the
  membership lock.
- **Authorization is the membership, not the instance.**
  - A current household admin may remove any member of that household,
    including themselves.
  - A plain member may remove only themselves (the "leave" path).
  - A plain member removing anyone else is `403 admin_required`.
  - A non-member, an unknown household, a malformed target, and a target who is
    not a member of this household all return the uniform `404 not_found`, so
    the answer never reveals the caller's role.
  - An instance administrator gets no extra authority here; household
    administration is the only grant.
- **The last admin cannot disappear.** Under the household lock the transaction
  counts the other admins of the household. Removing an admin when no other
  admin remains is `409 last_admin`, so both self-leave and removal of the sole
  admin are refused and change nothing.
- **One transaction, one documented order.** After the household row is locked
  through the shared admin protocol and the actor's role is re-read under that
  lock, the removal runs:

  1. revoke the target's still-pending invitations in this household;
  2. lock `user_accounts` for the target `FOR NO KEY UPDATE`;
  3. lock the target membership `FOR UPDATE` and check `If-Match`;
  4. refuse when the target is the last admin;
  5. release the target's item responsibility;
  6. unlink the target's canonical person subject;
  7. revoke reset links the target is party to inside this household;
  8. delete the membership;
  9. move or clear the account default;
  10. write the `household_member_removals` audit row.

  The order is a cross-module contract and must not be reordered. Invitations
  are revoked before the account row because invitation acceptance locks the
  invitation first and then the account; taking the locks in the same direction
  prevents a `40P01` deadlock. The membership is locked `FOR UPDATE` rather than
  `FOR NO KEY UPDATE` so a concurrent insert that references it through the
  `items_responsible_membership_fk` or `subjects_linked_membership_fk` (both
  `NO ACTION`) blocks until the removal commits and then fails with `23503`
  instead of succeeding against a membership that is about to disappear. The
  account lock is `FOR NO KEY UPDATE`: it serializes account-level writes but
  stays compatible with the `KEY SHARE` a session insert takes, so it cannot
  join the credential-replacement cycle documented in ADR 0018.
- **The effects are owned by their modules.** The household package declares
  narrow ports (`InvitationRevoker`, `ResponsibilityReleaser`,
  `MemberPersonUnlinker`, `PasswordResetRevoker`,
  `DefaultHouseholdReassigner`) and receives transaction-scoped adapters built
  on the same `pgx.Tx`. The removal core writes only household-owned rows; every
  other table is written by the module that owns it, and all writes commit or
  roll back together.
- **What the effects do and do not touch.**
  - Pending invitations created by the removed member in this household are
    revoked; accepted and already-terminal invitations are left as audit
    history.
  - Item responsibility is cleared on every item of the household, archived and
    done items included. The items keep their subject, history and schedule.
  - The canonical person subject is unlinked, not archived, so item and history
    associations survive. A later rejoin provisions a fresh person; nothing is
    merged by name.
  - Reset links the removed member is party to inside this household, as target
    or as issuer, are revoked. Links the same account has through another
    household are left alone: one household's removal must not change another
    household's reset state.
  - The account default is moved to the lowest-ID remaining membership or
    cleared to `NULL` in the same transaction, satisfying the deferred
    `user_default_membership_fk` duty from ADR 0015.
  - Sessions are **not** revoked. A session is account-wide, and the account may
    still belong to other households.
  - The `household_member_removals` row records household, target, actor,
    previous role and removal time. It keeps the fact after the membership row
    is gone without storing household content.
- **Audit migration is append-only.** Migration 024 adds
  `household_member_removals` with `SELECT, INSERT` for the runtime role. Its
  Down section is necessarily lossy: it drops the audit rows, while the
  membership deletion and the effect writes stay in place.

## Global lock order

Later slices that touch several households or the account itself must keep these
lock modes and this order. The removal above is the membership-scoped prefix of
the same protocol.

1. `installation_state FOR UPDATE` — only instance-administrator paths that
   change the installation-wide administrator set, including a
   last-instance-administrator guard.
2. `households FOR NO KEY UPDATE`, ascending household id when one transaction
   touches several households.
3. `household_invitations`.
4. `user_accounts FOR NO KEY UPDATE` — never `FOR UPDATE`, because a session
   insert takes `KEY SHARE` there (ADR 0018).
5. `household_memberships FOR UPDATE` — the row whose version authorizes the
   change.
6. items, subjects, and `password_reset_tokens`.
7. `local_credentials` and sessions last, in the ADR 0018 replacement order.

Slice C (account deletion) must keep exactly these modes and must never run at
`REPEATABLE READ`: every invariant it checks (last admin, last instance
administrator) is answered by rows read under these locks, and a snapshot could
return a stale answer that the locks were meant to prevent.

## Consequences

- Removal and leave share one implementation, so their effects cannot drift.
- The last-admin rule and the uniform not-found answer are enforced in one
  place and hold under concurrent removals because the household lock
  serializes them.
- A removed member who is later re-invited joins with a fresh membership version
  and a fresh person, and any invitation or reset link revoked by the removal
  stays unusable.
- The single outstanding reset link per target (the partial unique index from
  ADR 0014) means a target cannot simultaneously have one usable link in the
  removal household and another in a second household. The "issuer-side link in
  another household stays usable" case is covered instead: a link the removed
  member issued in another household is untouched.
- Sessions surviving the removal is deliberate. Revoking access to one
  household does not sign the account out of the others; every request
  re-authorizes against current memberships.
- `household_member_removals.target_user_id` and `actor_user_id` reference
  `user_accounts` without `ON DELETE`. Account deletion (slice C) must therefore
  anonymize the account row in place or handle these audit references
  explicitly; deleting the row would either fail on the foreign key or destroy
  the removal history.
