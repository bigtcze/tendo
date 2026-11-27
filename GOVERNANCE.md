# Governance

Tendo is a small open-source project led by a single maintainer. This page says who decides what, how changes are accepted, what happens if the maintainer is unavailable, and what the project plans for the next year.

## Roles and who holds them

| Role | Held by | Responsibilities |
| --- | --- | --- |
| Maintainer | [@bigtcze](https://github.com/bigtcze) (only maintainer and repository administrator) | Sets product direction and scope; accepts or declines changes; reviews or arranges review of pull requests (except allowlisted automatic dependency updates); cuts releases; handles security reports under the [security policy](.github/SECURITY.md); enforces the [code of conduct](CODE_OF_CONDUCT.md); manages repository settings, branch protection, the `ghcr.io/bigtcze/tendo` package and the security contact address. |
| Contributor | Anyone who opens an issue or pull request | Follows [contributing](CONTRIBUTING.md) and the code of conduct. Contributors have no write access; their changes are merged by the maintainer or an AI agent acting for them. |
| Automated contributors | [Renovate](https://docs.renovatebot.com/) (dependency updates) and AI coding agents run by the maintainer | Renovate opens dependency pull requests and merges a short allowlist of low-risk updates itself after all required checks pass. The AI coding agents act through the maintainer's GitHub account: they open, review and merge pull requests (including Renovate pull requests outside the automerge allowlist), comment on and label issues, and tag releases, always through branch protection and the `required` check. Their rules are in [AGENTS.md](AGENTS.md) and `.autonomous/`; by those rules they may not change the locked product, security or testing rules without the maintainer's explicit approval. This is a project rule, not a technical restriction. The maintainer is accountable for everything the automation merges or releases. |

There are no other roles today. A regular contributor may be offered review or write access by the maintainer; any such change is announced by updating this table.

## How decisions are made

- **Everyday changes** are proposed as pull requests and decided on review. A change is accepted when it meets the [contribution requirements](CONTRIBUTING.md#requirements-for-an-acceptable-contribution), including review, and the protected `main` branch's required checks pass.
- **Product scope** is decided by the maintainer. The permanent product, security, architecture and testing rules live in `.autonomous/` (start with the [charter](.autonomous/CHARTER.md)); they change only through a pull request the maintainer explicitly approves. Larger work for a release is planned as GitHub issues.
- **Disagreement** is discussed in the issue or pull request. The maintainer makes the final decision and explains it.
- **Security** decisions follow the [security policy](.github/SECURITY.md).
- **Releases** follow the [release process](docs/development/release.md): a tag on a green `main`, release notes for self-hosters and automated release qualification. Dependency and security updates ship as patch releases; a patch release is normally considered every 14 days when something that self-hosters run has changed, and a security fix is released promptly.

## Continuity

OpenSSF Best Practices asks that a project can keep going within a week if any one person cannot continue: create and close issues, accept changes, and publish releases. This section describes Tendo's actual status against that goal. It is not met today.

**Current status:**

- Tendo has one maintainer, and the bus factor is 1. No second person has administrator access to the repository or the `ghcr.io/bigtcze/tendo` image, and only the maintainer is known to have access to the security mailbox **info@veselarodina.cz**.
- The repository and the image belong to the personal GitHub account `bigtcze`. GitHub does not let a collaborator administer a repository owned by a personal account.
- Releases are built and published by GitHub Actions from this repository, so no separate publishing key exists outside GitHub.
- The AI coding agents run on the maintainer's own machine with the maintainer's credentials. They are not a continuity arrangement: they keep running only as long as that machine and those credentials work.
- A conduct report about the maintainer goes to the same address, because there is no independent second contact.

**If the maintainer becomes unavailable:**

- Self-hosters keep running their installed release, which needs no outside service. Published images stay available for as long as the `bigtcze` account exists.
- Anyone may fork the project from any published release under the [Apache License 2.0](LICENSE) and publish their own image. Self-hosters would then switch to that image with a Compose override file or the fork's own `compose.yaml`.

**Options that would close the gap:** giving a trusted second person write access to the repository and access to the `ghcr.io/bigtcze/tendo` package, which is enough to handle issues, accept changes and push release tags but not to change repository settings; a second owner through a GitHub organization; or a [GitHub account successor](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/repository-access-and-collaboration/maintaining-ownership-continuity-of-your-personal-accounts-repositories), which applies only after the account holder's death, goes through GitHub Support, and covers repositories but not the published image. Whether to use any of them is the maintainer's decision; none is a project requirement.

## Roadmap to the end of 2027

Planned:

- Release Tendo 5.0: membership in several households and switching between them, an installation administrator, local password change and an administrator password reset command, invitation clean-up, a configurable host IP for Tendo's published port (local-only by default), and per-user, OAuth-protected access for AI apps through MCP. The open release issues are listed under the [`release` label](https://github.com/bigtcze/tendo/issues?q=is%3Aissue+is%3Aopen+label%3Arelease).
- Keep dependencies and the toolchain current, and publish security and maintenance patch releases.
- Meet as many OpenSSF Best Practices (passing and silver) and Security Baseline level 1 criteria as the project can on its own, including a written assurance case, signed release artifacts and measured test coverage.

Possible later directions, not scheduled for the next year: the [post-V1 direction](.autonomous/ROADMAP.md), such as document upload with AI that extracts and suggests items for a person to review, or optionally creating a calendar event from an item. Each needs its own approved release.

Not planned: the charter's [non-goals](.autonomous/CHARTER.md#non-goals), such as a notification delivery system, a native mobile app, calendar replacement or an autonomous household agent; telemetry or analytics; and anything that needs an outside service for basic use.
