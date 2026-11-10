# V4-04 All items, item lifecycle and People and things

status=V4-04_implemented
issue=#412 (V4-04)
direction=`.autonomous/V4_DESIGN_BRIEF.md` (reference images are visual targets only)
tokens=`v4-design-system.md` (V4-02)
shell=`v4-home.md` (V4-03)
siblings=`v4-auth-settings.md` (V4-05)

This applies the V4 visual system to the three task screens: All items, the item create/edit forms and
item detail, and People and things. Behaviour, routing, API contracts, roles, accessible names, heading
levels, focus ownership, test IDs and the code-point limits are unchanged; only composition, wording
placement and surfaces moved.

## What changed

- **All items** (`AllItemsScreen.tsx`, `AllItemsControls.tsx`, `AllItemsResults.tsx`): the tall left filter
  column is gone. A header band (eyebrow "YOUR HOUSEHOLD · ALL ITEMS", 30 px headline, one supporting
  sentence) introduces one white toolbar surface: search plus a teal Search action, a hairline, then
  "Show" with the status counts and the three refinements ("For", "Who's looking after it", "Repeat").
  Below `lg` the three refinements stay behind the existing "Filters (n)" disclosure; the status choice
  stays visible because it is the primary narrowing control. Results are one grouped white list of
  compact rows using the Home vocabulary: real subject-type icon tile, title link, mint subject pill,
  semantic state written out next to its icon (coral attention, teal in progress, amber waiting,
  green-teal coming up, neutral paused/archived, green done), responsible line and the recurrence
  summary. The "Showing 1–4 of 4" line is a quiet count above the list, the cursor pager keeps its
  named navigation, and empty/no-match/error/loading and the archived listing use the same composition.
- **Item create and edit** (`AddItemPanel.tsx`, `ItemForm.tsx`, `ItemFormFields.tsx`,
  `ItemEditConflictSummary.tsx`): the form is one surface divided by hairlines into everyday fields
  (title, subject, attention date), the Repeat row, who is looking after it, and the collapsed notes.
  Repeat is still OFF by default; turning it on reveals the interval row and the after-completion switch
  inside a sunken panel with the one-sentence example, and the previous-completion disclosure sits at
  its foot. The primary/secondary action band is separated by a hairline; validation errors use the
  danger pair, and the 412 conflict summary is an amber caution surface listing the latest saved values
  above the rebased draft.
- **Item detail** (`ItemDetailReady.tsx`, `ItemDetailReadView.tsx`, `ItemDetailActions.tsx`,
  `ItemDetailHistory.tsx`, `ItemDetailLoadStates.tsx`): a header band with the subject as eyebrow, the
  title and the state pill (words + icon + semantic pair). Facts live on one hairline-divided white
  surface: completion sentence, attention/last-done dates, repeat sentence and mode, responsible
  member, notes. One action band carries the quiet Edit first (its existing focus-return target), the
  filled primary Done, Start/Waiting/Pause, and Archive/Restore separated below the hairline. The
  completion/undo notice keeps its status role and focus target. History is a grouped hairline list
  under its existing heading. Loading, failed load and gone states use the eyebrow + headline + white
  panel rhythm of the full-page secondary states.
- **People and things** (`SubjectsScreenContent.tsx`, `SubjectList.tsx`, `SubjectRow.tsx`,
  `SubjectsStatus.tsx`, `SubjectForm.tsx`, `MemberPersonTransfer.tsx`): the two-column card grid is one
  grouped hairline list. Each row has the real subject-type icon tile, the name, the type, the existing
  "Household member · login" line for member-backed people, and right-aligned "See items" plus
  Edit/Restore. The primary Add action sits above the list (it keeps its DOM position, so tab order is
  unchanged). The archived view reuses the same rows with Restore, the add/edit dialog keeps its field
  order and validation, and the member-transfer section uses the same sunken panels.
- **Shared primitives** (`components/ui/dialog.tsx`, `components/ui/empty-state.tsx`): headings now use
  the V4 `text-h2` heading token instead of the V3 `font-display` utility. `--font-display` already
  resolves to Inter Variable, so this is a naming cleanup with no pixel change; it removes the last
  `font-display` usages from the task screens.
- **New shared pieces**: `components/ui/glyph.tsx` (one stroke-icon frame), `features/subjects/SubjectTypeIcon.tsx`
  (the real subject-type glyphs, now owned by the subjects feature and reused by Home and All items) and
  `features/items/itemStateTone.tsx` (the real lifecycle state → V4 semantic pair + icon, shared by
  All items rows and the detail state pill).

## Deliberate deviations

| Deviation | Reason |
| --- | --- |
| The status filter stays visible on phones instead of moving behind "Filters" | It is the primary narrowing control; hiding it would add a tap to the most common filter and reduce reachability compared with the shipped screen. The three refinements stay collapsed. |
| Desktop keeps "Show" on its own line above the three refinements | Preserves the existing DOM and tab order (status → Filters disclosure → Clear → refinement fields) while the refinements use the full content width. One 4-up row would need a different DOM order. |
| Detail keeps Edit before Done in the DOM, as a quiet control at the start of the action band | The existing keyboard order and the `editButtonRef` focus return after cancel are behaviour; Done remains the only filled primary action. |
| Row tiles use the real subject type and the real lifecycle state; no topic categories or invented badges | The API stores only the subject type and the workflow/attention state. |
| People rows use a neutral tile, not a state colour | A person or thing has no workflow state; only items do. |
| A finished or archived item shows a quiet Archive/Restore row instead of an action card | With nothing left to do, a card holding one quiet button read as an empty container. |
| All items rows show the subject pill, state and responsible member on one metadata line below the title | Same facts as before, same wording; the row stays one title line plus one metadata line (plus the recurrence line when it exists) instead of a 100 px card. |
| New Czech captures were added for All items and People | The suite had no Czech evidence for these two screens; the toolbar, rows and archived view needed diacritic and long-label evidence. |
| The create/edit form stays inside the existing dialog/sheet | It is the shipped interaction (focus, busy lock, Escape, focus return); V4-04 restyles it rather than replacing the dialog with a page. |

## Reference-fidelity checklist (a)–(g)

| Check | Result | Evidence |
| --- | --- | --- |
| (a) Font, optical weight, navy hierarchy | Pass: Inter Variable throughout; 30 px 700 screen headings, 16 px 600 row titles, 14 px muted metadata, uppercase 0.68 rem 600 eyebrows. No `font-display` remains in these screens. | `all-items-en-light-1440.png`, `people-en-light-1440.png`, `detail-open-en-light-1440.png` |
| (b) Cool off-white canvas, white surfaces | Pass: `#F1F5FA` canvas, white grouped surfaces with 1 px `#D7E2EB` dividers; sunken `#E8EFF6` only for the repeat panel and notices. | all evidence captures |
| (c) Petrol-teal CTA, coral/amber/teal status semantics | Pass: teal Search/Add/Done/Save; coral attention, teal in progress, amber waiting, mint coming up, neutral paused, green done, amber conflict caution; every state also has its words and an icon. | `all-items-en-light-1440.png`, `detail-done-history-en-light-1440.png`, `edit-conflict-en-light-1440.png` |
| (d) Spacing and compact grouped rows | Pass: single header band, one toolbar surface, one list surface per screen; a two-line All items row measures 58 px at 1440 (asserted 48–58 in the visual suite, Home rows stay 56 px), People rows 63–74 px where the member line exists. | `all-items-en-light-1440.png`, `all-items-en-light-768.png`, `people-en-light-1440.png` |
| (e) Navigation / bottom bar | Pass: the V4-03 rail and bottom bar are untouched; only the selected surface and the page content changed. | `all-items-en-light-390.png`, `people-en-light-390.png` |
| (f) Icon/tile styling and focus | Pass: restrained 36 px radius-12 icon tiles, 2 px focus rings on every control, 44 px touch targets (including the title link and the row actions), 44 × 44 targets at 320 px. | `all-items-en-light-320.png`, `people-cs-light-390.png`, `add-form-en-light-390.png` |
| (g) Real data accuracy | Pass: captures come from the production build and real PostgreSQL fixtures; counts, subjects, members, dates and recurrence text are real API values. Nothing invented, no loading or unknown labels. | every capture; Czech and dark variants included |

## Behaviour and test coverage

- Vitest (`npm run check`): 1019 tests pass. `AllItemsScreen.test.tsx` now also asserts the row's real
  subject-type tile, the subject pill and the written state with its hidden icon, plus the compact row
  rhythm. `ItemDetailScreen.test.tsx` asserts the state pill (`data-item-state` with a hidden icon) and
  the facts order inside `[data-item-facts]`; the existing workflow, completion, undo, history, conflict
  and focus assertions are unchanged. All subject, form, dialog and primitive suites pass untouched.
- Playwright: the visual suite now measures a two-line All items row at 1440 (48–58 px), keeps every
  existing reference/Home capture unchanged, and adds Czech 390 captures for All items and People. The
  edit-conflict capture is taken from the top of the dialog so the conflict summary is visible evidence.
- Native production E2E (`scripts/e2e-smoke.sh`, Chromium/Firefox/WebKit, real binary and PostgreSQL):
  the full suite passed on the final source, including the serial journey (All items search/filter/paging,
  detail workflow and history, People create/edit/archive/transfer), the axe scans on All items, detail
  and the People edit form, the 320 px overflow checks and the 200%/400% zoom sweeps.
- Accessibility: no role, label, `aria-*` wiring, heading level, focus target or test id changed. The
  state is never colour-only (words + icon), the tile is `aria-hidden`, and the semantic pairs are the
  measured V4 tokens.

## Evidence

All images are committed production-build Playwright captures (Chromium, fixed business date
2026-05-14, real PostgreSQL) — not mockups or reference-derived composites.

| Capture | Purpose |
| --- | --- |
| [all items 1440](v4-items-people/all-items-en-light-1440.png) | Header band, toolbar, grouped result list |
| [all items 390](v4-items-people/all-items-en-light-390.png) | Phone toolbar and stacked rows |
| [all items 320](v4-items-people/all-items-en-light-320.png) | Narrow phone, no page overflow |
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
| [detail open 390](v4-items-people/detail-open-en-light-390.png) | Detail, phone action band |
| [detail done history 1440](v4-items-people/detail-done-history-en-light-1440.png) | Done pill, completion sentence, history list |
| [detail done history 390](v4-items-people/detail-done-history-en-light-390.png) | Done state, phone |
| [detail archived 1440](v4-items-people/detail-archived-en-light-1440.png) | Archived notice and quiet Restore |
| [detail dark 1440](v4-items-people/detail-en-dark-1440.png) | Dark detail |
| [detail dark 390](v4-items-people/detail-en-dark-390.png) | Dark detail, phone |
| [detail CS 390](v4-items-people/detail-cs-light-390.png) | Czech detail |
| [people 1440](v4-items-people/people-en-light-1440.png) | Grouped person/thing list |
| [people 390](v4-items-people/people-en-light-390.png) | People, phone |
| [people archived 1440](v4-items-people/people-archived-en-light-1440.png) | Archived view with Restore |
| [people archived 390](v4-items-people/people-archived-en-light-390.png) | Archived view, phone |
| [people dark 1440](v4-items-people/people-en-dark-1440.png) | Dark People |
| [people dark 390](v4-items-people/people-en-dark-390.png) | Dark People, phone |
| [people CS 390](v4-items-people/people-cs-light-390.png) | Czech People with a long name |

Gated baselines live in `frontend/e2e/visual/visual.spec.ts-snapshots/`. The only shared-primitive
change is the `text-h2` heading class in `dialog.tsx`/`empty-state.tsx`; because `--font-display` already
resolved to Inter Variable no other screen's pixels changed (every Home, reference, members, account,
admin, login and onboarding baseline is unchanged). The Add dialog captures changed because the form
itself is restyled.
