# All items capture index

Production-build UX audit (`frontend/e2e/ux-audit.spec.ts`) through `scripts/e2e-smoke.sh`, pinned toolchain/browser and real test API/database. Capture helper sets and asserts each viewport width; files are viewport-fold captures. Before uses detached `origin/main` at `cc74ce9`; after uses `feat/v3-09-all-items`. V2.1 audit baseline is separately identified. No human testing is claimed.

Commands:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/v309-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v309-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-all-items/after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

| Before | After | Purpose |
|---|---|---|
| `before/audit-all-items-en-320.png` | `after/audit-all-items-en-320.png` | Minimum phone width / overflow |
| `before/audit-all-items-en-390.png` | `after/audit-all-items-en-390.png` | Phone composition |
| — | `after/audit-all-items-en-360.png` | Intermediate phone width |
| `before/audit-all-items-en-768.png` | `after/audit-all-items-en-768.png` | Tablet composition |
| `before/audit-all-items-en-1024.png` | `after/audit-all-items-en-1024.png` | Desktop breakpoint and split |
| `before/audit-all-items-en-1440.png` | `after/audit-all-items-en-1440.png` | Wide desktop usage |
| `before/audit-all-items-cs-390.png` | `after/audit-all-items-cs-390.png` | Czech on phone |
| `before/audit-all-items-cs-1440.png` | `after/audit-all-items-cs-1440.png` | Czech desktop |
| — | `after/audit-all-items-dark-{390,1440}.png` | Forced-dark readiness |
| — | `after/audit-all-items-page2-{390,1440}.png` | Cursor page replacement/navigation |
| — | `after/audit-all-items-error-{390,1440}.png` | Server failure / Retry |
| `before/audit-all-items-done-en-390.png` | `after/audit-all-items-done-en-390.png` | Status-filter results |
| `before/audit-all-items-nomatch-en-390.png` | `after/audit-all-items-nomatch-en-390.png` | No-match state / clear action |
| — | `after/audit-all-items-search-en-{390,1440}.png` | Accent-insensitive Czech search at 1,000+ stage |
| — | `after/metrics.json` | Captured audit metrics, including route overflow and page/DOM measures |

The audit emits additional route and journey artifacts; only focused All items evidence is kept here. Captures are scripted evidence, not user approval.
