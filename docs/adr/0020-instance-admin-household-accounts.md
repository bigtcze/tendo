# ADR 0020: Instance-administrator household account center

Status: accepted

Builds on [ADR 0011](0011-household-admin-role.md),
[ADR 0015](0015-multi-household-membership.md),
[ADR 0016](0016-instance-administrator.md) and
[ADR 0019](0019-household-membership-removal.md). Amends ADR 0019, which said
an instance administrator gets no extra authority over memberships.

## Context

ADR 0016 created the instance-administrator grant but gave it no API. A
household whose only admin left the installation, or an account that should
join a household without an invitation link, still needed manual database work.
The admin center (Issue #454) needs an installation-wide view of households and
accounts and must let the instance administrator assign existing accounts and
manage memberships of any household, including one the administrator does not
belong to.

Household administration and instance administration must stay independent: a
household admin must not gain installation-wide reach, and an instance
administrator must not be silently treated as a member, see household content,
or bypass the last-admin rule. Every membership audit row must say which grant
the actor used.

## Decision

- **A separate resource tree.** Instance-administrator operations live under
  `/api/v1/admin/...`, never on the household routes:
  - `GET /api/v1/admin/households` (`listAdminHouseholds`): every household in
    ID order; items carry `id`, `name`, `timezone`, `createdAt` only.
  - `GET /api/v1/admin/accounts` (`listAdminAccounts`): the account directory,
    `userId` and `login` only, ordered by `(lower(login), id)`, with an optional
    `q` login substring matched literally (LIKE wildcards are escaped).
  - `GET /api/v1/admin/households/{householdId}/accounts`
    (`listAdminHouseholdAccounts`) and `GET .../accounts/{userId}`
    (`getAdminHouseholdAccount`, with the membership-version `ETag`).
  - `PUT .../accounts/{userId}` (`assignAdminHouseholdAccount`): assign an
    existing account with a role.
  - `PATCH .../accounts/{userId}` (`updateAdminHouseholdAccountRole`) and
    `DELETE .../accounts/{userId}` (`removeAdminHouseholdAccount`), both with
    the required `If-Match` membership version.

  No response carries items, subjects, completions, `viewerRole`, password-reset
  facts or any secret. Every response, including errors, is
  `Cache-Control: no-store`. MCP tokens are never accepted here.
- **Authorization order.** Session, then the origin/CSRF check, then the
  instance-administrator grant (`403 instance_admin_required`), then parameter
  and body validation, then household and account lookups. A caller without the
  grant therefore learns nothing from validation errors or from whether a
  household or account exists. Behind the grant, an unknown or malformed
  household or account, or an account that is not a member, is the uniform
  `404 not_found`.
- **The grant is re-checked in every transaction.** The HTTP pre-check only
  orders the `403`; each operation re-reads `instance_admins` for the actor in
  its own transaction after taking `installation_state FOR SHARE`. Concurrent
  instance-administrator commands do not block one another. A future grant
  revoke must take `installation_state FOR UPDATE` (lock-order step 1 of
  ADR 0019), so a revoked administrator's in-flight command either commits
  before the revoke or observes it and fails. Setup still required is treated
  as no grant.
- **The household lock replaces the membership gate.** After the grant check
  the household row is locked `FOR NO KEY UPDATE` by ID alone, the same mode the
  household-admin path takes through its membership gate. Household admins and
  instance administrators acting on the same household therefore serialize on
  that row, and the last-admin count is always read under it.
- **Role change and removal reuse the household-admin cores unchanged.** The
  instance-administrator PATCH runs the same role-change core (unchanged role is
  a no-op without version bump or audit row; demoting the last admin is
  `409 last_admin`). DELETE runs the ADR 0019 removal core with its ten-step
  order and effects; the account is never deleted and its sessions are not
  revoked.
- **Assignment semantics.** Under the household lock the assignment:
  1. locks `user_accounts` for the target `FOR NO KEY UPDATE` (as invitation
     acceptance does) and reads its login; an unknown account is `404`;
  2. reads the existing membership: the same role is an idempotent replay
     (`200`, no version bump, no audit row); another role is
     `409 already_member` (change it with PATCH);
  3. refuses a plain-member assignment into a household that has no admin with
     `409 household_requires_admin`, so an assignment can never create an
     admin-less populated household;
  4. inserts the membership (`201`); a concurrent duplicate insert surfacing as
     a unique violation maps to `409 already_member`;
  5. provisions the canonical member person through the subject module;
  6. sets the account default to this household only when it has none — an
     instance administrator never moves an existing navigation hint;
  7. writes the `household_member_assignments` audit row.

  No account is created and no credential, invitation or reset link is issued.
- **Self-assignment is permitted and audited.** An instance administrator may
  assign themselves to a household (for example to repair an admin-less
  household). The audit row records actor = target with authority
  `instance_admin`; it is not hidden or special-cased.
- **Audit rows record the authority used.** Migration 025 adds
  `actor_authority` (`household_admin`, `household_member`, `instance_admin`,
  enforced by `CHECK`) to `household_role_changes` and
  `household_member_removals`. Existing rows default to `household_admin`;
  removals where actor = target and the previous role was `member` are
  backfilled to `household_member` (self-leave). The new append-only
  `household_member_assignments` table records household, target, actor, role,
  authority (always `instance_admin`) and time, with `SELECT, INSERT` for the
  runtime role only.
- **Directory index.** Migration 025 adds `user_accounts(lower(login), id)` for
  the directory keyset. Its Down section is lossy: it drops the assignment audit
  rows and the authority columns, while memberships created by assignments stay.

## Lock order

The instance-administrator paths take a prefix of the ADR 0019 global order:

1. `installation_state FOR SHARE` (grant check; a future revoke takes
   `FOR UPDATE`);
2. `households FOR NO KEY UPDATE`;
3. `household_invitations` (removal only);
4. `user_accounts FOR NO KEY UPDATE`;
5. `household_memberships` (`FOR UPDATE` for change and removal; insert for
   assignment);
6. subjects, items and `password_reset_tokens`.

Assignment and invitation acceptance of the same account serialize on the
household and account rows; assignment versus removal or self-leave serialize on
the household row. None of these paths runs at `REPEATABLE READ`.

## Consequences

- An admin-less household can be repaired by assigning or promoting an admin,
  without database access, and every such action is attributable.
- A household admin's authority is unchanged; an instance administrator who is
  also a household member acts with whichever grant the route requires, and the
  audit row says which.
- The account directory reveals logins of all accounts to instance
  administrators only; it never reveals memberships, sessions or credentials.
- Global account deletion and the last-instance-administrator guard (slice C)
  will extend this center and must take `installation_state FOR UPDATE` first.
