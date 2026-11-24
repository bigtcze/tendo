# Your account and sign-in

Choose **Account** in the household navigation to open **Your account** and see which login you are signed in with. On phones and tablets, household destinations are in a bar at the bottom of the screen; on computers they are in a sidebar on the left. If your session ends, Tendo returns you to sign-in and says so; unsaved drafts are not kept.

## Changing your password

In **Your account** you can replace the password you sign in with. Tendo asks for the password you use now and for the new one. The new password must be 15 to 128 characters.

After the change, every other browser and device signed in to your account is signed out and has to sign in again with the new password. The browser where you made the change stays signed in. The same happens when an administrator resets your password through a reset link, or when whoever runs your Tendo installation sets a new password for you from the host.

You can only change your password from a session you started with that password. If you signed in with **Sign in with …**, sign out and sign in with your password first; a session opened that way cannot replace the Tendo password, even when your account also has one. An account that signs in only through the identity provider has no Tendo password to change.

If you no longer remember your current password, changing it here is not possible: ask an administrator for a reset link (see [Household members and invitations](household-members.md#reset-a-password-administrators)), or ask whoever runs your Tendo installation to set a new password from the host.

## Household

**Your account** has a **Household** section that names the household you are looking at now and offers **Leave household**. This is where a member who is not an administrator leaves, and it works for administrators too.

Leaving gives up access to everything in that household: your items stay there with no one responsible for them, completed items stay in the history, and invitations you sent and password reset links involving you in that household stop working. You keep your other households and your account keeps working; if you belong to no other household, Tendo explains that you can join one with an invitation. The last administrator of a household cannot leave; make someone else an administrator first. See [Remove someone or leave the household](household-members.md#remove-someone-or-leave-the-household).

## Connect an AI app

If whoever runs your Tendo installation has switched AI apps on, you can connect an AI app that supports MCP to your Tendo account. In the app, add Tendo as an MCP server with the address they give you, normally your Tendo address followed by `/mcp`, for example `https://tendo.example.com/mcp`.

The app then opens a Tendo page in your browser. If you are not signed in, sign in first, with your password or **Sign in with …**; Tendo brings you back to the same request afterwards. The page shows:

- **Connect …?** with the name the app gave itself. Tendo has not checked that name, so connect only an app you started connecting yourself.
- Where Tendo sends you back afterwards: a website address, an app on this computer, or an app on this device.
- What the app will be able to do, and that this covers all your current and future households until you disconnect it.
- Which account you are signed in as, with **Not you? Sign out**.

Select **Allow** to connect the app or **Deny** to refuse. **Allow** becomes available a moment after the page appears, and again a moment after you come back to the tab, so a click meant for something else cannot approve it. Tendo then shows **Returning to the app…** and hands you back to the app; you can close the tab if nothing happens. A request lasts 5 minutes and can be answered once; to try again, start connecting again in the app.

The app acts as you: it sees only what you can see. In this version it can only list your households and your role in each. It cannot read items or people, or change anything, yet, even though the page lists those.

## Connected apps

If you have connected an AI app to Tendo, **Your account** shows a **Connected apps** section. It is not shown when you have no connected apps. Each entry shows the name the app gave itself (Tendo has not checked it), when you connected it, whether it can only read or can also change items, and the date its access ends by at the latest. Access can end sooner, for example when the app has not been used for a long time.

Select **Disconnect** next to an app and confirm. The app loses access at once and cannot use Tendo again until you connect it again from the app. A request it had already started may still finish. You can only see and disconnect your own apps.

If whoever runs your Tendo installation has switched AI apps off, your connected apps stop working but are not removed: they still appear here and you can still disconnect them. They work again if AI apps are switched back on.

## Appearance and language

In **Your account**, choose **Appearance** to match this device, or select **Light** or **Dark**. The explicit choice is saved in this browser only. Under **Language**, choose English or Čeština; you can also change the language in the footer.

## Sign in with your identity provider

Some households set up a shared sign-in service, such as Pocket ID. When the administrator has turned it on, the sign-in screen shows a **Sign in with …** button below the password form. Your password always keeps working, even if that service is down.

### The first time

Select **Sign in with …** and approve the request at the identity provider. If this sign-in is already connected to your Tendo account, or the provider confirms an email address that Tendo already trusts for exactly one account, you go straight to your household. Otherwise Tendo shows **Finish signing in** with two choices:

- **Use my existing account:** enter your Tendo username and password, then select **Connect and sign in**. Tendo connects the sign-in to that account and saves the verified email address from the identity provider, so you are recognized next time. If someone else's account already uses that address, Tendo signs you in but does not save it and tells you so.
- **Create a new Tendo account:** Tendo creates an account with a generated username such as `member-k3x9q2ab` and no password. You sign in to it only with **Sign in with …**.

**Finish signing in** lasts 10 minutes in the browser where you started. To leave without choosing, select **Cancel and sign in another way**.

This is the only way to connect a sign-in to an account you already have; **Your account** has no separate connect step. After that one-time confirmation, **Sign in with …** takes you straight in, and your Tendo password keeps working too.

### Joining a household

A new account does not belong to a household. Tendo shows **Join your household by invitation**: ask an administrator for an invite link and open it while signed in. If you open the invite link first, you can select **Continue with …** on the invite page; you then join the household as soon as you finish signing in. If the invitation is no longer valid, you are still signed in and Tendo says so.

### Created a new account by mistake?

While the new account has no household, select **I already have a Tendo account — connect to it instead** on the **Join your household by invitation** screen, then **Remove and continue**. Tendo removes the new account and shows **Finish signing in** again, where you choose **Use my existing account**. After the new account has joined a household, it can no longer be removed this way, and Tendo never merges accounts.

## What signing in with an identity provider does not do

- It does not make anyone an administrator.
- It does not add anyone to a household without an invite link from an administrator; see [Household members and invitations](household-members.md).
- It does not use an email address that the identity provider has not verified, and an administrator cannot set your email address.

## Messages you may see

| Message | What to do |
| --- | --- |
| This sign-in can't be connected automatically … | The provider's email address belongs to a Tendo account that already uses a different sign-in from this provider, so Tendo did not connect or create anything. If that is your account, sign in with your Tendo password. If you don't have a Tendo account yet, ask your household administrator for an invite link. |
| This sign-in is already connected to a Tendo account. Sign in with your password instead. | While you were finishing, this sign-in was connected elsewhere, for example in another tab. Start again. |
| This sign-in attempt has expired. Sign in again to continue. | **Finish signing in** timed out, was completed in another tab, or had too many wrong passwords. Select **Sign in with …** again. |
| You're signed in, but your email address wasn't saved to this account. | Another account already uses that address. The sign-in is connected and keeps working; only the address was not saved. |
| That invitation is no longer valid. | You are signed in, but the invite had expired, was revoked, or was already used. Ask an administrator for a new link. |
| Sign-in was cancelled. | Try again, or use your password. |
| That sign-in attempt expired or was already used. / That sign-in attempt is no longer valid. | Start again: select **Sign in with …** on the sign-in screen. |
| You signed in or out in another tab while this was in progress. | Start again in one tab. |
| … can't be reached right now. | The identity provider is unavailable. Use your password. |
| This request is no longer valid. Start connecting again from the app. | The connection request expired after 5 minutes, was already answered, or was opened in another browser. Start connecting again in the AI app. |
| Can’t connect this app | The app's request could not be used: it is not registered with Tendo, its return address does not match, it sent something Tendo cannot use, or there were too many attempts. Follow the sentence under the heading, then start again in the app. |
| Other sign-in options couldn't be loaded. | Tendo couldn't check for the **Sign in with …** button. Choose **Try again**, or use your password. |
