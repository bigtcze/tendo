# Tendo V4 – Modern Everyday visual design brief

status=human_authorized_visual_direction
reference_desktop=docs/design/reference/tendo-v4-desktop.png
reference_mobile=docs/design/reference/tendo-v4-mobile.png
reference_sha256_desktop=9fb1901bc30d35a3df8c8a434b17edc1ba9708dc9260f36befdb209dc746a61d
reference_sha256_mobile=40a1571f1fb88e18f3ea2f84d60ed1967cba73fd59c3c8d3cb9d3c7f98520198

## Source of truth
The two supplied images are HUMAN-APPROVED VISUAL REFERENCES, not screenshots of an implemented product or permission to invent features. **The reference compositions, type character, spacing rhythm, density, cool palette, emphasis, and navigation are binding visual design targets**, subject only to actual functionality, legibility, accessibility and responsive constraints. Do not redesign the reference into a generic dashboard; do not experiment with an unrelated visual direction. Produce real app screenshots as implementation evidence. Do not clone unrelated GitHub pages.

## Visual identity
- Neutral cool off-white/light blue-gray canvas (#F4F7FB approximate); white or very pale blue surfaces with subtle 1px dividers. Keep broad areas calm, not gradient-heavy or glassy.
- Dark navy text (#17263A approximate); secondary text in accessible slate (#5F6E82 approximate). Primary accent deep petroleum blue/teal (#006E83 to #008C9E approximate), links/actions/focus state consistent.
- Semantic states: attention coral red-orange (#E65340), in progress cyan/teal (#00A2B4), waiting amber (#E89512), coming up green-teal (#34B7A6). Status always conveyed by wording/icon/shape, not color alone.
- Use **Inter Variable** as the primary UI font (self-hosted, license verified, full Czech glyph coverage, fallback only while loading/unavailable). Do not substitute another font by aesthetic preference; font change needs a documented technical blocker and explicit human review. Use weights about 700 for major headings, 600–700 for task titles, 400–500 for body, and 600 for controls. Avoid serif headings, geometric novelty fonts, fake heavy outlines or external font/CDN tracking. Polish font-size, optical weight and line height on small screens and in Czech with diacritics.
- Compact list-based cards, gentle radius 10–16px, subtle borders, very restrained shadow if needed, consistent icon stroke and sizing. No excessive shadows, badges, emoji, ornamental widgets, fake charts, glows, or gratuitous motion.

## Desktop reference, 1448×1086
- Slim top bar with Tendo wordmark at left and signed-in identity at right. Narrow persistent left navigation with Home, All items, People and things, Members, Account; admin sees Admin center too. Selected Home is a pale-blue surface, not a giant saturated block.
- Main area uses available width. Above fold: small 'YOUR HOME · OVERVIEW' eyebrow, bold 'Novák family.' headline, short practical secondary description, compact dark teal 'Add item' button aligned right.
- Below title, horizontally aligned filter/status pills 'Needs attention 4', 'In progress 2', 'Waiting 1', 'Coming up 3'. Attention is subtle warm tint, other pills cool neutral. All four groups are visible and easy to scan; no giant cards or wasteful whitespace.
- Group headers use a tiny colored semantic dot, clear heading/count and restrained contextual explanation aligned right. Rows have consistent height and alignment: semantic icon chip, bold task title, concise subordinate metadata, person/thing pill, relevant date/assignee, and a restrained outlined 'Done' action at right. Never invent task properties; only show real data. Preserve Undo and task detail access.
- Actual reference data: Replace the smoke alarm battery (Home), Give Rex his flea treatment (Rex), Renew Tom's passport (Tom), Book Anna's dental check-up (Anna), Prepare the balcony planters, Sort the hallway cupboard, Plan a family weekend away, Schedule the car inspection (Škoda Octavia), Change the water filter, Review home insurance.

## Mobile reference, 941×1672 representation of ~390px viewport
- Simple top app bar with Tendo branding and a concise account/settings affordance; no multirow developer navigation.
- Compact 'YOUR HOUSEHOLD' eyebrow, bold sans 'Novák family.' with restrained cyan period, short supporting sentence, prominent full-width teal Add item action.
- Single horizontal scrollable row of the four status filters, never squeezing all four unreadably or causing page overflow.
- 'Needs attention 4' heading, compact stacked items in a shared white surface with hairline separators. Each row shows a colored icon chip, clear task title, person chip and summary; completion check at right remains an accessible ~44px target. Below is 'In progress'.
- Persistent mobile bottom bar: Home, Items, People, Members, Account (admin entry can live behind account/menu or an intentional additional route without cramming six equal tabs). Current Home uses a subtle icy-blue background. Safe area and small viewports handled.
- No fabricated layout-only metrics, GitHub issue numbers, repository labels, and no duplicate/person placeholder requiring a second profile.

## Implementation contract
Use actual browser/Playwright production build with synthetic fixture data, EN and CZ, 320/360/390/768/1024/1440 widths; light/dark/system, zoom 200%/400%, long labels, empty/error/loading/signed-out/admin/member states. Review actual screenshots against reference and correct real issues. Preserve API behavior, performance, keyboard/touch/WCAG 2.2 AA orientation, no CSS overflow. Gradual rollout via existing design primitives and tokens, no new theme engine.


## Binding reference fidelity contract (V4 approval gate)
**Source assets:** `docs/design/reference/tendo-v4-desktop.png` (1448×1086) and `docs/design/reference/tendo-v4-mobile.png` (941×1672, visually represents a ~390 CSS-px mobile layout). Both must be copied byte-for-byte from the human-provided bootstrap packet in V4-00; check SHA-256 values in the header. These are design **references**, never screenshot evidence for a completed feature.

### Initial token palette (measured visual direction; adjust ONLY to satisfy tested contrast)
| Purpose | Starting value | Usage |
| --- | --- | --- |
| Page canvas | `#F1F5FA` | cool pale bluish white, no decorative gradients |
| Surface | `#FFFFFF` | cards, input panels, nav header |
| Primary text | `#16283C` | dark navy |
| Secondary text | `#52647B` | accessible slate; no low-contrast washed-out metadata |
| Primary CTA | `#006D82` | petrol teal, white text, hovered/pressed darker |
| CTA hover | `#005566` | distinct affordance |
| Subtle border | `#D7E2EB` | quiet 1px separators, use stronger control border if needed |
| Selected navigation | `#DDECF4` | light icy blue |
| Attention text/icon | `#C43F2D` | coral red on very pale coral tint |
| Waiting text/icon | `#935300` | amber on pale warm tint |
| Progress text/icon | `#006D82` | teal on pale cyan tint |
| Upcoming text/icon | `#04796B` | green-teal on pale mint tint |
These are **initial semantic token values**, not blanket replacements. Derive a properly contrasted **dark mode** using the same information hierarchy (navy/dark-slate background, brighter teal actions, distinct semantic states), and verify normal-text >=4.5:1, large text >=3:1, non-text controls/focus >=3:1. No color-only meaning. Maintain OS-following preference and first-paint correct theme.

### Composition measurements and hierarchy (targets; accessibility wins over hard pixels)
- Desktop reference at 1448px: top bar approx. 56px high, left nav approx. 220–230px wide, content starts with about 32–40px inset and fills remaining desktop width. Heading and primary action share one header band; **not** a narrow single-column feed. Main headline roughly 36–40px bold, task titles 14–16px, supporting text 12–14px but must remain genuinely readable, not artificially shrunk to fit screenshot.
- Desktop list rows aim at 48–55px each, grouped on one white surface separated by thin dividers; *avoid isolated tall task cards*. Ten fixture tasks and all four sections should scan compactly at 1440×~1080 without a gratuitous vertical dashboard. Each row: topic icon tile → prominent title with supporting metadata → person/thing pill → relevant date/assignee → Done control. Do not hide vital data just to fit the screenshot.
- Mobile at ~390px CSS viewport: brand/settings/identity in one compact bar; heading about 29–34px; full-width teal Add item; horizontally scrollable four status pills; one shared task panel per section with compact dividers and check-complete touch target >=44px. Fixed bottom bar exactly Home / Items / People / Members / Account, visible and safe-area-aware. In Account or a distinct route, admin-only Admin center must remain discoverable without squeezing in a sixth bar item.
- Minimal shadows, 10–16px radii, restrained colored icon tiles, square-ish outlined completion controls, no gradients in backgrounds, no colorful KPI cards, no GitHub icon, no enterprise tabs or generic task-manager artifacts. Pure decoration must never suppress real actions, dates or assignment details.

### Required visual comparison & sign-off
1. Commit a deterministic fixture that matches the **ten named tasks, group counts 4/2/1/3, people/thing labels and realistic metadata** in the references. Capture full-page production Playwright screenshots at 1440×~1080 desktop and 390×~844 mobile in **English light theme** as first visual milestone. Also validate Czech, dark and responsive widths separately. The screenshots MUST come from running Tendo, not generated assets.
2. Reviewer inspects reference and actual captures **side by side**, evaluating (a) font, optical weight and dark-navy hierarchy; (b) off-white canvas and white surfaces; (c) petrol-teal CTA / coral-amber-teal status semantics; (d) spacing and density, including compact grouped rows; (e) navigation/bottom bar; (f) icon/tile styling and focus; (g) actual data accuracy. Log discrepancies with screenshot paths, fix significant mismatches before closing V4-03, repeat after V4-05 and V4-13.
3. Quantitative guardrails: no horizontal page overflow at 320px; status pills scroll internally; touch targets and readable text survive 200/400% zoom; contrast passes; no false screenshots or relabelled reference art. Screenshot similarity alone is not a DoD or an excuse to sacrifice behavior.
4. Any intentional major deviation from the approved direction (e.g. a different font, new color family, giant cards, layout rearrangement) is **not at agent discretion**. Record why the real product/accessibility requires it, show the resulting actual screenshot, and request human approval before claiming visual acceptance. Subtle measured implementation adjustments do not require approval.
5. Apply the SAME system across setup/sign-in/OIDC, Account, Admin center, Members/Invitations, All items, item forms, People and things, dialogs, loading, empty/error states. The mockups illustrate Home only; their consistency requirement extends throughout the product.
