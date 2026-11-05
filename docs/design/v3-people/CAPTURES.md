# People and things capture index

All captures come from the production-build UX audit (`frontend/e2e/ux-audit.spec.ts` with `frontend/e2e/audit/people.ts`), run through `scripts/e2e-smoke.sh` with the pinned browser and toolchain and the test PostgreSQL.
- The populated, empty, conflict, archive and 1,000+ subject states use real API and database state.
- The error and loading states are stubbed in the browser.
- The before captures come from detached `origin/main` at `7ffa007`, the base of the V3-11 branch.
- The after set comes from `feat/v3-11-people-things`.
- No human usability testing is claimed.

Commands:

```sh
source ~/.config/tendo-test/env
git worktree add --detach /tmp/opencode/v311-before 7ffa007
TENDO_UX_AUDIT_DIR=/tmp/opencode/v311-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v311-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR=/tmp/opencode/v311/audit7 bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

Dimensions were verified from the PNG headers with `file`. "Viewport" means a viewport screenshot, so the fixed phone bar appears where users see it. "Full page" means a full-document screenshot; in those, the phone bar is stitched mid-page and does not overlap real content. "Scrolled viewport" captures assert that the framed element is in the viewport and that there is no horizontal overflow.

## Before / after

| Before | After | Content |
|---|---|---|
| `before/audit-people-things-en-320.png` (320×2051, full page) | `after/audit-people-en-320.png` (320×640, viewport) | English 320px: heading, full-width Add, first tiles above the bar |
| `before/audit-people-things-en-390.png` (390×1419, full page) | `after/audit-people-en-390.png` (390×844, viewport) | English phone list with See items and Edit |
| `before/audit-people-things-en-768.png` (768×1363, full page) | `after/audit-people-en-768.png` (768×1024, viewport) | Tablet, single column, Add beside the heading |
| `before/audit-people-things-en-1024.png` (1024×1319, full page) | `after/audit-people-en-1024.png` (1024×1024, viewport) | Two columns, actions stacked under the details |
| `before/audit-people-things-en-1440.png` (1440×1319, full page) | `after/audit-people-en-1440.png` (1440×900, viewport) | Desktop two-column tiles, inline actions, long-name tiles |
| `before/audit-people-things-cs-390.png` (390×1995, full page) | `after/audit-people-cs-390.png` (390×844, viewport) | Czech phone list |
| `before/audit-people-things-cs-1440.png` (1440×1319, full page) | `after/audit-people-cs-1440.png` (1440×900, viewport) | Czech desktop list |

## After only

| File | Type | Content |
|---|---|---|
| `after/audit-people-cs-320.png` (320×640) | viewport | Czech 320px fold |
| `after/audit-people-cs-320-long-names.png` (320×640) | scrolled viewport | Long Czech person (with linked account) and place names wrap inside tiles; Show archived clears the bar |
| `after/audit-people-cs-1024-long-names.png` (1024×1024) | scrolled viewport | Czech long names in the two-column range with actions stacked |
| `after/audit-people-empty-en-390.png` (390×844), `after/audit-people-empty-cs-390.png` (390×844), `after/audit-people-empty-en-1440.png` (1440×900) | viewport | Fresh household: shared EmptyState |
| `after/audit-people-add-en-390.png` (390×844) | viewport | Add sheet, Name focused (asserted) |
| `after/audit-people-add-validation-en-390.png` (390×844) | viewport | Empty-name validation inside the dialog |
| `after/audit-people-edit-linked-en-390.png` (390×844), `after/audit-people-edit-linked-en-1440.png` (1440×900) | viewport | Edit dialog for a long, linked person: wrapped title, visible Close, Archive apart from Save/Cancel |
| `after/audit-people-edit-cs-390.png` (390×844) | viewport | Czech Edit dialog with a long place name |
| `after/audit-people-conflict-en-390.png` (390×844) | viewport | 412 conflict: latest server name with explanation |
| `after/audit-people-archived-undo-en-390.png` (390×844) | viewport | Archived from the dialog; notice with Undo |
| `after/audit-people-archived-view-en-390.png` (390×844), `after/audit-people-archived-view-cs-390.png` (390×844), `after/audit-people-archived-view-en-1440.png` (1440×900) | viewport | Archived view with Restore beside each row |
| `after/audit-people-error-en-390.png` (390×844), `after/audit-people-error-en-1440.png` (1440×900) | viewport | Browser-stubbed 503: error Notice with Try again |
| `after/audit-people-loading-en-390.png` (390×844) | viewport | Browser-held list request: loading status |
| `after/audit-people-forced-dark-en-390.png` (390×844), `after/audit-people-forced-dark-en-1440.png` (1440×900), `after/audit-people-edit-forced-dark-en-390.png` (390×844) | viewport | Forced-dark readiness (V3-14 owns theme preference) |
| `after/audit-people-1000-page2-en-1440.png` (1440×900) | viewport | 1,014 active subjects, page 2 |
| `after/audit-people-1000-page2-pager-en-390.png` (390×844) | scrolled viewport | Page 2 pager: First page, Page 2, Next page |
| `after/audit-people-1000-last-page-pager-en-1440.png` (1440×900) | scrolled viewport | Last page (21): Previous page, no Next |

## Audit metrics (`metrics.json`, `stages.people`)

| Metric | Value |
|---|---|
| End-of-scroll clearance above the fixed phone bar | 168px (en 320), 168px (cs 320), 167.75px (en 390), 104.25px (en 768); bar is not fixed at 1024+ |
| Active subjects / pages walked | 1,014 / 21 |
| Max rows per page / max DOM elements | 50 / 487 |
| Browser list requests during the walk | 20 (one per Next, each `limit=50` with a cursor) |
