# Install Tendo

This guide installs Tendo with Docker Compose on one server. Compose runs two long-lived containers: PostgreSQL and the Tendo app, which also serves the web interface. A short one-shot `migrate` container prepares the database before the app starts.

## Before you start

You need:

- a Linux server or computer with GNU sed, Docker Engine, and the Docker Compose plugin (`docker compose version` should work);
- Git, OpenSSL, and curl;
- a free local port, `8080` by default.

Tendo does not need any outside account or service. It sends no email and no telemetry.

## 1. Get Tendo

```sh
git clone https://github.com/bigtcze/tendo.git
cd tendo
```

Run every later command from this `tendo` directory.

## 2. Create your settings file

Copy the example and keep it private:

```sh
umask 077
cp .env.example .env
```

Generate three independent secrets: two database passwords and the one-time setup code you will enter in the browser.

```sh
sed -i \
  -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 32)/" \
  -e "s/^TENDO_DATABASE_PASSWORD=.*/TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)/" \
  -e "s|^TENDO_SETUP_TOKEN=.*|TENDO_SETUP_TOKEN=$(openssl rand -base64 32)|" \
  .env
```

Never commit `.env` or share it. Keep a private copy with your backups; Tendo cannot recover lost database passwords for you. Every setting is described in the [configuration reference](configuration.md).

The defaults serve Tendo at `http://localhost:8080`, only on this machine. To reach Tendo from other devices, put it behind an HTTPS reverse proxy and set `TENDO_PUBLIC_URL` to that address first; see [reverse proxy](reverse-proxy.md). Do not open port 8080 to your network directly.

## 3. Start Tendo

```sh
docker compose up -d --build --wait
```

The first build takes a few minutes. Compose waits for PostgreSQL to become healthy and the migration to finish, then starts the app. Check that the app is ready:

```sh
curl --fail --show-error http://localhost:8080/health/ready
```

It prints `{"status":"ok"}`. If the app is still starting, wait a few seconds and run it again. If it does not, see [troubleshooting](troubleshooting.md).

## 4. Create your household

Show the setup code in a private terminal. Do not paste it anywhere except the Tendo setup screen:

```sh
grep '^TENDO_SETUP_TOKEN=' .env | cut -d= -f2-
```

Open `http://localhost:8080` (or your `TENDO_PUBLIC_URL`). Tendo asks for:

1. the setup code;
2. a name for your household, for example "The Novák family";
3. a login (lowercase letters, numbers, `.`, `_`, `-`; at least 3 characters) and a password of at least 15 characters;
4. your time zone. Tendo suggests the one your browser reports; change it if it is wrong.

Select **Create household**. Tendo creates your account and household, then signs you in and opens the home screen. If the sign-in screen appears instead, sign in with the account you just created. Setup runs only once on the same database; after that, signed-out visits show the sign-in screen.

## 5. Switch setup off

Clear the setup code and apply the change. Compose reads `.env` only when it creates a container, so recreate the app:

```sh
sed -i 's/^TENDO_SETUP_TOKEN=.*/TENDO_SETUP_TOKEN=/' .env
docker compose up -d --force-recreate --wait app
```

## Next steps

- Invite the rest of your household from **Household members**; see [household members and invitations](../user/household-members.md).
- Set up regular [backups](backup-restore.md).
- Optional: let people sign in through an identity provider such as Pocket ID; see [OIDC](oidc.md).
- Learn how to [update Tendo](update.md).

## How this is tested

Required CI runs the exact commands on this page, extracted from these code blocks, in `scripts/quickstart-smoke.sh` (replacing only the public GitHub clone command with a clone of the checkout being tested). It also performs the setup step over the same HTTP API used by the browser. Other deployment checks start this same `compose.yaml` with generated secrets in `scripts/runtime-smoke.sh` and `scripts/setup-smoke.sh`; browser end-to-end tests create the first household through the setup screen.
