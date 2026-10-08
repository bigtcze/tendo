# ADR 0007: Invitation-based local account admission

Status: accepted

## Context

V1 households are multi-user. New access must be granted explicitly by the household, without outbound email and without an external identity provider. The schema allows many memberships per account, but the V1 UI shows exactly one default household and must not silently put an account into a second household (PRODUCT: invite acceptance rejects/defers joining a second household).

## Decision

- An invitation is a bearer secret: 32 random bytes, unpadded base64url (43 characters). Only its SHA-256 digest is stored (`household_invitations.token_hash`). The raw token is returned once, in the `201` creation response, and never again (not in lists, retries, or errors). Whoever holds the token can redeem it; owners share it out of band.
- Only a current household **owner** may create, list, or revoke invitations. Role comes from the membership row read per request; the session principal stays role-free and `default_household_id` never authorizes. A member gets `403 owner_required`; a non-member gets the uniform `404` from ADR 0002.
- An invitation always grants the `member` role. Lifetime is fixed at 7 days; it is usable while `expiresAt > now`. Status (`pending`, `accepted`, `revoked`, `expired`) is derived, never stored.
- Creation requires an `Idempotency-Key` scoped to (household, creator). The first request returns `201` with the token; a retry with the same key returns `200` with the same invitation and no token, and never creates a second one. If the first response was lost, the owner revokes that invitation and creates a new one.
- Acceptance is single-use and atomic. One transaction locks the invitation, re-checks status and expiry, creates the account (new-account path) or locks the current account (signed-in path), adds the membership through the household module, sets the default household, and marks the invitation accepted. Any failure rolls everything back and leaves the invitation pending. Password hashing happens before the transaction, never while holding locks; an invalid token never triggers hashing.
- Every unusable token (malformed, unknown, expired, revoked, already used) gets the same `404 invalid_invitation`, so acceptance reveals nothing about invitation state. Owners see state in the list.
- Acceptance does not create a session. A new account signs in through the existing `POST /api/v1/session`.
- A signed-in account that already has a default household or any other membership gets `409 household_conflict`; an account already in the target household gets `409 already_member` (owners are never downgraded).
- Revocation (`DELETE`) and acceptance are one-way transitions serialized by a row lock, so invitations have no version, `ETag`, or `If-Match`. Revoking an invitation that is already accepted, revoked, or expired is a successful no-op and never removes the member.

## Consequences

- A leaked token admits one account until it expires, is used, or is revoked. Tokens travel only in request bodies, never in API URLs.
- Removing members and changing roles are not part of this decision. Revoking an invitation does not take access away from someone who already joined.
- Invitation rows are kept after they are used or revoked. There is no cleanup job.
