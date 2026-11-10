# Household members and invitations

An administrator can invite someone to join the household as a member. Tendo does not send email: it makes a private invite link that you share yourself.

## See who is in the household

Choose **Members** in the household navigation to open **Household members**. On phones and tablets, household destinations are in a bar at the bottom of the screen; on computers they are in a sidebar on the left. Everyone in the household sees the member list with each person's login and whether they are an administrator or a member. Long lists show one page at a time; use **Next page**, **Previous page** and **First page** to move through them. For administrators, the **Invitations** list pages the same way.

## Invite someone (administrators)

1. Choose **Members** in the navigation.
2. Select **Create invite link**.
3. Copy the link and send it to the person through a private channel, such as a direct message.

The link is shown only once. Tendo stores only a fingerprint of it, so it cannot show the link again later. Anyone who has the link can use it, so do not post it in a shared place.

Each link:

- works once,
- expires after seven days,
- always joins the person as a member, not an administrator.

The **Invitations** list shows each invite as pending, accepted, or revoked. An invite that expired before anyone used it no longer appears in the list or its pages; it still cannot be used, and Tendo keeps its record. If you still want that person to join, create a new invite link. Select **Revoke** on a pending invite if it should no longer be used. Tendo asks **Revoke?**; choose **Yes** to continue or **Cancel** to keep the invite. Revoking an accepted invite does not remove the member.

If the link could not be shown, for example because the connection dropped, select **Check again**. If the invite was created, Tendo cannot show its link again, but it offers to revoke that invite so you can create a new one.

Members cannot create, see, or revoke invitations.

## Admin center

Administrators have an **Admin center** in addition to the member list. On a computer it is the last entry in the sidebar. On any screen size you can also open **Account** and choose **Open Admin center**; on a phone or tablet this is the way in, because the bottom bar keeps its five household destinations.

The Admin center lists everyone in the household with their login, whether they are an administrator or a member, and how they sign in (password, single sign-on, or both). Long lists page 50 accounts at a time.

- **Make administrator** gives that person the same abilities you have: inviting people, managing invitations and changing roles.
- **Make member** takes those abilities away. The person keeps using the household and its items.

Tendo asks for confirmation before a change and names the person and what will change. A household must always keep at least one administrator, so the last administrator cannot be demoted; Tendo explains this and changes nothing. If you demote yourself while someone else is an administrator, you become a member straight away and the Admin center disappears from your navigation.

Members never see the Admin center, and opening its address directly shows a short explanation instead of the account list. Only administrators can change roles; Tendo checks this on the server for every request.

## Reset a password (administrators)

In the Admin center, open a member who signs in with a password and choose **Reset password**. Tendo shows a private reset link once. Copy it and send it to the person through a private channel, such as a direct message; anyone who has the link can set a new password.

Each link:

- works once and expires after 24 hours,
- sets the password and signs that person out everywhere, so they must sign in again,
- is replaced when you reset the same person again, because only one link can be active at a time.

Select **Cancel** on an outstanding reset if it should no longer be used. The person cannot use a cancelled or replaced link; ask them to tell you if a link no longer works, then create a new one.

Password reset works only for accounts with a local password (password only, or password plus single sign-on). A member who signs in only with single sign-on cannot use it, because Tendo does not set passwords for those accounts. Administrators cannot reset their own password through the Admin center; use **Account** instead.

The part of the link after `#` never reaches the server, a proxy, or another site's address, and Tendo removes it from the address bar when the page opens.

## Accept an invitation

Open the link you were sent.

- **No account yet:** choose a login and a password (at least 15 characters), then select **Create account and join**. Tendo signs you in and opens the household. If signing in does not work right away, your account is still ready: sign in with the login and password you just chose.
- **Already signed in:** select **Join household**. An account can belong to only one household. If your account already belongs to another household, select **Sign out and continue** and create a new account for this one.

If the page says the link can't be used, it has expired, been revoked, or already been used. Ask an administrator for a new one.

Opening the link does not send the part after `#` to the server, so it does not appear in server or proxy logs. Tendo removes it from the address bar as soon as the page opens and sends it to the server only when you accept the invitation.
