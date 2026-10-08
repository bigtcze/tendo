# Household invitations and members

This guide describes the API-backed invitation flow. An invitation lets a household owner invite one person to join the household as a member. Email is not sent; the API returns a bearer token once, which the owner shares out of band.

## For owners

- Create an invitation for a household you own. The first successful response contains the invitation token. Share it only with the intended person using a private channel.
- The token is a secret and can be redeemed by whoever has it. Tendo stores only a SHA-256 digest and will not return the token again in a retry or list response.
- An invitation is valid for seven days and can be used once. Owners can list invitations and see whether each is pending, accepted, revoked, or expired.
- Revoke an invitation if it should no longer be used. Revoking an accepted invitation does not remove the member.

## For invited people

- A new local account can accept the invitation and then sign in separately. Accepting does not sign the person in automatically.
- A signed-in account can accept an invitation if it does not already belong to another household. Joining a second household is refused; an account that already belongs to the invited household cannot accept the invitation again.
- All invited people join with the member role. Members can read the member list but cannot create, list, or revoke invitations.

The invitation endpoints are available through the API. A browser invitation-management interface is not available yet.