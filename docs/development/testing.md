# Development and verification

## Prerequisites

The complete local gate targets Go 1.27.3, a C compiler (`gcc`) for race-enabled tests, Python 3, Node.js 26.13.0 with npm, native PostgreSQL `psql` 18, native `sqlc` 1.31.1, and Playwright 1.64.1 with its matching Chromium, Firefox and WebKit browsers. Repository-pinned generators and test tools include oapi-codegen v2.8.2 (run by `api/Makefile` with `go run`), openapi-typescript 7.13.0 and `@playwright/test` 1.64.1. Node 22.22.2+ remains compatible with package engines; CI and the Docker web build use Node 26.13.0. See [dependency and toolchain inventory](dependencies.md).

## Pinned toolchain

[`mise.toml`](../../mise.toml) pins Go, Node.js and sqlc; [`mise.lock`](../../mise.lock) records each tool's official download URL and SHA-256 for Linux and macOS on x64 and arm64. Install [mise](https://mise.jdx.dev/installing-mise.html) 2026.11.0 or newer (`min_version` enforces it) as a user-level binary; verify a manually downloaded archive against the release's `SHASUMS256.txt`. No shell activation, global config or `mise trust` is needed.

`scripts/dev-exec.sh` runs any command with exactly those tools:

```sh
bash scripts/dev-exec.sh -- bash -c 'go version; node --version; sqlc version'
bash scripts/dev-exec.sh -- bash scripts/verify-local.sh    # complete pre-PR gate with the pins
bash scripts/dev-exec.sh -- bash -c 'cd backend && go test ./...'
```

It runs `mise install --locked`, so a tool missing from `mise.lock` or a downloaded archive whose SHA-256 differs is an error, never a fallback to another version. mise checks archives when it installs them and afterwards trusts the install directory, so before running the command the wrapper also confirms that `go`, `node`, `npm` and `sqlc` resolve inside its private install tree (never a host binary) and that `go`, `node` and `sqlc` report the pinned versions. If a cached install is damaged, delete `${XDG_CACHE_HOME:-~/.cache}/tendo/mise/data/installs` to re-download and re-verify it. The wrapper ignores inherited `MISE_*` variables, your user, system and parent-directory mise configuration, system-wide mise installs and Node.js/Go default-package files (`~/.default-npm-packages` and its variants, `~/.default-go-packages`), and keeps installs under `${XDG_CACHE_HOME:-~/.cache}/tendo/mise`, so other mise projects and global installs are neither read nor changed. Cancelling the wrapper while it installs also stops the installer. It sets `GOTOOLCHAIN=local` (Go never downloads another toolchain) and `GOENV=off` (settings saved with `go env -w` are ignored); explicit `GO*` variables in your environment still apply. The command replaces the wrapper process, so exit status and signals are the command's own.

Playwright Chromium, Firefox and WebKit come from the locked `@playwright/test` package itself: `verify-local.sh` runs `npx --no-install playwright install chromium firefox webkit` in `frontend/`, which installs matching revisions into Playwright's standard cache (`~/.cache/ms-playwright`). Their system libraries are a one-time host step (Debian/Ubuntu: `sudo npx --no-install playwright install-deps chromium firefox webkit` from `frontend/`). The host also needs Python 3, `gcc`, `make` and `psql` 18.

CI uses the same `mise.toml`/`mise.lock` through [`.github/actions/toolchain`](../../.github/actions/toolchain/action.yml): `jdx/mise-action` installs a version- and SHA-256-pinned mise, then `mise install --locked`. A wrong lock checksum therefore fails required CI.

The existing PostgreSQL service is not managed by the wrapper; set `TENDO_TEST_POSTGRES_ADMIN_URL` as described below.

The local integration and E2E layers use a dedicated native PostgreSQL service; Playwright Chromium runs the full journey, with bounded Firefox and WebKit browser smoke projects. Without the wrapper, install the pinned versions from trusted sources.

## Dedicated PostgreSQL service setup

Use a dedicated PostgreSQL 18 test instance, never a production/shared application database. It needs a `postgres` maintenance database and this pre-provisioned shared role:

```sql
CREATE ROLE tendo NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
```

Run the prerequisite SQL once as the administrator of that isolated test instance. The harness validates the role is NOLOGIN, has no elevated attributes and inherits no other roles. It never changes shared-role attributes/password or grants membership to shared `tendo`; it only grants the fixed role to each freshly-created per-run runtime login. Migrations retain existing permissions granted to `tendo`.

Export the service admin URL privately in the shell, replacing host, password and CA path with values for the dedicated service. This example uses a TLS-verifying connection and does not echo the password or URL:

```sh
read -rsp 'Dedicated test-service PostgreSQL admin password: ' PG_TEST_ADMIN_PASSWORD; printf '\n'
PG_TEST_ADMIN_PASSWORD_ENC=$(PG_TEST_ADMIN_PASSWORD="$PG_TEST_ADMIN_PASSWORD" python3 -c 'import os, urllib.parse; print(urllib.parse.quote(os.environ["PG_TEST_ADMIN_PASSWORD"], safe=""))')
export TENDO_TEST_POSTGRES_ADMIN_URL="postgresql://postgres:${PG_TEST_ADMIN_PASSWORD_ENC}@127.0.0.1:5432/postgres?sslmode=verify-full&sslrootcert=/path/to/test-service-ca.pem"
unset PG_TEST_ADMIN_PASSWORD PG_TEST_ADMIN_PASSWORD_ENC
```

For a local disposable PostgreSQL instance only, `sslmode=disable` is acceptable. The URI must select the dedicated server's `postgres` maintenance database; the value is a high-privilege service admin connection, must not target production, must not be stored in repo config, and must never be printed. The test helpers remove the admin URL from child environments. Service setup is operator-specific and deliberately does not create/alter/drop the shared grant role. Per-run migrator/runtime roles, databases and upgrade fixtures are unique; OID/owner-checked cleanup deletes only positively-owned resources. Collisions fail without adoption/deletion. SIGKILL cannot perform cleanup, so uniquely-named resources can remain for later ownership-verified cleanup. Never broad-drop cluster roles/databases or use `DROP OWNED`.

## Targeted checks and complete pre-PR gate

For focused debugging, run the relevant standard check directly:

```sh
(cd backend && go test ./path/to/package)
(cd frontend && npm test -- --run path/to/test)
bash scripts/setup-integration.sh  # requires the dedicated PostgreSQL service
bash scripts/check-sqlc.sh
bash scripts/e2e-smoke.sh          # production-build browser suite
```

Run the complete local gate with the pinned tools:

```sh
bash scripts/dev-exec.sh -- bash scripts/verify-local.sh
```

The gate runs gofmt, golangci-lint (vet, static analysis, structure), Go unit-race/integration/sqlc/build, API and frontend checks, then production E2E. A local pass never replaces required GitHub checks.

## Maintainability checks

Structural review triggers from `.autonomous/CODE_QUALITY.md` are enforced with standard, maintained linters; there is no project-specific metrics tool.

| Code | Tool and configuration | Thresholds |
|---|---|---|
| Authored frontend `src/**/*.ts(x)` | ESLint core rules in `frontend/eslint.config.js` | `complexity` 15, `max-depth` 4, `max-lines-per-function` 80, `max-lines` 350 non-blank |
| Authored Go | [golangci-lint](https://golangci-lint.run) v2 in `backend/.golangci.yml` (pinned in `mise.toml`) | `gocyclo` 15, `gocognit` 30, `funlen` 80 lines including signature and closing brace, revive `max-control-nesting` 4 and `file-length-limit` 500 non-blank lines |

`scripts/autonomous-loop-test.sh` runs the real V4 controller loop (`scripts/autonomous-loop.sh`) from disposable git checkouts with fake `systemctl` and `opencode` executables. It proves the loop refuses to start a cycle while the post-V3 maintenance timer/service is enabled, active, unreadable or holding its lock, holds the maintenance lock during a cycle, stops on a re-enabled timer, and exits only on `V4_COMPLETE` tracked on clean `main`. It runs first in the local gate and in the required `backend` job; it needs only Bash, Git, `flock` and `timeout`.

Go formatting stays a separate `gofmt -l` step over every `.go` file under `backend/`, generated and build-tagged files included, because golangci-lint skips generated files. golangci-lint also runs its `standard` set: `errcheck`, `govet` with all analyzers except `fieldalignment` and `shadow`, `ineffassign`, `staticcheck` and `unused`. Generated code (`Code generated ... DO NOT EDIT`: sqlc and oapi-codegen output) is not linted. Tests are excluded from the structural thresholds, and unchecked `Close` errors are allowed in tests only, for routine cleanup of test resources; a test whose correctness depends on a close result, such as flushing a written fixture file, must still check it.

Metric semantics, so reviewers know what each check does and does not catch:

- `gocyclo` counts the closures in a function toward that function, so a handler with inline callbacks is measured as a whole. Function literals assigned directly to package variables are measured on their own; literals wrapped in a call, such as `http.HandlerFunc(func ...)`, are not.
- revive's `max-control-nesting` counts `if`/`else`, `for`, `switch` and `select` cases, and counts each `else if` as one level deeper. It does not count `for ... range`. `gocognit`, which weights nested control-flow constructs by their depth, is a backstop rather than a depth cap: an otherwise simple chain of up to seven nested range loops still passes, so range nesting beyond four levels remains a review responsibility. `gocognit` does not measure package-level function literals on their own either.
- `funlen` measures declared functions only, not function literals.

Exceptions are explicit and reviewed. Go has no per-function exceptions; code over a threshold is refactored. A new exception must be an exact `exclusions.rules` entry with a rationale and owner, and `warn-unused` reports entries that no longer match. The two frontend locale catalogs are exempt from `max-lines` because they are flat translation tables. Inline ESLint configuration is disabled for authored source, so frontend rule exceptions are exact-file overrides in `frontend/eslint.config.js` with a rationale and owner.

History: until #359 these checks used a Python ratchet with JSON baselines, a frontend lint-ratchet framework and a Go `funcmetrics` tool. #359 replaced them with ESLint rules and a 413-line custom Go AST checker (`backend/tools/structcheck`), which #367 replaced with golangci-lint. golangci-lint then reported deferred transaction rollbacks whose errors were silently dropped (they now discard the error explicitly, as the other repositories already did), dead test helpers, ineffectual test assignments and staticcheck simplifications; all of these were fixed in code rather than excluded. The eight test-support functions the old checker excepted (the in-memory item repository, the test OIDC provider and its standalone server) were refactored under the default thresholds.

## CI job map

The required workflow retains one stable branch-protection aggregate check named `required`. Backend, API, frontend, PostgreSQL, browser E2E, visual regression and a nine-entry deployment-smoke matrix run independently. The aggregate requires all seven job results to succeed. Every job installs Go, Node.js, sqlc and golangci-lint through `.github/actions/toolchain` (`mise install --locked`), so CI and `dev-exec.sh` use the same verified archives.

| Job | Commands and prerequisites |
|---|---|
| `backend` | Go 1.27.3 from `mise.lock`; gofmt, `golangci-lint run ./...` (go vet, staticcheck, structure), unit race tests excluding four PostgreSQL adapters, and `go build ./...` |
| `api` | Go 1.27.3, Node 26.13.0, API npm cache; `npm ci`, moderate audit, `npm run check` |
| `frontend` | Node 26.13.0/frontend npm cache; `npm ci`, moderate audit, full `npm run check` |
| `postgres` | PostgreSQL 18.6 service, native psql 18 and sqlc 1.31.1; `tendo` NOLOGIN role bootstrap, race-enabled integration suite and sqlc drift check |
| `e2e` | Separate PostgreSQL 18.6 service, native psql18 and `tendo` bootstrap; Go setup, Node 26.13.0, frontend `npm ci`, Playwright 1.64.1 Chromium/Firefox/WebKit install, full native production-build E2E (Chromium journey plus Firefox/WebKit smoke) |
| `deployment` matrix, `fail-fast: false` | Nine entries: setup (first-owner/session/API plus runtime health, outage recovery, container hardening and SIGTERM shutdown), database startup (fresh bootstrap, role flags/memberships, no-op restart, concurrent starts and refusal/recovery for bad administrator credentials, dirty or newer schema, elevated role and admin `DATABASE_URL`), OIDC, proxy, household backup (full restore with `ensure-role` plus refused existing-target and truncated-archive restores), upgrade from v1.0.0, upgrade from v2.1.0, upgrade from v3.0.0, and quick start. Each checks out and installs Go/Node/API dependencies before running its Docker smoke. The upgrade smoke (`TENDO_UPGRADE_FROM`, default `v1.0.0`) starts the source release from that release's own published `compose.yaml` and image, populates only through the public API, then upgrades the same PostgreSQL volume to the checkout's two-service Compose and build with `--remove-orphans`; it compares table counts, the migration ledger and captured response bodies/ETags, and exercises current APIs. The quick-start smoke extracts and executes the install commands from the documentation. Each matrix job builds its own images; image transfer is YAGNI absent measured evidence for artifact handoff. Proxy, database startup, household-backup and upgrade retain their 12/15 minute bounds using per-matrix-entry `timeout` values. |
| `required` aggregate | `if: always()`, `needs` every other job, prints results and jq-requires exactly seven job successes (the deployment matrix is one job). |

### CI E2E failure artifacts

When the required `e2e` job fails, it stages two independent outputs for 14 days in a unique per-run directory under `runner.temp`, and the upload step runs only when the E2E step reports `ci_upload=ready` (emitted only after a successful single-directory rename):

- Sanitized public text: `process.redacted.log`, sanitized JSON reports, and sanitized `error-context.md` files. Redaction uses registered secret values and their predictable encodings plus structural and high-entropy patterns; it is best-effort for arbitrary free-form transformations, not an absolute credential-removal guarantee. Structured JSON stdout/stderr attachments are removed.
- Encrypted diagnostics: `diagnostics.tar.gpg` contains the run's Playwright HTML reports, `artifacts/` (traces of every browser context, failure screenshots, error context), JSON reports, `attention-clock.json`, `process.redacted.log`, and an inner `MANIFEST.json` with each file's path, size, and SHA-256. Raw `process.log` is never included. It is OpenPGP-encrypted (AES-256, integrity-protected) to the committed public certificate `ci/e2e-artifact-recipient.asc`, primary fingerprint `249E445F0DB60EC10F16136EA1E1C31A529EE4CA` (encryption subkey `D21649734E51A26906BF8861929F3886A26BDE16`); the runner verifies that fingerprint in a fresh temporary GnuPG home before encrypting. The public `diagnostics.manifest.json` records only format version, recipient fingerprint, ciphertext SHA-256 and size, and run identifiers.

Staging is fail-closed. If encryption fails for any reason (missing `gpg`, wrong or expired certificate, timeout, size limits of 10,000 files, 256 MiB per file, 512 MiB total, or 550 MiB ciphertext), only an `ENCRYPTED_DIAGNOSTICS_FAILED` marker with a fixed reason code is staged; there is never a plaintext fallback. Playwright starts the app and OIDC provider through its `webServer` configuration; each server receives only `PATH`, `HOME`, `TMPDIR`, `LANG` and its own settings, never the browser-only credentials. The runner stops Playwright with SIGINT so it shuts those servers down, then waits for every process group Playwright opened and SIGKILLs any survivor. On cancellation, stopping children is bounded to about 8 seconds and the whole cleanup, including encrypted staging, to 22 seconds, inside the PostgreSQL wrapper's 25-second grace; SIGKILL cannot run cleanup, so a private plaintext temp under `runner.temp` may remain on the ephemeral runner (never in the upload directory). If text sanitization fails, `SANITIZE_FAILED` replaces the text set and the encrypted bundle is still staged. If rename or output fails, `ready` is not emitted and nothing is uploaded. The `e2e` job does not reference any `secrets.*` values; the private key never enters CI. GnuPG is a runner/host system prerequisite (preinstalled on GitHub Ubuntu runners). Artifacts are downloadable by anyone because this is a public repository, which is why traces and screenshots are only uploaded encrypted.

Download with `gh run download <run-id> -n e2e-failure-<run-id>-<run-attempt>`. Older `gh` releases (for example 2.46) reject this archive with "would result in path traversal"; then fetch the zip directly with `gh api repos/bigtcze/tendo/actions/artifacts/<artifact-id>/zip > e2e-failure.zip`, taking the id from `gh api repos/bigtcze/tendo/actions/runs/<run-id>/artifacts`. Decrypt only in a private directory with the private key holder's GnuPG home:

```sh
umask 077
mkdir diagnostics-private
sha256sum diagnostics.tar.gpg   # must match diagnostics.manifest.json
gpg --decrypt diagnostics.tar.gpg | tar -x -C diagnostics-private
python3 -c 'import hashlib, json, pathlib, sys; root = pathlib.Path(sys.argv[1]); bad = [e["path"] for e in json.loads((root / "MANIFEST.json").read_text()) if hashlib.sha256((root / e["path"]).read_bytes()).hexdigest() != e["sha256"]]; print(bad or "all hashes match"); sys.exit(bool(bad))' diagnostics-private
```

From `frontend/`, open a trace with `npx playwright show-trace <trace.zip>` and a report with `npx playwright show-report <html-report-dir>`. Never upload decrypted content to public trace viewers or issues, and delete the private directory after use.

Key lifecycle: one active recipient. Rotate by replacing the certificate and fingerprint constant in a PR and re-proving decryption with an intentionally failing run. Keep old private keys until artifacts encrypted to them expire. A lost private key makes existing ciphertext unrecoverable: record the evidence gap and rotate. On compromise, rotate and delete affected artifacts (already downloaded ciphertext cannot be retracted).

The native browser suite is not a replacement for deployment smokes. Setup/API with production runtime, database startup, OIDC API, all reverse proxies, household backup/restore, published v1.0.0, v2.1.0 and v3.0.0 data upgrades, and the copy-paste quick start remain mandatory CI jobs. Run the upgrade check locally with `bash scripts/upgrade-smoke.sh` (or `TENDO_UPGRADE_FROM=v2.1.0` / `TENDO_UPGRADE_FROM=v3.0.0`); it uses a unique Compose project and volume, pulls the public `ghcr.io/bigtcze/tendo` source release image, downloads that release's own `compose.yaml`, and removes its containers, volume, and private temporary data on exit. To stress browser synchronization under slower reads, set `TENDO_E2E_READ_DELAY_MS` to a positive integer number of milliseconds when running `scripts/e2e-smoke.sh`; the runner delays ordinary household API GETs from the main journey browser context before forwarding them to the server (separately created member and accessibility contexts, and requests a test fulfils or routes itself, are not delayed). In this mode the default per-test timeout rises from 30 to 90 seconds because the slowed reads accumulate; tests that set their own timeout keep it. The option defaults off and does not affect required CI runs.

### Refresh the README screenshot

With the dedicated PostgreSQL service and pinned tools available, run:

```sh
TENDO_README_SCREENSHOT=$PWD/docs/images/v3-overview.png bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

The opt-in capture builds and runs the production app against real PostgreSQL, uses the fixed test clock `2026-05-14T08:00:00Z`, and creates realistic household data through the public API. It writes the 1280×800 full-page desktop image to the given path and a 390×844 phone viewport image next to it with a `-phone` suffix (`docs/images/v3-overview-phone.png`). It is separate from the required E2E journeys and is not part of CI.

### Required visual regression suite

`TENDO_VISUAL_MODE=1 bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` builds the production binary, creates isolated PostgreSQL, starts a fresh app with fixed server clock `2026-05-14T08:00:00Z`, then runs the Chromium-only `visual` project. Browser time is fixed to the same instant. Invitation timestamps are server wall-clock values outside the test clock's item-business-date scope, so the owner and theme-matrix visual contexts pin `createdAt`/`expiresAt` (and any `acceptedAt`/`revokedAt`) in real invitation-list responses to that instant plus the 7-day lifetime; nothing else in the response changes. Every browser context uses locale `en-US` and time zone `Europe/Prague`, so CI runners in UTC render the same onboarding default and dates. A serial route-group suite captures first-run onboarding and empty Home before API fixtures are seeded; subsequent fixtures use real API calls, and route tests share the saved owner session. Coverage includes Home/add/Repeat, All items/no-match, item detail open/done/archived, edit/412 conflict, People current/archived, Members owner/non-owner, Account, signed-out Login/error, Czech Home/add/detail, Home and All-items retry errors, and a final 1,000-active-item scale case. Onboarding, Home, the add form and All items have 320/390/768/1440 captures. Detail, edit/conflict, Repeat and the other route states are captured at 390/1440. Light/dark screenshots cover onboarding, login, Home, add, All items, detail, People, Members and Account at 390/1440. Czech Home/add/detail are captured at 390/1440. Full-page captures preserve long-content states; responsive and theme matrix captures use viewport images.

The large-data scenario adds 1,000 deterministic `Úkol 0001`… items through the public API sequentially (item ids are uuidv7 and list order tie-breaks on id, so concurrent creation would make row order nondeterministic), after all normal route captures. It checks the API summary count delta, bounds the DOM at `<3000` elements per Home/All-items page (2x headroom over observed bounded-list structure; Home groups cap at 100 and All-items paginates), and caps initial-load household API traffic at `<=8` requests (session plus bounded summary/list reads, never per-record requests). These bounds are measured on the settled screen: no row is still loading its subject label and the network is idle. It logs seeding time, navigation-to-first-row and navigation-to-settled timing without machine-specific timing thresholds. Large Home and All-items are captured at 390/1440.

`toHaveScreenshot` uses the default per-pixel color threshold and `maxDiffPixels: 0`; captures assert viewport, clear focus/pointer, disable animations, hide caret and use reduced motion. No page region is masked. The product now ships self-hosted Inter Variable, so captures render the app's own hashed woff2 from the production bundle; `capture.ts` additionally asserts the computed body family is Inter Variable and that a face is loaded before taking pixels. The visual runner sets `FONTCONFIG_FILE`/`FC_CONFIG_DIR` only for the visual process. `frontend/e2e/visual/fonts/` still commits Liberation Sans/Serif/Mono TTF from Debian `fonts-liberation` 1:2.1.5-3, source https://github.com/liberationfonts/liberation-fonts, SIL OFL 1.1, as the deterministic **system fallback only**; its config reads only committed fonts and aliases the fallback families (`system-ui`, `ui-sans-serif`, `sans-serif`, …). Snapshot paths omit platform suffixes because Chromium and complete font source are pinned. CI job `visual` is included in aggregate `required`; CI never updates snapshots and uploads only encrypted diagnostics through the existing E2E artifact pipeline.

Baseline refresh runs the whole serial suite (later route groups depend on the household created by the first tests) with Playwright `updateSnapshots: 'changed'`, so only mismatching or missing PNGs are rewritten and matching baselines stay byte-identical:

```sh
source ~/.config/tendo-test/env
TENDO_VISUAL_MODE=1 TENDO_VISUAL_UPDATE=1 bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
git status --short frontend/e2e/visual/visual.spec.ts-snapshots/
```

Review every changed PNG image-by-image at full resolution against its previous version, enumerate each changed path in the PR, and explain its intended change; revert any change that is not intended. `TENDO_VISUAL_UPDATE=1` is rejected when `CI` is set. Normal local and CI runs use `updateSnapshots: 'none'`; missing or changed baselines fail.

### UX/scale audit capture

With the dedicated PostgreSQL service, pinned tools, and frontend dependencies available:

```sh
TENDO_UX_AUDIT_DIR=/tmp/opencode/ux-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

This opt-in production-build run captures staged Home (empty, 1, 30, 500, and 1200-item) full-page and viewport-fold screenshots at 320/360/390/768/1024/1440 px in English and Czech; route/state captures at the supported phone and desktop widths; and `metrics.json` (request counts/bytes, render timing, DOM size, rows, horizontal overflow, smallest visible text, targets below 44/24 px with offenders, keyboard tab stops, and scripted journey records). A dark color-scheme capture records the light-only rendering. The server test clock is fixed to 2026-05-14, generated fixture data uses a seeded RNG, and capture viewport is set/asserted before each screenshot. The committed refresh set is under 30 MB; preserve `metrics.json` only after the harness credential/invitation-token assertions pass. Journey steps/interactions/keystrokes and wall times are measurements of scripted Playwright actions against loopback with reduced motion, not human task durations; wall times are inherently machine-dependent. The audit is deliberately excluded from the default browser suite and required CI.

To refresh committed baseline evidence, run the capture above with a dedicated output directory such as `/tmp/opencode/ux-audit`, inspect `metrics.json` for secrets and completeness, then curate the route/state PNGs and metrics into `docs/design/v3-baseline/` and update the screenshot index and metric/journey tables in `docs/design/v3-baseline-audit.md`. Capture filenames and metrics entries use the viewport as a suffix; keep every committed PNG linked in the index. Keep the committed set below 30 MB; do not commit runner logs or reports.

## Assertion coverage map

| Layer | Assertions retained |
|---|---|
| Go static/unit | formatting, vet, all unit packages, race detector |
| PostgreSQL | empty/upgrade migrations; privilege and SQLSTATE 42501 restrictions; rollback/concurrency/persistence/auth/session/household/item/OIDC/invitations |
| SQL generation | identity, household, subject, item complete dbgen trees; changed/missing/extra artifacts |
| API | recommended OpenAPI lint; all three Node files/78 checks; health/source checkers; exact 2 TypeScript and 5 Go output drift |
| Frontend | eslint, typecheck, Vitest, production build, npm audit |
| Runtime | production-image UI embedding, database role/schema startup and readiness, health/outage recovery, request IDs/problems, non-root/read-only, no published DB, bounded shutdown |
| Database startup | fresh two-service bootstrap and health, restricted role flags and no memberships, idempotent restart, concurrent advisory-lock starts, fail-closed and recoverable wrong admin credentials, dirty/newer schema, elevated role and admin `DATABASE_URL` |
| Setup | real response and DB graph; setup/origin/validation/rate limits; session digest/cookies; household isolation; subject/item CRUD/ETags/recurrence/completion/idempotency/undo/responsible; invitations/contracts |
| OIDC | real link/login, callback/replay/password gate/session replacement, unknown identity unchanged DB, denial/origin/log privacy/contracts |
| Proxy | Caddy/Nginx/Traefik TLS, SNI/Host, trusted forwarding, forged-header rejection, Origin, health/SPA headers, OIDC callback/link/login and `__Host-` cookies through HTTPS, outage recovery, secret-free logs |
| Backup | archive round trip, Unicode/null rows, ownership/FK/CHECK/runtime permissions, existing target/truncation safety |
| Household backup | API-populated restore with fresh credentials; counts/ledger/API bodies/ETags/session/password/idempotency/undo/new writes/invitations/restrictions |
| Quick start | Exact install-guide shell blocks and README command consistency; release-asset install into an empty directory with a locally built image, settings permissions/secrets, Compose startup, HTTP first-owner setup/login/household/UI, then token-disabled restart |
| Browser | 43 serial production journeys plus a two-phase fixed-clock midnight test; onboarding, CRUD, authorization, OIDC, viewports/keyboard/localization/security, axe WCAG 2.2 A/AA scans, 200 % zoom-equivalent reflow, real Chromium page zoom at 200 %/400 % (n23) and reduced-motion measurements on household routes/forms; persisted item across app-only midnight restart |

The E2E runner seeds an unowned foreign household as migrator; browser and API assertions verify cross-household denial, and a post-suite database check confirms the seed is unchanged. Home/create/detail layouts are checked at 360, 768 and 1280px widths.

## Historical CI baseline

Five successful required runs on 2026-10-09: median full run 11m19s (range 10m50s–11m45s), median job 11m17s. Median stage times: Go vet 25s; unit race 60s; PostgreSQL integration 93s; sqlc 2s; API install/audit/contract/generation 162s; frontend install/check 39s; production runtime 52s; setup 20s; OIDC 25s; proxy variants 81s; backup safety 9s; household backup 19s; browser E2E 78s. Detailed GitHub logs returned HTTP 403, so per-command/build/cache subtimings are unknown. Five runs describe the recent release family, not a long-term estimate.

Last serial run after the native-tool change (main, 2026-10-09): 11m09s.

**Parallel workflow (first successful PR run, 2026-10-09):** 3m58s wall clock from run creation to completion (jobs 3m54s from first start to last finish). Job durations: `postgres` 229s (critical path), `proxy-smoke` 148s, `e2e` 141s, `oidc-smoke` 101s, `backend` 98s, `api` 98s, `setup-smoke` 84s, `household-backup-smoke` 81s, `runtime-smoke` 76s, `backup-restore-smoke` 71s, `frontend` 52s, `required` aggregate 2s. A single sample: queueing and runner availability vary, so treat it as indicative rather than a guarantee. Total billed runner minutes increase because each job repeats checkout and tool setup.
