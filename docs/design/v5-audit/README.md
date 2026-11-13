# V5 design audit and direction choice

status=V5-05_evidence (Issue #449)
source_commit=5bd7647 (application code identical to published v4.0.0 UI; the baseline run below is this branch at 7aeab28,
whose frontend and backend are unchanged from 5bd7647)
approval=none. No human has reviewed, approved or user-tested anything in this document. The chosen direction is an
agent decision based on the evidence below and stays open to the owner's override.
governing=`.autonomous/UX.md` ("Production screenshot workflow", "Visual direction"; it carries the former V5 design
brief). Historical V4 reference PNGs are not evidence here.

## 1. Method

Capture harness: `frontend/e2e/design-capture.spec.ts` with `frontend/e2e/design/{capture,metrics,seed,hold}.ts`, opt-in
via `TENDO_DESIGN_CAPTURE_DIR=<dir> bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` (not part of required CI). It
runs the real production build against native PostgreSQL with the fixed business date 2026-05-14, Chromium, reduced
motion.

Matrix: 35 states x 6 widths (320, 390, 768, 1024, 1440, 1920) x 2 languages (cs, en) x 2 explicit themes (light,
dark) = 840 viewport captures, plus the 1000+ item Home at 390/1440 (8 viewport captures): 848 viewport captures. On
top of that, 32 full-page images: Home and item detail at 390/1440, and the invitation link and revoke confirmation at
320/390, whose point is below the fold on phones. 880 entries per run; the harness fails unless it has exactly 880 with
one PNG each. Fold height is 844 below 768, 1024 at 768 and 900 above. Each capture is paired with measurements in
`metrics.json`:

| Metric | Meaning |
| --- | --- |
| `horizontalOverflow` | any element past the document width |
| `smallestVisibleTextPx` | smallest computed font size among visible text |
| `smallTargets` | interactive targets under 44 x 44 px |
| `banner` | top-bar box and whether it spans the full viewport |
| `mainInset` | `main` landmark left/right offset from the viewport |
| `h1` | page title size and position |
| `firstRowY`, `rowsFullyAboveFold` | first item row position and rows visible without scrolling |
| `primaryAction` | box of the first visible primary button; the audit reports its gap to the viewport's right edge (the recorded `containerRightGap` to its parent is 0 almost by construction and is not used as evidence) |
| `domElements` | element count of the document |

Full-page entries carry the viewport measurements of the same screen and are marked `fullPage: true`; every number in
this document comes from the 848 viewport captures.

States (35):

- Signed out: onboarding, login, login-error, login-oidc (OIDC provider offered), invite-accept, reset-password (a real,
  unredeemed admin reset link), reset-password-invalid, signed-out-unavailable (server unreachable), not-found-signed-out.
- Signed in as the owner: home-empty, home-loading (items request held open), home-populated (10-item reference
  household), home-large (1000+ items, 390/1440 only), server-error, all-items, all-items-no-match, all-items-archived,
  item-detail, item-detail-history, item-detail-archived, add-item, add-item-repeat, edit-item, edit-conflict (stale
  save), people, people-archived, members, members-invite-link (link shown once), members-revoke-confirm, account,
  admin, admin-reset-link (one-time reset link dialog), not-found.
- Signed in as a non-admin member: members-member, admin-member.
- Signed in without a household: no-household.

Runs (scratch evidence, not committed in full; 44–50 MiB each). All three were made back to back on 2026-11-13 with
the identical harness, under one lock against the same native PostgreSQL 18.6 test cluster:

| Run | Build | Result | `metrics.json` SHA-256 |
| --- | --- | --- | --- |
| v4 baseline | `docs/v5-05-design-audit` (v4 application code, harness as committed here) | exit 0, 880 entries (848 viewport + 32 full-page) | `af9d71e15cf3d5ba1fe7dfb7910a23427ae6ac98754db4ea3dfd4d93c0666ac3` |
| Direction B | `proto/v5-direction-b` c0583c5 | exit 0, 880 entries | `f3bad30933d8765e06f7a18c4e33e8c15f779883f5c44ec12daf062b3a3f4ca7` |
| Direction A | `proto/v5-direction-a` 0b138d2 | exit 0, 880 entries | `365a11d01ac873c11aff901cb3423227db3cd02f75420651c1dc996a7adb01de` |

Harness on the prototypes: the prototype branches do not contain the harness. Each prototype worktree was captured
with uncommitted copies of the eight harness files (`design-capture.spec.ts`, the four `design/` modules,
`playwright.config.ts`, `e2e-native-runner.py`, `e2e-smoke.sh`) that are byte-identical to the versions committed with
this document (checked with `cmp`), so all three runs used the same capture code.

Measurement notes:

- Measurements are taken after the Inter font check and repeated frame by frame until two consecutive readings are
  identical (at most 10 frames, otherwise the run fails). Earlier runs measured right after the viewport resize and
  occasionally caught the phone bottom bar mid-reflow; with settled measurement v4 and B record no small target at all.
- The per-capture count of small targets is complete; the names are capped at 8 per capture by the shared audit
  measurement (`frontend/e2e/audit/metrics.ts`). In v4 and B no capture has an offender. In A 92 of 848 viewport
  captures (home-populated, add-item and add-item-repeat 24 each, home-empty 12, home-large 8) have more than 8, so
  A's offender table lists named controls only and its capture counts are lower bounds.
- `no-household` is the one partly simulated state. This runner starts without an OIDC provider, and an account without
  a household can only be created through SSO, so the capture uses a real signed-in session whose `/api/v1/session`
  reply is answered without a default household (the shape such an account receives). The screen itself is the app's
  real no-household screen.

### 1.1 Coverage and remaining exclusions

The matrix covers every route and every distinct screen state the app can reach with the runner's setup. These
variants are not in it; each is either the same screen as a captured state with different text, or an interaction
state rather than a screen:

| Not in the matrix | Why it is not a separate audit state | Other evidence (v4 only) |
| --- | --- | --- |
| Item with an invalid or foreign id | renders the captured `not-found` screen | journey `n10` |
| OIDC conflict, provider error and sign-in outcome | the captured `login` / `login-error` layout with a different alert | UX audit `auth-login-oidc-*` at 390 |
| Session ended, rate limited | the captured `login-error` layout with a different alert | UX audit `auth-login-session-ended`, `auth-login-rate-limited` |
| Boot loading (before the session probe answers) | transient; replaced as soon as the probe answers | UX audit `auth-boot-*` |
| Onboarding validation and server error | the captured `onboarding` form with an inline alert | visual `onboarding-error-*` |
| People, Members and Admin loading | short-lived; People and Members show one muted "Loading…" line, Admin a skeleton list. Only Home loading is captured | UX audit `audit-people-loading`, `audit-members-loading` |
| Admin promote/demote confirmation | same dialog component as the captured `admin-reset-link` | journey `n7` |
| Account appearance focus states | focus rings, not layout | UX audit `auth-account-appearance-focus-*` |
| Home with exactly 1 or about 100 items | same list as 10 and 1000+ items, which bound it | — |
| System theme (no stored preference) | resolves to the captured light or dark theme | `theme.spec.ts` asserts first paint for system/light/dark |

Curated v4 screenshots are in [`v4/`](v4/); curated direction screenshots are in [`a/`](a/) and [`b/`](b/). All three
folders contain the same 28 file names (`<state>-<lang>-<theme>-<width>.png`, plus `admin-cs-dark-320` and
`item-detail-en-light-1440` for the directions), so any state can be compared side by side by opening the same name
in each folder. All curated PNGs are byte copies from the three runs listed above.

## 2. v4 audit (production build)

### 2.1 Measured, all 848 viewport captures

| Check | Result |
| --- | --- |
| Horizontal overflow | 0 captures |
| Targets under 44 px | 0 captures |
| Smallest visible text | 12 px (meta and helper text) |
| Top bar full width | no at 1920 on every signed-in route (1440 px frame at x=240); no at 1440 and 1920 on every signed-out route (1152 px frame at x=144/384); no from 1024 on the no-household screen (768 px frame) |
| Top bar height | signed in: 61 px at 320–768, 56 px from 1024; signed out: 56 px |
| Home h1 | 34 px phone, 36 px tablet, 40 px desktop, with decorative coloured period |
| Home first item row | y=409 phone (446 when the 320 px count pill wraps), 396 tablet, 327 desktop |
| Home rows above the fold | 3 at 320/390, 6–7 at 768, 6 at 1024, 7 at 1440/1920 (10-item household); 5 and 10 with 1000+ items |
| All items first row | y=456 phone, 480 desktop; 3 rows above the fold at 390 |
| Home Add button gap to the viewport's right edge | 20 px at 390, 40 px at 1440, 280 px at 1920 (inside the 1440 px frame) |
| Home DOM | 398 elements for 10 items, 1291 for 1000+ items (bounded pagination holds) |

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

**Edge insets.** Insets are mostly consistent (20 px phone, 32 px tablet, 40 px desktop). In the screenshots the
primary actions sit flush with the right edge of the content column, and the phone Add button is a full-width slab. The
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
| Phone navigation | Bottom bar has Home, Items, People, Members, Account; Admin center has no phone tab, and archived items have no navigation entry of their own (they are reached through Items → Show: Archived) | [390](v4/home-populated-cs-light-390.png) |
| Signed-out | Language switch floats bottom-right, far from the form | [login 1920](v4/login-cs-light-1920.png) |
| Dark theme | Light-cyan primary buttons with dark text are the brightest element on screen and outweigh the content | [768 dark](v4/home-populated-cs-dark-768.png) |

### 2.4 Findings on the remaining states

| Route/state | Finding | Evidence |
| --- | --- | --- |
| No household | The signed-in shell collapses to a 768 px centred frame from 1024 up, header included, with the slogan, an eyebrow, a 30 px h1, a two-line subtitle and two cards; its navigation (Home, Account) is a small pill row at the bottom of the frame rather than the bottom bar or sidebar | [1440](v4/no-household-en-light-1440.png) |
| Signed-out (login with OIDC, reset password, unavailable, 404) | Same 1152 px frame and floating language switch as login; the OIDC button sits in its own band under an "or" divider inside the card | [OIDC 1440](v4/login-oidc-en-dark-1440.png) |
| Home loading | The full hero (eyebrow, household h1, tagline) renders before three grey skeleton bars, so the slowest moment shows the most decoration | [390](v4/home-loading-en-light-390.png) |
| Edit conflict | The conflict notice is a clear amber box at the top of the dialog that names the saved title; the dialog otherwise repeats the edit-form findings (Cancel twice, `mm/dd/yyyy`) | [1440](v4/edit-conflict-en-light-1440.png) |
| Archived item detail | State badge and a sentence restate "archived" twice; Restore is an outlined button, history card below | [cs 390](v4/item-detail-archived-cs-dark-390.png) |
| Archived people | The archive is a second heading with its own subtitle under the page eyebrow, h1 and subtitle: four lines of framing above one row | [cs 1024](v4/people-archived-cs-light-1024.png) |
| Member view of Members | Card list with role pills and avatar circles; the "Need to invite someone?" panel is a tinted card that only says to ask an admin | [cs 390](v4/members-member-cs-light-390.png) |
| Member view of Admin center | A full page with eyebrow, h1 and subtitle whose only content is "only administrators" | [1440](v4/admin-member-en-dark-1440.png) |

### 2.5 What v4 gets right (keep)

Zero overflow and zero sub-44 px targets across the full matrix; Inter is loaded and consistent (the harness asserts it
before every capture); light/dark theming boots before first paint; bounded fetching keeps 1000+ items at 1291 DOM elements; Czech strings fit without clipping; the
phone bottom bar works; row Done is one tap.

## 3. Directions

Both directions are functional prototypes on the real React app, real API and real data (`proto/v5-direction-a`,
`proto/v5-direction-b`), captured with the same harness and matrix as v4. Neither is merged by this Issue; the chosen
direction is implemented through the later V5 design Issues.

### 3.1 Direction B: structured workspace

Intent and full self-critique: `docs/design/v5-directions/b/README.md` on `proto/v5-direction-b` (c0583c5). B was
built in two passes: shell and Home (3174c0d), then item detail, create/edit and admin (c0583c5) after an independent
review found that the first pass did not cover the screen families the brief requires. Validation on c0583c5: lint,
typecheck, vitest 1033/1033 and build pass; the required E2E smoke result is in section 3.3.

Measured against v4 (848 viewport captures each):

| Check | v4 | B |
| --- | --- | --- |
| Overflow | 0 | 0 |
| Targets under 44 px | 0 | 0. The household switcher truncates to "N…" at 320 px Czech on every signed-in screen ([admin 320](b/admin-cs-dark-320.png)): a visual finding, not a target-size one |
| Header full width | no at 1920 signed in, no at 1440+ signed out | yes signed in at every width (57 px); still no at 1440+ signed out and from 1024 on no-household |
| Home h1 | 34/36/40 px with period | 24 px "Overview" / "Přehled" |
| Detail, edit and admin h1 | 28–36 px detail and edit, 30 px admin | 24 px |
| Home first row y (390 / 1440) | 409 / 327 | 254 / 262 |
| Rows above fold (390 / 768 / 1440) | 3 / 6–7 / 7 | 5 / 8 / 8 |
| All items first row y (390 / 1440) | 456 / 480 | 444 / 473 |
| Main left inset at 1920 | 464 (frame) | 224 (sidebar only) |
| Home Add gap to viewport right edge (390 / 1440 / 1920) | 20 / 40 / 280 | 16 / 40 / 40 |
| Home DOM, 10 / 1000+ items | 398 / 1291 | 401 / 1294 |
| Detail / edit / admin DOM | 121 / 143 / 132 | 133 / 152 / 141 |

Observed in screenshots:

- **Giant hero, tagline:** gone on Home ([1920](b/home-populated-en-light-1920.png),
  [cs 390](b/home-populated-cs-light-390.png)); the top-bar slogan is gone. Signed-out screens keep the v4 eyebrow and
  tagline and remain framed ([login 1920](b/login-cs-light-1920.png)).
- **Framed header:** fixed signed in; the household switcher and a header search sit in the bar. The search and
  account actions sit left of centre, leaving about half of the 1920 bar empty.
- **Hierarchy on Home:** subject pills became plain text and groups use small uppercase headings, but the pill row at
  the top is kept, so status is still stated three times. Rows keep a card frame, a coloured icon tile and an outlined
  Done button; at 1920 the subject, date and Done columns sit 800+ px from the title.
- **Detail:** 24 px title with the subject as an uppercase eyebrow and an uppercase status label on the right; facts,
  actions and history are three separate bordered panels. Done is the one filled action (with a check icon), but Edit
  has the same size as Done and Start/Waiting/Pause sit directly beside them; Archive is in the same panel below a rule
  ([1440](b/item-detail-en-light-1440.png), [cs 390](b/item-detail-cs-dark-390.png)). The sidebar still highlights
  "All items" for an item opened from Home.
- **Create/edit:** spacing and type refined, structure unchanged from v4: the same stacked dialog with Cancel twice
  (header and footer), a divider between fields, the Czech `mm/dd/yyyy` date placeholder and the bare "Add notes
  (optional)" link ([edit 1440](b/edit-item-en-light-1440.png), [add 390](b/add-item-cs-dark-390.png)).
- **Admin:** avatar circles and role pills are gone; each account is a row (login, uppercase role · sign-in method,
  actions at the row end) inside one panel. The "YOUR HOUSEHOLD" eyebrow and the subtitle remain. On phones the action
  buttons stack under each account, two per member ([1440](b/admin-en-dark-1440.png), [cs 320](b/admin-cs-dark-320.png)).
- **Untouched:** People, Members and Account keep v4's eyebrows, subtitles and card-in-card layout
  ([members](b/members-cs-light-1440.png), [account](b/account-cs-light-1440.png), [people 390](b/people-en-light-390.png)).
- **Remaining states:** Home loading drops the hero and shows "Overview" over the skeleton
  ([390](b/home-loading-en-light-390.png)). No-household keeps v4's 768 px framed shell, eyebrow, subtitle and two cards
  ([1440](b/no-household-en-light-1440.png)); signed-out OIDC, reset-password and error screens are v4. Archived detail,
  edit conflict and the member views follow B's detail, form and v4 members styling respectively
  ([archived cs 390](b/item-detail-archived-cs-dark-390.png), [conflict](b/edit-conflict-en-light-1440.png),
  [member cs 390](b/members-member-cs-light-390.png)).

### 3.2 Direction A: calm list

Intent and full self-critique (deviations D1–D4, weaknesses W1–W12): `docs/design/v5-directions/a/README.md` on
`proto/v5-direction-a` (0b138d2). Validation on that commit: lint, typecheck, vitest 1029/1029 and build pass; the
required E2E smoke result is in section 3.3.

Measured against v4 and B (848 viewport captures each):

| Check | v4 | B | A |
| --- | --- | --- | --- |
| Overflow | 0 | 0 | 0 |
| Captures with targets under 44 px | 0 | 0 | **608 of 848** (every state with the household switcher; 0 on signed-out states and no-household) |
| Header full width, signed in | no at 1920 | yes (57 px) | yes (56 px phone/tablet, 64 px desktop) |
| Header full width, signed out | no at 1440+ | no at 1440+ | no at 1440+ (1152 px frame) |
| Header full width, no household | no from 1024 (768 px frame) | same | same |
| Home h1 | 34/36/40 px | 24 px | 26 px |
| Home first row y (390 / 1440) | 409 / 327 | 254 / 262 | 241 / 265 |
| Rows above fold (390 / 768 / 1440) | 3 / 6–7 / 7 | 5 / 8 / 8 | 6 / 8 / 7 |
| Rows above fold, 1000+ items (390 / 1440) | 5 / 10 | 8 / 11 | 8 / 9 |
| All items first row y (390 / 1440) | 456 / 480 | 444 / 473 | 410 / 419 |
| Main left inset at 1920 | 464 | 224 | 232 |
| Home Add gap to viewport right edge (390 / 1440 / 1920) | 20 / 40 / 280 | 16 / 40 / 40 | 16 / 408 / 888 |
| Content measure | frame | full width | 760 px column |
| Home DOM, 10 / 1000+ items | 398 / 1291 | 401 / 1294 | 377 / 1209 |
| Detail DOM | 121 | 133 | 159 |

Small-target offenders in A (each repeats on every signed-in capture where it is shown):

| Control | Measured box | Where |
| --- | --- | --- |
| Household menu button in the header | 126 x 36 px | all 608 captures that show it, all widths |
| Sidebar links (Home … Admin center) | 207 x 40 px | 264 named captures at 1024+ (the other signed-in captures at those widths hit the 8-name cap first) |
| Row Done circle | 28 x 28 px | 80 captures (home-populated, add-item and add-item-repeat 24 each with Home behind the sheet, home-large 8; 288 named instances), every width. A's README (W11) says a pseudo-element extends the hit area to about 44 px; the harness measures the element box, so the larger hit area is unverified |
| Row title link | 252–724 x 24 px | 80 captures (208 named instances), same states as Done; the whole row is not the link |
| Home "Add item" | 121 x 40 px (Czech "Přidat položku" 164 x 40 px) | 56 captures (home-empty, home-populated, home-large) |
| Home "Add an item…" entry row | 288–760 x 40 px | 32 captures (home-populated, home-large) |

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
- **Remaining states:** Home loading is "Overview" over the skeleton ([390](a/home-loading-en-light-390.png)). Archived
  detail keeps the back link, an "Archived" label, one notice and a quiet Restore at the end of the band
  ([cs 390](a/item-detail-archived-cs-dark-390.png)). The member view of Members is a flat list with role as plain text
  ([cs 390](a/members-member-cs-light-390.png)); the member view of Admin center drops the eyebrow but keeps the
  subtitle above a single notice ([1440](a/admin-member-en-dark-1440.png)). No-household is still v4's 768 px framed
  shell with eyebrow, subtitle and cards ([1440](a/no-household-en-light-1440.png)); OIDC sign-in and the other
  signed-out screens keep v4's frame, eyebrow and tagline, with A's palette and without the top-bar slogan
  ([OIDC 1440](a/login-oidc-en-dark-1440.png)).
- **Duplicate entry points:** Home shows both an "Add item" button and an "Add an item…" row directly below it, and
  both open the same sheet. Two controls for one action, introduced by the unfinished inline create.
- **Phone navigation:** 4 tabs (Home, Items, People, More); Archive, Members, Account and Admin are in the More sheet,
  so archived items get their own phone entry. In v4 and B archived items are reachable on phones only through
  Items → Show: Archived.

### 3.3 Required E2E smoke on each prototype

| Branch | Result |
| --- | --- |
| B `proto/v5-direction-b` 3174c0d (first pass) | exit 0, "Native production-binary E2E passed." |
| B `proto/v5-direction-b` c0583c5 (final) | three runs: the Chromium suite passed 69/69 each time; the Firefox cross-browser test failed twice with `frame.evaluate: Page crashed` inside the 390 px axe scan of the shared routes, then the third run passed every suite (exit 0, "Native production-binary E2E passed."). The crash is not in a screen B changed in that pass; whether v4 shows the same intermittent crash was not established |
| A `proto/v5-direction-a` 0b138d2 | exit 1: 63 passed, 1 failed, 5 did not run. The failure is `n23` (real browser zoom at 200 %): the detail `Pause` button has viewport ratio 0.9989 against `toBeInViewport({ ratio: 1 })`, i.e. it ends about 0.5 px below the 406 px layout viewport. Same result as A's own README W1; the assertion was not changed |

All runs were made on 2026-11-13 against the same test cluster, one at a time under the shared E2E lock.

## 4. Comparative critique

Scored against the brief's three criteria. "Better" means a measured or directly visible difference in the captures
above, not taste.

| Concern | v4 | B | A |
| --- | --- | --- | --- |
| Usefulness: rows visible on a phone | 3 | 5 | 6 |
| Usefulness: steps to finish an item from Home | 1 tap (row Done) | 1 tap | 1 tap |
| Usefulness: dedicated Archive entry on phone | no (Items → Show: Archived) | no (same) | yes (More → Archive) |
| Usefulness: global search | All items only | header search at 640+ | All items only |
| Usefulness: household in header | no | yes, truncates to "N…" at 320 cs | yes |
| Accessible density: targets ≥ 44 px | yes | yes | **no** (608 captures) |
| Accessible density: 200 % zoom (n23) | pass (`required` run 43324802110 on main f182b88) | pass | **fail** (0.5 px) |
| Accessible density: smallest text | 12 px | 12 px | 12 px |
| Polish: hero/tagline/slogan removed | no | Home and shell; eyebrows and subtitles remain elsewhere | Home and shell; subtitles remain |
| Polish: framed header | signed in and out | signed out only | signed out only |
| Polish: detail hierarchy (one primary) | no | partly: one filled Done, but Edit equal in size and three bordered panels | yes |
| Polish: card-in-card admin/members/account | yes | admin flattened to rows in one panel; members/account unchanged | no |
| Polish: icon tiles and pills on every row | yes | tiles kept, pills removed | both removed |
| Polish: create/edit form | v4 | v4 structure, refined spacing | v4 structure, restyled sheet |
| Scope covered by the prototype | — | shell, Home, detail, create/edit, admin, phone nav | shell, Home, detail, create/edit, lists, people, members, account, admin, phone nav |

**Maturity benchmark (brief step 2).** The former brief asked to benchmark the information-architecture discipline
of mature task apps (Things 3) and dense but clear tools (Linear), principles only. No such app was opened or measured
for this audit, so no claim is made about how either direction compares to them. The principles used as the yardstick
in this section are the ones `.autonomous/UX.md` states directly: one obvious primary action per context; compact
title with the primary action on the same line and no promotional heading or tagline; no repeated status words or
pills on every row; no decorative cards or generic AI dashboard visuals; 44 px touch targets; no phone-width column
in a wide empty canvas. Each row of the table above applies one of these to a measured or visible difference.

**B's strengths:** it keeps every v4 accessibility guarantee intact (no target regressions), its header search is a
real usability gain on desktop, it is the smaller, safer diff, and its admin rows read well. **B's weakness:** it
restyles v4 rather than restructuring it. The grammar `.autonomous/UX.md` rejects survives on every screen B covers:
bordered panels around facts, actions and history; icon tiles, per-row outlined Done buttons and the pill row on Home;
uppercase eyebrows and decorative subtitles on detail and admin; and People, Members and Account are untouched. The
wide 1920 rows are harder to scan than v4's, and the Czech switcher truncates to one letter at 320 px.

**A's strengths:** it answers every composition finding in section 2 on the signed-in app, not only on Home; it shows
the most rows above the fold on phones, the lowest DOM per item, the clearest detail hierarchy and the only phone path
to a dedicated Archive view. **A's weaknesses:** it regresses the one thing v4 measured perfectly, target size (36–40 px controls and a
28 px Done circle; 608 captures); its required E2E smoke fails `n23` at 200 % zoom; inline create is missing and leaves two add
controls on Home; signed-out screens, subtitles and the Czech date placeholder are untouched; the 760 px column
under-uses 1920 px displays.

## 5. Chosen direction

**Direction A (calm list) as the visual and structural base, with these mandatory corrections carried into the later
V5 design Issues before any screen family ships:**

1. Every interactive target ≥ 44 x 44 px measured by the same harness (header household menu, sidebar links, Add,
   Done hit area as the measured box, row link covering the row). Zero small-target captures is an exit criterion.
2. The 200 % zoom journey (`n23`) passes with its assertion unchanged; the required E2E smoke is green.
3. One add entry point: either real inline create at the top of the list or only the "Add item" button, never both.
4. Full-width header on signed-out screens and on the no-household screen too; eyebrows and decorative subtitles
   removed there and under every h1.
5. Sidebar highlight follows the route of origin; localized date entry in Czech.
6. Take B's header search into the A shell (desktop/tablet), and B's evidence that the switcher must not truncate
   to one letter at 320 px Czech.
7. Reconsider the 760 px measure at 1440+ (A's Add button is 408 px from the right edge at 1440 and 888 px at 1920),
   for example by aligning the primary action to the workspace edge, with a captured comparison, not by assertion.

Why A over B: the brief's core complaint is generic, dashboard-like composition across the app. Both directions now
cover Home, create/edit, detail, admin and phone, but B keeps the composition the complaint is about (panels, tiles,
pills, eyebrows, subtitles) and changes its type and spacing, while A removes it on every signed-in screen family and
shows the clearer detail hierarchy and the denser phone list. A's main failures (target size, 200 % zoom, two add
entry points) are measured, specific and fixable inside that direction without changing its character; B's main
weakness is the direction itself. B is the better accessibility baseline today, which is why correction 1 and 2 are
exit criteria and correction 6 takes B's header search. This is an agent decision from the evidence above.

## 6. Approval status

No human has reviewed, approved, user-tested or signed off on either direction or on the choice in section 5. Every
statement here comes from automated production-build captures, the measurements listed, the prototypes' own test
runs, and an agent's reading of the screenshots. The owner may override the choice; until a recorded human action
exists, nothing in this document is human-approved. Neither prototype branch is merged.
