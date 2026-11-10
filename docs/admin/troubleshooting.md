# Troubleshooting

Run every command from the directory that holds Tendo's `compose.yaml` and `.env`. Start with these two:

```sh
docker compose ps -a
docker compose logs --tail 50 postgres app
```

The deployment has exactly two services, `postgres` and `app`; there is no `migrate` container. The app writes one line per request and one `ERROR runtime failed error="…"` line when it cannot start. Logs never contain passwords, setup codes, cookies, or invitation links.

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
| `database administrator connection failed` | The app could not connect as the PostgreSQL administrator during startup. Check that `postgres` is healthy and that `POSTGRES_PASSWORD` still matches the value the data volume was created with. |
| `database runtime role tendo has elevated privileges or role memberships` | The restricted role was changed outside Tendo. Remove the elevated attributes or role memberships, or restore a backup. |
| `database serving role is not restricted` | `DATABASE_URL` does not name the restricted `tendo` role, for example because a custom Compose override points it at the administrator. Restore the published `compose.yaml` environment. |

After fixing `.env`, apply it with `docker compose up -d --wait`.

## The database is not ready

The app logs `database schema validation failed` when it cannot use the database. The app prepares the database during startup, before it opens HTTP, so read `docker compose logs app`; there is no separate migration container. Check, in order:

1. **Did startup finish?** `docker compose ps` shows the app with `(health: starting)` while it bootstraps, then `(healthy)`. If it exits or keeps restarting, read `docker compose logs --tail 50 app`.
2. **Did the database passwords change?** PostgreSQL keeps the passwords from when its data volume was first created. If `POSTGRES_PASSWORD` or `TENDO_DATABASE_PASSWORD` in `.env` no longer match, the app cannot sign in to the database. Restore the original values from your private copy of `.env`.
3. **Did the administrator connection fail?** `database administrator connection failed` means the app could not connect as the PostgreSQL administrator to create the runtime role or migrate. Check `docker compose ps postgres` and `POSTGRES_PASSWORD`.
4. **Is the database schema newer than the app supports?** After going back to an older Tendo across a schema change, startup logs `database schema is newer than this application`. Return to the newer Tendo, or restore a backup that matches the older version; see [update](update.md#go-back-to-an-earlier-version).
5. **Does startup report `database has a dirty migration`?** Tendo refuses to use this database and never repairs it silently. Do not clear migration flags or edit the schema by hand. Restore your latest backup into a fresh installation; see [back up and restore](backup-restore.md).
6. **Does startup report an elevated or connected runtime role?** `database runtime role tendo has elevated privileges or role memberships` or `database serving role is not restricted` means the restricted role or `DATABASE_URL` was changed outside the published setup. Revert the change, or restore a backup.

## Readiness returns 503

`curl http://localhost:8080/health/ready` returns HTTP 503 while `/health/live` returns 200. The app runs but cannot reach PostgreSQL, or it is shutting down. Check `docker compose ps postgres`; start it with `docker compose up -d --wait`. Readiness recovers by itself once PostgreSQL answers.

## The page does not open

- **Connection refused at `http://localhost:8080`:** check that `app` is running and healthy. If you changed `TENDO_HOST_PORT`, use that port. The app listens only on this machine; other devices need a reverse proxy.
- **"Misdirected Request" (421):** the address in the browser does not match `TENDO_PUBLIC_URL`, for example `http://127.0.0.1:8080` instead of `http://localhost:8080`. Open exactly that address, or change `TENDO_PUBLIC_URL` and recreate the app. Behind a proxy, also check `TENDO_TRUSTED_PROXY_CIDRS` and the forwarded headers; see [reverse proxy](reverse-proxy.md).
- **Saving or signing in fails with "Forbidden" (403):** the request came from a page whose origin is not `TENDO_PUBLIC_URL`, for example a bookmark to another address or a proxy that rewrites `Origin`. Open Tendo at exactly `TENDO_PUBLIC_URL`.

## Setup problems

| What you see | Fix |
| --- | --- |
| "First-time setup is closed." | `TENDO_SETUP_TOKEN` is empty. Set only that value, for example `sed -i "s\|^TENDO_SETUP_TOKEN=.*\|TENDO_SETUP_TOKEN=$(openssl rand -base64 32)\|" .env`. Do not generate new database passwords. Then run `docker compose up -d --force-recreate --wait app`; a restart alone does not read `.env`. |
| "That setup code isn’t right." | Show the code again with `grep '^TENDO_SETUP_TOKEN=' .env \| cut -d= -f2-` and paste all of it, including the final `=`. If you edited `.env` after starting, recreate the app as above. |
| The sign-in screen appears instead of setup | Setup already ran. Sign in with the administrator account. Setup cannot run a second time on the same database. |
| "Too many attempts. Try again in a moment." | Wait one minute and try again. |

## Sign-in problems

| What you see | Fix |
| --- | --- |
| "That username or password isn’t right." | Check the username (lowercase). Tendo has no password reset in this version; another household member cannot reset it either. Keep your password in a password manager. If the correct password is always refused, check `docker compose logs app` for an error-level `http request` line with `fault=password_hash_integrity_fault`: the stored password record is damaged. Restore your latest backup; see [back up and restore](backup-restore.md). |
| "There have been too many sign-in attempts." | Sign-in allows 10 attempts per minute from one address. Wait one minute. |
| You are signed out after some weeks | Sessions last 30 days and then end. Sign in again. |
| Sign-in works on `http://localhost` but not through your domain | `TENDO_PUBLIC_URL` must be the HTTPS address of your domain. With an `https://` address the session cookie is marked `Secure` and is sent only over HTTPS. |
| "Your account isn’t part of a household yet." | The account exists but has no household. Ask an administrator for an invite link; see [household members](../user/household-members.md). |

For problems with **Sign in with …**, see the [OIDC troubleshooting table](oidc.md#troubleshooting).

## Getting help

When you report a problem, include the Tendo version (`TENDO_VERSION` from `.env`; a value of `v2` means the 2.x line), the output of `docker compose images app`, `docker compose ps -a`, and the relevant `docker compose logs` lines. Remove your domain name if you prefer; logs contain no secrets, but do not attach your `.env`.
