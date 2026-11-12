# Direction B: structured workspace (prototype)

Status: functional prototype on `proto/v5-direction-b`. Not reviewed or approved by a human, and not user-tested.
Governing brief: `.autonomous/V5_DESIGN_BRIEF.md`. This document records what this branch does, what it does not, and
where it falls short.

## Intent

A calm, dense, desk-style workspace. The household name and account controls sit in one top bar, the sections sit in a
left sidebar, and Home reads as a compact aligned table of items grouped by state. Visual weight comes from alignment and
rhythm, not from pills or decoration. One accent colour (teal) marks the single primary action and the active state.

## Scope of this pass

Implemented without routing or data-model changes:

- Full-width top bar (56 px minimum): brand, active-household switcher, header search, language and account actions.
- Active-household switcher: a disclosure button that names the household. "Manage households" links to the Admin center
  only when the viewer's role hint is `admin`. The server still authorizes every admin request.
- Header search: submits to All items with `?q=`. Hidden on All items (that page has its own search field) and hidden on
  phones (see layout).
- Overview header: one line with the "Overview" h1 (Czech "Přehled") and the single "Add item" action. The household name
  moved out of the h1 into the switcher and the document title (`Overview · {household} — Tendo`).
- Home rows: plain-text subject (no pill on every row), compact uppercase group headers with plain counts.
- Sidebar: 224 px (14rem) from `lg`, tighter rail link spacing.
- Type scale tightened to 13 / 14 / 16 / 20 / 24 px (see tokens).

Explicitly deferred. Not built on this branch:

- Detail as a right-hand drawer with its own `/items/:id` URL. Detail stays a full page.
- Sidebar section counts.
- Tablet collapsible icon rail. Tablet uses the same bottom bar as phones below `lg`.
- Phone "More" tab with Archive. Phone bottom bar keeps Home, Items, People, Members, Account.
- Admin table restyle. Admin keeps its existing layout.
- Paginated item history in detail.
- Create/edit as a centred modal with keyboard-first ordering. Forms keep their existing structure.
- Done/Undo restructuring.

## Layout and navigation per breakpoint

Breakpoints are the existing Tailwind ones: `sm` 640 px, `lg` 1024 px, `xl` 1280 px.

| Width | Top bar | Navigation | Main insets |
| --- | --- | --- | --- |
| < 640 px (phone) | brand, household switcher (truncates), account actions; one row, no wrap | fixed bottom bar: Home, Items, People, Members, Account | 16 px |
| 640–1023 px (tablet) | brand, switcher, header search, account actions; may wrap | fixed bottom bar (same as phone); no icon rail yet | 24 px |
| ≥ 1024 px (desktop) | same as tablet, 56 px | 224 px sidebar with labels; Admin center when authorized | 32 px, 40 px at `xl` |

Notes:

- Phone header: search is hidden to keep one row. The All items tab searches.
- The bottom bar is `fixed`. Content reserves its height as bottom padding and scroll margin.
- Admin center appears in the sidebar and in the switcher panel for admins only.

## Tokens

### Type scale (px)

| Role | Size | Token |
| --- | --- | --- |
| Caption / group label | 13 | `--text-caption` |
| Small / body-secondary / nav | 14 | `--text-small`, `--text-label` |
| Body / control | 16 | `--text-body`, `--text-control`, `--text-row-title` |
| Section heading | 20 | `--text-h2` |
| Page title (h1) and display | 24 | `--text-h1`, `--text-display` |

Inter (self-hosted) is the only family. Weights 400 and 600 are used for text; 700 for page titles.

### Spacing

Scale: 4 / 8 / 12 / 16 / 24 / 32 px (Tailwind 1 / 2 / 3 / 4 / 6 / 8). Main insets 16 / 24 / 32 px by breakpoint. Row
height 44–48 px.

### Colour roles (light / dark)

Each token is written once as `light-dark(light, dark)` in `frontend/src/index.css`.

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| Paper (page) | `#f1f5fa` | `#0f1a24` | Page background |
| Raised | `#ffffff` | `#16232f` | Header, rows, panels |
| Ink | `#16283c` | `#eaf1f8` | Primary text |
| Muted | `#52647b` | `#a9b8c8` | Secondary text |
| Line | `#d7e2eb` | `#2c3d4d` | Decorative dividers and row borders |
| Field | `#6b7c90` | `#8496a8` | Input boundaries (non-text, must reach 3:1) |
| Accent | `#006d82` | `#4fc8da` | Primary action and active state |
| Accent strong | `#005566` | `#7bdce9` | Primary hover and pressed |
| On accent | `#ffffff` | `#072028` | Text on accent |
| Focus ring | `#005566` | `#6fd6e6` | Focus outline |

One accent. State colours (needs attention, waiting, upcoming, paused, done, danger) exist for status text and tones and
are not used as decoration.

### Contrast (computed WCAG 2.x ratios)

Computed from the token values above with the WCAG relative-luminance formula. The E2E contrast auditor measures the
rendered pages separately (see Validation).

| Pair | Ratio | Threshold | Result |
| --- | --- | --- | --- |
| Light ink on raised | 14.97 | 4.5 | pass |
| Light ink on paper | 13.67 | 4.5 | pass |
| Light muted on raised | 6.06 | 4.5 | pass |
| Light muted on paper | 5.53 | 4.5 | pass |
| Light on-accent on accent | 5.98 | 4.5 | pass |
| Light on-accent on accent-strong (hover) | 8.43 | 4.5 | pass |
| Light accent text on raised | 5.98 | 4.5 | pass |
| Light field border on raised | 4.28 | 3.0 | pass (non-text) |
| Light focus ring on raised | 8.43 | 3.0 | pass |
| Light needs-attention on its tint | 4.75 | 4.5 | pass |
| Light waiting on its tint | 5.40 | 4.5 | pass |
| Light danger on its tint | 6.02 | 4.5 | pass |
| Light line on raised | 1.32 | — | decorative only, not a boundary for controls |
| Dark ink on raised | 14.01 | 4.5 | pass |
| Dark muted on raised | 7.89 | 4.5 | pass |
| Dark muted on paper | 8.69 | 4.5 | pass |
| Dark accent text on raised | 8.06 | 4.5 | pass |
| Dark on-accent on accent | 8.51 | 4.5 | pass |
| Dark field border on raised | 5.25 | 3.0 | pass (non-text) |
| Dark focus ring on raised | 9.45 | 3.0 | pass |
| Dark line on raised | 1.43 | — | decorative only |

Line colours are decorative, so they are not required to reach 3:1. Control boundaries use the field token.

## Interaction model

- Home: "Add item" is the one primary action. Rows are one tap or click to detail. Done is the trailing control on each
  row. Groups are headings with counts, not collapsible.
- Create/edit: unchanged structure in this pass. Title is the first field. Repeat is a toggle that reveals interval and
  fluid options.
- Detail: a full page at `/items/:id` on every width. Facts are in the existing order. History is not paginated.
- Admin: the switcher panel and sidebar expose Admin center to admins only. Admin keeps its existing list.
- Phone: bottom bar is the only navigation. The switcher opens in place and closes on Escape or focus leaving.
- Keyboard: switcher opens on click or Enter, closes on Escape (focus returns to the trigger) and on blur. The skip link
  targets `#main`.

## Validation (this branch, final state)

| Check | Result |
| --- | --- |
| `npm run lint` | pass |
| `npm run typecheck` | pass |
| `npx vitest run` | 51 files, 1033 tests, pass |
| `npm run build` | pass (pre-existing chunk-size warning) |
| Required E2E smoke (`scripts/e2e-smoke.sh`, under `flock`) | pass, including Firefox and WebKit cross-browser runs |
| UX audit (`TENDO_UX_AUDIT_DIR`, under `flock`) | pass; 524 screenshots captured in a scratch directory |
| Visual regression (`TENDO_VISUAL_MODE=1`) | not run; baselines intentionally not updated, so differences are expected |

The Firefox cross-browser run failed twice with "Page crashed" inside an axe scan on the 390 px pass, then passed on the
final run after the header changes. The cause is not established. I did not confirm whether HEAD shows the same
behaviour.

## Deviations from the brief

- Phone header search is hidden (CSS, still in the DOM). The brief asks for a compact global search in the top bar; it
  does not fit on one phone row beside brand, household and account.
- The household name is no longer the Home h1. It appears in the switcher and document title. Existing E2E identity
  checks now go through `frontend/e2e/home.ts` (`expectHome`), which asserts the locale-neutral Overview title and the
  switcher name.
- `App.test.tsx` header assertion changed from `flex-wrap` to `flex-nowrap` on phones and `sm:flex-wrap` from `sm`. The
  min-height and no-fixed-height guard is unchanged.
- The switcher's trigger text and panel use the same name as the accessible name (`Active household: {name}`).

## Known weaknesses

1. The Overview header is generic. It is a plain title with one button and does not carry a distinctive identity.
2. Detail is still a full page. The right-hand drawer with its own URL is not built.
3. There is no sidebar count, so the sidebar does not show how much needs attention.
4. Tablet has no icon rail, so tablet users get a bottom bar that is more cramped than the brief intends.
5. Phone has no More tab, and Archive is not reachable from the phone bottom bar.
6. Admin is not restyled. The table-style list in the brief is not built.
7. Item history is not paginated.
8. Create/edit forms keep the existing structure and are not a centred keyboard-first modal.
9. Done/Undo is not restructured.
10. Phone header search is hidden, so the global search is unavailable on phones outside All items.
11. The header and switcher add height at `sm` where they may wrap. Bar growth could push content, and this is only checked
    at 320 px in Czech.
12. Visual-regression baselines were not updated, so the visual suite will report differences until someone reviews and
    updates them.
13. The Firefox crash above is unexplained.
14. Several large test files and i18n dictionaries are over the 350-line guideline. They predate this branch and were only
    edited for assertions and keys.
