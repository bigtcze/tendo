# Governance

Tendo is a small open-source project led by a single maintainer. This page says who decides what, how changes are accepted, what happens if the maintainer is unavailable, and what the project plans for the next year.

## Roles and who holds them

| Role | Held by | Responsibilities |
| --- | --- | --- |
| Maintainer | [@bigtcze](https://github.com/bigtcze) (only maintainer and repository administrator) | Sets product direction and scope; accepts or declines changes; reviews or arranges review of every pull request; cuts releases; handles security reports under the [security policy](.github/SECURITY.md); enforces the [code of conduct](CODE_OF_CONDUCT.md); manages repository settings, branch protection, the `ghcr.io/bigtcze/tendo` package and the security contact address. |
| Contributor | Anyone who opens an issue or pull request | Follows [contributing](CONTRIBUTING.md) and the code of conduct. Contributors have no write access; their changes are merged by the maintainer. |
| Automated contributors | [Renovate](https://docs.renovatebot.com/) (dependency updates) and an AI coding agent run by the maintainer | Open branches and pull requests only. They work under rules the maintainer sets in [AGENTS.md](AGENTS.md) and `.autonomous/`, and pass the same protected review and checks as any contribution. The maintainer remains accountable for everything they merge. |

There are no other roles today. A regular contributor may be offered review or write access by the maintainer; any such change is announced by updating this table.

## How decisions are made

- **Everyday changes** are proposed as pull requests and decided on review. A change is accepted when it meets the [contribution requirements](CONTRIBUTING.md#requirements-for-an-acceptable-contribution), has been reviewed by someone other than its author, and the protected `main` branch's required checks pass.
- **Product scope** is decided by the maintainer. The permanent product, security, architecture and testing rules live in `.autonomous/` (start with the [charter](.autonomous/CHARTER.md)); they change only through a pull request the maintainer explicitly approves. Larger work for a release is planned as GitHub issues.
- **Disagreement** is discussed in the issue or pull request. The maintainer makes the final decision and explains it.
- **Security** decisions follow the [security policy](.github/SECURITY.md).
- **Releases** follow the [release process](docs/development/release.md): a tag on a green `main`, release notes for self-hosters and automated release qualification. Dependency and security updates ship as patch releases; a patch release is normally considered every 14 days when something that self-hosters run has changed, and a security fix is released promptly.

## Continuity

The project must be able to keep going if the maintainer cannot continue: create and close issues, accept changes, and publish releases within a week.

What a successor needs:

- administrator access to the `bigtcze/tendo` GitHub repository, which also controls branch protection, GitHub Actions, the security advisories and the `ghcr.io/bigtcze/tendo` container package (releases are built and published by GitHub Actions from this repository, so no separate signing or publishing key is held outside GitHub);
- access to the security contact mailbox **info@veselarodina.cz**;
- the right to continue the project under the [Apache License 2.0](LICENSE), which anyone already has; a fork can continue from any published release.

**Current status:** Tendo has one maintainer, and no second person holds these accesses today. The arrangement above is therefore not yet in place. Until it is, if the maintainer is unavailable, self-hosters keep running their installed release, which needs no outside service, and anyone may fork the project under its license. Setting up a trusted second person with emergency access is an open maintainer task.

## Roadmap for the next year

Planned:

- Release Tendo 5.0: membership in several households and switching between them, an installation administrator, local password change and an administrator password reset command, invitation clean-up, a configurable listening address, and per-user, OAuth-protected access for AI apps through MCP. The open release issues are listed under the [`release` label](https://github.com/bigtcze/tendo/issues?q=is%3Aissue+is%3Aopen+label%3Arelease).
- Keep dependencies and the toolchain current, and publish security and maintenance patch releases.
- Work towards the OpenSSF Best Practices badge and the OpenSSF Security Baseline, including a written assurance case, signed release artifacts and measured test coverage.

Possible later directions, not commitments: the extensions described in the [post-V1 direction](.autonomous/ROADMAP.md), such as document upload, optional calendar links or an AI assistant that only suggests. Each needs its own approved release.

Not planned: everything in the charter's [non-goals](.autonomous/CHARTER.md#non-goals) and the product's [excluded list](.autonomous/PRODUCT.md#explicitly-deferred-or-excluded), including notifications or reminders delivered by Tendo, telemetry or analytics, a native mobile app, calendar replacement, and features that require an outside service for basic use.
