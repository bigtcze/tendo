# ADR 0003: UI foundation

Status: accepted

## Context

Tendo needs a consumer-friendly, mobile-first web UI (see the UX rules). The UX rules ask for a mature, permissively licensed foundation, not a dense admin-dashboard template and not hand-built generic primitives. Self-hosters should not have to run a separate frontend container.

## Decision

- The UI is a single-page app in `frontend/`: React, TypeScript, Vite, and Tailwind CSS.
- Primitives (button, input, label) are copied from shadcn/ui (https://ui.shadcn.com, MIT license) into `frontend/src/components/ui` and adapted. shadcn/ui is source we own, not a runtime dependency. Each copied file names its origin in a header comment. Add further primitives the same way when a screen needs them.
- The production image builds the frontend in a Node stage and embeds the result in the Go binary (`backend/internal/platform/webui`, `//go:embed`). There is no separate frontend container or web server.
- The API client is `openapi-fetch`, typed from `api/generated/api.d.ts`, which is generated from `api/openapi.yaml`. API types are never written by hand.
- Localization (Czech and English) uses a small in-house typed module in `frontend/src/i18n`. Both locales must define the same keys, enforced by types and a test. No i18n library is used yet: there are two locales and a small set of strings. Revisit if plural rules, formatting, or more locales make the module hard to maintain.

## Consequences

- One image and one origin: no CORS configuration, and the session cookie stays same-origin.
- Building the image needs Node. Local frontend work needs Node.js 22.22.2+ (or 24+).
- Copied primitives do not receive upstream updates automatically; upgrades are manual.
- Changing the API contract requires regenerating `api/generated/api.d.ts`; frontend type checks then show what broke.
- Adding a locale or a string means editing the locale files by hand until a library is justified.
