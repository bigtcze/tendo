# V4-03 shell and Home

status=V4-03_implemented
issue=#411 (V4-03)
direction=`.autonomous/V4_DESIGN_BRIEF.md` (desktop/mobile references are visual targets only)
tokens=`v4-design-system.md` (V4-02)

## What changed

- **Top bar** ~56 px: teal rounded house tile, sans "Tendo" wordmark and a small
  "Home, handled." tagline (hidden on phones). Signed-in desktop shows
  "Signed in as …", an avatar initial and Sign out; phones show the avatar
  account affordance plus a compact sign-out control with the same behaviour
  and error alert.
- **Rail** 224 px from `lg`: "WORKSPACE" label, the five destinations with
  20 px outline icons, `bg-nav-selected` for the current page (icy blue, not a
  saturated block). The five-tab bottom bar is unchanged in meaning and keeps
  its short-viewport static and safe-area behaviour. Language switching stays
  in the quiet footer (and on Account).
- **Main** uses the available width; the shell frame stays capped at 90 rem.
- **Home header band** shares one row on desktop: eyebrow
  "YOUR HOME · OVERVIEW" (phone: "YOUR HOUSEHOLD"), 36–40 px household headline
  with an `aria-hidden` accent period, supporting sentence and the teal
  "Add item" action at the right (full width above the pills on phones). The
  heading's accessible name is exactly the household name; `Heading` keeps the
  document title free of the decorative period.
- **Status pills** use the V4 semantic pairs: warm coral Needs attention, amber
  Waiting, neutral cool In progress/Coming up (and Paused when it has rows).
  They remain in-page links that focus the matching group heading, scroll
  horizontally inside their own row and never widen the page.
- **Groups and rows**: semantic dot + heading + count pill, restrained static
  context on the right. One white hairline-divided surface per group. From
  `xl` rows use aligned columns: real subject-type icon tile → selectable title
  link with metadata → compact mint subject pill → date/assignee column →
  outlined "✓ Done". At `lg` (1024 px) the same three-part grid is used with
  the facts stacked in the metadata block, so titles keep their width. Phones
  keep the same DOM: icon, title, subject pill + metadata, and a 44 × 44
  outlined check whose accessible name stays `items.done.label`.
- **Metadata is complete at every width.** Attention status, last completion,
  recurrence summary (interval + fixed/after-completion mode) and the
  responsible member are independent facts and are all rendered. Below `xl`
  they stack in the order time/attention (semantic colour + icon), responsible
  member, then recurrence/last-done in muted text; from `xl` the right column
  carries the date/assignee while the metadata line keeps recurrence/last-done
  plus whichever of responsible or attention is not already in that column.
  Nothing is suppressed. The normal title link keeps its text selectable; its
  one-line layout box carries a 44 px pointer target through compensated
  vertical padding. A one-line row measures 56 px at 1440.

## Deliberate deviations

| Deviation | Reason |
| --- | --- |
| Icon tiles derive from the real subject type (person/home/vehicle/pet/generic custom), not invented topic categories | The API only stores subject type; no topic data exists. |
| Paused stays as a fifth pill/group, rendered only when it has rows | Paused is a real lifecycle state; the reference fixture happens to have none. |
| The rail's "YOUR HOUSEHOLD" name footer is omitted | It is not part of the session payload; adding it to the shell would cost an extra household request on every screen. The large-data gate bounds initial Home requests (currently 7). |
| Phone "View all" appears only when the existing see-all condition is met (a group overflows the 100-row cap) | Reuses the existing `/items?status=<group>` contract without adding per-group links that would duplicate the current navigation and break the bounded-list focus rules. |
| One supporting sentence for both widths instead of two reference variants | Avoid duplicate text in the accessibility tree; the sentence is short and works at both sizes. |
| Phone top bar keeps a compact sign-out next to the account affordance | Sign out is only in the desktop header on Account, so the phone bar keeps it directly reachable. |

## Reference-fidelity checklist (a)–(g)

| Check | Result | Evidence |
| --- | --- | --- |
| (a) Font, optical weight, navy hierarchy | Pass: Inter Variable, 700 display/H1, 600 row titles, ink `#16283C`. | `home-reference-en-light-1440.png`, `home-reference-en-light-390.png` |
| (b) Cool off-white canvas, white surfaces | Pass: `#F1F5FA`, white grouped surfaces, 1 px `#D7E2EB` dividers. | same captures |
| (c) Petrol-teal CTA, coral/amber/teal status semantics | Pass: teal Add item; coral attention pill/row status, amber Waiting, teal In progress, mint Coming up. | `home-reference-en-light-1440.png` |
| (d) Spacing and compact grouped rows | Pass: 56 px top bar, 224 px rail, single header band; aligned columns from `xl`, a compact three-part row at `lg`/1024 px, no isolated cards. The measured 1440 one-line row is 56 px; rows grow only when several real facts wrap. | `home-reference-en-light-1440.png`, `home-reference-en-light-1024.png`, `home-reference-en-light-768.png` |
| (e) Navigation / bottom bar | Pass: rail with WORKSPACE + icy selected state; phone bottom bar exactly Home/Items/People/Members/Account with safe-area padding. | `home-reference-en-light-1440.png`, `home-reference-en-light-390.png` |
| (f) Icon/tile styling and focus | Pass: restrained semantic icon tiles, outlined Done, 44 px phone check, visible focus rings. | `home-reference-en-light-320.png`, `home-reference-cs-light-390.png` |
| (g) Real data accuracy | Pass: the UI shows the ten approved tasks, heading counts 4/2/1/3 (Paused 0), resolved subject pills and no loading/unknown labels; date and responsible text are asserted visible at 390 px in English and Czech. Dark mode uses the same checks. | all captures in `v4-home/` |

The reference fixture is seeded through the real API by
`frontend/e2e/referenceFixture.ts`. Before every capture series (EN light at
each width, Czech, dark) the visual suite asserts from the rendered UI: the
household heading, ten rows, group headings with counts 4/2/1/3, all ten task
titles, resolved subject pills (Home, Rex, Tom, Anna, Škoda Octavia) and no
loading/unknown labels. At 390 px it additionally asserts with `toBeVisible()`
that Tom's attention status and responsible member and the car row's
date/assignee are actually visible, in both English and Czech, not merely
present in the DOM. At 320, 390, 1024 and 1440 it checks the title target is
at least 44 px tall, wins `elementFromPoint` at its top+2 and bottom−2 edges,
and starts inside the group panel. It also clicks the title (detail
navigation), goes back, clicks Done (no navigation), and verifies Undo
restores the row. Committed images are production Playwright captures, not
mockups or reference-derived composites.

## Evidence

| Capture | Purpose |
| --- | --- |
| [en light 1440](v4-home/home-reference-en-light-1440.png) | Desktop reference comparison |
| [en light 390](v4-home/home-reference-en-light-390.png) | Phone reference comparison |
| [en light 320](v4-home/home-reference-en-light-320.png) | Narrow phone, no page overflow |
| [en light 1024](v4-home/home-reference-en-light-1024.png) | Compact lg/1024 layout with all facts |
| [en light 768](v4-home/home-reference-en-light-768.png) | Tablet |
| [cs light 390](v4-home/home-reference-cs-light-390.png) | Czech diacritics and longer labels |
| [en dark 1440](v4-home/home-reference-en-dark-1440.png) | Dark desktop |
| [en dark 390](v4-home/home-reference-en-dark-390.png) | Dark phone |

Gated baselines live in
`frontend/e2e/visual/visual.spec.ts-snapshots/home-reference-*.png`
(plus 360/1024 and Czech 1440 and dark variants).
