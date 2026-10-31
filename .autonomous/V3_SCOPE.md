# Tendo V3 – Everyday Elegance

status=locked_human_authorized
release_target=v3.0.0
focus=professional_consumer_product_redesign_and_ux_without_feature_scope_growth

## Goal
Transform the proven Tendo v2.1 product into an exceptionally usable,
beautifully restrained, coherent FAMILY web application. The desired result
is an everyday household experience that feels designed by experienced
consumer-product designers, not a generic AI-generated developer interface.
No new backend product features are authorized merely for this redesign.
Preserve the complete V2.1 test suite, code-quality guarantees and semantics.

## Required delivery order
A. Discover: inspect all implemented routes, UI controls, states, current
   design, real production screenshots, UX specifications and test coverage.
   Create `docs/design/v3-baseline-audit.md` with concrete findings and
   authentic production screenshots; prioritize by everyday household impact.
B. Explore: implement at least two real interactive design concepts on
   isolated branches/prototypes, each showing home and add/edit/detail plus
   navigation at phone and desktop widths. Review independently using the
   locked design brief, preserve evidence in `docs/design/v3-direction-review.md`.
   Pick one coherent direction by evidence; do not allow an AI to claim
   subjective human approval. Prototypes are not a mandate to ship two themes.
C. System: define design tokens, typography, responsive breakpoints, spacing,
   color, elevation, semantic states, motion and core component patterns in
   `docs/design/v3-design-system.md`. Avoid needless design-token machinery.
D. Implement: deliver a consistent redesign for ALL current product routes,
   including every state and responsive size, as focused, protected PRs.
   Review and fix visual problems after EACH meaningful UI slice.
E. Validate: real novice household journeys, visual regression baselines,
   contrast, keyboard, touch, zoom, localization, responsiveness, light/dark,
   large backlogs, performance, all V2.1 regressions and deployed flows.
F. Release: write professional public `docs/releases/v3.0.0.md`, update README
   and user docs, capture and COMMIT authentic current-product screenshots,
   tag and publish v3.0.0 on GitHub, verify release files and public GHCR
   amd64+arm64, verify clean install and upgrade from v2.1.0 without data loss.
   Only then merge final V3_COMPLETE through protected PR and stop the loop.

## Concrete UX acceptance targets
- Clear information hierarchy for today/needs attention and optional other
  states; relevant person, due/attention date and assignment are scan-friendly.
- A first-time user can navigate and add a one-off item without instruction.
- Repeat interval and cadence are explained in natural Czech and English;
  advanced decisions do not dominate the simple default path.
- Completion + undo, create/edit, archive/restore, search and invitations
  require few obvious actions; errors guide safe recovery.
- Useful adaptive layout for 320/360/390/768/1024/1440px, avoiding both a
  giant desktop void and a crammed phone interface; 200%/400% zoom where
  applicable; comfortable accessible targets and keyboard access.
- Light and dark mode have intentionally designed contrast and states, no
  flicker and no CSS flashes; OS-following mode and saved preference.
- Realistic content examples: empty, 1, 10, 100, and 1,000+ items; long Czech
  names, diacritics, long notes, multiple responsible people; no clipping.
- Visual QA proves pixel/layout consistency on browser screenshots but never
  "approves" design solely by arbitrary screenshot similarity.
- No known user-visible functional, accessibility or visual release blockers.

## Scope exclusions
- No unrequested new product features, account model changes, native mobile
  app, AI components, calendar/Nextcloud/email integrations, alerts or charts.
- No DB/schema/API/security rewrite for decoration. Existing frontend
  navigation/UI structure may be refactored where needed and fully tested.
- No forced replatforming or new third-party design system without technical
  and licensing justification.
- No false claims of human user testing, approval or aesthetic certainty.

## GitHub issue-based execution
All V3 work, defect investigations, design slices, validation and release
must be tracked under V3_ISSUES_WORKFLOW as GitHub Issues that BOTH belong
to the V3 milestone AND bear the exclusive `tendo:v3-autonomous` label.
Unlabelled or otherwise out-of-scope Issues, including user/third-party
Issues placed in the milestone, are NEVER autonomous work assignments.
Issues are the source for V3 progress, claims and blockers; never recreate
STATE.md after the one-time protected migration. Release and V3_COMPLETE
require verification of the agent-managed labelled backlog and locked DoD.

## Execution constraints
Read V3_DESIGN_BRIEF, V3_DEFINITION_OF_DONE, CODE_QUALITY, GUARDRAILS,
PRODUCT, UX, ARCHITECTURE, API, TESTING and all applicable existing docs.
Use the current native toolchain, real API, production-built browser and
isolated PostgreSQL for E2E; keep tests meaningful, fix defects discovered
along the way, and maintain green required GitHub CI. One focused PR at a time,
protected main only, no unattended speculative scope growth.
