# V2.1 Hand-Written Production Code Inventory

Regenerate with `bash scripts/dev-exec.sh -- python3 scripts/maintainability_check.py --inventory-out docs/development/v2-1-production-inventory.md`.
This is a snapshot at Git HEAD `8c3008841f4a2f5381b46727870aafd82f87869d`; rerun the command after source changes.
The maintainability ratchet checks the live tree and guarantees ongoing threshold compliance between inventory snapshots.

## Summary

| Area | Production files |
|---|---:|
| backend Go | 78 |
| frontend TS | 148 |
| SQL | 10 |
| script | 33 |
| **Total production** | **269** |

Test files: 148; test-support files: 12.
Frontend function-level rules are enforced by ESLint with `frontend/maintainability-baseline.json` (0 entries).

## Production files

| Path | Area | Non-blank lines | Long lines | Go functions; max lines (function); complexity (function); nesting (function) | Disposition |
|---|---|---:|---:|---|---|
| `api/check-generation.mjs` | script | 91 | 24 | — | baseline exception: long_lines allowed=24 (tooling-scripts) |
| `api/check-health.mjs` | script | 331 | 0 | — | within thresholds |
| `backend/cmd/tendo/items.go` | backend Go | 56 | 0 | 6; 13 (itemHouseholds); 3 (itemHouseholds.func1); 1 (itemHouseholds.func1) | within thresholds |
| `backend/cmd/tendo/main.go` | backend Go | 237 | 6 | 17; 51 (registerAPI); 9 (serve); 3 (serve) | baseline exception: long_lines allowed=6 (backend-platform) |
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
| `backend/internal/identity/httpapi/oidc.go` | backend Go | 178 | 2 | 13; 35 (OIDCHandler.start); 8 (OIDCHandler.start); 2 (OIDCHandler.callback) | baseline exception: long_lines allowed=2 (backend-identity) |
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
| `backend/internal/identity/postgres/oidc.go` | backend Go | 187 | 7 | 11; 55 (Repository.LinkAndCreateSession); 15 (Repository.LinkAndCreateSession); 3 (Repository.LinkAndCreateSession) | baseline exception: long_lines allowed=7 (backend-identity) |
| `backend/internal/identity/postgres/queries/invitations.sql` | SQL | 27 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/queries/oidc.sql` | SQL | 16 | 1 | — | baseline exception: long_lines allowed=1 (backend-identity) |
| `backend/internal/identity/postgres/queries/session.sql` | SQL | 15 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/queries/setup.sql` | SQL | 10 | 0 | — | within thresholds |
| `backend/internal/identity/postgres/session.go` | backend Go | 70 | 3 | 7; 19 (Repository.LoginsByUserIDs); 5 (Repository.LoginsByUserIDs); 2 (Repository.LoginsByUserIDs) | baseline exception: long_lines allowed=3 (backend-identity) |
| `backend/internal/identity/postgres/setup.go` | backend Go | 75 | 0 | 9; 23 (Repository.WithSetupTransaction); 5 (Repository.WithSetupTransaction); 1 (Repository.WithSetupTransaction) | within thresholds |
| `backend/internal/identity/session.go` | backend Go | 204 | 0 | 12; 36 (SessionService.Login); 13 (SessionService.Login); 2 (SessionService.Login) | within thresholds |
| `backend/internal/identity/setup.go` | backend Go | 114 | 0 | 7; 57 (SetupService.CreateOwner); 8 (SetupService.CreateOwner); 2 (SetupService.CreateOwner) | within thresholds |
| `backend/internal/item/completion.go` | backend Go | 206 | 1 | 11; 40 (Service.UndoCompletion); 9 (Service.UndoCompletion); 2 (ValidateIdempotencyKey) | baseline exception: long_lines allowed=1 (backend-item) |
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
| `backend/internal/item/postgres/queries/completion.sql` | SQL | 24 | 9 | — | baseline exception: long_lines allowed=9 (backend-item) |
| `backend/internal/item/postgres/queries/item.sql` | SQL | 104 | 13 | — | baseline exception: long_lines allowed=13 (backend-item) |
| `backend/internal/item/postgres/summary.go` | backend Go | 50 | 2 | 1; 43 (Repository.Summary); 11 (Repository.Summary); 2 (Repository.Summary) | baseline exception: long_lines allowed=2 (backend-item) |
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
| `backend/internal/platform/httpx/server.go` | backend Go | 185 | 5 | 25; 44 (newRouterWithLogger); 5 (Health.ready); 2 (requestMiddleware.func1) | baseline exception: long_lines allowed=5 (backend-platform) |
| `backend/internal/platform/ids/uuid.go` | backend Go | 23 | 0 | 1; 19 (IsCanonicalUUID); 11 (IsCanonicalUUID); 3 (IsCanonicalUUID) | within thresholds |
| `backend/internal/platform/security/password.go` | backend Go | 105 | 0 | 7; 22 (parsePasswordParameters); 8 (parsePasswordParameters); 2 (parsePasswordParameters) | within thresholds |
| `backend/internal/platform/security/password_gate.go` | backend Go | 39 | 0 | 7; 9 (PasswordGate.Acquire); 2 (NewPasswordGate); 1 (NewPasswordGate) | within thresholds |
| `backend/internal/platform/webui/webui.go` | backend Go | 101 | 1 | 6; 35 (handler.ServeHTTP); 14 (handler.ServeHTTP); 2 (handler.ServeHTTP) | baseline exception: long_lines allowed=1 (backend-platform) |
| `backend/internal/schedule/schedule.go` | backend Go | 226 | 0 | 15; 40 (Date.AddInterval); 12 (Date.AddInterval); 3 (Date.AddInterval) | within thresholds |
| `backend/internal/subject/create.go` | backend Go | 93 | 0 | 4; 43 (Service.Create); 11 (Service.Create); 2 (Service.validateLinkedUser) | within thresholds |
| `backend/internal/subject/httpapi/subject.go` | backend Go | 181 | 0 | 10; 30 (Handler.create); 7 (Handler.create); 2 (validCreationKey) | within thresholds |
| `backend/internal/subject/httpapi/subject_mapping.go` | backend Go | 54 | 0 | 3; 23 (patchRequest); 6 (patchRequest); 2 (patchRequest) | within thresholds |
| `backend/internal/subject/list.go` | backend Go | 131 | 0 | 8; 53 (Service.List); 14 (Service.List); 2 (CanonicalIDs) | within thresholds |
| `backend/internal/subject/postgres/queries/subject.sql` | SQL | 35 | 1 | — | baseline exception: long_lines allowed=1 (backend-subject) |
| `backend/internal/subject/postgres/subject.go` | backend Go | 205 | 0 | 12; 33 (Repository.CreateIdempotent); 7 (Repository.CreateIdempotent); 2 (Repository.ListByIDs) | within thresholds |
| `backend/internal/subject/postgres/subject_update.go` | backend Go | 59 | 0 | 2; 27 (Repository.Update); 9 (Repository.Update); 2 (updateParams) | within thresholds |
| `backend/internal/subject/subject.go` | backend Go | 151 | 0 | 6; 17 (Service.Get); 4 (Service.Get); 1 (Service.Get) | within thresholds |
| `backend/internal/subject/update.go` | backend Go | 118 | 0 | 7; 39 (Service.Update); 14 (Service.Update); 2 (Service.Update) | within thresholds |
| `backend/internal/subject/validate.go` | backend Go | 32 | 0 | 2; 17 (ValidateName); 9 (ValidateName); 2 (ValidateName) | within thresholds |
| `backend/tools/funcmetrics/main.go` | backend Go | 321 | 0 | 27; 36 (collect); 11 (collect.func1); 3 (valueLiterals) | within thresholds |
| `frontend/eslint.config.js` | script | 28 | 0 | — | within thresholds |
| `frontend/playwright.config.ts` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet-core.mjs` | script | 172 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet-identity.mjs` | script | 160 | 0 | — | within thresholds |
| `frontend/scripts/lint-ratchet.mjs` | script | 76 | 0 | — | within thresholds |
| `frontend/src/app/AccountActions.tsx` | frontend TS | 38 | 0 | — | within thresholds |
| `frontend/src/app/App.tsx` | frontend TS | 68 | 0 | — | within thresholds |
| `frontend/src/app/Heading.tsx` | frontend TS | 27 | 0 | — | within thresholds |
| `frontend/src/app/HouseholdNavigation.tsx` | frontend TS | 31 | 1 | — | baseline exception: long_lines allowed=1 (frontend-app) |
| `frontend/src/app/Layout.tsx` | frontend TS | 52 | 1 | — | baseline exception: long_lines allowed=1 (frontend-app) |
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
| `frontend/src/components/ui/button.tsx` | frontend TS | 27 | 1 | — | baseline exception: long_lines allowed=1 (frontend-design-system) |
| `frontend/src/components/ui/input.tsx` | frontend TS | 14 | 1 | — | baseline exception: long_lines allowed=1 (frontend-design-system) |
| `frontend/src/components/ui/label.tsx` | frontend TS | 6 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountOidcLinkForm.tsx` | frontend TS | 52 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountOidcSectionView.tsx` | frontend TS | 42 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountPreferences.tsx` | frontend TS | 10 | 0 | — | within thresholds |
| `frontend/src/features/auth/AccountScreen.tsx` | frontend TS | 39 | 0 | — | within thresholds |
| `frontend/src/features/auth/LoginProviderOptions.tsx` | frontend TS | 67 | 0 | — | within thresholds |
| `frontend/src/features/auth/LoginScreen.tsx` | frontend TS | 100 | 0 | — | within thresholds |
| `frontend/src/features/auth/OnboardingScreen.tsx` | frontend TS | 189 | 0 | — | within thresholds |
| `frontend/src/features/auth/oidcApi.ts` | frontend TS | 82 | 1 | — | baseline exception: long_lines allowed=1 (frontend-auth) |
| `frontend/src/features/auth/onboardingModel.ts` | frontend TS | 63 | 0 | — | within thresholds |
| `frontend/src/features/auth/sessionApi.ts` | frontend TS | 105 | 0 | — | within thresholds |
| `frontend/src/features/auth/useAccountOidcSection.ts` | frontend TS | 62 | 0 | — | within thresholds |
| `frontend/src/features/auth/useLoginActions.ts` | frontend TS | 67 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOidcLinkStart.ts` | frontend TS | 40 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOidcProviderStatus.ts` | frontend TS | 41 | 0 | — | within thresholds |
| `frontend/src/features/auth/useOnboardingForm.ts` | frontend TS | 87 | 0 | — | within thresholds |
| `frontend/src/features/home/HomeScreen.tsx` | frontend TS | 84 | 1 | — | baseline exception: long_lines allowed=1 (frontend-home) |
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
| `frontend/src/features/members/membersApi.ts` | frontend TS | 86 | 7 | — | baseline exception: long_lines allowed=7 (frontend-members) |
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
| `frontend/src/features/subjects/pendingSubjectCreate.ts` | frontend TS | 59 | 0 | — | within thresholds |
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
| `frontend/src/i18n/cs.ts` | frontend TS | 418 | 1 | — | baseline exception: file_lines allowed=418 (frontend-localization); long_lines allowed=1 (frontend-localization) |
| `frontend/src/i18n/en.ts` | frontend TS | 417 | 0 | — | baseline exception: file_lines allowed=417 (frontend-localization) |
| `frontend/src/i18n/index.tsx` | frontend TS | 53 | 0 | — | within thresholds |
| `frontend/src/lib/api.ts` | frontend TS | 8 | 0 | — | within thresholds |
| `frontend/src/lib/cn.ts` | frontend TS | 5 | 0 | — | within thresholds |
| `frontend/src/lib/idempotencyKey.ts` | frontend TS | 5 | 0 | — | within thresholds |
| `frontend/src/lib/textLength.ts` | frontend TS | 8 | 0 | — | within thresholds |
| `frontend/src/main.tsx` | frontend TS | 12 | 0 | — | within thresholds |
| `frontend/vite.config.ts` | frontend TS | 48 | 0 | — | within thresholds |
| `scripts/autonomous-loop.sh` | script | 88 | 1 | — | baseline exception: long_lines allowed=1 (tooling-scripts) |
| `scripts/backup-restore-smoke.sh` | script | 136 | 8 | — | baseline exception: long_lines allowed=8 (tooling-scripts) |
| `scripts/check-api-generation.sh` | script | 31 | 0 | — | within thresholds |
| `scripts/check-sqlc.sh` | script | 39 | 1 | — | baseline exception: long_lines allowed=1 (tooling-scripts) |
| `scripts/dev-exec.sh` | script | 7 | 0 | — | within thresholds |
| `scripts/dev_toolchain.py` | script | 503 | 0 | — | baseline exception: file_lines allowed=503 (tooling-scripts) |
| `scripts/e2e-native-runner.py` | script | 497 | 3 | — | baseline exception: long_lines allowed=3 (tooling-scripts) |
| `scripts/e2e-smoke.sh` | script | 101 | 1 | — | baseline exception: long_lines allowed=1 (tooling-scripts) |
| `scripts/e2e_db_fixtures.py` | script | 326 | 0 | — | within thresholds |
| `scripts/e2e_native_diagnostics.py` | script | 371 | 0 | — | within thresholds |
| `scripts/e2e_orca_session.py` | script | 51 | 0 | — | within thresholds |
| `scripts/household-backup-smoke.sh` | script | 302 | 14 | — | baseline exception: long_lines allowed=14 (tooling-scripts) |
| `scripts/maintainability_check.py` | script | 495 | 0 | — | within thresholds |
| `scripts/oidc-smoke.sh` | script | 187 | 9 | — | baseline exception: long_lines allowed=9 (tooling-scripts) |
| `scripts/postgres-test-service.py` | script | 309 | 7 | — | baseline exception: long_lines allowed=7 (tooling-scripts) |
| `scripts/proxy-smoke.sh` | script | 451 | 26 | — | baseline exception: long_lines allowed=26 (tooling-scripts) |
| `scripts/proxy_smoke_oidc.py` | script | 130 | 0 | — | within thresholds |
| `scripts/quickstart-smoke.sh` | script | 209 | 8 | — | baseline exception: long_lines allowed=8 (tooling-scripts) |
| `scripts/quickstart_docs.py` | script | 141 | 0 | — | within thresholds |
| `scripts/release-assets.sh` | script | 9 | 0 | — | within thresholds |
| `scripts/runtime-smoke.sh` | script | 142 | 1 | — | baseline exception: long_lines allowed=1 (tooling-scripts) |
| `scripts/setup-integration.sh` | script | 5 | 1 | — | baseline exception: long_lines allowed=1 (tooling-scripts) |
| `scripts/setup-smoke.sh` | script | 627 | 142 | — | baseline exception: file_lines allowed=627 (tooling-scripts); long_lines allowed=142 (tooling-scripts) |
| `scripts/setup_smoke_api_capture.py` | script | 156 | 0 | — | within thresholds |
| `scripts/sr-orca-session.sh` | script | 89 | 4 | — | baseline exception: long_lines allowed=4 (tooling-scripts) |
| `scripts/upgrade-smoke.sh` | script | 241 | 20 | — | baseline exception: long_lines allowed=20 (tooling-scripts) |
| `scripts/verify-local.sh` | script | 63 | 4 | — | baseline exception: long_lines allowed=4 (tooling-scripts) |

## Excluded source paths

### *.test.*: Test suites are excluded; policy does not require splitting them.

- `api/check-contract.test.mjs`
- `api/check-health.test.mjs`
- `api/dependency-overrides.test.mjs`
- `api/oidc-middleware.test.mjs`
- `frontend/scripts/lint-ratchet-core.test.mjs`
- `frontend/src/app/App.routing.test.tsx`
- `frontend/src/app/App.test.tsx`
- `frontend/src/app/Heading.test.tsx`
- `frontend/src/app/routes.test.ts`
- `frontend/src/app/useAppSession.test.ts`
- `frontend/src/contrast.test.ts`
- `frontend/src/features/auth/Oidc.test.tsx`
- `frontend/src/features/auth/OnboardingScreen.test.tsx`
- `frontend/src/features/auth/onboardingModel.test.ts`
- `frontend/src/features/auth/useOnboardingForm.test.ts`
- `frontend/src/features/items/AllItemsScreen.test.tsx`
- `frontend/src/features/items/ItemDetailScreen.test.tsx`
- `frontend/src/features/items/ItemForm.test.tsx`
- `frontend/src/features/items/ItemsScreen.test.tsx`
- `frontend/src/features/items/itemsApi.test.ts`
- `frontend/src/features/items/pendingCompletions.test.ts`
- `frontend/src/features/items/pendingItemCreate.test.ts`
- `frontend/src/features/items/useAllItems.test.ts`
- `frontend/src/features/items/useAllItemsFacets.test.ts`
- `frontend/src/features/items/usePendingCreate.test.ts`
- `frontend/src/features/items/useSubjectLabels.test.tsx`
- `frontend/src/features/members/InviteScreen.test.tsx`
- `frontend/src/features/members/MembersScreen.focus.test.tsx`
- `frontend/src/features/members/MembersScreen.test.tsx`
- `frontend/src/features/members/inviteAcceptModel.test.ts`
- `frontend/src/features/subjects/SubjectForm.test.tsx`
- `frontend/src/features/subjects/SubjectsScreen.test.tsx`
- `frontend/src/features/subjects/pendingSubjectCreate.test.ts`
- `frontend/src/features/subjects/refreshAfterCreate.test.ts`
- `frontend/src/features/subjects/subjectCreateSettlement.test.ts`
- `frontend/src/features/subjects/subjectFormModel.test.ts`
- `frontend/src/i18n/i18n.test.ts`
- `frontend/src/lib/textLength.test.ts`
- `frontend/src/motion.test.ts`

### *_test.go: Go tests are excluded; policy does not require splitting a test suite.

- `backend/cmd/tendo/items_test.go`
- `backend/cmd/tendo/runtime_test.go`
- `backend/cmd/tendo/security_contract_test.go`
- `backend/cmd/tendo/subjects_test.go`
- `backend/internal/household/household_test.go`
- `backend/internal/household/httpapi/household_test.go`
- `backend/internal/household/httpapi/members_test.go`
- `backend/internal/household/membership_test.go`
- `backend/internal/household/postgres/household_test.go`
- `backend/internal/household/postgres/keyset_property_test.go`
- `backend/internal/household/postgres/membership_test.go`
- `backend/internal/identity/httpapi/invitation_service_test.go`
- `backend/internal/identity/httpapi/invitations_test.go`
- `backend/internal/identity/httpapi/oidc_start_characterization_test.go`
- `backend/internal/identity/httpapi/oidc_test.go`
- `backend/internal/identity/httpapi/password_gate_test.go`
- `backend/internal/identity/httpapi/session_test.go`
- `backend/internal/identity/httpapi/setup_strict_body_test.go`
- `backend/internal/identity/httpapi/setup_test.go`
- `backend/internal/identity/invitations_order_test.go`
- `backend/internal/identity/invitations_test.go`
- `backend/internal/identity/oidc_characterization_test.go`
- `backend/internal/identity/oidc_error_mapping_characterization_test.go`
- `backend/internal/identity/oidc_start_characterization_test.go`
- `backend/internal/identity/oidc_test.go`
- `backend/internal/identity/oidcprovider/exchange_order_test.go`
- `backend/internal/identity/oidcprovider/provider_test.go`
- `backend/internal/identity/postgres/invitation_keyset_property_test.go`
- `backend/internal/identity/postgres/invitations_concurrency_test.go`
- `backend/internal/identity/postgres/invitations_expiry_test.go`
- `backend/internal/identity/postgres/invitations_round3_test.go`
- `backend/internal/identity/postgres/invitations_test.go`
- `backend/internal/identity/postgres/oidc_admission_test.go`
- `backend/internal/identity/postgres/oidc_test.go`
- `backend/internal/identity/postgres/session_test.go`
- `backend/internal/identity/postgres/setup_test.go`
- `backend/internal/identity/session_test.go`
- `backend/internal/identity/setup_test.go`
- `backend/internal/item/completion_order_test.go`
- `backend/internal/item/completion_test.go`
- `backend/internal/item/export_test.go`
- `backend/internal/item/helpers_test.go`
- `backend/internal/item/httpapi/completion_test.go`
- `backend/internal/item/httpapi/generated_contract_test.go`
- `backend/internal/item/httpapi/item_test.go`
- `backend/internal/item/httpapi/request_mapping_test.go`
- `backend/internal/item/httpapi/responsible_test.go`
- `backend/internal/item/item_test.go`
- `backend/internal/item/order_test.go`
- `backend/internal/item/postgres/attention_sort_test.go`
- `backend/internal/item/postgres/completion_characterization_test.go`
- `backend/internal/item/postgres/completion_decode_test.go`
- `backend/internal/item/postgres/completion_history_property_test.go`
- `backend/internal/item/postgres/completion_order_test.go`
- `backend/internal/item/postgres/concurrency_test.go`
- `backend/internal/item/postgres/item_mapping_test.go`
- `backend/internal/item/postgres/item_repository_characterization_test.go`
- `backend/internal/item/postgres/item_test.go`
- `backend/internal/item/postgres/keyset_property_test.go`
- `backend/internal/item/postgres/search_explain_test.go`
- `backend/internal/item/postgres/search_test.go`
- `backend/internal/item/postgres/search_validation_test.go`
- `backend/internal/item/postgres/summary_test.go`
- `backend/internal/item/responsible_test.go`
- `backend/internal/item/summary_test.go`
- `backend/internal/platform/config/config_test.go`
- `backend/internal/platform/config/oidc_test.go`
- `backend/internal/platform/config/order_test.go`
- `backend/internal/platform/database/migrate_discovery_test.go`
- `backend/internal/platform/httporigin/origin_test.go`
- `backend/internal/platform/httpx/decode_characterization_test.go`
- `backend/internal/platform/httpx/jsonapi_test.go`
- `backend/internal/platform/httpx/origin_characterization_test.go`
- `backend/internal/platform/httpx/origin_contract_test.go`
- `backend/internal/platform/httpx/origin_test.go`
- `backend/internal/platform/httpx/server_test.go`
- `backend/internal/platform/ids/uuid_test.go`
- `backend/internal/platform/security/password_test.go`
- `backend/internal/platform/webui/webui_test.go`
- `backend/internal/schedule/schedule_property_test.go`
- `backend/internal/schedule/schedule_test.go`
- `backend/internal/subject/helpers_test.go`
- `backend/internal/subject/httpapi/generated_contract_test.go`
- `backend/internal/subject/httpapi/linked_user_test.go`
- `backend/internal/subject/httpapi/request_mapping_test.go`
- `backend/internal/subject/httpapi/subject_test.go`
- `backend/internal/subject/postgres/keyset_property_test.go`
- `backend/internal/subject/postgres/subject_repository_characterization_test.go`
- `backend/internal/subject/postgres/subject_test.go`
- `backend/internal/subject/subject_test.go`
- `backend/internal/testoidc/provider_test.go`
- `backend/internal/testpostgres/fixture_test.go`
- `backend/tools/funcmetrics/main_test.go`

### api/generated/api.d.ts: openapi-typescript API declaration; generation tests detect drift.

- `api/generated/api.d.ts`

### api/generated/health.d.ts: openapi-typescript health declaration; generation tests detect drift.

- `api/generated/health.d.ts`

### backend/internal/household/httpapi/household.gen.go: oapi-codegen household types; API generation checks detect drift.

- `backend/internal/household/httpapi/household.gen.go`

### backend/internal/household/postgres/dbgen/: sqlc generated household code; scripts/check-sqlc.sh checks drift.

- `backend/internal/household/postgres/dbgen/db.go`
- `backend/internal/household/postgres/dbgen/household.sql.go`
- `backend/internal/household/postgres/dbgen/membership.sql.go`
- `backend/internal/household/postgres/dbgen/models.go`
- `backend/internal/household/postgres/dbgen/querier.go`
- `backend/internal/household/postgres/dbgen/setup.sql.go`

### backend/internal/identity/httpapi/setup.gen.go: oapi-codegen setup types; API generation checks detect drift.

- `backend/internal/identity/httpapi/setup.gen.go`

### backend/internal/identity/postgres/dbgen/: sqlc generated identity code; scripts/check-sqlc.sh checks drift.

- `backend/internal/identity/postgres/dbgen/db.go`
- `backend/internal/identity/postgres/dbgen/invitations.sql.go`
- `backend/internal/identity/postgres/dbgen/models.go`
- `backend/internal/identity/postgres/dbgen/oidc.sql.go`
- `backend/internal/identity/postgres/dbgen/querier.go`
- `backend/internal/identity/postgres/dbgen/session.sql.go`
- `backend/internal/identity/postgres/dbgen/setup.sql.go`

### backend/internal/item/httpapi/item.gen.go: oapi-codegen item types; API generation checks detect drift.

- `backend/internal/item/httpapi/item.gen.go`

### backend/internal/item/postgres/dbgen/: sqlc generated item code; scripts/check-sqlc.sh checks drift.

- `backend/internal/item/postgres/dbgen/completion.sql.go`
- `backend/internal/item/postgres/dbgen/db.go`
- `backend/internal/item/postgres/dbgen/item.sql.go`
- `backend/internal/item/postgres/dbgen/models.go`
- `backend/internal/item/postgres/dbgen/querier.go`

### backend/internal/platform/database/NNN_*.sql: Embedded numbered migration history is immutable.

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

- `backend/internal/platform/httpx/health.gen.go`

### backend/internal/subject/httpapi/subject.gen.go: oapi-codegen subject types; API generation checks detect drift.

- `backend/internal/subject/httpapi/subject.gen.go`

### backend/internal/subject/postgres/dbgen/: sqlc generated subject code; scripts/check-sqlc.sh checks drift.

- `backend/internal/subject/postgres/dbgen/db.go`
- `backend/internal/subject/postgres/dbgen/models.go`
- `backend/internal/subject/postgres/dbgen/querier.go`
- `backend/internal/subject/postgres/dbgen/subject.sql.go`

### frontend/e2e/*.spec.ts: Browser E2E specs are test suites.

- `frontend/e2e/attention-clock.spec.ts`
- `frontend/e2e/journey.spec.ts`
- `frontend/e2e/readme-screenshot.spec.ts`
- `frontend/e2e/screen-reader.spec.ts`
- `frontend/e2e/ux-audit.spec.ts`

### scripts/test_*.py: Python harness suites are test code.

- `scripts/test_ci_workflow.py`
- `scripts/test_dev_toolchain.py`
- `scripts/test_e2e_interrupt.py`
- `scripts/test_e2e_native_runner.py`
- `scripts/test_generation_drift.py`
- `scripts/test_maintainability_check.py`
- `scripts/test_postgres_live.py`
- `scripts/test_postgres_test_service.py`
- `scripts/test_quickstart_docs.py`
- `scripts/test_toolchain_manifest.py`
- `scripts/test_verify_local.py`
