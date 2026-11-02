# Home capture index

Captured with the production-build UX audit and real test database; `scripts/e2e-smoke.sh` uses the pinned toolchain. Before images are from temporary `origin/main` worktree at d2f3b1a. After images are from `feat/v3-08-home`. Every file is a 390/1440/etc viewport-fold screenshot unless the filename lacks `-fold`; the audit sets and asserts viewport width before each capture.

Command pattern:

```sh
source ~/.config/tendo-test/env
TENDO_UX_AUDIT_DIR=/tmp/opencode/v308-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v308-before && bash scripts/e2e-smoke.sh'
TENDO_UX_AUDIT_DIR=/tmp/opencode/v308-after-final-captures bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

| Before | After | Purpose |
|---|---|---|
| `before/home-empty-en-390-fold.png` | `after/home-empty-en-390-fold.png` | Empty state / one action |
| `before/home-1-en-390-fold.png` | `after/home-1-en-390-fold.png` | One-item layout |
| `before/home-30-en-320-fold.png` | `after/home-30-en-320-fold.png` | Minimum phone width |
| `before/home-30-en-390-fold.png` | `after/home-30-en-390-fold.png` | Populated phone |
| `before/home-30-en-768-fold.png` | `after/home-30-en-768-fold.png` | Tablet stack |
| `before/home-30-en-1024-fold.png` | `after/home-30-en-1024-fold.png` | Desktop breakpoint |
| `before/home-30-en-1440-fold.png` | `after/home-30-en-1440-fold.png` | Desktop columns and measure |
| `before/home-30-cs-320-fold.png`, `390-fold.png`, `1440-fold.png` | matching files | Czech wrapping |
| `before/home-500-en-390-fold.png` | `after/home-500-en-390-fold.png` | Large stage |
| `before/home-1200-en-390-fold.png`, `1440-fold.png` | `after/` matching files | 1,000+ stage / cap navigation |
| — | `after/home-1200-en-390-forced-dark.png`, `1440-forced-dark.png` | Explicit dark-token readiness |
| `before/audit-home-server-error-en-390.png` | `after/` same | Error/retry state |
| `before/home-30-cs-390.png` | `after/` same | Long Czech title/data |

The capture output also contains full-page audit images, route sweeps and `metrics.json`; only the focused evidence set is kept here. Before and after are stored in separate folders with name prefixes in the index below.
