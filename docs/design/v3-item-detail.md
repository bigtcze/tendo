# V3 item detail (V3-10, packet 2)

> Historical record. It describes the V3 stage (released as `v3.0.0`) and is not the current UX or security contract. Current contracts: [UX](../../.autonomous/UX.md), [SECURITY](../../.autonomous/SECURITY.md).

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Primary action | Done is the first, filled action for a live item; Start/Resume are secondary, Waiting/Pause quiet. | Baseline B5: someone opening an item usually wants to finish it; workflow states are secondary. |
| Completion | Done/Undo reuse the shared `completionOutcomes` path (same idempotency key, read-then-write, notices) as Home. | One completion rule set; no detail-specific API behavior. |
| Refresh failure | If completion succeeds but the follow-up read fails, the success notice and Undo stay; Done, workflow, Edit and Archive are disabled until **Reload** succeeds. | Prevents a second completion against stale data (for repeating items it would complete the next cycle). |
| Focus | Done/Retry outcome focuses the (programmatically focusable) notice; successful Undo returns focus to Done once the lock clears. | Removed controls must not strand keyboard and screen-reader users. |
| Dates and repeat | Summary card shows status, next date, last done, repeat sentence with plural-aware unit words, responsible and notes; history below is subordinate. | Baseline B10: essential facts distinct from history. |

## Evidence

Production captures and dimensions: [`v3-item-detail/CAPTURES.md`](v3-item-detail/CAPTURES.md). Out of scope here: dark-theme history contrast (V3-14).
