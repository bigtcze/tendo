# Members and invitations capture index

Production-build screenshots are produced by `frontend/e2e/ux-audit.spec.ts` and `frontend/e2e/audit/members.ts` through `scripts/e2e-smoke.sh`, with pinned Chromium and the real test API/database. Error states are browser-stubbed. Before captures are from detached `origin/main` at `96c33f1`; after captures are from `feat/v3-12-members`. Invite bearer links must be masked and must never appear in committed evidence. No human usability testing is claimed.

Commands (source the private test environment first):

```sh
source ~/.config/tendo-test/env
git worktree add --detach /tmp/opencode/v312-before 96c33f1
TENDO_UX_AUDIT_DIR=/tmp/opencode/v312-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v312-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-members/after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

## Captures and critique

Dimensions are viewport captures and asserted before each shot.

| File | What it shows / honest critique |
|---|---|
| `before/audit-members-en-320.png`, `...-360.png`, `...-390.png`, `...-768.png`, `...-1024.png`, `...-1440.png` | Baseline owner state; full-width list rows leave desktop whitespace, invitation history competes equally, raw link and role terms lack explanation. |
| `before/audit-members-non-owner-en-390.png`, `...-1440.png` | Baseline non-owner copy is low-emphasis and does not identify who can help. |
| `before/audit-members-invite-en-320.png`, `...-360.png`, `...-390.png`, `...-768.png`, `...-1024.png`, `...-1440.png`; `before/audit-invite-accept-en-320.png`, `...-360.png`, `...-390.png`, `...-768.png`, `...-1024.png`, `...-1440.png` | Baseline raw one-time URL and acceptance asks for credentials without naming what happens next. No preview API means household name remains unavailable. |
| `after/audit-members-owner-en-320.png` | Useful owner roles fit, though invite section begins below the phone fold; bottom navigation remains visible. |
| `after/audit-members-owner-cs-320.png` | Czech labels wrap naturally without overflow; the invite action is below the fold due to the explanatory copy. |
| `after/audit-members-owner-en-390.png`, `after/audit-members-owner-en-768.png`, `after/audit-members-owner-en-1024.png`, `after/audit-members-owner-en-1440.png` | Owner list across target widths; wide layouts use two readable columns, phones one. At 390 the primary invite action fits, history starts lower in the page. |
| `after/audit-members-owner-cs-390.png`, `...-768.png`, `...-1024.png`, `...-1440.png` | Czech equivalents; longer role and invite text wraps without horizontal overflow. |
| `after/audit-members-owner-forced-dark-en-390.png`, `...-1440.png` | Forced-dark readiness follows semantic surfaces and state tokens; not a theme preference test. |
| `after/audit-members-non-owner-en-390.png` | Clear owner contact direction and role context; broad lower whitespace is calm but not dense. |
| `after/audit-members-non-owner-cs-390.png` | Czech owner-help state, naturally short and readable. |
| `after/audit-members-link-shown-en-390.png` | Link state appears with a ready notice, but the success notice moves the member context down; bearer field is deliberately masked. |
| `after/audit-members-link-manual-copy-en-390.png` | Clipboard denial is explicit and selected link can be copied manually; fallback notice pushes the primary action near the fixed bar. |
| `after/audit-members-revoke-confirm-en-390.png` | The browser clipboard-denial notice remains visible and the pending row is below the fold; revoke confirmation was asserted in the audit but is not shown in this frame. The real interaction remains covered by Vitest. |
| `after/audit-members-error-en-390.png` | Retryable failure uses the established danger Notice and a clear recovery action; naturally sparse. |
| `after/audit-invite-accept-en-320.png`, `...-390.png`, `...-768.png`, `...-1024.png`, `...-1440.png`, `after/audit-invite-accept-cs-390.png` | Acceptance states and next-step copy at target widths; does not invent household identity. |

The real browser does not receive clipboard permission, so evidence records the safe select-to-copy fallback rather than pretending a copy succeeded. The 320px fold deliberately prioritizes role clarity over fitting invitation controls above the bottom bar; the entire page remains scrollable. Audit checked for horizontal overflow on these captures. Axe scans run for populated owner and non-owner 390px states; keyboard/focus paths remain in the journey and Vitest suites. No human usability test is claimed. The after folder is curated to these 24 slice-specific screenshots; full audit artifacts were removed.
