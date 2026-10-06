# Tendo

**Remember less. Nothing disappears until it is actually done.**

Tendo is a self-hosted family backlog for the things a household needs to remember without forcing everything into a calendar.

Examples:
- remember a one-off household task with no strict deadline;
- repeat a task on a fixed cadence;
- optionally make a repeat "fluid", so the next interval starts when the task is actually completed;
- keep an item visible while it is being handled or while you are waiting for someone else;
- track obligations around family members, home, vehicles, pets, or anything custom.

> **Project status:** early development. The backend now includes an operator-authorized first-owner and named-household setup API, but there is no login, household/item API, or user interface. This is not yet a usable household product.

## Development runtime

Copy `.env.example` to `.env`, set independent hexadecimal database passwords, and run `docker compose up -d --build`. Compose starts PostgreSQL, runs the mandatory one-shot migration service, then starts the app with restricted database credentials. Keep PostgreSQL private; the app binds to loopback by default.

First-owner setup is an operator-only backend API, not a login or browser onboarding flow. Generate a token and add it to `.env` without printing it:

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

Use Python 3 to send the request to the canonical origin with a matching Origin header. This client reads the token from `.env` and prompts for the password with `getpass`; neither secret is placed in command arguments or shell history. Run it interactively in a terminal (the password prompt uses `/dev/tty`):

```sh
python3 - <<'PY'
import getpass
import json
from pathlib import Path
import urllib.error
import urllib.request

origin = 'http://localhost:8080'
token = next(line.partition('=')[2].strip() for line in Path('.env').read_text().splitlines() if line.startswith('TENDO_SETUP_TOKEN='))
def prompt(label):
    with open('/dev/tty', 'w') as terminal:
        terminal.write(label)
        terminal.flush()
    with open('/dev/tty', 'r') as terminal:
        return terminal.readline().strip()

payload = {
    'login': prompt('Owner login: '),
    'password': getpass.getpass('Owner password: '),
    'householdName': prompt('Household name: '),
    'timezone': prompt('Timezone (for example Europe/Prague): '),
}
request = urllib.request.Request(
    origin + '/api/v1/auth/setup',
    data=json.dumps(payload, ensure_ascii=False).encode(),
    headers={
        'Content-Type': 'application/json',
        'Origin': origin,
        'X-Tendo-Setup-Token': token,
    },
    method='PUT',
)
try:
    with urllib.request.urlopen(request, timeout=15) as response:
        print(f'Setup response: HTTP {response.status}')
except urllib.error.HTTPError as error:
    print(f'Setup failed: HTTP {error.code}')
PY
```

Remove the setup token from `.env` and recreate the app when setup is complete. This endpoint is not public visitor registration. There is no login/session flow, household UI, or usable household application yet; do not treat the API response as a finished onboarding experience.

For configuration, health checks, and stop/start details, see [runtime development](docs/development/runtime.md) and the [configuration reference](docs/admin/configuration.md). See [reverse proxy deployment](docs/admin/reverse-proxy.md) before internet exposure and [backup and restore](docs/admin/backup-restore.md) for tested recovery limits.

## Documentation

- [Runtime development](docs/development/runtime.md)
- [Configuration reference](docs/admin/configuration.md)
- [Schedule-domain notes](docs/development/schedule-domain.md)

## Development

The repository contains a Go runtime skeleton, PostgreSQL wiring, and the pure Go schedule package. This remains development-only and does not provide a usable household application.
