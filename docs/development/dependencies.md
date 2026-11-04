# Dependency and toolchain inventory

Snapshot date: 2026-10-09. Developer and CI tool pins live in [`mise.toml`](../../mise.toml) with download checksums in [`mise.lock`](../../mise.lock); image, workflow and generator pins live where they are used. Renovate discovers each of them natively (see below).

## Runtimes, images, and native tools

| Component | Pinned | Newest checked | Status |
| --- | --- | --- | --- |
| Go toolchain (CI, Docker build, OIDC test provider) | 1.27.3 | 1.27.3 | Current |
| `go` directive (`backend/go.mod`) | 1.26.0 | — | Minimum language version; builds use the pinned toolchain |
| Node.js (CI, Docker web build) | 26.13.0 | 26.13.0 (LTS "Lithium") | Current; see Node.js 26 migration notes below; package engines still allow `^22.22.2` for local work |
| PostgreSQL (Compose, CI services) | 18.6 | 18.6 (19 in beta) | Current |
| sqlc | 1.31.1 | 1.31.1 | Current; release archive SHA-256 verified against the GitHub release asset digest |
| mise (CI via `jdx/mise-action` v5.2.6; local minimum via `min_version`) | 2026.11.0 | 2026.11.0 | CI binary SHA-256 pinned in `.github/actions/toolchain`, verified against the release's `SHASUMS256.txt`; MIT |
| oapi-codegen | v2.8.2 | v2.8.2 | Current; see migration notes below |
| Distroless runtime images | `base-debian13`, `static-debian13` (`nonroot`) | same | Current; binaries are static (`CGO_ENABLED=0`) |
| Proxy smoke images | Caddy 2.11.7, Nginx 1.30.5 (stable), Traefik v3.7.14 | same | Current within supported lines |
| GitHub Actions | checkout v7.0.1, cache v6.1.0, jdx/mise-action v5.2.6, upload-artifact v7.0.2 | same | Current; exact release tags; actions run on Node 24 (runner ≥ 2.327.1) |

## Go modules (`backend/go.mod`)

| Module | Version | Type |
| --- | --- | --- |
| `github.com/coreos/go-oidc/v3` | v3.21.0 | direct |
| `github.com/go-chi/chi/v5` | v5.4.0 | direct |
| `github.com/jackc/pgx/v5` | v5.11.0 | direct |
| `golang.org/x/crypto` | v0.58.0 | direct |
| `golang.org/x/oauth2` | v0.37.0 | direct |
| `github.com/go-jose/go-jose/v4` | v4.1.5 | indirect |
| `github.com/jackc/puddle/v2` | v2.2.3 | indirect |
| `golang.org/x/sync` | v0.24.0 | indirect |
| `golang.org/x/sys` | v0.49.0 | indirect |
| `golang.org/x/text` | v0.43.0 | indirect |

All direct and required indirect modules are at their latest releases. `go list -m -u all` lists only newer versions for module-graph entries that do not enter the build.

`govulncheck` (x/vuln v1.8.0, Go 1.27.3): no reachable vulnerabilities. It reports GO-2026-5932 in `golang.org/x/crypto/openpgp`, which Tendo does not import or call, and for which no fixed version exists.

## Node packages

`npm outdated` is clean for `api/` and `frontend/` except `typescript` (see below). `npm audit` reports zero vulnerabilities for both, with the minimatch security override below.

| Package | Pinned | Notes |
| --- | --- | --- |
| `api`: `@redocly/cli` | 2.62.1 | |
| `api`: `openapi-typescript` | 7.13.0 | Generated TypeScript is type-identical to 7.6.1; only JSDoc/comments changed |
| `api`: `ajv` / `ajv-formats` / `yaml` | 8.20.0 / 3.0.1 / 2.9.1 | |
| `frontend`: `@playwright/test` | 1.64.1 | Browser revision installed via `npx --no-install playwright install chromium` |
| `frontend`: `vite` / `vitest` | 8.3.4 / 5.0.5 | |
| `frontend`: `react` / `react-dom` | 19.3.0 | |
| `frontend`: `typescript` | 6.0.3 | Held; see below |
| `frontend`: `typescript-eslint` | 8.71.1 | |
| `frontend`: `@axe-core/playwright` (+ `axe-core`) | 4.14.0 | MPL-2.0; test-only, runs WCAG scans in production E2E, not shipped in the bundle |

## Upgrading a pin

1. Go, Node.js, sqlc: change the version in `mise.toml`, run `mise lock --platform linux-x64,linux-arm64,macos-x64,macos-arm64` in the checkout (the wrapper and CI refuse a stale lock), and commit `mise.toml` and `mise.lock` together. Check the new checksums against upstream (Go `https://go.dev/dl/?mode=json&include=all`, Node.js `SHASUMS256.txt`, sqlc release asset digests via `gh api repos/sqlc-dev/sqlc/releases/tags/<tag> --jq '.assets[] | [.name, .digest]'`). Go and Node.js version changes also update the Dockerfile images in the same branch.
2. mise in CI: update `version` and `sha256` (the `mise-v<version>-linux-x64` entry of the release's `SHASUMS256.txt`) in `.github/actions/toolchain/action.yml`. Raise `min_version` in `mise.toml` only when the configuration needs a newer mise.
3. Playwright: update `@playwright/test` with npm; `npx --no-install playwright install chromium` installs the browser revision that the locked package expects.
4. Run the complete gate through the wrapper. Tools install side by side under the wrapper's private directory, so a candidate version never replaces what other branches or tools use.
5. For a major version, read the upstream release notes and migration guide first and record behavior changes under a migration-notes heading in this file.

## Renovate maintenance

[`renovate.json`](../../renovate.json) uses Mend-hosted Renovate with `config:recommended`, the Dependency Dashboard, at most three regular PRs/branches and one new regular PR per hour. Ordinary releases wait three days; sources without a release timestamp remain eligible rather than being silently disabled. GitHub vulnerability-alert fixes skip the normal queue and age delay, carry `security`, and require review. Dependabot alerts are enabled for this repository; Dependabot's separate update bot is not enabled. Alert delivery also requires the Renovate app's permission to read alerts; `npm audit` remains a mandatory independent gate.

### Detection and synchronization

| Dependency | Detection | Completion boundary |
| --- | --- | --- |
| npm packages | Native npm manager, both package manifests and lockfiles | Install/audit, API or frontend checks. React/types, Tailwind/compiler integration and ESLint/core configuration have narrow groups. Required indirect Go modules are enabled too. |
| Go modules | Native gomod manager | Module graph/sums, release notes, race/integration/build checks; dependencies may require a deliberate language-minimum migration. |
| Go runtime | Native mise manager (`mise.toml`) and Dockerfiles | Grouped proposal; developer refreshes `mise.lock`, synchronizes image pins and runs the gate through `dev-exec.sh`. Go's `1.x` language releases are semver-minor but never automerge. The `go` directive in `backend/go.mod` is a deliberate minimum, not an automatically bumped runtime. |
| Node.js | Native mise manager (`mise.toml`) and Dockerfile | Node versioning admits LTS, not Current, retaining image suffixes; developer refreshes `mise.lock`. Package engines are compatibility ranges, not authoritative runtime pins. |
| sqlc | Native mise manager (`mise.toml`, aqua backend) | Developer refreshes `mise.lock`, regenerates code and passes the drift check. |
| mise | Native GitHub Actions manager (`jdx/mise-action` `version`/`sha256` inputs) | Renovate proposes the version and checksum together; developer checks release notes and `min_version` compatibility. |
| oapi-codegen | `api/Makefile` regex against official GitHub releases | Regenerate Go output, migrate behavior and validate contracts. |
| openapi-typescript / Playwright | Native npm manager | Package and lockfile agree; generator drift or Playwright E2E checks must pass. |
| PostgreSQL | Native Compose and workflow service images | Synchronize server/client/configuration pins and integration fixtures. Majors require an explicit data-preserving migration and backup/restore validation. |
| Docker images | Native Dockerfile/Compose/workflow managers; proxy-script regex | One image family per PR. Nginx is restricted to even-minor stable releases. Distroless `nonroot` tags are digest-pinned/updated; switching Debian image families is a developer migration, not version discovery. |
| GitHub Actions | Native workflow manager, including the composite `.github/actions/toolchain` | Each action groups its own workflow references, not unrelated actions. GitHub runner `ubuntu-latest` is intentionally floating, not a version pin. |

The test-provider Dockerfile under `backend/test/` is explicitly included by overriding the recommended test-directory exclusions. Compose overrides without images still remain supported package-file locations. Documentation versions, shell installer comparisons, PostgreSQL client-major assertions and generated headers are not rewritten automatically. Different datasources may publish at different times, so grouping does **not** promise a complete synchronized upgrade.

Hosted Renovate cannot safely be assumed to execute arbitrary `postUpgradeTasks`; none are configured. Renovate treats `mise lock` as an unsafe execution, so a `mise.toml` proposal may arrive without a refreshed `mise.lock`; required CI then fails at `mise install --locked` until the developer refreshes the lock, which is the intended fail-closed boundary. The autonomous developer finishes incomplete proposals following `.autonomous/FLOW.md` **Dependency maintenance**, which already requires open-PR triage, candidate-branch toolchain provisioning, migration/release-note review, the complete native local gate, protected merge and green main CI. Agent/controller instructions are unchanged.

### Merge policy

Automerge is opt-in only for patch/minor updates to `clsx`, `tailwind-merge`, `ajv`, `ajv-formats`, `yaml`, `@testing-library/jest-dom`, `@testing-library/user-event` and `github.com/go-chi/chi/v5`, with current versions at least 1.0.0 and no grouping with other packages. All majors, pre-1.0 updates, runtime/toolchain/images/actions, generators, Playwright, PostgreSQL and security-alert PRs require developer review. Unlisted dependencies do not automerge.

Renovate performs **PR-based** automerge (`automergeType=pr`, `platformAutomerge=false`, `ignoreTests=false`) after checks pass; this works without enabling GitHub's repository-native auto-merge setting. It never uses direct-main/branch automerge. Strict `required` protection, enforced for administrators, still requires an up-to-date branch and resolved conversations. The `required` aggregate includes lockfile-verified toolchain installation in every job, generated-output drift, audits, native integration/E2E and deployment/backup checks. A version-only toolchain proposal is not complete while any of these fail; never merge it merely to test Renovate.

### Validate a configuration change

Use the official validator through the pinned runtime (temporary npm tool cache only; no project dependency or new manager):

```sh
bash scripts/dev-exec.sh -- npm exec --yes --package=renovate -- renovate-config-validator --strict renovate.json
```

For extraction/lookup diagnostics, use the official Renovate CLI with `--platform=local --dry-run=extract` or `--dry-run=lookup` in the checkout. Provide `GITHUB_COM_TOKEN` privately for GitHub datasources; local-platform `RENOVATE_TOKEN` alone does not authenticate those lookups. Never print or commit tokens or raw authenticated logs. A local dry run does not prove the hosted app has run; inspect its Dashboard and first actual PR after onboarding.

## Security overrides

- **minimatch 11.0.3** ([GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249), published 2026-10-30, moderate: `**/..` glob-part amplification at `optimizationLevel >= 2`, fixed only in 11.0.1+; no 10.x/5.x backport). Both manifests carry an npm `overrides` entry until upstream depends on minimatch 11 (tracked in eslint/eslint#21463; openapi-typescript 7.13.0 still requires the archived `@redocly/openapi-core` 1.x, which pins minimatch ^5). Tendo never passes untrusted glob patterns. All three are build-time dev tools, so the shipped bundle and server are unaffected.
  - `frontend`: global override. `eslint`, `@eslint/config-array` and `@typescript-eslint/typescript-estree` only use `new Minimatch(pattern, { dot, flipNegate })`, `.match(path, partial)` and the named `minimatch()` export, all of which 11.x keeps. ESLint's per-file results (files, rule IDs, lines) are byte-identical before and after. 616 option/path/partial combinations of the config patterns match identically on 10.2.6 and 11.0.3.
  - `api`: override scoped to `@redocly/openapi-core` 1.x. Its only minimatch use is `require('minimatch')(url, pattern)` in the header-matching loop of `readFileFromUrl`. minimatch 11 is not callable as a module, so that loop would throw once any `resolve.http.headers` are configured. The openapi-typescript 7.13.0 CLI calls `loadConfig`, which also turns Redocly login tokens into headers, only when `findConfig` finds `redocly.yaml` or `.redocly.yaml` in its working directory (`api/`, no parent search). Otherwise it calls `createConfig`, which configures no headers. `@redocly/cli` 2.71.2 bundles its own openapi-core 2.x and is unaffected. `api/dependency-overrides.test.mjs` fails if a Redocly config, a remote `$ref` or an `externalValue` appears, or if openapi-core resolves a minimatch older than 11. Generated `api/generated/*` output is byte-identical.
  - Remove the overrides once upstream releases depend on minimatch ≥ 11.0.1, then confirm `npm audit` is still clean.

## Held upgrades

- **TypeScript 7.0.2**: `typescript-eslint` 8.71.1 declares a `typescript <6.1.0` peer range. TypeScript's documented side-by-side setup (TS 6 for tooling, TS 7 native compiler for type checking) is possible but adds a second compiler; revisit when `typescript-eslint` supports TS 7.

## Node.js 26 LTS migration notes (24.22.2 → 26.13.0)

- **LTS only**: 26.13.0 (2026-10-28) is the first Node 26 LTS release ("Lithium", supported through April 2029) and changes only release metadata from 26.12.1, which was Current. Renovate PR #286 first proposed 26.12.0/26.12.1 and left the former toolchain manifest's Node pin on 24.22.2; all pins now move together to 26.13.0.
- **Bundled tools**: npm 11.19.1 → 11.20.0 (same major, no breaking changes listed); V8 13.6 → 14.6; OpenSSL 3.5.9 unchanged; Undici 8; Temporal enabled by default.
- **Removed or deprecated APIs** (Node 25/26): `http.ServerResponse.prototype.writeHeader()` (use `writeHead()`), legacy `_stream_*` modules, `--experimental-transform-types`, `fs.F_OK`/`R_OK`/`W_OK`/`X_OK` aliases and `SlowBuffer` removed; `module.register()` runtime-deprecated; Corepack no longer bundled. Repository scripts use none of these and Tendo does not use Corepack.
- **libatomic**: since Node 25 official Linux binaries link against `libatomic`. The official `node:*-bookworm-slim` image, GitHub-hosted runners and the mise-installed binary all run it; the Docker web build and every deployment smoke pass. Native Linux developer machines without it must install the distribution package (Debian/Ubuntu `libatomic1`).
- Sources: [v26.0.0](https://nodejs.org/en/blog/release/v26.0.0), [v25.0.0](https://nodejs.org/en/blog/release/v25.0.0), [v26.13.0](https://nodejs.org/en/blog/release/v26.13.0).

## oapi-codegen v2.8.2 notes

- Security release ([GHSA-2vcr-56fq-97rw](https://github.com/oapi-codegen/oapi-codegen/security/advisories/GHSA-2vcr-56fq-97rw), [GHSA-x3wv-qq4q-hj6r](https://github.com/oapi-codegen/oapi-codegen/security/advisories/GHSA-x3wv-qq4q-hj6r)): implicit `oneOf`/`anyOf` discriminator mapping keys and the contents of whole-document external `$ref`s are now validated before reaching generated Go.
- `api/openapi.yaml` uses only local `#/components` references and no implicit discriminator mappings from untrusted specifications. Regenerated output differs from v2.8.1 only in the `Code generated … version` header.

## oapi-codegen v2.8.1 notes

- Security release: strict servers bind the validated request body, `allow-remote-refs-from` (default `"*"`) gates remote `$ref`s, and specification strings reaching generated Go are quoted or validated.
- Tendo generates models only (`strict-server: false`), and `api/openapi.yaml` uses only local `#/components` references, so none of these paths apply. Regenerated output differs from v2.8.0 only in the `Code generated … version` header; `scripts/check-api-generation.sh` pins that header.

## oapi-codegen v2.8.0 migration notes

- **Nullability**: v2.8.0 maps OpenAPI 3.1 `type: [T, 'null']` and `oneOf: [..., {type: 'null'}]` to `*T` natively. `x-go-type` now names the base Go type, so pointer hints (`x-go-type: '*string'`) stack into `**T`. Response schemas therefore carry no pointer `x-go-type` hints; `api/check-generation.mjs` fails if a required nullable response property is not a single, always-serialized pointer. Request schemas keep their `x-go-type: '*T'` hints so the generated optional request models stay `**T` (omitted / null / value), exactly as with v2.7.2; requests are decoded by `httpx.ReadObject`, not these models. The readOnly `Item.lastCompletedOn` keeps its v2.7.2 `**string` shape, always set by the adapter.
- **Enum constants**: colliding enum values are now prefixed with the type name (for example `SubjectValidationProblemCodeInvalidLength`), and `const` values generate enums (`OIDCValidationProblemStatus`). Wire values are unchanged; no adapter referenced the renamed constants.
- **Security scopes**: `SessionCookieScopes`/`SetupTokenScopes` and their context-key types are no longer emitted. They were never used for enforcement. `backend/cmd/tendo/security_contract_test.go` instead walks the production route composition and checks every operation against its declared `security` requirement. The `enable-auth-scopes-on-context` compatibility flag is intentionally not used.
