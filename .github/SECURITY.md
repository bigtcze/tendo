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

Security fixes are published as a new release. Only the latest published release receives them; there are no backported fixes for older major or minor versions. Update to the newest release ([update guide](https://github.com/bigtcze/tendo/blob/main/docs/admin/update.md)).

| Version | Security fixes |
| --- | --- |
| Latest release ([releases page](https://github.com/bigtcze/tendo/releases/latest)) | Yes |
| Any older release | No — update to the latest release |
| Unreleased `main` | Reports welcome; fixes ship in the next release |

If you pin `TENDO_VERSION` to a major or minor line such as `v4`, you stop receiving security fixes once a newer line is released; follow the update guide to move to it.

## What happens after you report

1. **Acknowledgement.** The maintainer confirms receipt within 14 days.
2. **Triage.** The maintainer reproduces the report, decides whether it is a vulnerability, rates its severity, and tells you the result and the planned next steps. If it is not a vulnerability, you are told why.
3. **Fix.** When public discussion would put self-hosters at risk, the fix is prepared privately, for example in the advisory's private fork. GitHub runs no CI there, so the maintainer runs the complete local test gate on the fix before merging it, and the release workflow publishes a release only from a `main` commit that passed the `required` check. A confirmed vulnerability is released promptly, as a patch release unless the fix needs a breaking change, without waiting for the regular release schedule.
4. **Disclosure.** The release notes name the security fix and the required update, without exploit details. The full GitHub security advisory (with a CVE when one applies) is published once the fixed release is available. The goal is to publish a fix within 60 days of the report; if that is not possible, the maintainer agrees a disclosure date with you. A vulnerability that is already public or exploited is fixed and disclosed as fast as possible, and one of medium or higher severity within 60 days at most.
5. **Credit.** Reporters are credited by name in the advisory and the release notes, unless they ask to remain anonymous.

Please give the maintainer a chance to fix the issue before you disclose it. Tendo has a single maintainer and no bug bounty.

## Scope

In scope: the Tendo application and its API, the published image `ghcr.io/bigtcze/tendo`, and the published `compose.yaml` and `example.env` release assets, including their documented defaults.

Out of scope: vulnerabilities in a reverse proxy, identity provider, Docker or host you run yourself, unless Tendo's documentation tells you to configure it unsafely; problems that need an attacker to already control the server, the Docker daemon or the app container; and reports from automated scanners without a demonstrated impact.

Deployment security settings and their tradeoffs are described in the [configuration reference](https://github.com/bigtcze/tendo/blob/main/docs/admin/configuration.md) and the [reverse proxy trust boundary](https://github.com/bigtcze/tendo/blob/main/docs/admin/reverse-proxy.md#trust-boundary).
