# V3-14 themes — System, Light and Dark

## Decisions

- **Default:** System. With no `data-theme` attribute, `@media (prefers-color-scheme: dark)` selects the dark color scheme, and `light-dark(light, dark)` resolves every semantic token. No JavaScript is required for a System user's initial theme or for a live OS appearance change.
- **Explicit preference:** Light or Dark is stored in this browser's `localStorage` under `tendo.theme`. System removes the key. Invalid values and unavailable storage fall back to System. This is a local presentation choice, not account or household data; sign-in is not required to set it.
- **First paint:** `frontend/public/theme-boot.js` is a synchronous same-origin classic script in `<head>`. It reads only the two explicit values and sets `data-theme` before style calculation. An external file is used because the web UI's `script-src 'self'` CSP disallows inline scripts. CSP and response security headers remain unchanged; the boot script is served from the embedded frontend root as `text/javascript` and is revalidated with `no-cache`.
- **Native controls:** `<meta name="color-scheme" content="light dark">` declares both supported palettes. CSS pins `color-scheme` for explicit choices, and sets it from the OS in System mode. Inputs, selects and scrollbars therefore match the resolved palette.
- **Control placement:** Appearance sits first in the quiet Account preferences column, above Language. The three labelled buttons match the existing Language switch pattern, expose `aria-pressed`, have native keyboard/touch activation, and use no extra component dependency. Signed-out screens have no settings control; they inherit the same saved local preference or System default, which avoids duplicating controls and leaves sign-in/onboarding focused.
- **Tokens:** Existing V3-06 light/dark semantic values are retained. `light-dark()` holds each pair once; the plain `:root` light values remain as a fallback for browsers without `light-dark()`. `designTokens.test.ts` checks fallback/pair synchronization, both token sets, and every previously documented contrast pair. No contrast ratio or semantic color was changed in this slice.
- **Status meaning:** Existing semantic labels and explicit text/icon cues remain; state is not communicated by color alone. Dark surfaces use the same raised/sunken hierarchy as Light. `ItemDetailHistory` now uses `bg-raised`, not a translucent white fill.

## Verification

- Unit tests cover accepted/invalid preference parsing, missing and blocked storage, storage writes/removal, `data-theme` application, boot-script parity and Account control labels, keyboard input, selection, and Czech text.
- Playwright's required `e2e` project covers System/Light/Dark against both OS schemes, parser-completion theme (recorded at `readyState=interactive`, before deferred app mount), reload persistence, invalid storage, live OS updates, and production HTML ordering. A separate render-timing test holds the app module request, samples the root background and `data-theme` on each animation frame until React mounts, and records `first-paint`/`first-contentful-paint` timings; it asserts every frame for saved Dark/light OS, saved Light/dark OS, and System/dark OS. Its negative control aborts `theme-boot.js` and asserts the same detector sees light frames for saved Dark/light OS. The serial journey drives the real Account control through Dark → reload → System, checks dark contrast, and runs the existing keyboard/focus-contrast sweep in Dark.

The opt-in audit captures focused Appearance buttons in Light and Dark at 390px as `auth-account-appearance-focus-{light,dark}-en-390.png`; focus is reached by keyboard and the screenshot retains the visible outline.
- The webui Go test checks root-level JavaScript serving, content type, cache behavior, and the unchanged exact CSP.
- The opt-in UX audit captures production-build screens from a real API-backed synthetic household. Dark captures use `tendo.theme` followed by a reload; there are no `data-theme` attribute or inline-token hacks in the audit.

## Capture index

The 55 production PNGs in `docs/design/v3-themes/after/` were captured by the opt-in UX audit against the production build and synthetic household. To regenerate the complete uncurated audit output into a scratch directory:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/tendo-theme-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

The repository keeps a curated subset (3.17 MiB); the tall full-page 1,200-item Home captures are deliberately excluded. Home width-sweep evidence uses viewport-height screenshots, while the Home phone/desktop light comparisons use the corresponding fold screenshots.

| Files | State | Theme / viewport | Review |
|---|---|---|---|
| `home-1200-en-{320,360,390,768,1024,1440}-dark.png` | Populated Home, viewport height | Dark, width sweep, English | Phone and desktop composition checked; section order and row contrast remain clear |
| `home-1200-en-{390,1440}-fold.png`, `home-after-setup-en-390.png`, `home-empty-dark-en-390.png` | Home light phone/desktop and empty state | Light 390/1440; Dark 390 | Light/dark visual comparison; dark empty state included |
| `home-1200-cs-390-fold.png`, `home-1200-cs-390-dark.png` | Populated Home | Czech, Light/Dark, 390 | Locale and status labels remain legible in both themes |
| `audit-all-items-{en-,dark-}{390,1440}.png` | All items | Light/Dark, English, 390/1440 | Side-by-side route comparison |
| `audit-all-items-error-{390,1440}.png`, `audit-all-items-error-dark-en-{390,1440}.png` | All items error | Light/Dark, English, 390/1440 | Error text, outline and retry action remain clear; meaning is not color-only |
| `audit-item-detail-en-{390,1440}.png`, `audit-item-detail-dark-en-{390,1440}.png` | Item detail/history | Light/Dark, English, 390/1440 | History rows use a raised semantic surface |
| `audit-add-{fixed,after-completion}-en-{390,1440}.png`, `audit-add-{fixed,after-completion}-dark-en-{390,1440}.png` | Add item, Repeat on; fixed and after-completion | Light/Dark, English, 390/1440 | Recaptured after repeat-copy change; both examples wrap without clipping |
| `audit-add-fixed-cs-390.png`, `audit-add-after-completion-cs-390.png`, `audit-add-after-completion-dark-cs-390.png` | Add item, Repeat on | Czech, Light/Dark, 390 | Recaptured; Czech fixed and after-completion examples remain readable |
| `audit-people-en-{390,1440}.png`, `audit-people-dark-en-{390,1440}.png`, `audit-people-edit-dark-en-390.png`, `audit-people-archived-view-dark-en-1440.png` | People and things, edit, archived | Light/Dark, English | Tile text, controls and action boundaries remain legible |
| `audit-members-owner-en-{390,1440}.png`, `audit-members-owner-dark-en-{390,1440}.png`, `audit-members-owner-cs-390.png`, `audit-members-owner-dark-cs-390.png` | Members/invitations | English Light/Dark 390/1440; Czech Light/Dark 390 | Invitations, statuses and actions stay distinguishable |
| `auth-account-unlinked-en-{390,1440}.png`, `auth-account-dark-en-{390,1440}.png` | Account with Appearance and Language | Light/Dark, English, 390/1440 | Recaptured after spacing fix; Language has deliberate separation beneath Appearance |
| `auth-account-appearance-focus-light-en-390.png`, `auth-account-appearance-focus-dark-en-390.png` | Account with keyboard-focused Appearance option | Light/Dark, English, 390 | Visible focus outline retained in both captures |
| `auth-login-en-{390,1440}.png`, `auth-login-dark-en-{390,1440}.png` | Login | Light/Dark, English, 390/1440 | Fields and secondary sign-in option remain legible |

## Visual critique and limitations

I opened and reviewed representative Light/Dark Home captures, all four English 390px Repeat-on states (fixed/after-completion × light/dark), Czech Light/Dark after-completion forms, the Czech Home pair, All items and its error state, detail/history, People, Members, and Account at 1440 in both themes. The charcoal paper and raised cards remain distinct without glaring white patches. Semantic borders and labels make the list rows and status controls readable; the error state combines a sentence and retry action with its color. The item history reads as a separate surface in Dark after replacing its translucent white fill with `bg-raised`.

The longer English fixed example wraps over three short lines in the 390px form; the after-completion example wraps without clipping in Light and Dark. Czech after-completion copy wraps to two lines at 390px, and the completion switch remains adjacent to its label. The Account 1440 pair confirms the Language divider and controls now have clear breathing room beneath Appearance. No theme token change was needed.

The phone form sheet remains taller than the viewport. Its lower Save/Cancel actions require scrolling; this is the existing sheet interaction, not a dark-specific defect, and merits separate form-usability review. Dark raised surfaces are deliberately close to the paper color; the border keeps groups distinct, though the surface contrast is quieter than in Light. No human household testing is claimed.

CSS `light-dark()` is supported from Chrome/Edge 123, Firefox 120, and Safari 17.5. Older browsers retain the light fallback and do not receive dark token values; if those browsers are in the supported matrix, a separate fallback strategy will be needed rather than silently duplicating the token set.

## Microcopy

Every string in `frontend/src/i18n/en.ts` and `cs.ts` was reviewed against UX.md "Language" and the V3 design brief. Most copy was already warm and jargon-free; the review found the Czech plural forms (`one`/`few`/`other` via `Intl.PluralRules`) and the consistent formal address (vykání) correct. Changed strings:

| Key | EN before → after | CS before → after | Why |
|---|---|---|---|
| `items.repeatExample.fixed` | A boiler check keeps its planned dates, even if you finish late. → Keeps the plan: the boiler check stays on its planned dates. If you finish late, the next one is the next planned date. | Kontrola kotle zůstává podle plánu, i když ji dokončíte později. → Drží se plánu: kontrola kotle zůstává v plánovaných termínech. Když ji dokončíte později, další bude v nejbližším plánovaném termínu. | States what happens after a late finish. Deliberately does not promise "one interval after the planned date": the backend advances by whole intervals to the first planned date after completion (PRODUCT.md fixed cadence), so a weekly item finished 9 days late skips to the following week. |
| `items.repeatExample.after` | For example, the next boiler check is due {interval} after you finish. → Counts from when you finish: the next boiler check is {interval} after the day you finish it. | Například další kontrola kotle bude {interval} po dokončení. → Počítá se od dokončení: další kontrola kotle bude za {interval} ode dne, kdy ji dokončíte. | Parallel structure with the fixed example, so the two read as a direct choice ("Keeps the plan" vs "Counts from when you finish"). |
| `items.repeatMode.fixed` | keeps planned dates → stays on the planned dates | drží plánované termíny → drží se plánovaných termínů | Czech calque replaced; English reads naturally. |
| `items.attentionDate.hint` | Leave empty to start now. → Leave blank if it needs attention now. | Nechte prázdné a začne platit hned. → Nechte prázdné, pokud má položka vyžadovat pozornost hned. | Names the consequence instead of an ambiguous "start". |
| `items.group.inProgress`, `itemDetail.state.progress` | (unchanged) | Rozpracováno → Probíhá | "Rozpracováno" sounds like work software; "Probíhá" is the natural household status. |
| `onboarding.householdName.placeholder` | (unchanged) | Rodina Novákova → Rodina Novákových | Correct Czech family-name form. |
| `account.appearance.*` (new) | Appearance; Match this device, or choose light or dark for this browser.; Match this device / Light / Dark | Vzhled; Řídit se podle zařízení, nebo v tomto prohlížeči zvolit světlý nebo tmavý vzhled.; Podle zařízení / Světlý / Tmavý | Theme preference. |

`i18n.test.ts` asserts the fixed example speaks of planned dates without an `{interval}` promise and the after-completion example counts `{interval}` from the finish day, in both languages. It also runs a vocabulary smoke check: no en/cs string contains the listed internal terms (ETag, tenant, fluid, API, workflow, cursor, idempotency, recurrence, payload, HTTP, UUID, SLA) or lateness-shaming words (overdue, po termínu, zpožděn). This is a guard against known regressions, not proof that all copy is jargon-free; that rests on this review. cs/en key and placeholder parity tests are unchanged.
