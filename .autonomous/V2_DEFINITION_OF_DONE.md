# V2 GUI Definition of Done

status=locked_human_authorized

- [ ] Audit existing V1 UX and implementation. Record evidence-based, prioritized
      usability and scale problems; review the design approach independently.
- [ ] A coherent, distinctive but restrained consumer design language is used across
      all currently implemented routes, not merely the home page.
- [ ] A normal household user can find and manage an item among 500 and 1000+
      realistic seeded items without paging blindly through everything.
- [ ] Search/filter/group behaviors cover the FULL matching dataset; stable paginated
      lists preserve navigation context, correct ordering, and truthful counts.
- [ ] No unbounded data fetch or DOM expansion; realistic large-backlog scenarios
      receive deterministic performance/API-query checks and measured review.
- [ ] Attention-first home is calm; other items/states remain accessible. Clear
      status, ownership, dates and Repeat/Fluid meaning without jargon.
- [ ] Create, edit, complete, undo, pause, resume and archive flows are simple,
      reliable and tested; no regression to recurrence or concurrency semantics.
- [ ] Phone (320px/390px), tablet (768px) and desktop (1440px) flows verified;
      no clipped actions or horizontal overflow at common sizes/200% zoom.
- [ ] WCAG 2.2 AA-oriented checks cover contrast, keyboard, focus, semantics,
      accessible forms/errors, mobile targets, and reduced motion; automated
      accessibility checks plus meaningful manual/keyboard review.
- [ ] Empty, loading, error, permission and offline/network-failure states work;
      Czech and English copy has no hard-coded or truncation surprises.
- [ ] Unit, integration, API-contract and production-build Playwright E2E tests
      prove behavior with real PostgreSQL, including 1000+ item datasets.
- [ ] Existing pinned toolchain/full local verification and required GitHub CI
      pass; main is green, branch protections respected and no known blocker.
- [ ] User/admin/developer documentation is updated for changed workflows and
      server-side list/search/filter contract; V1 data upgrades without loss.
- [ ] Before the v2.0.0 release tag, the root README.md professionally
      presents the actual V2 product, its main workflows and current
      installation instructions, without obsolete V1-only claims.
      It prominently includes at least one authentic, high-quality
      screenshot captured with Playwright from the production-built V2
      application using representative synthetic household data.
      The image is committed to the repository, has descriptive alt text,
      contains no personal data or secrets, and renders correctly on GitHub.
      No mockups, AI-generated UI images, placeholders or outdated images.
      The README and screenshot must be reviewed before release.
- [ ] Human-facing release notes are merged; annotated v2.0.0 tag points to
      green main; existing release workflow publishes GitHub Release and public
      ghcr.io/bigtcze/tendo:v2.0.0 image (amd64/arm64) with release assets.
- [ ] Validate actual release installation/upgrade and published artifacts,
      not merely a green workflow; do not pretend success if external settings
      or human intervention block publishing.
- [ ] Only AFTER all above, merge `.autonomous/V2_COMPLETE` via normal PR,
      sync clean main and end autonomous cycles. Preserve `PROJECT_COMPLETE`.
