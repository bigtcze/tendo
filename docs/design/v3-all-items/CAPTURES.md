# All items capture index

Production-build UX audit (`frontend/e2e/ux-audit.spec.ts`) through `scripts/e2e-smoke.sh`, pinned browser/toolchain. The All items backlog, separately created empty household, and item details use real test API/database data; the transport-error state is browser-stubbed. Before captures are from detached `origin/main` at `cc74ce9`; dimensions and capture type are labeled per file. Curated after captures are generated from `feat/v3-09-all-items-r4`. The audit also emits other route/state artifacts; this index lists every curated All items PNG. No human testing is claimed.

Commands:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/v309-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v309-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-all-items/after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

| Before | After | Capture dimensions / visible content |
|---|---|---|
| `before/audit-all-items-en-320.png` | `after/audit-all-items-en-320.png` | Before full page 320×4923; after viewport 320×640, English list; Status and Filters use separate full-width rows |
| `before/audit-all-items-en-390.png` | `after/audit-all-items-en-390.png` | Before full page 390×4702; after viewport 390×844, full first row above phone navigation |
| — | `after/audit-all-items-en-360.png` | Viewport 360×900, English list; stacked Status and Filters |
| — | `after/audit-all-items-cs-320.png` | Viewport 320×640, Czech long selected Status and collapsed Filters |
| — | `after/audit-all-items-cs-320-active-filter.png` | Viewport 320×640, Czech Needs status, Filters (1), expanded Subject, and visible Clear |
| — | `after/audit-all-items-filters-expanded-390.png` | Viewport 390×844, expanded secondary filter controls |
| — | `after/audit-all-items-filtered-count-390.png` | Viewport 390×844, active secondary-filter count and Clear |
| `before/audit-all-items-en-768.png` | `after/audit-all-items-en-768.png` | Before full page 768×4275; after viewport 768×900, tablet layout |
| `before/audit-all-items-en-1024.png` | `after/audit-all-items-en-1024.png` | Before full page 1024×4151; after viewport 1024×900, desktop breakpoint |
| `before/audit-all-items-en-1440.png` | `after/audit-all-items-en-1440.png` | Before full page 1440×4127; after viewport 1440×900, desktop rail and results |
| `before/audit-all-items-cs-390.png` | `after/audit-all-items-cs-390.png` | Before full page 390×4642; after viewport 390×844, Czech list |
| `before/audit-all-items-cs-1440.png` | `after/audit-all-items-cs-1440.png` | Before full page 1440×4127; after viewport 1440×900, Czech desktop |
| — | `after/audit-all-items-dark-390.png` | Viewport 390×844, forced-dark list |
| — | `after/audit-all-items-dark-1440.png` | Viewport 1440×900, forced-dark desktop list |
| — | `after/audit-all-items-page2-390.png` | Viewport 390×844, page-2 pager scrolled into view; Previous and Next shown |
| — | `after/audit-all-items-page2-1440.png` | Viewport 1440×900, page-2 pager scrolled into view; Previous and Next shown |
| — | `after/audit-all-items-error-390.png` | Viewport 390×844, browser-stubbed error and Retry |
| — | `after/audit-all-items-error-1440.png` | Viewport 1440×900, browser-stubbed error and Retry |
| `before/audit-all-items-done-en-390.png` | `after/audit-all-items-done-en-390.png` | Before full page 390×4702; after viewport 390×844, Done results |
| `before/audit-all-items-nomatch-en-390.png` | `after/audit-all-items-nomatch-en-390.png` | Before full page 390×986; after viewport 390×844, no-match and Clear |
| — | `after/audit-all-items-search-unaccented-en-390.png` | Viewport 390×844, ASCII query returns accented Czech records |
| — | `after/audit-all-items-search-unaccented-en-1440.png` | Viewport 1440×900, same ASCII query at desktop width |
| — | `after/audit-all-items-search-accented-en-390.png` | Viewport 390×844, accented-query comparison |
| — | `after/audit-all-items-search-accented-en-1440.png` | Viewport 1440×900, accented-query comparison |
| — | `after/audit-all-items-empty-390.png` | Viewport 390×844, empty fresh household through real API |
| — | `after/audit-all-items-empty-1440.png` | Viewport 1440×900, same empty real-API household |
| — | `after/metrics.json` | Scripted routes, dimensions, overflow, targets, DOM size and journeys |

Empty-state evidence uses the audit owner's household created by the real first-run API setup, before backlog fixtures are seeded; its summary/list are real API responses with zero items. Error/retry is still a browser-stubbed presentation, not an offline API test. Page-2 captures scroll to the pager and assert both buttons are in view. English baseline screenshots are full-page; all curated after screenshots are viewport images. Czech before screenshots are full-page; Czech after screenshots are viewport images. Dimensions above are verified from the PNG headers.
