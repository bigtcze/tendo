# Direction B — Household overview, organised by who and what

branch=`proto/v3-direction-b`
head_sha=see Issue #325 (prototype branch is not merged)

## Intent and interaction
This direction presents household work by person and thing, not as a task manager. Home opens with the local date, the household name and a short serif line ("A little care makes a home."). Below that is a quiet "Add something to remember" opener. Needs-attention items are grouped under subject headings (for example "Anna Nováková") with an outlined Done button on every row. In progress and Waiting are secondary groups. Text links at the top of the list jump to each group and show its count.

Item detail is a dedicated `/items/:id` page, with no side pane. It has one filled primary action (Mark done, then Undo), followed by the state and attention-date facts, quiet Edit and workflow actions (Start, Waiting, Pause), Archive, and History. Edit opens inline on the detail page. All of this uses the existing completion, undo, edit and workflow APIs.

Direction A's layout differs: it uses a calm single list, a bottom tab bar on phones, a left rail and a persistent detail pane on desktop, and modal add/edit dialogs.

## Layout and navigation
- Phone (390): a horizontal header shows Home, All items and People and things (wrapped onto two lines), plus More. More opens Members and Account inline under the header (`navigation-more-390-en.png`). There is no bottom bar. Groups stack in one column.
- Tablet (768): the full five-link horizontal header and a single-column body.
- Desktop (1024/1440): the full five-link horizontal header, a shell up to 1440px wide, Needs attention in the wider left column, and In progress/Waiting in a narrower right column. There is no sidebar or detail pane.

## Visual language
- Display: system serif (Charter / Georgia). Household heading 36px, group heading 24px, subject heading 18px.
- Body: existing system sans at 16px. Row titles are 18px and underlined as links; supporting meta is 16px muted text.
- Palette (`frontend/src/index.css`): paper `#faf7f2`, ink `#25302b`, muted `#5b655f`, sand `#efe9df`, line `#d8d0c3`, accent `#2f6b57`, strong accent `#234f41`.
- Contrast (WCAG relative luminance from these hex values): ink/paper 12.4:1, muted/paper 5.7:1, white/accent 6.0:1, white/strong accent 8.2:1. All text pairs exceed 4.5:1.
- Density (observed in the captures, not a user measurement): at 390×844 the first needs-attention row starts below the fold-top intro and opener, with about 3–4 rows visible. At 1440×844 only 1–2 needs-attention rows are visible above the fold because of the gap described below.

## Visual review / known weaknesses
Orchestrator review of the production-build captures (2026-11-01):
- Desktop Home (`home-populated-1024-en.png`, `home-populated-1440-*.png`): the left column leaves about 200px of empty space between the group links and the Needs attention heading. The left and right grid rows share their height, so the most important group starts low on the page.
- Empty Home (`home-empty-390-en.png`): four empty divider lines render above the "Nothing needs attention right now." card.
- Add form (`add-*`): the opener expands into the existing form inline, and the opener heading disappears. Repeat is a checkbox rather than a switch, and the repeat panel reuses the earlier layout. Fixed vs after-completion explanations appear in English and Czech.
- Detail after Done (`detail-done-undo-*`): Edit and workflow actions are hidden while the item is done, leaving an empty band between dividers.
- Rows: subject grouping applies only to Needs attention. In the secondary groups, metadata wraps onto two lines and Done drops below the metadata at 1024px.
- All items (`all-items-*`) keeps most of the previous filter-form layout. It was not redesigned.
- The header shows the signed-in username (`direction_b_empty` is the capture account), which is long on phones and is truncated.
- No dark theme, no human household testing, and no zoom or very large backlog proof. Screen-reader output of the new grouping was not separately audited.

These are inputs for the V3-05 comparison. They are not fixed in the prototype.

## Test status
Branch head and results are recorded on Issue #325 and the docs PR.
- `npm run lint`, `npm run typecheck`, `npm run build`: pass.
- Vitest full suite: 785/785 pass.
- Required default PostgreSQL E2E (`scripts/e2e-smoke.sh`): pass on the branch head.
- Direction capture (opt-in `TENDO_DIRECTION_CAPTURE=1` with `TENDO_UX_AUDIT_DIR`): pass. All 26 screenshots in this folder come from that run, using the production build, real PostgreSQL and authenticated synthetic households.

## Screenshot index
All links are relative to this README.

| File | Screen | Width | Locale |
|---|---|---:|---|
| [home-empty-390-en.png](home-empty-390-en.png) | Empty Home | 390 | en |
| [home-empty-1440-en.png](home-empty-1440-en.png) | Empty Home | 1440 | en |
| [home-populated-360-en.png](home-populated-360-en.png) | Populated Home | 360 | en |
| [home-populated-390-en.png](home-populated-390-en.png) | Populated Home | 390 | en |
| [home-populated-390-cs.png](home-populated-390-cs.png) | Populated Home | 390 | cs |
| [home-populated-768-en.png](home-populated-768-en.png) | Populated Home | 768 | en |
| [home-populated-1024-en.png](home-populated-1024-en.png) | Populated Home | 1024 | en |
| [home-populated-1440-en.png](home-populated-1440-en.png) | Populated Home | 1440 | en |
| [home-populated-1440-en-full.png](home-populated-1440-en-full.png) | Populated Home, full page | 1440 | en |
| [home-populated-1440-cs.png](home-populated-1440-cs.png) | Populated Home | 1440 | cs |
| [navigation-more-390-en.png](navigation-more-390-en.png) | Phone navigation with More open | 390 | en |
| [add-oneoff-390-en.png](add-oneoff-390-en.png) | Add, one-off default | 390 | en |
| [add-oneoff-1440-en.png](add-oneoff-1440-en.png) | Add, one-off default | 1440 | en |
| [add-repeat-390-en.png](add-repeat-390-en.png) | Add with Repeat | 390 | en |
| [add-repeat-390-cs.png](add-repeat-390-cs.png) | Add with Repeat | 390 | cs |
| [add-repeat-1440-en.png](add-repeat-1440-en.png) | Add with Repeat | 1440 | en |
| [detail-390-en.png](detail-390-en.png) | Item detail | 390 | en |
| [detail-390-cs.png](detail-390-cs.png) | Item detail | 390 | cs |
| [detail-1440-en.png](detail-1440-en.png) | Item detail | 1440 | en |
| [detail-1440-cs.png](detail-1440-cs.png) | Item detail | 1440 | cs |
| [detail-done-undo-390-en.png](detail-done-undo-390-en.png) | Detail after Mark done, Undo offered | 390 | en |
| [detail-done-undo-1440-en.png](detail-done-undo-1440-en.png) | Detail after Mark done, Undo offered | 1440 | en |
| [edit-390-en.png](edit-390-en.png) | Inline edit on detail | 390 | en |
| [edit-1440-en.png](edit-1440-en.png) | Inline edit on detail | 1440 | en |
| [all-items-390-en.png](all-items-390-en.png) | All items | 390 | en |
| [all-items-1440-en.png](all-items-1440-en.png) | All items | 1440 | en |

## Not prototyped
People and things, Members, Account and setup/login routes keep the previous layout. There is no title-first quick composer, no dark theme, and no user approval or participant testing is claimed.
