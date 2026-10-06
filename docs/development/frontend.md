# Frontend development

The web UI is a React 19, TypeScript, Vite, and Tailwind CSS single-page app in `frontend/`. In production it is embedded in the Go binary. See [ADR 0003](../adr/0003-ui-foundation.md) for the reasoning.

Today the UI shows first-run onboarding when setup has not been completed (setup code, household name, login, password, and a time zone pre-filled from the browser), a sign-in page, and, after sign-in, a home screen with the household name and an empty state. Items, subjects, and invitations do not exist yet.

## Prerequisites

- Node.js 22.22.2 or newer (or 24+) with npm
- Docker Engine with the Compose plugin (for the backend)

## Run locally

Start the backend from the repository root with `TENDO_PUBLIC_URL=http://localhost:8080` in `.env` (see [runtime development](runtime.md)):

```sh
docker compose up -d --build
```

Set `TENDO_SETUP_TOKEN` as described in the README if you want to try onboarding, then start the dev server:

```sh
cd frontend
npm ci
npm run dev
```

Vite proxies `/api` and `/health` to `http://localhost:8080` and rewrites the `Origin` header to that address, because the backend requires `Origin` to equal `TENDO_PUBLIC_URL`. If the backend runs elsewhere, change `backend` in `frontend/vite.config.ts` and set `TENDO_PUBLIC_URL` to match.

## Structure

- `src/app`: shell, layout, and top-level status screens
- `src/features/<feature>`: one folder per feature (`auth`, `home`) with its screens and API calls
- `src/components/ui`: shared primitives copied from shadcn/ui (MIT)
- `src/i18n`: translations (`en.ts`, `cs.ts`) and the language provider
- `src/lib`: the API client and small helpers
- `src/test`: test setup and the fake server used by tests

## Localization

Every user-visible string lives in `src/i18n`. Add each new key to both `en.ts` and `cs.ts`; types and a test fail if the locales differ. The language comes from the saved choice, otherwise from the browser language, and can be switched in the footer.

## API types

The client in `src/lib/api.ts` is typed from `api/generated/api.d.ts`. Do not write API types by hand. After changing `api/openapi.yaml`, run `npm run generate` in `api/` and commit the generated output. The Docker build copies `api/generated/` into the frontend stage.

## Checks

From `frontend/`:

```sh
npm run check
npm audit --audit-level=moderate
```

`npm run check` runs lint, type check, unit tests, and a production build. Run the scripts separately when debugging: `lint`, `typecheck`, `test`, `build`.

## End-to-end tests

From the repository root, after `npm ci` in `frontend/`:

```sh
bash scripts/e2e-smoke.sh
```

It runs Playwright in the `mcr.microsoft.com/playwright:v1.63.0-noble` container against the production Compose stack with real PostgreSQL. The container uses `--network host`, which works on Linux Docker hosts (CI is Linux). The image version must match `@playwright/test` in `frontend/package.json`; the script checks this. It covers browser onboarding (proposed time zone from a fixed browser time zone, correcting it, a wrong setup code, and the household name and time zone stored by the server), closing setup after first use, failed and successful sign-in, the home screen, reloading a deep link, Czech and English, narrow and wide viewports, keyboard use, sign out, and security headers.

## Production build

`npm run build` writes to `backend/internal/platform/webui/dist`, which the Go binary embeds with `//go:embed`. A committed placeholder keeps `go build` working without a frontend build. The `Dockerfile` has a Node stage that runs the build and a Go stage that copies the output in, so `docker compose up --build` produces one image.

Go serves `index.html` for app routes with a strict same-origin Content-Security-Policy (no inline scripts or styles) and `X-Frame-Options: DENY`, and serves hashed files under `/assets/` with immutable caching. Unknown `/api` and `/health` paths still return `application/problem+json`.

App routes whose last path segment contains a dot are treated as files and return 404 instead of `index.html`. Client routes must not end in a file-like segment.
