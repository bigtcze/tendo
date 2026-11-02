# V3 Home: attention and household tasks (V3-08)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Layout | Single column below `lg`; at `lg`, Needs attention is the wider primary column and the other groups share an independent secondary column. If Needs attention has no loaded rows, all populated secondary groups use the full content width. | Keeps the first useful work prominent without shared grid rows or an empty primary column. |
| Measure | Home follows the signed-in shell's max-width; each group and row can shrink and wrap rather than spreading title/action to opposite page edges. | Avoids the previous wide white panel and preserves readable context. |
| Group navigation | Two or more populated groups produce quiet sand pill links with a muted count and 44px hit area. Links focus existing group headings; fewer than two groups hides the overview. | Preserves the in-page navigation contract without competing with Add. |
| Add | The primary Add item action sits before groups, close to the list; empty Home has one obvious Add action inside the empty-state primitive. | One primary action and a clear next step. |
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

At 1024px and wider, when Needs attention exists, the group layout uses independent columns (wider attention / narrower secondary) with `items-start`; there is no shared row alignment. The second column contains In progress, Waiting, Coming up, and Paused in their existing order. With no attention rows, the secondary column becomes the only column. At phone/tablet widths the same order becomes one vertical stack. Group caps, Show more, See all, and overview focus behavior are retained.

## Screenshot critique

The refreshed 1440px capture keeps independent attention and secondary columns, with Done beside the flexible text column and metadata clear of the title focus outline. At 390px, Done remains consistently right-aligned rather than wrapping below row text. In the 320px Czech view, the action stays beside the title even when longer row details wrap; the shell account controls wrap and bottom navigation crowds the fold. Captures are scripted evidence only; no household user testing is claimed.

## States and behavior

- Empty: EmptyState text plus one primary Add item action; the duplicate list-level Add is suppressed.
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
