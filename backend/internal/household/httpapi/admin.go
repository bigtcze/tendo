package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

var createHouseholdFields = map[string]httpx.Field{
	"name":        {Kind: httpx.KindString, Required: true},
	"timezone":    {Kind: httpx.KindString, Required: true},
	"adminUserId": {Kind: httpx.KindString, Required: true},
}

type instanceAdminService interface {
	RequireInstanceAdmin(ctx context.Context, actorID string) error
	ListHouseholds(ctx context.Context, actorID, cursor string, limit int) ([]household.AdminHousehold, string, error)
	CreateHousehold(ctx context.Context, actorID, key, name, timezone, adminUserID string) (household.AdminHousehold, int64, error)
	GetHousehold(ctx context.Context, actorID, householdID string) (household.AdminHousehold, int64, error)
	ListDirectory(ctx context.Context, actorID, search, cursor string, limit int) ([]household.DirectoryAccount, string, error)
	ListAccounts(ctx context.Context, actorID, householdID, cursor string, limit int) ([]household.AdminAccount, string, error)
	GetAccount(ctx context.Context, actorID, householdID, targetUserID string) (household.AdminAccount, error)
	AssignAccount(ctx context.Context, actorID, householdID, targetUserID, role string) (household.AdminAccount, bool, error)
	ChangeRole(ctx context.Context, actorID, householdID, targetUserID, role string, expectedVersion int64) (household.AdminAccount, error)
	RemoveAccount(ctx context.Context, actorID, householdID, targetUserID string, expectedVersion int64) error
}

// AdminHandler serves the instance-administrator household account center
// (ADR 0020). Every route requires a session, then the instance-admin grant,
// before any parameter, body or lookup is evaluated, so a caller without the
// grant learns nothing about households or accounts. Responses never carry
// household content.
type AdminHandler struct {
	service instanceAdminService
	auth    func(http.Handler) http.Handler
	userID  func(context.Context) (string, bool)
}

func NewAdmin(s instanceAdminService, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *AdminHandler {
	return &AdminHandler{s, auth, userID}
}

func (h *AdminHandler) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(h.auth, noStore, h.requireInstanceAdmin)
		r.Get("/api/v1/admin/households", h.listHouseholds)
		r.Post("/api/v1/admin/households", h.createHousehold)
		r.Get("/api/v1/admin/households/{householdId}", h.getHousehold)
		r.Get("/api/v1/admin/accounts", h.listDirectory)
		r.Get("/api/v1/admin/households/{householdId}/accounts", h.listAccounts)
		r.Get("/api/v1/admin/households/{householdId}/accounts/{userId}", h.getAccount)
		r.Put("/api/v1/admin/households/{householdId}/accounts/{userId}", h.assignAccount)
		r.Patch("/api/v1/admin/households/{householdId}/accounts/{userId}", h.changeRole)
		r.Delete("/api/v1/admin/households/{householdId}/accounts/{userId}", h.removeAccount)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requireInstanceAdmin is the transport pre-check. Each service operation
// re-checks the grant in its own transaction; this only orders the 403 before
// validation and lookups.
func (h *AdminHandler) requireInstanceAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.userID(r.Context())
		if !ok {
			httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if err := h.service.RequireInstanceAdmin(r.Context(), actor); err != nil {
			writeAdminError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *AdminHandler) actor(r *http.Request) string {
	actor, _ := h.userID(r.Context())
	return actor
}

func listLimit(p httpx.ListParams) int {
	if p.Limit == 0 {
		return 50
	}
	return p.Limit
}

func (h *AdminHandler) listHouseholds(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.ParseListParamsFor(w, r, 100, httpx.ListFilter(0))
	if !ok {
		return
	}
	households, next, err := h.service.ListHouseholds(r.Context(), h.actor(r), p.Cursor, listLimit(p))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	out := apimodel.AdminHouseholdList{Items: make([]apimodel.AdminHousehold, 0, len(households))}
	for _, house := range households {
		out.Items = append(out.Items, adminHouseholdDTO(house))
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) createHousehold(w http.ResponseWriter, r *http.Request) {
	key, ok := httpx.IdempotencyKey(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, createHouseholdFields, accountBodyLimit)
	if !ok {
		return
	}
	house, version, err := h.service.CreateHousehold(r.Context(), h.actor(r), key,
		values["name"].(string), values["timezone"].(string), values["adminUserId"].(string))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/admin/households/"+strings.ToLower(house.ID))
	w.Header().Set("ETag", httpx.ETag(version))
	httpx.WriteJSON(w, http.StatusCreated, adminHouseholdDTO(house))
}

func (h *AdminHandler) getHousehold(w http.ResponseWriter, r *http.Request) {
	house, version, err := h.service.GetHousehold(r.Context(), h.actor(r), chi.URLParam(r, "householdId"))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(version))
	httpx.WriteJSON(w, http.StatusOK, adminHouseholdDTO(house))
}

func adminHouseholdDTO(house household.AdminHousehold) apimodel.AdminHousehold {
	return apimodel.AdminHousehold{Id: house.ID, Name: house.Name, Timezone: house.Timezone, CreatedAt: house.CreatedAt.UTC()}
}

func (h *AdminHandler) listDirectory(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.ParseListParamsFor(w, r, 100, httpx.ListFilter(0))
	if !ok {
		return
	}
	search := ""
	if values, present := r.URL.Query()["q"]; present {
		if len(values) != 1 {
			httpx.QueryProblemResponse(w, "q")
			return
		}
		search = values[0]
	}
	accounts, next, err := h.service.ListDirectory(r.Context(), h.actor(r), search, p.Cursor, listLimit(p))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	out := apimodel.DirectoryAccountList{Items: make([]apimodel.DirectoryAccount, 0, len(accounts))}
	for _, account := range accounts {
		out.Items = append(out.Items, apimodel.DirectoryAccount{UserId: account.UserID, Login: account.Login})
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.ParseListParamsFor(w, r, 100, httpx.ListFilter(0))
	if !ok {
		return
	}
	accounts, next, err := h.service.ListAccounts(r.Context(), h.actor(r), chi.URLParam(r, "householdId"), p.Cursor, listLimit(p))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	out := apimodel.AdminAccountList{Items: make([]apimodel.AdminAccount, 0, len(accounts))}
	for _, account := range accounts {
		out.Items = append(out.Items, adminAccountDTO(account))
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) getAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.service.GetAccount(r.Context(), h.actor(r), chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminAccount(w, http.StatusOK, account)
}

// assignAccount puts an existing account into the household. 201 means a new
// membership; 200 is an idempotent replay of the same role.
func (h *AdminHandler) assignAccount(w http.ResponseWriter, r *http.Request) {
	values, ok := httpx.ReadObject(w, r, accountRoleFields, accountBodyLimit)
	if !ok {
		return
	}
	account, created, err := h.service.AssignAccount(r.Context(), h.actor(r), chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"), values["role"].(string))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeAdminAccount(w, status, account)
}

func (h *AdminHandler) changeRole(w http.ResponseWriter, r *http.Request) {
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, accountRoleFields, accountBodyLimit)
	if !ok {
		return
	}
	account, err := h.service.ChangeRole(r.Context(), h.actor(r), chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"), values["role"].(string), expected)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminAccount(w, http.StatusOK, account)
}

func (h *AdminHandler) removeAccount(w http.ResponseWriter, r *http.Request) {
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	if err := h.service.RemoveAccount(r.Context(), h.actor(r), chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"), expected); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeAdminAccount(w http.ResponseWriter, status int, account household.AdminAccount) {
	w.Header().Set("ETag", httpx.ETag(account.Version))
	httpx.WriteJSON(w, status, adminAccountDTO(account))
}

func adminAccountDTO(account household.AdminAccount) apimodel.AdminAccount {
	return apimodel.AdminAccount{
		UserId: account.UserID, Login: account.Login, Role: apimodel.MemberRole(account.Role),
		SignInMethods: apimodel.AccountSignInMethods{Password: account.SignInMethods.Password, Oidc: account.SignInMethods.OIDC},
	}
}

func writeAdminError(w http.ResponseWriter, err error) {
	var validation *household.ValidationError
	switch {
	case errors.Is(err, household.ErrInstanceAdminRequired):
		httpx.ProblemResponse(w, http.StatusForbidden, "instance_admin_required")
	case errors.Is(err, household.ErrIdempotencyKeyReused):
		httpx.ProblemResponse(w, http.StatusUnprocessableEntity, "idempotency_key_reused")
	case errors.Is(err, household.ErrNotFound):
		httpx.ProblemResponse(w, http.StatusNotFound, "not_found")
	case errors.Is(err, household.ErrPreconditionFailed):
		httpx.ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.Is(err, household.ErrLastAdmin):
		httpx.ProblemResponse(w, http.StatusConflict, "last_admin")
	case errors.Is(err, household.ErrAlreadyMember):
		httpx.ProblemResponse(w, http.StatusConflict, "already_member")
	case errors.Is(err, household.ErrHouseholdRequiresAdmin):
		httpx.ProblemResponse(w, http.StatusConflict, "household_requires_admin")
	case errors.As(err, &validation):
		if validation.Field == "cursor" || validation.Field == "q" {
			httpx.QueryProblemResponse(w, validation.Field)
			return
		}
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	default:
		httpx.ProblemResponse(w, http.StatusServiceUnavailable, "unavailable")
	}
}
