# Tendo

**Remember less. Nothing disappears until it is actually done.**

Tendo is a self-hosted family backlog for the things a household needs to remember without forcing everything into a calendar.

Examples:
- remember a one-off household task with no strict deadline;
- repeat a task on a fixed cadence;
- optionally make a repeat "fluid", so the next interval starts when the task is actually completed;
- keep an item visible while it is being handled or while you are waiting for someone else;
- track obligations around family members, home, vehicles, pets, or anything custom.

> **Project status:** early development. The repository now has a minimal HTTP health/readiness runtime and Compose wiring, but it is operational scaffolding only—not a usable household installation. It has no household features, authentication, usable API, or UI.

## Development runtime

This repository is early development, not an installable household product. The current Compose stack provides only a Go health process and PostgreSQL; there is no authentication, household API, or UI. Do not expose it publicly. PostgreSQL stays private to Compose and the app binds to loopback.

For prerequisites, generated development credentials, start/stop commands, and health checks, see [runtime development](docs/development/runtime.md) and the [configuration reference](docs/admin/configuration.md).

## Documentation

- [Runtime development](docs/development/runtime.md)
- [Configuration reference](docs/admin/configuration.md)
- [Schedule-domain notes](docs/development/schedule-domain.md)

## Development

The repository contains a Go runtime skeleton, PostgreSQL wiring, and the pure Go schedule package. This remains development-only and does not provide a usable household application.
