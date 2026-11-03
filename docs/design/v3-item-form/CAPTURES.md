# V3 item form captures

Source: opt-in `frontend/e2e/ux-audit.spec.ts`, production binary and test PostgreSQL via `scripts/e2e-smoke.sh`. Form states use viewport captures (not full-page). Width is set before each sequence and `capture()` asserts `window.innerWidth`. Before captures use a detached worktree at `76004a0`; after captures use this branch. PNG dimensions were checked with `file`.

| State / locale | Before files and dimensions | After files and dimensions | Visual review |
|---|---|---|---|
| Add empty, EN 320/390/768/1024/1440 | `audit-add-form-en-390.png` (390 × 20557; legacy full-page) | `audit-add-empty-en-{320,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390; all dimensions checked |
| Add fixed, EN 320/390/768/1024/1440 | `audit-add-form-en-390.png` (390 × 20557; legacy combined form) | `audit-add-fixed-en-{320,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390; all dimensions checked |
| After completion, EN 320/390/768/1024/1440 | No matching baseline state capture | `audit-add-after-completion-en-{320,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390/1440; all saved captures dimension-checked |
| Validation, EN 320/390/768/1024/1440 | No matching baseline state capture | `audit-add-validation-en-{320,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390; all saved captures dimension-checked |
| Add empty/fixed/after completion, CS 320/360/390/768/1024/1440 | `audit-add-form-cs-390.png` (390 × 21337; legacy combined form) | `audit-add-{empty,fixed,after-completion}-cs-{320,360,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390/1440; dimensions checked | 
| Validation, CS 320/360/390/768/1024/1440 | No matching baseline state capture | `audit-add-validation-cs-{320,360,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390; dimensions checked |
| Edit, EN 320/390/768/1024/1440 | `audit-item-edit-en-390.png` (390 × 1399), `audit-item-edit-en-1440.png` (1440 × 1366) | `audit-item-edit-en-{320,390,768,1024,1440}.png` (each viewport width × 900) | Inspected 320/390/1440 | 
| Edit, after-completion, EN 320/360/390/768/1024/1440 | No baseline after-completion edit capture | `audit-item-edit-after-completion-en-{320,360,390,768,1024,1440}.png` (each viewport width × 900) | Inspected all six; dimensions checked |
| Edit conflict, EN 390 | `audit-item-edit-conflict-en-390.png` (390 × 1581) | `audit-item-edit-conflict-en-390.png` (390 × 900) | Inspected |
| Edit, CS 390/1440 | `audit-item-edit-cs-390.png` (390 × 1399), `audit-item-edit-cs-1440.png` (1440 × 1366) | `audit-item-edit-cs-390.png` (390 × 900), `audit-item-edit-cs-1440.png` (1440 × 900) | Inspected |
| Forced dark readiness, EN 390/1440 | No baseline state captures | `audit-add-forced-dark-en-{390,1440}.png` (390/1440 × 900) | Inspected; roles distinguishable, not full dark-theme sign-off |
| Pending-create recovery | No baseline state capture | Not captured | Not feasible in current audit driver |

Before and after captures use the same production audit fixture format and are scripted comparisons only, not usability evidence. Dark readiness is isolated to `<html data-theme="dark">`; broader theme behavior belongs to V3-14. The audit capture command must not run concurrently with another production E2E/verification process (L012).
