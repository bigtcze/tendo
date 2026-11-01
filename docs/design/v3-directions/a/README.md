# Direction A — Calm list, adaptive detail

branch=`proto/v3-direction-a`
head_sha=to_be_filled_by_orchestrator

## Intent
A direct, low-noise daily list that gets wider as the screen allows. The item itself stays the main object: check it off from the list or open its stable `/items/:id` route for facts, actions and history. This direction differs from the editorial, grouped-person board concept of direction B.

## Layout and navigation
- Phone (<768px): compact top brand/account line, single-column list, and a fixed bottom bar for Home, All items and People and things plus a visible More button. More opens a small menu with Members and Account.
- Tablet (768–1023px): list structure with wider reading width; bottom navigation remains.
- Desktop (≥1024px): 190px left rail and two useful surfaces: the Home/All items list remains on the left while selected item detail opens persistently on the right; the URL remains `/items/:id`, including deep links, reload and browser Back. Lists and facts stay near 72ch. On deep-link reload, Home provides the list context beside the selected item.
- Home leads with local date and household, followed by attention groups and quieter groups. A row uses a circular done target, title, compact subject/assignee/date context and a repeat mark.

## Type and color
- Body: existing system-rounded sans stack at 16px; row title 18px; section headings 24px; display heading 36px; bottom-nav labels 13px. Existing serif is reserved for brand and large headings.
- Paper `#faf7f2` page; sand `#efe9df` selected/quiet surfaces; ink `#25302b` main text; muted `#5b655f` supporting text; line `#d8d0c3` dividers; accent `#2f6b57` primary action/date; strong accent `#234f41` existing deeper action token.
- WCAG contrast, calculated from sRGB relative luminance: ink on paper 12.6:1; muted on paper 5.6:1; white on accent 6.0:1; accent on paper 5.6:1. Each exceeds 4.5:1 for normal text. No new font, icon or UI package added.

## Density, interaction and household fit
- In the populated 390×844 capture, six row starts are visible; on 1440×844, eight are visible. These are screenshot observations, not user-study measurements.
- Create uses the existing API-backed form and one-off default. Phone uses a full-screen sheet; desktop/tablet uses a centered dialog. Escape and Cancel close it, keyboard Tab is trapped inside, and focus returns to Add. Repeat is a real switch with a 54×32 track inside a larger touch row. Fixed vs after-completion explanatory copy gives a short household example in English and Czech. Edit reuses the same form.
- Home uses the existing idempotent completion/Undo flow. Detail completion calls the existing `completeItem` and `undoCompletion` APIs, with ETag reads, result messaging and reload. Workflow, edit and archive stay secondary. Detail facts and history are distinct.
- The title, household object and a clear done affordance aim to make a routine check understandable without project-management terminology.

## Visual review / known weaknesses
- Final captures are still pending after these larger layout changes. The prior captures document the earlier inline-add/single-route state and are not current evidence for the completed concept.
- The add sheet now traps keyboard focus and has Escape, backdrop, and Cancel close paths. Confirm visual polish and screen-reader announcement in the next capture/review round.
- More opens a visible Members/Account menu, as shown in `navigation-more-390-en.png`; it has not been tested with real phone users.
- Repeat and navigation marks now use inline SVG. Repeat cadence stays available to assistive technology in the list; list metadata intentionally shows a short relative date. Validate low-vision contrast and keyboard ordering against fresh captures.
- Only focused screens have this direction’s styling. Dark theme, other routes/states, very large backlogs, zoom and actual human testing are not proven. No human household participants were involved.

## Test status
- `npm run lint`, `npm run typecheck`, `npm run build`: latest combined run is pending after the dialog and All items row refinements.
- Vitest full suite: passed 785/785 before the most recent accessibility expectation edits. Targeted item suites are being rerun.
- Direction capture: passed 1/1 and refreshed the screenshot matrix, including `home-selected-pane-1440-en.png`. A small subsequent edit changes the Done/Undo capture viewport sequence; rerun capture for final evidence.
- Required default PostgreSQL E2E: capture mode still leaked into a default run; journey case `a` failed onboarding, 50 did not run. Native runner selection isolation remains unresolved; do not claim a clean default pass.

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
| [home-populated-1440-en.png](home-populated-1440-en.png) | Populated Home | 1440 | en |
| [home-selected-pane-1440-en.png](home-selected-pane-1440-en.png) | Home with selected item open in persistent pane | 1440 | en |
| [home-populated-1440-cs.png](home-populated-1440-cs.png) | Populated Home | 1440 | cs |
| [navigation-more-390-en.png](navigation-more-390-en.png) | More menu open | 390 | en |
| [all-items-1440-en.png](all-items-1440-en.png) | All items | 1440 | en |
| [add-oneoff-390-en.png](add-oneoff-390-en.png), [add-oneoff-1440-en.png](add-oneoff-1440-en.png) | One-off add | 390/1440 | en |
| [add-repeat-390-en.png](add-repeat-390-en.png), [add-repeat-1440-en.png](add-repeat-1440-en.png), [add-repeat-390-cs.png](add-repeat-390-cs.png) | Repeat on | 390/1440 | en; 390 cs |
| [edit-390-en.png](edit-390-en.png), [edit-1440-en.png](edit-1440-en.png) | Edit | 390/1440 | en |
| [detail-390-en.png](detail-390-en.png), [detail-1440-en.png](detail-1440-en.png), [detail-390-cs.png](detail-390-cs.png), [detail-1440-cs.png](detail-1440-cs.png) | Detail/history | 390/1440 | en/cs |
| [detail-done-undo-390-en.png](detail-done-undo-390-en.png), [detail-done-undo-1440-en.png](detail-done-undo-1440-en.png) | Detail after Done with Undo | 390/1440 | en |
