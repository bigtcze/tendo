# V4 design system — modern everyday tokens (V4-02)

status=V4-02_system_tokens
issue=#410 (V4-02)
supersedes=visual tokens, typography, radii and control styling of [v3-design-system.md](v3-design-system.md)
scope=system-level primitives only; shell/navigation/Home composition is V4-03, per-screen work is V4-04/V4-05

This is the implemented V4 visual foundation. Values come from `.autonomous/V4_DESIGN_BRIEF.md`
(initial palette + Inter Variable + 10–16 px radii) adjusted only where a measured contrast check
required it. Measured ratios are computed from the token source by `frontend/src/designTokens.test.ts`
and the CSS/TS mirror is parity-tested; the tables below repeat those measured values.

## Typography

The product ships **Inter Variable** as a self-hosted webfont; there is no CDN, no external font
request and no tracking. It is used for body text **and** headings (the V3 serif display stack is
gone). Headings are 700, row titles 600, body 400, controls 600.

| Item | Value |
| --- | --- |
| Package | `@fontsource-variable/inter` pinned exactly to `5.3.0` (dependency, not devDependency) |
| License | SIL Open Font License 1.1 (upstream Inter Project Authors); full text committed as [INTER-OFL-LICENSE.txt](v4-tokens/INTER-OFL-LICENSE.txt) and shipped in the npm package |
| Axis | `wght` 100–900 variable; no italic face is shipped because the UI never renders italic text |
| Subsets | `latin` (48,256 B) and `latin-ext` (85,068 B); only these two `@font-face` rules are declared in `frontend/src/fonts.css` |
| Czech coverage | `latin-ext` carries `ě š č ř ž ý á í é ů ú ń ť ď`; verified in a production browser by `e2e/theme.spec.ts` (`document.fonts.check(…, 'ěščřžýáíéůú')`) and by Czech visual baselines |
| Fallback | `Inter`, `ui-sans-serif`, `system-ui`, `-apple-system`, `Segoe UI`, sans-serif — used only while loading or if the bundled font is unavailable |
| Delivery | Vite emits hashed `/assets/inter-*-wght-normal-*.woff2`; Go serves them same-origin as `font/woff2` (CSP `font-src 'self'`, covered by a `webui` test) |
| Loading | `font-display: swap`; `unicode-range` means English fetches only `latin`, Czech fetches `latin-ext` too |

| Role | Size / line-height | Weight | Use |
| --- | ---: | ---: | --- |
| Display | 36 px / 1.12 | 700 | Home/onboarding welcome headline |
| H1 | 30 px / 1.2 | 700 | Screen title |
| H2 | 24 px / 1.3 | 700 | Section/group heading |
| Row title | 16 px / 1.4 | 600 | Primary item text |
| Body | 16 px / 1.5 | 400 | Main content and form values |
| Meta | 14 px / 1.45 | 400 | Dates, context, hints; never below 14 px |
| Label | 14 px / 1.35 | 600 | Field labels |
| Control | 16 px / 1.25 | 600 | Buttons and interactive values |
| Small | 14 px / 1.35 | 400 | Compact supporting text |

`h1`/`h2` get tightened letter-spacing in the base layer (`-0.02em` / `-0.01em`); explicit text
utilities still win over those defaults. The UX audit measures 14 px as the smallest visible text
across all audited routes and viewports.

## Colour tokens

The palette is cool: pale blue-gray canvas, white raised surfaces, navy text, petrol-teal action,
coral attention, amber waiting, teal-green coming up. `needsAttention`, `inProgress`, `waiting`,
`upcoming`, `paused`, `done`, `danger` and `success` are **foreground / subtle-background pairs**;
the state is always also written in words and/or an icon or shape. `upcoming`, `upcoming-bg` and
`nav-selected` are new V4 tokens; V3 names (`paper`, `raised`, `sand`, `accent`, …) are retained to
avoid churn.

| Role | Light | Dark |
| --- | --- | --- |
| Page canvas / paper | `#F1F5FA` | `#0F1A24` |
| Surface / raised | `#FFFFFF` | `#16232F` |
| Sunken / sand | `#E8EFF6` | `#1E2C3A` |
| Primary text / ink | `#16283C` | `#EAF1F8` |
| Secondary text / muted | `#52647B` | `#A9B8C8` |
| Subtle border / line | `#D7E2EB` | `#2C3D4D` |
| Control border / field | `#6B7C90` | `#8496A8` |
| Primary CTA / accent | `#006D82` | `#4FC8DA` |
| CTA hover/pressed / accent-strong | `#005566` | `#7BDCE9` |
| Text on accent | `#FFFFFF` | `#072028` |
| Focus ring | `#005566` | `#6FD6E6` |
| Selected navigation | `#DDECF4` | `#1D3546` |
| Needs attention | `#BE3A29` on `#FCEBE7` | `#FFA98F` on `#3B241E` |
| In progress | `#006D82` on `#E3F2F5` | `#5BCBDE` on `#10333C` |
| Waiting | `#935300` on `#FDF1DC` | `#F0C46B` on `#34280F` |
| Coming up | `#04796B` on `#E4F4F0` | `#5BD8C4` on `#0F322C` |
| Paused | `#52647B` on `#EDF2F7` | `#B9C7D7` on `#26323F` |
| Done | `#35694A` on `#E7F2EC` | `#8AD9B2` on `#163229` |
| Error / danger | `#A3243B` on `#F9E4E8` | `#FF97A6` on `#3B1E26` |
| Success | `#35694A` on `#E7F2EC` | `#8AD9B2` on `#163229` |

The brief's starting attention value `#C43F2D` measured 4.44:1 on its pale-coral tint; it was
darkened to `#BE3A29` (4.75:1) only to satisfy the normal-text threshold. Everything else starts
from the brief unchanged. Attention (coral, warm hue) and danger (crimson/pink) remain distinct
roles; state wording and alert semantics also differ.

### Measured contrast (sRGB WCAG ratios)

Normal text ≥ 4.5:1, large text ≥ 3:1, non-text controls/focus ≥ 3:1. Both themes are enforced in
`designTokens.test.ts`.

| Pair | Light | Dark | Minimum |
| --- | ---: | ---: | ---: |
| Ink / paper | 13.67 | 15.45 | 4.5 |
| Muted / paper | 5.53 | 8.69 | 4.5 |
| Ink / raised | 14.97 | 14.01 | 4.5 |
| Muted / raised | 6.06 | 7.89 | 4.5 |
| Ink / sunken | 12.91 | 12.49 | 4.5 |
| Muted / sunken | 5.22 | 7.03 | 4.5 |
| On-accent / accent | 5.98 | 8.51 | 4.5 |
| On-accent / accent-strong | 8.43 | 10.64 | 4.5 |
| Accent / paper | 5.47 | 8.89 | 4.5 |
| Accent / raised | 5.98 | 8.06 | 4.5 |
| Focus / paper | 7.70 | 10.42 | 3 |
| Focus / raised | 8.43 | 9.45 | 3 |
| Field border / paper | 3.91 | 5.79 | 3 |
| Control border / raised | 4.28 | 5.25 | 3 |
| Switch unchecked track / thumb | 4.28 | 5.25 | 3 |
| Switch selected track / thumb | 5.98 | 8.06 | 3 |
| Nav text / selected | 12.39 | 11.17 | 4.5 |
| Accent / selected nav | 4.95 | 6.43 | 3 |
| Needs attention / subtle | 4.75 | 7.77 | 4.5 |
| In progress / subtle | 5.21 | 7.07 | 4.5 |
| Waiting / subtle | 5.40 | 8.80 | 4.5 |
| Coming up / subtle | 4.68 | 7.97 | 4.5 |
| Paused / subtle | 5.38 | 7.58 | 4.5 |
| Done / subtle | 5.60 | 8.31 | 4.5 |
| Error / subtle | 6.02 | 7.31 | 4.5 |
| Success / subtle | 5.60 | 8.31 | 4.5 |

## Shape, border, elevation

- 1 px borders only: `line` for quiet separators, `field` for interactive control boundaries
  (≥ 3:1 against paper/raised in both themes).
- Radii: 12 px buttons/inputs/selects/textarea/notices, 10 px completion controls, 16 px
  dialogs/panels/empty states, 999 px count pills. All inside the brief's 10–16 px range.
- No decorative shadows. A dialog gets one restrained functional shadow
  (`0 24px 60px -32px`), and the native backdrop is `rgb(7 20 30 / 60%)`.
- Buttons carry a 1 px pressed translate; reduced motion removes the movement.

## Component states

| Primitive | States |
| --- | --- |
| Button | Primary filled teal, secondary outlined, quiet text; hover/pressed use a darker teal or sunken surface; visible 2 px focus outline with offset; disabled 60 % + not-allowed; 44 px minimum target. |
| Input / select / textarea | Control-border boundary, raised surface, hover darkens the boundary, focus outline, `aria-invalid` uses danger border; disabled 60 %; 44 px minimum. |
| Switch | Field-coloured track with raised thumb; checked uses accent and a darker hover; visible focus on the native input that covers the full 44 px track; disabled 60 %; reduced-motion removes slide. |
| Completion control | Outlined 44 × 44 target, 10 px radius, field border; checked uses success foreground/background; pressed state exposed as `aria-pressed`; glyph hidden from assistive tech. |
| Notice | Neutral sunken surface, success and error use their token pair as border/background/text, with status/alert roles unchanged. |
| Dialog | Raised surface, 16 px radius, element border, subtle functional shadow; native modal, focus return, busy lock and Escape behaviour unchanged. |
| Group header | 24 px 700 heading with a supplementary count pill; the count is not part of the heading name. |
| List row | 16 px 600 title, 14 px meta, 1 px divider (removed on the last row), minimum 64 px height; composition changes are V4-03. |
| Empty state / skeleton / pagination | Raised empty panel; skeleton uses the line token and `animate-pulse` disabled by reduced motion; pagination keeps quiet buttons, named navigation and disabled bounds. |

## Theme behaviour

`public/theme-boot.js` is unchanged: it applies a saved `light`/`dark` choice before first paint and
System follows the device through `light-dark()` without JavaScript. The rebuilt palette keeps the
same CSS pipeline: a plain-light `:root` fallback plus one `light-dark(light, dark)` declaration per
token, mirrored in `frontend/src/designTokens.ts`. `theme.spec.ts` still proves parser-completion
colour, every rendered frame before app mount, persistence, tampering fallback and OS switching;
its paper constants were updated to the new palette.

## Tests that lock this down

- `frontend/src/designTokens.test.ts` — CSS ↔ TS parity for every token, light fallback equals the
  light side of `light-dark()`, and the measured ratios above (new pairs for `navSelected`,
  `upcoming` and focus/control boundaries). A changed hex without a documented measurement fails.
- `frontend/e2e/theme.spec.ts` — System/Light/Dark first paint and reload, rendered-frame evidence,
  tamper fallback, contrast audit on Login in both themes, and a new production-browser test:
  body/heading resolve to Inter Variable, the heading weight is 700, a loaded Inter face renders
  Czech diacritics, and every woff2 response is 200, same-origin, under `/assets/`, `font/woff2`.
- `backend/internal/platform/webui/webui_test.go` — `.woff2`/`.woff` are served with their real
  font MIME types and immutable caching (Go's builtin table has no woff entries).
- `frontend/e2e/visual/capture.ts` — every visual capture asserts Inter Variable is the computed
  body family and a face is loaded before pixels are taken.
- `frontend/e2e/ux-audit.spec.ts` — captures a real invalid + focused field state for evidence.

## Evidence and reference fidelity

Committed captures are real production-build screenshots (Playwright Chromium, fixed business date
2026-05-14, real PostgreSQL), not mockups:

| Capture | Purpose |
| --- | --- |
| [home-light-1440](v4-tokens/home-light-1440.png) | Home, English, light, 1440 |
| [home-dark-1440](v4-tokens/home-dark-1440.png) | Home, English, dark, 1440 |
| [home-light-390](v4-tokens/home-light-390.png) | Home, English, light, 390 |
| [home-dark-390](v4-tokens/home-dark-390.png) | Home, English, dark, 390 |
| [home-cs-390](v4-tokens/home-cs-390.png) | Home, Czech, 390 (diacritics in Inter) |
| [account-light-390](v4-tokens/account-light-390.png) | Account appearance controls |
| [form-focus-error-390](v4-tokens/form-focus-error-390.png) | Add form: invalid border + error text + visible focus ring |
| [login-light-390](v4-tokens/login-light-390.png) | Signed-out Login |

Reference-vs-capture composites are review artifacts only: they embed the AI reference images, so per
`.autonomous/FLOW.md` "V4 design and visual validation" they are attached to the PR review, never
committed. Side-by-side checklist against `.autonomous/V4_DESIGN_BRIEF.md` §"Required visual comparison",
limited to what V4-02 owns:

| Check | Result in V4-02 |
| --- | --- |
| (a) Font, optical weight, navy hierarchy | Pass: Inter Variable body + headings; display/H1/H2 700; row titles 600; ink `#16283C`. No serif remains. |
| (b) Off-white canvas, white surfaces | Pass: `#F1F5FA` canvas, `#FFFFFF` raised surfaces, 1 px cool dividers. |
| (c) Petrol-teal CTA, coral/amber/teal status semantics | Pass at token level: teal `#006D82` CTA with hover `#005566`, coral attention, amber waiting, teal-green coming up, plus Paused/Done/danger/success retained; dark theme uses brighter teal actions and distinct dark state tints. |
| (d) Border/radius density | Pass: 1 px borders, 10–16 px radii, no decorative shadows, compact control heights (44 px minimum). |
| Czech glyph coverage | Pass: `latin-ext` face loaded and used; Czech visual baselines are deterministic. |
| (e) Navigation/bottom bar composition | **Deferred to V4-03.** Existing rail/tab labels and order are untouched; only the selected-surface token changed. |
| (f) Icon/tile styling | **Deferred to V4-03** (per-row semantic icon tiles are composition work). |
| (g) Reference data (ten tasks, 4/2/1/3) | **Deferred to V4-03.** Captures use the existing visual/audit fixtures, not the approved reference fixture. |

Any larger visual change (different font, new colour family, layout rearrangement) would require
human approval; none was made. The only measured deviations from the brief's starting palette are
the attention darkening for contrast and the derived dark theme, which the brief explicitly asks for.
