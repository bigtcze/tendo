# OpenID Connect (OIDC)

OIDC lets an existing Tendo account connect to one configured identity provider. A successful connection enables that linked identity to sign in to the same account. Tendo does not create accounts from provider identities, match by email, or grant household memberships through OIDC. The local password remains available for recovery and is required when connecting an identity.

When OIDC is enabled, the sign-in screen shows a **Sign in with <display name>** button below the password form, and each person can connect their own identity from **Your account** on the home screen. See [Your account and sign-in](../user/account.md) for the user steps.

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
6. Sign in to Tendo with your password, open **Your account**, enter your current password, and select **Connect Pocket ID**. Every other person connects their own Pocket ID account the same way.

Pocket ID users who are not connected to a Tendo account cannot get in through Pocket ID: they see a message asking them to sign in with their password first. To give someone new access, invite them from **Household members**; after they accept, they can connect Pocket ID from **Your account**.

Tendo requests only the `openid` scope. Discovery is lazy: local login and app startup do not require the provider. If the provider is unavailable, OIDC start returns a sanitized 503 `oidc_unavailable` and the sign-in screen says the provider cannot be reached; local password sign-in remains available.

## API use

The browser screens use these endpoints; scripts may use them directly.


- `GET /api/v1/auth/oidc` reports whether OIDC is enabled.
- `GET /api/v1/auth/oidc/identity` reports the signed-in account's link status.
- Start login with `POST /api/v1/auth/oidc/start` and `{"purpose":"login"}`.
- While signed in, connect an identity with `{"purpose":"link","currentPassword":"..."}`.
- Complete the provider redirect at the registered callback. See [API conventions](../development/api.md) for response and failure behavior.

## Troubleshooting

| Symptom | Check and fix |
| --- | --- |
| Startup says OIDC settings require an issuer | Set `TENDO_OIDC_ISSUER`, or remove all other OIDC variables to disable OIDC. |
| Startup rejects issuer scheme | Make provider and public URL schemes compatible. HTTPS public URLs require HTTPS issuers. |
| OIDC start returns 503 `oidc_unavailable` | Check provider availability, issuer spelling, client registration, and app-to-provider network access. Retry later; local login is independent. |
| Provider reports redirect URI mismatch | Register the exact `<TENDO_PUBLIC_URL>/api/v1/auth/oidc/callback`, including scheme, host, and port. |
| Sign-in screen says the sign-in isn't connected to a Tendo account yet (`identity_not_linked`) | Sign in with the local password, open **Your account**, and connect the provider. Email equality does not link accounts. |
| Account screen says the sign-in is already connected to another Tendo account (`identity_conflict`) | That provider identity is linked to a different Tendo login. Sign in to that account instead. Unlinking is not available in this release. |
| Account screen says "That password isn't right" (`invalid_credentials`) | Enter the current Tendo password, not the provider password. |
| No sign-in button appears | `GET /api/v1/auth/oidc` reports `enabled: false`; check `TENDO_OIDC_ISSUER` and recreate the app. |
| Secret-file configuration fails at startup | Confirm the override mounts a readable, non-empty file at the configured container path and that the literal secret variable is unset. |
| `TENDO_OIDC_DISPLAY_NAME` is rejected | Use printable text up to 80 bytes, or leave it unset for `OpenID Connect`. |
