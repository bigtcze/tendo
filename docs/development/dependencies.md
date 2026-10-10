# Dependency and toolchain inventory

Snapshot date: 2026-10-09. The machine-readable source of truth for toolchain pins is [`toolchain.json`](../../toolchain.json); `scripts/test_toolchain_manifest.py` (run by the `backend` CI job and the local gate) fails when the workflow, Dockerfiles, Compose file, scripts, generated-code headers, or package manifests drift from it. Update the manifest and every pin in the same change.

## Runtimes, images, and native tools

| Component | Pinned | Newest checked | Status |
| --- | --- | --- | --- |
| Go toolchain (CI, Docker build, OIDC test provider) | 1.27.2 | 1.27.2 | Current |
| `go` directive (`backend/go.mod`) | 1.26.0 | — | Minimum language version; builds use the pinned toolchain |
| Node.js (CI, Docker web build) | 24.21.0 | 24.21.0 (LTS) | Current; package engines still allow `^22.22.2` for local work |
| PostgreSQL (Compose, CI services) | 18.6 | 18.6 (19 in beta) | Current |
| sqlc | 1.31.1 | 1.31.1 | Current; release archive SHA-256 verified against the GitHub release asset digest |
| mise (installer used by `scripts/dev-exec.sh`) | 2026.10.7 | 2026.10.7 | Current; release archive SHA-256 verified against the GitHub release asset digest; MIT |
| oapi-codegen | v2.8.0 | v2.8.0 | Current; see migration notes below |
| Distroless runtime images | `base-debian13`, `static-debian13` (`nonroot`) | same | Current; binaries are static (`CGO_ENABLED=0`) |
| Proxy smoke images | Caddy 2.11.7, Nginx 1.30.5 (stable), Traefik v3.7.14 | same | Current within supported lines |
| GitHub Actions | checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0 | same | Current; exact release tags; actions run on Node 24 (runner ≥ 2.327.1) |

## Go modules (`backend/go.mod`)

| Module | Version | Type |
| --- | --- | --- |
| `github.com/coreos/go-oidc/v3` | v3.21.0 | direct |
| `github.com/go-chi/chi/v5` | v5.3.2 | direct |
| `github.com/jackc/pgx/v5` | v5.11.0 | direct |
| `golang.org/x/crypto` | v0.58.0 | direct |
| `golang.org/x/oauth2` | v0.37.0 | direct |
| `github.com/go-jose/go-jose/v4` | v4.1.5 | indirect |
| `github.com/jackc/puddle/v2` | v2.2.3 | indirect |
| `golang.org/x/sync` | v0.24.0 | indirect |
| `golang.org/x/sys` | v0.49.0 | indirect |
| `golang.org/x/text` | v0.43.0 | indirect |

All direct and required indirect modules are at their latest releases. `go list -m -u all` lists only newer versions for module-graph entries that do not enter the build.

`govulncheck` (x/vuln v1.8.0, Go 1.27.2): no reachable vulnerabilities. It reports GO-2026-5932 in `golang.org/x/crypto/openpgp`, which Tendo does not import or call, and for which no fixed version exists.

## Node packages

`npm outdated` is clean for `api/` and `frontend/` except `typescript` (see below). `npm audit` reports zero vulnerabilities for both.

| Package | Pinned | Notes |
| --- | --- | --- |
| `api`: `@redocly/cli` | 2.62.1 | |
| `api`: `openapi-typescript` | 7.13.0 | Generated TypeScript is type-identical to 7.6.1; only JSDoc/comments changed |
| `api`: `ajv` / `ajv-formats` / `yaml` | 8.20.0 / 3.0.1 / 2.9.1 | |
| `frontend`: `@playwright/test` | 1.64.0 | Browser revision installed via `npx --no-install playwright install chromium` |
| `frontend`: `vite` / `vitest` | 8.3.4 / 5.0.3 | |
| `frontend`: `react` / `react-dom` | 19.3.0 | |
| `frontend`: `typescript` | 6.0.3 | Held; see below |
| `frontend`: `typescript-eslint` | 8.71.1 | |

## Upgrading a pin

1. Change `toolchain.json` and every pin it lists in the same branch; `scripts/test_toolchain_manifest.py` names any you missed.
2. For a release archive (sqlc, mise), take the SHA-256 from the release asset digest (`gh api repos/<owner>/<repo>/releases/tags/<tag> --jq '.assets[] | [.name, .digest]'`), never from a local download alone.
3. Playwright: update `@playwright/test` with npm so `frontend/package-lock.json` and `toolchain.json` agree; the wrapper derives the Chromium revision from the lockfile.
4. Run `bash scripts/dev-exec.sh --versions`, then the complete gate through the wrapper. Both use the branch's own pins, so a candidate version never replaces what other branches or tools use.
5. For a major version, read the upstream release notes and migration guide first and record behavior changes under a migration-notes heading in this file.

## Held upgrades

- **TypeScript 7.0.2**: `typescript-eslint` 8.71.1 declares a `typescript <6.1.0` peer range. TypeScript's documented side-by-side setup (TS 6 for tooling, TS 7 native compiler for type checking) is possible but adds a second compiler; revisit when `typescript-eslint` supports TS 7.

## oapi-codegen v2.8.0 migration notes

- **Nullability**: v2.8.0 maps OpenAPI 3.1 `type: [T, 'null']` and `oneOf: [..., {type: 'null'}]` to `*T` natively. `x-go-type` now names the base Go type, so pointer hints (`x-go-type: '*string'`) stack into `**T`. Response schemas therefore carry no pointer `x-go-type` hints; `api/check-generation.mjs` fails if a required nullable response property is not a single, always-serialized pointer. Request schemas keep their `x-go-type: '*T'` hints so the generated optional request models stay `**T` (omitted / null / value), exactly as with v2.7.2; requests are decoded by `httpx.ReadObject`, not these models. The readOnly `Item.lastCompletedOn` keeps its v2.7.2 `**string` shape, always set by the adapter.
- **Enum constants**: colliding enum values are now prefixed with the type name (for example `SubjectValidationProblemCodeInvalidLength`), and `const` values generate enums (`OIDCValidationProblemStatus`). Wire values are unchanged; no adapter referenced the renamed constants.
- **Security scopes**: `SessionCookieScopes`/`SetupTokenScopes` and their context-key types are no longer emitted. They were never used for enforcement. `backend/cmd/tendo/security_contract_test.go` instead walks the production route composition and checks every operation against its declared `security` requirement. The `enable-auth-scopes-on-context` compatibility flag is intentionally not used.
