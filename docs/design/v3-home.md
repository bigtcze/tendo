# V3 Home: attention and household tasks (V3-08)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Layout | One stack at every width: Needs attention first, then In progress, Waiting, Coming up, Paused. No desktop columns. | Order alone carries priority, and no column is left empty beside a longer one at any data size. |
| Measure | All Home content (notices, overview, Add, groups) is bounded to `max-w-3xl` (48rem) inside the signed-in shell; rows can shrink and wrap. | Keeps title and Done close together and avoids the previous wide white panel. |
| Group navigation | Two or more populated groups produce quiet sand pill links with a muted count and 44px hit area, in one horizontally scrollable row without scroll snapping. A 4px inset on every side keeps each focus outline unclipped. Links focus existing group headings; fewer than two groups hides the overview. | Keeps the in-page navigation contract without competing with Add. Snapping would pull a focused chip back to a clipped position. |
| Add | One stable Add item button sits before the groups. On empty Home the same button sits directly above the EmptyState message, and EmptyState does not contain it. | One primary action, a clear next step, and focus that stays on the same button whether Home is empty or populated. |
| Date | Attention dates omit the year when it matches the server's `evaluatedOn` year; cross-year dates include it. Formatting stays locale-aware and date-only values remain UTC-stable. | Dates scan faster without a relative phrase that can read unnaturally in sentence contexts. |
| Row | Strong, non-underlined title; one visible supporting scan line (subject, attention date, responsible person) plus a visible secondary line for last done and repeat mode when present. | Preserve subject/assignment information in both the visual and accessibility trees without hiding recurrence/history. |
| Done | Keep the visible labelled Done button and title-inclusive accessible name, minimum 44px target, `data-action="done"`, and existing completion/Undo/focus lifecycle. | The meaning is immediately recognizable and interaction remains one tap. |
| States | Decorative skeletons paired with real status text; retry uses the shared Notice primitive; empty state uses EmptyState. | Reuses V3-06 semantic primitives and keeps feedback actionable. |

## Row anatomy

1. Title is the dominant element, uses the row-title token, wraps long Czech text anywhere, and remains a normal navigation link with a visible focus ring.
2. Supporting line is plain visible text: subject · attention phrase/date · responsible member when known. No subject or assignee is removed from assistive technology.
3. Secondary information remains visible below: last completed date and recurrence cadence/mode. No hover-only content.
4. Done is a text button with the unchanged localized accessible name. Completion notices and Undo remain owned by the existing completion flow.

The current item schema represents one `responsibleUserId`; the Home view displays the resolved member or the existing unknown-member copy and does not invent multiple assignments.

## Desktop composition

Home uses one stack at every width. Groups keep their fixed order, and all Home content shares the `max-w-3xl` reading measure. An earlier two-column desktop variant left a large empty area beside the long secondary column at the 1,200-item stage, so it was removed. With one stack, the empty area is a stable right margin that does not depend on how many rows each group has loaded. The n11 fixture's complete counts, initial 25 rows, request bounds, and Show more behavior remain unchanged. Group caps, Show more, See all, and overview focus behavior are retained. The UX audit asserts that the stack is at most 48rem wide and that every group starts at the same left edge.

Add is one stable button in one stable wrapper once the initial Home read completes, positioned immediately before the empty-state message when Home is empty and before the groups otherwise. EmptyState renders only its explanatory text and does not own the Add control. A refresh keeps the action mounted even while loading; the wrapper is hidden only while the add form is open, preserving the established return-focus reference.

## Screenshot critique

At 1440px, all groups stack in one 48rem column beside the navigation rail. The extra width is a calm right margin, not an empty column beside content. The expanded 1,200-item light and forced-dark captures show the same single stack. Done stays beside its flexible text column, and metadata clears the title focus outline. At 390px, the action remains right-aligned instead of wrapping below the row. In the 320px Czech view, the action stays beside the title while long metadata wraps. The account controls wrap, and bottom navigation still crowds the fold. The overview is one horizontally scrollable chip row with focus-driven horizontal reveal and a subtle right fade. The audit tabs through every chip at 390px and 1440px and asserts that each focus outline stays inside the nav. The 320px n15 and audit overflow checks pass. In forced dark, the overview's right-edge fade shows as a faint box against the dark page. This is a known dark-readiness item for #335. Captures are blurred and un-hovered before each screenshot. They are scripted evidence only; no household user testing is claimed.

## States and behavior

- Empty: the one stable Add item button directly above the EmptyState message (which owns no action); no second Add is rendered.
- Loading: skeletons are decorative and hidden from assistive technology; a `role=status` label remains available.
- Error: Notice tone=error with retry; rows remain absent on initial failed group load as before.
- Populated: counts remain server-summary counts; home queries and summary-count behavior are unchanged.
- Completion: Done and Undo behavior, NoticeRegion focus, uncertain retry, and focus fallback remain in existing hooks.
- Attention dates: same-year dates use locale short month/day; cross-year dates include year. The server summary's `evaluatedOn` is the reference year, not a client clock.

## Dark readiness

Home group surfaces use `bg-raised`, borders use `line`, text uses `ink`/`muted`, and notices/skeletons/empty state use semantic V3 tokens. The forced-dark evidence is a readiness check only; V3-14 still owns the theme preference and broader dark-mode behavior.

## Evidence

Production-build, real API/PostgreSQL UX audit is driven by `frontend/e2e/ux-audit.spec.ts` and `scripts/e2e-smoke.sh`. The capture command sets `TENDO_UX_AUDIT_DIR`, uses the pinned toolchain, and applies viewport assertions before each screenshot. See [`v3-home/CAPTURES.md`](v3-home/CAPTURES.md) for curated before/after pairs and the capture index.

## Not done here

- All items search, filters, paging and 1,000+ list design belong to #330.
- Detail layout and detail-specific Done/Undo integration belong to #331.
- Theme preference, OS-following behavior, persistence and full dark-theme review belong to #335.
- No real household usability test is claimed; captures are scripted evidence, not user approval.
