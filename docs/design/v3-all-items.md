# V3 All items: search, filters and cursor results (V3-09)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Desktop layout | At `lg`, a compact 18rem search/filter rail sits beside the flexible results list; it sticks near the top while scrolling. | Uses the shell's wide main area without stretching row text into an unreadable line or reserving an empty secondary results column. |
| Phone layout | Search and status are first; the remaining filters follow in a compact two-column grid from `sm`, then results. All controls remain visible and reachable without an extra tap. | Search is the common entry point, status is the main scope, and the fold can still show the range/results on ordinary phone heights. |
| Search | Search input and explicit Search action remain together; input is visible at every width. | Predictable touch action and preserved form-submit, focus, validation, and live outcome behavior. |
| Filters | Status counts remain in the status choices. Subject, responsible person, and repeat use labeled native selects; clear appears only when filters are active. | Familiar controls, counts aid recognition, and clear is not a competing action when nothing is filtered. |
| Rows | Strong non-underlined title; one visible scan line combines subject, state/date, and responsible person; repeat text appears on a second line only when present. | Aligns with Home row anatomy while preserving all displayed content in the accessibility tree. |
| Paging | Shared named pagination controls and range status; each cursor page replaces the prior 25 rows. | Keeps 1,000+ backlogs bounded and preserves the existing cursor stack / exact back restoration. |
| States | Shared EmptyState for empty/no-match, Notice for stale/error, with Retry in the error notice. | Consistent recovery language and semantic tokens, including dark readiness. |

## Row anatomy

1. The item title is the dominant, normal text link with a visible focus ring and natural wrapping.
2. The scan line reads subject · state/attention date · responsible person when assigned. No metadata is hidden from assistive technology.
3. A recurrence summary occupies a supporting line only when the item repeats.
4. No hover-only facts or actions.

## Desktop and phone composition

On desktop, the sticky filter rail stays narrow and results take the remaining width. Rows have a clear reading measure while the overall page makes use of the signed-in shell width. At phone and tablet widths the layout becomes one stack; search and status are at the top, other filters are compactly arranged, and results follow immediately. The current mobile composition keeps all secondary filters expanded for direct access rather than hiding them behind an extra disclosure. On a very dense 1,000+ fixture, the first result falls below those controls, so the strict first-view goal remains an open concern to address without making filters difficult to discover. No filter depends on hover or a hidden panel.

## Screenshot critique

The before/after production captures are indexed in [`v3-all-items/CAPTURES.md`](v3-all-items/CAPTURES.md). Review the named viewport at each capture rather than assuming the filename proves dimensions. The target is a visibly wider results area on desktop, an uncramped search-first phone fold, readable Czech wrapping, and no horizontal overflow. Captures are scripted evidence only; no human usability testing is claimed.

## States and behavior

- Initial load retains a screen-level loading status and decorative/list-free state until current results settle.
- Empty household uses EmptyState and the existing Home action.
- No match uses EmptyState and clear search/filters.
- Request failure uses an error Notice and Retry; stale cursor recovery uses a neutral Notice.
- Settled outcomes stay announced through the persistent polite live region.
- Search and filters retain URL/back restoration and accent-insensitive API behavior.
- Cursor paging replaces rows, preserves previous cursors, and retains the 25-row DOM bound.

## Dark readiness

Surfaces use `bg-raised`, borders use `line`, text uses `ink`/`muted`, and errors use semantic Notice tones. No All items control or row uses a hard-coded white surface. Forced-dark captures are readiness evidence; V3-14 owns theme preference and broader dark-mode behavior.

## Evidence

The production-build UX audit is driven by `frontend/e2e/ux-audit.spec.ts` and `scripts/e2e-smoke.sh`; it uses the pinned browser and real test API/database. Focused capture pairs and scenarios are listed in [`v3-all-items/CAPTURES.md`](v3-all-items/CAPTURES.md).

## Not done here

- Theme preference, OS-following behavior and full dark-mode review belong to V3-14.
- No backend/API behavior or product scope changed.
- No real household usability test is claimed.
