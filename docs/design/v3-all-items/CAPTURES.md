# All items capture index

Production-build UX audit (`frontend/e2e/ux-audit.spec.ts`) through `scripts/e2e-smoke.sh`, using the pinned browser/toolchain and test PostgreSQL. The staged backlog and the separate empty household use real API/database state; transport-error views are browser-stubbed. Before captures are from detached `origin/main` at `cc74ce9`. The curated after set is from `feat/v3-09-all-items-r4`. No human usability testing is claimed.

Commands:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/v309-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v309-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-all-items/after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

## Curated screenshots

All dimensions below were verified from PNG headers with `file` after the final capture run. “Viewport” means a viewport screenshot, including page-2 images scrolled to the pager; “full page” means the full document screenshot.

| Before | After | Exact capture type and content |
|---|---|---|
| `before/audit-all-items-en-320.png` (320×4923, full page) | `after/audit-all-items-en-320.png` (320×640, viewport) | English 320px list fold; Status and Filters occupy separate full-width rows, first title target clears navigation |
| — | `after/audit-all-items-en-360.png` (360×900, viewport) | English list at 360px; stacked controls and first results |
| `before/audit-all-items-en-390.png` (390×4702, full page) | `after/audit-all-items-en-390.png` (390×844, viewport) | English 390px list fold; full first result row above navigation |
| — | `after/audit-all-items-cs-320.png` (320×640, viewport) | Czech 320px list fold with long selected attention status |
| — | `after/audit-all-items-cs-320-active-filter.png` (320×640, viewport) | Czech 320px, Needs status, Filters (1), active secondary filter, and visible Clear; disclosure expanded |
| — | `after/audit-all-items-filters-expanded-390.png` (390×844, viewport) | English phone disclosure expanded |
| — | `after/audit-all-items-filtered-count-390.png` (390×844, viewport) | English phone with secondary filter count and visible Clear |
| `before/audit-all-items-en-768.png` (768×4275, full page) | `after/audit-all-items-en-768.png` (768×900, viewport) | English tablet list |
| `before/audit-all-items-en-1024.png` (1024×4151, full page) | `after/audit-all-items-en-1024.png` (1024×900, viewport) | English list at desktop breakpoint |
| `before/audit-all-items-en-1440.png` (1440×4127, full page) | `after/audit-all-items-en-1440.png` (1440×900, viewport) | English desktop rail and result list |
| `before/audit-all-items-cs-390.png` (390×4642, full page) | `after/audit-all-items-cs-390.png` (390×4103, viewport) | Czech phone list at 390px; after image is a viewport fold, not a full-page capture |
| `before/audit-all-items-cs-1440.png` (1440×4127, full page) | `after/audit-all-items-cs-1440.png` (1440×3315, viewport) | Czech desktop at 1440px; after image is a viewport fold, not a full-page capture |
| — | `after/audit-all-items-dark-390.png` (390×844, viewport) | Forced-dark phone list readiness |
| — | `after/audit-all-items-dark-1440.png` (1440×900, viewport) | Forced-dark desktop list readiness |
| — | `after/audit-all-items-page2-390.png` (390×844, viewport) | Page 2 scrolled to pager; Previous and Next are visible and asserted before capture |
| — | `after/audit-all-items-page2-1440.png` (1440×900, viewport) | Page 2 scrolled to pager; Previous and Next are visible and asserted before capture |
| — | `after/audit-all-items-error-390.png` (390×844, viewport) | Browser-stubbed error and Retry on phone |
| — | `after/audit-all-items-error-1440.png` (1440×900, viewport) | Browser-stubbed error and Retry on desktop |
| `before/audit-all-items-done-en-390.png` (390×4702, full page) | `after/audit-all-items-done-en-390.png` (390×844, viewport) | English Done results fold |
| `before/audit-all-items-nomatch-en-390.png` (390×986, full page) | `after/audit-all-items-nomatch-en-390.png` (390×844, viewport) | English no-match card with recovery Clear |
| — | `after/audit-all-items-search-unaccented-en-390.png` (390×844, viewport) | ASCII query returns matching Czech titles containing accents |
| — | `after/audit-all-items-search-unaccented-en-1440.png` (1440×900, viewport) | Same ASCII-query evidence at desktop width |
| — | `after/audit-all-items-search-accented-en-390.png` (390×844, viewport) | Accented-query comparison on phone |
| — | `after/audit-all-items-search-accented-en-1440.png` (1440×900, viewport) | Accented-query comparison on desktop |
| — | `after/audit-all-items-empty-390.png` (390×844, viewport) | Empty household created by real setup API before backlog seeding |
| — | `after/audit-all-items-empty-1440.png` (1440×900, viewport) | Same fresh real-API empty household at desktop width |

`after/metrics.json` contains the scripted route, viewport, overflow, target-size, DOM-size, and journey measurements. It is generated by the same audit and retained with this set.

## Provenance and limitations

The empty state is not a stub: the audit completes first-run setup through the API, verifies the newly created household is empty in All items, captures it at 390×844 and 1440×900, and only then seeds the household used for backlog journeys. The error state is intentionally route-fulfilled and demonstrates presentation/retry UI only; it is not evidence of a real offline API failure. Page-2 images are viewport captures after explicit pager visibility assertions. English baseline images are full-page while their after pairs are viewport folds; Czech before files are full-page while Czech after files are viewport folds. Pair filenames do not imply identical capture height or type.
