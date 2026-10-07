# Runtime configuration

This development Compose stack provides PostgreSQL, health/readiness, browser first-run onboarding protected by an operator setup code, local login sessions, and a read-only household endpoint (`GET /api/v1/households/{householdId}`, members only), and a subjects API (`/api/v1/households/{householdId}/subjects`, members only, used by the "People and things" screen). It does not provide OIDC, items, or invitations. It is not a finished household product. PostgreSQL has no host-published port; keep the app bound to loopback unless you understand the network exposure.

Copy `.env.example` to `.env` and generate two independent hexadecimal database passwords:

```sh
cp .env.example .env
openssl rand -hex 32
openssl rand -hex 32
```

Set them in their respective variables, then start all services (including the one-shot migration):

```sh
docker compose up -d --build
```

Never commit `.env` or reuse development credentials elsewhere. Generate an independent setup token only when ready to initialize the first owner:

```sh
openssl rand -base64 32
```

The output is 44-character standard base64 ending in `=`. Set it as the existing `TENDO_SETUP_TOKEN` key in `.env` (replace its value; do not add duplicate keys), ensure `.env` is mode `0600` (`chmod 600 .env`), and recreate the app with `docker compose up -d --build --force-recreate app`; restarting an existing container does not load changed Compose environment. Empty configuration disables setup; `.env.example` intentionally leaves setup disabled. Malformed nonempty tokens fail startup. The token is a bootstrap secret, not a visitor-registration credential; only perform setup over the canonical origin, with the token kept private. Never export it to logs or commit it. The onboarding screen calls this code the "setup code"; paste it there together with the household name, owner login, password, and time zone (see the README). Clear it after setup.

| Variable | Required/default | Meaning and security |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | Required; no default | PostgreSQL bootstrap superuser password; independent random hexadecimal value. |
| `TENDO_DATABASE_PASSWORD` | Required; no default | Restricted web-role password; independent random hexadecimal value. |
| `TENDO_SETUP_TOKEN` | Empty; first-run setup disabled (the onboarding screen says so) | Standard base64 of exactly 32 random bytes; generate using `openssl rand -base64 32`. Never place it in URLs or logs. |
| `TENDO_HOST_PORT` | `8080` | Loopback-only host port for HTTP. Match `TENDO_PUBLIC_URL`. |
| `TENDO_PUBLIC_URL` | Required; example `http://localhost:8080` | Canonical external origin with scheme, host, optional port, and no path except `/`, query, or fragment. Use HTTPS for internet-facing deployments. |
| `TENDO_TRUSTED_PROXY_CIDRS` | Empty; trusts no proxy | CIDRs for the immediate, controlled proxy peer only. |
| `TENDO_DB_TIMEOUT` | `2` seconds | PostgreSQL readiness ping bound, 1–10 seconds. |
| `TENDO_SHUTDOWN_TIMEOUT` | `10` seconds | HTTP drain and pool cleanup bound, 1–60 seconds. Compose allows 70 seconds. |
| `TENDO_RESTART_POLICY` | `unless-stopped` | App Compose restart policy. |
| `DATABASE_URL` | Compose constructs it | Migration service receives admin access separately; web app receives restricted DML role only. Compose uses `sslmode=disable` on its private bridge. |
| `TENDO_LISTEN_ADDR` | Compose sets `0.0.0.0:8080` | Container HTTP bind address; process defaults to `:8080` outside Compose. |

Compose migrates using administrator credentials in a successful one-shot migration dependency; the app itself does not receive database administrator credentials. On upgrades with an existing volume, migrations do not reset data or automatically repair old grants: follow the documented upgrade path and grant migration deliberately with administrator access if needed. See [reverse proxy deployment](reverse-proxy.md) before exposing HTTP behind TLS and [backup and restore](backup-restore.md) for data recovery scope. Setup creates the first owner and household only; OIDC and household editing are not implemented.

## Login sessions

`POST /api/v1/session` creates a server-side session stored in PostgreSQL (only a SHA-256 digest of the cookie value is stored) and sets an `HttpOnly`, `SameSite=Lax`, `Path=/` cookie with no `Domain` that lasts 30 days from login; it is not renewed. The cookie policy follows the scheme of `TENDO_PUBLIC_URL`, not the scheme the app listens on: an `https://` URL uses the name `__Host-tendo_session` with `Secure`; an `http://` URL uses `tendo_session` without `Secure`. Plain HTTP is for trusted local development only; use an HTTPS public URL (through a TLS-terminating reverse proxy) for any other deployment. Login is limited to 10 attempts per client address per minute and 2 concurrent attempts, returning 429 with `Retry-After: 60`. No new environment variable is required. The migration adds the `user_sessions` table; the runtime role may select, insert, and delete sessions but not update them.
