# V5 design audit and direction choice

status=V5-05_evidence (Issue #449)
source_commit=5bd7647 (main, application code identical to published v4.0.0 UI)
approval=none. No human has reviewed, approved or user-tested anything in this document. The chosen direction is an
agent decision based on the evidence below and stays open to the owner's override.
governing=`.autonomous/V5_DESIGN_BRIEF.md`, `.autonomous/V5_SCOPE.md` (UI bullets). Historical V4 reference PNGs are not
evidence here.

## 1. Method

Capture harness: `frontend/e2e/design-capture.spec.ts` with `frontend/e2e/design/{capture,metrics,seed}.ts`, opt-in via
`TENDO_DESIGN_CAPTURE_DIR=<dir> bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` (not part of required CI). It runs
the real production build against native PostgreSQL with the fixed business date 2026-05-14, Chromium, reduced motion.

Matrix: 20 states x 6 widths (320, 390, 768, 1024, 1440, 1920) x 2 languages (cs, en) x 2 explicit themes (light,
dark) = 480 viewport captures, plus the 1000+ item Home at 390/1440 (8 viewport captures) and full-page Home and detail
at 390/1440 (16 captures): 504 entries per run. Fold height
is 844 below 768, 1024 at 768 and 900 above. Each capture is paired with measurements in `metrics.json`:

| Metric | Meaning |
| --- | --- |
| `horizontalOverflow` | any element past the document width |
| `smallestVisibleTextPx` | smallest computed font size among visible text |
| `smallTargets` | interactive targets under 44 x 44 px |
| `banner` | top-bar box and whether it spans the full viewport |
| `mainInset` | `main` landmark left/right offset from the viewport |
| `h1` | page title size and position |
| `firstRowY`, `rowsFullyAboveFold` | first item row position and rows visible without scrolling |
| `primaryAction.containerRightGap` | distance from the primary button to its parent's right edge |
| `domElements` | element count of the document |

States: onboarding, login, login-error, invite-accept, reset-password-invalid, home-empty, home-populated (10-item
reference household), home-large (1000+ items), all-items, all-items-no-match, item-detail, item-detail-history,
add-item, add-item-repeat, edit-item, people, members, account, admin, not-found, server-error.

Runs (scratch evidence, not committed in full; about 31 MiB each). All three were regenerated back to back on
2026-11-12 with the identical harness, under one lock against the same native PostgreSQL 18.6 test cluster:

| Run | Build | Result | `metrics.json` SHA-256 |
| --- | --- | --- | --- |
| v4 baseline | `main` 5bd7647 | exit 0, 504 entries (488 viewport + 16 full-page) | `78247de99ffcda3ebbdc0582435411b2612e93646714fa805755cf40f72147ab` |
| Direction B | `proto/v5-direction-b` 3174c0d | exit 0, 504 entries | `f55f2c75c158ecba7c5f5f14d7fc578a7fd9c5fadb892cbc789d5bac9c0eab58` |
| Direction A | `proto/v5-direction-a` 0b138d2 | exit 0, 504 entries | `a37a88418de6eb5bc282393b95eac54c161e2814a96c7653666b763ab4aa3b5a` |

Measurement caveat: the small-target check runs right after the viewport is resized. A single capture per run can
catch the phone bottom bar mid-reflow (links measured 4 x 56 px while the screenshot shows a normal bar). In this run
that hit v4 `item-detail-cs-dark-320` (5 bottom-bar links); an earlier run hit B `admin-cs-dark-320` instead. Such
one-off 4 px-wide readings are treated as harness noise, not as findings, and are called out where they appear.

Curated v4 screenshots are in [`v4/`](v4/); curated direction screenshots are in [`a/`](a/) and [`b/`](b/). All three
folders contain the same file names (`<state>-<lang>-<theme>-<width>.png`, plus `admin-cs-dark-320` and
`item-detail-en-light-1440` for the directions), so any state can be compared side by side by opening the same name
in each folder. All curated PNGs come from the three runs listed below.

## 2. v4 audit (production build)

### 2.1 Measured, all 488 viewport captures

| Check | Result |
| --- | --- |
| Horizontal overflow | 0 captures |
| Targets under 44 px | 0 real offenders (one mid-reflow reading, see the caveat in section 1) |
| Smallest visible text | 12 px (meta and helper text) |
| Top bar full width | no at 1920 on every signed-in route (1440 px frame at x=240); no at 1440 and 1920 on every signed-out route (1152 px frame at x=144/384) |
| Top bar height | 56 px desktop, 61 px at 320–768 signed in |
| Home h1 | 34 px phone, 36 px tablet, 40 px desktop, with decorative coloured period |
| Home first item row | y=409 phone (446 when the 320 px count pill wraps), 396 tablet, 327 desktop |
| Home rows above the fold | 3 at 320/390, 6–7 at 768, 6 at 1024, 7 at 1440/1920 (10-item household); 5 and 10 with 1000+ items |
| All items first row | y=456 phone, 480 desktop; 3 rows above the fold at 390 |
| Home primary action right gap | 0 px to its container at every width (desktop and the full-width phone button) |
| Home DOM | 398 elements for 10 items, 1298 for 1000+ items (bounded pagination holds) |

The measured accessibility basics are sound. The problems are composition, hierarchy and density, which the numbers
expose only indirectly (the first row starts 48 % of the way down a phone screen and 36 % down a desktop one).

### 2.2 Findings by brief concern

**Giant hero.** Home opens with an eyebrow (`YOUR HOME · OVERVIEW` / `VÁŠ DOMOV · PŘEHLED`), the household name as a
34–40 px h1 with a teal period, then a tagline, then the Add button, then a pill row. On a 390 px phone this takes
y=60–360 before the first row ([cs 390](v4/home-populated-cs-light-390.png), [cs 320](v4/home-populated-cs-light-320.png)).
At 320 px the first group's count pill wraps to its own line. The same oversized block repeats on the server-error
state, where it pushes the only useful content (the retry) below a hero ([error](v4/server-error-cs-dark-390.png)), and on
empty Home ([empty](v4/home-empty-cs-dark-1440.png)).

**Pointless tagline.** Three slogans compete: the top-bar `HOME, HANDLED.` / `DOMOV POD KONTROLOU.`, the Home tagline
"A calmer way to keep the important things moving." / "Klidnější způsob, jak udržet důležité věci v pohybu.", and group
captions "Tasks requiring action" / "Your household, at a glance" that restate the group title. Each page below Home
repeats the pattern with an eyebrow plus a one-sentence subtitle (All items, People, Members, Account, Admin). None of
them carries a fact or an action.

**Framed header.** At 1920 the signed-in shell, header included, is a centred 1440 px frame: page background shows on
both sides of the header ([1920](v4/home-populated-en-light-1920.png), [detail 1920](v4/item-detail-en-light-1920.png)).
Signed-out screens frame the header at 1152 px already from 1440 ([login 1920](v4/login-cs-light-1920.png),
[onboarding 1440](v4/onboarding-en-light-1440.png)). The header holds a tagline but no household switcher; the household
name lives only in the Home h1.

**Edge insets.** Insets are mostly consistent (20 px phone, 32 px tablet, 40 px desktop), but primary actions are glued
to their container's right edge (0 px gap measured everywhere), and the phone Add button is a full-bleed slab. The
1920 layout leaves 240 px of dead page on each side while the item list ends 40 px from the frame. The pill row on
phones is cut off mid-pill at the right edge rather than wrapping ([cs 390](v4/home-populated-cs-light-390.png)).

**Poor hierarchy.** Status is said three times per row group: a coloured pill row at the top, a group heading with a
count pill and a caption, and per-row red "Needs attention since …" text. Every row carries a subject pill, so pills
are the loudest repeated element. On desktop the row puts subject, date and assignee in widely separated columns with
large unexplained gaps ([1024](v4/home-populated-en-light-1024.png)). Detail stacks Edit, Done, Start, Waiting, Pause
as equal-weight buttons and Archive in a separate card; Done is coloured but not separated from status changes
([detail](v4/item-detail-en-light-1920.png), [phone](v4/item-detail-cs-dark-390.png)). The sidebar highlights
"All items" on an item opened from Home.

**Generic AI styling.** Rounded-everything cards around every list, pill counts on headings, uppercase letter-spaced
eyebrows on every page, icon tiles on every row, and a decorative period on headlines read as a template dashboard
rather than a household tool. The People page is a stack of cards each with a "See items" link and an Edit button
([people 390](v4/people-en-light-390.png)); Members and Admin are cards inside cards with a subtitle under each
([members](v4/members-cs-light-1440.png), [admin](v4/admin-en-dark-1440.png)); Account is four separate cards for four
short settings and leaves 400 px empty at 1440 ([account](v4/account-cs-light-1440.png)).

### 2.3 Other route/state findings

| Route/state | Finding | Evidence |
| --- | --- | --- |
| Add/edit form | Modal on desktop, full screen on phone; one long column with dividers between every field; Cancel appears twice (header and footer); notes is a collapsed text link with weak affordance | [edit 1440](v4/edit-item-en-light-1440.png), [add 390](v4/add-item-cs-dark-390.png) |
| Add/edit form | Date field shows the browser's `mm/dd/yyyy` placeholder in Czech | [add 390](v4/add-item-cs-dark-390.png) |
| All items | Filter panel is a large card above results; on phones search and filters push the first result to y≈455; rows repeat the full status sentence with the year | [1440](v4/all-items-en-light-1440.png), [390](v4/all-items-cs-dark-390.png) |
| Detail | Facts card plus actions card plus history card; history is unpaginated | [1920](v4/item-detail-en-light-1920.png) |
| Large data | 1000+ items: 519 "needs attention" rows render in groups; phone shows 5 rows above the fold | [390](v4/home-large-en-dark-390.png) |
| Phone navigation | Bottom bar has Home, Items, People, Members, Account; Archive and Admin center are not on it | [390](v4/home-populated-cs-light-390.png) |
| Signed-out | Language switch floats bottom-right, far from the form | [login 1920](v4/login-cs-light-1920.png) |
| Dark theme | Light-cyan primary buttons with dark text are the brightest element on screen and outweigh the content | [768 dark](v4/home-populated-cs-dark-768.png) |

### 2.4 What v4 gets right (keep)

Zero overflow and zero sub-44 px targets across the full matrix; Inter is loaded and consistent (the harness asserts it
before every capture); light/dark theming boots before first paint; bounded fetching keeps 1000+ items at 1298 DOM elements; Czech strings fit without clipping; the
phone bottom bar works; row Done is one tap.

## 3. Directions

Both directions are functional prototypes on the real React app, real API and real data (`proto/v5-direction-a`,
`proto/v5-direction-b`), captured with the same harness and matrix as v4. Neither is merged by this Issue; the chosen
direction is implemented through the later V5 design Issues.

### 3.1 Direction B: structured workspace

Intent and full self-critique: `docs/design/v5-directions/b/README.md` on `proto/v5-direction-b` (3174c0d).
Validation on that commit: lint, typecheck, vitest 1033/1033, build and the required E2E smoke all pass.

Measured against v4 (488 viewport captures each):

| Check | v4 | B |
| --- | --- | --- |
| Overflow | 0 | 0 |
| Targets under 44 px | 0 real | 0 in this run. The earlier run flagged `admin-cs-dark-320` (switcher 20 x 44 px plus mid-reflow bottom-bar readings); the switcher truncation to "N…" is still visible in that screenshot, so it stays a finding |
| Header full width | no at 1920 signed in, no at 1440+ signed out | yes signed in at every width (57 px); still no at 1440+ signed out |
| Home h1 | 34/36/40 px with period | 24 px "Overview" / "Přehled" |
| Home first row y (390 / 1440) | 409 / 327 | 254 / 262 |
| Rows above fold (390 / 768 / 1440) | 3 / 6–7 / 7 | 5 / 8 / 8 |
| All items first row y (390 / 1440) | 456 / 480 | 444 / 473 |
| Main left inset at 1920 | 464 (frame) | 224 (sidebar only) |
| Primary action right gap | 0 | 0 |
| Home DOM, 10 / 1000+ items | 398 / 1298 | 401 / 1301 |

Observed in screenshots: the eyebrow, household-name hero, tagline and top-bar slogan are gone on Home
([1920](b/home-populated-en-light-1920.png), [cs 390](b/home-populated-cs-light-390.png)); the household switcher sits in
the header; subject pills became plain text; groups use small uppercase headings. Unchanged from v4: item detail
([1440](b/item-detail-en-light-1440.png), [cs 390](b/item-detail-cs-dark-390.png)), add/edit forms
([edit](b/edit-item-en-light-1440.png), [add](b/add-item-cs-dark-390.png)), admin ([1440](b/admin-en-dark-1440.png)),
people, members and account, all of which still carry eyebrows and subtitles. The header search sits left-centre with
account actions next to it rather than at the right edge, leaving the right two thirds of the 1920 bar empty. At 320 px
Czech the switcher truncates to "N…" ([admin 320](b/admin-cs-dark-320.png)). Sign-in remains framed
([login 1920](b/login-cs-light-1920.png)). The pill row at the top of Home is kept, so status is still stated three
times. Rows keep v4's card frame, a coloured icon tile per row and an outlined Done button per row; at 1920 the subject,
date and Done columns sit 800+ px away from the title, so the eye travels the full width per row. Detail keeps the
eyebrow, the card-in-card facts and the five equal-weight buttons plus a separate Archive
([detail 1440](b/item-detail-en-light-1440.png)).

### 3.2 Direction A: calm list

Intent and full self-critique (deviations D1–D4, weaknesses W1–W12): `docs/design/v5-directions/a/README.md` on
`proto/v5-direction-a` (0b138d2). Validation on that commit: lint, typecheck, vitest 1029/1029 and build pass; the
required E2E smoke result is in section 3.3.

Measured against v4 and B (488 viewport captures each):

| Check | v4 | B | A |
| --- | --- | --- | --- |
| Overflow | 0 | 0 | 0 |
| Captures with targets under 44 px | 0 real | 0 | **368 of 488** (every signed-in state; 0 on signed-out states) |
| Header full width, signed in | no at 1920 | yes (57 px) | yes (56 px phone/tablet, 64 px desktop) |
| Header full width, signed out | no at 1440+ | no at 1440+ | no at 1440+ (1152 px frame) |
| Home h1 | 34/36/40 px | 24 px | 26 px |
| Home first row y (390 / 1440) | 409 / 327 | 254 / 262 | 241 / 265 |
| Rows above fold (390 / 768 / 1440) | 3 / 6–7 / 7 | 5 / 8 / 8 | 6 / 8 / 7 |
| Rows above fold, 1000+ items (390 / 1440) | 5 / 10 | 8 / 11 | 8 / 9 |
| All items first row y (390 / 1440) | 456 / 480 | 444 / 473 | 410 / 419 |
| Main left inset at 1920 | 464 | 224 | 232 |
| Content measure | frame | full width | 760 px column |
| Home DOM, 10 / 1000+ items | 398 / 1298 | 401 / 1301 | 377 / 1216 |
| Detail DOM | 121 | 130 | 159 |

Small-target offenders in A (each repeats on every signed-in capture where it is shown):

| Control | Measured box | Where |
| --- | --- | --- |
| Household menu button in the header | 126 x 36 px | every signed-in capture, all widths |
| Sidebar links (Home … Admin center) | 207 x 40 px | every signed-in capture at 1024+ |
| Home "Add item" | 121 x 40 px | Home at every width |
| Home "Add an item…" entry row | full column x 40 px | Home at every width |
| Row Done circle | 28 x 28 px | every Home row. A's README (W11) says a pseudo-element extends the hit area to about 44 px; the harness measures the element box, so the larger hit area is unverified |
| Row title link | column x 24 px | every Home row (the whole row is not the link) |

Observed in screenshots:

- **Giant hero, tagline:** gone. Home is a 26 px "Overview"/"Přehled" with Add on one line, then the list
  ([cs 390](a/home-populated-cs-light-390.png), [cs 320](a/home-populated-cs-light-320.png),
  [1920](a/home-populated-en-light-1920.png)). The top-bar slogan is gone. The server-error state is a heading plus one
  retry panel instead of a hero ([error](a/server-error-cs-dark-390.png)).
- **Framed header:** fixed signed in at every width, household name in the header; signed-out screens are still framed
  at 1440+ and still carry the eyebrow and tagline ([login 1920](a/login-cs-light-1920.png)).
- **Edge insets:** 16 px phone, 24 px tablet, 32 px next to the sidebar. At 1440 and 1920 the 760 px column leaves the
  right part of the screen empty and puts "Add item" in the middle of the screen instead of at the edge of the
  workspace ([1920](a/home-populated-en-light-1920.png)); that is a deliberate reading measure, but on a 1920 display it
  reads as unused space.
- **Hierarchy:** status is said once per group (heading + count); the pill row and per-row pills are gone; each row is
  title over one metadata line (subject, since/date, owner, recurrence), wrapping honestly at 320 px. Detail is a page
  with a back link, a labelled definition list, one filled Done, quiet Edit/Start/Waiting/Pause and a small Archive at
  the end of the band ([1440](a/item-detail-en-light-1440.png), [cs 390](a/item-detail-cs-dark-390.png)). On item
  detail opened from Home the sidebar still highlights "All items" (v4 defect kept; the back link correctly says
  "Back to overview").
- **Generic styling:** no cards around lists, no icon tiles, no coloured Done buttons per row; one warm paper palette.
  Members, Admin and Account are flat sections with dividers instead of card-in-card
  ([members](a/members-cs-light-1440.png), [admin](a/admin-en-dark-1440.png),
  [account](a/account-cs-light-1440.png)), but every one of them still has a one-sentence subtitle under the h1.
  People keeps a "See items" + "Edit" pair under every entry at phone width ([people 390](a/people-en-light-390.png)).
- **Forms:** the add/edit sheet is restyled but structurally v4 (D1, no inline create); the Czech date field still
  shows the browser's `mm/dd/yyyy` placeholder; "Add notes (optional)" is still a bare text link
  ([edit 1440](a/edit-item-en-light-1440.png), [add 390](a/add-item-cs-dark-390.png)).
- **Duplicate entry points:** Home shows both an "Add item" button and an "Add an item…" row directly below it, and
  both open the same sheet. Two controls for one action, introduced by the unfinished inline create.
- **Phone navigation:** 4 tabs (Home, Items, People, More); Archive, Members, Account and Admin are in the More sheet,
  so Archive is reachable on phones (v4 and B have no phone Archive).

### 3.3 Required E2E smoke on each prototype

| Branch | Result |
| --- | --- |
| B `proto/v5-direction-b` 3174c0d | exit 0, "Native production-binary E2E passed." |
| A `proto/v5-direction-a` 0b138d2 | exit 1: 63 passed, 1 failed, 5 did not run. The failure is `n23` (real browser zoom at 200 %): the detail `Pause` button has viewport ratio 0.9989 against `toBeInViewport({ ratio: 1 })`, i.e. it ends about 0.5 px below the 406 px layout viewport. Same result as A's own README W1; the assertion was not changed |

Both runs were made on 2026-11-13 against the same test cluster, one after the other under the shared E2E lock.

## 4. Comparative critique

Scored against the brief's three criteria. "Better" means a measured or directly visible difference in the captures
above, not taste.

| Concern | v4 | B | A |
| --- | --- | --- | --- |
| Usefulness: rows visible on a phone | 3 | 5 | 6 |
| Usefulness: steps to finish an item from Home | 1 tap (row Done) | 1 tap | 1 tap |
| Usefulness: Archive on phone | no | no | yes (More) |
| Usefulness: global search | All items only | header search at 640+ | All items only |
| Usefulness: household in header | no | yes, truncates to "N…" at 320 cs | yes |
| Accessible density: targets ≥ 44 px | yes | yes | **no** (368 captures) |
| Accessible density: 200 % zoom (n23) | pass | pass | **fail** (0.5 px) |
| Accessible density: smallest text | 12 px | 12 px | 12 px |
| Polish: hero/tagline/slogan removed | no | Home only | Home and app shell |
| Polish: framed header | signed in and out | signed out only | signed out only |
| Polish: detail hierarchy (one primary) | no | no (unchanged) | yes |
| Polish: card-in-card admin/members/account | yes | yes (unchanged) | no |
| Polish: icon tiles and pills on every row | yes | tiles kept, pills removed | both removed |
| Scope covered by the prototype | — | shell + Home | shell, Home, detail, lists, people, members, account, admin, phone nav |

**B's strengths:** it keeps every v4 accessibility guarantee intact (no target regressions), its header search is a
real usability gain on desktop, and it is the smaller, safer diff. **B's weakness:** it is a shell-and-Home pass. The
generic dashboard grammar the brief rejects (cards, icon tiles, per-row outlined Done buttons, pill row, eyebrows,
card-in-card admin and detail) survives everywhere except the top of Home, and the wide 1920 rows are harder to scan
than v4's.

**A's strengths:** it answers every composition finding in section 2 on the signed-in app, not only on Home; it shows
the most rows above the fold on phones, the lowest DOM per item, the clearest detail hierarchy and the only phone path
to Archive. **A's weaknesses:** it regresses the one thing v4 measured perfectly, target size (36–40 px controls and a
28 px Done circle across 368 captures); its required E2E smoke fails `n23` at 200 % zoom; inline create is missing and leaves two add
controls on Home; signed-out screens, subtitles and the Czech date placeholder are untouched; the 760 px column
under-uses 1920 px displays.

## 5. Chosen direction

**Direction A (calm list) as the visual and structural base, with these mandatory corrections carried into the later
V5 design Issues before any screen family ships:**

1. Every interactive target ≥ 44 x 44 px measured by the same harness (header household menu, sidebar links, Add,
   Done hit area as the measured box, row link covering the row). Zero small-target captures is an exit criterion.
2. The 200 % zoom journey (`n23`) passes with its assertion unchanged; the required E2E smoke is green.
3. One add entry point: either real inline create at the top of the list or only the "Add item" button, never both.
4. Full-width header on signed-out screens too; eyebrows and decorative subtitles removed there and under every h1.
5. Sidebar highlight follows the route of origin; localized date entry in Czech.
6. Take B's header search into the A shell (desktop/tablet), and B's evidence that the switcher must not truncate
   to one letter at 320 px Czech.
7. Reconsider the 760 px measure at 1440+ (for example aligning the primary action to the workspace edge) with a
   captured comparison, not by assertion.

Why A over B: the brief's core complaint is generic, dashboard-like composition across the app. B fixes it on one
screen and leaves the rest of v4 as is, so choosing B would mean designing the remaining screens later without a tested
direction. A demonstrates a coherent direction on every signed-in screen family in the real app, and its main failures
(target size, zoom) are measured, specific and fixable inside that direction without changing its character. B's
failures are the absence of a direction on most screens. This is an agent decision from the evidence above.

## 6. Approval status

No human has reviewed, approved, user-tested or signed off on either direction or on the choice in section 5. Every
statement here comes from automated production-build captures, the measurements listed, the prototypes' own test
runs, and an agent's reading of the screenshots. The owner may override the choice; until a recorded human action
exists, nothing in this document is human-approved. Neither prototype branch is merged.
