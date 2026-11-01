# V3 baseline UX audit (shipped v2.1)

status=V3-02_baseline_evidence_complete
source_commit=9fe718e (main after v2.1.0 + V3 Issues migration; no UI change since v2.1.0)
evidence=opt-in production-build capture `TENDO_UX_AUDIT_DIR=/tmp/opencode/v3-02/ux-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh`; real PostgreSQL 18, fixed business date 2026-05-14 Europe/Prague, Chromium with reduced motion; successful run on 2026-11-01, 273 PNG + `metrics.json`, all 12 journey records, zero horizontal overflow
dataset=public-API seeding only: owner + member, 15 people/things with Czech diacritics (some archived), stages 0/1/30/500/1200 items; final counts 883 open, 76 in progress, 57 waiting, 56 paused, 68 done, 60 archived (1072 active)
limits=automated captures plus agent review only; no real household participants; no aesthetic approval is claimed
capture_summary=273 PNG in working output; 98 curated PNG + `metrics.json` in docs/design/v3-baseline/ (17 MiB); 12 journey records including 390×844 and 1440×844 J1/J4/J5 variants

This document is the V3 starting point required by V3_SCOPE step A and V3_DESIGN_BRIEF process steps 1–2.
It records what the shipped UI is and where it falls short for a non-technical household member. It does not
prescribe the new design; directions are explored in V3-03/V3-04 and chosen in V3-05.
Screenshot names below are the files produced by the capture command above (`<name>.png`); V3-02 commits the
curated set under `docs/design/v3-baseline/` and adds journey step/interaction/timing metrics.

Related evidence, not repeated here: [V2 UX and scale audit](../development/v2-ux-audit.md) (findings F1–F23,
R1–R4, scale metrics), [V2.1 coverage matrix](../development/v2-1-coverage-matrix.md) G6 control inventory and
G7 state summary, [screen-reader audit](../development/screen-reader-audit.md).

## 1. Shared chrome

| Element | Shipped behavior | Source |
|---|---|---|
| App shell | One centered `max-w-2xl` shell (672 px outer width; usable content ~608 px after `sm:px-8` padding); identical at 768, 1024 and 1440 px. No sidebar, no multi-column region anywhere. | `frontend/src/app/Layout.tsx:33` |
| Header | Serif "Tendo" wordmark; right side "Signed in as {login}" + Sign out (no confirmation, alert on failure). | `Layout.tsx:41-44`, `AccountActions.tsx` |
| Navigation | Five equal-weight tabs: Home, All items, People and things, Members, Account; `aria-current="page"`; item detail marks All items current; household links hidden without household. | `HouseholdNavigation.tsx:9-32` |
| Skip link | Visible on focus, moves focus to programmatically focusable `main`. | `Layout.tsx:30-39,46` |
| Language | Footer group "Language" with English / Čeština toggle buttons (`aria-pressed`) on every layout screen; persisted. | `Layout.tsx:6-25,49-51` |
| Heading focus/title | In-app navigation focuses the `h1` and sets `document.title` to "{heading} — Tendo"; first load keeps natural focus. | `app/Heading.tsx` |
| Feedback | No toast system and no modal primitive; inline focusable notices (`role=status`), field errors `role=alert`, All items hidden polite live region. | `HomeOverview.tsx`, `AllItemsScreen.tsx:27-34` |
| Theme | Light only: no `dark:` classes, no `prefers-color-scheme`, no theme preference. Reduced motion honored. | `frontend/src/index.css` |
| Unknown path | Falls back to Home; no 404 screen. | `app/routes.ts:14-30`, `App.routing.test.tsx:38-50` |

Design-system facts: Tailwind 4.3.3 CSS-first `@theme` tokens (paper `#faf7f2`, sand `#efe9df`, ink `#25302b`,
muted `#5b655f`, line `#d8d0c3`, field `#857c6e`, accent `#2f6b57`, accent-strong `#234f41`, danger `#a3342a`);
system sans body and system serif display (Charter/Georgia); `text-sm` used 52× and the only sub-base size;
44 px minimum button/input height (`components/ui/button.tsx`, `input.tsx`); bespoke select styling per screen;
no icon library; rounded bordered cards for every list.

## 2. Route × state × control inventory

Routing is a custom pathname/history implementation (`app/routes.ts`, `app/useAppRoute.ts`). Every G6 route and
control group still exists in source; the G6 "absent by design" controls (All items sort, Apply/Reset pair, OIDC
unlink, invitation revoke undo, People row-open) are still absent. Nothing in source lacks a G6 entry.

### Signed-out and boot

| Route / condition | States | Controls | Focus / notes |
|---|---|---|---|
| Boot (any URL) | loading; server unavailable + retry; setup required → first run; signed out → login | Retry | `SignedOutApp.tsx:20-28`, `StatusScreens.tsx` |
| First run (boot state, no URL) | empty; submitting (submit disabled); per-field validation (setup code empty/wrong, name, login pattern, password length, timezone); server error | Setup code, household name, Login, Password, Time zone select (pre-filled detected zone), Create household | error focuses the target field (`OnboardingScreen.tsx`) |
| `/login` | default; invalid credentials; rate limited; unavailable; session ended notice ("You were signed out…"); OIDC provider loading / error + Retry / available; OIDC outcome notices (cancelled, expired/used, unlinked, conflict, changed session, failure, unavailable, unknown) | Login, Password, Sign in, Sign in with {provider}, Retry | signed-in visitor replaced to Home; OIDC outcome parsed from hash then removed (`useOidcOutcome.ts`) |
| `/invite#token` | incomplete link; invalid/already member; signed out (create account + join, 409/422/429 field errors); signed in (Join household); household conflict (Sign out and continue); accepted but session refresh failed (Continue) | Login, Password, Create account and join, Join household, Sign out and continue, Continue, Home link | token read from fragment, never an input; no household navigation (`InviteScreen.tsx`) |

### Signed-in household routes

| Route | States | Controls | Focus / notes |
|---|---|---|---|
| `/` Home | household loading / error + Retry / unavailable / no household; empty household; populated groups Needs attention, In progress, Waiting, Coming up, Paused with server counts; group overview links (≥2 groups); Show more (n remaining) up to 100 rows then See all in All items; item data / name label / member metadata partial failures each with own retry; create uncertain → Check again; catalogue failure retry/cancel; completion success + Undo; completion uncertain + Retry; completion 401/404/412/422/network notices | Add item; add form (What needs doing?, For, When should this need attention?, Repeat, Repeat every n + unit, Count the next repeat from when I complete this, Who's looking after it, Add notes disclosure, Add a previous completion disclosure, Add item, Cancel); per row title link + Done; overview links; Show more; See all | overview link focuses group heading; validation opens collapsed disclosure and focuses field; final Show more focuses heading |
| `/items` All items | loading; load error + Try again; populated "Showing a–b of n"; no match + Clear search and filters; empty household; search validation (200 code points) | Search items + Search; Show (Active/Needs attention/…/Done/Archived, with counts), For, Who's looking after it, Repeat selects (apply on change); First/Previous/Next page; row title links | invalid search returns focus to field; detail Back returns to exact filtered page |
| `/items/:id` detail | loading; load error + retry; gone (404 or malformed id); open (Coming up / Needs attention), in progress, waiting, paused, done, archived; mutation notices (archive with Undo) | Back; Edit; Start / Resume work / Waiting / Pause / Resume (state-dependent); Archive / Restore; history Older / Newer / Latest | no Done/complete action on detail (completion only from Home rows); done and archived hide Edit/workflow |
| `/items/:id` edit (in place) | loading choices; editing; validation; 412 conflict (latest saved values summarized, entries kept); save pending | same fields as Add except previous completion; Save changes; Cancel | cancel focuses Edit; save focuses notice; history hidden while editing |
| `/people` People and things | loading; error + Retry; no household; empty; current list; archived list; create uncertain → Check again; 412 refresh notice; 404 "no longer exists" | Add; Name; Person/Home/Vehicle/Pet/Other radios; linked account select (person); Add/Save/Cancel; row Edit, Archive/Restore, notice Undo; Show archived / Back to the list; First/Previous/Next | focus after archive/undo/edit/paging is tested |
| `/members` | loading; error + Retry; 401 ends session; member list (Owner/Member, "(you)"); non-owner "Only household owners can invite someone"; 403 owner-required vs other refusal; invite created → one-time link; invite uncertain → Check again; invitation list pending/accepted/revoked/expired, empty, error + retry | Create invite link; read-only link + Copy (fallback selects text); Revoke → inline Revoke? Yes/Cancel; member and invitation First/Previous/Next independently | cancel restores focus to Revoke |
| `/account` | base identity; OIDC loading / error + Retry; provider unlinked (current password + Connect); provider linked (display only); OIDC outcome notices | Current password, Connect {provider}, Retry; language text points to footer | no OIDC unlink by design |

Error classes rendered in UI (G7): offline/5xx retry, 401 return to sign-in with notice, 403 owner-required,
404 gone/unavailable, 409 invitation/account conflict, 412 review-and-save-again, 422 field validation,
uncertain-create/complete replay with the same idempotency key. Loading and error never render as empty.

## 3. Findings

Priority = everyday household impact. "Why it matters" statements are audit judgments from screenshots and source, not measured household outcomes. P1 = a household member is slowed, confused or put off in a frequent task,
or a V3 DoD target is unmet; P2 = noticeable friction or inconsistency; P3 = polish. Every P1/P2 maps to the
owning managed V3 Issue.

| ID | Pri | Route · viewport · locale · state | What is wrong | Why it matters to a household member | Evidence | Owner |
|---|---|---|---|---|---|---|
| B1 | P1 | all signed-in routes · 768/1024/1440 · en/cs · any | The app is constrained to a centered `max-w-2xl` shell (672 px outer, ~608 px content) with horizontal padding; at 1440 px over half of the canvas is unused paper and no region adapts to the extra width. | Audit judgment: on laptop/tablet the layout reads as a phone page; likely more scrolling than necessary. The brief explicitly rejects "a phone-width column in a wide empty canvas". | [home-30-en-1440-fold.png](v3-baseline/home-30-en-1440-fold.png), [audit-item-detail-en-1440.png](v3-baseline/audit-item-detail-en-1440.png), [home-empty-en-1440-fold.png](v3-baseline/home-empty-en-1440-fold.png); `Layout.tsx:33` | V3-07 |
| B2 | P1 | `/` · all · en/cs · populated | Home groups use one bordered list with separated rows; each row stacks an underlined title, then 1–3 separate muted lines (person/thing, date, "Looked after by …", "Repeats every 2 weeks · keeps planned dates"). At 390 px only ~2.5 items fit above the fold; at 320 px the Done button drops below the metadata and only ~1.5 items fit. | The daily question "what do I need to do?" needs scanning, not reading; repeated recurrence wording on every row is noise for a list someone checks every day. | [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png), [home-500-en-320-fold.png](v3-baseline/home-500-en-320-fold.png), [home-30-cs-390-fold.png](v3-baseline/home-30-cs-390-fold.png) | V3-08 |
| B3 | P1 | `/` · all · en/cs · populated | Hierarchy is flat: household name as H1, a row of five underlined count links, a full-width Add item button, then group heading in the same serif, all rows with identical weight. Overdue-since dates, assignee and recurrence share one small grey style. Nothing says "today" or makes the next useful action stand out beyond the green Add item. | Users cannot tell at a glance what is most pressing versus merely scheduled; "Needs attention 220" + "Coming up 147" reads like a dashboard counter rather than guidance. | [home-30-en-1440-fold.png](v3-baseline/home-30-en-1440-fold.png), [home-500-en-320-fold.png](v3-baseline/home-500-en-320-fold.png) | V3-08 |
| B4 | P1 | `/` add form · all · en/cs | The expanded Add item form appears inline above the Home groups and pushes them far down the page (title, For, attention date, Repeat, interval, cadence, previous completion, responsible, notes); the form, page and list share the same card style. | Adding a one-off item is a core journey (V3_SCOPE); optional recurrence and history controls make the form feel configuration-heavy and visually dominate when Repeat is on, contrary to the "simple default path" target. | [audit-add-form-en-390.png](v3-baseline/audit-add-form-en-390.png), [audit-add-form-cs-390.png](v3-baseline/audit-add-form-cs-390.png), [audit-add-form-en-1440.png](v3-baseline/audit-add-form-en-1440.png) | V3-10 |
| B5 | P1 | `/items/:id` · all · en/cs · open item | Detail has no Done action; completion exists only on Home rows (and only while the item is in a Home group). The primary filled button is "Start", a workflow concept; Edit, Waiting, Pause, Archive are bare quiet text buttons spread across three separated bands. | Someone who opens a task to finish it cannot finish it there; the most prominent action is one most households never need. (Behavior gap noted for V3-10 to resolve within existing API semantics: completion endpoint already exists.) | [audit-item-detail-en-1440.png](v3-baseline/audit-item-detail-en-1440.png), [audit-item-detail-en-768.png](v3-baseline/audit-item-detail-en-768.png); `ItemDetailActions.tsx` | V3-10 |
| B6 | P1 | all · all · en/cs · any | No dark theme, no OS-following mode, no preference. | DoD requires intentional light/dark with saved preference and no flash. | `index.css`; no `prefers-color-scheme` | V3-14 |
| B7 | P1 | `/items` · all · en/cs · populated | All items rows repeat the Home card pattern with four stacked lines (subject, state in green, recurrence, responsible) and no action; title link + green state text are the only hierarchy. At 1440 px the list stays in the narrow shell; a 25-row page is a very long scroll; filters are four bare native selects with terse labels ("Show", "For"). | Finding an old item among 1000+ is a scan task; at desktop width the narrow list and repeated stacked metadata make scanning a large result set slow. Layout choice is left to V3-03..V3-05. | [audit-all-items-en-1440.png](v3-baseline/audit-all-items-en-1440.png), [audit-all-items-en-390.png](v3-baseline/audit-all-items-en-390.png) | V3-09 |
| B8 | P2 | nav · 320/390 · en/cs | Five equal tabs wrap to two rows on phones ("People and things" itself wraps to two lines; at 320 px the header login also wraps), consuming ~170 px before the page title; Account and Members get the same weight as Home. | Navigation competes with content on the screen people use most; secondary destinations (Account, Members) are as loud as daily ones. | [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png), [home-500-en-320-fold.png](v3-baseline/home-500-en-320-fold.png), [audit-account-en-390.png](v3-baseline/audit-account-en-390.png) | V3-07, V3-15 |
| B9 | P2 | all · all · en/cs | Walls of `text-sm` muted metadata (#5b655f on paper) with 3–4 lines per row; dates are long ("Needs attention since Mar 30, 2026" / "Vyžaduje pozornost od 30. 3. 2026"); muted is used both for supporting text and for actionable state. | Older or low-vision users must read grey small text to understand state; long date phrases wrap at 390 px. | [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png), [home-30-cs-390-fold.png](v3-baseline/home-30-cs-390-fold.png), [audit-all-items-en-390.png](v3-baseline/audit-all-items-en-390.png) | V3-06, V3-08, V3-09 |
| B10 | P2 | `/items/:id` · all · en | "Next attention date: Jun 13, 2026", "Last done", "Repeats every 1 day / Keeps the planned dates." are stacked grey lines in one card with equal spacing; history rows ("Done … / Planned for … / Next …") are three-line boxes that look like the summary card. | The item's essential facts (when is it due, how does it repeat, who has it) are not distinguishable from history. | [audit-item-detail-en-1440.png](v3-baseline/audit-item-detail-en-1440.png), [audit-item-detail-done-en-390.png](v3-baseline/audit-item-detail-done-en-390.png) | V3-10 |
| B11 | P2 | `/people` · all · en/cs · populated | Each person/thing row shows Edit and Archive as equal text buttons; type shown as a grey word; list stays in the narrow shell at all widths; the screen does not make the relationship between a person and their items discoverable. | Audit judgment: Archive beside Edit on every row makes a mis-tap easy (Undo exists). Whether existing navigation (e.g. the All items "For" filter) can clarify the person→items relationship is for V3-11 to assess without adding product scope. | [audit-people-things-en-390.png](v3-baseline/audit-people-things-en-390.png), [audit-people-things-en-1440.png](v3-baseline/audit-people-things-en-1440.png) | V3-11 |
| B12 | P2 | `/members` · all · en · owner with invitations | In the captured owner state, invitation rows have equal visual weight (Accepted/Pending + Created/Expires dates); the one-time invite link is shown as a raw URL in a text input; role words "Owner/Member" lack explanation. | Audit judgment: inviting a partner is a one-time moment; equal-weight rows and a raw URL read as admin tooling. | [audit-members-invite-en-1440.png](v3-baseline/audit-members-invite-en-1440.png), [audit-members-en-390.png](v3-baseline/audit-members-en-390.png) | V3-12 |
| B13 | P2 | first run · all · en | Setup form mixes field label styles: household name uses a large serif question while Setup code, Login, Password, Time zone use small labels; "Login" and "Setup code" are technical; time zone is a raw IANA select ("Europe/Prague") with helper repeating the value. | The first screen sets the product's tone; inconsistent hierarchy and technical words make it feel like a server install wizard. | [onboarding-en-390.png](v3-baseline/onboarding-en-390.png) | V3-13 |
| B14 | P2 | `/account` · all · en | Account is nearly empty: one sentence, then a "Language" section telling users to use the footer. No theme setting exists to place here. | A settings page that only points elsewhere is confusing and wastes a primary nav slot. | [audit-account-en-390.png](v3-baseline/audit-account-en-390.png) | V3-13, V3-14 |
| B15 | P2 | `/login` · all · en | Login error highlights both fields red and focuses Password; layout is a bare form at the top of an otherwise empty page at 1440 px, same width as phone. | Sign-in and errors are the first impression for invited members; visual weight is fine but generic. | [audit-login-error-en-390.png](v3-baseline/audit-login-error-en-390.png), [audit-login-en-1440.png](v3-baseline/audit-login-en-1440.png) | V3-13 |
| B16 | P2 | all · all · en/cs | Vocabulary: "Needs attention", "attention date", "When should this need attention?", "Coming up", "Looked after by", "For", "Login", "Setup code", "Show"; cs "Vyžaduje pozornost", "Stará se: …", "drží plánované termíny". Fixed vs after-completion cadence is a checkbox with a long sentence and an inverse explanation ("Keeps the planned dates, even if you finish late." shown when *unchecked*). No household examples. | The brief requires warm, exact, short copy and clear fixed vs after-completion explanation with examples; the inverse-state hint is easy to misread. | [audit-add-form-en-390.png](v3-baseline/audit-add-form-en-390.png), [audit-add-form-cs-390.png](v3-baseline/audit-add-form-cs-390.png); `i18n/en.ts:243,275-279,295-298` | V3-14, V3-10 |
| B17 | P2 | all forms · all | Forms are inconsistent: native date inputs show `mm/dd/yyyy` placeholder on en, bespoke selects per screen, checkboxes styled as Repeat "toggle", disclosures as quiet text buttons ("Add notes (optional)", "Add a previous completion"), radio group for subject type. | Mixed control vocabulary increases cognitive load and looks unplanned. | [audit-add-form-en-390.png](v3-baseline/audit-add-form-en-390.png), [audit-item-edit-en-390.png](v3-baseline/audit-item-edit-en-390.png), [audit-people-things-en-390.png](v3-baseline/audit-people-things-en-390.png), [onboarding-en-390.png](v3-baseline/onboarding-en-390.png) | V3-06 (shared patterns), V3-10, V3-11, V3-12, V3-13, V3-15 |
| B18 | P2 | `/` · all · en/cs · populated | Home group overview counts are underlined text links that look like body text; Show more / See all are quiet buttons at the bottom of long groups. | Hard to discover that the page can jump to a group; long groups bury later groups (In progress, Waiting) far below. | [home-500-en-1440.png](v3-baseline/home-500-en-1440.png), [home-1200-en-390-fold.png](v3-baseline/home-1200-en-390-fold.png) | V3-08 |
| B19 | P2 | all · all | Repetitive card styling for unlike things: empty state, add form, list groups, item summary, invite panel, history entries all use rounded bordered paper boxes with similar padding. | No visual difference between "a thing you act on", "a form", and "information". | [home-empty-en-1440-fold.png](v3-baseline/home-empty-en-1440-fold.png), [audit-item-detail-en-1440.png](v3-baseline/audit-item-detail-en-1440.png), [audit-members-invite-en-1440.png](v3-baseline/audit-members-invite-en-1440.png) | V3-06 |
| B20 | P3 | `/` · all · en/cs | Home H1 is the household name; there is no date or "today" anchor. | Mild; household name is warm but does not orient a daily check-in. | [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png) | V3-08 |
| B21 | P3 | unknown path · all | Unknown URLs silently show Home. | Rare; acceptable, but a typo in a shared link gives no feedback. | `App.routing.test.tsx:38-50` | V3-13 |
| B22 | P3 | footer · all | Language switch only in the footer, far below long lists. | Discoverable once; acceptable if Account also offers it. | all full-page captures | V3-14 |
| B23 | P2 | `/invite#token` · 390 · en · signed out | The screen says "You've been invited to Tendo" but does not name the household or the inviter before asking for credentials. | The recipient has no visible context to confirm which household they are joining. Note: no invitation-preview API exists; V3-12/V3-13 may only improve copy unless a separately justified API change is approved (V3_SCOPE excludes API rewrites for decoration). | [audit-invite-accept-en-390.png](v3-baseline/audit-invite-accept-en-390.png) | V3-12, V3-13 |
| B24 | P2 | `/items/:id` edit · 390 · en · 412 conflict | The conflict card shows the latest saved title above the form, but the edited value remains in the field below with only a general hint; the two versions are not adjacent or labeled together in a way that makes comparison easy. | A member can hesitate or overwrite their work because they must compare separated versions and decide whether to save again. | [audit-item-edit-conflict-en-390.png](v3-baseline/audit-item-edit-conflict-en-390.png) | V3-10 |
| B25 | P2 | `/` · 390 · en · item summary 503 | The service-error view still displays the household heading and a full-width Add item action while the task list is unavailable. | A household member may mistake the partial screen for a usable empty home and start adding a duplicate instead of retrying. | [audit-home-server-error-en-390.png](v3-baseline/audit-home-server-error-en-390.png) | V3-08 |
| B26 | P2 | `/members` · 390/1440 · en · non-owner | The non-owner screen shows the invite restriction as low-emphasis muted text separated below the members list, without saying who in the household can help. | A member who needs to invite someone has no clear next step or contact target. | [audit-members-non-owner-en-390.png](v3-baseline/audit-members-non-owner-en-390.png), [audit-members-non-owner-en-1440.png](v3-baseline/audit-members-non-owner-en-1440.png) | V3-12 |
| B27 | P2 | `/` Home · 320/390 · en · keyboard | The first Home result link takes 14 Tab stops from page start, including skip link, shared navigation and overview links. | Keyboard users traverse substantial chrome before reaching the first task needing attention during a daily check-in. | [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png); `metrics.json` `tabStopsToFirstResult=14` | V3-07, V3-08 |
| B28 | P3 | `/` add/edit form · all · en/cs · Repeat | The Repeat and "count from completion" switches are native checkboxes drawn at about 18×24 px beside their text; the 44 px label row is clickable, but the visible control is tiny and reads as a checkbox styled as a toggle. Home group overview links are 24 px tall text links. | Touch users aim at the visible control; tiny or text-sized targets look fiddly and inconsistent with the 44 px buttons. | [audit-add-form-en-390.png](v3-baseline/audit-add-form-en-390.png), [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png); `metrics.json` `targetOffenders` | V3-06, V3-10, V3-15 |

Confirmed strengths to keep: calm warm palette and serif display identity; no overflow at 320–1440 px on any
captured route (metrics `overflow=false` for all captured Home stages and route captures); 44 px targets on
buttons/inputs; visible focus rings; human tone ("It has been…", no shaming); bounded Home groups and truthful
server counts; fast loads (Home 1200 items: 11 requests, ~60 kB, <260 ms to content on loopback; see V2 audit).

## 4. Behaviors that MUST NOT change in V3 (feeds V3-16)

Visual redesign may restyle and reorganize, but these behaviors keep their existing tests green and unweakened.

1. Route/history contract: direct paths, deep links, reload, Back/Forward, invalid item id → gone, All items
   filter/search/page context restored on Back, invite token from URL fragment only, never an input, removed
   from visible URL. (`App.routing.test.tsx`, `InviteScreen.test.tsx`)
2. Accessibility contract: skip link; `h1` focus + document title on in-app navigation; landmarks; live regions
   (`status` for outcomes, `alert` for validation); keyboard reachability; visible focus; ≥44 px targets;
   reflow at 200 %/400 % zoom; reduced motion; axe WCAG 2.2 A/AA scans. (journey n14, n15, n16, n21, n23)
3. Language: complete en/cs, persisted choice, long Czech strings wrap without clipping. (journey f, n18)
4. Home: server-truthful group counts; groups Needs attention / In progress / Waiting / Coming up / Paused;
   bounded rows (Show more remaining, 100-row cap, See all); empty guidance distinct from loading/error.
   (`ItemsScreen.test.tsx`, journey n11)
5. Item recurrence: Repeat OFF by default and a fully valid one-off; Repeat ON reveals interval and a separate
   after-completion choice; fixed keeps planned cadence; after-completion counts from completion; previous
   completion only for repeating Add and initializes the first cycle; undo restores prior schedule; changes apply
   from next completion. (`ItemForm.test.tsx`, journey n1–n3b, n6; TESTING frontend list)
6. Completion safety: quick completion with Undo while allowed; uncertain create/complete replay with the same
   idempotency key and body; no duplicate create/completion; definitive errors never offer retry. (journey n24)
7. Item detail/edit: workflow transitions; archive/restore with Undo; history paging; edit sends only changed
   fields; 412 conflict keeps user entries and shows latest values, never silently overwrites; focus after
   save/cancel. (`ItemDetailScreen.test.tsx`, journey n4, n5, n27)
8. People and things: type choices; linked-account constraints; archive/restore/undo; paging; 412/404 handling;
   focus after archive/undo/edit. (`SubjectsScreen.test.tsx`, journey i, l, n16b, n22)
9. Members/invitations: owner vs member controls; owner-required 403 distinct; independent paging; idempotent
   invite creation and uncertain recovery; one-time link with clipboard fallback; revoke confirmation with focus
   restore. (`MembersScreen*.test.tsx`, journey n7, n16b, n25, n29)
10. Auth/OIDC: login validation; session-ended notice; sign-out revokes server session; provider outage never
    blocks password login; Account link flow and callback outcome notices; no household switcher, no household
    IDs shown. (`Oidc.test.tsx`, journey n9, n19, n20, o, p, q)
11. Failure states stay distinct and recoverable: offline/5xx, 401, 403, 404, 409, 412, 422 and independent
    sub-resource failures. (journey n17, n18, n29; G7)
12. Household authorization and cross-household denial, security headers, no console errors. (journey n10, r)
13. Product invariants from GUARDRAILS/UX: time never completes or removes items; never shame lateness; no
    dashboards, charts, gamification; one-off is first class.

## 5. Coverage gaps observed while auditing

- Responsible assignment is one user per item; realistic variation is across seeded rows rather than several assignees on one item.
- Czech auth states are representative, not every validation/OIDC outcome and viewport. OIDC outcomes remain covered by functional tests, not baseline screenshots.
- J1–J9 are scripted Playwright actions, not observed novice-participant research. No real participants were observed; timings are automated loopback machine timings, not human task durations.
- Dark appearance is not supported; the dark color-scheme capture documents the current light rendering.


## 6. Baseline evidence (V3-02)

The committed baseline is a curated set of authentic production-build browser captures. Every image is generated by the opt-in harness; Home uses fold captures for long groups, while route captures use full-page screenshots. The curated set is 98 PNG at 17 MiB (below the 30 MiB limit); every committed PNG has a metrics entry.

### Screenshot index

| File | Route | Viewport | Locale | State |
|---|---|---:|---|---|
| [home-empty-en-320-fold.png](v3-baseline/home-empty-en-320-fold.png), [home-empty-en-360-fold.png](v3-baseline/home-empty-en-360-fold.png), [home-empty-en-390-fold.png](v3-baseline/home-empty-en-390-fold.png), [home-empty-en-768-fold.png](v3-baseline/home-empty-en-768-fold.png), [home-empty-en-1024-fold.png](v3-baseline/home-empty-en-1024-fold.png), [home-empty-en-1440-fold.png](v3-baseline/home-empty-en-1440-fold.png), [home-empty-cs-320-fold.png](v3-baseline/home-empty-cs-320-fold.png), [home-empty-cs-360-fold.png](v3-baseline/home-empty-cs-360-fold.png), [home-empty-cs-390-fold.png](v3-baseline/home-empty-cs-390-fold.png), [home-empty-cs-768-fold.png](v3-baseline/home-empty-cs-768-fold.png), [home-empty-cs-1024-fold.png](v3-baseline/home-empty-cs-1024-fold.png), [home-empty-cs-1440-fold.png](v3-baseline/home-empty-cs-1440-fold.png) | `/` | 320–1440 | en/cs | empty |
| [home-30-en-320-fold.png](v3-baseline/home-30-en-320-fold.png), [home-30-en-360-fold.png](v3-baseline/home-30-en-360-fold.png), [home-30-en-390-fold.png](v3-baseline/home-30-en-390-fold.png), [home-30-en-768-fold.png](v3-baseline/home-30-en-768-fold.png), [home-30-en-1024-fold.png](v3-baseline/home-30-en-1024-fold.png), [home-30-en-1440-fold.png](v3-baseline/home-30-en-1440-fold.png) | `/` | 320–1440 | en | populated 30-stage |
| [home-30-cs-320-fold.png](v3-baseline/home-30-cs-320-fold.png), [home-30-cs-360-fold.png](v3-baseline/home-30-cs-360-fold.png), [home-30-cs-390-fold.png](v3-baseline/home-30-cs-390-fold.png), [home-30-cs-768-fold.png](v3-baseline/home-30-cs-768-fold.png), [home-30-cs-1024-fold.png](v3-baseline/home-30-cs-1024-fold.png), [home-30-cs-1440-fold.png](v3-baseline/home-30-cs-1440-fold.png) | `/` | 320–1440 | cs | populated 30-stage |
| [audit-all-items-en-390.png](v3-baseline/audit-all-items-en-390.png), [audit-all-items-en-1440.png](v3-baseline/audit-all-items-en-1440.png), [audit-all-items-cs-390.png](v3-baseline/audit-all-items-cs-390.png), [audit-all-items-cs-1440.png](v3-baseline/audit-all-items-cs-1440.png) | `/items` | 390/1440 | en/cs | active list |
| [audit-item-detail-en-390.png](v3-baseline/audit-item-detail-en-390.png), [audit-item-detail-en-1440.png](v3-baseline/audit-item-detail-en-1440.png), [audit-item-detail-cs-390.png](v3-baseline/audit-item-detail-cs-390.png), [audit-item-detail-cs-1440.png](v3-baseline/audit-item-detail-cs-1440.png) | `/items/:id` | 390/1440 | en/cs | open, history |
| [audit-item-detail-done-en-390.png](v3-baseline/audit-item-detail-done-en-390.png), [audit-item-detail-done-en-1440.png](v3-baseline/audit-item-detail-done-en-1440.png), [audit-item-detail-done-cs-390.png](v3-baseline/audit-item-detail-done-cs-390.png), [audit-item-detail-done-cs-1440.png](v3-baseline/audit-item-detail-done-cs-1440.png) | `/items/:id` | 390/1440 | en/cs | done |
| [audit-item-detail-archived-en-390.png](v3-baseline/audit-item-detail-archived-en-390.png), [audit-item-detail-archived-en-1440.png](v3-baseline/audit-item-detail-archived-en-1440.png), [audit-item-detail-archived-cs-390.png](v3-baseline/audit-item-detail-archived-cs-390.png), [audit-item-detail-archived-cs-1440.png](v3-baseline/audit-item-detail-archived-cs-1440.png) | `/items/:id` | 390/1440 | en/cs | archived |
| [audit-item-edit-en-390.png](v3-baseline/audit-item-edit-en-390.png), [audit-item-edit-en-1440.png](v3-baseline/audit-item-edit-en-1440.png), [audit-item-edit-cs-390.png](v3-baseline/audit-item-edit-cs-390.png), [audit-item-edit-cs-1440.png](v3-baseline/audit-item-edit-cs-1440.png), [audit-item-edit-conflict-en-390.png](v3-baseline/audit-item-edit-conflict-en-390.png), [audit-item-detail-long-notes-en-390.png](v3-baseline/audit-item-detail-long-notes-en-390.png), [audit-item-detail-long-notes-en-1440.png](v3-baseline/audit-item-detail-long-notes-en-1440.png), [audit-item-detail-long-title-en-390.png](v3-baseline/audit-item-detail-long-title-en-390.png), [audit-item-detail-long-title-en-1440.png](v3-baseline/audit-item-detail-long-title-en-1440.png), [audit-item-detail-long-title-cs-390.png](v3-baseline/audit-item-detail-long-title-cs-390.png) | `/items/:id` edit/detail | 390/1440 | en/cs | edit, 412 conflict, long Czech title and multi-paragraph notes |
| [audit-add-form-en-390.png](v3-baseline/audit-add-form-en-390.png), [audit-add-form-en-1440.png](v3-baseline/audit-add-form-en-1440.png), [audit-add-form-cs-390.png](v3-baseline/audit-add-form-cs-390.png), [audit-add-form-cs-1440.png](v3-baseline/audit-add-form-cs-1440.png) | `/` add form | 390/1440 | en/cs | recurring form |
| [audit-people-things-en-390.png](v3-baseline/audit-people-things-en-390.png), [audit-people-things-en-1440.png](v3-baseline/audit-people-things-en-1440.png), [audit-people-things-cs-390.png](v3-baseline/audit-people-things-cs-390.png), [audit-people-things-cs-1440.png](v3-baseline/audit-people-things-cs-1440.png) | `/people` | 390/1440 | en/cs | populated |
| [audit-members-en-390.png](v3-baseline/audit-members-en-390.png), [audit-members-en-1440.png](v3-baseline/audit-members-en-1440.png), [audit-members-cs-390.png](v3-baseline/audit-members-cs-390.png), [audit-members-cs-1440.png](v3-baseline/audit-members-cs-1440.png), [audit-members-non-owner-en-390.png](v3-baseline/audit-members-non-owner-en-390.png), [audit-members-non-owner-en-1440.png](v3-baseline/audit-members-non-owner-en-1440.png), [audit-members-non-owner-cs-390.png](v3-baseline/audit-members-non-owner-cs-390.png), [audit-members-non-owner-cs-1440.png](v3-baseline/audit-members-non-owner-cs-1440.png) | `/members` | 390/1440 | en/cs | owner and non-owner |
| [audit-account-en-390.png](v3-baseline/audit-account-en-390.png), [audit-account-en-1440.png](v3-baseline/audit-account-en-1440.png), [audit-account-cs-390.png](v3-baseline/audit-account-cs-390.png), [audit-account-cs-1440.png](v3-baseline/audit-account-cs-1440.png) | `/account` | 390/1440 | en/cs | base account |
| [onboarding-en-390.png](v3-baseline/onboarding-en-390.png), [onboarding-en-1440.png](v3-baseline/onboarding-en-1440.png), [onboarding-cs-390.png](v3-baseline/onboarding-cs-390.png), [onboarding-cs-1440.png](v3-baseline/onboarding-cs-1440.png), [onboarding-error-en-390.png](v3-baseline/onboarding-error-en-390.png) | first run | 390/1440 | en/cs | empty and validation error |
| [audit-login-en-390.png](v3-baseline/audit-login-en-390.png), [audit-login-en-1440.png](v3-baseline/audit-login-en-1440.png), [audit-login-cs-390.png](v3-baseline/audit-login-cs-390.png), [audit-login-cs-1440.png](v3-baseline/audit-login-cs-1440.png) | `/login` | 390/1440 | en/cs | sign-in |
| [audit-login-error-en-390.png](v3-baseline/audit-login-error-en-390.png), [audit-login-error-en-1440.png](v3-baseline/audit-login-error-en-1440.png) | `/login` | 390/1440 | en | invalid credentials |
| [audit-invite-accept-en-390.png](v3-baseline/audit-invite-accept-en-390.png), [audit-invite-accept-en-1440.png](v3-baseline/audit-invite-accept-en-1440.png) | `/invite#token` | 390/1440 | en | signed-out acceptance |
| [audit-members-invite-en-390.png](v3-baseline/audit-members-invite-en-390.png), [audit-members-invite-en-1440.png](v3-baseline/audit-members-invite-en-1440.png) | `/members` | 390/1440 | en | one-time invite link shown (link field masked: bearer token) |
| [home-1-en-390-fold.png](v3-baseline/home-1-en-390-fold.png), [home-1-cs-390-fold.png](v3-baseline/home-1-cs-390-fold.png) | `/` | 390 | en/cs | single item |
| [home-500-en-320-fold.png](v3-baseline/home-500-en-320-fold.png), [home-500-en-1440-fold.png](v3-baseline/home-500-en-1440-fold.png), [home-500-en-1440.png](v3-baseline/home-500-en-1440.png) | `/` | 320/1440 | en | 500-item stage (fold and full page) |
| [audit-500-find-manage-detail-en-390.png](v3-baseline/audit-500-find-manage-detail-en-390.png) | `/items/:id` | 390 | en | 500-stage search → pause → back → resume detail |
| [home-1200-en-390-fold.png](v3-baseline/home-1200-en-390-fold.png), [home-1200-en-390.png](v3-baseline/home-1200-en-390.png), [home-1200-en-1440.png](v3-baseline/home-1200-en-1440.png) | `/` | 390/1440 | en | 1200-item stage (fold and full page) |
| [audit-home-server-error-en-390.png](v3-baseline/audit-home-server-error-en-390.png), [audit-home-server-error-cs-390.png](v3-baseline/audit-home-server-error-cs-390.png), [home-1200-en-390-dark-scheme.png](v3-baseline/home-1200-en-390-dark-scheme.png) | `/` | 390 | en/cs | 503; dark preference renders light |

### Capture metrics

Metric records are keyed by screenshot filename in `metrics.json`; values are measured at capture viewport. Overflow means document horizontal scroll exceeds the viewport.

| Route/state capture | Smallest font | Targets <44 px (of which <24 px) | Tab stops to first result | DOM size | Overflow |
|---|---:|---:|---:|---:|---|
| Home empty, en 320 fold | 14 px | 0 | n/a (no result) | 44 | false |
| Home populated 30, en 320/390 fold | 14 px | 5 (0): group overview links, 24 px tall | 14 | 271 | false |
| All items active, en 390 | 14 px | 0 | 14 | 216 | false |
| Add form with Repeat on, en 390 | 14 px | 2 (1): Repeat switch input 18×24 px (its label row is 44 px) | n/a | 1128 | false |
| Item detail, en 390 | 14 px | 0 | n/a | 66 | false |
| People and things, en 390 | 14 px | 0 | n/a | 140 | false |
| Members, en 390 | 14 px | 0 | n/a | 73 | false |
| Account, en 390 | 14 px | 0 | n/a | 42 | false |

Tab stops are counted from a pointer click on an inert page corner (resets Chromium's sequential focus start), then
Tab until the first result link is focused. Targets count each visible interactive element's own box; a control
wrapped in a larger clickable label (the Repeat switch) is reported by its input box.

All values are per-capture metrics from `metrics.json` (`routes` is keyed by PNG filename); the full matrix covers each capture width/locale/state.

For quick comparison, `metrics.json` is the exact per-screenshot source of all measurements. It records computed font sizes, both undersized-target thresholds and initial offenders, DOM counts, overflow, and first-result keyboard tab stops for Home and All items.

### Novice journeys

Journey counts and wall time are collected by scripted Playwright actions with the 390×844 viewport, loopback production build, and reduced motion; J1/J4/J5 also run at 1440×844. All nine journeys are action-scripted; timings are non-deterministic loopback machine timings. A step is a user-visible screen/decision; an interaction is a click or a grouped field-entry/keypress action; keystrokes count typed characters separately. Wall time spans first to last recorded action. These are automation-only machine timings, not human task times; there were no real participants.

| Journey | Viewport | Steps | Interactions | Keystrokes | Wall ms | Obstacles |
|---|---:|---:|---:|---:|---:|---|
| J1 add one-off from Home | 390×844 + 1440×844 | 2 | 4 | 26 | 322 / 375 | none |
| J2 fixed + after-completion recurrence | 390×844 | 3 | 15 | 49 | 800 | Repeat reveals cadence fields |
| J3 find Needs attention by keyboard | 390×844 | 2 | 14 | 0 | 576 | 14 Tab stops to first row |
| J4 mark done + undo | 390×844 + 1440×844 | 2 | 2 | 0 | 384 / 403 | none |
| J5 search old done item | 390×844 + 1440×844 | 2 | 4 | 15 | 234 / 247 | none |
| J6 edit/archive/restore | 390×844 | 2 | 5 | 30 | 459 | detail has no Done action |
| J7 invite member and copy | 390×844 | 2 | 2 | 0 | 146 | one-time invite link |
| J8 sign in | 390×844 | 2 | 3 | 43 | 339 | none |
| J9 first-run setup | 390×844 | 2 | 6 | 102 | 163 | setup code required |

The capture run completed with all 12 journey records in `metrics.json`; counts and timings above are from that file. J1/J4/J5 include 390×844 and 1440×844 measurements (wall ms listed in that order).
