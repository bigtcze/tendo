# V4-05 auth, household, settings and secondary states

> Historical record. It describes the V4 stage (released as `v4.0.0`) and is not the current UX or security contract. Current contracts: [UX](../../.autonomous/UX.md), [SECURITY](../../.autonomous/SECURITY.md).

status=V4-05_implemented
issue=#413 (V4-05)
direction=[`.autonomous/V4_DESIGN_BRIEF.md`](https://github.com/bigtcze/tendo/blob/v4.0.0/.autonomous/V4_DESIGN_BRIEF.md) (reference images are visual targets only)
tokens=`v4-design-system.md` (V4-02)
shell=`v4-home.md` (V4-03)

This completes the V4 visual system on every screen that is not a task screen: first-run setup, sign-in and
the OIDC option area, invitation acceptance, Account, Members and invitations, the full-page secondary
states, and the signed-in language placement. Behaviour, routing, API contracts, roles, accessible names
and focus ownership are unchanged.

## What changed

- **First-run setup** (`features/auth/OnboardingScreen.tsx`): the wide unframed two-column form is now a
  single grouped white surface. An eyebrow ("GETTING STARTED"), a 34–36 px headline with the accent period
  and a short supporting sentence lead into two hairline-divided steps (1 household, 2 administrator
  account). Each step keeps its existing heading, intro, field order, 14 px hints, timezone behaviour and
  described-by wiring. The teal primary action sits in the final action band; the form-level error uses the
  danger pair.
- **Sign-in** (`LoginScreen.tsx`, `LoginProviderOptions.tsx`): centred eyebrow, 36 px "Welcome back"
  headline, short subtitle, and one white surface for the password form. The login/OIDC error is an inline
  danger notice that keeps `role="alert"`, its `login-error` id and the password/provider `aria-describedby`
  links. Provider choices sit in a separated quiet band below a hairline; the provider control is the
  shared outlined secondary button. The error/retry branch keeps its status role and focus target.
- **Invitation acceptance** (`features/members/InviteScreen.tsx`): a narrow invitation column with an
  eyebrow, headline, intro, then a white surface for the join/create form. Focusable status notices keep
  their role and token-based colours; the incomplete-link state is the same panel composition. The one
  meaningful addition is the eyebrow ("INVITATION").
- **Account** (`AccountScreen.tsx`, `AccountPreferences.tsx`, `AccountOidcSectionView.tsx`,
  `AccountOidcLinkForm.tsx`): the mostly empty two-column layout is gone. A single compact settings column
  carries the eyebrow, headline, subtitle and three grouped white surfaces: your sign-in (avatar initial +
  the existing signed-in sentence), the existing OIDC link section, and labelled Appearance and Language
  rows. The rows put copy on the left and the control on the right from `sm`, stacked on phones. The OIDC
  section is restyled only; its headings, status roles, retry focus and link flow are unchanged.
- **Segmented choices** (`components/ui/segmented.tsx`, `theme/AppearanceControl.tsx`,
  `app/LanguageSwitch.tsx`): the underlined-text links are now a shared segmented control with a pale
  track, raised selected surface, 600 weight and a check glyph. Selection is conveyed by `aria-pressed`,
  the glyph and weight, not by colour alone. On phones each option is a full-width row and from `sm` the
  group becomes one horizontal row; the signed-out footer keeps the inline two-option form. Keyboard order
  and the accessible group names are unchanged.
- **Members** (`MembersScreen.tsx`, `MemberListSection.tsx`, `InvitePanel.tsx`, `InvitationRows.tsx`,
  `MembersPager.tsx`): the V3 card grid is a single white hairline-divided member list: avatar-initial
  tile, login with the existing "(you)" marker, and a role pill. The invite panel is a matching white
  grouped surface with the teal "Create invite link" action and the one-time link in a sunken share panel;
  invitation history is a separate grouped list with a semantic status pill (pending amber, accepted
  green, revoked/expired neutral), created/expires dates and a right-aligned outlined Revoke control; the
  confirmation keeps its focus choreography. The memberless explanation is a quiet sunken panel. Which
  invitations are shown is unchanged (expired filtering stays V4-12).
- **Secondary states** (`app/StatusScreens.tsx`): NotFound, Loading and Error now use the shared eyebrow +
  34–36 px headline + white surface composition instead of the V3 `border-l-2` accent rule and serif-era
  `font-display`. Not-found keeps the signed-in household link and the signed-out sign-in link; the error
  screen keeps its single Retry action. Member/invitation permission and request refusals continue to use
  the shared Notice with their existing roles and wording.
- **Language placement** (`app/Layout.tsx`): the signed-in footer is removed. Language lives on Account
  (already in the signed-in rail and phone bar) and in the signed-out/invite footer, where it is not
  orphaned under the fixed bottom bar. `Layout` therefore renders the footer only when there is no
  household navigation. Every signed-in path still reaches the language choice through Account; every
  signed-out path keeps the footer switch.

## Deliberate deviations

| Deviation | Reason |
| --- | --- |
| Language is not offered in the signed-in shell outside Account | The old footer duplicated Account and at 390 px rendered below the fixed bottom bar. Account is one tap away in both the rail and the bottom bar, so the choice stays reachable without a second competing control. |
| Account remains a settings column, not a two-column dashboard | The old right column was mostly empty. A single 3xl column keeps the page compact and deliberate at 1440 and stacks naturally on phones. |
| The OIDC link section is restyled but not removed | Removing it is V4-11; V4-05 is visual only and must not change its behaviour. |
| The invite action stays auto-width on desktop and full-width on phones | The existing Czech wrap behaviour and test contract (`w-full` + `whitespace-normal`) are preserved. |
| Segmented labels keep their existing wording | Shortening "Match this device" would change copy semantics; below `sm` every option becomes a full-width row, and from `sm` the group is one horizontal row. |
| At phone widths the member role pill sits under the login | A long login such as `novak_owner` must stay readable without mid-word breaks; from `sm` the role returns to the right of the login in the same row. |
| Members keeps exactly the invitation rows the API returns | Hide-expired is V4-12; adding filters here would change behaviour. |
| No role-change control was added | The Admin center is V4-07. |

## Reference-fidelity checklist (a)–(g)

| Check | Result | Evidence |
| --- | --- | --- |
| (a) Font, optical weight, navy hierarchy | Pass: Inter Variable throughout; eyebrows uppercase 600, screen headings 700, row logins 600, body 400, hints 14 px in muted slate. No serif/`font-display` remains in the touched screens. | `onboarding-en-light-1440.png`, `members-owner-en-light-1440.png`, `account-en-light-390.png` |
| (b) Cool off-white canvas, white surfaces | Pass: `#F1F5FA` canvas, white grouped surfaces, 1 px `#D7E2EB` dividers; the invite share panel uses the sunken tint. | all evidence captures |
| (c) Petrol-teal CTA, coral/amber/teal status semantics | Pass: teal Create household/Sign in/Create invite link/Join; danger login and setup error pairs; pending amber, accepted green, revoked/expired neutral invitation pills; attention/toolbar states keep their tokens. | `login-error-en-light-390.png`, `members-owner-en-light-1440.png`, `members-en-dark-390.png` |
| (d) Spacing and compact grouped rows | Pass: 44 px controls, 64 px member rows, single-action bands, no isolated cards; Account rows are a labelled two-column layout from `sm`. | `members-owner-en-light-1440.png`, `account-en-light-1440.png` |
| (e) Navigation / bottom bar | Pass: the V4-03 navigation composition is untouched; selected Account/Members states remain icy blue. The signed-in footer is removed and the 640–1023 px main bottom padding was raised so focused rows still clear the fixed phone/tablet bar. | `members-owner-en-light-390.png`, `account-en-light-390.png` |
| (f) Icon/tile styling and focus | Pass: avatar-initial tiles, step-number tiles, check-marked selected segments, outlined secondary controls, visible 2 px focus rings, 44 px targets. | `onboarding-en-light-390.png`, `account-en-light-390.png`, `members-owner-en-light-390.png` |
| (g) Real data accuracy | Pass: captures use the real production build and PostgreSQL fixtures; member logins/roles, invitation statuses and dates are real API rows (dates pinned by the visual fixture route). No invented counts or labels. | `members-owner-en-light-1440.png`, `members-owner-cs-light-390.png` |

## Behaviour and test coverage

- Vitest: `StatusScreens.test.tsx` (eyebrow/heading/retry/status roles, no `border-l-2`), `App.test.tsx`
  (signed-in shell has no `contentinfo`, Account holds the Language group, signed-out footer keeps it),
  `AppearanceControl.test.tsx` (selected segment has `aria-pressed` plus a visible check, unselected does
  not), `MembersScreen.test.tsx` (member rows carry login, "(you)", role and an aria-hidden avatar
  initial), plus the existing members focus, invite, onboarding and OIDC suites.
- Playwright: the journey language steps now choose the locale on Account; the keyboard sweeps no longer
  require a signed-in footer, and n15b asserts the Account language group clears the fixed bottom bar.
  The optional UX audit checks the same placement at 390.
- Visual suite additions: onboarding validation error, signed-in and signed-out not-found, the real
  invitation acceptance screen, signed-out connection error, Account/Members at 320, and Czech Account and
  Members at 390. Existing captures are unchanged in intent.

## Evidence

All images are committed production-build Playwright captures from the visual suite (Chromium, fixed
business date 2026-05-14, real PostgreSQL) — not mockups or reference-derived composites.

| Capture | Purpose |
| --- | --- |
| [onboarding 1440](v4-auth-settings/onboarding-en-light-1440.png) | Setup, desktop grouped steps |
| [onboarding 390](v4-auth-settings/onboarding-en-light-390.png) | Setup, phone field rhythm |
| [login 1440](v4-auth-settings/login-en-light-1440.png) | Sign-in, desktop |
| [login 390](v4-auth-settings/login-en-light-390.png) | Sign-in, phone |
| [login error 1440](v4-auth-settings/login-error-en-light-1440.png) | Inline danger alert, focus links |
| [login error 390](v4-auth-settings/login-error-en-light-390.png) | Phone alert wrapping |
| [invite accept 1440](v4-auth-settings/invite-accept-en-light-1440.png) | Invitation acceptance |
| [invite accept 390](v4-auth-settings/invite-accept-en-light-390.png) | Invitation acceptance, phone |
| [account 1440](v4-auth-settings/account-en-light-1440.png) | Settings column and segmented controls |
| [account 390](v4-auth-settings/account-en-light-390.png) | Account, phone stack (viewport capture; the fixed bar covers the lower language option) |
| [account 390 scrolled to end](v4-auth-settings/account-scrolled-end-en-light-390.png) | Production UX-audit capture scrolled to the end: both language options sit above the fixed bottom bar |
| [members owner 1440](v4-auth-settings/members-owner-en-light-1440.png) | Member list, invite panel, invitation statuses |
| [members owner 390](v4-auth-settings/members-owner-en-light-390.png) | Members, phone |
| [members non-owner 1440](v4-auth-settings/members-non-owner-en-light-1440.png) | Member-only help state |
| [members non-owner 390](v4-auth-settings/members-non-owner-en-light-390.png) | Member-only help, phone |
| [not found signed in 1440](v4-auth-settings/not-found-signed-in-en-light-1440.png) | Secondary state with shell |
| [not found signed in 390](v4-auth-settings/not-found-signed-in-en-light-390.png) | Secondary state, phone |
| [not found signed out 390](v4-auth-settings/not-found-signed-out-en-light-390.png) | Signed-out recovery link |
| [error screen 1440](v4-auth-settings/error-screen-en-light-1440.png) | Connection failure/retry |
| [error screen 390](v4-auth-settings/error-screen-en-light-390.png) | Connection failure, phone |
| [account dark 1440](v4-auth-settings/account-en-dark-1440.png) | Dark settings surfaces and segments |
| [account dark 390](v4-auth-settings/account-en-dark-390.png) | Dark Account, phone |
| [members dark 1440](v4-auth-settings/members-en-dark-1440.png) | Dark member/invitation rows |
| [members dark 390](v4-auth-settings/members-en-dark-390.png) | Dark Members, phone |
| [login dark 1440](v4-auth-settings/login-en-dark-1440.png) | Dark sign-in |
| [login dark 390](v4-auth-settings/login-en-dark-390.png) | Dark sign-in, phone |
| [account CS 390](v4-auth-settings/account-cs-light-390.png) | Czech labels and diacritics |
| [members CS 390](v4-auth-settings/members-owner-cs-light-390.png) | Czech member/invitation labels |

Gated baselines live under `frontend/e2e/visual/visual.spec.ts-snapshots/`; the suite also holds the 320 px
Account/Members captures and the existing 768 px references, which are not duplicated here. Removing the
signed-in footer intentionally changed every signed-in full-page baseline (the removed switch and its
reserved footer space). Because that footer also added a scrollable tail, the shared main bottom padding at
640–1023 px was raised to the same 6 rem clearance used on phones, so focused rows cannot end under the
fixed bar; only that bottom spacing changed on Home/items/people/detail, not their content composition.
