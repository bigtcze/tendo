# UX contract

status=canonical_version_independent
source_of_truth=this_file
target=consumer_family_app
style=calm_simple_responsive_professional
reference=mature_consumer_task_apps_and_disciplined_admin_UI_principles_only; no cloning of proprietary design
native_mobile_app=no
visual_direction=this_file "Visual direction"

## North star
Complexity belongs in the system, not in the interface. Tendo is a household tool, not work software. A normal user does not need to understand recurrence engines, states, APIs or integrations.

## Visual direction
- This contract incorporates the owner-authorized refined household visual direction and governs every new visual decision. Earlier briefs and reference images remain historical context in Git only, never current visual targets or production evidence.
- Older palette, font and layout values (for example the V4 Inter/petrol-teal reference palette) are historical. They are not new targets. A future change of them needs its own reviewed decision.
- Tokens: a small shared system. Typography, spacing scale 4/8/12/16/24/32, color roles (text, surface, border, accent, semantic statuses), radius, focus, hover, pressed, disabled, loading, error and success states, system/light/dark.
- No gradients, glassmorphism, decorative cards, KPI tiles, charts, gamification, emoji UI, random icons, badge clutter, fake analytics or "generic AI dashboard" visuals.
- Color communicates status. Status is always also conveyed by text, icon or shape.

## Shell and navigation
- Full-viewport top bar spanning the complete width. Starting target ~60–64px; accessibility beats the exact height.
- Top bar contains: Tendo brand, an accessible active-household selector, and account or settings access. Nothing else.
- Practical sidebar on desktop (~220–240px). Navigation: Home, All items, Archive, People and things, Members, Account, and Admin center when authorized.
- Phones: Archive must stay discoverable without overloading the bottom bar. Core destinations stay reachable on narrow and wide layouts alike.
- Main content insets: 24–32px on desktop, 16px on phones. No buttons flush against panel edges. No random huge voids.
- Compact page title with the primary action on the same line. Remove the existing promotional "DOMOV POD KONTROLOU.", "VÁŠ DOMOV · PŘEHLED", oversized household heading, decorative period and marketing tagline from Home; do not add replacements. Household identity belongs in the practical header selector.
- Apply this same professional visual language across EVERY route and state: Home, lists, forms/detail/history, people/things, members/invitations, setup/login/OIDC, account/password, household selector, admin center, MCP settings, dialogs and error/empty/loading/permission/offline states. No old unstyled corners.

## Home
- Shows what needs attention now, then in-progress and waiting groups. Upcoming follows. Other states remain reachable.
- Grouped, scannable rows with: aligned title, contextual subject, relevant due date, responsibility, recurrence summary when relevant, and a Done action.
- Use plain wording. Avoid repeated status words and pills on every row. Do not hide vital facts to save space; wrap honestly on small screens.
- No KPI dashboard, charts, productivity score, activity wall or admin-console chrome.

## Create and edit item
Default path: 1 title, 2 subject, 3 optional attention date, 4 Repeat toggle, 5 save.
- Repeat is an ordinary toggle, OFF by default. OFF is a complete one-off item.
- Repeat ON reveals the interval and a second toggle: "Count the next repeat from when I complete this". Fluid is OFF by default.
- One short contextual example explains fixed vs after-completion cadence. Never a settings essay. Advanced or rare fields stay collapsed.
- Responsible member and notes are optional and visually secondary.
- A date is not required when the user means "now".

## Completion and undo
- Done is one fast action. Undo is immediately available. No heavy confirmation modal.
- Destructive actions may require confirmation.
- Errors and recovery are explicit and actionable.
- Optimistic UI only when failure and rollback are safe and visible.
- Recurrence result and undo semantics: RECURRENCE.md.

## History
- Item detail shows completion history, subordinate to the main content.
- History uses straightforward paginated lists (for example 20 entries per page, Previous and Next, stable ordering, accessible current page). No infinite scroll, virtual timeline or complicated timeline UI.
- Server cursor pagination is acceptable internally.

## Scale
- Realistic states to verify: empty, 1, 10, 100, 1000+ items.
- Search, filters and sort operate on the complete matching dataset, not the loaded page (API.md "Collections").
- Practical performance with 1000+ items on ordinary commodity and mobile devices: no unbounded DOM, no eager fetching of all records, no N+1 requests.
- Measure realistic large-backlog behavior before optimizing.

## Language
- Czech and English. Strings in locale resources. Long Czech names, diacritics and long translated strings must not clip or truncate surprisingly.
- Dates are localized.
- Human wording only. Never expose ETag, tenant, fluid recurrence, API, policy or workflow-engine vocabulary.
- Visible states: "Needs attention", "In progress", "Waiting", "Paused", "Done".
- Never shame: no "OVERDUE 47 DAYS". Prefer "It has been 13 months since the last visit."
- Examples:
  - good: "It is time to book Anna's dental check."
  - good: "Repeat every 12 months."
  - good: "Count the next 12 months from when I complete this."
  - bad: "Task overdue by 31 days." / "SLA breached."

## Themes and motion
- Light, dark and system themes are all intentionally designed and persist the saved preference.
- No bright flash on first paint: the correct theme is applied before render.
- Motion is subtle and never required for comprehension. Reduced motion is honored.

## Accessibility
- WCAG 2.2 AA-oriented: contrast (normal text 4.5:1, large text and non-text controls and focus 3:1), keyboard, logical focus order, visible focus, landmarks and semantic controls.
- Touch targets are at least 44px unless a justified accessible exception applies.
- 200% and 400% zoom work with no horizontal overflow and no clipped actions.
- Forms have accessible names, errors are associated with fields, and error recovery is clear.
- Actionable icons have accessible names. No icon-only critical action without an obvious accessible alternative.
- Screen-reader announcements for mutations.
- Reduced motion is honored.
- Automated checks plus meaningful manual or keyboard review. AI review is never presented as a human accessibility audit.

## Responsive
- One responsive app serves phone (320/360/390), tablet (768/1024) and desktop (1440/1920).
- Design phone, tablet and desktop as one product. Do not design desktop first and shrink it.
- Do not make mobile cramped to achieve desktop density. Do not leave a phone-width column in a wide empty canvas.

## Production screenshot workflow
- Screenshots are evidence of the IMPLEMENTED production build only, using synthetic fixture data. Never concept art, AI mockups or generated UI.
- Before the broad design implementation, audit the real current production UI at 320/390/768/1024/1440/1920px, in Czech and English, light and dark.
- Build at least two distinct, FUNCTIONAL, interactive directions in the real frontend covering Home, create/edit/detail, navigation, the admin screen and a phone width.
- Evaluate each direction independently for usefulness, accessible density, professional polish and responsive behavior. Record the comparison and the reasoned choice in a committed review.
- AI-only critique is not human approval. Do not claim human approval, real household testing or aesthetic certainty without a human action.
- Each shipped slice takes real before and after browser screenshots and fixes the problems found before the next slice.
- Screenshot similarity is never the sole approval. Visual regression tests protect layout; they do not approve design.
- Obtain independent senior-designer skeptical review plus user journeys in the REAL production browser; actually fix findings. A beginner must be able to add a one-off item, set recurrence, see what needs attention, Done/Undo, search and invite a member without instruction or internal vocabulary. Never call AI-only review human testing.

## Interaction principles
- One obvious primary action per context.
- Common actions take very few taps.
- No hidden gesture as the only path to a core action.
- Settings are secondary and quiet but reachable.
- Empty, loading and error states are simple and actionable.
- Advanced functionality is quiet, not unreachable.

## Starter and components
- Extend the existing React, Tailwind and shadcn-style foundation. No wholesale component-library replacement for a fresh look.
- No dense admin-dashboard template.
- Shared components are generic presentation primitives.
- Starter or template source and license are documented.

## Links
- Product behavior: PRODUCT.md
- Recurrence wording and mechanics: RECURRENCE.md
- Code structure of UI: CODE_QUALITY.md
- Visual regression and accessibility tests: TESTING.md
- UX acceptance: DEFINITION_OF_DONE.md "User experience"
