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
| oapi-codegen | v2.4.1 | v2.8.0 | Held; see generator notes |
| Distroless runtime images | `base-debian12`, `static-debian12` (`nonroot`) | `debian13` variants | Held; base OS change is a separate upgrade |
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

## Held upgrades

- **TypeScript 7.0.2**: `typescript-eslint` 8.71.1 declares a `typescript <6.1.0` peer range. TypeScript's documented side-by-side setup (TS 6 for tooling, TS 7 native compiler for type checking) is possible but adds a second compiler; revisit when `typescript-eslint` supports TS 7.
- **oapi-codegen v2.5.1–v2.7.2**: v2.5.1 output differs only in the version header. v2.6.0 and v2.7.2 add enum `Valid()` methods and typed security-scope context keys; the output builds and passes vet and unit tests unchanged. Adopting either is a separate generator change.
- **oapi-codegen v2.8.0**: breaking for this contract. Nullable fields become double pointers, enum constant names change, and OAuth scope constants are removed. Generated code does not compile against current adapters without migration.
- **Debian 13 distroless images**: base-OS change lands as a separate, focused change with full CI evidence.
