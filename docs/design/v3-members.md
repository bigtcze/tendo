# V3 Members, invitations and household access (V3-12)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Members | Two compact raised rows per desktop/tablet measure from 640px; one column on phones. Put the role in a quiet, text-labelled chip and mark the signed-in person inline. Explain owner/member responsibilities once above the list. | Gives the wide shell useful density without long empty rows; keeps role meaning accessible without color. |
| Member help | A distinct, calm section tells members to ask a household owner to send the link. | Answers “who can help?” instead of merely blocking the action. |
| Invite creation | Keep one primary action, explain the link is one-use and shown once before creating, then show the URL with Copy and manual-select behavior. Keep Check again for uncertain creation. | The one-time nature is a decision users need before they create, not after. Existing replay semantics remain unchanged. |
| Invitation history | Pending invitations use a stronger border and ink title; accepted/revoked/expired history is subdued on the shared sunken surface with readable ink. | Pending is actionable; history remains available without competing for attention. Status remains a visible word; axe caught and helped correct insufficient muted-text contrast on the original history surface. |
| Acceptance | Add a short explanation of what account creation or joining does next; no household name is implied because the API has no invitation preview. | Builds trust without inventing data or adding an API. |
| Surfaces | Shared role tokens and EmptyState/Notice/Button primitives; reduced card framing for unlike things. | Preserve design-system contrast and avoid another wall of equal-weight boxes. |

## Screenshot critique

The production-build captures show the intended change clearly: desktop uses a balanced two-column member list rather than long, empty full-width rows. On phones, the role key consumes a few extra lines; it is useful context, but the start of the invite section falls below the first fold at 320px. At 390px the primary action fits, but history begins below the fixed navigation fold. This is a reasonable trade-off for a less cramped reading rhythm, though the extra explanation is not compact. Czech wraps naturally, and the 390px non-owner screen names both the permission boundary and a person to ask without alarming language.

The one-time link is accompanied by specific sharing guidance. The dynamic success/manual-copy notice appears above the member list and pushes the current context down; in the manual-copy capture, the primary action reaches the fixed navigation edge. The link field is intentionally masked in captured evidence because it contains a bearer secret. Clipboard permission is unavailable in the production audit browser; evidence captures the verified manual-select fallback, not a fabricated success. The revoke-confirm capture shares that fallback notice, so the confirmation controls themselves are validated by the UI audit assertion and covered in the existing Vitest journey rather than claimed visible in the image. The error view is direct and retryable, though naturally sparse. Dark forced-readiness captures follow the shared tokens. No horizontal overflow was seen in the named viewport captures. Screenshot review is not human usability testing.

## Accessibility

- Member and invitation paging remain independent and named; list queries are scoped to the screen.
- Roles are visible words, not color-only signals. Member help is a labelled section.
- Existing invite copy/manual-select, Check again replay, revoke confirmation and focus restoration remain behaviorally covered by the existing suites.
- The opt-in audit asserts actual viewport width and no horizontal overflow before captures; axe checks run on owner and non-owner 390px states. Existing journey checks continue to cover invite acceptance.

## Evidence

Capture paths, before/after sets, scenario notes and one-line critique are indexed in [v3-members/CAPTURES.md](v3-members/CAPTURES.md). Production captures use the opt-in UX audit with real API/database state for populated households; transport failures are browser-stubbed. One-time invite links are masked in evidence. Dark captures force the existing token theme for readiness only; V3-14 owns saved theme preference.

## Not done here

- No invitation-preview API or household-name claim on the acceptance screen.
- No backend/API/schema changes or new dependencies.
- No human usability testing is claimed.
