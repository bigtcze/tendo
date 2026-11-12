# V5 Direction A — "Calm list"

Status: **prototype, not human-reviewed, not approved.** No user has tested this direction. Nothing here is a
product decision. It is an evaluable branch (`proto/v5-direction-a`), not for merge. Its purpose is to let the
owner compare one direction against Direction B side by side.

## 1. Intent

- Home reads as a list of things to do, not a dashboard. One column, one quiet metadata line per item.
- The page's own heading ("Overview") and one primary action ("Add item") are the only chrome above the list.
- Status is shown by group heading and a semantic icon, never by colour alone.
- Chrome (top bar, sidebar, bottom bar) stays quiet so the list carries the screen.

## 2. Scope

**Built**

- Full-width top bar at every width (56–64 px): Tendo mark, active household as a read-only menu button with
  "Manage households" (to Admin when authorised), account menu.
- Home: small "Overview" heading with "Add item" on one line; groups (Needs attention, In progress, Waiting,
  Upcoming) as light headings with counts; rows with a 44 px round Done control, title and one metadata line.
- All items, Archive (All items with the archived filter), People and things, Members, Account, Admin centre.
- Item detail as its own page: title, facts as a labelled definition list, one filled Done, quiet secondary
  actions, history section.
- Phone navigation: bottom bar with Home, Items, People, More. "More" sheet holds Archive, Members, Account, Admin.
- Light and dark themes, English and Czech copy, Inter, a small type scale.

**Deferred / not built**

- **Inline create.** The brief asked for quick entry that expands inline at the top of the list. Only the entry row
  exists; it still opens the existing add sheet. See deviation D1.
- Household switching. The API has no switch endpoint, so the menu only names the household.
- Any change to backend, API, hooks, recurrence logic or dates. None was made, per the brief.

## 3. Layout per breakpoint

| Breakpoint | Top bar | Navigation | Content |
|---|---|---|---|
| 320–599 px (phone) | Full width; wraps if needed; min 56 px | Fixed bottom bar: Home, Items, People, More (4 items) | Single column, 16 px side insets, bottom padding clears the bar |
| 640–1023 px (tablet) | Full width; 24 px insets (`sm:`) | Bottom bar (same four items) | Single column, measure max 760 px, 24 px insets |
| ≥ 1024 px (desktop) | Full width at 1920 too; 64 px tall | Sidebar ~232 px: Home, All items, Archive, People and things, Members, Account, Admin centre | Column next to sidebar, measure max 760 px, 32 px insets |
| Short viewport (`max-height: 30rem`) | As above | Bar becomes static (in flow) | Bottom reserve drops to 24 px |

Known layout facts:

- Header is full width at every size (no `max-w` on the household shell). Verified by code, not by a 1920 capture
  in this pass.
- Content measure is `47.5rem` (760 px).
- No horizontal overflow was recorded in the probe runs (`scrollWidth == innerWidth` at 200 % zoom).

## 4. Tokens

### Type scale (rem / px at 100 %)

| Token | Size | Line height | Use |
|---|---|---|---|
| display | 2 rem / 32 px | 1.15 | Rarely used |
| h1 | 1.625 rem / 26 px | 1.2 | Page heading ("Overview") |
| h2 | 1.0625 rem / 17 px | 1.35 | Group headings and dialog titles |
| row-title | 1 rem / 16 px | 1.4 | Item titles |
| body | 0.9375 rem / 15 px | 1.55 | Body copy |
| control | 0.9375 rem / 15 px | 1.4 | Buttons |
| small | 0.875 rem / 14 px | 1.5 | Secondary copy |
| meta | 0.8125 rem / 13 px | 1.5 | Row metadata |
| label | 0.8125 rem / 13 px | 1.35 | Form and definition labels |
| micro | 0.75 rem / 12 px | 1.35 | Bottom-bar labels |

### Spacing

Base unit 0.25 rem (4 px). Used steps: 4, 8, 12, 16, 24, 32 px. Row padding is 12 px vertical.

### Radius

Controls `rounded-lg` (8 px); cards and sheets `rounded-xl`/`rounded-2xl` (12/16 px); pills and the Done control
fully round.

### Colour roles (light / dark)

| Role | Light | Dark |
|---|---|---|
| paper (page) | `#f8f7f3` | `#171614` |
| raised (surfaces) | `#fffefc` | `#1f1e1b` |
| sunken (quiet fills) | `#eeece4` | `#262420` |
| ink (text) | `#1c1b17` | `#f2efe8` |
| muted (secondary text) | `#5f5c53` | `#aaa599` |
| line (dividers) | `#e3dfd4` | `#33312b` |
| field (control borders) | `#857f72` | `#8b8578` |
| accent (primary, focus) | `#1a6a55` | `#7ad3b6` |
| on-accent (text on accent) | `#ffffff` | `#0b211a` |
| needs-attention | `#a63f2d` on `#fbeae4` | `#ffa48c` on `#3a231d` |
| in-progress | `#1a6a55` on `#e4f1ec` | `#7ad3b6` on `#12332a` |
| waiting | `#8a5a10` on `#fbf0d9` | `#e8bf72` on `#33280f` |
| upcoming | `#4b6472` on `#e9eef2` | `#a9c3d3` on `#1e2a33` |
| paused | `#6d6a61` on `#efede8` | `#b3aea3` on `#2a2823` |
| done | `#3f6b4e` on `#e8f1e9` | `#93d8b0` on `#163026` |
| danger | `#a32b3f` on `#f9e6e9` | `#ff9aa8` on `#3a1d22` |

## 5. Computed contrast

WCAG 2.x relative-luminance formula, computed from the token values above (`/tmp/opencode/v5-05/a/contrast.py`).
Required: 4.5:1 for text, 3:1 for non-text boundaries and focus.

**Light theme**

| Pair | Ratio | Required | Result |
|---|---|---|---|
| ink on paper | 16.08 | 4.5 | pass |
| ink on raised | 17.10 | 4.5 | pass |
| muted on paper | 6.23 | 4.5 | pass |
| muted on raised | 6.63 | 4.5 | pass |
| muted on sunken | 5.65 | 4.5 | pass |
| accent on paper (links) | 6.05 | 4.5 | pass |
| on-accent on accent (Done) | 6.49 | 4.5 | pass |
| on-accent on accent-strong (hover) | 9.23 | 4.5 | pass |
| needs-attention on its bg | 5.34 | 4.5 | pass |
| waiting on its bg | 5.23 | 4.5 | pass |
| upcoming on its bg | 5.34 | 4.5 | pass |
| paused on its bg | 4.62 | 4.5 | pass (thin margin) |
| done on its bg | 5.31 | 4.5 | pass |
| danger on its bg | 5.89 | 4.5 | pass |
| in-progress on its bg | 5.59 | 4.5 | pass |
| field border on raised | 3.95 | 3.0 | pass (non-text) |
| focus ring (accent) on paper | 6.05 | 3.0 | pass |
| ink on selected nav | 13.43 | 4.5 | pass |
| line on paper | 1.24 | — | decorative divider only, not a required boundary |

**Dark theme**

| Pair | Ratio | Required | Result |
|---|---|---|---|
| ink on paper | 15.75 | 4.5 | pass |
| ink on raised | 14.52 | 4.5 | pass |
| muted on paper | 7.36 | 4.5 | pass |
| muted on raised | 6.79 | 4.5 | pass |
| muted on sunken | 6.31 | 4.5 | pass |
| accent on paper (links) | 10.18 | 4.5 | pass |
| on-accent on accent (Done) | 9.48 | 4.5 | pass |
| on-accent on accent-strong (hover) | 11.71 | 4.5 | pass |
| needs-attention on its bg | 7.61 | 4.5 | pass |
| waiting on its bg | 8.36 | 4.5 | pass |
| upcoming on its bg | 7.97 | 4.5 | pass |
| paused on its bg | 6.66 | 4.5 | pass |
| done on its bg | 8.55 | 4.5 | pass |
| danger on its bg | 7.57 | 4.5 | pass |
| in-progress on its bg | 7.71 | 4.5 | pass |
| field border on raised | 4.54 | 3.0 | pass (non-text) |
| focus ring (accent) on paper | 10.18 | 3.0 | pass |
| ink on selected nav | 12.78 | 4.5 | pass |
| line on paper | 1.39 | — | decorative divider only |

These are token-level ratios. They do not cover text drawn over images (none exist) or anti-aliasing effects.

## 6. Interaction model

- **Home.** Rows are read in order; each row's Done control is a 28 px visual circle inside a larger hit area (see
  W11). Groups show counts; "show more" pages per group. Completion, Undo and Retry behaviour is the existing one;
  this pass did not change it.
- **Add item.** The "Add item" button and the list-top entry both open the existing add sheet (full-screen on
  phone, centred dialog on desktop). Repeat is a switch that reveals interval and unit controls; "Count the next
  repeat from when I complete this" is a second switch. Validation and focus return follow the existing dialog.
- **Detail.** A dedicated page with a back link (to Home, or to All items when opened from there). Facts are a
  labelled definition list. One filled Done; Edit, Start/Waiting/Pause/Resume are quiet; Archive sits at the end
  of the band as a small text action. Edit opens the existing edit dialog (not inline).
- **Admin centre.** Same tokens and type scale as the rest; restyle only. Not separately walked through in this
  pass (see weakness W6).
- **Phone.** Bottom bar with four items. Archive, Members, Account and Admin live in the "More" sheet, so Archive
  stays discoverable.

## 7. Validation

Real results from this branch. Commands run in `/home/tomas/repositories/tendo-v5-dir-a`.

| Check | Command / source | Result |
|---|---|---|
| Lint | `npm run lint` (`eslint . --max-warnings=0`) | pass on final commit (see §9) |
| Typecheck | `npm run typecheck` (`tsc -b --noEmit`) | pass on final commit |
| Unit tests | `npx vitest run` | 51 files, 1029 tests pass on final commit |
| Build | `npm run build` | pass on final commit |
| Production E2E smoke | `flock /tmp/opencode/v5-05/e2e.lock bash scripts/e2e-smoke.sh` | **not green**: 63 passed, 1 failed (`n23`), 5 did not run; exit 1 (see W1) |
| Screenshots | design-capture harness | not run in this pass; owner will run it on this branch |

The failing E2E test is `n23` (real browser zoom at 200 %). It was not weakened, skipped or edited.

## 8. Deviations from the brief

- **D1. Inline create is not built.** The list-top entry opens the existing add sheet rather than expanding inline.
  Reason: the add flow is covered by many unit and E2E tests that query the dialog; converting it was out of this
  pass. The sheet is the entry; phone already uses the full-screen sheet.
- **D2. Admin centre restyle is light.** Tokens and type are shared, but it was not separately checked against the
  brief's admin layout.
- **D3. Eyebrows remain on auth, invite, onboarding and status screens.** They were removed from Home-adjacent
  screens only. The brief's removal targeted Home and the app shell.
- **D4. Household menu is read-only naming** plus "Manage households". No switcher (no API endpoint).

## 9. Commits

| SHA | Subject |
|---|---|
| `63371f8` | wip: partial calm-list shell, nav, home rows and tokens |
| `b00b4df` | fix: keep home row facts separate with flex separators |
| `4b45776` | fix: start the phone detail Archive control on its own edge |
| `71837c4` | chore: drop unused eyebrow copy from Home-adjacent screens |
| (this commit) | docs: Direction A README |

## 10. Known weaknesses

Numbered so they can be referenced.

- **W1. Production E2E smoke is not green.** `n23` fails at 200 % zoom: `Pause` on item detail has
  `viewport ratio 0.9989346861839294` against `toBeInViewport({ ratio: 1 })`. Probe numbers (200 % zoom,
  layout viewport 640 × 406 CSS px, DPR 2): Pause `y 362.55, h 44`, bottom edge `406.547`, i.e. **0.547 px below
  the bottom of the viewport**. The action band (`data-item-actions`) shares that top `362.546875`. `main` is
  `y 15, h 553.49`. The viewport is under 30 rem tall, so the `short:` rules apply: the bottom bar is static, and
  the bottom reserve drops to `pb-6`. Horizontal position is fine (right edge 460 px). Document-level scroll is
  used, not an inner scroller (checked against v4 `5bd7647`: neither the shell nor `index.css` adds `overflow` or a
  fixed height; the only `overflow-y-auto` is the desktop sidebar at `lg`, which is not rendered at 200 %).
  Attempt that did not fix it: `short:pb-8` on `MAIN_HOUSEHOLD`, one full smoke run (`e2e-final-1.log`), same
  failure, reverted. A header edit was tried but only as a comment, never tested, and reverted. No verified layout fix
  exists. Root cause not identified. The `ratio: 1` assertion is unchanged.
  The runner reports five tests as "did not run" after `n23` (`n24`, `n25`, `n27`, `n29`, `r`). I did not verify
  the reason; their results are unknown on this branch.
- **W2. Inline create is missing** (see D1).
- **W3. Phone bottom bar hides Archive, Members, Account and Admin** behind "More". That is discoverable, but it
  adds a tap for rarely used destinations.
- **W4. Paused text on its tinted background is 4.62:1**, a thin margin above AA. It passes but is near the limit
  in both themes (dark is 6.66).
- **W5. Household menu is informational only.** Users cannot switch households here; "Manage households" takes them
  to the Admin centre, which is an indirect path.
- **W6. Admin centre has not been checked against this direction** beyond shared tokens (D2).
- **W7. 320 px and 1920 px layouts** were checked by code and computed numbers, not by a captured screenshot in this
  pass. The full-width header at 1920 is structural, not visually verified here.
- **W8. Five E2E tests did not run** after `n23` in the smoke (reason unverified), so they are unknown on this branch.
- **W11. Done control is a 28 px visual circle** (`size-7`) inside a larger invisible hit area (`after:-inset-2`).
  That gives an effective target of about 44 px, but the visible control is smaller than the brief's 44 px target
  implies; not verified by a target-size audit.
- **W12. Tablet insets change at 640 px, not 600 px** as the table implies (the `sm:` breakpoint). Minor.
- **W9. Eyebrows remain on auth and status screens** (D3), so the direction is not yet consistent across the app.
- **W10. No human review, user testing or stakeholder approval.** Every claim here is from code, unit tests, a
  partial E2E run and computed contrast, not from anyone looking at the screens.
