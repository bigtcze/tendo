# V3 People and things (V3-11)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Composition | Each person or thing is a raised tile: name, type, and the descriptive linked-account hint on the left, actions on the right. Phones and tablets use one column. At `lg`, tiles form two columns with actions stacked under the details. From `xl`, the actions return inline. | The baseline's single full-width list left wide desktop rows mostly empty. Czech names stay readable at 1024px, where two inline columns would have squeezed them into narrow, broken lines. |
| Actions | Each row offers a visible "See items" link (to All items filtered by that subject) and a secondary Edit button. Archive moved from the row into the Edit dialog. | The baseline gave Edit and a one-click Archive equal weight on every row. The new everyday action is finding a subject's items. Archive is rarer and is still reversible through Undo and Restore. |
| Add / Edit | Add and Edit open in the shared modal `Dialog` (a sheet on phones). Add focuses the Name field on open. While saving, the dialog cannot be dismissed, and focus returns to the control that opened it. Long titles wrap, and the header Close button keeps a 44px target. | One form pattern for create, rename, retype and link, with predictable focus. The page behind the dialog is inert. |
| Archive | Archive is a quiet action on the left of the Edit dialog footer, apart from Save/Cancel on the right. Success shows the existing notice with Undo. Failure keeps keyboard focus on the row's Edit button. | Neutral, not destructive-red: archiving is reversible. |
| Archived view | Rows show the details with Restore beside them, even on phones. "Back to the list" stays at the end. | Compact rows; Restore stays reachable and does not leave a half-empty tile. |
| Paging | Cursor pages of 50 replace the previous page. A named `nav` ("People and things pages") holds First/Previous, the polite page number, and Next, in the shared `Pagination` layout. | Matches All items. The list stays bounded for 1,000+ subjects. |
| States | Shared `EmptyState` for empty and empty-archive views. Load failures show an error `Notice` with Retry, announced through one persistent polite host. | Matches the sibling screens. One announcer, no duplicate or pre-populated live regions. |
| Heading | On phones, Add is a full-width primary button under the heading. From `md`, it aligns with the heading, and only then does the heading reserve space for it. | The baseline reserved that space at every width, which wrapped the phone heading early. |

## Screenshot critique

All captures are from the production build with the real API and database; error and loading states are stubbed in the browser. The 320/390px viewport folds show the heading, Add, and full first tiles above the bottom navigation. Measured end-of-scroll clearance between the last People control and the fixed phone bar is about 168px at 320/390px and 104px at 768px. Earlier full-page images showed the bar over content, but that was a stitching artifact, not real overlap.

Long Czech names wrap inside their tiles at 320px and at 1024px, where the two columns stack actions under the names, without horizontal overflow. In the Edit dialog, a long title wraps beside a fully visible Close button. The conflict capture shows the latest server version with the conflict explanation. The archive capture shows the Undo notice; the Undo-then-focus behavior is asserted, not only captured. The forced-dark captures use semantic tokens only (`bg-raised`, `line`, `ink`/`muted`, `Notice` tones).

Trade-off: phone tiles show fewer subjects per fold than the baseline's dense list (about 4 versus 8 at 390px). In exchange, every row has a visible "See items" path and 44px actions, and the one-tap Archive is no longer on every row. Paging keeps long lists bounded either way. No human usability testing is claimed.

## Scale evidence

The audit seeds 1,000 extra active subjects (1,014 active in total) and walks all 21 pages at 1440px. Results:
- At most 50 rows rendered per page.
- At most 486 DOM elements.
- Every active subject visited exactly once.
- 20 bounded list requests, one per Next, each `limit=50` with a cursor.
- No eager page walking.

## Accessibility

Behavior that is asserted in tests:
- Dialogs are labelled by their titles.
- Initial focus lands on Name in both Add and Edit.
- Escape and Close are refused while saving.
- Focus returns after Cancel, Save, Archive (to the notice), Undo (to the row), and a failed Archive (to the row).

Visual and structural facts:
- All row and dialog controls keep 44px targets.
- The pager is a named navigation landmark.
- Mutation notices use the existing focused status region. List-load failures use a separate persistent polite host, and the visible error `Notice` stays presentational.

## Evidence

The production-build UX audit is driven by `frontend/e2e/ux-audit.spec.ts` and `frontend/e2e/audit/people.ts` through `scripts/e2e-smoke.sh`, using the pinned browser and the real test API and database. Curated captures and commands are in [`v3-people/CAPTURES.md`](v3-people/CAPTURES.md).

## Not done here

- Theme preference and full dark-mode review belong to V3-14. The dark captures here are forced-dark readiness evidence only.
- No backend, API or product-scope change.
- No real household usability test is claimed.
