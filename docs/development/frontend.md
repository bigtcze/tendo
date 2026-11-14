# Frontend development

The web UI is a React 19, TypeScript, Vite, and Tailwind CSS single-page app in `frontend/`. In production it is embedded in the Go binary. See [ADR 0003](../adr/0003-ui-foundation.md) for the reasoning.

Today the UI shows first-run onboarding when setup has not been completed (setup code, household name, login, password, and a time zone pre-filled from the browser), a sign-in page, and, after sign-in, a home screen with the household name and an empty state. A "People and things" screen lets members add, rename, archive, and restore subjects through the subjects API. Household members can view members and owners can manage invitation links.

## Prerequisites

- Node.js 26.13.0 (22.22.2+ also supported) with npm
- Docker Engine with the Compose plugin (for the backend)
- Playwright 1.64.1 and matching Chromium browser for native E2E (see [development testing](testing.md))

## Run locally

Start the backend from the repository root with `TENDO_PUBLIC_URL=http://localhost:8080` in `.env` (see [runtime development](runtime.md)):

```sh
docker compose -f compose.yaml -f deploy/compose.build.yaml up -d --build
```

Set `TENDO_SETUP_TOKEN` as described in [install](../admin/install.md) if you want to try onboarding, then start the dev server:

```sh
cd frontend
npm ci
npm run dev
```

Vite proxies `/api` and `/health` to `http://localhost:8080` and rewrites the `Origin` header to that address, because the backend requires `Origin` to equal `TENDO_PUBLIC_URL`. If the backend runs elsewhere, change `backend` in `frontend/vite.config.ts` and set `TENDO_PUBLIC_URL` to match.

## Structure

- `src/app`: shell, layout, and top-level status screens
- `src/features/<feature>`: one folder per feature (`auth`, `home`, `subjects`, `members`) with its screens and API calls
- `src/components/ui`: shared primitives copied from shadcn/ui (MIT)
- `src/fonts.css`: self-hosted Inter Variable faces (`@fontsource-variable/inter` 5.3.0, SIL OFL 1.1; latin + latin-ext, no CDN)
- `public/fonts/INTER-OFL-LICENSE.txt`: canonical OFL text, copied into the production build at `/fonts/INTER-OFL-LICENSE.txt`
- `public/favicon.svg`, `public/favicon.ico`, `public/apple-touch-icon.png`, `public/icon-192.png`, `public/icon-512.png`, `public/manifest.webmanifest`: the brand tile and app icons linked from `index.html`
- `src/i18n`: translations (`en.ts`, `cs.ts`) and the language provider
- `src/lib`: the API client and small helpers
- `src/test`: test setup and the fake server used by tests

## Localization

Every user-visible string lives in `src/i18n`. Add each new key to both `en.ts` and `cs.ts`; types and a test fail if the locales differ. The language comes from the saved choice, otherwise from the browser language, and can be switched in the footer.

## API types

The client in `src/lib/api.ts` is typed from `api/generated/api.d.ts`. Do not write API types by hand. After changing `api/openapi.yaml`, run `npm run generate` in `api/` and commit the generated output. The Docker build copies `api/generated/` into the frontend stage.

## Brand assets

`public/favicon.svg` is the only drawn source: the accent rounded-square tile with the house glyph from `src/app/BrandMark.tsx`. It carries a `prefers-color-scheme: dark` rule so the tab icon follows the browser theme, and it uses the accent/on-accent values from `src/designTokens.ts` (a test fails if the drawn colours and the tokens drift apart). `index.html` links it together with the Apple touch icon and the web app manifest.

The PNG icons are exported from that SVG with `rsvg-convert`; regenerate them from `frontend/` after changing the SVG or the tokens. `rsvg-convert` (librsvg) is a host tool, not pinned by `mise`, so it is only needed when the PNGs are regenerated:

```sh
rsvg-convert -w 180 -h 180 -b '#1a6a55' -o public/apple-touch-icon.png public/favicon.svg
rsvg-convert -w 192 -h 192 -b '#1a6a55' -o public/icon-192.png public/favicon.svg
rsvg-convert -w 512 -h 512 -b '#1a6a55' -o public/icon-512.png public/favicon.svg
```

`public/favicon.ico` is the raster fallback for browsers without SVG favicon support and for the browser's own `/favicon.ico` request. It holds 16, 32 and 48 px PNG exports of the same SVG (transparent corners, light variant). `index.html` links it first with `sizes="32x32"` and the typed SVG last, so browsers that support SVG favicons choose the SVG. Regenerate it with the PNGs:

```sh
for n in 16 32 48; do rsvg-convert -w $n -h $n -o /tmp/favicon-$n.png public/favicon.svg; done
python3 - <<'PY'
import struct
sizes = [16, 32, 48]
images = [open(f'/tmp/favicon-{n}.png', 'rb').read() for n in sizes]
out, offset = struct.pack('<HHH', 0, 1, len(images)), 6 + 16 * len(images)
for n, data in zip(sizes, images):
    out += struct.pack('<BBBBHHII', n, n, 0, 0, 1, 32, len(data), offset)
    offset += len(data)
open('public/favicon.ico', 'wb').write(out + b''.join(images))
PY
```

The browser chrome tint (`theme-color`) comes from two tagged metas in `index.html` (`data-scheme="light"`/`"dark"`, paper token colours). With System they follow the device through `prefers-color-scheme`; an explicit Light or Dark choice keeps only its own meta (`media="all"`, the other `"not all"`). `public/theme-boot.js` applies this before first paint and `src/theme/theme.ts` on every Appearance change.

`-b` paints the light tile colour behind the rounded corners, because Apple touch icons and the manifest icons must be opaque (`rsvg-convert` ignores the dark media rule and always exports the light variant). The manifest declares `display: browser`: Tendo is an ordinary web app, not an installable standalone app, and it registers no service worker.

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

It builds the production frontend assets and native Go app/provider binaries, then runs Playwright 1.64.1 against the embedded production UI and real PostgreSQL using the locally installed matching browser. It does not use Docker or Vite's dev server. The app is gracefully stopped and restarted at the controlled 2026-01-15 midnight boundary with the same database; the same future one-off item is verified before and after. These dates use the explicit isolated test clock, never sleeping. The complete serial journey covers onboarding (timezone proposal/correction, wrong setup code, persisted household name/timezone), closing setup, failed/successful sign-in, home, people/things CRUD/archive/restore and no account creation, deep links, Czech/English, narrow/wide viewports, keyboard, accessibility (axe-core WCAG A/AA scans of key screens, sampled visible focus, 200 % zoom-equivalent reflow, reduced motion), sign out, security headers, invitations/member access, item lifecycle/recurrence/history, OIDC link/login/unknown identity/cancel, and authorization boundaries. Artifacts are kept in a unique `frontend/test-results/native-*` directory for diagnosis. See [development testing](testing.md) for the complete pre-PR command and native dependencies.

## Production build

`npm run build` writes to `backend/internal/platform/webui/dist`, which the Go binary embeds with `//go:embed`. A committed placeholder keeps `go build` working without a frontend build. The `Dockerfile` has a Node stage that runs the build and a Go stage that copies the output in, so `docker compose -f compose.yaml -f deploy/compose.build.yaml up --build` produces one image.

Go serves `index.html` for app routes with a strict same-origin Content-Security-Policy (no inline scripts or styles) and `X-Frame-Options: DENY`, and serves hashed files under `/assets/` with immutable caching. Self-hosted woff2 fonts are served with `font/woff2` so browsers accept them under `font-src 'self'`. Unknown `/api` and `/health` paths still return `application/problem+json`.

App routes whose last path segment contains a dot are treated as files and return 404 instead of `index.html`. Client routes must not end in a file-like segment.

There is no router. The only client path is `/people`; `App` switches between it and the home screen with the History API (`pushState` and `popstate`), so browser Back and reloads work. Go serves `index.html` for `/people` like any other app route.
