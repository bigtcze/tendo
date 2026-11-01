# Direction B — Household overview, organised by who and what

branch=`proto/v3-direction-b`; head SHA=orchestrator fills

## Intent and interaction

This direction frames household work by people and things instead of as a task manager. The home intro anchors the day and household; an obvious “Add something to remember” opener expands the existing add form inline. Needs-attention rows are separated by subject headings; rows keep the action explicit and put recurrence detail behind hover/focus. In-progress, waiting, upcoming and paused remain secondary groups. Detail is a dedicated route with its own “Mark done” and Undo action using the existing completion API, plus item facts, editing and history.

Desktop navigation is a horizontal header and the shell grows to a 1440px maximum. At 1024px and wider, needs-attention occupies the larger left region while workflow/upcoming groups use the right column. At tablet widths groups use two columns. Phone keeps the existing primary links in one horizontally scrollable top navigation row (not five wrapping tabs or bottom tabs) and stacks the groups. No sidebar or right-hand detail pane is used.

## Visual language

- Display: system serif / Charter / Georgia; household heading 36px, group heading 24px, subject heading 18px.
- Body: existing rounded system sans, 16px base; row title 18px; supporting dates 16px; small labels 14px.
- Palette: paper `#faf7f2`, ink `#25302b`, muted `#5b655f`, sand `#efe9df`, line `#d8d0c3`, accent `#2f6b57`, strong accent `#234f41`.
- Contrast measured from these hex colors (WCAG relative luminance calculation): ink/paper 12.4:1; muted/paper 5.7:1; white/accent 6.0:1; white/strong-accent 8.2:1. Text pairs exceed 4.5:1.
- Density: realistic capture fixture includes around 30 household tasks. Approximate intended fold density: 390px, 2–3 needs-attention rows; 1440px, 4–6 rows in primary column plus several secondary items. These are design estimates, not measured capture evidence.

## Household fit and known limits

People can recognize “Anna” or “the car” as a section instead of decoding a state-first wall of metadata. One-off is still the form default. Recurrence is explicit and examples explain the two schedule choices in plain language. The controls retain keyboard focus styling and 44px targets. This is an AI prototype with no household participant testing; English and Czech recurrence prose is currently placed directly in the form component, and needs copy review. Phone navigation is horizontally scrollable rather than a menu and may hide destinations until swiped. Subject grouping currently applies to needs-attention results only. Existing tests intentionally assert some old row text/layout contracts and need careful review rather than weaker assertions.

## Not prototyped / rough

Most routes outside Home and item detail retain much of their prior layout. The quick-add trigger expands to the existing form rather than an inline title-first composer. Detail completion currently has a local status/Undo implementation and needs deeper race, focus, retry and screen-reader review. No dark theme, screenshot harness, capture evidence, phone menu state, empty-state redesign, broad form redesign, or final all-route polish was delivered. No claims of user approval are made.

## Screenshot index

No direction B screenshots have been captured yet. The required production-build, PostgreSQL-backed capture matrix and authentic screenshot review remain uncompleted. Do not interpret baseline images under `../v3-baseline/` as direction B evidence.

## Test status

- `npm run lint`: passed.
- `npm run typecheck`: passed.
- `npm run build`: passed.
- Vitest full suite: 784 passed / 1 failed (33 files passed, 1 failed). The remaining failure is `src/features/items/ItemsScreen.test.tsx > Home items > groups by server states and formats date-only values without timezone shifting`; it queries rows using the former `ul.divide-y > li` selector, while this prototype removes that card-list styling. Assertions were not weakened. A prior focused run during iteration had 172 passed / 13 failed; the latest full run supersedes it.
- Required default PostgreSQL E2E: 30 passed, 1 failed, 20 did not run. Failure: `e2e/journey.spec.ts` n13, “home batch-loads labels only for rendered subjects, including archived references”; the expected archived subject name was not visible in Home. Treat as a prototype regression to investigate.

## Test status (continued)

No screenshots or viewport density were measured. This README is a partial concept note, not completion evidence for Issue V3-04.
