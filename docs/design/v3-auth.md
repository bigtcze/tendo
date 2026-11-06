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

Before evidence is copied from the V3-02 production audit at `a9159e3`: the 390px setup/login/account and validation captures and the 1440px setup/login captures. At phone size the original forms were serviceable but the onboarding mixed label styles and presented a raw IANA value; account mostly pointed to the footer. At 1440px the login was a small form high on an otherwise empty page. After captures are not yet available: the production audit was started but hit the 30-minute harness timeout during its pre-existing invite audit, before completing or saving metrics. Therefore no after screenshot comparison or visual-verification claim is made.

## Accessibility and behavior

- Shared tokens/surfaces and existing button/input/label primitives are used; no new dependency or theme behavior.
- Setup errors retain per-field focus and `aria-describedby`; timezone display is separately tested against the unchanged IANA option value.
- Wrong-password feedback is an alert and does not mark either login field individually invalid. Provider errors keep password sign-in available.
- Language picker is a real in-page `LanguageSwitch` and updates/persists locale.
- OIDC allowlist parsing and fragment scrubbing remain unchanged.
- Unknown signed-out routes show a recovery screen. Existing malformed item-path behavior is preserved.
- Full frontend Vitest suites passed; see local validation in handoff. No e2e acceptance claim is made.

## Evidence

Curated baseline screenshots: [v3-auth/before](v3-auth/before/). Production capture output is in the private local test-results directory and was not copied because the audit did not finish; no after captures are committed. Automated checks: frontend lint, typecheck, and all Vitest tests passed. The required E2E journey and opt-in audit remain to be verified; do not infer a pass from the interrupted audit.

## Not done here

- Opt-in auth-state screenshot matrix/helper and axe scans have not been added.
- No production before/after screenshot set or screenshot-based review is claimed.
- No saved theme preference (V3-14 owns it), API/backend change, or human usability test.
- Full e2e smoke did not finish within 30 minutes; all browser journey/screen reader acceptance remains unverified.
