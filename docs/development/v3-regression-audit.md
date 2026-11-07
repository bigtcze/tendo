# V3 regression audit (V3-16, #337)

`comparison=v2.1.0 (44706a5) vs main (2db4347)` plus the fixes in the V3-16 PR. Requirement baseline: [V2.1 coverage matrix](v2-1-coverage-matrix.md). Visual suite and large-data method: [testing](testing.md#required-visual-regression-suite).

## Method

- Inventories come from two detached worktrees at the compared refs. Go: `go test -list . ./...` (compiles and lists without executing; 518 lines at v2.1.0, 515 at main) and sorted top-level `func Test…` names from all `*_test.go` files. Vitest and Playwright: sorted source titles of `describe`/`it`/`test` declarations (static grep; suites were not listed by the runner).
- For every removed or renamed title, the old body was read at v2.1.0 and its replacement at main. Classification: equal, stronger, weaker, or removed with a reason.
- Deleted deployment smoke scripts were compared assertion by assertion with their replacements.
- Assertion counts are grep proxies (Go `t.Error*`/`t.Fatal*` lines, frontend `expect(`). They show trends and are not evidence of coverage.

| Layer | v2.1.0 | main | delta |
|---|---:|---:|---:|
| Go top-level test functions | 515 | 510 | -5 (7 removed, 2 added) |
| Go assertion proxy lines | 2985 | 2967 | -18 |
| Vitest declaration titles | 597 | 687 | +90 |
| Vitest `expect(` | 2306 | 2731 | +425 |
| Playwright declaration titles | 55 | 69 | +14 |
| Playwright `expect(` | 1286 | 1619 | +333 |
| Go / Vitest / Playwright test files | 93 / 34 / 5 | 92 / 40 / 8 | -1 / +6 / +3 |

The PR adds a further Vitest file (`useHomeMembers.test.ts`) and the Playwright visual suite.

## Removed or renamed tests

| v2.1.0 test | Disposition |
|---|---|
| Go `TestCollectExcludesOnlyTestFilesAndTestdata`, `TestCollectsFunctionLiteralsWithParentNames`, `TestElseIfDoesNotIncreaseNesting`, `TestLabeledNestedControlCountsNesting`, `TestMetricsCountControlFlowAndReceiver`, `TestNestingCountsSwitchBodiesAndElseBlocks`, `TestRepeatedInitAndBlankVarLiteralIdentitiesAreUnique` | Removed with the bespoke `structcheck` tool, which golangci-lint (`gocyclo`, `gocognit`, `funlen`, `revive`) replaced in #367. These tests covered the deleted tool, not product behavior. The maintainability gate remains in place ([testing](testing.md)). |
| Vitest `a pending edit PATCH disables paging and still handles a 412 conflict` | Stronger: `SubjectsScreen.test.tsx` `a pending edit save disables archive and paging and still handles a 412 conflict` adds the archive-disabled assertion. |
| Vitest `explains where to change language … when sign-in linking is unavailable` | Changed with the V3-13 copy: the in-page language switch is now exercised in English and Czech (`Oidc.test.tsx`), including `aria-pressed`. The removed instructional sentence was intentional copy removal. |
| Vitest `focuses notice on mount instead of title` | Stronger: `ItemForm.test.tsx` `Dialog focuses the recovery notice instead of the title` covers the production dialog. |
| Vitest `renders Repeat and Fluid switches at least 24px square` | Stronger: `ItemsScreen.test.tsx` asserts 44 px switch targets, and Playwright checks the 24 px WCAG 2.5.8 targets across routes and themes. |
| Vitest `shows the welcome form with the detected time zone and no login form or switcher` | Equal or stronger: renamed `…with a readable detected time zone…`. It keeps all four `autocomplete` assertions and the IANA select value, and adds the readable label and option. |
| Playwright `e. deep link reload is served the app by the Go binary` | Changed intentionally: same 200 response for the deep path from Go. Unknown paths now show not-found with Home recovery (V3 shell) instead of rendering Home. |
| Playwright `n15. responsive …320, 360, 390, 768 and 1440 px…` | Stronger: adds 1024 px and keeps every route and assertion. |

## Deleted deployment smokes (#369 consolidation)

`runtime-smoke.sh` and `backup-restore-smoke.sh` were folded into `setup-smoke.sh` (runtime phase) and `household-backup-smoke.sh`. The deployment matrix went from 8 entries to 7 and added the v2.1.0 upgrade. Each deleted assertion was compared with its replacement:

| v2.1.0 check | Main | Result |
|---|---|---|
| Health contract, outage 503 / recovery, request ID, no-store, UID 65532, read-only root, no published PostgreSQL port, SIGTERM exit 0 within 15 s, redacted failure logs | `setup-smoke.sh` runtime phase | Equal |
| Dump non-empty, side-by-side restore, refused existing target with sentinel unchanged, truncated archive rejected as corrupt | `household-backup-smoke.sh` | Equal |
| Constraint, owner and role-privilege checks on a synthetic fixture | Full real-schema constraint/owner/ACL comparison across restore, plus `rolbypassrls` | Stronger |
| Restored runtime role: CHECK and FK reject invalid inserts, rejected rows do not persist | Missing after #369 | **Restored in this PR** against the real `items` CHECK (blank title) and composite subject FK, with an unchanged item count |
| Failed restore leaves no `items_search_text_trgm_idx` | Missing after #369 (other search objects still checked) | **Restored in this PR** |

## Journey n10 / n13

- `n10` (foreign household and resource access is indistinguishable from missing data) has the same title and assertions on main.
- `n13` (batched subject labels including archived references) was weakened in V3: an exact archived-label match became `toContainText` because the subject joined a `·`-separated scan line. **Restored in this PR**: the subject must now be the exact first segment of the row's scan line.

## Security preservation (main source evidence)

| Contract | Evidence |
|---|---|
| CSP, X-Frame-Options, nosniff, Referrer-Policy | `backend/internal/platform/webui/webui_test.go` `TestIndexFallback`, `TestAssetsAndStatic`; V3 adds `TestDocumentCSPIsExactlyUnchanged` and `TestThemeBootScriptServedAsSameOriginJavaScript` |
| Session and OIDC cookie attributes | `identity/httpapi/session_test.go` `TestLoginCookiePolicyFollowsPublicURL`, `TestDeleteSessionAlwaysClearsCookie`; `oidc_test.go` `TestOIDCStartCookiePolicy` |
| Origin checks | `platform/httpx/origin_test.go` `TestOriginMiddleware`; `TestOIDCStartOriginDuplicatesAndRateLimit`; live checks in `setup-smoke.sh` and `proxy-smoke.sh` |
| Cross-household isolation | Journey `n10` (unchanged); `TestUndoCompletionHouseholdIsolation`, `TestCreateCompletionHouseholdIsolation`, `TestInvitationHouseholdIsolationAndOwnerAuthorization` |
| No secrets in UI or logs | `TestOIDCCallbackRequestLogOmitsSecrets`, `TestMiddlewareRequestIDAndSafeLogging`; log scans in `oidc-smoke.sh` and `proxy-smoke.sh`. The documented best-effort redaction limit still applies. |

## Large data

The visual suite seeds 1,000 additional active items. On Home and All items at 390 and 1440 px, recorded runs show 262–270 DOM elements (bound `<3000`) and 6–7 initial API requests (bound `<=8`). Both are measured once the screen has settled: subject labels are resolved and the network is idle. Navigation-to-first-row (67–124 ms) and navigation-to-settled (567–619 ms) are logged locally and not thresholded. Method: [testing](testing.md#required-visual-regression-suite).

## Carried-over V2.1 follow-ups (#321)

| # | Disposition |
|---:|---|
| 1 Home members keyed by household | Fixed in code. This PR adds the missing direct regression test `useHomeMembers.test.ts`, which fails when the household key or generation guard is removed. |
| 2 Account reachable without household | Resolved; `App.test.tsx` no-household navigation test. |
| 3 Login OIDC duplicate click | Resolved; `Oidc.test.tsx` pending reactivation by click and Enter. |
| 4 OIDC checker callback/session correlation | Resolved in `oidc-smoke.sh` and `proxy_smoke_oidc.py` callback `Location` checks, which run in the required deployment matrix. |
| 5 `httpx.NewAppWithLogger` test-only export | No action: intentional test seam with no product defect. |
| 6 Home undo focus shared ref | Resolved (#312); `useCompletionFocus.ts`. |
| 7 Retry focus on activation | Resolved; `Oidc.test.tsx`, `ItemDetailScreen.test.tsx`. |
| 8 Household name Cf/Zl/Zp | Confirmed defect: `household.Validate` accepts U+200B-only and U+FEFF-only names. Tracked as managed Issue #389. |
| 9 One-off Docker metadata hang | No action: not reproduced. No retry-to-green. |

## Visual review findings

Every baseline was reviewed. Product observations go to managed Issues instead of this PR. The suite is not a design-change vehicle and no product CSS changed.

| Observation | Disposition |
|---|---|
| All items Filters chevron renders as a missing glyph | Defect, tracked as managed Issue #390 |
| `add-repeat-1440`, `edit-conflict-390`: the dialog body continues below the viewport | Not a defect. The dialog scrolls internally: the Add/Edit sheet is capped at `90dvh` from the `md` breakpoint and is full height on phones. A viewport capture shows only the visible part, and the remaining fields and actions are reachable by scrolling. |
| `detail-*-390` full-page captures: the bottom bar overlaps the History heading | Expected fixed-navigation behaviour, not a defect. The bar is fixed below `lg`, so content scrolls underneath it. A full-page capture freezes it over one mid-page position. `main` reserves `6rem` plus the safe area below the content, so the last row can be scrolled fully clear of the bar. |
| `onboarding-320`: the closed time-zone select truncates "Prague · Central European Time" | Accepted. A native select truncates long values. The helper text directly below shows the full readable zone. |

## Limits

- Assertion counts are proxies.
- No human assistive-technology test (G12) and no simultaneous two-browser completion E2E. Both residuals are unchanged from V2.1.
