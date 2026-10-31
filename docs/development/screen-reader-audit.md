# Opt-in Orca screen-reader evidence

Run an additional, opt-in production-build screen-reader check with
`source ~/.config/tendo-test/env` (private file; do not print it), then:

```bash
TENDO_SR_AUDIT_DIR=/tmp/opencode/tendo-orca-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh
```

The mode is mutually exclusive with `TENDO_README_SCREENSHOT` and
`TENDO_UX_AUDIT_DIR`. It is not selected in required PR CI or default E2E runs;
it is a required release-qualification gate (see `release-qualification.md`).
Run only one Orca session per host user at a time: `orca --replace` kills every
same-user Orca process regardless of display.
It starts the production-built app against the isolated PostgreSQL test service,
then starts a private Xvfb/D-Bus/AT-SPI/Orca session and headful Chromium with
renderer accessibility enabled. The harness makes a real xdotool click in web
content (accounting for browser-chrome offsets), focuses the intended DOM control,
and sends real xdotool keys so Orca observes keyboard events and AT-SPI changes.
It rejects clicks that land in browser chrome. Output includes per-step JSON and
Markdown transcripts plus a run manifest. Only `transcript.json`, `transcript.md`,
and `manifest.json` are sanitized.
`orca-debug.out`, `runtime/` logs, and `runtime/logs/audio.raw` are unsanitized,
owner-only local outputs that may contain sensitive data; do not upload or commit
them. Keep the audit directory private and review sanitized artifacts before sharing.

Host prerequisites on Linux: `orca`, `at-spi2-core`, `speech-dispatcher`,
`espeak-ng`, `xdotool`, `openbox`, `dbus-x11`, `xvfb`, Chromium runtime
libraries, and a Playwright Chromium installation from the pinned toolchain.

## What it proves and limits

This provides bounded automated evidence that Orca 48 can speak selected
localized controls in a production build under Chromium on Linux. It does not
prove complete keyboard accessibility, user comprehension, or every screen/state.
It is synthesized speech, not human screen-reader user testing; it does not test
NVDA, JAWS, VoiceOver, or TalkBack; and it is not WCAG certification. A passing
run does not replace the meaningful manual/keyboard review required by V2.

Repeatability measurement on 2026-10-25: five sequential runs with no code
changes between them and one additional run after a 125-second idle gap all
passed, each with 12/12 transcript steps passing. Versions recorded in each
manifest: Orca 48.1, AT-SPI2 2.56.2 (package 2.56.2-1+deb13u2),
speech-dispatcher 0.12.0, eSpeak NG 1.52.0+dfsg-5, Chromium for Testing
156.0.8078.4, and Playwright 1.64.1. This is a small local sample, not proof
of universal determinism; longer idle periods, different hosts, and future
versions remain untested. Earlier failures showed Orca exiting with
`Cannot open display ":0"` before Chromium registration; the session now waits
for Orca's startup speech, and the spec fails fast unless Chrome registers with
AT-SPI after the first page load. The harness does not yet prove item form,
completion/Undo live-region updates, filters/results, pagination, network retry,
onboarding completion, invitation acceptance, or switch state. Successful
transcripts are per-run evidence only. Do not treat the harness as evidence for
unobserved flows.
