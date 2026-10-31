# V3 Product Design Brief – Everyday Elegance

status=locked_human_authorized
agent_must_not_edit=this_file
product=family_household_organizer
north_star=effortlessly_usable_by_nontechnical_household_members
visual_direction=calm_warm_elegant_human_consumer_app

## Intent
Deliver an interface a non-technical household member would willingly use
repeatedly, without onboarding to project-management jargon. Think of the
careful restraint and usability of Things 3 and Apple Reminders, but do NOT
clone proprietary layouts, copyrighted artwork, branding or distinctive trade
dress. Tendo must develop its own recognizable, modest product identity.

The desired outcome is NOT an enterprise dashboard, developer tool, or generic
AI-generated UI. It must look cohesive, intentional, mature and crafted.
Do not use a superficial reskin of the existing narrow single-column page.

## Design principles
- Household-first: prioritize what a person needs to remember today and the
  next useful action; people, home, car, pets remain comprehensible.
- Progressive disclosure: first action obvious, advanced recurrence details
  present only when relevant; one-off tasks remain first class.
- Clear hierarchy: a readable type scale, decisive page structure, strong task
  titles, restrained metadata, meaningful group headings, clear affordances.
- Comfortable density: avoid walls of faint small text and repeated oversized
  cards; design for 5 items and 1,000+ without losing scanning/navigation.
- Effective use of desktop/tablet width, rather than a phone-width column in
  a wide empty canvas. Do not make mobile cramped to achieve desktop density.
- Warm, neutral palette and restrained purpose-specific color. Avoid generic
  gradients, neon accents, decorative glassmorphism, random icons, badge soup,
  emoji UI and gratuitous shadows. Color must communicate, not decorate.
- Typographic consistency across body, headings, labels, dates, controls and
  error copy. Human legible at common phone sizes and 200%/400% zoom.
- Thoughtful spacing, borders, elevations, alignment and content rhythm.
- Subtle states and motion, never motion required for comprehension. Honor
  prefers-reduced-motion and accessibility settings.
- Icons have accessible names where actionable; no icon-only critical action
  without an obvious accessible alternative.
- Every interactive state (hover/focus/pressed/disabled/loading/success/error)
  must look deliberate, consistent, and remain usable by keyboard and touch.
- Build a genuinely polished light theme; provide an equally deliberate dark
  theme with OS-following preference and accessible contrast. Theme settings
  should persist and avoid bright flashes on page load.
- Czech and English copy should be warm, exact and naturally short; no
  exposure of technical terms like ETag, tenant, fluid recurrence or API.
  Explain fixed vs after-completion cadence with clear household examples.
- Minimize taps to add, complete, undo or locate an item; never hide core
  actions behind undiscoverable gestures. Recover gracefully from mistakes.
- Functional simplicity and predictable interactions matter more than visuals.

## Screens that require equal design investment
- First-run setup, sign-in, account, OIDC and meaningful authentication errors.
- Home, needs attention, in progress, waiting, coming up, empty and full lists.
- All items with search, filtering, sorting, paging and 1,000+ records.
- Create/edit/detail, completion/undo, dates, recurrence, history, archive.
- People and things, members/invitations and owner/member permission states.
- Every dialog, drawer, menu, form, toast, error, loading and offline state.
- Mobile, tablet and desktop navigation and interaction flows.

## Design process / feedback loop
1. Audit REAL shipped v2.1 UI using screenshots from the running production
   build at 320/390/768/1440px in light and dark where applicable. Inspect
   real routes, realistic content density and states, not just the home page.
2. Record honest usability/visual problems and baseline task timings, taps,
   overflow, keyboard navigation, legibility, layout and accessibility.
3. Create at least two meaningfully different coherent design directions as
   INTERACTIVE implemented prototypes in the actual Tendo frontend. Use
   authentic Playwright screenshots from the running application and synthetic
   household fixtures; no AI mockups or static fake UI as implementation proof.
4. Review directions independently: readability, user effort, aesthetics,
   keyboard/touch discoverability, household fit and maintainability. Record
   structured pros/cons and a reasoned choice; avoid self-congratulation.
   AI-only critique is not equivalent to actual user approval. Do not claim
   real household testing without real participants.
5. Commit the selected design principles, measured layout/density, tokens and
   component patterns before broad implementation. Keep decisions pragmatic.
6. Implement through small reviewable PRs; take authentic before/after
   screenshots for each major screen and critique in context. Fix discovered
   aesthetic AND functional problems before moving to the next slice.
7. Perform independent final end-to-end UX review: a beginner should be able
   to add a one-off item, set a recurring task, see what needs attention,
   mark done/undo, search old items and invite a household member without
   understanding internal domain vocabulary.

## Non-negotiable boundaries
- Existing product semantics, authorization, recurrence, household scoping,
  APIs, persistence, accessibility and V2.1 regressions must remain correct.
- Do not add speculative features, charts, AI, notifications, calendar sync,
  native app, or an enterprise admin dashboard to "improve" aesthetics.
- Reuse existing React, Tailwind and dependencies if practicable; avoid
  wholesale component-library replacement solely for a fresh look.
- Keep .autonomous/CODE_QUALITY.md mandatory: no new tangled giant components.
- A screenshot or clean test result is evidence, not proof of design quality.
