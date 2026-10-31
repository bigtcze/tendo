# Tendo V3 – Everyday Elegance: Definition of Done

status=locked_human_authorized
- [ ] V2.1 release and V2_1_COMPLETE verified as historical baseline.
- [ ] STATE.md migrated and removed through protected PR; V3_ISSUES_READY
      recorded; all V3-00..V3-19 agent-created Issues have the dedicated
      `tendo:v3-autonomous` ownership label AND the V3 milestone, with useful
      acceptance criteria, dependencies, status labels and no duplicates.
- [ ] Every V3 design/refactor/QA defect and remaining release blocker tracked
      in agent-owned labelled GitHub Issues; claims and handoff via comments,
      work backed by merged PR and green main. No recreated STATE file.
- [ ] Autonomous work and V3 completion queries exclude user/third-party
      Issues without the ownership label, even within the V3 milestone.
      Agents never adopt pre-existing unlabelled Issues by labelling them.
- [ ] Real shipped V2.1 UI audited across ALL routes, relevant states,
      phone/tablet/desktop, realistic content and accessibility; authentic
      baseline screenshots, prioritized findings and user-effort evidence.
- [ ] At least two genuinely distinct IMPLEMENTED interactive design concepts
      compared for usability, visual coherence, accessibility, density and
      responsive behavior, with authentic browser screenshots and independent
      critical review; chosen direction and tradeoffs recorded.
- [ ] Cohesive, distinctive, restrained consumer design language implemented:
      deliberate type, color, spacing, responsive rhythm, controls, states,
      icons, feedback and a consistent reusable component vocabulary.
- [ ] Home/All items/create/edit/detail/history/people/members/account/setup/
      login/OIDC and every empty/loading/error/permission/offline state
      visually and functionally refined; no old unstyled corners.
- [ ] First-time nontechnical user scenarios verified with observable actions
      and comprehensible Czech/English copy, especially one-off vs Repeat
      and fixed vs after-completion recurrence (no internal jargon).
- [ ] Adaptive layout works on 320/360/390/768/1024/1440px, mobile touch
      and desktop keyboard; real 200%/400% zoom, long strings, large data.
- [ ] Light/dark/OS-following modes implemented intentionally and persist
      preference without flashes; visual contrast and semantic status remain
      understandable without color alone.
- [ ] WCAG 2.2 AA-oriented accessibility retained/improved: forms/errors,
      landmarks, focus order/visibility, keyboard, motion, touch targets,
      contrast; evidence from automated AND meaningful manual-style review.
- [ ] Meaningful screenshot-based visual regression coverage across routes,
      breakpoints, themes and key interaction states; stable deterministic
      fixtures and explicitly reviewed changes, no snapshot rubber-stamping.
- [ ] V2.1 behavior, permissions, schema/API and quality invariants intact;
      no functional E2E, integration, unit, contract, race or security
      coverage weakened to accommodate design.
- [ ] New/changed UI remains professionally maintainable under permanent
      CODE_QUALITY checks, without oversized mixed-responsibility modules.
- [ ] Independent skeptical visual/UX and code reviews completed on realistic
      browser screenshots; found problems actually fixed, not just logged.
- [ ] Full pinned native verification and all required PR + main CI pass;
      production clean-install, V2.1→V3 persistence upgrade, backups and
      reverse proxies verified where relevant.
- [ ] README has a current, committed REAL v3.0.0 screenshot captured from
      the released UI with synthetic household data (not an AI mockup);
      user/admin docs and version text match actual product.
- [ ] Professionally written human-facing `docs/releases/v3.0.0.md` merged:
      visual changes, workflow improvements, upgrades, compatibility,
      tested artifacts and known limitations; not a raw commit dump.
- [ ] ANNOTATED v3.0.0 tag on green main; GitHub Release with correct notes,
      public compose.yaml and example.env, public GHCR multi-arch image
      (amd64/arm64, version tags and latest) actually published and VERIFIED.
- [ ] Actual released v3.0.0 clean install and upgrade from released v2.1.0
      pass with prior household data preserved and end-to-end use verified.
- [ ] Only AFTER all above, merge `.autonomous/V3_COMPLETE` through a
      separate final protected PR; sync clean main, marker tracked, loop
      exits; preserve V1, V2 and V2.1 completion markers and prior tags.
