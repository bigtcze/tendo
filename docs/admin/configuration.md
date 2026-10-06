# Runtime configuration

This reference applies only to the development/CI Compose runtime. It is not a supported household installation: the current process exposes health endpoints only, with no authentication, household API, or UI. Do not expose it to the internet, even behind a reverse proxy. PostgreSQL has no host-published port; keep the app bound to loopback unless you understand the network exposure.

Copy `.env.example` to `.env` and generate two independent hexadecimal passwords (hex avoids Compose/PostgreSQL URI interpolation issues):

```sh
cp .env.example .env
openssl rand -hex 32
openssl rand -hex 32
```

Put one generated value in each password variable, then run `docker compose up -d --build`. Never commit `.env` or reuse these development credentials in another environment.

| Variable | Required/default | Meaning and security |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | Required; no default | PostgreSQL bootstrap superuser password. Use independent hexadecimal random data. |
| `TENDO_DATABASE_PASSWORD` | Required; no default | Password for the restricted `tendo` database role. Use independent hexadecimal random data. |
| `TENDO_HOST_PORT` | `8080` | Loopback-only host port for the HTTP app. Choose an unused port if needed; keep the port in `TENDO_PUBLIC_URL` matched. |
| `TENDO_PUBLIC_URL` | Required; `.env.example` uses `http://localhost:8080` | Canonical external origin: absolute `http` or `https` URL with host and optional port, no path except `/`, query, or fragment. Must match the URL users use. Use HTTPS on an internet-facing deployment. |
| `TENDO_TRUSTED_PROXY_CIDRS` | Empty; trusts no proxy | Optional comma-separated CIDRs for the immediate proxy peer only. Never list broad private ranges unless every peer in that range is a controlled sanitizing proxy. |
| `TENDO_DB_TIMEOUT` | `2` seconds | Maximum duration for readiness DB pings. Integer seconds, from 1 to 10. |
| `TENDO_SHUTDOWN_TIMEOUT` | `10` seconds | Graceful HTTP shutdown bound. Integer seconds, from 1 to 60; Compose allows 70 seconds for graceful stop before force-kill. |
| `TENDO_RESTART_POLICY` | `unless-stopped` | Compose app restart policy. Use `no` only for bounded runtime smoke tests. |
| `DATABASE_URL` | Compose constructs it | PostgreSQL connection URI. The current Compose value uses `sslmode=disable` only on its private bridge network; do not reuse it on an untrusted network. |
| `TENDO_LISTEN_ADDR` | Compose sets `0.0.0.0:8080` | Container HTTP bind address. The process defaults to `:8080` outside Compose. |

`TENDO_SHUTDOWN_TIMEOUT` bounds HTTP draining and database-pool cleanup together. Compose allows 70 seconds before force-kill, covering the process's maximum 60-second budget plus margin. Outside Compose, both timeout variables also accept positive Go durations within their respective limits.

The process requires `DATABASE_URL` with PostgreSQL credentials, database name, and explicit `sslmode`. The app container runs as UID/GID 65532 with a read-only root filesystem and a small `/tmp` tmpfs. See [reverse proxy deployment](reverse-proxy.md) for canonical-origin and trusted-proxy behavior and executable Caddy, Nginx, and Traefik examples. This Compose runtime remains development/health-only; it does not provide authentication, sessions, OIDC, or a household application.
