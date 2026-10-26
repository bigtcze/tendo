# V2.1 maintainability audit

Status: in progress — baseline inventory; refactors pending. Snapshot and rankings are a measured triage of the current worktree; they identify review/refactor targets, not a mandate to split cohesive code solely to reduce line counts. Policy: `.autonomous/CODE_QUALITY.md` and `.autonomous/V2_1_SCOPE.md`.

## Inventory and measurement

- Production inventory excludes tests, generated code, immutable numbered SQL migrations under `backend/internal/platform/database/`, and test-only support. Test-support entries (including `itemtest`, `subjecttest`, `testpostgres/fixture.go`, `testoidc`, `backend/test/oidc-provider`, `frontend/src/test/`, and `frontend/e2e/` helpers) remain named where metrics trigger but are not ranked as production hotspots. `frontend/src/components/ui/` utility-class primitives and `HomeScreen` have separate design-system and frontend-home ownership rather than backend/test ownership.
- Other authored inventory includes frontend `src` TypeScript/TSX, API and scripts/deploy sources. Migration history remains immutable; generated sources remain subject to generation-drift checks.
- Go function measurements use `backend/tools/funcmetrics`: source lines, branch complexity, and maximum control-flow nesting. ESLint measurements use the current pinned frontend dependencies; per-file exceptions reflect the worst function reported for each rule.

## Ranked hotspots

P1 items are V2.1 refactor targets. Metrics are current measured values unless identified as a file-level inventory size. Grouped functions indicate the highest measured functions in each area.

| Rank | Area and measured signal | Rationale / focused direction |
|---|---|---|
| 1 P1 | `frontend/src/features/items/ItemsScreen.tsx`: 567 non-blank lines; `ItemsScreen` 481 lines / complexity 76 | Home list, paging, creation/recovery, completion/undo, focus and view rendering converge in one component. Extract cohesive data/paging and mutation hooks plus focused view components; retain screen composition and characterize behavior. |
| 2 P1 | `frontend/src/features/subjects/SubjectsScreen.tsx`: 669 non-blank lines; `SubjectsScreen` 605 lines / complexity 61 | Subject paging, membership, archive/restore, create/edit recovery, focus and rendering overlap. Extract focused hooks/components while preserving keyboard and data behavior. |
| 3 P1 | `backend/internal/item/item.go`: 875 non-blank lines; `Service.List` 88 / complexity 31, `Service.Create` 89 / 25, `Service.Update` 78 / 22 | Domain definitions, validation, list/cursor and lifecycle behavior share a large file with high churn and user-data impact. Split into cohesive same-package files; do not change API or persistence semantics. |
| 4 P1 | `frontend/src/features/items/ItemDetailScreen.tsx`: `ItemDetailScreen` 294 lines / complexity 90; `saveEdit` complexity 35 | Detail/history, conflict-aware editing, mutations, focus and rendering are coupled. Extract edit/conflict and history responsibilities with API and E2E behavior characterized. |
| 5 P1 | `backend/internal/identity/oidc.go`: `OIDCService.Callback` 108 lines / complexity 40; `Start` 63 / 22 | Security-sensitive callback combines state, provider, identity, account and session outcomes. Extract focused helpers without obscuring validation order. |
| 6 P1 | `frontend/src/features/items/AllItemsScreen.tsx`: `AllItemsScreen` 278 lines / complexity 53 | Filtering, URL synchronization, catalogue loading, paging and rendering are coupled. Extract URL/paging logic and focused filter/list views. |
| 7 P1 | `backend/internal/platform/config/config.go`: `LoadFrom` 104 lines / complexity 53; `backend/internal/platform/httpx/jsonapi.go`: `decodeObject` 89 / 34 | Configuration validation and shared JSON boundary have security and fan-in impact. Extract staged validation/decoding while preserving fail-fast order and error contracts. |
| 8 P1 | `frontend/src/features/members/MembersScreen.tsx`: `MembersScreen` 271 lines / complexity 38 | Membership, invitations, pagination, recovery and focus behavior share a screen. Separate cohesive member and invitation flows; preserve authorization and recovery semantics. |
| 9 P2 | `backend/internal/item/completion.go`: `Service.Complete` 81 lines / complexity 24; `backend/internal/item/postgres/completion.go`: `decodeReceipt` complexity 27 | Completion lifecycle and persistence paths warrant focused review because of recurrence and data-integrity consequences. |
| 10 P2 | `backend/internal/item/httpapi/item.go`: file/function threshold exceptions; handlers translate list/create/update and domain errors | Keep transport concerns cohesive while extracting request parsing or response mapping where it improves clarity without contract changes. |

Other measured candidates include `backend/internal/platform/config/oidc.go:loadOIDC` (complexity 40), `backend/internal/platform/httporigin/origin.go:ParseAuthority` (35), `backend/internal/platform/httpx/origin.go:requestOrigin` (34), `frontend/src/features/items/ItemForm.tsx:ItemForm` (118 lines / complexity 101), `frontend/src/features/auth/OnboardingScreen.tsx` (191 lines), and the operational scripts `scripts/dev_toolchain.py` and `scripts/e2e-native-runner.py`. Prioritize by security/data/user risk and meaningful extraction, not metric rank alone.

## Exceptions and ratchet

Named Go/file metric exceptions are in [`ci/maintainability-baseline.json`](../../ci/maintainability-baseline.json); frontend per-file worst-value exceptions are in [`frontend/maintainability-baseline.json`](../../frontend/maintainability-baseline.json). Each entry records an allowed current value, technical rationale, and owning domain. Test-support entries document test doubles but are excluded from the production hotspot ranking.

The permanent CI/local ratchet checks tracked authored file size and long lines, Go function size/complexity/nesting, and frontend ESLint complexity/depth/function lines. New violations and worsened values fail; stale entries, missing targets, duplicate entries, and missing rationale/owner fail. Exceptions may only tighten or be removed. Generated sources, test suites, fixture data, and numbered embedded migration files (`backend/internal/platform/database/NNN_*.sql`) have explicit exclusions; none is a blanket application-code suppression.

Refactor evidence, before/after metrics, and relevant characterization tests should be added as each ranked target is addressed. This audit remains in progress until scoped high-risk hotspots are refactored and verified.
