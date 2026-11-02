# V3 application shell (V3-07, #328)

Responsive household shell built on the [design system](v3-design-system.md). Screen composition inside `main` (Home columns, rows, detail) stays with V3-08 onward.

## Decisions

| Viewport | Presentation | Why |
|---|---|---|
| `< lg` (phone, tablet) | Fixed bottom bar, five links, icon plus compact visible label (`Items`/`Položky`, `People`/`Lidé`) | Thumb reach; full names stay in each link's accessible name (WCAG 2.5.3) |
| `short` (`max-height: 30rem` and `< lg`: landscape phones, 200–400% zoom) | Same bar, static, directly below the header | Never covers content; visual order equals DOM/Tab order |
| `lg` (≥ 1024 px) | Sticky 15 rem left rail with full labels beside a wide `main` (up to `max-w-6xl`) | Removes the narrow centred column; content gets usable desktop width |
| Signed out / no household | No rail space; centred frame (no household) or a small inline bar (household-less) | No empty desktop chrome |

- One `Household navigation` landmark, native links, `aria-current="page"` (item detail marks All items).
- A fixed bar covers the viewport's last ~5 rem, so household content reserves it as scroll margin as well as padding. Tab-focused controls scroll clear of the bar. Disabled where the bar is static or replaced by the rail.
- Skip link is first in DOM, token-styled, and sits above the bar. The language switch lives in a quiet footer, last in tab order.
- Colours are semantic tokens and `currentColor` only; icons are decorative inline SVG (no dependency).

## Evidence

Production build, isolated PostgreSQL 18, pinned Chromium via `scripts/e2e-smoke.sh` and `ux-audit.spec.ts` (`captureShell`). Before = `origin/main` 43659ec; after = this branch. Files in [`v3-shell/`](v3-shell/): `before-*` / `after-*` pairs at 320–1440 px (en, cs), plus `after-shell-*` viewport captures:

- presentation per viewport (`320x640`, `390x844`, `768x1024` fixed; `640x360` static; `1024x768`, `1440x900` sticky rail), asserted in the audit;
- `after-shell-skip-link-en-390.png` (focused skip link), `after-shell-members-end-en-390.png` (page end clears the bar).

Regression coverage: `journey.spec.ts` n15 (six widths, no overflow) and n15b (runs within the serial journey's seeded household) (Tab sweep at 390×640 and 768×640 with no focused control under the bar; short-viewport order; rail beside main; compact/full label visibility); `App.test.tsx` landmark and accessible-name checks. The audit found no horizontal overflow on any route.

## Dark readiness

V3-14 owns theme preference. `after-shell-home-forced-dark-en-{390,1440}.png` force `data-theme="dark"` on `<html>`: header, rail, bottom bar, footer and focus colours follow the dark tokens. Page content inside `main` still uses hard-coded light surfaces (`bg-white`/`bg-white/70` in Home, All items, People and forms), so row lists render as grey panels. That belongs to V3-08/V3-14 screen work, not the shell.

## Not done here

Desktop content measure at 1440 px is wide, and short rows leave long gaps to their actions. Home columns/rows (V3-08, #329) and detail composition (#331) use the space the shell now provides.
