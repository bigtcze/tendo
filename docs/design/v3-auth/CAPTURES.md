# Authentication and first-run capture index

Production-build audit files below come from the opt-in `ux-audit.spec.ts` using the native runner, synthetic API data, and browser stubs for outcomes not safely generated through the configured provider. Every auth capture asserts viewport width and no horizontal overflow before capture. No secrets are shown: synthetic credentials are only entered into masked controls. No human usability testing is claimed.

## Before: V3-02 baseline

The prior exact commit is `a9159e3`; before captures were regenerated from detached worktree `/tmp/opencode/v313-before` using the same production audit. These were curated from that run (the existing V3-02 committed captures at the same commit are retained too).

| File | Route/state | Size, locale | Critique |
|---|---|---|---|
| `before/onboarding-en-390.png` | First run | 390/en | Mixed label hierarchy and raw IANA timezone make the first view feel like a technical setup form. |
| `before/onboarding-en-320.png`, `before/onboarding-en-768.png`, `before/onboarding-en-1024.png` | First run | 320/768/1024/en | Field sequence is consistent, but setup language remains terse and raw timezone naming is technical at each width. |
| `before/onboarding-error-en-390.png` | First-run validation | 390/en | Empty submission focuses an error but does not yet distinguish validation on one chosen field. |
| `before/onboarding-cs-390.png`, `before/onboarding-cs-1440.png` | First run | 390/1440/cs | Czech labels keep the mixed hierarchy; the timezone identifier remains raw and technical. |
| `before/onboarding-en-1440.png` | First run | 1440/en | The narrow form uses little of the desktop measure. |
| `before/audit-login-en-390.png`, `before/audit-login-cs-390.png`, `before/audit-login-cs-1440.png` | Sign-in | 390 en/cs; 1440 cs | The form is sparse and lacks helpful context; Czech labels add length without changing the composition. |
| `before/audit-login-en-320.png`, `before/audit-login-en-768.png`, `before/audit-login-en-1024.png` | Sign-in | 320/768/1024/en | The single-column form provides no desktop composition and leaves an empty area below it. |
| `before/audit-login-en-1440.png` | Sign-in | 1440/en | A narrow bare form sits high on a large empty page. |
| `before/audit-login-error-en-390.png`, `before/audit-login-error-en-1440.png` | Wrong credentials | 390/1440/en | Both fields look invalid even though the response cannot identify which credential was wrong; desktop remains a narrow column. |
| `before/audit-account-en-390.png`, `before/audit-account-cs-390.png`, `before/audit-account-en-1440.png`, `before/audit-account-cs-1440.png` | Account | 390/1440 en/cs | The page is sparse and directs people to the distant footer for language; desktop has a large unused content area. |

## After: auth state captures

| File(s) | Route/state | Size, locale | Critique |
|---|---|---|---|
| `after/auth-onboarding-en-{320,390,768,1024,1440}.png` | First run | 320–1440/en | Consistent sections and field labels clarify order. The form is intentionally taller than the phone viewport and continues below the fold; at 1440px, two-column fields use more measure without over-wide controls. |
| `after/auth-onboarding-cs-{390,1440}.png` | First run | 390/1440/cs | Czech copy wraps naturally; the human-readable timezone proposal still retains IANA value under the select. |
| `after/auth-onboarding-validation-en-390.png`, `after/auth-onboarding-server-error-en-390.png` | Validation and service error | 390/en | Validation marks only the target field. The server error remains recoverable but requires scrolling to reach the action on a short phone. The setup code is masked; the server error capture shows only dots. |
| `after/auth-login-en-{320,390,768,1024,1440}.png`, `after/auth-login-cs-{390,1440}.png` | Sign-in default | requested widths/locale | The primary path and optional provider share a purposeful panel. At desktop the panel still leaves broad quiet margins; that is preferable to adding an invented illustration or marketing copy. |
| `after/auth-login-error-en-{320,390,768,1024,1440}.png` | Wrong credentials | requested widths/en | One shared alert explains recovery without marking either credential invalid. Long recovery guidance makes the 320px panel vertically dense. |
| `after/auth-login-rate-limited-en-390.png`, `after/auth-login-session-ended-en-390.png` | Rate limit and session ended | 390/en | Messages are distinct and actionable; session notice fits above fields. |
| `after/auth-login-oidc-available-en-390.png`, `after/auth-login-oidc-provider-error-en-390.png`, `after/auth-login-oidc-outcome-en-390.png` | OIDC available, retryable outage, callback notice | 390/en | Provider outage stays quiet and password sign-in remains primary. The callback notice is generic and fragment is scrubbed. |
| `after/auth-login-forced-dark-en-{390,1440}.png` | Dark-token readiness | 390/1440/en | Surfaces, inputs and actions switch to dark tokens; readiness only, not saved preference or OS-following behavior. |
| `after/auth-account-unlinked-en-{390,1440}.png`, `after/auth-account-linked-en-{390,1440}.png`, `after/auth-account-link-error-en-390.png` | Account/provider states | 390/1440/en | Identity and settings now earn the page; language is directly switchable. The 1440px desktop uses a balanced two-column layout. Link error is attached to current-password flow. |
| `after/auth-account-cs-{390,1440}.png` | Account | 390/1440/cs | Czech labels wrap without clipping; the desktop layout remains calm but lower content is sparse when provider is linked. |
| `after/auth-account-forced-dark-en-{390,1440}.png` | Dark-token readiness | 390/1440/en | Account surfaces and controls use dark tokens; V3-14 preference behavior remains untested. |
| `after/auth-boot-loading-en-390.png`, `after/auth-boot-unavailable-en-390.png` | Boot | 390/en | Loading is deliberately quiet; unavailable provides a retry. |
| `after/auth-not-found-signed-out-en-390.png`, `after/auth-not-found-signed-in-en-390.png` | Not found | 390/en | Clear recovery action; the page remains sparse because there is no useful destination content to show. |

## Commands and limitations

After: `source ~/.config/tendo-test/env && TENDO_UX_AUDIT_DIR=/tmp/opencode/v313-auth-after bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh`; log `/tmp/opencode/v313-auth-logs/after-e2e.log`, `E2E_EXIT=0`. Before: `source ~/.config/tendo-test/env && TENDO_UX_AUDIT_DIR=/tmp/opencode/v313-auth-before bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v313-before && bash scripts/e2e-smoke.sh'`; log `/tmp/opencode/v313-auth-logs/before-e2e.log`, `E2E_EXIT=0`. Before auth-state captures used the real installation-computer setup source and baseline account/login screens; before did not contain later V3 OIDC test harness configuration so provider/OIDC callback states have no before pair. The opt-in audit is an evidence harness; its browser-stubbed states supplement, but do not replace, the full smoke's real OIDC and journey checks.
