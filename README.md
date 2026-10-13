# Tendo

**Remember less. Nothing disappears until it is actually done.**

Tendo is a self-hosted family backlog for the things a household needs to remember without forcing everything into a calendar.

Examples:
- remember a one-off household task with no strict deadline;
- repeat a task on a fixed cadence;
- optionally make a repeat "fluid", so the next interval starts when the task is actually completed;
- keep an item visible while it is being handled or while you are waiting for someone else;
- track obligations around family members, home, vehicles, pets, or anything custom.

> **Project status:** Tendo is in active development. It supports household items, recurring obligations, and owner-issued invitation links. See [items](docs/user/items.md) and [household members and invitations](docs/user/household-members.md) for current usage; this is not a claim that V1 is complete or production-ready.

## Development runtime

Copy `.env.example` to `.env`, set independent hexadecimal database passwords, and run `docker compose up -d --build`. Compose starts PostgreSQL, runs the mandatory one-shot migration service, then starts the app with restricted database credentials. Keep PostgreSQL private; the app binds to loopback by default.

## First household

Setup is protected by a one-time setup code, so a stranger who reaches your Tendo first cannot claim it. Generate the code and add it to `.env` without printing it:

```sh
umask 077
export TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
python3 - <<'PY'
import os
from pathlib import Path

path = Path('.env')
lines = path.read_text().splitlines() if path.exists() else []
lines = [line for line in lines if not line.startswith('TENDO_SETUP_TOKEN=')]
lines.append('TENDO_SETUP_TOKEN=' + os.environ.pop('TENDO_SETUP_TOKEN'))
path.write_text('\n'.join(lines) + '\n')
path.chmod(0o600)
PY
```

Apply the changed environment by recreating the app (Compose does not import `.env` variables into your shell):

```sh
docker compose up -d --build --force-recreate app
```

Show the code in a private terminal when you are ready to paste it into the browser. Do not share the output or include it in logs:

```sh
grep '^TENDO_SETUP_TOKEN=' .env | cut -d= -f2-
```

Open `TENDO_PUBLIC_URL` (by default `http://localhost:8080`). Tendo asks for:

1. the setup code;
2. a name for your household, for example "The Novák family";
3. a login and a password (at least 15 characters);
4. your time zone. Tendo suggests the one your browser reports; change it if it is wrong.

Select **Create household**. Tendo creates your account and household, signs you in, and opens the empty home screen. If automatic sign-in fails, sign in with the account you just created. After you sign out, Tendo shows the sign-in form instead of setup.

Setup works only once. When it is done, clear `TENDO_SETUP_TOKEN` in `.env` and run `docker compose up -d --force-recreate app` again. Use an HTTPS `TENDO_PUBLIC_URL` for any non-local deployment; see [configuration](docs/admin/configuration.md).

For configuration, health checks, and stop/start details, see [runtime development](docs/development/runtime.md) and the [configuration reference](docs/admin/configuration.md). See [reverse proxy deployment](docs/admin/reverse-proxy.md) before internet exposure and [back up and restore](docs/admin/backup-restore.md) to protect your household data.

## Documentation

- [People and things](docs/user/people-and-things.md)
- [Household invitations and members](docs/user/household-members.md)
- [Your account and sign-in](docs/user/account.md)
- [Runtime development](docs/development/runtime.md)
- [Frontend development](docs/development/frontend.md)
- [Complete development testing and pre-PR verification](docs/development/testing.md)
- [Dependency and toolchain inventory](docs/development/dependencies.md)
- [Configuration reference](docs/admin/configuration.md)
- [Schedule-domain notes](docs/development/schedule-domain.md)

## Development

Create one-off items or set them to repeat on a fixed cadence or from the date you complete them. Items can have an optional attention date, status, notes, and responsible household member; completing an item preserves its history and can be undone. Owners can invite members with private links—Tendo does not send email. Sign-in works with a local password, with optional OIDC when configured. See [items](docs/user/items.md), [household members and invitations](docs/user/household-members.md), and [your account and sign-in](docs/user/account.md).
