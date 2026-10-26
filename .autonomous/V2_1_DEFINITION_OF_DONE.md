# V2.1 Quality Hardening Definition of Done

status=locked_human_authorized
- [ ] Baseline source, routes, screens, API, tests, CI and schema audited.
- [ ] Permanent locked CODE_QUALITY rules adopted in agent instructions,
      enforced for subsequent autonomous PRs; source references documented.
- [ ] ALL hand-written production code inventoried for maintainability and
      hotspots ranked with evidence in v2-1-maintainability-audit.md.
- [ ] High-risk oversized/tangled files and complex mixed-responsibility
      functions ACTUALLY REFACTORED through reviewed PRs; not merely ticketed.
- [ ] Sensitive refactors protected by before/after regression evidence and
      meaningful tests; no changed contracts, migrations, or user outcomes.
- [ ] Maintainability regression check active in CI with honest baseline,
      justified exceptions and no new/worsened oversized/complex hotspots.
- [ ] A requirements-to-tests coverage matrix inventories ALL implemented
      features, controls, user journeys, states and API boundaries; every
      supported documented workflow has evidence or an explicitly justified gap.
- [ ] Representative positive, invalid, boundary, error, interrupted, retry,
      concurrency, security and state transition cases are automated at their
      appropriate layer; risk-based combination coverage is documented.
- [ ] Production-build Playwright + real backend + isolated PostgreSQL E2E
      exercises complete journeys and verifies actual persisted results.
- [ ] Repeat, Fluid, one-off, dates, DST, undo and historical migration paths
      have deterministic property/boundary/regression evidence.
- [ ] Identity, OIDC, invitation, authorization, session and household isolation
      have meaningful real-backend negative and adversarial scenario coverage.
- [ ] All supported UI routes and controls are checked on phone/tablet/desktop;
      Czech/English, focus, keyboard, accessibility, network/error recovery.
- [ ] Sorting/search/filter/count/pagination and navigation are verified
      against whole datasets, including 1000+ items and concurrent changes.
- [ ] Every discovered reproducible product defect was FIXED at the root and
      protected by a permanent regression test; no release-blocking defect open.
- [ ] Independent review challenged coverage and assertions, with evidence
      selected critical tests fail under controlled injected regressions.
- [ ] Required PR CI stays practical, comprehensive release qualification is
      repeatable and enforced, and no required checks are flaky/skipped/weakened.
- [ ] Existing native no-Docker integration/E2E infrastructure, pinned
      toolchain and all prior V1/V2 invariants remain intact.
- [ ] Complete local verification passes, required GitHub checks pass on
      branch and main, migrations and deployed clean-install/upgrade smoke pass.
- [ ] Coverage matrix records exact command/results, unresolved non-blocking
      risks and justified exclusions. User/admin/developer docs are updated.
- [ ] Human-readable release notes merged; annotated v2.1.0 tag on green main,
      GitHub Release/assets + public multi-arch GHCR image verified, and
      published install and upgrade verified with retained existing data.
- [ ] Only AFTER all above, `V2_1_COMPLETE` merged by a separate final PR;
      local main clean, marker tracked and autonomous runner exits successfully.
