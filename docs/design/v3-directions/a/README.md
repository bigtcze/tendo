# Direction A — Calm list, adaptive detail

branch=`proto/v3-direction-a`
head_sha=see Issue #324 (prototype branch is not merged)

## Intent
A direct, low-noise daily list that gets wider as the screen allows. The item itself stays the main object: check it off from the list or open its stable `/items/:id` route for facts, actions and history. This direction differs from the editorial, grouped-person board concept of direction B.

## Layout and navigation
- Phone (<768px): compact top brand/account line, single-column list, and a fixed bottom bar for Home, All items and People and things plus a visible More button. More opens a small menu with Members and Account.
- Tablet (768–1023px): list structure with wider reading width; bottom navigation remains.
- Desktop (≥1024px): 190px left rail and two useful surfaces: a list remains on the left while selected item detail opens persistently on the right; the URL stays `/items/:id`, so a deep link opens the same split view. The list beside the item is always Home, even when the item was opened from All items. Lists and facts stay near 72ch.
- Home leads with local date and household, followed by attention groups and quieter groups. A row uses a circular done target, title, compact subject/assignee/date context and a repeat mark.

## Type and color
- Body: existing system-rounded sans stack at 16px; row title 18px; section headings 24px; display heading 36px; bottom-nav labels 13px. Existing serif is reserved for brand and large headings.
- Paper `#faf7f2` page; sand `#efe9df` selected/quiet surfaces; ink `#25302b` main text; muted `#5b655f` supporting text; line `#d8d0c3` dividers; accent `#2f6b57` primary action/date; strong accent `#234f41` existing deeper action token.
- WCAG contrast, calculated from sRGB relative luminance of these hex values: ink on paper 12.79:1; muted on paper 5.66:1; white on accent 6.25:1; white on strong accent 9.27:1; accent on paper 5.85:1. These token pairs pass 4.5:1; not every rendered pair (for example muted text on sand) was measured. No new font, icon or UI package added.

## Density, interaction and household fit
- In the populated 390×844 capture, six complete rows (seven row starts) are visible; on 1440×844, eight complete rows (nine row starts). These are screenshot observations, not user-study measurements.
- Create uses the existing API-backed form and one-off default. Phone uses a full-screen sheet; desktop/tablet uses a centered dialog. Escape and Cancel close it, keyboard Tab is trapped inside, and focus returns to Add. Repeat is a real switch; its touch target is the 54×32 track itself (the surrounding row is not clickable). Fixed vs after-completion explanatory copy gives a short household example in English and Czech. Edit reuses the same form.
- Home uses the existing idempotent completion/Undo flow. Detail completion calls the existing `completeItem` and `undoCompletion` APIs, with ETag reads, result messaging and reload. Done is the only filled action inside detail; Start/Resume is outlined and Waiting, Pause, Edit and Archive are quiet. Detail completion keeps one idempotency key across retries after an uncertain result, and Undo is offered only when a completion was recorded. Detail facts and history are distinct.
- In the desktop split view the selected item owns the page's single h1, document title and screen-change focus; the list beside it uses an h2 and refreshes in place after any change made in the pane (done/undo, edit, workflow, archive).
- The title, household object and a clear done affordance aim to make a routine check understandable without project-management terminology.

## Visual review / known weaknesses
Orchestrator review of the final production-build captures (2026-11-01):
- Desktop with the persistent detail pane (`home-selected-pane-1440-en.png`, `detail-*-1440-*.png`): the left column narrows and the primary navigation wraps to a second line (Members drops below, and the phone-only More entry shows). The rail layout seen on Home at 1440 does not carry over to the split view. The nav highlights All items while the list shown is Home, because the route is `/items/:id`.
- Desktop Home (`home-populated-1440-en.png`): Add item is pushed to the far right edge, away from the list it belongs to; the list keeps a ~500px measure, leaving a wide empty right half when no item is selected.
- Rows: the title and subject sit on separate lines with a large gap, so rows read as loose pairs rather than one item; repeat glyphs are small and low-contrast.
- Phone (`home-populated-390-en.png`): attention chips wrap their counts onto a second line; the bottom bar wraps "People & things" onto two lines.
- Desktop split view keeps Done as the only filled action on screen: Add item in the list beside the pane is outlined there (`detail-1440-*.png`), and stays filled on Home without a selected item and on phone/tablet.
- Detail: the facts card repeats state ("Done", "For: Dům") as unlabelled lines; Archive sits directly under the completion notice with little separation.
- Add and Edit dialogs trap Tab and close on Escape/backdrop/Cancel; an uncertain restored create focuses its notice. Screen-reader announcement is not separately audited.
- More (phone) opens a visible Members/Account menu (`navigation-more-390-en.png`); not tested with real users.
- Repeat interval unit initially reads "years" (existing form default), while the explanatory examples speak of a monthly filter change; the default may surprise households adding a monthly task.
- Capture coverage gaps: no 360px or 1280px captures of Add, Edit or Detail; no phone All items capture; Czech captures cover Home, phone Repeat and Detail only (no desktop Czech Repeat, Czech one-off Add, Edit, Done/Undo or open More menu); no capture scrolls to Home's In progress or Waiting sections (only their counts in the overview chips are visible).
- Only focused screens carry this direction. Dark theme, other routes/states, very large backlogs, zoom and human testing are not proven. No human household participants were involved.

These are inputs for the V3-05 comparison; they are not fixed in the prototype.

## Test status
Results are for prototype code head `3bd3810`; later branch commits change only this folder. Details are on Issue #324.
- `npm run lint`, `npm run typecheck`, `npm run build`: pass.
- Vitest full suite: 788/788 pass.
- Required default PostgreSQL E2E (`scripts/e2e-smoke.sh`, production binary, real PostgreSQL): the suite passed as a whole ("Native production-binary E2E passed.", exit 0). Default E2E selection is unchanged; the capture spec is opt-in only.
- Direction capture (opt-in `TENDO_DIRECTION_CAPTURE=1` with `TENDO_UX_AUDIT_DIR`): pass; all 28 screenshots in this folder come from that run, using the production build, real PostgreSQL and authenticated synthetic households. Done/Undo captures wait for the Undo round-trip so each starts from an open item.

## Screenshot index
All links are relative to this README. Captures are production-build screenshots from the opt-in authenticated synthetic-household run.

| File | Screen | Width | Locale |
|---|---|---:|---|
| [home-empty-390-en.png](home-empty-390-en.png) | Empty Home | 390 | en |
| [home-empty-1440-en.png](home-empty-1440-en.png) | Empty Home | 1440 | en |
| [home-populated-360-en.png](home-populated-360-en.png) | Populated Home | 360 | en |
| [home-populated-390-en.png](home-populated-390-en.png) | Populated Home | 390 | en |
| [home-populated-390-cs.png](home-populated-390-cs.png) | Populated Home | 390 | cs |
| [home-populated-768-en.png](home-populated-768-en.png) | Populated Home | 768 | en |
| [home-populated-1024-en.png](home-populated-1024-en.png) | Populated Home | 1024 | en |
| [home-populated-1280-en.png](home-populated-1280-en.png) | Populated Home | 1280 | en |
| [home-populated-1440-en.png](home-populated-1440-en.png) | Populated Home | 1440 | en |
| [home-selected-pane-1440-en.png](home-selected-pane-1440-en.png) | Home with selected item open in persistent pane | 1440 | en |
| [home-populated-1440-cs.png](home-populated-1440-cs.png) | Populated Home | 1440 | cs |
| [navigation-more-390-en.png](navigation-more-390-en.png) | More menu open | 390 | en |
| [all-items-1440-en.png](all-items-1440-en.png) | All items | 1440 | en |
| [add-oneoff-390-en.png](add-oneoff-390-en.png), [add-oneoff-1440-en.png](add-oneoff-1440-en.png) | One-off add | 390/1440 | en |
| [add-repeat-fixed-390-en.png](add-repeat-fixed-390-en.png), [add-repeat-fixed-1440-en.png](add-repeat-fixed-1440-en.png) | Repeat on, fixed schedule explanation | 390/1440 | en |
| [add-repeat-390-en.png](add-repeat-390-en.png), [add-repeat-1440-en.png](add-repeat-1440-en.png), [add-repeat-390-cs.png](add-repeat-390-cs.png) | Repeat on, after-completion explanation | 390/1440 | en; 390 cs |
| [edit-390-en.png](edit-390-en.png), [edit-1440-en.png](edit-1440-en.png) | Edit | 390/1440 | en |
| [detail-390-en.png](detail-390-en.png), [detail-1440-en.png](detail-1440-en.png), [detail-390-cs.png](detail-390-cs.png), [detail-1440-cs.png](detail-1440-cs.png) | Detail/history | 390/1440 | en/cs |
| [detail-done-undo-390-en.png](detail-done-undo-390-en.png), [detail-done-undo-1440-en.png](detail-done-undo-1440-en.png) | Detail after Done with Undo | 390/1440 | en |
