# ADR 0016: Instance administrator

Status: accepted

Builds on [ADR 0011](0011-household-admin-role.md) and
[ADR 0015](0015-multi-household-membership.md). Household roles are unchanged.

## Context

Tendo 1.0–4.0 had exactly one installation-wide authority, the person who ran
first-time setup, but never stored it. "Administrator" always meant a household
role. With accounts able to belong to several households (ADR 0015), a
household admin role can no longer stand in for authority over the whole
installation: inviting someone as admin of a second household must not make
them able to manage every account and household. The installation-wide
administration surface (#454) therefore needs an explicit, separately granted
capability, and existing installations need a safe way to carry the setup
authority over without guessing.

## Decision

- **Separate table.** `instance_admins(user_id PRIMARY KEY REFERENCES
  user_accounts ON DELETE RESTRICT, granted_at, grant_source)` with
  `grant_source` in `setup`, `v4_bootstrap_migration`, `cli`. It is independent
  of `household_memberships`; neither implies the other.
- **Who can grant.** Only three paths insert a row:
  1. the first-time setup transaction grants the account it creates
     (`setup`), in the same transaction as the account, credential and
     household;
  2. migration 022 grants the provable v4 setup account once
     (`v4_bootstrap_migration`, below);
  3. the recovery command `tendo instance-admin grant --login <login>` run
     inside the app container (`cli`).
  Invitation acceptance, OIDC sign-in and just-in-time accounts, identity
  provider claims, household role changes and every HTTP route never grant.
- **Least privilege.** The runtime role `tendo` receives `SELECT` and `INSERT`
  only. There is no revoke path and no `UPDATE`/`DELETE` privilege yet.
  `ON DELETE RESTRICT` makes deleting an instance administrator's account fail
  instead of silently removing the grant.
- **v4 bootstrap evidence rule.** Earlier releases created the setup account,
  its local credential and the only household in one transaction, so all three
  share `created_at` (the transaction start time); setup was the only code path
  that ever created a household, and invited or OIDC accounts are created in
  other transactions. Migration 022 grants an account only when **all** hold:
  setup is complete; exactly one household exists; exactly one account has that
  household's `created_at`; that account has a local credential with the same
  `created_at` that was never replaced (`credential_version = 1`; the v4 admin
  password reset is the only path that rewrites a password and it increments
  the version, so a co-admin who reset the setup account's password cannot
  inherit the authority); and it is still an `admin` member of that household.
  Anything else (setup incomplete or the installation row missing, several or
  no households, several candidate accounts, credential reset or missing, setup
  account no longer admin) grants
  **nobody**. The migration still succeeds. It never falls back to "every
  household admin", the earliest admin, or the default-household holder,
  because each of those can name a person the operator never trusted with the
  installation.
- **Fail-closed recovery.** When nobody is granted, the operator runs
  `docker compose exec app /tendo instance-admin grant --login <login>`. Being
  able to exec into the app container already implies database-administrator
  access, so the command adds no new trust. It uses the app's `DATABASE_URL`
  (never the administrator URL), refuses while setup is incomplete, refuses an
  unknown login with one generic message, and is idempotent: an existing grant
  keeps its original source and time. It prints no secrets.
- **Startup signal.** When setup is complete and no instance administrator
  exists, startup logs one warning without logins or IDs and continues. A
  missing instance administrator never blocks household use. Migration notices
  are not a signal: the driver does not surface them.
- **Session hint.** `Session` responses carry a required boolean
  `instanceAdmin`, resolved from `instance_admins` on every request rather than
  cached in the session row. For clients it is a UI hint only. Backend
  authorization of future instance-admin routes uses the server-side
  `Principal.InstanceAdmin`, which is read from the table on every request, and
  any route that changes the grant set re-checks the table under the lock
  below. The `GET /api/v1/session`
  `ETag` stays scoped to the session row (ADR 0015), so a new grant does not
  change it.
- **No data access.** Being an instance administrator grants no household
  data access in this change. Household routes keep authorizing against
  memberships only.

- **Module ownership.** The `household` module owns the instance-admin
  application service and repository port (recovery grant and the startup
  health read), because installation administration is the admin-center
  domain. The `identity` module writes the setup grant inside its own setup
  transaction and reads the table only to resolve `Principal.InstanceAdmin`;
  it never exposes another write path.

## Duties for the admin center (#454)

- Grant, revoke and account deletion must serialize on one fixed lock (for
  example the `installation_state` singleton row) so two concurrent revokes or a
  revoke racing a delete cannot remove the last instance administrator.
- Add an append-only grant/revoke audit before any `DELETE` privilege on
  `instance_admins` is granted to the runtime role.
- Account deletion must handle the `RESTRICT` foreign key explicitly (refuse or
  revoke first under the lock), never by relaxing it to `CASCADE`.

## Consequences

- A fresh installation always has exactly one instance administrator after
  setup. A typical upgraded single-household installation gets its setup
  account automatically; anything ambiguous needs one documented command.
- Down migration 022 drops the table and every grant, including recovery
  grants. Upgrading again re-derives only the bootstrap grant from unchanged
  evidence. A backup restored from before the upgrade behaves like any other
  v4 database and is converted by the same rule.
- An older app container started against the upgraded schema refuses the newer
  schema as before; it cannot bypass the table.
