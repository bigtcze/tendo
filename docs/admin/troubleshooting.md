# Troubleshooting

Run every command from the directory that holds Tendo's `compose.yaml` and `.env`. Start with these two:

```sh
docker compose ps -a
docker compose logs --tail 50 postgres migrate app
```

The app writes one line per request and one `ERROR runtime failed error="…"` line when it cannot start. Logs never contain passwords, setup codes, cookies, or invitation links.

## Compose cannot start Tendo

- **An error asks you to set `POSTGRES_PASSWORD`, `TENDO_DATABASE_PASSWORD`, or `TENDO_PUBLIC_URL`:** check that `.env` exists next to `compose.yaml` and that the named value is not empty. On an existing installation, restore the original database passwords instead of generating new ones.
- **The port is already allocated:** choose a free `TENDO_HOST_PORT` in `.env`. If you use the local `http://localhost:…` address, change the port in `TENDO_PUBLIC_URL` too. Then run `docker compose up -d --wait`.
- **PostgreSQL is unhealthy or exits:** read `docker compose logs --tail 50 postgres`. Keep the data volume while you investigate; never use `docker compose down --volumes` as a repair step.

## The app keeps restarting

`docker compose ps -a` shows `app` as `Restarting`. Find the reason in `docker compose logs --tail 20 app`:

| Log message | Fix |
| --- | --- |
| `TENDO_PUBLIC_URL is required` or `TENDO_PUBLIC_URL must be an absolute HTTP(S) root origin` | Set the full address people open, with scheme and no path, for example `http://localhost:8080` or `https://tendo.example.com`. |
| `TENDO_SETUP_TOKEN must be canonical standard base64 encoding of 32 bytes` | Generate the code with `openssl rand -base64 32`, or leave the value empty after setup. |
| `invalid TENDO_TRUSTED_PROXY_CIDRS` | Use comma-separated CIDRs such as `172.29.0.10/32`; see [reverse proxy](reverse-proxy.md). |
| A message naming `TENDO_DB_TIMEOUT` or `TENDO_SHUTDOWN_TIMEOUT` | Use whole seconds: 1–10 for the database timeout, 1–60 for shutdown. |
| A message naming `TENDO_OIDC_…` | See the [OIDC troubleshooting table](oidc.md#troubleshooting). Leave `TENDO_OIDC_ISSUER` and the other OIDC settings empty to switch OIDC off. |
| `database schema validation failed` | See [the database is not ready](#the-database-is-not-ready) below. |

After fixing `.env`, apply it with `docker compose up -d --wait`.

## The database is not ready

The app logs `database schema validation failed` when it cannot use the database. Check, in order:

1. **Did the migration succeed?** `docker compose ps -a` must show `migrate` as `Exited (0)`. If not, read `docker compose logs migrate`.
2. **Did the database passwords change?** PostgreSQL keeps the passwords from when its data volume was first created. If `POSTGRES_PASSWORD` or `TENDO_DATABASE_PASSWORD` in `.env` no longer match, the app cannot sign in to the database. Restore the original values from your private copy of `.env`.
3. **Is the database schema newer than the app supports?** After going back to an older Tendo across a schema change, the migration logs `database schema is newer than this application`. Return to the newer Tendo, or restore a backup that matches the older version; see [update](update.md#go-back-to-an-earlier-version).
4. **Does the migration report `database has a dirty migration`?** Tendo refuses to use this database. Do not clear migration flags or edit the schema by hand. Restore your latest backup into a fresh installation; see [back up and restore](backup-restore.md).

## Readiness returns 503

`curl http://localhost:8080/health/ready` returns HTTP 503 while `/health/live` returns 200. The app runs but cannot reach PostgreSQL, or it is shutting down. Check `docker compose ps postgres`; start it with `docker compose up -d --wait`. Readiness recovers by itself once PostgreSQL answers.

## The page does not open

- **Connection refused at `http://localhost:8080`:** check that `app` is running and healthy. If you changed `TENDO_HOST_PORT`, use that port. The app listens only on this machine; other devices need a reverse proxy.
- **"Misdirected Request" (421):** the address in the browser does not match `TENDO_PUBLIC_URL`, for example `http://127.0.0.1:8080` instead of `http://localhost:8080`. Open exactly that address, or change `TENDO_PUBLIC_URL` and recreate the app. Behind a proxy, also check `TENDO_TRUSTED_PROXY_CIDRS` and the forwarded headers; see [reverse proxy](reverse-proxy.md).
- **Saving or signing in fails with "Forbidden" (403):** the request came from a page whose origin is not `TENDO_PUBLIC_URL`, for example a bookmark to another address or a proxy that rewrites `Origin`. Open Tendo at exactly `TENDO_PUBLIC_URL`.

## Setup problems

| What you see | Fix |
| --- | --- |
| "Setup isn't switched on for this Tendo." | `TENDO_SETUP_TOKEN` is empty. Set only that value, for example `sed -i "s\|^TENDO_SETUP_TOKEN=.*\|TENDO_SETUP_TOKEN=$(openssl rand -base64 32)\|" .env`. Do not generate new database passwords. Then run `docker compose up -d --force-recreate --wait app`; a restart alone does not read `.env`. |
| "That setup code isn't right." | Show the code again with `grep '^TENDO_SETUP_TOKEN=' .env \| cut -d= -f2-` and paste all of it, including the final `=`. If you edited `.env` after starting, recreate the app as above. |
| The sign-in screen appears instead of setup | Setup already ran. Sign in with the owner account. Setup cannot run a second time on the same database. |
| "Too many attempts." | Wait one minute and try again. |

## Sign-in problems

| What you see | Fix |
| --- | --- |
| "Login or password is incorrect." | Check the login (lowercase). Tendo has no password reset in this version; another household member cannot reset it either. Keep your password in a password manager. |
| "Too many attempts." | Sign-in allows 10 attempts per minute from one address. Wait one minute. |
| You are signed out after some weeks | Sessions last 30 days and then end. Sign in again. |
| Sign-in works on `http://localhost` but not through your domain | `TENDO_PUBLIC_URL` must be the HTTPS address of your domain. With an `https://` address the session cookie is marked `Secure` and is sent only over HTTPS. |
| "Your account isn't part of a household yet." | The account exists but has no household. Ask an owner for an invite link; see [household members](../user/household-members.md). |

For problems with **Sign in with …**, see the [OIDC troubleshooting table](oidc.md#troubleshooting).

## Getting help

When you report a problem, include the Tendo commit (`git log -1 --oneline`), `docker compose ps -a`, and the relevant `docker compose logs` lines. Remove your domain name if you prefer; logs contain no secrets, but do not attach your `.env`.
