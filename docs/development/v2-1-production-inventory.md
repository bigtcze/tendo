# V2.1 hand-written production code inventory

Regenerate with `bash scripts/dev-exec.sh -- python3 scripts/production_inventory.py --out docs/development/v2-1-production-inventory.md`.
Snapshot at Git HEAD `c82f572d81812c06ed9024549f2d562a36d1656b`. The maintainability ratchet (`scripts/maintainability_check.py`) checks the live tree on every PR.
Go files have function-level metrics. Frontend TypeScript function rules are enforced by ESLint with `frontend/maintainability-baseline.json` (0 entries). Python, shell and SQL are measured at file level only.
Dispositions are derived from measured values against thresholds and named baseline exceptions; generation fails on any violation.

## Summary

| Area | Production files |
|---|---:|
| backend Go | 78 |
| frontend TS | 148 |
| SQL | 10 |
| script/tooling | 34 |
| **Total** | **270** |

Measured test files: 148; test-support files: 12.

## Measured production files

| Path | Area | Non-blank lines | Long lines | Go functions; max lines; max complexity; max nesting | Disposition |
|---|---|---:|---:|---|---|
| `api/check-generation.mjs` | script/tooling | 91 | 24 | — | baseline exception long_lines measured=24 allowed=24 (tooling-scripts) |
| `api/check-health.mjs` | script/tooling | 331 | 0 | — | within thresholds |
| `backend/cmd/tendo/items.go` | backend Go | 56 | 0 | 6; 13 (itemHouseholds); 3 (itemHouseholds.func1); 1 (itemHouseholds.func1) | within thresholds |
| `backend/cmd/tendo/main.go` | backend Go | 237 | 6 | 17; 51 (registerAPI); 9 (serve); 3 (serve) | baseline exception long_lines measured=6 allowed=6 (backend-platform) |
| `backend/cmd/tendo/subjects.go` | backend Go | 37 | 0 | 4; 13 (subjectMembers); 3 (subjectMembers.func1); 1 (subjectMembers.func1) | within thresholds |
| `backend/internal/household/household.go` | backend Go | 45 | 0 | 2; 15 (Service.Get); 5 (Service.Get); 1 (Service.Get) | within thresholds |
| `backend/internal/household/httpapi/household.go` | backend Go | 58 | 0 | 5; 18 (Handler.get); 4 (Handler.get); 1 (Handler.get) | within thresholds |
| `backend/internal/household/httpapi/members.go` | backend Go | 62 | 0 | 3; 39 (MembersHandler.list); 12 (MembersHandler.list); 2 (MembersHandler.list) | within thresholds |
| `backend/internal/household/membership.go` | backend Go | 133 | 0 | 8; 41 (MembershipService.ListMembers); 11 (MembershipService.ListMembers); 2 (MembershipService.ListMembers) | within thresholds |
| `backend/internal/household/postgres/household.go` | backend Go | 34 | 0 | 2; 17 (Repository.FindForMember); 5 (Repository.FindForMember); 1 (Repository.FindForMember) | within thresholds |
| `backend/internal/household/postgres/membership.go` | backend Go | 75 | 0 | 6; 28 (MembershipRepository.ListMembers); 9 (MembershipRepository.ListMembers); 2 (MembershipRepository.ListMembers) | within thresholds |
| `backend/internal/household/postgres/queries/household.sql` | SQL | 5 | 0 | — | within thresholds |
| `backend/internal/household/postgres/queries/membership.sql` | SQL | 13 | 0 | — | within thresholds |
| `backend/internal/household/postgres/queries/setup.sql` | SQL | 4 | 0 | — | within thresholds |
| `backend/internal/household/postgres/setup.go` | backend Go | 31 | 0 | 3; 10 (BootstrapRepository.AddOwner); 3 (BootstrapRepository.AddOwner); 1 (BootstrapRepository.AddOwner) | within thresholds |
| `backend/internal/household/setup.go` | backend Go | 63 | 0 | 5; 21 (Validate); 8 (Validate); 2 (Validate) | within thresholds |
| `backend/internal/identity/errors.go` | backend Go | 3 | 0 | 0; 0 (—); 0 (—); 0 (—) | within thresholds |
| `backend/internal/identity/httpapi/invitations.go` | backend Go | 233 | 0 | 14; 33 (writeInvitationError); 12 (writeInvitationError); 3 (writeInvitationError) | within thresholds |
| `backend/internal/identity/httpapi/limiter.go` | backend Go | 43 | 0 | 2; 27 (limiter.admitAttempt); 10 (limiter.admitAttempt); 3 (limiter.admitAttempt) | within thresholds |
| `backend/internal/identity/httpapi/oidc.go` | backend Go | 178 | 2 | 13; 35 (OIDCHandler.start); 8 (OIDCHandler.start); 2 (OIDCHandler.callback) | baseline exception long_lines measured=2 allowed=2 (backend-identity) |
| `backend/internal/identity/httpapi/oidc_start.go` | backend Go | 79 | 0 | 3; 27 (OIDCHandler.resolveStartSession); 8 (readOIDCStartRequest); 3 (OIDCHandler.resolveStartSession) | within thresholds |
| `backend/internal/identity/httpapi/session.go` | backend Go | 162 | 0 | 15; 32 (SessionHandler.create); 6 (SessionHandler.create); 2 (SessionHandler.delete) | within thresholds |
| `backend/internal/identity/httpapi/setup.go` | backend Go | 148 | 0 | 9; 63 (Handler.put); 13 (Handler.put); 1 (Handler.get) | within thresholds |
| `backend/internal/identity/invitation_accept.go` | backend Go | 112 | 0 | 4; 59 (InvitationService.AcceptInvitationNewAccount); 10 (InvitationService.AcceptInvitationExistingAccount.func1); 1 (InvitationService.AcceptInvitationExistingAccount) | within thresholds |
| `backend/internal/identity/invitation_accept_helpers.go` | backend Go | 32 | 0 | 3; 13 (checkPendingInvitation); 4 (checkPendingInvitation); 1 (checkPendingInvitation) | within thresholds |
| `backend/internal/identity/invitation_admin.go` | backend Go | 130 | 0 | 8; 45 (InvitationService.CreateInvitation); 10 (InvitationService.ListInvitations); 1 (InvitationService.CreateInvitation) | within thresholds |
| `backend/internal/identity/invitations.go` | backend Go | 117 | 0 | 7; 13 (NewInvitationService); 6 (ValidCreationKey); 2 (ValidCreationKey) | within thresholds |
| `backend/internal/identity/oidc.go` | backend Go | 179 | 0 | 8; 33 (OIDCService.Callback); 9 (NewOIDCService); 1 (NewOIDCService) | within thresholds |
| `backend/internal/identity/oidc_stages.go` | backend Go | 209 | 0 | 12; 37 (OIDCService.prepareStartFlow); 11 (OIDCService.checkCallbackSession); 2 (OIDCService.checkCallbackSession) | within thresholds |
| `backend/internal/identity/oidcprovider/provider.go` | backend Go | 266 | 0 | 16; 39 (Provider.discovery); 14 (Provider.validateClaims); 3 (Provider.discovery) | within thresholds |
| `backend/internal/identity/postgres/invitations.go` | backend Go | 256 | 0 | 21; 32 (invitationTx.CreateInvitation); 7 (invitationTx.RevokeInvitation); 2 (invitationTx.ListInvitations) | within thresholds |
| `backend/internal/identity/postgres/oidc.go` | backend Go | 187 | 7 | 11; 55 (Repository.LinkAndCreateSession); 15 (Repository.LinkAndCreateSession); 3 (Repository.LinkAndCreateSession) | baseline exception long_lines measured=7 allowed=7 (backend-identity) |
| `backend/internal/identity/postgres/queries/invitations.sql` | SQL | 27 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/queries/oidc.sql` | SQL | 16 | 1 | — | baseline exception long_lines measured=1 allowed=1 (backend-identity) |
| `backend/internal/identity/postgres/queries/session.sql` | SQL | 15 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/queries/setup.sql` | SQL | 10 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/session.go` | backend Go | 70 | 3 | 7; 19 (Repository.LoginsByUserIDs); 5 (Repository.LoginsByUserIDs); 2 (Repository.LoginsByUserIDs) | baseline exception long_lines measured=3 allowed=3 (backend-identity) |
| `backend/internal/identity/postgres/setup.go` | backend Go | 75 | 0 | 9; 23 (Repository.WithSetupTransaction); 5 (Repository.WithSetupTransaction); 1 (Repository.WithSetupTransaction) | within thresholds |
| `backend/internal/identity/session.go` | backend Go | 204 | 0 | 12; 36 (SessionService.Login); 13 (SessionService.Login); 2 (SessionService.Login) | within thresholds |
| `backend/internal/identity/setup.go` | backend Go | 114 | 0 | 7; 57 (SetupService.CreateOwner); 8 (SetupService.CreateOwner); 2 (SetupService.CreateOwner) | within thresholds |
| `backend/internal/item/completion.go` | backend Go | 206 | 1 | 11; 40 (Service.UndoCompletion); 9 (Service.UndoCompletion); 2 (ValidateIdempotencyKey) | baseline exception long_lines measured=1 allowed=1 (backend-item) |
| `backend/internal/item/completion_list.go` | backend Go | 33 | 0 | 1; 26 (parseCompletionListQuery); 10 (parseCompletionListQuery); 2 (parseCompletionListQuery) | within thresholds |
| `backend/internal/item/completion_plan.go` | backend Go | 85 | 0 | 5; 38 (completionDecider); 9 (completionDecider.func1); 2 (completionDecider.func1) | within thresholds |
| `backend/internal/item/create.go` | backend Go | 214 | 0 | 10; 42 (creationFingerprint); 9 (Service.validateDraft); 2 (Service.validateDraft) | within thresholds |
| `backend/internal/item/httpapi/completion.go` | backend Go | 156 | 0 | 6; 33 (Handler.createCompletion); 10 (writeCompletionError); 2 (Handler.listCompletions) | within thresholds |
| `backend/internal/item/httpapi/item.go` | backend Go | 194 | 0 | 8; 39 (Handler.list); 6 (Handler.create); 1 (Handler.create) | within thresholds |
| `backend/internal/item/httpapi/item_body.go` | backend Go | 133 | 0 | 5; 32 (patchFromBody); 8 (patchFromBody); 2 (newItemFromBody) | within thresholds |
| `backend/internal/item/httpapi/item_query.go` | backend Go | 76 | 0 | 6; 14 (queryReader.scalar); 5 (queryReader.scalar); 2 (queryReader.forbid) | within thresholds |
| `backend/internal/item/httpapi/item_response.go` | backend Go | 89 | 0 | 5; 29 (toJSON); 7 (writeServiceError); 1 (listJSON) | within thresholds |
| `backend/internal/item/item.go` | backend Go | 223 | 0 | 8; 18 (Service.check); 5 (Service.check); 1 (ParseWorkflowState) | within thresholds |
| `backend/internal/item/list.go` | backend Go | 299 | 0 | 12; 40 (prepareListQuery); 15 (decodeBoundCursor); 2 (buildListPage) | within thresholds |
| `backend/internal/item/postgres/completion.go` | backend Go | 258 | 0 | 15; 38 (Repository.Complete); 10 (Repository.Complete); 2 (Repository.Complete) | within thresholds |
| `backend/internal/item/postgres/completion_rows.go` | backend Go | 93 | 0 | 7; 19 (completionFromRowValues); 1 (completionFromExisting); 0 (completionFromExisting) | within thresholds |
| `backend/internal/item/postgres/completion_undo.go` | backend Go | 72 | 0 | 2; 32 (loadCompletionUndo); 9 (loadCompletionUndo); 1 (loadCompletionUndo) | within thresholds |
| `backend/internal/item/postgres/completion_write.go` | backend Go | 73 | 0 | 3; 25 (insertCompletionAndUpdateItem); 7 (loadCompletionForWrite); 1 (completionInsertParams) | within thresholds |
| `backend/internal/item/postgres/date.go` | backend Go | 9 | 0 | 1; 3 (timeOf); 1 (timeOf); 0 (timeOf) | within thresholds |
| `backend/internal/item/postgres/item.go` | backend Go | 210 | 0 | 14; 38 (Repository.createInitialized); 9 (Repository.createInitialized); 2 (Repository.CreateIdempotent) | within thresholds |
| `backend/internal/item/postgres/item_list.go` | backend Go | 117 | 0 | 6; 43 (listParams); 11 (listParams); 2 (Repository.executeList) | within thresholds |
| `backend/internal/item/postgres/item_rows.go` | backend Go | 112 | 0 | 7; 26 (recurrenceFromRow); 8 (recurrenceFromRow); 2 (recurrenceFromRow) | within thresholds |
| `backend/internal/item/postgres/item_update.go` | backend Go | 98 | 0 | 6; 24 (mapUpdateFields); 7 (mapUpdateFields); 2 (mapUpdateFields) | within thresholds |
| `backend/internal/item/postgres/queries/completion.sql` | SQL | 24 | 9 | — | baseline exception long_lines measured=9 allowed=9 (backend-item) |
| `backend/internal/item/postgres/queries/item.sql` | SQL | 104 | 13 | — | baseline exception long_lines measured=13 allowed=13 (backend-item) |
| `backend/internal/item/postgres/summary.go` | backend Go | 50 | 2 | 1; 43 (Repository.Summary); 11 (Repository.Summary); 2 (Repository.Summary) | baseline exception long_lines measured=2 allowed=2 (backend-item) |
| `backend/internal/item/summary.go` | backend Go | 72 | 0 | 2; 36 (validateCollectionFilters); 10 (validateCollectionFilters); 2 (Service.Summary) | within thresholds |
| `backend/internal/item/update.go` | backend Go | 131 | 0 | 5; 47 (Service.buildScalarChanges); 13 (Service.buildScalarChanges); 3 (Service.buildScalarChanges) | within thresholds |
| `backend/internal/item/validate.go` | backend Go | 178 | 0 | 13; 27 (ParseDateForField); 9 (ValidateNotes); 2 (ParseDateForField.func1) | within thresholds |
| `backend/internal/platform/config/config.go` | backend Go | 209 | 0 | 14; 25 (loadTestClock); 15 (loadTestClock); 2 (LoadFrom) | within thresholds |
| `backend/internal/platform/config/oidc.go` | backend Go | 95 | 0 | 6; 25 (loadOIDC); 11 (loadOIDC); 2 (clientSecret) | within thresholds |
| `backend/internal/platform/database/migrate.go` | backend Go | 162 | 0 | 11; 37 (Migrate); 8 (Migrate); 2 (applyMigrations) | within thresholds |
| `backend/internal/platform/database/search_validation.go` | backend Go | 78 | 0 | 1; 74 (validateItemSearch); 3 (validateItemSearch); 1 (validateItemSearch) | within thresholds |
| `backend/internal/platform/httporigin/origin.go` | backend Go | 161 | 0 | 15; 21 (ParseAuthority); 11 (invalidParsedOrigin); 2 (ParseAuthority) | within thresholds |
| `backend/internal/platform/httpx/jsonapi.go` | backend Go | 326 | 0 | 22; 34 (decodeObject); 11 (decodeObject); 2 (checkPresence) | within thresholds |
| `backend/internal/platform/httpx/origin.go` | backend Go | 342 | 0 | 29; 29 (trustedForwardedMetadata); 14 (validateXForwarded); 3 (trustedForwardedMetadata) | within thresholds |
| `backend/internal/platform/httpx/server.go` | backend Go | 185 | 5 | 25; 44 (newRouterWithLogger); 5 (Health.ready); 2 (requestMiddleware.func1) | baseline exception long_lines measured=5 allowed=5 (backend-platform) |
| `backend/internal/platform/ids/uuid.go` | backend Go | 23 | 0 | 1; 19 (IsCanonicalUUID); 11 (IsCanonicalUUID); 3 (IsCanonicalUUID) | within thresholds |
| `backend/internal/platform/security/password.go` | backend Go | 105 | 0 | 7; 22 (parsePasswordParameters); 8 (parsePasswordParameters); 2 (parsePasswordParameters) | within thresholds |
| `backend/internal/platform/security/password_gate.go` | backend Go | 39 | 0 | 7; 9 (PasswordGate.Acquire); 2 (NewPasswordGate); 1 (NewPasswordGate) | within thresholds |
| `backend/internal/platform/webui/webui.go` | backend Go | 101 | 1 | 6; 35 (handler.ServeHTTP); 14 (handler.ServeHTTP); 2 (handler.ServeHTTP) | baseline exception long_lines measured=1 allowed=1 (backend-platform) |
| `backend/internal/schedule/schedule.go` | backend Go | 226 | 0 | 15; 40 (Date.AddInterval); 12 (Date.AddInterval); 3 (Date.AddInterval) | within thresholds |
| `backend/internal/subject/create.go` | backend Go | 93 | 0 | 4; 43 (Service.Create); 11 (Service.Create); 2 (Service.validateLinkedUser) | within thresholds |
| `backend/internal/subject/httpapi/subject.go` | backend Go | 181 | 0 | 10; 30 (Handler.create); 7 (Handler.create); 2 (validCreationKey) | within thresholds |
| `backend/internal/subject/httpapi/subject_mapping.go` | backend Go | 54 | 0 | 3; 23 (patchRequest); 6 (patchRequest); 2 (patchRequest) | within thresholds |
| `backend/internal/subject/list.go` | backend Go | 131 | 0 | 8; 53 (Service.List); 14 (Service.List); 2 (CanonicalIDs) | within thresholds |
| `backend/internal/subject/postgres/queries/subject.sql` | SQL | 35 | 1 | — | baseline exception long_lines measured=1 allowed=1 (backend-subject) |
| `backend/internal/subject/postgres/subject.go` | backend Go | 205 | 0 | 12; 33 (Repository.CreateIdempotent); 7 (Repository.CreateIdempotent); 2 (Repository.ListByIDs) | within thresholds |
| `backend/internal/subject/postgres/subject_update.go` | backend Go | 59 | 0 | 2; 27 (Repository.Update); 9 (Repository.Update); 2 (updateParams) | within thresholds |
| `backend/internal/subject/subject.go` | backend Go | 151 | 0 | 6; 17 (Service.Get); 4 (Service.Get); 1 (Service.Get) | within thresholds |
| `backend/internal/subject/update.go` | backend Go | 118 | 0 | 7; 39 (Service.Update); 14 (Service.Update); 2 (Service.Update) | within thresholds |
| `backend/internal/subject/validate.go` | backend Go | 32 | 0 | 2; 17 (ValidateName); 9 (ValidateName); 2 (ValidateName) | within thresholds |
| `backend/tools/funcmetrics/main.go` | backend Go | 321 | 0 | 27; 36 (collect); 11 (collect.func1); 3 (valueLiterals) | within thresholds |
| `frontend/eslint.config.js` | script/tooling | 28 | 0 | — | within thresholds |
| `frontend/playwright.config.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet-core.mjs` | script/tooling | 172 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet-identity.mjs` | script/tooling | 160 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet.mjs` | script/tooling | 76 | 0 | — | within thresholds |
| `frontend/src/app/AccountActions.tsx` | frontend TS | 38 | 0 | — | within thresholds |
| `frontend/src/app/App.tsx` | frontend TS | 68 | 0 | — | within thresholds |
| `frontend/src/app/Heading.tsx` | frontend TS | 27 | 0 | — | within thresholds |
| `frontend/src/app/HouseholdNavigation.tsx` | frontend TS | 31 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-app) |
| `frontend/src/app/Layout.tsx` | frontend TS | 52 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-app) |
| `frontend/src/app/NavLink.tsx` | frontend TS | 26 | 0 | — | within thresholds |
| `frontend/src/app/SignOutContext.ts` | frontend TS | 2 | 0 | — | within thresholds |
| `frontend/src/app/SignedInApp.tsx` | frontend TS | 125 | 0 | — | within thresholds |
| `frontend/src/app/SignedOutApp.tsx` | frontend TS | 33 | 0 | — | within thresholds |
| `frontend/src/app/StatusScreens.tsx` | frontend TS | 23 | 0 | — | within thresholds |
| `frontend/src/app/routes.ts` | frontend TS | 36 | 0 | — | within thresholds |
| `frontend/src/app/useAppNavigation.ts` | frontend TS | 91 | 0 | — | within thresholds |
| `frontend/src/app/useAppRoute.ts` | frontend TS | 8 | 0 | — | within thresholds |
| `frontend/src/app/useAppSession.ts` | frontend TS | 129 | 0 | — | within thresholds |
| `frontend/src/app/useInviteVisit.ts` | frontend TS | 60 | 0 | — | within thresholds |
| `frontend/src/app/useOidcOutcome.ts` | frontend TS | 18 | 0 | — | within thresholds |
| `frontend/src/components/ui/button.tsx` | frontend TS | 27 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-design-system) |
| `frontend/src/components/ui/input.tsx` | frontend TS | 14 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-design-system) |
| `frontend/src/components/ui/label.tsx` | frontend TS | 6 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountOidcLinkForm.tsx` | frontend TS | 52 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountOidcSectionView.tsx` | frontend TS | 42 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountPreferences.tsx` | frontend TS | 10 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountScreen.tsx` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/features/auth/LoginProviderOptions.tsx` | frontend TS | 67 | 0 | — | within thresholds |
| `frontend/src/features/auth/LoginScreen.tsx` | frontend TS | 100 | 0 | — | within thresholds |
| `frontend/src/features/auth/OnboardingScreen.tsx` | frontend TS | 189 | 0 | — | within thresholds |
| `frontend/src/features/auth/oidcApi.ts` | frontend TS | 82 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-auth) |
| `frontend/src/features/auth/onboardingModel.ts` | frontend TS | 63 | 0 | — | within thresholds |
| `frontend/src/features/auth/sessionApi.ts` | frontend TS | 105 | 0 | — | within thresholds |
| `frontend/src/features/auth/useAccountOidcSection.ts` | frontend TS | 62 | 0 | — | within thresholds |
| `frontend/src/features/auth/useLoginActions.ts` | frontend TS | 67 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOidcLinkStart.ts` | frontend TS | 40 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOidcProviderStatus.ts` | frontend TS | 41 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOnboardingForm.ts` | frontend TS | 87 | 0 | — | within thresholds |
| `frontend/src/features/home/HomeScreen.tsx` | frontend TS | 84 | 1 | — | baseline exception long_lines measured=1 allowed=1 (frontend-home) |
| `frontend/src/features/items/AddItemPanel.tsx` | frontend TS | 84 | 0 | — | within thresholds |
| `frontend/src/features/items/AllItemsControls.tsx` | frontend TS | 75 | 0 | — | within thresholds |
| `frontend/src/features/items/AllItemsResults.tsx` | frontend TS | 104 | 0 | — | within thresholds |
| `frontend/src/features/items/AllItemsScreen.tsx` | frontend TS | 38 | 0 | — | within thresholds |
| `frontend/src/features/items/HomeGroupList.tsx` | frontend TS | 57 | 0 | — | within thresholds |
| `frontend/src/features/items/HomeGroupSection.tsx` | frontend TS | 122 | 0 | — | within thresholds |
| `frontend/src/features/items/HomeOverview.tsx` | frontend TS | 122 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailActions.tsx` | frontend TS | 44 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailHistory.tsx` | frontend TS | 50 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailLoadStates.tsx` | frontend TS | 22 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailNotice.tsx` | frontend TS | 13 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailReadView.tsx` | frontend TS | 25 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemDetailScreen.tsx` | frontend TS | 79 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemEditConflictSummary.tsx` | frontend TS | 48 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemEditSection.tsx` | frontend TS | 25 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemForm.tsx` | frontend TS | 61 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemFormFields.tsx` | frontend TS | 159 | 0 | — | within thresholds |
| `frontend/src/features/items/ItemsScreen.tsx` | frontend TS | 155 | 0 | — | within thresholds |
| `frontend/src/features/items/allItemsLoad.ts` | frontend TS | 25 | 0 | — | within thresholds |
| `frontend/src/features/items/allItemsPageState.ts` | frontend TS | 51 | 0 | — | within thresholds |
| `frontend/src/features/items/allItemsUrl.ts` | frontend TS | 79 | 0 | — | within thresholds |
| `frontend/src/features/items/archivedPaging.ts` | frontend TS | 41 | 0 | — | within thresholds |
| `frontend/src/features/items/collectMembers.ts` | frontend TS | 21 | 0 | — | within thresholds |
| `frontend/src/features/items/completionOutcomes.ts` | frontend TS | 135 | 0 | — | within thresholds |
| `frontend/src/features/items/formatDate.ts` | frontend TS | 7 | 0 | — | within thresholds |
| `frontend/src/features/items/homeGroups.ts` | frontend TS | 12 | 0 | — | within thresholds |
| `frontend/src/features/items/homeItemsHelpers.ts` | frontend TS | 33 | 0 | — | within thresholds |
| `frontend/src/features/items/itemEditModel.ts` | frontend TS | 61 | 0 | — | within thresholds |
| `frontend/src/features/items/itemEditSave.ts` | frontend TS | 56 | 0 | — | within thresholds |
| `frontend/src/features/items/itemFormModel.ts` | frontend TS | 110 | 0 | — | within thresholds |
| `frontend/src/features/items/itemsApi.ts` | frontend TS | 326 | 0 | — | within thresholds |
| `frontend/src/features/items/itemsScreenHelpers.ts` | frontend TS | 20 | 0 | — | within thresholds |
| `frontend/src/features/items/loadEditChoices.ts` | frontend TS | 40 | 0 | — | within thresholds |
| `frontend/src/features/items/pendingCompletions.ts` | frontend TS | 60 | 0 | — | within thresholds |
| `frontend/src/features/items/pendingItemCreate.ts` | frontend TS | 96 | 0 | — | within thresholds |
| `frontend/src/features/items/repeatSummary.ts` | frontend TS | 13 | 0 | — | within thresholds |
| `frontend/src/features/items/useAddItem.ts` | frontend TS | 179 | 0 | — | within thresholds |
| `frontend/src/features/items/useAddSubjectCatalogue.ts` | frontend TS | 158 | 0 | — | within thresholds |
| `frontend/src/features/items/useAllItems.ts` | frontend TS | 135 | 0 | — | within thresholds |
| `frontend/src/features/items/useAllItemsFacets.ts` | frontend TS | 57 | 0 | — | within thresholds |
| `frontend/src/features/items/useCompletionActions.ts` | frontend TS | 65 | 0 | — | within thresholds |
| `frontend/src/features/items/useCompletionFocus.ts` | frontend TS | 52 | 0 | — | within thresholds |
| `frontend/src/features/items/useHomeItems.ts` | frontend TS | 86 | 0 | — | within thresholds |
| `frontend/src/features/items/useHomeMembers.ts` | frontend TS | 53 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemDetail.ts` | frontend TS | 72 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemDetailActions.ts` | frontend TS | 48 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemDetailBusy.ts` | frontend TS | 26 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemDetailEdit.ts` | frontend TS | 69 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemDetailHistory.ts` | frontend TS | 65 | 0 | — | within thresholds |
| `frontend/src/features/items/useItemFormState.ts` | frontend TS | 90 | 0 | — | within thresholds |
| `frontend/src/features/items/useLabelRetryFocus.ts` | frontend TS | 33 | 0 | — | within thresholds |
| `frontend/src/features/items/useLoadGeneration.ts` | frontend TS | 17 | 0 | — | within thresholds |
| `frontend/src/features/items/useMountedRef.ts` | frontend TS | 9 | 0 | — | within thresholds |
| `frontend/src/features/items/useMutationLock.ts` | frontend TS | 16 | 0 | — | within thresholds |
| `frontend/src/features/items/usePendingCreate.ts` | frontend TS | 49 | 0 | — | within thresholds |
| `frontend/src/features/items/useShowMoreFocus.ts` | frontend TS | 21 | 0 | — | within thresholds |
| `frontend/src/features/items/useSubjectLabels.ts` | frontend TS | 101 | 0 | — | within thresholds |
| `frontend/src/features/members/InvitationRows.tsx` | frontend TS | 40 | 0 | — | within thresholds |
| `frontend/src/features/members/InvitePanel.tsx` | frontend TS | 62 | 0 | — | within thresholds |
| `frontend/src/features/members/InviteScreen.tsx` | frontend TS | 125 | 0 | — | within thresholds |
| `frontend/src/features/members/MemberListSection.tsx` | frontend TS | 27 | 0 | — | within thresholds |
| `frontend/src/features/members/MembersPager.tsx` | frontend TS | 24 | 0 | — | within thresholds |
| `frontend/src/features/members/MembersScreen.tsx` | frontend TS | 52 | 0 | — | within thresholds |
| `frontend/src/features/members/inviteAcceptModel.ts` | frontend TS | 60 | 0 | — | within thresholds |
| `frontend/src/features/members/membersApi.ts` | frontend TS | 86 | 7 | — | baseline exception long_lines measured=7 allowed=7 (frontend-members) |
| `frontend/src/features/members/membersModel.ts` | frontend TS | 14 | 0 | — | within thresholds |
| `frontend/src/features/members/useInvitationList.ts` | frontend TS | 88 | 0 | — | within thresholds |
| `frontend/src/features/members/useInviteAccept.ts` | frontend TS | 95 | 0 | — | within thresholds |
| `frontend/src/features/members/useInviteAcceptState.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/features/members/useInviteActions.ts` | frontend TS | 85 | 0 | — | within thresholds |
| `frontend/src/features/members/useInviteLink.ts` | frontend TS | 22 | 0 | — | within thresholds |
| `frontend/src/features/members/useMemberList.ts` | frontend TS | 46 | 0 | — | within thresholds |
| `frontend/src/features/members/useMembersCore.ts` | frontend TS | 33 | 0 | — | within thresholds |
| `frontend/src/features/members/useMembersDiscovery.ts` | frontend TS | 77 | 0 | — | within thresholds |
| `frontend/src/features/members/useMembersFocus.ts` | frontend TS | 56 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectForm.tsx` | frontend TS | 68 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectFormFields.tsx` | frontend TS | 86 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectList.tsx` | frontend TS | 45 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectNotice.tsx` | frontend TS | 25 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectRow.tsx` | frontend TS | 45 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectsEmpty.tsx` | frontend TS | 15 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectsScreen.tsx` | frontend TS | 106 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectsScreenContent.tsx` | frontend TS | 49 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectsStatus.tsx` | frontend TS | 17 | 0 | — | within thresholds |
| `frontend/src/features/subjects/SubjectsViewToggle.tsx` | frontend TS | 11 | 0 | — | within thresholds |
| `frontend/src/features/subjects/pendingSubjectCreate.ts` | frontend TS | 73 | 0 | — | within thresholds |
| `frontend/src/features/subjects/refreshAfterCreate.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/features/subjects/settleTerminalCreate.ts` | frontend TS | 27 | 0 | — | within thresholds |
| `frontend/src/features/subjects/subjectFormModel.ts` | frontend TS | 40 | 0 | — | within thresholds |
| `frontend/src/features/subjects/subjectsApi.ts` | frontend TS | 127 | 0 | — | within thresholds |
| `frontend/src/features/subjects/subjectsModel.ts` | frontend TS | 65 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useAddSubjectForm.ts` | frontend TS | 34 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useGuardedMutation.ts` | frontend TS | 32 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectActions.ts` | frontend TS | 23 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectArchive.ts` | frontend TS | 56 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectCreate.ts` | frontend TS | 70 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectEditing.ts` | frontend TS | 43 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectFormState.ts` | frontend TS | 83 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectList.ts` | frontend TS | 74 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectMembers.ts` | frontend TS | 37 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectPaging.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectSave.ts` | frontend TS | 66 | 0 | — | within thresholds |
| `frontend/src/features/subjects/useSubjectsFocus.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/i18n/cs.ts` | frontend TS | 418 | 1 | — | baseline exception file_lines measured=418 allowed=418 (frontend-localization); baseline exception long_lines measured=1 allowed=1 (frontend-localization) |
| `frontend/src/i18n/en.ts` | frontend TS | 417 | 0 | — | baseline exception file_lines measured=417 allowed=417 (frontend-localization) |
| `frontend/src/i18n/index.tsx` | frontend TS | 53 | 0 | — | within thresholds |
| `frontend/src/lib/api.ts` | frontend TS | 8 | 0 | — | within thresholds |
| `frontend/src/lib/cn.ts` | frontend TS | 5 | 0 | — | within thresholds |
| `frontend/src/lib/idempotencyKey.ts` | frontend TS | 5 | 0 | — | within thresholds |
| `frontend/src/lib/textLength.ts` | frontend TS | 8 | 0 | — | within thresholds |
| `frontend/src/main.tsx` | frontend TS | 12 | 0 | — | within thresholds |
| `frontend/vite.config.ts` | frontend TS | 48 | 0 | — | within thresholds |
| `scripts/autonomous-loop.sh` | script/tooling | 88 | 1 | — | baseline exception long_lines measured=1 allowed=1 (tooling-scripts) |
| `scripts/backup-restore-smoke.sh` | script/tooling | 136 | 8 | — | baseline exception long_lines measured=8 allowed=8 (tooling-scripts) |
| `scripts/check-api-generation.sh` | script/tooling | 31 | 0 | — | within thresholds |
| `scripts/check-sqlc.sh` | script/tooling | 39 | 1 | — | baseline exception long_lines measured=1 allowed=1 (tooling-scripts) |
| `scripts/dev-exec.sh` | script/tooling | 7 | 0 | — | within thresholds |
| `scripts/dev_toolchain.py` | script/tooling | 503 | 0 | — | baseline exception file_lines measured=503 allowed=503 (tooling-scripts) |
| `scripts/e2e-native-runner.py` | script/tooling | 497 | 3 | — | baseline exception long_lines measured=3 allowed=3 (tooling-scripts) |
| `scripts/e2e-smoke.sh` | script/tooling | 101 | 1 | — | baseline exception long_lines measured=1 allowed=1 (tooling-scripts) |
| `scripts/e2e_db_fixtures.py` | script/tooling | 326 | 0 | — | within thresholds |
| `scripts/e2e_native_diagnostics.py` | script/tooling | 371 | 0 | — | within thresholds |
| `scripts/e2e_orca_session.py` | script/tooling | 51 | 0 | — | within thresholds |
| `scripts/household-backup-smoke.sh` | script/tooling | 302 | 14 | — | baseline exception long_lines measured=14 allowed=14 (tooling-scripts) |
| `scripts/maintainability_check.py` | script/tooling | 378 | 0 | — | within thresholds |
| `scripts/oidc-smoke.sh` | script/tooling | 187 | 9 | — | baseline exception long_lines measured=9 allowed=9 (tooling-scripts) |
| `scripts/postgres-test-service.py` | script/tooling | 309 | 7 | — | baseline exception long_lines measured=7 allowed=7 (tooling-scripts) |
| `scripts/production_inventory.py` | script/tooling | 180 | 0 | — | within thresholds |
| `scripts/proxy-smoke.sh` | script/tooling | 451 | 26 | — | baseline exception long_lines measured=26 allowed=26 (tooling-scripts) |
| `scripts/proxy_smoke_oidc.py` | script/tooling | 130 | 0 | — | within thresholds |
| `scripts/quickstart-smoke.sh` | script/tooling | 209 | 8 | — | baseline exception long_lines measured=8 allowed=8 (tooling-scripts) |
| `scripts/quickstart_docs.py` | script/tooling | 141 | 0 | — | within thresholds |
| `scripts/release-assets.sh` | script/tooling | 9 | 0 | — | within thresholds |
| `scripts/runtime-smoke.sh` | script/tooling | 142 | 1 | — | baseline exception long_lines measured=1 allowed=1 (tooling-scripts) |
| `scripts/setup-integration.sh` | script/tooling | 5 | 1 | — | baseline exception long_lines measured=1 allowed=1 (tooling-scripts) |
| `scripts/setup-smoke.sh` | script/tooling | 627 | 142 | — | baseline exception file_lines measured=627 allowed=627 (tooling-scripts); baseline exception long_lines measured=142 allowed=142 (tooling-scripts) |
| `scripts/setup_smoke_api_capture.py` | script/tooling | 156 | 0 | — | within thresholds |
| `scripts/sr-orca-session.sh` | script/tooling | 89 | 4 | — | baseline exception long_lines measured=4 allowed=4 (tooling-scripts) |
| `scripts/upgrade-smoke.sh` | script/tooling | 241 | 20 | — | baseline exception long_lines measured=20 allowed=20 (tooling-scripts) |
| `scripts/verify-local.sh` | script/tooling | 63 | 4 | — | baseline exception long_lines measured=4 allowed=4 (tooling-scripts) |

## Authored files in unmeasured formats

Configuration, deployment, markup, style and data files have no calibrated metric; each was reviewed for scope.

| Path | Non-blank lines | Test-only | Disposition |
|---|---:|---|---|
| `.dockerignore` | 13 | no | reviewed; unmeasured format |
| `.env.example` | 19 | no | reviewed; unmeasured format |
| `.github/workflows/release-qualification.yml` | 167 | no | reviewed; unmeasured format |
| `.github/workflows/release.yml` | 120 | no | reviewed; unmeasured format |
| `.github/workflows/required.yml` | 368 | no | reviewed; unmeasured format |
| `.gitignore` | 16 | no | reviewed; unmeasured format |
| `Dockerfile` | 26 | no | reviewed; unmeasured format |
| `api/Makefile` | 19 | no | reviewed; unmeasured format |
| `api/oapi-codegen.yaml` | 8 | no | reviewed; unmeasured format |
| `api/openapi.yaml` | 1876 | no | reviewed; unmeasured format |
| `api/package.json` | 20 | no | reviewed; unmeasured format |
| `backend/go.mod` | 18 | no | reviewed; unmeasured format |
| `backend/sqlc.yaml` | 42 | no | reviewed; unmeasured format |
| `backend/test/oidc-provider/Dockerfile` | 9 | yes | reviewed; unmeasured format |
| `ci/maintainability-baseline.json` | 423 | no | reviewed; unmeasured format |
| `compose.yaml` | 77 | no | reviewed; unmeasured format |
| `deploy/compose.build.yaml` | 11 | no | reviewed; unmeasured format |
| `deploy/compose.e2e.yaml` | 6 | yes | reviewed; unmeasured format |
| `deploy/compose.oidc-test.yaml` | 30 | yes | reviewed; unmeasured format |
| `deploy/reverse-proxy/Caddyfile` | 17 | no | reviewed; unmeasured format |
| `deploy/reverse-proxy/nginx.conf` | 32 | no | reviewed; unmeasured format |
| `deploy/reverse-proxy/traefik-dynamic.yml` | 27 | no | reviewed; unmeasured format |
| `deploy/reverse-proxy/traefik.yml` | 7 | no | reviewed; unmeasured format |
| `frontend/.gitignore` | 1 | no | reviewed; unmeasured format |
| `frontend/index.html` | 13 | no | reviewed; unmeasured format |
| `frontend/maintainability-baseline.json` | 1 | no | reviewed; unmeasured format |
| `frontend/package.json` | 51 | no | reviewed; unmeasured format |
| `frontend/src/index.css` | 51 | no | reviewed; unmeasured format |
| `frontend/tsconfig.json` | 22 | no | reviewed; unmeasured format |
| `renovate.json` | 265 | no | reviewed; unmeasured format |
| `toolchain.json` | 36 | no | reviewed; unmeasured format |

Not production source (counts):

- agent context: 21
- binary asset or placeholder: 3
- documentation: 39
- lockfile: 3

## Measured-format paths excluded from the ratchet

### *.test.*: Test suites are excluded; policy does not require splitting them.

39 file(s).

### *_test.go: Go tests are excluded; policy does not require splitting a test suite.

93 file(s).

### api/generated/api.d.ts: openapi-typescript API declaration; generation tests detect drift.

1 file(s).

- `api/generated/api.d.ts`

### api/generated/health.d.ts: openapi-typescript health declaration; generation tests detect drift.

1 file(s).

- `api/generated/health.d.ts`

### backend/internal/household/httpapi/household.gen.go: oapi-codegen household types; API generation checks detect drift.

1 file(s).

- `backend/internal/household/httpapi/household.gen.go`

### backend/internal/household/postgres/dbgen/: sqlc generated household code; scripts/check-sqlc.sh checks drift.

6 file(s).

- `backend/internal/household/postgres/dbgen/db.go`
- `backend/internal/household/postgres/dbgen/household.sql.go`
- `backend/internal/household/postgres/dbgen/membership.sql.go`
- `backend/internal/household/postgres/dbgen/models.go`
- `backend/internal/household/postgres/dbgen/querier.go`
- `backend/internal/household/postgres/dbgen/setup.sql.go`

### backend/internal/identity/httpapi/setup.gen.go: oapi-codegen setup types; API generation checks detect drift.

1 file(s).

- `backend/internal/identity/httpapi/setup.gen.go`

### backend/internal/identity/postgres/dbgen/: sqlc generated identity code; scripts/check-sqlc.sh checks drift.

7 file(s).

- `backend/internal/identity/postgres/dbgen/db.go`
- `backend/internal/identity/postgres/dbgen/invitations.sql.go`
- `backend/internal/identity/postgres/dbgen/models.go`
- `backend/internal/identity/postgres/dbgen/oidc.sql.go`
- `backend/internal/identity/postgres/dbgen/querier.go`
- `backend/internal/identity/postgres/dbgen/session.sql.go`
- `backend/internal/identity/postgres/dbgen/setup.sql.go`

### backend/internal/item/httpapi/item.gen.go: oapi-codegen item types; API generation checks detect drift.

1 file(s).

- `backend/internal/item/httpapi/item.gen.go`

### backend/internal/item/postgres/dbgen/: sqlc generated item code; scripts/check-sqlc.sh checks drift.

5 file(s).

- `backend/internal/item/postgres/dbgen/completion.sql.go`
- `backend/internal/item/postgres/dbgen/db.go`
- `backend/internal/item/postgres/dbgen/item.sql.go`
- `backend/internal/item/postgres/dbgen/models.go`
- `backend/internal/item/postgres/dbgen/querier.go`

### backend/internal/platform/database/NNN_*.sql: Embedded numbered migration history is immutable.

16 file(s).

- `backend/internal/platform/database/001_setup.sql`
- `backend/internal/platform/database/002_sessions.sql`
- `backend/internal/platform/database/003_household_version.sql`
- `backend/internal/platform/database/004_subjects.sql`
- `backend/internal/platform/database/005_items.sql`
- `backend/internal/platform/database/006_item_recurrence.sql`
- `backend/internal/platform/database/007_item_completions.sql`
- `backend/internal/platform/database/008_completion_undo.sql`
- `backend/internal/platform/database/009_invitations.sql`
- `backend/internal/platform/database/010_item_responsible_member.sql`
- `backend/internal/platform/database/011_oidc.sql`
- `backend/internal/platform/database/012_subject_create_idempotency.sql`
- `backend/internal/platform/database/013_item_create_idempotency.sql`
- `backend/internal/platform/database/014_subject_linked_user.sql`
- `backend/internal/platform/database/015_item_attention_sort.sql`
- `backend/internal/platform/database/016_item_text_search.sql`

### backend/internal/platform/httpx/health.gen.go: oapi-codegen health types; API generation checks detect drift.

1 file(s).

- `backend/internal/platform/httpx/health.gen.go`

### backend/internal/subject/httpapi/subject.gen.go: oapi-codegen subject types; API generation checks detect drift.

1 file(s).

- `backend/internal/subject/httpapi/subject.gen.go`

### backend/internal/subject/postgres/dbgen/: sqlc generated subject code; scripts/check-sqlc.sh checks drift.

4 file(s).

- `backend/internal/subject/postgres/dbgen/db.go`
- `backend/internal/subject/postgres/dbgen/models.go`
- `backend/internal/subject/postgres/dbgen/querier.go`
- `backend/internal/subject/postgres/dbgen/subject.sql.go`

### frontend/e2e/*.spec.ts: Browser E2E specs are test suites.

5 file(s).

### scripts/test_*.py: Python harness suites are test code.

11 file(s).
