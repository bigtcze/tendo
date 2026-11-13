package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

const passwordResetBodyLimit = 4096

// passwordResetTargetLimit caps how often one target account can have a link
// issued per hour, independent of the issuing admin or client address, so
// issuance cannot be used to flood a person or to probe accounts.
const passwordResetTargetLimit = 5
const passwordResetTargetWindow = time.Hour

var passwordResetRedeemFields = map[string]httpx.Field{
	"token":    {Kind: httpx.KindString, Required: true},
	"password": {Kind: httpx.KindString, Required: true},
}

type passwordResetService interface {
	IssuePasswordReset(context.Context, string, string, string) (identity.PasswordResetIssued, error)
	RevokePasswordReset(context.Context, string, string, string) error
	RedeemPasswordReset(context.Context, string, string) (string, error)
}

// PasswordResetHandler serves admin-issued reset links and the public redeem
// endpoint. It never echoes a password, and the issued token appears only in
// the 201 response.
type PasswordResetHandler struct {
	service     passwordResetService
	auth        func(http.Handler) http.Handler
	userID      func(context.Context) (string, bool)
	adminLimit  *limiter
	targetLimit *limiter
	redeemLimit *limiter
}

func NewPasswordReset(s passwordResetService, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *PasswordResetHandler {
	return &PasswordResetHandler{
		service:     s,
		auth:        auth,
		userID:      userID,
		adminLimit:  newLimiter(10),
		targetLimit: newWindowLimiter(passwordResetTargetLimit, passwordResetTargetWindow),
		redeemLimit: newLimiter(5),
	}
}

func (h *PasswordResetHandler) Register(r chi.Router) {
	r.With(h.auth).Post("/api/v1/households/{householdId}/accounts/{userId}/password-reset", h.issue)
	r.With(h.auth).Delete("/api/v1/households/{householdId}/accounts/{userId}/password-reset", h.revoke)
	r.Post("/api/v1/auth/password-reset", h.redeem)
}

func (h *PasswordResetHandler) issue(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if !h.adminLimit.admitAttempt(clientIP(r)) {
		rateLimited(w)
		return
	}
	if !h.targetLimit.admitAttempt(chi.URLParam(r, "userId")) {
		rateLimited(w)
		return
	}
	if _, ok = httpx.ReadObjectAllowEmpty(w, r, map[string]httpx.Field{}, passwordResetBodyLimit); !ok {
		return
	}
	issued, err := h.service.IssuePasswordReset(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"))
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, apimodel.PasswordResetCreated{
		Token: issued.Token, ExpiresAt: issued.ExpiresAt.UTC(), CreatedAt: issued.CreatedAt.UTC(),
	})
}

func (h *PasswordResetHandler) revoke(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if !h.adminLimit.admitAttempt(clientIP(r)) {
		rateLimited(w)
		return
	}
	if err := h.service.RevokePasswordReset(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "userId")); err != nil {
		writePasswordResetError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *PasswordResetHandler) redeem(w http.ResponseWriter, r *http.Request) {
	if !h.redeemLimit.admitAttempt(clientIP(r)) {
		rateLimited(w)
		return
	}
	values, ok := httpx.ReadObject(w, r, passwordResetRedeemFields, passwordResetBodyLimit)
	if !ok {
		return
	}
	login, err := h.service.RedeemPasswordReset(r.Context(), values["token"].(string), values["password"].(string))
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, apimodel.PasswordResetRedeemed{Login: login})
}

func writePasswordResetError(w http.ResponseWriter, err error) {
	var validation *identity.ValidationError
	switch {
	case errors.Is(err, household.ErrAdminRequired):
		httpx.ProblemResponse(w, http.StatusForbidden, "admin_required")
	case errors.Is(err, identity.ErrInvalidPasswordReset):
		httpx.ProblemResponse(w, http.StatusNotFound, "invalid_password_reset")
	case errors.Is(err, household.ErrNotFound), errors.Is(err, identity.ErrNotFound):
		httpx.ProblemResponse(w, http.StatusNotFound, "not_found")
	case errors.Is(err, identity.ErrCannotResetSelf):
		httpx.ProblemResponse(w, http.StatusConflict, "cannot_reset_self")
	case errors.Is(err, identity.ErrPasswordResetNotPermitted):
		httpx.ProblemResponse(w, http.StatusConflict, "password_reset_not_permitted")
	case errors.Is(err, identity.ErrNoLocalPassword):
		httpx.ProblemResponse(w, http.StatusConflict, "no_local_password")
	case errors.Is(err, security.ErrPasswordWorkLimit):
		rateLimited(w)
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	default:
		httpx.ProblemResponse(w, http.StatusServiceUnavailable, "unavailable")
	}
}
