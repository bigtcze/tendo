# Household members and invitations

An owner can invite someone to join the household as a member. Tendo does not send email: it makes a private invite link that you share yourself.

## See who is in the household

On the home screen, open **Household members**. Everyone in the household sees the member list with each person's login and whether they are an owner or a member.

## Invite someone (owners)

1. Open **Household members**.
2. Select **Create invite link**.
3. Copy the link and send it to the person through a private channel, such as a direct message.

The link is shown only once. Tendo stores only a fingerprint of it, so it cannot show the link again later. Anyone who has the link can use it, so do not post it in a shared place.

Each link:

- works once,
- expires after seven days,
- always joins the person as a member, not an owner.

The **Invitations** list shows each invite as pending, accepted, revoked, or expired. Select **Revoke** on a pending invite if it should no longer be used. Revoking an accepted invite does not remove the member.

If the link could not be shown, for example because the connection dropped, select **Check again**. If the invite was created, Tendo cannot show its link again, but it offers to revoke that invite so you can create a new one.

Members cannot create, see, or revoke invitations.

## Accept an invitation

Open the link you were sent.

- **No account yet:** choose a login and a password (at least 15 characters), then select **Create account and join**. Tendo signs you in and opens the household. If signing in does not work right away, your account is still ready: sign in with the login and password you just chose.
- **Already signed in:** select **Join household**. An account can belong to only one household. If your account already belongs to another household, select **Sign out and continue** and create a new account for this one.

If the page says the link can't be used, it has expired, been revoked, or already been used. Ask the owner for a new one.

Opening the link does not send the part after `#` to the server, so it does not appear in server or proxy logs. Tendo removes it from the address bar as soon as the page opens and sends it to the server only when you accept the invitation.
