# V2 UX and scale audit

status=baseline_2026-10-16
source_commit=e802241 (v1.0.0 + V2 authorization)
evidence=opt-in production-build capture `TENDO_UX_AUDIT_DIR=… scripts/e2e-smoke.sh` (see [testing](testing.md#uxscale-audit-capture)); real PostgreSQL 18, fixed clock 2026-05-14 Europe/Prague
reviews=designer screenshot review; oracle list/search/count API design (decisions below)

This is the evidence-based starting point required by V2_SCOPE step 1. It records measured problems and the
ordered plan. It is not a specification; each slice is defined and tested in its own PR.

## Dataset

Seeded through the public API only: two members (owner + invited member), 15 subjects (Czech diacritics,
some archived), cumulative stages of 0 / 1 / 30 / 500 / 1200 items with a deterministic generator mixing
one-off, fixed and fluid recurrence (day/week/month/year), past/future/no attention dates, historical
completion dates, owner/member/no assignee, ~5 % long titles with long unbreakable Czech words, notes.
Final 1200 state verified by a full API page walk: 848 open, 71 in progress, 59 waiting, 94 paused,
67 done, 61 archived.

## Measurements (home, en, 390 px)

| Stage | API requests | Response bytes | Time to content | DOM elements | Rows rendered |
|---|---:|---:|---:|---:|---:|
| empty | 5 | 3.4 kB | 0.62 s | 35 | 0 |
| 30 | 5 | 14 kB | 0.74 s | 222 | 24 |
| 500 | 5 | 49 kB | 1.12 s | 791 | 100 |
| 1200 | 5 | 49 kB | 1.01 s | 791 | 100 |

"Show more" at 1200: 10 clicks to reach all 1071 active rows; DOM grows to 8037 elements and the page to
~128 000 px; each click 120–400 ms and growing. Request count per load is bounded (session, household, item
page of 100, active and archived subject loops); subjects are fetched eagerly in up to 20 pages per state.

## Findings (prioritized)

P0 = blocks a V2 DoD item; P1 = significant usability or quality problem; P2 = polish.

| ID | Pri | Finding | Evidence |
|---|---|---|---|
| F1 | P0 | Home groups only the loaded pages: the first 100 items in **creation order**, so "Needs attention" at 500+ items shows an arbitrary subset and groups below it are untruthful until every page is loaded. | `items?limit=100` ordered by id; `home-500-en-390-fold.png` |
| F2 | P0 | No search, filter or counts anywhere; finding an item among 1200 means paging blindly (10 clicks, 128 000 px). | `home-1200-en-*-expanded.png`, Show-more metrics |
| F3 | P0 | Done and archived items are not reachable in the UI at all (no list ever requests `done=true` or `archived=true`); only a deep link to the detail page reaches them. | `itemsApi.ts` |
| F4 | P0 | Unbounded DOM growth: "Show more" appends without limit (8000+ elements). | Show-more metrics |
| F5 | P1 | Long unbreakable titles overflow horizontally at **every** width (spans 912 px wide at 390 and 1440). | `metrics.json` overflow offenders, `home-1200-en-390.png` |
| F6 | P1 | Rows show no attention date or relative timing and no assignee; order inside a group gives no urgency signal. | `home-1200-en-1440-fold.png` |
| F7 | P1 | Every row carries a filled primary "Done" button; competes with "Add item" and makes long lists heavy. | `home-500-en-390-fold.png` |
| F8 | P1 | Navigation to People & things / Members / Account exists only on home; other screens offer only "Back to home". | `audit-people-things-en-1440.png`, `audit-members-en-390.png` |
| F9 | P1 | Home eagerly loads all subjects (active and archived, up to 2 × 20 pages) just to label rows. | network log per load |
| F10 | P2 | Large display heading and generous gaps push items below the fold on phones; spacing and type scale are not systematized (only colour/font tokens exist). | `home-1200-en-390-fold.png`, `index.css` |
| F11 | P2 | Page structure differs between People & things, Members and Account (width, heading, action placement). | `audit-*-1440.png` |
| F12 | P2 | Group headings carry no counts; "Show more" does not say how much remains. | home screenshots |
| F13 | P2 | No automated accessibility scan exists; focus and contrast are covered only by targeted unit/E2E assertions. | `frontend/e2e` |

Not a problem: no horizontal overflow at 320–1440 other than F5; Czech strings fit at 320 px in the captured
screens; request count per load is constant; detail, edit and history pages work at 390 px.

## Decisions

Server-side list contract (oracle): additive, backward-compatible parameters on
`GET /households/{id}/items` — `q` (title + notes, case/diacritic-insensitive literal substring),
`subjectId`, `responsibleUserId` (UUID or `unassigned`), `workflowState`, `attention`
(`needs_attention`/`upcoming`, computed in SQL from household-local today), `recurring`, `sort`
(`created` default, `attention_on` = `attention_on ASC NULLS FIRST, id ASC`) and an optional
`attentionDate` freshness guard. V1 queries keep the `i1` cursor and identical results; extended queries
use a versioned composite cursor bound to household, filter fingerprint and date (mismatch →
400 `invalid_query`). Counts come from a separate `GET …/items/summary` with the same filters, one
aggregate statement, partitioned into needs_attention / in_progress / waiting / upcoming / paused / done /
archived. Search uses `pg_trgm` + `unaccent` (both available in the pinned PostgreSQL 18.6 image) with a
trigger-maintained normalized column and GIN index; the runtime role gets no new privileges.
Pagination is deterministic keyset traversal for unchanged rows, not snapshot isolation.

Design direction (designer): keep the warm paper palette and serif display face; add a type and spacing
scale and semantic status text colours (AA-checked against `#faf7f2`, always paired with text labels);
one filled primary action per context; quieter row completion; consistent household navigation on every
route.

## Plan (ordered PR slices)

1. Audit harness + this document (this PR).
2. Backend: structured list filters + extended cursor (no migration), OpenAPI, real-PG tests.
3. Backend: attention sort + date guard + migration 015 indexes.
4. Backend: items summary endpoint (counts).
5. Backend: text search (extensions, normalized column, GIN).
6. Frontend: server-backed attention-first home with truthful counts and bounded per-group pages; F5, F6, F7.
7. Frontend: "All items" search/filter view incl. done and archived, URL-preserved context, bounded rendering (F2–F4, F12).
8. Frontend: shared navigation and design tokens across every route (F8, F10, F11); subject labels without eager loading (F9).
9. Accessibility: automated axe checks in production-build E2E plus keyboard/zoom/reduced-motion review (F13).
10. Large-dataset production E2E in required CI (1000+ items), docs, release v2.0.0.

The audit is re-run after slices 6–8 and the measurements above are replaced, not appended.
