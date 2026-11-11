# V5 design brief — a crafted household product, not a vibe-coded dashboard

status=human_authorized_design_goal_not_fixed_reference_image

## Workflow
1. Inspect real v4 production build and authenticated routes/states; capture genuine before screenshots at 320,390,768,1024,1440,1920px in Czech and English, light and dark. Inventory edge-hugging controls, inconsistent spacing, overlarge header, information density, visual hierarchy, workflows and forms. Do NOT mistake previously generated V4 reference PNGs for production evidence.
2. Benchmark the information-architecture and interaction discipline of mature task apps (e.g. Things 3) and dense yet clear applications (e.g. Linear), observing their principles only. Do NOT clone branding/trade dress or insert generic AI/card-dashboard visuals.
3. Produce at least two distinctly different FUNCTIONAL interactive directions using current React app / synthetic fixture data for Home, create/edit/detail, global navigation, admin screen and phone width; independently critique both by usefulness, accessible density and professional polish; document comparison, pick one based on evidence, never fabricate 'human-approved' sign-off.
4. Freeze a small shared token/component system (typography, spacing 4/8/12/16/24/32, color roles, border/radius, focus, hover, disabled, loading, semantic statuses, system/light/dark). Ship progressive, reviewable PRs with real browser screenshots for every screen family.
5. Independent senior-designer skeptical review + user journeys in REAL production browser; fix review findings, not just generate markdown claims or green snapshot tests. Update real screenshots on release.

## Explicit Home/shell decisions
- Full-viewport top bar spanning 100% width, not a framed ~90rem banner; ~60–64px is a starting target, accessible responsiveness wins.
- Show Tendo branding, functional active-household switcher, account access. Remove promotional 'DOMOV POD KONTROLOU.' and duplicate big in-content 'VÁŠ DOMOV · PŘEHLED / Veselá rodina / Klidnější způsob...' block. Active household label belongs in header, not an oversized hero. Main may have a small 'Přehled' section heading and 'Přidat položku' action aligned to one line.
- Practical sidebar ~220–240px, consistent main insets 24–32px desktop, 16px phones; avoid buttons against panel edges. Navigation presents Home, All items, Archive, People/things, Members, Account and Admin center when authorized. Phone Archive must remain discoverable without overloading bottom tab bar.
- Scannable grouped item rows with aligned title, contextual subject, relevant due date, responsibility, recurrence metadata and Done; avoid random pills everywhere, repeated status words, excessive row DOM, huge blank space or tiny low-contrast metadata. Use honest small-screen wrapping, don't suppress facts.
- Same professional visual language on task forms/history, authentication/OIDC, account/password, household selector, admin center, MCP settings and error/empty/loading states. Favicon/app icon from Tendo's own brand and correct browser title.
- Normal history pagination (e.g. 20 entries per page, Previous/Next, stable ordering and accessible current page), NO complicated infinite-scroll/virtual timeline UI. Server cursor internally acceptable if necessary.

## Nonfunctional QA
- WCAG 2.2 AA-oriented contrast, logical focus/tab order, reduced motion, 200/400% zoom, no horizontal overflow, touch targets >=44px unless justified by accessible exceptions; screen reader announcements for mutations.
- CZ/EN strings, localized date display, long Czech names/diacritics, no clipping; light, dark and system themes with no first-paint flash.
- Realistic empty, 1, 10, 100, 1000+ items; ordinary commodity/mobile device budgets; no hidden focus, duplicated taps or ornamental distracting charts. E2E screenshots are evidence of IMPLEMENTED production build, never concept art.
