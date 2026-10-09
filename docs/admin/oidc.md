# OpenID Connect (OIDC)

OIDC lets an existing Tendo account connect to one configured identity provider. A successful connection enables that linked identity to sign in to the same account. Tendo does not create accounts from provider identities, match by email, or grant household memberships through OIDC. The local password remains available for recovery and is required when connecting an identity.

**The sign-in and account screens do not yet have OIDC buttons in this release.** Connecting and signing in with OIDC are API-only for now; the browser controls are planned for a following update.

## Configuration

| Variable | Required/default | Notes |
| --- | --- | --- |
| `TENDO_OIDC_ISSUER` | Empty disables OIDC | Provider issuer base URL, exact spelling, no query or fragment. Scheme must match `TENDO_PUBLIC_URL`: HTTPS public URLs require HTTPS issuers. |
| `TENDO_OIDC_CLIENT_ID` | Required when enabled | Client identifier from the provider. |
| `TENDO_OIDC_CLIENT_SECRET` | Required when enabled unless secret file is used | Confidential-client secret. Keep `.env` private and mode `0600`; never commit or log it. |
| `TENDO_OIDC_CLIENT_SECRET_FILE` | Optional alternative to client secret | Absolute path to a mounted, non-empty secret file. Set exactly one of the secret variables. |
| `TENDO_OIDC_DISPLAY_NAME` | `OpenID Connect` | Printable label returned by the status API; at most 80 bytes. |

The callback URL to register is:

```text
<TENDO_PUBLIC_URL>/api/v1/auth/oidc/callback
```

For Compose, set the issuer, client ID, and client secret in `.env`; recreate the app after changing values. The secret-file option is intended for a user-provided Compose override that mounts a secret file read-only into the app and sets `TENDO_OIDC_CLIENT_SECRET_FILE` to that container path. The standard Compose file does not mount a secret or start a provider. A secret-file override must also set `TENDO_OIDC_CLIENT_SECRET` to empty because Compose forwards that variable with an empty default; the application requires exactly one non-empty secret source.

### Pocket ID setup

1. Create a confidential OIDC client in Pocket ID.
2. Add the exact callback URL above to its redirect URLs.
3. Copy the client ID and client secret to the corresponding Tendo settings.
4. Set the issuer to the Pocket ID base issuer URL shown by Pocket ID; do not append discovery paths, query parameters, or fragments.
5. Set `TENDO_OIDC_DISPLAY_NAME` if a different status label is desired, then recreate the Tendo app.

Tendo requests only the `openid` scope. Discovery is lazy: local login and app startup do not require the provider. If the provider is unavailable, OIDC start returns a sanitized 503 `oidc_unavailable`; local password sign-in remains available.

## API-only use

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
| Login redirects with `identity_not_linked` | Sign in with the local account and explicitly connect that provider identity first. Email equality does not link accounts. |
| Link start returns `invalid_credentials` | Confirm the current local password and that the session is still active. |
| Secret-file configuration fails at startup | Confirm the override mounts a readable, non-empty file at the configured container path and that the literal secret variable is unset. |
| `TENDO_OIDC_DISPLAY_NAME` is rejected | Use printable text up to 80 bytes, or leave it unset for `OpenID Connect`. |
