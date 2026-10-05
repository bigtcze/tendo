# Tendo

**Remember less. Nothing disappears until it is actually done.**

Tendo is a self-hosted family backlog for the things a household needs to remember without forcing everything into a calendar.

Examples:
- remember a one-off household task with no strict deadline;
- repeat a task on a fixed cadence;
- optionally make a repeat "fluid", so the next interval starts when the task is actually completed;
- keep an item visible while it is being handled or while you are waiting for someone else;
- track obligations around family members, home, vehicles, pets, or anything custom.

> **Project status:** early development. Tendo is not ready for household use yet.

## Planned V1

- simple responsive web UI for phone, tablet, and desktop;
- first-run setup that asks for your household/family name;
- multiple household members;
- people, home, vehicle, pet, and custom subjects;
- one-off items;
- repeating items with fixed cadence;
- optional fluid recurrence based on actual completion;
- in-progress and waiting states;
- history;
- local login plus optional OIDC/PocketID;
- Docker Compose deployment behind your existing reverse proxy.

Installation instructions will be added when the first usable release is available. The repository must not publish untested setup commands before then.

## Documentation

User, administrator, and developer documentation lives in [`docs/`](docs/).

## Development

The implementation uses Go, PostgreSQL, a versioned REST/OpenAPI contract, and React/TypeScript.

See [`docs/development/`](docs/development/) once development setup documentation is available.
