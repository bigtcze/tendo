# V3 baseline UX audit (shipped v2.1)

status=V3-01_inventory_and_findings; V3-02 adds committed screenshots, journey metrics and timings
source_commit=9fe718e (main after v2.1.0 + V3 Issues migration; no UI change since v2.1.0)
evidence=opt-in production-build capture `TENDO_UX_AUDIT_DIR=/tmp/opencode/v3-baseline/ux-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` (see [testing](../development/testing.md#uxscale-audit-capture)); real PostgreSQL 18, fixed business date 2026-05-14 Europe/Prague, Chromium with reduced motion; run 2026-10-31 exit 0, 164 PNG + `metrics.json`, zero horizontal overflow recorded
dataset=public-API seeding only: owner + member, 15 people/things with Czech diacritics (some archived), stages 0/1/30/500/1200 items; final counts 882 open, 77 in progress, 56 waiting, 57 paused, 68 done, 60 archived (1072 active)
limits=automated captures plus agent review only; no real household participants; no aesthetic approval is claimed

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
| B1 | P1 | all signed-in routes · 768/1024/1440 · en/cs · any | The app is constrained to a centered `max-w-2xl` shell (672 px outer, ~608 px content) with horizontal padding; at 1440 px over half of the canvas is unused paper and no region adapts to the extra width. | Audit judgment: on laptop/tablet the layout reads as a phone page; likely more scrolling than necessary. The brief explicitly rejects "a phone-width column in a wide empty canvas". | `home-30-en-1440-fold`, `audit-item-detail-en-1440`, `home-empty-en-1440-fold`; `Layout.tsx:33` | V3-07 |
| B2 | P1 | `/` · all · en/cs · populated | Home groups use one bordered list with separated rows; each row stacks an underlined title, then 1–3 separate muted lines (person/thing, date, "Looked after by …", "Repeats every 2 weeks · keeps planned dates"). At 390 px only ~2.5 items fit above the fold; at 320 px the Done button drops below the metadata and only ~1.5 items fit. | The daily question "what do I need to do?" needs scanning, not reading; repeated recurrence wording on every row is noise for a list someone checks every day. | `home-30-en-390-fold`, `home-500-en-320-fold`, `home-30-cs-390-fold` | V3-08 |
| B3 | P1 | `/` · all · en/cs · populated | Hierarchy is flat: household name as H1, a row of five underlined count links, a full-width Add item button, then group heading in the same serif, all rows with identical weight. Overdue-since dates, assignee and recurrence share one small grey style. Nothing says "today" or makes the next useful action stand out beyond the green Add item. | Users cannot tell at a glance what is most pressing versus merely scheduled; "Needs attention 220" + "Coming up 147" reads like a dashboard counter rather than guidance. | `home-30-en-1440-fold`, `home-500-en-320-fold` | V3-08 |
| B4 | P1 | `/` add form · all · en/cs | The expanded Add item form appears inline above the Home groups and pushes them far down the page (title, For, attention date, Repeat, interval, cadence, previous completion, responsible, notes); the form, page and list share the same card style. | Adding a one-off item is a core journey (V3_SCOPE); optional recurrence and history controls make the form feel configuration-heavy and visually dominate when Repeat is on, contrary to the "simple default path" target. | `audit-add-form-en-390`, `audit-add-form-cs-390`, `audit-add-form-en-1440` | V3-10 |
| B5 | P1 | `/items/:id` · all · en/cs · open item | Detail has no Done action; completion exists only on Home rows (and only while the item is in a Home group). The primary filled button is "Start", a workflow concept; Edit, Waiting, Pause, Archive are bare quiet text buttons spread across three separated bands. | Someone who opens a task to finish it cannot finish it there; the most prominent action is one most households never need. (Behavior gap noted for V3-10 to resolve within existing API semantics: completion endpoint already exists.) | `audit-item-detail-en-1440`, `audit-item-detail-en-768`; `ItemDetailActions.tsx` | V3-10 |
| B6 | P1 | all · all · en/cs · any | No dark theme, no OS-following mode, no preference. | DoD requires intentional light/dark with saved preference and no flash. | `index.css`; no `prefers-color-scheme` | V3-14 |
| B7 | P1 | `/items` · all · en/cs · populated | All items rows repeat the Home card pattern with four stacked lines (subject, state in green, recurrence, responsible) and no action; title link + green state text are the only hierarchy. At 1440 px the list stays in the narrow shell; a 25-row page is a very long scroll; filters are four bare native selects with terse labels ("Show", "For"). | Finding an old item among 1000+ is a scan task; at desktop width the narrow list and repeated stacked metadata make scanning a large result set slow. Layout choice is left to V3-03..V3-05. | `audit-all-items-en-1440`, `audit-all-items-en-390` | V3-09 |
| B8 | P2 | nav · 320/390 · en/cs | Five equal tabs wrap to two rows on phones ("People and things" itself wraps to two lines; at 320 px the header login also wraps), consuming ~170 px before the page title; Account and Members get the same weight as Home. | Navigation competes with content on the screen people use most; secondary destinations (Account, Members) are as loud as daily ones. | `home-30-en-390-fold`, `home-500-en-320-fold`, `audit-account-en-390` | V3-07, V3-15 |
| B9 | P2 | all · all · en/cs | Walls of `text-sm` muted metadata (#5b655f on paper) with 3–4 lines per row; dates are long ("Needs attention since Mar 30, 2026" / "Vyžaduje pozornost od 30. 3. 2026"); muted is used both for supporting text and for actionable state. | Older or low-vision users must read grey small text to understand state; long date phrases wrap at 390 px. | `home-30-en-390-fold`, `home-30-cs-390-fold`, `audit-all-items-en-390` | V3-06, V3-08, V3-09 |
| B10 | P2 | `/items/:id` · all · en | "Next attention date: Jun 13, 2026", "Last done", "Repeats every 1 day / Keeps the planned dates." are stacked grey lines in one card with equal spacing; history rows ("Done … / Planned for … / Next …") are three-line boxes that look like the summary card. | The item's essential facts (when is it due, how does it repeat, who has it) are not distinguishable from history. | `audit-item-detail-en-1440`, `audit-item-detail-done-en-390` | V3-10 |
| B11 | P2 | `/people` · all · en/cs · populated | Each person/thing row shows Edit and Archive as equal text buttons; type shown as a grey word; list stays in the narrow shell at all widths; the screen does not make the relationship between a person and their items discoverable. | Audit judgment: Archive beside Edit on every row makes a mis-tap easy (Undo exists). Whether existing navigation (e.g. the All items "For" filter) can clarify the person→items relationship is for V3-11 to assess without adding product scope. | `audit-people-things-en-390`, `audit-people-things-en-1440` | V3-11 |
| B12 | P2 | `/members` · all · en · owner with invitations | In the captured owner state, invitation rows have equal visual weight (Accepted/Pending + Created/Expires dates); the one-time invite link is shown as a raw URL in a text input; role words "Owner/Member" lack explanation. | Audit judgment: inviting a partner is a one-time moment; equal-weight rows and a raw URL read as admin tooling. | `audit-members-invite-en-1440`, `audit-members-en-390` | V3-12 |
| B13 | P2 | first run · all · en | Setup form mixes field label styles: household name uses a large serif question while Setup code, Login, Password, Time zone use small labels; "Login" and "Setup code" are technical; time zone is a raw IANA select ("Europe/Prague") with helper repeating the value. | The first screen sets the product's tone; inconsistent hierarchy and technical words make it feel like a server install wizard. | `onboarding-en-390` | V3-13 |
| B14 | P2 | `/account` · all · en | Account is nearly empty: one sentence, then a "Language" section telling users to use the footer. No theme setting exists to place here. | A settings page that only points elsewhere is confusing and wastes a primary nav slot. | `audit-account-en-390` | V3-13, V3-14 |
| B15 | P2 | `/login` · all · en | Login error highlights both fields red and focuses Password; layout is a bare form at the top of an otherwise empty page at 1440 px, same width as phone. | Sign-in and errors are the first impression for invited members; visual weight is fine but generic. | `audit-login-error-en-390`, `audit-login-en-1440` | V3-13 |
| B16 | P2 | all · all · en/cs | Vocabulary: "Needs attention", "attention date", "When should this need attention?", "Coming up", "Looked after by", "For", "Login", "Setup code", "Show"; cs "Vyžaduje pozornost", "Stará se: …", "drží plánované termíny". Fixed vs after-completion cadence is a checkbox with a long sentence and an inverse explanation ("Keeps the planned dates, even if you finish late." shown when *unchecked*). No household examples. | The brief requires warm, exact, short copy and clear fixed vs after-completion explanation with examples; the inverse-state hint is easy to misread. | `audit-add-form-en-390`, `audit-add-form-cs-390`; `i18n/en.ts:243,275-279,295-298` | V3-14, V3-10 |
| B17 | P2 | all forms · all | Forms are inconsistent: native date inputs show `mm/dd/yyyy` placeholder on en, bespoke selects per screen, checkboxes styled as Repeat "toggle", disclosures as quiet text buttons ("Add notes (optional)", "Add a previous completion"), radio group for subject type. | Mixed control vocabulary increases cognitive load and looks unplanned. | `audit-add-form-en-390`, `audit-item-edit-en-390`, `audit-people-things-en-390`, `onboarding-en-390` | V3-06 (shared patterns), V3-10, V3-11, V3-12, V3-13, V3-15 |
| B18 | P2 | `/` · all · en/cs · populated | Home group overview counts are underlined text links that look like body text; Show more / See all are quiet buttons at the bottom of long groups. | Hard to discover that the page can jump to a group; long groups bury later groups (In progress, Waiting) far below. | `home-500-en-1440`, `home-1200-en-390-expanded` | V3-08 |
| B19 | P2 | all · all | Repetitive card styling for unlike things: empty state, add form, list groups, item summary, invite panel, history entries all use rounded bordered paper boxes with similar padding. | No visual difference between "a thing you act on", "a form", and "information". | `home-empty-en-1440-fold`, `audit-item-detail-en-1440`, `audit-members-invite-en-1440` | V3-06 |
| B23 | P2 | `/invite#token` · 390 · en · signed out | The screen says "You've been invited to Tendo" but does not name the household or the inviter before asking for credentials. | The recipient has no visible context to confirm which household they are joining. Note: no invitation-preview API exists; V3-12/V3-13 may only improve copy unless a separately justified API change is approved (V3_SCOPE excludes API rewrites for decoration). | `audit-invite-accept-en-390` | V3-12, V3-13 |
| B20 | P3 | `/` · all · en/cs | Home H1 is the household name; there is no date or "today" anchor. | Mild; household name is warm but does not orient a daily check-in. | `home-30-en-390-fold` | V3-08 |
| B21 | P3 | unknown path · all | Unknown URLs silently show Home. | Rare; acceptable, but a typo in a shared link gives no feedback. | `App.routing.test.tsx:38-50` | V3-13 |
| B22 | P3 | footer · all | Language switch only in the footer, far below long lists. | Discoverable once; acceptable if Account also offers it. | all full-page captures | V3-14 |

Confirmed strengths to keep: calm warm palette and serif display identity; no overflow at 320–1440 px on any
captured route (metrics `overflow=false` for all 40 Home stages and all route captures); 44 px targets on
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

- The capture harness does not yet record: dark mode (absent), 360 and 1024 px widths, long notes, a list
  with many different responsible members (the API has one `responsibleUserId` per item, so "multiple
  responsible people" means across rows, not per item), members-view as non-owner, OIDC/login variants in Czech at all widths,
  first-run error state, item edit conflict, offline/error states. V3-02 extends captures where the harness can
  observe them deterministically; remaining states are already asserted functionally by journey/unit tests
  listed above.
- No novice journey step/interaction counts or timings exist yet; V3-02 owns them.
- No committed screenshots exist for V3 comparison; V3-02 commits the curated baseline set.
