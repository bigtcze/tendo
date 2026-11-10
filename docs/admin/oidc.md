# OpenID Connect (OIDC)

OIDC lets people sign in to Tendo with one configured identity provider. A provider identity, the exact issuer and subject pair, belongs to at most one Tendo account. On first use Tendo either recognizes the account through a verified email it already trusts, or asks the person to create a new account or confirm an existing one with its Tendo password. OIDC never grants the administrator role and adds household membership only through a valid invite link the person opened. Local password sign-in keeps working and does not depend on the provider. The design decision is [ADR 0012](../adr/0012-oidc-jit-and-verified-email.md).

When OIDC is enabled, the sign-in screen and invite pages show a **Sign in with <display name>** or **Continue with <display name>** button. People who already have a password can still connect their identity from **Account** in the household navigation (the **Your account** screen). See [Your account and sign-in](../user/account.md) for the user steps.

## Configuration

| Variable | Required/default | Notes |
| --- | --- | --- |
| `TENDO_OIDC_ISSUER` | Empty disables OIDC | Provider issuer base URL, exact spelling, no query or fragment. Scheme must match `TENDO_PUBLIC_URL`: HTTPS public URLs require HTTPS issuers. |
| `TENDO_OIDC_CLIENT_ID` | Required when enabled | Client identifier from the provider. |
| `TENDO_OIDC_CLIENT_SECRET` | Required when enabled unless secret file is used | Confidential-client secret. Keep `.env` private and mode `0600`; never commit or log it. |
| `TENDO_OIDC_CLIENT_SECRET_FILE` | Optional alternative to client secret | Absolute path to a mounted, non-empty secret file. Set exactly one of the secret variables. |
| `TENDO_OIDC_DISPLAY_NAME` | `OpenID Connect` | Printable label shown on the sign-in button and account screen, for example `Pocket ID`; at most 80 bytes. |

The callback URL to register is:

```text
<TENDO_PUBLIC_URL>/api/v1/auth/oidc/callback
```

For Compose, set the issuer, client ID, and client secret in `.env`; recreate the app after changing values. The secret-file option is intended for a user-provided Compose override that mounts a secret file read-only into the app and sets `TENDO_OIDC_CLIENT_SECRET_FILE` to that container path. The standard Compose file does not mount a secret or start a provider. A secret-file override must also set `TENDO_OIDC_CLIENT_SECRET` to empty because Compose forwards that variable with an empty default; the application requires exactly one non-empty secret source.

### Pocket ID setup

These steps assume Pocket ID already runs behind HTTPS, for example at `https://id.example.com`, and Tendo is reachable at `https://tendo.example.com`.

1. In Pocket ID, open **OIDC Clients** and add a client named `Tendo`.
2. Set its callback URL to exactly `https://tendo.example.com/api/v1/auth/oidc/callback`. Leave the client confidential (not public); PKCE may stay enabled.
3. Save, then copy the client ID and client secret Pocket ID shows.
4. Add these lines to Tendo's `.env` and keep the file mode `0600`:

   ```dotenv
   TENDO_OIDC_ISSUER=https://id.example.com
   TENDO_OIDC_CLIENT_ID=<client ID from Pocket ID>
   TENDO_OIDC_CLIENT_SECRET=<client secret from Pocket ID>
   TENDO_OIDC_DISPLAY_NAME=Pocket ID
   ```

   The issuer is the Pocket ID base URL; do not append `/.well-known/openid-configuration`, a trailing path, query, or fragment.
5. Recreate the app so it reads the new values: `docker compose up -d app`.
6. Select **Sign in with Pocket ID** on the Tendo sign-in screen. The first time, Tendo asks whether to confirm your existing Tendo account with its password or to create a new one; choose your existing account. Every other person with a Tendo password does the same once.

To give someone new access, invite them from **Household members** and send the link. On the invite page they can select **Continue with Pocket ID**; Tendo creates their account and they join the household in one step.

Tendo requests the `openid` and `email` scopes. Discovery is lazy: local login and app startup do not require the provider. If the provider is unavailable, OIDC start returns a sanitized 503 `oidc_unavailable` and the sign-in screen says the provider cannot be reached; local password sign-in remains available.

### First sign-in and email

Tendo reads `email` from the ID token only when `email_verified` is the JSON boolean `true`. Configure the provider so that addresses are verified before they are released; in Pocket ID, require email verification for users. A string `"true"`, a missing claim, or an address Tendo cannot normalize counts as no email. Tendo does not call the userinfo endpoint.

| Situation at sign-in | Result |
| --- | --- |
| Provider identity already belongs to an account | Signs in to that account. A changed provider email never moves the identity or overwrites a stored email. |
| Unknown identity, verified email stored on exactly one account that has no identity from this provider | Connects the identity to that account and signs in. |
| Same, but that account already has a different identity from this provider | Refused with the generic "already connected to another Tendo account" message. |
| Unknown identity, any other case | Shows **Finish signing in**: confirm an existing account with its Tendo login and password, or create a new account. |

An email is stored on an account only after it arrived verified through this provider: either at sign-in or when the person confirmed their account with its password. Accounts from before V4 have no email until then. Administrators cannot enter or change it, and each address belongs to at most one account. A stored email connects accounts automatically only for the provider (issuer) that verified it; after switching to a different provider, everyone confirms their account with its password once.

**Finish signing in** lasts 10 minutes and works only in the browser that started it. Five wrong passwords end it. A new account gets a generated login such as `member-k3x9q2ab`, no password, no household and no administrator role; it joins a household only through an invite link. Because it has no password, its owner signs in only through the provider.

If someone created a new account by mistake while they already had one, they can select **I already have a Tendo account — connect to it instead** on the "Join your household by invitation" screen. That removes the new account and returns them to **Finish signing in**. This works only while the new account has no household and no password. After it has joined a household, Tendo does not merge accounts; the provider identity stays with the new account.

## API use

The browser screens use these endpoints; scripts may use them directly.


- `GET /api/v1/auth/oidc` reports whether OIDC is enabled.
- `GET /api/v1/auth/oidc/identity` reports the signed-in account's link status.
- Start login with `POST /api/v1/auth/oidc/start` and `{"purpose":"login"}`, optionally with `"invitationToken"` from an invite link.
- While signed in, connect an identity with `{"purpose":"link","currentPassword":"..."}`.
- Complete the provider redirect at the registered callback. A redirect to `/login#oidc=continue` means a pending first sign-in.
- Inspect, finish or cancel it with `GET /api/v1/auth/oidc/pending`, `POST /api/v1/auth/oidc/pending/account`, `POST /api/v1/auth/oidc/pending/link` and `DELETE /api/v1/auth/oidc/pending`.
- `POST /api/v1/auth/oidc/account/discard` removes a just-created account under the conditions above.

See `api/openapi.yaml` and [API conventions](../development/api.md) for request, response and failure behavior.

## Troubleshooting

| Symptom | Check and fix |
| --- | --- |
| Startup says OIDC settings require an issuer | Set `TENDO_OIDC_ISSUER`, or remove all other OIDC variables to disable OIDC. |
| Startup rejects issuer scheme | Make provider and public URL schemes compatible. HTTPS public URLs require HTTPS issuers. |
| OIDC start returns 503 `oidc_unavailable` | Check provider availability, issuer spelling, client registration, and app-to-provider network access. Retry later; local login is independent. |
| Provider reports redirect URI mismatch | Register the exact `<TENDO_PUBLIC_URL>/api/v1/auth/oidc/callback`, including scheme, host, and port. |
| Someone expected to land in their existing account but sees **Finish signing in** | Expected for accounts without a stored email. They choose **I already have a Tendo account** and enter their Tendo login and password once. |
| **Finish signing in** says the attempt expired | The 10-minute window passed, five passwords were wrong, the attempt was finished in another tab, or the provider configuration changed. Sign in with the provider again. |
| Someone created a new account by mistake and has already joined a household | Tendo does not merge accounts, and this release has no way to remove a member. The person keeps using that account; their older account still signs in with its password. |
| Sign-in or account screen says the sign-in is already connected to another Tendo account (`identity_conflict`) | That provider identity, or the account its email points to, is already bound elsewhere. Sign in to that account instead, or choose **Create a new Tendo account**. Unlinking is not available in this release. |
| No email is ever saved | Check that the provider releases `email` with `email_verified` as boolean `true` for the `email` scope. |
| Account screen says "That password isn’t right." (`invalid_credentials`) | Enter the current Tendo password, not the provider password. |
| No sign-in button appears | `GET /api/v1/auth/oidc` reports `enabled: false`; check `TENDO_OIDC_ISSUER` and recreate the app. |
| Secret-file configuration fails at startup | Confirm the override mounts a readable, non-empty file at the configured container path and that the literal secret variable is unset. |
| `TENDO_OIDC_DISPLAY_NAME` is rejected | Use printable text up to 80 bytes, or leave it unset for `OpenID Connect`. |
