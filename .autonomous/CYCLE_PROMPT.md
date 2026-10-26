Work autonomously on Tendo V2.1 comprehensive quality hardening, FIXING bugs
and ACTUALLY REFACTORING existing maintainability debt. V2 and V1 are complete.
Read AGENTS.md and linked docs, especially V2_1_SCOPE,
V2_1_DEFINITION_OF_DONE and locked CODE_QUALITY; preserve all product invariants.

Reconcile git, active PRs, STATE and required CI. Finish existing PRs first.
Maintain BOTH complete behavior/test coverage and prioritized maintainability
audit. Pick the highest-value unverified workflow, confirmed defect OR
high-risk oversized/tangled code hotspot as ONE focused PR-sized slice.
Discover real product behavior independently; do not just re-run existing tests.

For every discovered bug: reproduce with a failing deterministic test, FIX its
root cause, prove regression passes, run relevant full gates and preserve the
test. Do not merely report/defer bugs or manufacture green checks. Independently
inspect all hand-written code and refactor risky oversized modules, complex
functions and mixed responsibilities into readable cohesive units. Prove
behavior parity with characterization/E2E/integration tests, implement a
permanent maintainability CI regression check; do not stop at an audit report.
Run real production-built browser E2E with real API and isolated PostgreSQL where appropriate.
Use unit/integration/contract/property/race tests at more suitable layers.

Never directly commit/push main. Branch + PR, required green CI, independent
review and complete local verification before merge; sync and verify green main.
Use bounded subagent tasks, consult oracle on architecture/security/data
boundaries, and keep STATE and LEARNINGS accurate. Preserve existing controller,
OpenCode/systemd, pinned native test infrastructure and release pipeline.

After EVERY V2.1 DoD item is evidenced, including verified codebase refactoring
and lasting CODE_QUALITY enforcement, follow FLOW's V2.1 release/completion
protocol. Publish and VERIFY v2.1.0 artifacts, then merge V2_1_COMPLETE via
final protected PR and stop on clean main. V1/V2 markers remain historical.
