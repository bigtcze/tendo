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

The **Invitations** list shows each invite as pending, accepted, or revoked. An invite that expired before anyone used it no longer appears in the list or its pages and cannot be used. Once an invite is accepted, revoked, or expired, Tendo deletes its fingerprint and keeps only a record of when it was created and what happened to it. If you still want that person to join, create a new invite link. Select **Revoke** on a pending invite if it should no longer be used. Tendo asks **Revoke?**; choose **Yes** to continue or **Cancel** to keep the invite. Revoking an accepted invite does not remove the member.

If the link could not be shown, for example because the connection dropped, select **Check again**. If the invite was created, Tendo cannot show its link again, but it offers to revoke that invite so you can create a new one.

Members cannot create, see, or revoke invitations.

## Admin center

Administrators have an **Admin center** in addition to the member list. On a computer it is the last entry in the sidebar. On any screen size you can also open **Account** and choose **Open Admin center**; on a phone or tablet this is the way in, because the bottom bar keeps its five household destinations.

The Admin center lists everyone in the household with their login, whether they are an administrator or a member, and how they sign in (password, single sign-on, or both). Long lists page 50 accounts at a time.

- **Make administrator** gives that person the same abilities you have: inviting people, managing invitations and changing roles.
- **Make member** takes those abilities away. The person keeps using the household and its items.

Tendo asks for confirmation before a change and names the person and what will change. A household must always keep at least one administrator, so the last administrator cannot be demoted; Tendo explains this and changes nothing. If you demote yourself while someone else is an administrator, you become a member straight away and the Admin center disappears from your navigation.

Members never see the Admin center, and opening its address directly shows a short explanation instead of the account list. Only administrators can change roles; Tendo checks this on the server for every request.

## Remove someone or leave the household

An administrator removes someone from the Admin center: find the person's row and select **Remove from household**. To leave yourself, open **Account** and use the **Household** section there: select **Leave household**. That is the only way for a member who is not an administrator, because the Admin center stays closed to them. An administrator can also leave from their own row in the Admin center, where that row's button says **Leave household**.

Tendo asks for confirmation first and names the household, and the person when it is someone else. What it explains is the same either way:

- the person loses access to everything in that household;
- their items stay there, but no one is responsible for them;
- completed items stay in the history;
- invitations they sent and password reset links involving them in that household stop working;
- they keep their other households.

Removal is from one household only. The account keeps working normally and stays in the person's other households; deleting the account everywhere is a different action that only an installation administrator can take, see [Delete an account](#delete-an-account-installation-administrators).

A household always needs at least one administrator, so the last administrator cannot be removed and cannot leave. Tendo says so and changes nothing; make someone else an administrator first.

After you leave from **Account** or your household’s Admin center, Tendo opens Home in another household you belong to; if you belong to no household now, it explains that you can join one with an invitation.

## Reset a password (administrators)

In the Admin center, open a member who signs in with a password and choose **Reset password**. Tendo shows a private reset link once. Copy it and send it to the person through a private channel, such as a direct message; anyone who has the link can set a new password.

Each link:

- works once and expires after 24 hours,
- sets the password and signs that person out everywhere, so they must sign in again,
- is replaced when you reset the same person again, because only one link can be active at a time.

Select **Cancel link** on an outstanding reset if it should no longer be used. The Admin center also shows a link that another administrator of this household created, even through another household this person belongs to, and you can cancel it. The person cannot use a cancelled or replaced link; ask them to tell you if a link no longer works, then create a new one.

Password reset works only for accounts with a local password (password only, or password plus single sign-on). A member who signs in only with single sign-on cannot use it, because Tendo does not set passwords for those accounts. Administrators cannot reset their own password; to replace your own password, change it in **Your account** (see [your account](account.md#changing-your-password)) or ask another administrator for a reset link. If the only administrator forgets their password, whoever runs your Tendo installation can set a new one from the host; see [troubleshooting](../admin/troubleshooting.md#restore-a-forgotten-password-from-the-host).

A password belongs to the person, not to one household. If the person also belongs to another household, only someone who is an administrator of every household they belong to can reset their password; otherwise Tendo says you can't reset it. Only an administrator of the whole installation can reset the password of another installation administrator. A link stops working if, before it is used, the person joins a household you don't administer, or becomes an installation administrator while you are not one.

The part of the link after `#` never reaches the server, a proxy, or another site's address, and Tendo removes it from the address bar when the page opens.

## Households (installation administrators)

Whoever runs first-time setup also administers the whole installation. That account, and one granted the capability from the host, sees **Households**, **Accounts** and **AI apps** in the Admin center. Without that capability, an administrator's Admin center shows only their own household: no **Households**, no **Accounts** and no **AI apps**. The capability covers accounts and roles, not what a household contains; see [No instance administrator](../admin/troubleshooting.md#no-instance-administrator).

**Households** lists every household in the installation with its time zone and the date it was created, and never what it contains. Select a household's name to open its accounts; opening it does not make you a member. There you see everyone who belongs, their role and how they sign in, and you can use **Make administrator**, **Make member**, **Remove from household** and **Add an account** on that household without belonging to it.

**Add an account** puts an account that already exists into the household: search for it by login, choose it, choose its **Role** (member or administrator) and select **Add to household**. Nothing new is created and no invitation is sent. A household always needs an administrator, so the first account added to a household that has none must be an administrator.

**Create household** starts a new household. Give it a name of 1 to 100 characters, choose the **Time zone** its dates follow, and choose the **First administrator** — an account that already exists. **Create household** waits for a name and an account, and Tendo never creates a household without an administrator. After it is created, Tendo opens the new household's accounts.

Removing someone from a single household stays in **Households**; **Accounts** is where an account is deleted for good.

## AI apps (installation administrators)

**AI apps** decides whether people can connect AI apps to their Tendo account (see [Connect an AI app](account.md#connect-an-ai-app)) and what those apps may do. Nothing changes until you select **Save**.

- **Allow AI apps in Tendo** is the switch for the whole installation. Turning it off pauses every connected app: no app can connect or work with Tendo, but nothing is disconnected, and the apps work again when you turn it back on. A long pause can still outlast an app's sign-in; that app then asks its person to connect it again.
- **Let accounts use AI apps** applies to every account unless it is set differently for that account.
- **What apps may do** is, by default, the most an app may do for an account, unless it is set differently for that account: **Read** (see households, items, and people and things) and **Change** (add and change items, add people and things, complete items and undo a completion; this does not include reading). Choose at least one. People can allow less for their own account, never more.

If someone else saved these settings while you were editing, Tendo shows their settings and keeps nothing of yours; make your change again. When whoever runs Tendo has switched AI apps off on the server, the screen says so: no app can connect until it is switched on there, but you can still change these settings.

**Recent changes**, under the settings, lists every change to AI apps, newest first: for everyone, for one account, or a person's own choice in their Account. Each change shows the setting as it was saved, who saved it and when; an account that has since been deleted is shown as a deleted account. **Show more** loads older changes. Your own saves appear there as soon as they are saved.

### One account's AI apps

To set one account differently, open **Accounts** and choose **AI apps** on its row. The page shows **What apps can do now** for that account and **The person's own choice**, which only they can change in their Account; it can allow less than you do, never more. Under **Your choice for this account**:

- **AI apps for this account** is **Same as other accounts** (it follows **Let accounts use AI apps**), **Allowed** or **Not allowed**. **Not allowed** pauses the account's apps without disconnecting them; they can work again once you allow them.
- **The most apps may do** is **Same as other accounts** (it follows **What apps may do**), **Read**, **Change** or **Read and change**.

Nothing changes until you select **Save**. If someone else, including the person themselves, changed the account's AI apps while you were editing, Tendo shows them as they are now and keeps nothing of yours; make your change again. Each app still does only what the person approved for it.

**Connected apps** on the same page lists the apps the person has connected, newest first, with when each was connected, what it may do and when its access ends at the latest. **Disconnect** on a row ends that app's access right away, after you confirm; **Disconnect all** does the same for every app of the account at once. Nothing is disconnected until you confirm. A disconnected app has to be connected again by the person to work with Tendo; to keep them from doing that, choose **Not allowed** under **AI apps for this account**. If an app was already disconnected, Tendo says so and refreshes the list.

## Delete an account (installation administrators)

**Accounts** lists every account in Tendo, including people who belong to no household; search by login to find one.

Choose **Delete account** on the person's row. Tendo explains what happens and asks you to type their login exactly as shown before it deletes anything. Deleting an account:

- signs the person out on every device straight away, and they can no longer sign in, with a password or with single sign-on,
- removes them from every household they belong to; items they were responsible for stay, with no one responsible,
- keeps household history, and keeps their person entry in each household so a household administrator can rename or archive it,
- cancels invitations they sent that no one has accepted yet, and makes password reset links they were sent or created stop working, in every household,
- frees their login, so a new account may use it later.

It can't be undone. Your own row has no delete button; another installation administrator has to delete your account. If the person is the only administrator of a household, Tendo refuses and changes nothing: make someone else an administrator of that household first, then try again.

## Use a password reset link (members)

An administrator may send you a private reset link. Open it in your browser:

1. Choose a new password of 15 to 128 characters. Tendo does not show or store it anywhere you can read it back, and the link stops working once it is used.
2. Tendo signs you out on every device, so any browser or phone that was signed in must sign in again. Your items and household are unchanged.
3. Sign in again with your login and the new password.

If the page says the link can't be used, it has expired, been cancelled, been replaced by a newer link, or already been used. Ask an administrator for a new one. The part of the link after `#` never reaches the server, a proxy, or another site's address.

## Accept an invitation

Open the link you were sent.

- **No account yet:** choose a login and a password (at least 15 characters), then select **Create account and join**. Tendo signs you in and opens the household. If signing in does not work right away, your account is still ready: sign in with the login and password you just chose.
- **Already signed in:** select **Join household**. If your account already belongs to another household, you keep that membership and your role in it; Tendo opens the household you just joined.

If the page says the link can't be used, it has expired, been revoked, or already been used. Ask an administrator for a new one.

Opening the link does not send the part after `#` to the server, so it does not appear in server or proxy logs. Tendo removes it from the address bar as soon as the page opens and sends it to the server only when you accept the invitation.

## Switch between households

If your account belongs to more than one household, select the household name in the top bar. The panel marks the household you are in now and lists your other households; choose one to open its Home. Tendo remembers your choice the next time you sign in. Switching only changes which household you are looking at: it never changes who belongs to a household or what they may do there. If you belong to only one household, the panel shows just that household.
