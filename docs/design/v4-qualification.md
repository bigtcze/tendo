# V4-13 final qualification

status=in_progress (visual sign-off waits for #439)
issue=#422 (V4-13)
main_under_review=`d4d818d`
fixed_business_date=2026-05-14
references=`reference/tendo-v4-desktop.png` (SHA-256 `9fb1901b…`, 1448×1086), `reference/tendo-v4-mobile.png` (SHA-256 `40a1571f…`, 941×1672); both match the hashes in `.autonomous/V4_DESIGN_BRIEF.md`

This record covers the independent V4-13 reviews. The rules are in `.autonomous/V4_SCOPE.md`,
`.autonomous/V4_DEFINITION_OF_DONE.md` and `.autonomous/V4_DESIGN_BRIEF.md`; this file does not
repeat them.

## Evidence used

| Evidence | Source | Result |
| --- | --- | --- |
| Production UX audit (`TENDO_UX_AUDIT_DIR`, see [testing](../development/testing.md)) | production build of `d4d818d`, 523 PNG + `metrics.json` | exit 0; 495 audited routes; 0 horizontal overflow; 0 targets under 44 px; smallest visible text 10.88 px on 487 routes and 14 px on 8 |
| Committed visual baselines | `frontend/e2e/visual/visual.spec.ts-snapshots/` (140 production captures) | used for the reference matrix below |
| Large data | audit stages empty/1/30/100/500/1200 at 320–1440 px, EN/CZ | initial 1200-item Home (five groups, 25 rows each) renders 125 rows and 3076 DOM elements at every width, time to content 157–372 ms; 13 "Show more" clicks render up to 388 rows at 107–226 ms each; People walks 1016 active people in 21 pages of at most 50 rows |
| Compose startup smoke | `scripts/compose-startup-smoke.sh` | pass (127 s); now also asserts that the serving app keeps no administrator connection and serves through the restricted `tendo` role |
| Upgrade smoke from published v3.0.0 | `scripts/upgrade-smoke.sh` with `TENDO_UPGRADE_FROM=v3.0.0` | pass (25 s); table counts, migration ledger, response bodies and ETags preserved |
| Identity integration | `backend/internal/identity/postgres/oidc_jit_test.go` | new regression subtest: a changed provider email never overwrites the stored address on an existing identity; fails when the `email IS NULL` guard is removed |

The English 1200-item Home stages record 33 requests (the `requestCount` snapshot reads 26–31)
because the harness records the initial load plus the dark and System theme reloads it captures
for that stage (`frontend/e2e/audit/appearance.ts`). Each of the three loads uses the same 11
bounded paths (session, household, members, summary, one 25-item page per group, two batched
subject-label reads) with no per-item fetch; the Czech stages, which skip the theme reloads, report 12–18 (the 11 paths plus
a few aborted responses from the locale-switch navigation, recorded with 0 bytes).

The initial 1200-item Home DOM (3076 elements for 125 rows, about 25 per row) is bounded but
three times the V2 measurement (1091 for the same 125 rows in
[the V2 UX audit](../development/v2-ux-audit.md)), because V4 rows add the subject icon tile,
subject pill and stacked metadata. The enforced `<3000` Home bound
(`frontend/e2e/visual/largeData.ts`) is measured on the 1,000-item visual fixture, which passes;
the audit fixture fills five groups instead and slightly exceeds that number. Row-markup trimming
is tracked with the phone row density work in #439.

## Reference versus real UI

Real captures are the committed production baselines `home-reference-en-light-1440.png` and
`home-reference-en-light-390.png`, rendered from the reference fixture. Measurements are ink
bounding boxes (dark pixels in the region) and sampled pixel colours.

### Desktop (reference 1448 px, real 1440 px)

| Element | Reference | Real | Change |
| --- | --- | --- | --- |
| Top bar brand | 85×20 at x 70 | 88×15 at x 81 | same position, slightly lighter wordmark |
| Eyebrow "YOUR HOME · OVERVIEW" | 163×10 | 171×8 | 2 px smaller cap height; see #439 |
| Headline | 259×40 at x 272 | 249×39 at x 267 | matches size and weight |
| Supporting sentence | 368×15 | 383×15 | matches |
| Status pill row | 614 px wide from x 284 | 606 px wide from x 265 | matches; real pills carry an icon |
| Add item fill | `#00556c` | `#006d82` | V4-02 token, lighter petrol teal (contrast checked in V4-02) |
| Canvas | `#f0f2f8` (gradient) | `#f1f5fa` (flat) | flat cool canvas, deliberate in V4-02 |
| Layout | top bar, 220–230 px rail, one header band, grouped rows with right-hand date/assignee column | same composition, 224 px rail | matches |

### Phone (reference 941 px mock at roughly 2.4×, real 390 px)

| Element | Reference | Real | Change |
| --- | --- | --- | --- |
| Header | eyebrow, headline, sentence, full-width Add item | same order | matches |
| Status pills | horizontally scrolling pill row | same; real pills add an icon | matches |
| Group heading | dot, heading, count, "View all" | dot, heading, "4 items" count; "View all" only when a group overflows (V4-03 deviation) | documented in [v4-home.md](v4-home.md) |
| Row | title on one line, subject pill and responsible on one line, check at right | title wraps at 390 px, subject pill and each fact on its own line; attention text such as "since May 10" can break inside the date | rows are about twice as tall; see #439 |

**Reviewer conclusion:** the real Home reproduces the key composition and visual character of
both references at desktop and phone. Phone rows are denser in information and taller than the
reference; this is a should-fix tracked in #439, not a major intentional deviation, so no human
approval is requested.

## Findings and dispositions

| Finding | Severity | Disposition |
| --- | --- | --- |
| Design-system doc claimed 14 px minimum text; audit measures a 10.88 px eyebrow and the top-bar tagline is 11.2 px | should-fix | Doc corrected in this PR; both raised to at least 12 px in #439 |
| Phone row height and date wrapping | should-fix | #439 |
| Inconsistent detail action variants, invalid reset-link layout, 320 px bar labels, row alignment, Add item alignment, auth top bar, Account inset | should-fix / minor | #439 |
| Members/troubleshooting docs said an administrator could use Account to reset their own password and that Tendo had no password reset | should-fix (docs) | Fixed in this PR; documents the single-administrator recovery limit |
| Install guide did not point to the administrator-password tradeoff | should-fix (docs) | Fixed in this PR; links the tradeoff in `docs/admin/configuration.md`. V4-14 (#423) repeats it in the release notes |
| Healthcheck start period of 2 minutes could mark a long upgrade migration unhealthy | should-fix | Raised to 10 minutes in `compose.yaml`; ADR 0013 and install guide updated |
| No test proved an existing identity keeps its stored email when the provider address changes | should-fix (test) | Regression subtest added |
| Startup smoke did not prove the serving app drops its administrator connection | should-fix (test) | Assertion added |
| Migration 019/020 Down sections did not say they lose data | minor | Comments added |
| UX audit records smallest text and small targets but asserts only overflow | should-fix (test) | #439 adds the assertion together with the 12 px floor |

Known accepted risk, not a finding: whoever holds a reset link, including the administrator who
created it, can set the member's new password. The guide says so ("anyone who has the link can set
a new password") in [household members](../user/household-members.md).

No security, migration or deployment release blocker was found.

## Reviewer limits

- All reviews were independent AI subagent sessions; no human participants were observed and no
  human approval is claimed.
- Visual review inspected 58 production screenshots and 12 measured crops against both
  references, not every one of the 523 captures. Static images do not exercise screen readers,
  focus restoration, motion or 200/400 % zoom; those rely on the existing E2E gate.
- Security review used code reading and unit and integration tests; no penetration test or live
  OIDC provider was used.
- Deployment review used source reading and two local Compose smokes (startup, v3.0.0 upgrade);
  the remaining deployment smokes run in required CI.
