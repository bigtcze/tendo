# UX Principles

status=locked_for_v1
target=consumer_family_app
style=calm_simple_responsive
reference=Apple_like_simplicity_not_visual_clone
native_mobile_app=no

## North star
Complexity belongs in the system, not in the interface.

## Product feel
- Household tool, not work software.
- Calm, obvious, forgiving.
- A normal user should not need to understand recurrence engines, states, APIs, or integrations.
- Prefer recognition over configuration.

## Responsive web
- One responsive web application serves phone, tablet, and desktop.
- No native mobile app is planned for V1.
- Core functionality must be equally reachable on narrow and wide layouts.
- Do not design desktop first and merely shrink it later.

## First-run
- Ask for account details/authentication.
- Ask: household/family name.
- Pre-fill detected timezone and allow correction.
- Do not show household IDs, tenant concepts, membership schemas, or a household switcher.
- Enter a useful empty home screen with one obvious "Add item" action.

## Home
- Show only what deserves attention now plus compact in-progress/waiting sections.
- No KPI dashboard, charts, productivity score, activity wall, or admin-console chrome.
- Upcoming/everything else is available but subordinate.

## Create/edit item
Default path:
1=title
2=subject
3=optional_when_to_start_attention
4=repeat_toggle
5=save

Repeat behavior:
- `Repeat` is a normal toggle, OFF by default.
- OFF leaves a fully valid one-off item.
- ON reveals interval.
- ON also reveals a separate fluid-recurrence toggle.
- Human copy example:
  "Count the next repeat from when I complete this"
- Fluid OFF = keep fixed cadence.
- Fluid ON = next cycle starts from completion.
- Explain the difference with one short contextual sentence/example; never a settings essay.
- Advanced/rare fields stay collapsed.

Other:
- Assignee/notes optional and visually secondary.
- Do not require exact date if user wants "now".
- Do not force recurrence fields for one-off item.

## Language
- Human wording; never Jira/admin wording.
- Never shame: avoid "OVERDUE 47 DAYS".
- Prefer "It has been 13 months since the last visit."
- Visible states: "Needs attention", "In progress", "Waiting", "Paused", "Done".
- Avoid "recurrence policy", "SLA", "derived state", "workflow engine", "tenant".

## Interaction
- One obvious primary action per card/screen.
- Completion fast with easy undo, not a heavy confirmation modal.
- Destructive actions may require confirmation.
- Common actions require very few taps.
- History available but visually subordinate.
- Settings secondary and quiet.
- No hidden gesture as the only path for a core action.
- Empty/loading/error states simple and actionable.
- Optimistic UI only when failure/rollback is safe and visible.

## Visual
- Whitespace > dense tables.
- Responsive layouts mandatory.
- No large enterprise sidebar/navigation tree.
- No gamification, streaks, points, confetti, badges.
- No charts in V1.
- Accessible contrast, semantic controls, keyboard support, reduced-motion support.
- Touch targets suitable for phone use.

## Visual implementation
- Start from a mature, maintained, permissively licensed React/shadcn-style component foundation or consumer-oriented starter.
- Do not spend early cycles recreating generic buttons/modals/forms/layout primitives.
- Do not adopt a dense admin-dashboard template.
- Record starter/template source + license.
- Theme lightly; usability beats branding work in V1.

## Example tone
good="It is time to book Anna's dental check."
good="Last completed 12 months ago."
good="Waiting: appointment booked."
good="Repeat every 12 months."
good="Count the next 12 months from when I complete this."
bad="Task overdue by 31 days."
bad="SLA breached."
