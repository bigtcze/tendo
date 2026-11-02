# V3 All items: search, filters and cursor results (V3-09)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Desktop layout | At `lg`, a compact 18rem search/filter rail sits beside the flexible results list; it sticks near the top while scrolling. | Uses the shell's wide main area without stretching row text into an unreadable line or reserving an empty secondary results column. |
| Phone layout | Search and full-width Status remain visible; Subject, Responsible and Repeat filters sit behind a full-width accessible disclosure below Status. Its label reports the active secondary-filter count, and Clear stays available for active criteria and no-match states. | Long localized status values remain readable while secondary choices stay collapsed by default. |
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

On desktop, the sticky filter rail stays narrow, uses a single column with consistent field spacing, and results take the remaining width. On phones, search, full-width Status, and a full-width 44px Filters disclosure form a clear vertical sequence; secondary controls start collapsed. Its label reports active secondary filters. An active search or filter keeps one visible Clear action outside the panel, and a no-match state owns a single Clear action, including for status-only filtering. At 390×844 the complete first result row clears phone navigation; at 320×640 the first title link and focus outline clear the bar without shrinking the 44px link target. The disclosure uses `aria-expanded` and `aria-controls`; no filter depends on hover.

## Screenshot critique

The capture index records the actual PNG dimensions and viewport/full-page types. At 390×844 the complete first row clears bottom navigation; at 320×640 the first title link and focus target clear it. I inspected English and Czech viewport folds, including three distinct Czech 320px states: plain Active list, Needs status only with the first title above navigation, and Needs plus active Subject with Filters (1) and visible Clear. The long selected attention status fits the full-width control without clipping. Long Czech-title search captures at 390 and 1440 show the title inset from the card border and metadata separated below. Page-2 captures visibly include the pager and both controls. Empty-state captures use a fresh household created via real API setup before backlog seeding; error captures remain browser-stubbed. The ASCII query returns accented Czech records; the accented query is comparison evidence. No human usability testing is claimed.

## States and behavior

- Initial load retains a screen-level loading status and decorative/list-free state until current results settle.
- Empty household uses the shared EmptyState heading and existing Home action.
- No match uses the shared EmptyState heading and one contextual Clear action, including when the only criterion is a non-default status.
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
