# ADR 0015: Multi-household membership

Status: accepted

Amends [ADR 0007](0007-invitation-admission.md). The other invitation rules
(token secrecy, admin-only management, seven-day lifetime, single-use atomic
acceptance, uniform invalid-token response) are unchanged.

## Context

ADR 0007 allowed many membership rows per account in the schema but treated a
second household as a conflict: a signed-in account with any default household
or other membership was refused with `409 household_conflict`. That rule made
the first household permanent for an account, which is wrong now that a person
can legitimately belong to, for example, a family household and a shared
household. The product must let an existing account accept an invitation into a
second household without silently losing its role or default in the first.

The account still has exactly one `default_household_id`. It is a selection
hint for navigation and for the initial screen, not an authorization input;
every protected operation re-authorizes against membership rows.

## Decision

- **Join on invite.** Existing-account invitation acceptance no longer inspects
  the account's current default household or other memberships. It locks the
  invitation row, then the user row (lock order invitation → user, unchanged
  from ADR 0007), rejects an account already in the target household with the
  unchanged `409 already_member`, and otherwise adds a `member` membership,
  provisions the member person, sets the default household to the accepted
  household, and marks the invitation accepted in one transaction. Role is
  always `member`; owners are never downgraded and admins of other households
  are unaffected.
- **One shared rule.** The local and OIDC existing-account paths call one
  exported identity helper instead of duplicating the checks. The OIDC path
  maps `already_member` and a vanished user to its "invitation invalid but sign
  in" outcome; every other error still aborts the transaction.
- **`household_conflict` is retired.** The code, its sentinel and its HTTP
  mapping are removed. The OpenAPI response still lists it as a documented
  legacy example so generated client types and old consumers do not break, but
  no server version emits it. `already_member` is the only invitation conflict
  code.
- **"My memberships" list.** `GET /api/v1/households` returns the caller's own
  memberships in household ID ascending order, using the opaque `h1` cursor and
  the common `limit` rules (1–100, default 50). The user ID comes only from the
  authenticated principal. An admin does not automatically see every household
  in the installation through this route: a global household directory, if it
  is ever needed, must be a separate admin resource with its own authorization
  and contract, never a widening of this list. The list has no per-item `ETag`.
- **Default selection is last-write-wins.** `PATCH /api/v1/session` writes one
  value with a single `UPDATE` of `user_accounts.default_household_id` inside a
  transaction. Membership is proven by the composite foreign key
  `user_default_membership_fk`, not by application SQL: the transaction sets it
  `IMMEDIATE`, so a non-member household fails the statement with `23503`
  instead of at commit and is reported as the uniform `422 invalid_reference`.
  There is no `If-Match` and no version: two devices selecting different
  households leave the last committed value, which is acceptable for a
  navigation hint. Malformed, unknown, and non-member household IDs produce the
  identical `422 invalid_reference` with `field: defaultHouseholdId`; a vanished
  user produces `401`. Re-selecting the current value is a successful no-op. The
  response is `200` with `Cache-Control: no-store` and no `ETag`.
- **`ETag` on `GET /api/v1/session` identifies the session row, not the body.**
  It remains a session-lifetime validator for clients that want to know whether
  the same session is still active. A default-household change from another
  device does not invalidate it, and it must not be used as a body validator or
  as a precondition for selecting a default.
- **Foreign-key duty for later membership removal.** `default_household_id` is
  guarded by `user_default_membership_fk`, which is `DEFERRABLE INITIALLY
  DEFERRED` and `NO ACTION`. Because selecting a default does not create a
  membership and the default is now expected to change repeatedly, any future
  work that removes a membership or deletes a household (for example the
  membership-removal scope) **must reassign or NULL the affected accounts'
  defaults in the same transaction**, before the constraint is checked at
  commit. It may not rely on the constraint to fail the removal, and it may not
  leave an account pointing at a household it no longer belongs to.
- **Social engineering.** Switching the default is not an access grant, and no
  UI or API may present it as one. A person who joins a second household can
  navigate to it, but every household-scoped route still authorizes against
  membership. An attacker who convinces a user to "make my household the
  default" gains nothing beyond changing the landing screen; conversely, the
  default must never be accepted in place of a membership check. The signed-in
  Join confirmation does not yet name the target household, so a user cannot
  fully verify which household they are joining from that screen; the UX
  follow-up (#450/#451) will name it.

## Consequences

- Accounts can accumulate memberships across households, each with its own
  independent role. Removing memberships and deleting households become
  ordinary lifecycle operations, subject to the foreign-key duty above.
- Clients must treat `defaultHouseholdId` as a preference. They must handle a
  default referencing a household they can no longer access by falling back to
  the membership list rather than by assuming the default is authoritative.
- The list endpoint is the only new read surface. A global directory, member
  search across households, or any admin-wide household list requires a new
  owned decision; it is not implied by the existence of this list.
