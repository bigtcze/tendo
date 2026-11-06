# V3 Members, invitations and household access (V3-12)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Members | Two compact raised rows per desktop/tablet measure from 640px; one column on phones. Put the role in a quiet, text-labelled chip and mark the signed-in person inline. Explain owner/member responsibilities once above the list. | Gives the wide shell useful density without long empty rows; keeps role meaning accessible without color. |
| Member help | A distinct, calm section tells members to ask a household owner to send the link. | Answers “who can help?” instead of merely blocking the action. |
| Invite creation | Keep one primary action, explain the link is one-use and shown once before creating, then show the URL with Copy and manual-select behavior. Keep Check again for uncertain creation. | The one-time nature is a decision users need before they create, not after. Existing replay semantics remain unchanged. |
| Invitation history | Pending invitations use a stronger border and ink title; accepted/revoked/expired history sits on the explicit `bg-sand` sunken surface with muted metadata that passes AA. | Pending is actionable; history remains available without competing for attention. The browser-computed contrast check caught that the old `bg-sunken` class was not emitted by the configured token utility, leaving the row transparent. |
| Acceptance | Add a short explanation of what account creation or joining does next; no household name is implied because the API has no invitation preview. | Builds trust without inventing data or adding an API. |
| Surfaces | Shared role tokens and EmptyState/Notice/Button primitives; reduced card framing for unlike things. | Preserve design-system contrast and avoid another wall of equal-weight boxes. |

## Screenshot critique

The reviewed production captures show the intended desktop/tablet density: member rows sit in two columns and leave more room for the invite action. At 320px, role rows and the full invite action are visible above the fixed bottom navigation in English and Czech, and both role labels remain readable. The narrow layout is compact, with little breathing room; a separate scrolled 320px history capture shows accepted/pending rows, dates, and a revoke action rather than implying the entire page fits one viewport. By 360px the invite section feels more comfortable. At 390px the action is visible, while history remains a scroll-away section. The sparse member-only page communicates who to ask, but its lower whitespace is not especially purposeful.

Invitation history now has an explicit sunken surface. Browser-computed muted metadata contrast is 5.01:1 in light and 6.87:1 in forced dark, above the 4.5:1 AA target. The light/dark phone captures remain legible; dark is forced-token readiness only, not a saved preference test. Czech navigation/header are visibly denser because of longer labels, but fit without horizontal overflow.

The create flow explains one-use/private sharing before creation. Success and manual-copy notices sit near the top and shift the list down; the large link field is deliberately masked, so its magenta block is not product UI. In the 390px manual-copy view the notice and form make the fixed bar cross the history heading; this is a real visual crowding issue, while lower rows remain scrollable. The revoke confirmation now visibly shows its prompt and both actions without the stale clipboard notice; farther history sits below the fixed bar, which still overlays the remaining list during scrolling. Check again is visible and actionable, though its notice leaves only a sliver of history above the fixed bar; mobile vertical density/persistent-nav overlap remains a visual issue. The owner-required state says to ask an owner in plain language. Error and loading captures are correctly sparse and name the real states. Screenshot review is not human usability testing.

## Accessibility

- Member and invitation paging remain independent and named; list queries are scoped to the screen.
- Roles are visible words, not color-only signals. Member help is a labelled section.
- Existing invite copy/manual-select, Check again replay, revoke confirmation and focus restoration remain behaviorally covered by the existing suites.
- The opt-in audit asserts actual viewport width and no horizontal overflow before captures; axe checks run on owner and non-owner 390px states. The metadata contrast helper measures browser-computed foreground/background colors in light and forced-dark tokens. Existing journey checks continue to cover invite acceptance.

## Evidence

Capture paths, before/after sets, scenario notes and one-line critique are indexed in [v3-members/CAPTURES.md](v3-members/CAPTURES.md). Production captures use the opt-in UX audit with real API/database state for populated households; transport failures are browser-stubbed. One-time invite links are masked in evidence. Dark captures force the existing token theme for readiness only; V3-14 owns saved theme preference.

## Not done here

- No invitation-preview API or household-name claim on the acceptance screen.
- No backend/API/schema changes or new dependencies.
- No human usability testing is claimed.
