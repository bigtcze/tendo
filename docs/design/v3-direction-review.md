# V3 direction review — A vs B, and the chosen direction

issue=#326 (V3-05)
inputs=`docs/design/v3-baseline-audit.md`, `docs/design/v3-directions/a/README.md`, `docs/design/v3-directions/b/README.md`, all committed captures in those folders, prototype branches `proto/v3-direction-a` and `proto/v3-direction-b`
decision=Direction A visual language and list-first layout, with Direction B's dedicated-page model (item detail is its own page; no persistent split pane)

"Dedicated-page model" means one route/screen owns the page heading, document title, focus and actions. It does not mean B's subject grouping is adopted, and it has nothing to do with which household member owns a task.

## 0. What this is and is not
- AI critique only. Two independent reviewer sessions, separate from the prototype implementers, reviewed the evidence: a design reviewer looked at the screenshots, and an architecture reviewer read the prototype diffs. The orchestrator wrote this synthesis.
- No household members, real participants or human approvers took part. Nothing here claims human approval, measured usability or aesthetic certainty.
- Journey counts for the prototypes are estimates from visible controls. Only the V2.1 baseline counts (audit §6, J1–J9) were measured by scripted Playwright runs.
- The reviewers did not rerun tests. Prototype test results are the claims recorded in the direction READMEs and on Issues #324/#325.

## 1. The two directions in brief
| | A — Calm list, adaptive detail | B — Household overview by who and what |
|---|---|---|
| Home | One list with attention groups first; round done control in each row; subject/date/assignee below the title | Editorial intro and tagline, "Add something to remember" opener; Needs attention grouped under subject headings; outlined Done button in each row |
| Phone nav | Bottom bar (Home, All items, People and things, More) | Header links wrapping onto two lines, More expands inline |
| Desktop | 190px left rail; selecting an item opens a persistent detail pane beside the Home list at `/items/:id` | Five-link header, 1440px shell, Needs attention left and secondary groups right; no pane |
| Add/edit | Add: full-screen sheet on phone, centered dialog on desktop/tablet. Edit: centered modal, including on phone. Repeat as a switch | Inline expansion on Home; inline edit on detail; Repeat as a checkbox |
| Detail | In the pane (desktop) or as a page (phone); Done is the only filled action | Dedicated page; Mark done is the only filled action; Undo replaces it |
| Frontend diff vs main | 38 files, +983/−381 | 16 files, +290/−61 |

## 2. Comparison by criterion
Severity: major = real friction against a locked V3 target; minor = visible issue to fix.

| # | Criterion | Visual review | Code review | Key evidence |
|---|---|---|---|---|
| 1 | Readability and type hierarchy | **A** | — | A: date → household → groups → rows reads cleanly (`a/home-populated-390-en.png`); repeat glyph small and faint, title and subject on separate lines (minor). B: intro, tagline, opener, jump links and two heading levels compete with the list (`b/home-populated-390-en.png`, major). |
| 2 | User effort (journeys) | **A, narrowly** | — | See §3. Counts are similar; A keeps forms contained and puts attention rows higher. |
| 3 | Visual coherence and identity | **A** | — | Both keep the warm paper/ink/green palette and serif display without copying a known product. B's tagline and generous pauses read over-composed for a daily check (`b/home-empty-390-en.png`). A strands Add item at the far right edge at 1440 (`a/home-populated-1440-en.png`, minor). |
| 4 | Accessibility | A (visual), unknowns | **B, narrowly** | Screenshots: B's Repeat is a small native checkbox (`b/add-repeat-fixed-390-en.png`); A's repeat glyph has no visible text. Code: A's desktop split renders two `<main id="main">` elements (duplicate id, competing skip target); A's dialog trap does not make the background inert; on A's Home rows the visible subject/assignee metadata is hidden from assistive technology, and the accessible replacement keeps only attention/history text. B keeps one main/h1 but shows recurrence text only on hover/focus (`display:none`). |
| 5 | Density, 5 vs 1,000+ | **A** (visible list only) | — | Screenshot observations at the captured fold, not a general density result: A shows six complete rows at 390×844 and eight at 1440×844. B shows 3–4 at 390 and 1–2 at 1440; its empty Home has four stray dividers (`b/home-empty-390-en.png`). Neither direction has a 100 or 1,000+ capture. |
| 6 | Responsive, desktop void | **B, narrowly** | — | A: an empty right half without a selection; in split view the nav wraps, Members drops a line and the phone-only More appears (`a/home-selected-pane-1440-en.png`, major). B: shared grid rows push Needs attention ~200px down at 1024/1440 (`b/home-populated-1024-en.png`, `b/home-populated-1440-en.png`, major). Neither is acceptable unchanged. |
| 7 | Keyboard/touch discoverability | **A** | — | A's More is visible and Done is on list and detail. B's More pushes the page down; B's outlined row Done can read as secondary. A's round control needs a clear accessible name and a 44px target. |
| 8 | Household fit, plain words | **B** | — | B's subject headings make people and things prominent. A shows the subject in each row; its detail "For: Dům" line reads like system output. |
| 9 | Czech copy fit | **A, slightly** | — | A: attention chip counts wrap at 390 cs (`a/home-populated-390-cs.png`). B: long Czech opener; secondary-row metadata wraps at 1024 (per README); Repeat uses a checkbox in cs as well. |
| 10 | Maintainability | B, narrowly | **B** | A mounts two full screens in the split view, with one-way refresh only (completing in the list does not refresh the pane), an All items/Home nav-active mismatch, a "Back to all items" link that can return to another detail, and 231 inserted lines of global CSS, including structural shell overrides (one existing line replaced; net growth 230 lines). B is smaller, but `ItemDetailScreen.tsx` mixes rendering with a second completion controller, and its grouping keys on display name rather than subject id. **Both** duplicate completion handling instead of reusing `frontend/src/features/items/completionOutcomes.ts`. |

## 3. User effort against the measured baseline
The baseline numbers were measured on shipped V2.1 (audit §6). The prototype columns are reviewer estimates from the captures, not measurements.

| Journey | V2.1 measured interactions | A estimate | B estimate | Note |
|---|---:|---:|---:|---|
| J1 add one-off | 4 | ~4 | ~4 | A's sheet/dialog keeps Home stable; B's inline form pushes the list down. |
| J2 set recurring | 15 (fixed + after-completion) | ~7–9 per item | ~7–9 per item | Both start the interval unit at "years"; this may surprise people adding monthly or weekly tasks and needs a deliberate decision in V3-10. |
| J3 find attention by keyboard | 14 Tab stops | not measured | not measured | Visually A puts attention rows higher; B has more stops before the list. Must be measured in rollout. |
| J4 done + undo | 2 | 2 | 2 | Both offer Done on the row and in detail. |
| J5 find old item | 4 | not captured on phone | ~4, filters fill the first phone screen | Neither redesigned All items. |
| J7 invite member | 2 (on Members: create + copy) | ~4 from Home, estimated (≈2 to reach Members via More + 2) | ~4 from Home, estimated (same) | Not like-for-like: the baseline starts on Members. Members was not prototyped by either. |

Neither direction substantially reduces measured effort on its own. The real gains must come from the rollout: a sensible recurrence default, fewer Tab stops before the first row, and a phone-friendly All items.

## 4. Decision
**Adopt Direction A's visual language and list-first layout as the single V3 direction, and replace its persistent desktop split pane with Direction B's dedicated-page model: item detail is its own page.**

Why one direction, not two themes: there is one visual language (A) and one structure everywhere. B contributes the one-screen-one-owner rule, not a second look.

Why A as the base:
- Better on most user-facing criteria (1, 2, 3, 5, 7, 9): calmer hierarchy, contained add/edit, more rows on screen, clearer daily action.
- It fits the brief's "Things 3 / Reminders level of restraint" intent better than B's editorial home, without copying either.

Why not A's split pane:
- The split composition introduced duplicate main landmarks, competing data lifecycles, stale pane content after completing from the list, navigation/context confusion (wrong active nav, broken return context) and additional shell overrides. It also caused the worst visual defect in A (`a/home-selected-pane-1440-en.png`).
- A dedicated page avoids those by construction and keeps the existing, well-tested `/items/:id` behavior (V2.1 routing, focus and return-context tests).
- Separately, and **not** fixed by removing the pane: A's global desktop shell reserves navigation width on sign-in/setup screens, its modal isolation is incomplete, and its row metadata loses accessible subject/assignee text. Duplicated completion handling exists in both prototypes. These are tracked in §6 and §7.

How desktop width could be used without a pane. These are **hypotheses, not demonstrated by either prototype**. V3-06/V3-07/V3-08/V3-10 must review them in empty, populated and long-content states (including long Czech strings) and drop any part that creates a new void or does not earn its space:
- A left rail at ≥1024px for primary navigation, used on every signed-in route, including detail.
- Home: Needs attention as the main column; In progress / Waiting / Coming up in a secondary column with **independent** column heights (no shared grid rows, avoiding B's gap). Add item sits with the list it belongs to.
- Rows: a responsive row rule that keeps the title and essential context (subject, date, assignee) scannable without clipping; one line where width allows, wrapping cleanly on phones and with long names.
- Detail page: facts and actions beside history at ≥1024px, only if both keep a readable measure.

Accepted tradeoffs:
- On desktop, opening an item leaves the list (one click back). We accept this in exchange for one owner per screen, simpler focus and h1 handling, and preserved V2.1 behavior. A split view could come back later as a separate, fully tested decision; it is not part of v3.0.0.
- Subject grouping on Home (B's main idea) is not adopted for v3.0.0; see §5.
- The phone bottom bar stays, so it must handle long labels such as "People and things" and Czech text.

## 5. Elements borrowed and rejected
Borrowed from B:
- Dedicated detail page with a single main, h1, title and focus owner.
- Plain, warm opener copy ("Add something to remember") as a candidate empty-state / add prompt, subject to V3-14 copy review.
- A visible text "Done" label on the detail page and wherever the row control's meaning is not obvious; recurrence is never icon-only.

Kept from A:
- Palette, type scale, list rhythm and the round row done control. It needs a ≥44px hit target and an accessible name ("Mark <title> done"), checked in V3-15.
- Phone full-screen sheet and desktop dialog for Add; the same pattern is chosen for Edit going forward (A's prototype Edit was still a centered modal on phone). Repeat as a switch with household examples.
- Done as the only filled action on any screen showing detail.

Rejected:
- A's persistent split pane, desktop shell override and handwritten focus trap.
- B's tagline, editorial intro, inline add form on Home, header-only phone nav and four-divider empty state.
- B's subject grouping on Home. Its value is real, but the current implementation groups by display name and only over loaded pages, so rows reorder as pages and names load. The subject stays visible in each row. Any later grouping must key on subject id over a stable order.

Prototype branches: `proto/v3-direction-a` and `proto/v3-direction-b` stay on origin, unmerged, as reference. Neither will be merged wholesale; rollout PRs re-implement chosen patterns from `main`.

## 6. Constraints for every rollout slice
- One completion behavior: reuse/refactor the existing completion command and outcome handling (`frontend/src/features/items/completionOutcomes.ts`) rather than adding a second controller. Preserve pending-key recovery, definitive versus uncertain outcomes, replay of an already-undone completion, authentication handling, stale-response guards and tracked read-before-write. Within detail, Done/Undo share the existing edit/workflow/archive mutation lock (not an application-global lock).
- Dialogs and sheets: real modal semantics (inert background, Escape), via native `<dialog>` or a justified existing primitive, not a new library. One owner for returning focus, no dismissal while a save is pending, and a fallback when the dialog has no focusable control.
- Changing breakpoint or browser zoom while adding/editing keeps the draft, validation/conflict state and pending operation, without remounting the form or stealing focus.
- No essential information available only on hover. Disclosed content needs a keyboard/touch-accessible control; recurrence and subject stay readable to touch and screen-reader users.
- Shell semantics on every route: one `main`, unique ids, one h1/title/focus owner; no empty navigation rail on setup, sign-in or household-less screens.
- Composable layout primitives instead of global structural CSS overrides. Dates go through locale resources and the server's household evaluation context, not UTC "today"; test midnight/timezone boundaries.
- Preserve and do not weaken V2.1 tests, especially `App.routing.test.tsx` return context, `ItemDetailScreen.test.tsx` locks/focus/failures, and `journey.spec.ts` n11–n16b, n23, n24 and n27. n24 covers lost create responses, so detail completion recovery needs its own new tests. Locators can be scoped more precisely; assertions must not be removed.
- Each UI slice updates the opt-in `ux-audit.spec.ts` and affected screen-reader flows in the same PR (LEARNINGS L011); V3-16 consolidates.

## 7. Problems to fix, with owning issue
These are acceptance input for the owning managed Issue.

| # | Problem | Evidence | Owner |
|---|---|---|---|
| 1 | Define tokens, type scale, spacing, row anatomy, focus ring, light/dark roles and contrast table for the chosen direction; layout primitives replace structural global CSS | §2 rows 1, 3, 10 | V3-06 #327 |
| 2 | Dialog/sheet, switch, button hierarchy (one filled action), round done control (≥44px, named), empty state, notice primitives built to §6, including the draft-preserving responsive form contract and a shared date-formatting contract | §2 row 4; §5; §6 | V3-06 #327 |
| 3 | Signed-in shell: left rail ≥1024 on every route including detail; shell variants without a rail for setup/sign-in/household-less; phone bottom bar without wrapping (320/360/390, en/cs); correct active item; no stray More on desktop; More dismissal and focus, safe-area padding, fixed-bar occlusion at 320px and zoom | `a/home-selected-pane-1440-en.png`, `a/home-populated-390-en.png`; A `frontend/src/index.css` desktop `.app-frame` grid | V3-07 #328 |
| 4 | Desktop Home composition: independent columns, Add near the list, no void, no group gap; attention chip counts that do not wrap | `a/home-populated-1440-en.png`, `b/home-populated-1024-en.png`, `b/home-populated-1440-en.png`, `a/home-populated-390-cs.png` | V3-08 #329 |
| 5 | Responsive row rule (one line where width allows, clean wrapping otherwise); repeat cue with text/accessible name; measure Tab stops to the first row against baseline J3 (14); visible focus and screen-reader name of the row done control | §2 rows 1, 4; §3 | V3-08 #329 |
| 6 | One shared completion behavior (§6). #329 owns the shared behavior for rows; #331 owns detail integration and parity tests (401, 409, 412, lost response, already-undone replay, focus after Done and Undo) | A `items/relativeAttention.ts`, `items/ItemDetailScreen.tsx`; B `items/ItemDetailScreen.tsx` on the prototype branches | V3-08 #329, then V3-10 #331 |
| 7 | All items on phone: search visible, filters disclosed progressively, results in the first view; 1,000+ paging preserved | `b/all-items-390-en.png`; no A phone capture | V3-09 #330 |
| 8 | Recurrence default unit is "years"; decide a default deliberately (or require a choice) and validate it; Czech/English examples; draft kept across breakpoint/zoom changes while editing | `a/add-repeat-fixed-390-en.png`, `b/add-repeat-fixed-390-en.png` | V3-10 #331 |
| 9 | Detail page: two-column facts/history at ≥1024; labelled facts instead of "For: Dům"; clear separation of Archive; no empty band after Done | `a/detail-1440-en.png`, `b/detail-done-undo-1440-en.png` | V3-10 #331 |
| 10 | People and things, Members, Account, setup/login were not prototyped by either direction and must be designed in the chosen system | READMEs | V3-11 #332, V3-12 #333, V3-13 #334 |
| 11 | Dark theme, Czech wrapping at all widths, long names, 200/400% zoom, 1,000+ captures, focus order and Tab stops, visible focus, screen-reader checks, contrast of rendered pairs (not only tokens), form state across resize/zoom | §0; both READMEs | V3-14 #335, V3-15 #336, V3-16 #337 |

## 8. Open risks
- Without a split pane, desktop users go back and forth between list and detail. Watch this in the V3-08/V3-10 screenshot reviews; do not fill the space with decoration.
- The round done control may be less discoverable than a labelled button for first-time users. No real-user evidence exists; V3-15 must check the name, target and focus state, and use a visible label if review finds it unclear.
- Density gains are only shown for small fixtures. 100 and 1,000+ item views must be captured before V3-08/V3-09 close.
- The bottom bar with long Czech labels may still wrap at 320px; V3-07 must prove 320/360 in Czech.
- This decision rests on AI review of prototype captures and code, not on household testing.
