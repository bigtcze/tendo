# Security policy

## Report a vulnerability privately

Do not report a vulnerability in a public GitHub issue, pull request, discussion or commit until a fix is released.

Report it privately in one of these ways:

- Email **info@veselarodina.cz** with "Tendo security" in the subject.
- Or use GitHub private vulnerability reporting: [Report a vulnerability](https://github.com/bigtcze/tendo/security/advisories/new) on the repository's **Security** tab.

Include what you can:

- the Tendo version (`TENDO_VERSION` and the image digest) and how it is deployed, for example behind which reverse proxy;
- the affected part, such as sign-in, OIDC, invitations, household access, MCP or the Compose deployment;
- steps to reproduce, and what an attacker gains;
- whether you want to be credited, and under which name.

Never send real household data, passwords or tokens. Use a test installation.

## Supported versions

Security fixes are published as a new release. Only the latest published release receives them; there are no backported fixes for older major or minor versions. Update to the newest release ([update guide](../docs/admin/update.md)).

| Version | Security fixes |
| --- | --- |
| Latest release ([releases page](https://github.com/bigtcze/tendo/releases/latest)) | Yes |
| Any older release | No — update to the latest release |
| Unreleased `main` | Reports welcome; fixes ship in the next release |

## What happens after you report

1. **Acknowledgement.** The maintainer confirms receipt within 14 days, normally much sooner.
2. **Triage.** The maintainer reproduces the report, decides whether it is a vulnerability, rates its severity, and tells you the result and the planned next steps. If it is not a vulnerability, you are told why.
3. **Fix.** The fix is developed privately when disclosure would put self-hosters at risk, for example in a GitHub security advisory's private fork, and goes through the same review and tests as every change. A fix for a confirmed vulnerability is released promptly as a patch release, without waiting for the regular release schedule.
4. **Disclosure.** After the fixed release is published, the maintainer publishes a GitHub security advisory (with a CVE when one applies) and describes the issue and the required update in the release notes. The goal is to publish a fix within 90 days of the report; if that is not possible, the maintainer agrees a disclosure date with you. If a vulnerability is already being exploited or is already public, it is fixed and disclosed as fast as possible.
5. **Credit.** Reporters are credited by name in the advisory and the release notes, unless they ask to remain anonymous.

Please give the maintainer a chance to fix the issue before you disclose it. Tendo has a single maintainer and no bug bounty.

## Scope

In scope: the Tendo application and its API, the published image `ghcr.io/bigtcze/tendo`, and the published `compose.yaml` and `example.env` release assets, including their documented defaults.

Out of scope: vulnerabilities in a reverse proxy, identity provider, Docker or host you run yourself, unless Tendo's documentation tells you to configure it unsafely; problems that need an attacker to already control the server, the Docker daemon or the app container; and reports from automated scanners without a demonstrated impact.

Deployment security settings and their tradeoffs are described in the [configuration reference](../docs/admin/configuration.md) and the [reverse proxy trust boundary](../docs/admin/reverse-proxy.md#trust-boundary).
