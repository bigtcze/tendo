# V2: Professional household UX

status=locked_human_authorized
release_target=v2.0.0
focus=polish_information_architecture_task_discovery_scale_and_accessibility

## Product outcome
Tendo must feel like a professionally designed, trustworthy consumer household app,
not an AI-generated template, enterprise dashboard, or developer tool. Preserve the
V1 product and API behavior, all household boundaries, recurrence semantics, and
Czech/English support. This release is a deliberate GUI/UX upgrade, not new modules.

## User journeys and design standards
1. Start by auditing the existing frontend, browser journeys, actual screenshots,
   usability bottlenecks, and server list/API semantics. Document a short prioritized
   plan based on evidence, not subjective styling guesses. Use independent review.
2. Home answers "What needs my attention?" instantly. Surface urgent/relevant items
   first, with in-progress/waiting clearly separated. Upcoming, paused, completed,
   archived, and the full backlog must remain discoverable without noise.
   Never hide or drop items merely to make a screen look clean.
3. Make hundreds to thousands of items usable: discoverable search, useful filters,
   intuitive groupings, clear result counts/empty states, stable cursor pagination,
   and incremental rendering/loading. Search and filters MUST operate on the complete
   matching household collection, not only an arbitrary loaded page. Prefer
   server-side querying/indexing where needed; preserve API compatibility.
   Avoid eager fetching of all records, unbounded DOM, N+1 requests, and unstable
   ordering; measure realistic large-backlog behavior before optimizing.
4. Keep the primary add/complete/edit/undo paths short and self-explanatory.
   Progressive disclosure for infrequent settings and history; one clear primary
   action per context. Preserve Repeat vs Fluid distinction and all existing
   concurrency/idempotency/undo safeguards. Never sacrifice discoverability.
5. Harmonize typography, spacing scale, semantic color tokens, contrast, alignment,
   form controls, buttons, navigation, feedback, empty/loading/error states,
   hierarchy, and touch targets. Extend existing React/shadcn/Tailwind foundations;
   avoid fashionable gradients, glassmorphism, decorative cards, gratuitous icons,
   fake analytics, animation, and redesign-by-library. No UI slop.
6. Design mobile, tablet, desktop as one responsive product, including small phones,
   touch, keyboard, long Czech strings, zoom and reduced motion. Respect WCAG 2.2 AA
   wherever applicable. Accessible names, focus management, screen-reader announcements
   and error recovery are real behavior, not box-ticking.
7. Apply the same design language to onboarding/login, home, list/detail/add/edit,
   people and things, household members/invitations, and account settings.
   Keep advanced functionality quiet without making it unreachable.
8. Be conservative with domain/backend changes. Add only the API fields/query support
   demonstrably needed for reliable scale/UX; document and test OpenAPI, database
   indexes, pagination stability, isolation and authorization, and migration safety.
9. Evaluate real production-built pages with Playwright and representative content:
   empty, 1 item, 30 items, 500 items, 1000+ items, long titles, all states,
   recurring and one-off items, different users, Czech and English. Review screenshots
   at mobile/tablet/desktop widths; fix visual/usability issues, not just snapshots.
10. User-facing copy must be simple, non-technical, non-judgmental, and localized.
    Keep the V1 product principle: complexity lives in the system, not the interface.

## Review references
Use W3C WCAG 2.2 AA (https://www.w3.org/TR/WCAG22/), WAI ARIA Authoring
Practices (https://www.w3.org/WAI/ARIA/apg/), and proven progressive-disclosure
principles (https://www.nngroup.com/articles/progressive-disclosure/).
Apply guidelines to this app's actual journeys; do not copy visual templates.

## Scope exclusion
No AI, document processing, cloud connectors, calendar, notifications, multi-household
UI, unrelated backend features, native mobile app, analytics, gamification, or
framework rewrite. The historical V1 specs remain baseline invariants; the post-V1
ROADMAP does not grant permission to implement unrelated modules.

## Execution
Use the existing orchestrator and single cycle runner. Work as focused vertical PRs,
finish already-open PRs first, verify production UI and tests before merge, keep
STATE/LEARNINGS accurate, and never change the host/controller configuration.
The release is not complete until the v2.0.0 GitHub Release and public GHCR image
are published and validated against the existing release process, and the V2
completion marker has been merged through a separate PR onto green main.
