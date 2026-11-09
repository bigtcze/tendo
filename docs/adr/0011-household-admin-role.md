# ADR 0011: Household administrator role

Status: accepted

## Context

V1 had two household roles, `owner` and `member`, where `owner` meant "may manage invitations" rather than property ownership. V4 introduces administration of member accounts, so the role needs a name that describes household management, and the first mutable role operation needs a rule that keeps every reachable household manageable. Section 2 of the [V4 baseline audit](../design/v4-baseline-audit.md#2-roles-ownermember--adminmember-v4-06-v4-07) records the shipped V3 model and the proposals this decision implements. Account management UI is V4-07; password reset is V4-08.

## Decision

- Roles are household-scoped: `admin` or `member` on a membership. There is no global administrator, and `admin` is never derived from OIDC claims, email addresses, or any external identity. The account created by initial setup becomes `admin`; invitation acceptance always creates `member`. Unknown role values fail closed on read.
- Migration `017` changes the role in one transaction: it replaces the `owner` constraint value with `admin`, rewrites existing `owner` rows, adds `household_memberships.version` (default `1`), and creates the append-only `household_role_changes` audit table (`household_id`, target, actor, previous and new role, `changed_at`). The runtime role gets only `SELECT` and `INSERT` on the audit table. The migration fails closed when a household with any membership has no admin, instead of inventing one. Downgrade restores `owner` rows and drops the version column and audit table.
- Wire contract: the `MemberRole` enum is `[admin, member]` (previously `[owner, member]`). This is a deliberate breaking change for clients that read `role`, and is called out in the release notes. The legacy problem code `owner_required` stays unchanged on the three existing invitation routes (`createInvitation`, `listInvitations`, `revokeInvitation`) for client compatibility; the new admin routes return `admin_required`.
- New admin-only operations under `/api/v1/households/{householdId}/accounts`: list accounts (cursor paged, limit at most 100), read one account (strong `ETag` equal to the membership version), and change a role with `PATCH .../accounts/{userId}`. PATCH requires `If-Match` (missing `428`, malformed or stale `412`); an unchanged role is a no-op with no version bump and no audit row; a change that would leave zero admins returns `409 last_admin`; self-demotion is allowed when another admin remains. A member gets `403 admin_required`; non-members, cross-household and malformed IDs get the uniform `404`. Representations carry `signInMethods` `{password, oidc}` booleans and never credentials.
- Concurrency: every admin write (invitation create, list and revoke, and role change) runs in one transaction that locks the household row `FOR NO KEY UPDATE` gated by the caller's membership, then re-reads the caller's role under that lock. `FOR NO KEY UPDATE` avoids deadlock with the `KEY SHARE` locks taken by foreign keys that reference the household. Two admins demoting each other concurrently leave exactly one admin, and a demoted admin's in-flight admin write fails with `admin_required`.
- User-facing text renames Owner to Administrator (Czech: Správce). The rest of the Admin center, including member account management, arrives in V4-07.

## Consequences

- Every household with members keeps at least one administrator, so it can never lose the ability to manage members and invitations.
- Members keep reading the household and using its items; they cannot manage invitations or roles.
- The audit table is append-only and has no retention, export, or UI yet; rows are removed only when their household is deleted.
- Removal of members, password reset, and OIDC account policy remain separate decisions (V4-07, V4-08, V4-10).
- Any external reader that hard-codes `owner` must accept `admin`; internal clients and generated models change with the contract.
