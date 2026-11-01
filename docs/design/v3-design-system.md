# V3 design system — foundation (V3-06)

Practical foundation for Direction A's calm list-first language and a dedicated detail page. This defines reusable visual/interaction rules, not a shell or screen redesign. Tokens are in `frontend/src/index.css`; hex source and measured contrast checks are `frontend/src/designTokens.ts` and `.test.ts`.

## Principles

- Household-first, progressive disclosure, clear hierarchy: use plain words, one obvious next action, strong titles and quiet metadata (Design Brief §20–27; UX §9–16).
- Comfortable density and adaptive width: lists must scan from a few to 1,000+ items without a narrow desktop column or cramped phone controls (Brief §27–30; V3_SCOPE §41–55).
- Warm, neutral, purpose-colored: color conveys state, never decoration; avoid gradients, badge soup and gratuitous shadows (Brief §31–34).
- Readable and accessible: consistent type, AA text contrast, named actions, visible keyboard focus, ≥44px touch targets, reduced motion (Brief §35–42; Direction review §6).
- Forgiving and predictable: completion is reversible; no essential hover-only information; one filled action per screen (UX §68–85; Direction review §5).

## Typography and rhythm

Use existing system stacks; no font dependency is added. Body uses the system-rounded stack; display uses the existing system serif. Sizes/line-height (rem; line-height unitless):

| Role | Size / line-height | Use |
|---|---:|---|
| Display | 36 / 1.15 | Brand or large welcome heading |
| H1 | 30 / 1.2 | Screen title |
| H2 | 24 / 1.3 | Section/group heading |
| Row title | 18 / 1.35 | Primary item text |
| Body | 16 / 1.5 | Main content and form values |
| Meta | 14 / 1.4 | Supporting dates/context |
| Label | 14 / 1.35 | Field labels |
| Control | 16 / 1.25 | Buttons and interactive values |
| Small | 14 / 1.35 | Supporting compact text; never shrink existing text |

Each `--text-<role>` token also defines `--text-<role>--line-height` in Tailwind 4 `@theme`; using its `text-*` utility therefore keeps the documented size and line height together.

Spacing scale: 4, 8, 12, 16, 24, 32, 48, 64px. Use 8/16 for routine grouping, 24/32 between sections; 4 is for tight inline alignment. Keep row content naturally wrapping.

## Responsive rules

Design/test widths: **320, 360, 390, 768, 1024, 1440px**. At 768px tablet content can gain measure while retaining phone navigation and full-width touch controls. At 1024px desktop composition may introduce a navigation rail and independent content columns, only when both columns earn their width and retain readable measure. Do not reserve shell space on sign-in/setup. These are system breakpoints; screen composition is owned by later screen issues.

## Color roles

Dark roles apply only when explicitly selected with `:root[data-theme='dark']`. No OS theme behavior or switch is introduced here (V3-14 owns preference, OS-following and no-flash behavior).

| Role | Light | Dark |
|---|---|---|
| Surface / paper | #faf7f2 | #1d211f |
| Raised | #ffffff | #282e2a |
| Sunken / sand | #efe9df | #343a35 |
| Text / ink | #25302b | #f1eee8 |
| Muted | #5b655f | #c1c9c1 |
| Border / line | #d8d0c3 | #59615a |
| Field border | #857c6e | #aab2a8 |
| Accent | #2f6b57 | #9bc8ad |
| Accent-strong | #234f41 | #b6ddc4 |
| On-accent | #ffffff | #17221b |
| Focus ring | #234f41 | #b6ddc4 |
| Needs attention | #8a3e19 / #f5e5d8 | #ffc49f / #493126 |
| In progress | #285a78 / #e0edf4 | #a9d8f1 / #243943 |
| Waiting | #66518a / #eee8f5 | #d2b9f3 / #392e49 |
| Paused | #625d52 / #edeae4 | #d0c9b9 / #3b3932 |
| Done | #356149 / #e3eee6 | #a8d5b3 / #293b2e |
| Error / danger | #a3342a / #f6e5e2 | #ffb4a9 / #492b28 |
| Success | #356149 / #e3eee6 | #a8d5b3 / #293b2e |

Semantic pairs are **foreground / subtle background**; they also need a short visible label or explicit icon/shape (not color alone). Needs attention uses a clear attention phrase; progress/waiting/paused/done retain text labels; error/success use alert/status semantics and concise text. Never convey state by color alone.

### Contrast table (computed sRGB ratios)

WCAG AA: normal text ≥4.5:1; large text ≥3:1; non-text controls/boundaries ≥3:1. Values below are computed by the test using the shared WCAG utility, not estimated. Semantic state foregrounds are tested on their subtle background; text roles on paper/raised/sunken; focus and field boundaries against paper.

| Pair | Light | Dark | Minimum |
|---|---:|---:|---:|
| Ink / paper | 12.79 | 14.06 | 4.5 |
| Muted / paper | 5.66 | 9.61 | 4.5 |
| Ink / raised | 13.67 | 11.97 | 4.5 |
| Muted / raised | 6.05 | 8.18 | 4.5 |
| Ink / sunken | 11.32 | 10.06 | 4.5 |
| Muted / sunken | 5.01 | 6.87 | 4.5 |
| On-accent / accent | 6.25 | 8.79 | 4.5 |
| On-accent / accent-strong | 9.27 | 11.03 | 4.5 |
| Accent / paper | 5.85 | 8.73 | 4.5 |
| Focus / paper | 8.67 | 10.96 | 3 |
| Field border / paper | 3.85 | 7.48 | 3 |
| Needs attention / subtle | 6.14 | 7.80 | 4.5 |
| In progress / subtle | 6.23 | 7.91 | 4.5 |
| Waiting / subtle | 5.63 | 7.21 | 4.5 |
| Paused / subtle | 5.45 | 7.01 | 4.5 |
| Done / subtle | 5.96 | 7.29 | 4.5 |
| Error / subtle | 5.59 | 7.44 | 4.5 |
| Success / subtle | 5.96 | 7.29 | 4.5 |

Field boundary passes 3:1 as non-text UI; field values themselves use ink. The CSS token sync and all pair thresholds are enforced in `designTokens.test.ts`.

## Elevation, shape, focus, motion, icons

- Elevation is minimal: paper and raised surfaces differ by color/border; use no decorative shadow. A modal backdrop is functional separation.
- Radius: 8px compact controls, 12px buttons/fields, 16px panels, 999px round done control.
- Focus: 2px solid focus-ring, 2px offset; never remove the browser focus indicator without an equally visible replacement.
- Motion: color/transform transitions around 150–200ms, ease-out; entrance 400ms ease-out only for restrained settling. Reduced-motion removes transitions/animation and never removes information.
- Prefer text labels for important actions. Any actionable icon has an accessible name; no icon-only critical action. No new icon package without a clear need.

## Component patterns

Primitives in `frontend/src/components/ui` are presentation-only: copy and labels arrive through props or typed i18n resources, never API/domain work.

| Pattern | Anatomy and states |
|---|---|
| Button | Primary filled, secondary outlined, quiet text. One filled action per screen; min-height 44px. Include disabled and busy state (`aria-busy`) without losing label. Focus visible. |
| Field / select / textarea | `Field` renders visible label, control render slot, optional hint and error; stable id and `htmlFor`; error uses `aria-invalid` and `aria-describedby`; focus, disabled and invalid states. |
| Switch | Label plus track; `role=switch`, `aria-checked`; 44px minimum target; checked/unchecked/disabled. |
| Disclosure | Button with `aria-expanded`/`aria-controls`, keyboard and touch activation; collapsed content is not exposed. |
| Dialog / sheet | Native `<dialog>` with `showModal()`, theme-neutral functional dim `::backdrop`, inert background, Escape, one focus-return owner and heading fallback target. A labelled close control sits in the heading row. No dismissal while busy; an unexpected native close reopens while busy. Phone sheet is a dialog variant; no custom focus trap. |
| List row / done | Title, always-readable context, optional action. Round completion button has ≥44px target, caller-provided localized accessible name and pressed state; glyph is hidden from assistive tech. |
| Group header | Clear heading with optional count; count is supplementary, not the heading name. |
| Empty state | Plain title, brief helpful context and optional one obvious action; no ornamental dividers. |
| Inline notice / toast | Status or alert semantics, concise text, success/error tone plus text; persistence/dismissal owned by caller. |
| Skeleton / loading | Decorative skeleton hidden from accessibility tree; pair with a real `aria-busy`/live loading label at screen level. Reduced motion is still legible. |
| Pagination | Cursor paging in named navigation: localized previous/next controls disabled at bounds plus localized results-range/status label (for example “Items 26–50 of 80”); no numbered pages. The caller may omit Next when the service confirms there is no next cursor. |

Dialog behavior tests use a minimal `HTMLDialogElement.showModal` polyfill if jsdom does not implement native methods; browser semantics remain native and should be covered in browser QA.

## Migration notes

Existing `Button`, `Input`, and `Label` remain shadcn/ui-derived copied primitives; Button/Input now use role tokens and shared focus color. Added cohesive presentation primitives without a runtime dependency. Login's existing notice is the first migrated real use; layout and authentication behavior are unchanged. Existing global `.app-frame`/screen layout stays owned by later slices; this foundation does not replace shell structure. `settle` remains with the global reduced-motion rule. ADR 0003 records shadcn/ui source and MIT license; new copied/adapted files identify origin in their headers. No new font, icon or npm dependency.

## Sample screen evidence

Production-build captures use the opt-in `ux-audit.spec.ts` through `scripts/e2e-smoke.sh`, with isolated PostgreSQL and the pinned Playwright browser. Before captures came from a temporary `origin/main` worktree; after captures came from this branch. Commands: `source ~/.config/tendo-test/env && TENDO_UX_AUDIT_DIR=/tmp/opencode/v306-before-captures bash scripts/dev-exec.sh -- bash -c 'cd /tmp/opencode/v306-before && bash scripts/e2e-smoke.sh'` and `source ~/.config/tendo-test/env && TENDO_UX_AUDIT_DIR="$PWD/docs/design/v3-design-system/capture-after" bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` (credentials redacted). Files: `login-before-390-en.png`, `login-before-1440-en.png`, `login-after-390-en.png`, `login-after-1440-en.png`, `login-after-390-cs.png`, and `login-after-390-en-dark.png`; dark theme is forced on `<html>` only for that screenshot. Each was captured at the named viewport using the production binary and app.
