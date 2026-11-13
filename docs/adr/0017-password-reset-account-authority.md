# ADR 0017: Password reset requires authority over the whole account

Status: accepted

Amends [ADR 0014](0014-admin-password-reset.md). Builds on
[ADR 0015](0015-multi-household-membership.md) and
[ADR 0016](0016-instance-administrator.md).

## Context

ADR 0014 authorized a reset link by one household: the issuer had to be an
admin of the household in the URL and the target a member of it. Before
multi-household membership that household was the account's only one. Since
ADR 0015 an account can belong to several households, but a password, its
`credential_version` and its sessions remain account-wide. An admin of
household A could therefore replace the password of a person who is also a
member of household B, sign in as them, and read or change B's data, or bind
an OIDC identity to keep the access after the password is changed back. The
same path let a household admin take over an instance administrator. A role
in one household must not grant authority over data in another.

## Decision

- One predicate, evaluated in the database inside the reset transaction:
  the actor may reset the target only when the target has no membership in a
  household where the actor is not `admin`, and, when the target is in
  `instance_admins`, the actor is too. There is no instance-admin override for
  household membership; that belongs to the admin center (#454).
- Issue: after the self check, the target lock and the membership lookup, and
  before the local-password fact, a failed predicate returns
  `409 password_reset_not_permitted`. No row is written and an outstanding link
  issued by someone who does hold authority is not revoked. The answer only
  tells an admin, who can already see the person as a member, that the person
  is outside their authority; it does not name the other household.
- Redeem: the existing re-authorization additionally re-evaluates the same
  predicate for the issuer against current facts. A target who joined another
  household or became an instance administrator after issue voids the link
  with the uniform `404 invalid_password_reset`, without consuming it. If the
  issuer later gains the missing authority the link works again until it
  expires, exactly like the existing issuer-role re-check.
- Redeem takes no extra lock on the user or membership rows. The token row is
  locked `FOR UPDATE` and the credential row is locked before the write;
  adding a user-row lock in a different order from issue would deadlock with a
  concurrent issue. The residual window is a membership or instance-admin
  grant committing between the predicate read and the redeem commit, which is
  the same read-committed window ADR 0014 accepted for the issuer-role check.
- Cancel (`DELETE`) is unchanged: revoking a link never grants access, so any
  admin of the household may still cancel a link targeting its member.

## Consequences

- Behaviour change for 5.0: an admin who could reset a person in 4.0 gets
  `409 password_reset_not_permitted` when that person also belongs to a
  household the admin does not administer, or is an instance administrator.
  The release notes must say who can reset such an account (an admin of every
  household the person belongs to; an instance administrator for an instance
  administrator) and that an outstanding link stops working if the person
  joins another household before using it.
- The web client shows a calm refusal naming the account, without the other
  household.
- Accepted residual risk: if a resetting admin later becomes an admin of every
  household the person belongs to, a link they issued earlier becomes usable
  again until it expires. The 24-hour lifetime bounds it.
- The admin center (#454) must keep this predicate when it adds instance-wide
  account management, and must revoke or re-check outstanding links when it
  grants instance administrator or changes memberships.
- Follow-ups not decided here: the per-target issuance throttle is consumed
  before authorization, so an admin of another household can exhaust a target's
  hourly budget; and household-scoped account views show and cancel a link
  issued in another household.
