# V3 first-run, sign-in, account and recovery (V3-13)

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Setup | Give setup and owner account consistent field labels and separate them with two short household-first sections. Explain that the one-time code is in the private `.env` on the installation computer. | Replaces the technical install-wizard feel while accurately describing the documented source of the code. |
| Time zone | Keep the detected IANA zone as the select value and submitted value; show a localized place name plus the browser's long generic time-zone name. | Makes the proposal legible without changing server semantics. |
| Sign-in | Password form remains first and available during provider status failure. The form and copy are placed in the signed-out content measure, rather than pinned to a narrow strip in the wide desktop frame. | Sign-in must not depend on an optional provider, and desktop should use available measure intentionally. |
| Authentication errors | Server-reported credential failure is announced in one error region without claiming both credentials are individually wrong. Explain password recovery through the person responsible for the installation. | Avoids false field-level blame and provides a safe next step. |
| Account | Pair identity/provider status with in-page language selection; retain a clean appearance preference section without adding a nonfunctional theme control. | The language switch is reachable here; theme preference belongs to V3-14. |
| Signed-out framing | No rail reservation. Setup and sign-in forms use responsive measure; boot failure and unknown route are centered recovery states. | Keeps signed-out pages purposeful on desktop without a fabricated marketing panel. |

## Screenshot critique

Before evidence was regenerated from the detached `a9159e3` worktree; after evidence is from this branch. The indexed captures show the old mixed onboarding field hierarchy/raw IANA timezone, narrow login at desktop, wrong-password field coloring, and account language pointer. After captures include the requested widths for setup/login, representative phone and desktop Account states, callback/provider/service errors, and dark-token readiness. Visual review of the actual PNGs: the signin panel has clear priority, preserves a calm 1440px measure without a fake marketing column, and fits at 320px; long recovery guidance is dense but remains readable. Onboarding at 320px continues below the fold (expected for this multi-field first-run flow); its prose and field sequence remain legible. Account at 1440px uses a modest two-column layout; linked-provider states leave a quiet lower area, and the appearance entry explicitly explains that choices are coming rather than simulating a control. Error screens remain sparse by design. Account/OIDC and failure captures are synthetic browser-stubbed states, not real provider interactions; forced-dark captures check token readiness only.

## Accessibility and behavior

- Shared tokens/surfaces and existing button/input/label primitives are used; no new dependency or theme behavior.
- Setup errors retain per-field focus and `aria-describedby`; timezone display is separately tested against the unchanged IANA option value.
- Wrong-password feedback is an alert and does not mark either login field individually invalid. Provider errors keep password sign-in available.
- Language picker is a real in-page `LanguageSwitch` and updates/persists locale.
- OIDC allowlist parsing and fragment scrubbing remain unchanged.
- Unknown signed-out routes show a recovery screen. Existing malformed item-path behavior is preserved.
- Full frontend Vitest suites passed. The opt-in production UX audit completed successfully and captured login, onboarding, account, OIDC, boot and not-found states. The full production E2E smoke suite also passed after updating affected route, language-selector, and journey expectations; this includes the serial journey and screen-reader audit projects.

## Evidence

Curated production-build before/after screenshots and honest per-state critique are indexed in [v3-auth/CAPTURES.md](v3-auth/CAPTURES.md). The opt-in production audit completed with `E2E_EXIT=0` and wrote 41 `auth-*.png` captures plus the wider audit metrics under `/tmp/opencode/v313-auth-after`; these captures are curated in the docs directory. The command was `source ~/.config/tendo-test/env && TENDO_UX_AUDIT_DIR=/tmp/opencode/v313-auth-after bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh`; log: `/tmp/opencode/v313-auth-logs/after-e2e.log`. The baseline production audit completed at detached `a9159e3`; its curated auth captures are under `v3-auth/before/`. The full production smoke was run with `source ~/.config/tendo-test/env && bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh`; log: `/tmp/opencode/v313-auth-logs/full-e2e.log`, `E2E_EXIT=0` (native production-binary e2e passed; 55 Playwright tests across the serial journey and browser primitives, plus the separate screen-reader audit project). Lint, typecheck, and Vitest also passed.

## Not done here

- Some callback/provider and error states are browser-stubbed because those states are not safely reproducible on demand through the external identity provider; the configured OIDC smoke journeys still exercise actual provider redirects.
- No saved theme preference (V3-14 owns it), API/backend change, or human usability test.
