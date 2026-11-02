# V3 All items: search, filters and cursor results (V3-09)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Desktop layout | At `lg`, a compact 18rem search/filter rail sits beside the flexible results list; it sticks near the top while scrolling. | Uses the shell's wide main area without stretching row text into an unreadable line or reserving an empty secondary results column. |
| Phone layout | Search and status remain visible; Subject, Responsible and Repeat filters sit behind an accessible disclosure. Its label reports the active secondary-filter count, and Clear stays visible when any filters or search are active. | Search stays prominent, status is the quick scope, and secondary choices do not push results below the first view. |
| Search | Search input and explicit Search action remain together; input is visible at every width. | Predictable touch action and preserved form-submit, focus, validation, and live outcome behavior. |
| Filters | Status counts remain in the status choice. Subject, responsible person, and repeat use labeled native selects; on phone they are progressively disclosed with active count. Clear stays reachable for active criteria. | Familiar controls, counts aid recognition, and secondary choices do not dominate the phone's first view. |
| Rows | Strong non-underlined title; one visible scan line combines subject, state/date, and responsible person; repeat text appears on a second line only when present. | Aligns with Home row anatomy while preserving all displayed content in the accessibility tree. |
| Paging | Named Previous/Next controls and a range heading; each cursor page replaces the prior 25 rows. | Keeps 1,000+ backlogs bounded and preserves the existing cursor stack / exact back restoration. |
| States | Shared EmptyState for empty/no-match, Notice for stale/error, with Retry in the error notice. | Consistent recovery language and semantic tokens, including dark readiness. |

## Row anatomy

1. The item title is the dominant, normal text link with a visible focus ring and natural wrapping.
2. The scan line reads subject · state/attention date · responsible person when assigned. No metadata is hidden from assistive technology.
3. A recurrence summary occupies a supporting line only when the item repeats.
4. No hover-only facts or actions.

## Desktop and phone composition

On desktop, the sticky filter rail stays narrow, uses a single column with consistent field spacing, and results take the remaining width. At phone and tablet widths, the search remains prominent; Status and a 44px Filters disclosure sit side by side, with secondary controls collapsed by default. Its label reports the active secondary-filter count; one Clear action remains visible when search or any filter is active, including while the disclosure is closed. The loaded first result appears above phone navigation at 390×844 and 320×640. An explicit search/status/secondary criterion keeps one visible Clear action outside the collapsed panel; the no-match state instead owns exactly one Clear action in its EmptyState. The disclosure uses `aria-expanded` and `aria-controls`; no filter depends on hover.

## Screenshot critique

The before/after captures and their actual dimensions and capture types are indexed in [`v3-all-items/CAPTURES.md`](v3-all-items/CAPTURES.md). At 390×844 the first full result row sits above bottom navigation; at 320×640 the first title link clears the bar, though its row can continue below it. Phone Status and Filters share a row, without overlapping the title or search label. I checked the title-to-metadata spacing and card inset in the single-line rows and long Czech-title search captures at phone and desktop widths; the title has inset from the card border and metadata follows with a small gap. Page-2 captures are viewport-scrolled to the named pager with both navigation buttons visible. Expanded and active-filter phone captures show disclosure state, count, and one visible Clear action. The unaccented ASCII query returns seeded accented Czech titles; the accented query is a separate comparison. Czech captures, empty/error presentation stubs, and their limitations are documented in the index. No human usability testing is claimed.

## States and behavior

- Initial load retains a screen-level loading status and decorative/list-free state until current results settle.
- Empty household uses the shared EmptyState heading and existing Home action.
- No match uses the shared EmptyState heading and one contextual Clear action; the filter-rail Clear is suppressed while the no-match action is shown.
- Request failure uses an error Notice and Retry; stale cursor recovery uses a neutral Notice.
- Settled outcomes stay announced through the persistent polite live region.
- Search and filters retain URL/back restoration and accent-insensitive API behavior.
- Cursor paging replaces rows, preserves previous cursors, and retains the 25-row DOM bound.

## Dark readiness

Surfaces use `bg-raised`, borders use `line`, text uses `ink`/`muted`, and errors use semantic Notice tones. The visible error notice is presentational and the persistent polite outcome region remains the screen's only error announcer. No All items control or row uses a hard-coded white surface. Forced-dark captures are readiness evidence; V3-14 owns theme preference and broader dark-mode behavior.

## Evidence

The production-build UX audit is driven by `frontend/e2e/ux-audit.spec.ts` and `scripts/e2e-smoke.sh`; it uses the pinned browser and real test API/database. Focused capture pairs and scenarios are listed in [`v3-all-items/CAPTURES.md`](v3-all-items/CAPTURES.md).

## Not done here

- Theme preference, OS-following behavior and full dark-mode review belong to V3-14.
- No backend/API behavior or product scope changed.
- No real household usability test is claimed.
