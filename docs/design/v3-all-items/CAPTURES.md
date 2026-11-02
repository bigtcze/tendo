# All items capture index

Production-build UX audit (`frontend/e2e/ux-audit.spec.ts`) through `scripts/e2e-smoke.sh`, pinned browser/toolchain, real test API/database for the staged household. Before captures are from detached `origin/main` at `cc74ce9`; dimensions are labeled per file because the baseline helper emitted full pages. The curated after set uses viewport captures from `feat/v3-09-all-items`. The audit also emits full-page route/state artifacts; this index identifies the curated All items files and whether they are fold or full-page. No human testing is claimed.

Commands:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/v309-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v309-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-all-items/after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

| Before | After | Capture type / purpose |
|---|---|---|
| `before/audit-all-items-en-320.png` | `after/audit-all-items-en-320.png` | Before full page; after viewport fold 320×640; title and focus target clear bottom navigation |
| `before/audit-all-items-en-390.png` | `after/audit-all-items-en-390.png` | Before full page; after viewport fold 390×844; full first row above phone navigation |
| — | `after/audit-all-items-en-360.png` | Viewport fold, intermediate phone width |
| — | `after/audit-all-items-filters-expanded-390.png` | Viewport fold, secondary filter disclosure open |
| — | `after/audit-all-items-filtered-count-390.png` | Viewport fold, active secondary-filter count and visible Clear |
| `before/audit-all-items-en-768.png` | `after/audit-all-items-en-768.png` | Before full page; after viewport fold, tablet |
| `before/audit-all-items-en-1024.png` | `after/audit-all-items-en-1024.png` | Before full page; after viewport fold, desktop breakpoint |
| `before/audit-all-items-en-1440.png` | `after/audit-all-items-en-1440.png` | Before full page; after viewport fold, wide desktop |
| `before/audit-all-items-cs-390.png` | `after/audit-all-items-cs-390.png` | Full-page pair: before 390×4642; after 390×4048, Czech phone |
| `before/audit-all-items-cs-1440.png` | `after/audit-all-items-cs-1440.png` | Full-page pair: before 1440×4127; after 1440×3323, Czech desktop |
| — | `after/audit-all-items-dark-{390,1440}.png` | Viewport fold, forced-dark token readiness |
| — | `after/audit-all-items-page2-390.png` | Viewport 390×844, scrolled to page-2 pager; Previous and Next are visible |
| — | `after/audit-all-items-page2-1440.png` | Viewport 1440×900, scrolled to page-2 pager; Previous and Next are visible |
| — | `after/audit-all-items-error-{390,1440}.png` | Viewport fold, server failure and Retry |
| `before/audit-all-items-done-en-390.png` | `after/audit-all-items-done-en-390.png` | Before full page; after viewport fold, filtered status state |
| `before/audit-all-items-nomatch-en-390.png` | `after/audit-all-items-nomatch-en-390.png` | Before full page; after viewport fold, no-match state / clear recovery |
| — | `after/audit-all-items-search-unaccented-en-{390,1440}.png` | Viewport fold; ASCII query searches seeded Czech records containing accents |
| — | `after/audit-all-items-search-accented-en-{390,1440}.png` | Viewport fold; accented query comparison |
| — | `after/audit-all-items-empty-{390,1440}.png` | Browser render with stubbed empty response; not real-API empty-household evidence |
| — | `after/metrics.json` | Scripted route, viewport, overflow, target-size, and DOM metrics |

Empty state limitation: the audit household has a large staged backlog and is shared with the scale journey. The empty captures deliberately stub summary/list responses in the browser; they prove the frontend presentation only, not an empty real API household. The real API failure/retry captures likewise use a route stub. The curated page-2 images scroll to the pager before viewport capture; the audit asserts the pager and both controls are in view. The original baseline pair used for English captures is full-page, while the additional Czech before captures were re-run as viewport folds. Do not infer matching dimensions from pair names; use the labeled capture type.
