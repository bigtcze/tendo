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

Developer notes for the current schedule-domain slice are in [`docs/development/schedule-domain.md`](docs/development/schedule-domain.md). User and administrator guides will be added when those parts of Tendo are implemented.

## Development

Go, PostgreSQL, a versioned REST/OpenAPI contract, and React/TypeScript are the planned V1 stack. Only a pure Go schedule package and its tests are implemented so far; there is no API, database, UI, or runnable application yet. See the [schedule-domain development notes](docs/development/schedule-domain.md) for checks and current limits.
