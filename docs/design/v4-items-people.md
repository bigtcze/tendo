# V4-04 All items, item lifecycle and People and things

status=V4-04_implemented
issue=#412 (V4-04)
direction=`.autonomous/V4_DESIGN_BRIEF.md` (reference images are visual targets only)
tokens=`v4-design-system.md` (V4-02)
shell=`v4-home.md` (V4-03)
siblings=`v4-auth-settings.md` (V4-05)

This applies the V4 visual system to the three task screens: All items, the item create/edit forms and
item detail, and People and things. Behaviour, routing, API contracts, roles, accessible names, heading
levels, focus ownership and the code-point limits are unchanged; only composition, wording placement and
surfaces moved.

## What changed

- **All items** (`AllItemsScreen.tsx`, `AllItemsControls.tsx`, `AllItemsResults.tsx`): the tall left filter
  column is gone. A header band (eyebrow "YOUR HOUSEHOLD · ALL ITEMS", 30 px headline, one supporting
  sentence) introduces one white toolbar surface: search plus a teal Search action, a hairline, then one
  filter row — "Show" with the status counts followed by "For", "Who's looking after it" and "Repeat".
  From `lg` all four selects share that single row (the refinement panel flattens with `lg:contents`), with
  the quiet "Clear search and filters" reset as the band's last right-aligned cell; below `lg` the three
  refinements stay behind the existing "Filters (n)" disclosure. On phones the status shares the line with
  that disclosure, and when a reset is available it takes the full width so the reset and the disclosure
  keep the next line together. The reset only exists while filters are active. Results are one grouped white list of compact rows using the Home vocabulary: real
  subject-type icon tile, title link, mint subject pill, the semantic state written out next to its icon
  (coral attention, teal in progress, amber waiting, green-teal coming up, neutral paused/archived, green
  done), the responsible line and the recurrence summary. The state words and the state colour come from
  one value (`itemState`), so an open upcoming item reads "Attention date …" with the mint coming-up tile
  and calendar icon instead of claiming "Needs attention". The "Showing 1–4 of 4" line is a 16 px 600
  small heading above the list, the cursor pager keeps its named navigation, and empty/no-match/error/
  loading and the archived listing use the same composition.
- **Item create and edit** (`AddItemPanel.tsx`, `ItemForm.tsx`, `ItemFormFields.tsx`,
  `ItemEditConflictSummary.tsx`): the form is one surface divided by hairlines into everyday fields
  (title, subject, attention date), the Repeat row, who is looking after it, and the collapsed notes.
  Repeat is still OFF by default; turning it on reveals the interval row and the after-completion switch
  inside a sunken panel with the one-sentence example, and the previous-completion disclosure sits at its
  foot. The primary/secondary action band is separated by a hairline; validation errors use the danger
  pair, and the 412 conflict summary is an amber caution surface listing the latest saved values above the
  rebased draft. Every control, including the responsible member, uses the same full width and control
  text size.
- **Item detail** (`ItemDetailReady.tsx`, `ItemDetailReadView.tsx`, `ItemDetailActions.tsx`,
  `ItemDetailHistory.tsx`, `ItemDetailLoadStates.tsx`): a header band with the subject as eyebrow, the
  title and the state pill (words + icon + semantic pair, all from `itemState`, so an archived item reads
  "Archived" with the archive glyph). Facts live on one hairline-divided white surface: completion
  sentence, attention/last-done dates, repeat sentence and mode, responsible member, notes; an item with
  none of those facts renders no surface at all instead of an empty bordered sliver. One action band
  carries the quiet Edit first (its existing focus-return target) on the same line as the filled primary
  Done, with Start/Waiting/Pause wrapping beneath them, and Archive/Restore in its own hairline-separated
  row. A finished or archived item shows that quiet action on a slim hairline band under its facts, not in a
  card of its own. The
  completion/undo notice keeps its status role and focus target, and the band spaces the stale notice away
  from the action row. History is a grouped hairline list under its existing heading, separated from the
  actions by a hairline. Loading, failed load and gone states use the eyebrow + white panel rhythm of the
  full-page secondary states, with the loading message in place of a headline.
- **People and things** (`SubjectsScreenContent.tsx`, `SubjectAddAction.tsx`, `SubjectList.tsx`,
  `SubjectRow.tsx`, `SubjectsStatus.tsx`, `SubjectForm.tsx`, `MemberPersonTransfer.tsx`): the two-column
  card grid is one grouped hairline list inside a capped 64 rem column. The primary Add action moved into
  the header band, right-aligned beside the title from `lg` and full width on phones (like Home's Add
  item); it keeps its DOM identity (`data-focus="add"`), its dialog and its focus return. Each row puts
  the real subject-type icon tile, the name and the right-aligned "See items"/Edit (or Restore) on one
  band, with the type and the existing "Household member · login" line after the name in the DOM (so
  assistive tech hears the facts before the actions) and beneath it visually. The wrapping is width-driven,
  not breakpoint-driven: the name column has a 10 rem basis, so the actions drop to their own right-aligned
  line exactly when they would squeeze the name below that — at 390 px in both locales — and stay on the
  name line from `sm`. The
  archived view reuses the same rows with Restore, the add/edit dialog keeps its field order and
  validation, and the member-transfer section uses the same sunken panels.
- **Shared primitives** (`components/ui/dialog.tsx`, `components/ui/empty-state.tsx`): headings now use
  the V4 `text-h2` heading token instead of the V3 `font-display` utility, and the now-unused
  `--font-display` token is removed from `index.css`. `--font-display` already resolved to Inter Variable,
  so nothing rendered differently; no component references the token any more.
- **New shared pieces**: `components/ui/glyph.tsx` (one stroke-icon frame), `features/subjects/SubjectTypeIcon.tsx`
  (the real subject-type glyphs, now owned by the subjects feature and reused by Home and All items) and
  `features/items/itemStateTone.tsx` (the real lifecycle state → words + icon + V4 semantic pair). The five
  workflow states are spread from `homeGroupTone.ts`; only the `done` and `archived` listing states are
  added there, so there is one source of truth for a state's colour.
- **New copy keys**: `allItems.eyebrow`, `allItems.subtitle` and `subjects.eyebrow` (English and Czech).
  They name the header bands; no existing copy changed. Four keys became dead when the shared state map took
  over and were removed from both catalogs: `items.attentionOn`, `itemDetail.state.progress`,
  `itemDetail.state.waiting` and `itemDetail.state.paused`.

## Deliberate deviations

| Deviation | Reason |
| --- | --- |
| Toolbar tab order is status → Filters (phones) → reset → refinement fields | Unchanged from origin/main. The reset keeps its DOM place before the fields and is the last visual cell of the desktop band; on phones it follows the disclosure and takes the next right-aligned line when the locale's labels do not fit beside it (320 px Czech). |
| The status filter stays visible on phones instead of moving behind "Filters" | It is the primary narrowing control; hiding it would add a tap to the most common filter. |
| Detail keeps Edit before Done in the DOM, on the same line | The existing keyboard order and the `editButtonRef` focus return after cancel are behaviour; Done remains the only filled primary action. |
| Row tiles use the real subject type and the real lifecycle state; no topic categories or invented badges | The API stores only the subject type and the workflow/attention state. |
| People rows use a neutral tile, not a state colour | A person or thing has no workflow state; only items do. |
| The reset wraps to its own right-aligned line on phones when it does not fit beside the disclosure | 320 px Czech needs about 300 px for both controls in a 248 px content column, so forcing one line would overlap them. The wrap is content-driven, so they share the line wherever they fit. |
| The All items card drops its inner divider and tightens spacing below `sm` | Keeps the 320 px result list high enough that the first title's 44 px target stays above the fixed bottom bar with the reset visible. |
| People is capped at 64 rem while All items uses the full width | All items rows carry five facts across four columns and its toolbar needs the width; a People row carries a name, a type and two actions, so at 1440 a full-bleed row left roughly 1000 px of dead space between the name and those actions. Home keeps the full width as the brief asks. |
| The People Add action moved from the list into the screen header band | The finding asks for Home's header-band placement. It keeps its `data-focus="add"` identity, dialog, trigger and focus return; it is now the first control in `main`, as on Home, instead of sitting between the list notices and the rows. |
| The People row wraps the actions onto their own right-aligned line at 390 px | The Czech actions need about 210 px, which would leave a 56 px name column. The name keeps a 10 rem basis, so the actions move below it exactly when they would squeeze it; from `sm` they sit on the name line again. The metadata follows the name in the DOM, so assistive tech hears the facts before the actions. |
| A finished or archived item shows Archive/Restore on a slim hairline band | With nothing else to do, a card holding one quiet button read as an empty container; a bare text link read as an orphan. A top hairline separates the quiet action from the facts without a second surface. |
| All items rows show the subject pill, state and responsible member in one metadata block below the title | Same facts as before, same wording; the block wraps as needed instead of a 100 px card. |
| New Czech captures were added for All items and People | The suite had no Czech evidence for these two screens; the toolbar, rows and archived view needed diacritic and long-label evidence. |
| The create/edit form stays inside the existing dialog/sheet | It is the shipped interaction (focus, busy lock, Escape, focus return); V4-04 restyles it rather than replacing the dialog with a page. |

## Reference-fidelity checklist (a)–(g)

| Check | Result | Evidence |
| --- | --- | --- |
| (a) Font, optical weight, navy hierarchy | Pass: Inter Variable throughout; 30 px 700 screen headings, 16 px 600 row titles and result count, 14 px muted metadata, uppercase 0.68 rem 600 eyebrows. No `font-display` utility or token remains. | `all-items-en-light-1440.png`, `people-en-light-1440.png`, `detail-open-en-light-1440.png` |
| (b) Cool off-white canvas, white surfaces | Pass: `#F1F5FA` canvas, white grouped surfaces with 1 px `#D7E2EB` dividers; sunken `#E8EFF6` only for the repeat panel and notices. | all evidence captures |
| (c) Petrol-teal CTA, coral/amber/teal status semantics | Pass: teal Search/Add/Done/Save; coral attention, teal in progress, amber waiting, mint coming up, neutral paused/archived, green done, amber conflict caution; every state also has its words and an icon. The upcoming group is captured with its mint tile, calendar icon and "Attention date" wording. | `all-items-en-light-1440.png`, `all-items-upcoming-en-light-1440.png`, `detail-done-history-en-light-1440.png`, `edit-conflict-en-light-1440.png` |
| (d) Spacing and compact grouped rows | Pass: single header band, one toolbar surface, one list surface per screen. A two-line All items row measures 57.7 px at 1440 (asserted 48–58, Home rows stay 56 px). A People row is its text plus 24 px of padding: at 390 px a type-only row measures 121 px and a member row 142 px, because the actions take their own line there; from `sm` they sit on the name line and add no height. In both locales the name column measures 268 px at 390 and no fixture name breaks mid-word. The visual suite asserts those two invariants. | `all-items-en-light-1440.png`, `all-items-en-light-768.png`, `people-en-light-1440.png`, `people-en-light-390.png`, `people-en-light-320.png` |
| (e) Navigation / bottom bar | Pass: the V4-03 rail and bottom bar are untouched; only the selected surface and the page content changed. | `all-items-en-light-390.png`, `people-en-light-390.png` |
| (f) Icon/tile styling and focus | Pass: restrained 36 px radius-12 icon tiles, visible 2 px focus rings, 44 px touch targets (the All items title link keeps a 44 px target with a −2 px outline offset, so its focus outline is drawn inside the target and never paints over a row divider), 44 × 44 targets at 320 px. | `all-items-en-light-320.png`, `people-cs-light-390.png`, `add-form-en-light-390.png` |
| (g) Real data accuracy | Pass: captures come from the production build and real PostgreSQL fixtures; counts, subjects, members, dates and recurrence text are real API values. Nothing invented, no loading or unknown labels. | every capture; Czech and dark variants included |

## Behaviour and test coverage

- Vitest (`npm run check`): 1022 tests pass. `AllItemsScreen.test.tsx` asserts the row's real
  subject-type tile, the subject pill and the written state with its hidden icon, the compact row rhythm,
  the upcoming item's "Attention date" wording (never "Needs attention" under a mint tile) and the
  archived listing's "Archived" wording with `data-item-state-kind="archived"`.
  `ItemDetailScreen.test.tsx` asserts the archived detail pill reads "Archived" with the archived tone and
  icon, that an item with no dates/repeat/responsible/notes renders no facts surface, the state pill and
  the facts order inside `[data-item-facts]`, and that Done is the only filled primary action in the
  action band. All subject, form, dialog and primitive suites pass untouched.
- Playwright visual suite: measures a two-line All items row at 1440 (48–58 px, chosen structurally from
  the rows that carry pill + state + responsible and no recurrence), asserts that the four desktop filter
  selects share one row, asserts at 320 and 390 in both locales that the "Filters" disclosure and the reset
  never intersect (expanded and collapsed) and stay inside the viewport, asserts at 390 that every People
  name column is at least 150 px, no name breaks mid-word, the actions take their own right-aligned line and
  the row stays at or below 150 px, asserts the actions sit on the name line at 768, keeps every other
  reference/Home capture unchanged, and adds Czech 390 captures for All items and People, the
  upcoming-group All items capture and 1024 px captures for both list screens.
- Playwright UX audit (`TENDO_UX_AUDIT_DIR`, opt-in): the app top bar is addressed as the `banner` landmark
  (the in-main header bands are not banners), the desktop results relationship is now "below the whole
  toolbar and full content width", and the title focus indicator is asserted to stay inside the row card.
  The audit also sets a 45 s action timeout so a blocked control fails with the intercepting element instead
  of consuming the whole audit budget. Its 320 px Czech checks are the strict originals: the first title's
  target and focus outline must end above the fixed bar. One audit-only repair was needed to get a complete
  run, and it touches no product code: the OIDC outcome captures now load each callback fragment in a fresh
  document, because Playwright's same-path hash navigation is a same-document navigation and the app
  correctly never re-reads the fragment.
- Accessibility: no role, label, `aria-*` wiring, heading level, focus target or test id changed. The
  state is never colour-only (words + icon), the tile is `aria-hidden`, and the semantic pairs are the
  measured V4 tokens.

## Evidence

All images are committed production-build Playwright captures (Chromium, fixed business date
2026-05-14, real PostgreSQL) — not mockups or reference-derived composites.

| Capture | Purpose |
| --- | --- |
| [all items 1440](v4-items-people/all-items-en-light-1440.png) | Header band, one-row toolbar, grouped result list |
| [all items 768](v4-items-people/all-items-en-light-768.png) | Tablet: Show + Filters on one line |
| [all items 1024](v4-items-people/all-items-en-light-1024.png) | Four-up toolbar, labels unbroken |
| [all items 390](v4-items-people/all-items-en-light-390.png) | Phone toolbar and stacked rows |
| [all items 320](v4-items-people/all-items-en-light-320.png) | Narrow phone, no page overflow |
| [all items upcoming 1440](v4-items-people/all-items-upcoming-en-light-1440.png) | Coming-up group: mint tile, calendar icon, "Attention date" |
| [all items no match 390](v4-items-people/all-items-no-match-en-light-390.png) | No-match panel and clear action |
| [all items error 1440](v4-items-people/all-items-error-en-light-1440.png) | Failed load with retry |
| [all items dark 1440](v4-items-people/all-items-en-dark-1440.png) | Dark toolbar and rows |
| [all items dark 390](v4-items-people/all-items-en-dark-390.png) | Dark phone |
| [all items CS 390](v4-items-people/all-items-cs-light-390.png) | Czech labels and diacritics |
| [all items large 1440](v4-items-people/all-items-large-en-light-1440.png) | 1,003-item household: 25 bounded rows, real counts |
| [add form 1440](v4-items-people/add-form-en-light-1440.png) | Create form sections |
| [add form 390](v4-items-people/add-form-en-light-390.png) | Create form, phone rhythm |
| [add repeat 1440](v4-items-people/add-repeat-en-light-1440.png) | Repeat ON: interval, after-completion switch, example |
| [add repeat 390](v4-items-people/add-repeat-en-light-390.png) | Repeat ON, phone |
| [add form CS 390](v4-items-people/add-form-cs-light-390.png) | Czech create form |
| [edit form 390](v4-items-people/edit-form-en-light-390.png) | Edit form, same sections |
| [edit conflict 1440](v4-items-people/edit-conflict-en-light-1440.png) | 412 summary with the latest saved values |
| [edit conflict 390](v4-items-people/edit-conflict-en-light-390.png) | 412 summary, phone |
| [detail open 1440](v4-items-people/detail-open-en-light-1440.png) | Header band, facts, action band, history |
| [detail open 390](v4-items-people/detail-open-en-light-390.png) | Detail, phone action band (Edit + Done share a line) |
| [detail done history 1440](v4-items-people/detail-done-history-en-light-1440.png) | Done pill, completion sentence, history list |
| [detail done history 390](v4-items-people/detail-done-history-en-light-390.png) | Done state, phone |
| [detail archived 1440](v4-items-people/detail-archived-en-light-1440.png) | "Archived" pill, archived notice, quiet Restore surface |
| [detail dark 1440](v4-items-people/detail-en-dark-1440.png) | Dark detail |
| [detail dark 390](v4-items-people/detail-en-dark-390.png) | Dark detail, phone |
| [detail CS 390](v4-items-people/detail-cs-light-390.png) | Czech detail |
| [people 1440](v4-items-people/people-en-light-1440.png) | Capped grouped list, Add in the header band |
| [people 390](v4-items-people/people-en-light-390.png) | People, phone: actions on the name line, metadata beneath |
| [people 320](v4-items-people/people-en-light-320.png) | Narrow phone: wrapped actions, 150 px+ name column, no overflow |
| [people 1024](v4-items-people/people-en-light-1024.png) | Tablet: actions on the name line |
| [people archived 1440](v4-items-people/people-archived-en-light-1440.png) | Archived view with Restore |
| [people archived 390](v4-items-people/people-archived-en-light-390.png) | Archived view, phone |
| [people dark 1440](v4-items-people/people-en-dark-1440.png) | Dark People |
| [people dark 390](v4-items-people/people-en-dark-390.png) | Dark People, phone |
| [people CS 390](v4-items-people/people-cs-light-390.png) | Czech People with a long name |

Gated baselines live in `frontend/e2e/visual/visual.spec.ts-snapshots/`. The only shared-primitive change
is the `text-h2` heading class in `dialog.tsx`/`empty-state.tsx`; because `--font-display` already resolved
to Inter Variable, no other screen's pixels changed. Every Home and reference capture is unchanged, except
the four Add-dialog captures (`home-add-light-1440`, `home-add-light-390`, `home-add-dark-1440`,
`home-add-dark-390`), which changed because Home hosts the same restyled item form in its dialog.
