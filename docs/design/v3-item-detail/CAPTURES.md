# V3 item detail captures

Source: opt-in `frontend/e2e/ux-audit.spec.ts`, production binary and test PostgreSQL via `scripts/e2e-smoke.sh` (fixed clock 2026-05-14 Europe/Prague, reduced motion). Default detail states are full-page captures; Done/Undo/gone/archived states are viewport captures. Before captures are the committed baseline in [`../v3-baseline/`](../v3-baseline/). PNG dimensions were checked with `file`.

| State / locale | Before | After files and dimensions | Visual review |
|---|---|---|---|
| Open repeating item, EN 320/360/390/768/1024/1440 | `audit-item-detail-en-{390,768,1440}.png` | `audit-item-detail-en-{320,360,390,768,1024,1440}.png` (320 × 1313; 360/390 × 1281; 768 × 1172; 1024/1440 × 1128) | Inspected 390/1440; Done is first and primary |
| Open repeating item, CS 320/360/390/768/1024/1440 | `audit-item-detail-cs-{390,1440}.png` | `audit-item-detail-cs-{320,360,390,768,1024,1440}.png` (320/360/390 × 1281; 768 × 1172; 1024/1440 × 1128) | Inspected 320/390 |
| Done (one-off), EN/CS 390/1440 | `audit-item-detail-done-{en,cs}-{390,1440}.png` | same names (390/1440 × 900) | Inspected EN 390 |
| Done notice with Undo, EN 390/1440 | No baseline (no Done on detail) | `audit-item-detail-done-notice-en-{390,1440}.png` (390/1440 × 900) | Inspected 390 |
| After Undo, EN 390/1440 | No baseline | `audit-item-detail-undone-en-{390,1440}.png` (390/1440 × 900) | Inspected 390 |
| Gone (404), EN 390/1440 | No baseline | `audit-item-detail-gone-en-{390,1440}.png` (390/1440 × 900) | Inspected 390 |
| Archived, EN/CS 390/1440 | `audit-item-detail-archived-{en,cs}-{390,1440}.png` | same names (390/1440 × 900) | Inspected EN 390 |
| Long title, EN 390/1440, CS 390 | `audit-item-detail-long-title-*` | same names (390 × 1336; 1440 × 1022) | Inspected; wraps without horizontal overflow |
| Long notes, EN 390/1440 | `audit-item-detail-long-notes-*` | same names (390 × 1313; 1440 × 1068) | Inspected |
| Forced dark readiness, EN 390/1440 | No baseline | `audit-item-detail-forced-dark-en-{390,1440}.png` (390/1440 × 1128/1281) | Readiness only; history card contrast in dark is V3-14 scope |

Capture artifacts, not product defects:

- Full-page phone/tablet captures show the fixed bottom navigation over mid-page content (the History heading). Playwright stitches the page at the viewport position; in a real scroll the main region reserves `pb-[calc(6rem+safe-area)]` (`frontend/src/app/Layout.tsx`) so the last row clears the bar.
- The native date input in Czech form captures shows `mm/dd/yyyy` because headless Chromium formats native date controls by its own UI language, not the page `lang`. Real browsers follow the user's OS/browser locale.

These are scripted visual evidence, not usability testing or human approval. Do not run the audit concurrently with another production E2E/verification process (L012).
