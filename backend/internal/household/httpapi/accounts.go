package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

const accountBodyLimit = 4096

var accountRoleFields = map[string]httpx.Field{
	"role": {Kind: httpx.KindString, Required: true},
}

type accountService interface {
	ListAccounts(context.Context, string, string, string, int) ([]household.Account, string, error)
	GetAccount(context.Context, string, string, string) (household.Account, error)
	ChangeRole(context.Context, string, string, string, string, int64) (household.Account, error)
}

// AccountsHandler serves admin-only household member account resources.
type AccountsHandler struct {
	service accountService
	auth    func(http.Handler) http.Handler
	userID  func(context.Context) (string, bool)
}

func NewAccounts(s accountService, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *AccountsHandler {
	return &AccountsHandler{s, auth, userID}
}

func (h *AccountsHandler) Register(r chi.Router) {
	r.With(h.auth).Get("/api/v1/households/{householdId}/accounts", h.list)
	r.With(h.auth).Get("/api/v1/households/{householdId}/accounts/{userId}", h.get)
	r.With(h.auth).Patch("/api/v1/households/{householdId}/accounts/{userId}", h.update)
}

func (h *AccountsHandler) list(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	p, ok := httpx.ParseListParamsFor(w, r, 100, httpx.ListFilter(0))
	if !ok {
		return
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	accounts, next, err := h.service.ListAccounts(r.Context(), actor, chi.URLParam(r, "householdId"), p.Cursor, limit)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	out := apimodel.AccountList{Items: make([]apimodel.Account, 0, len(accounts)), NextCursor: nil}
	for _, account := range accounts {
		out.Items = append(out.Items, accountDTO(account))
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *AccountsHandler) get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	account, err := h.service.GetAccount(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(account.Version))
	httpx.WriteJSON(w, http.StatusOK, accountDTO(account))
}

func (h *AccountsHandler) update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, accountRoleFields, accountBodyLimit)
	if !ok {
		return
	}
	account, err := h.service.ChangeRole(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"), values["role"].(string), expected)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(account.Version))
	httpx.WriteJSON(w, http.StatusOK, accountDTO(account))
}

func accountDTO(account household.Account) apimodel.Account {
	dto := apimodel.Account{
		UserId: account.UserID, Login: account.Login, Role: apimodel.AccountRole(account.Role),
		SignInMethods: apimodel.AccountSignInMethods{Password: account.SignInMethods.Password, Oidc: account.SignInMethods.OIDC},
	}
	if account.PasswordReset != nil {
		dto.PasswordReset = &apimodel.AccountPasswordReset{
			CreatedAt: account.PasswordReset.CreatedAt.UTC(),
			ExpiresAt: account.PasswordReset.ExpiresAt.UTC(),
		}
	}
	return dto
}

func writeAccountError(w http.ResponseWriter, err error) {
	var validation *household.ValidationError
	switch {
	case errors.Is(err, household.ErrAdminRequired):
		httpx.ProblemResponse(w, http.StatusForbidden, "admin_required")
	case errors.Is(err, household.ErrNotFound):
		httpx.ProblemResponse(w, http.StatusNotFound, "not_found")
	case errors.Is(err, household.ErrPreconditionFailed):
		httpx.ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.Is(err, household.ErrLastAdmin):
		httpx.ProblemResponse(w, http.StatusConflict, "last_admin")
	case errors.As(err, &validation):
		if validation.Field == "cursor" {
			httpx.QueryProblemResponse(w, "cursor")
			return
		}
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	default:
		httpx.ProblemResponse(w, http.StatusServiceUnavailable, "unavailable")
	}
}
