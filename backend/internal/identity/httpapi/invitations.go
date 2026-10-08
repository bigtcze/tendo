package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type invitationService interface {
	CreateInvitation(context.Context, string, string, string) (identity.InvitationResult, error)
	ListInvitations(context.Context, string, string, int, string) ([]identity.Invitation, string, error)
	RevokeInvitation(context.Context, string, string, string) error
	AcceptInvitationNewAccount(context.Context, string, string, string) (identity.InvitationUser, error)
	AcceptInvitationExistingAccount(context.Context, string, string) (identity.InvitationUser, error)
}
type InvitationHandler struct {
	service                     invitationService
	auth                        func(http.Handler) http.Handler
	userID                      func(context.Context) (string, bool)
	newLimiter, existingLimiter *limiter
}

func NewInvitations(s invitationService, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *InvitationHandler {
	return &InvitationHandler{service: s, auth: auth, userID: userID, newLimiter: newLimiter(5), existingLimiter: newLimiter(10)}
}
func (h *InvitationHandler) Register(r chi.Router) {
	r.With(h.auth).Post("/api/v1/households/{householdId}/invitations", h.create)
	r.With(h.auth).Get("/api/v1/households/{householdId}/invitations", h.list)
	r.With(h.auth).Delete("/api/v1/households/{householdId}/invitations/{invitationId}", h.revoke)
	r.Post("/api/v1/auth/invitations/accept", h.acceptNew)
	r.With(h.auth).Post("/api/v1/invitations/accept", h.acceptExisting)
}
func (h *InvitationHandler) actor(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, 401, "unauthenticated")
		return "", false
	}
	return id, true
}
func (h *InvitationHandler) create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) == 0 {
		httpx.ProblemResponse(w, 400, "idempotency_key_required")
		return
	}
	if len(keys) != 1 || !identity.ValidCreationKey(keys[0]) {
		httpx.ProblemResponse(w, 400, "invalid_idempotency_key")
		return
	}
	if _, ok = httpx.ReadObjectAllowEmpty(w, r, map[string]httpx.Field{}, 4096); !ok {
		return
	}
	result, e := h.service.CreateInvitation(r.Context(), actor, chi.URLParam(r, "householdId"), keys[0])
	if e != nil {
		writeInvitationError(w, e)
		return
	}
	w.Header().Set("Location", "/api/v1/households/"+strings.ToLower(chi.URLParam(r, "householdId"))+"/invitations/"+strings.ToLower(result.Invitation.ID))
	if result.Created {
		out := invitationDTO(result.Invitation)
		httpx.WriteJSON(w, 201, InvitationCreated{Id: out.Id, Role: InvitationCreatedRoleMember, Status: out.Status, CreatedAt: out.CreatedAt, ExpiresAt: out.ExpiresAt, AcceptedAt: out.AcceptedAt, RevokedAt: out.RevokedAt, Token: result.Token})
	} else {
		httpx.WriteJSON(w, 200, invitationDTO(result.Invitation))
	}
}
func (h *InvitationHandler) list(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
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
	items, next, e := h.service.ListInvitations(r.Context(), actor, chi.URLParam(r, "householdId"), limit, p.Cursor)
	if e != nil {
		writeInvitationError(w, e)
		return
	}
	out := InvitationList{Items: make([]Invitation, 0, len(items)), NextCursor: nil}
	for _, i := range items {
		out.Items = append(out.Items, invitationDTO(i))
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, 200, out)
}
func (h *InvitationHandler) revoke(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if e := h.service.RevokeInvitation(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "invitationId")); e != nil {
		writeInvitationError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(204)
}
func (h *InvitationHandler) acceptNew(w http.ResponseWriter, r *http.Request) {
	if !h.newLimiter.admitAttempt(clientIP(r)) {
		rateLimited(w)
		return
	}
	fields := map[string]httpx.Field{"token": {Kind: httpx.KindString, Required: true}, "login": {Kind: httpx.KindString, Required: true}, "password": {Kind: httpx.KindString, Required: true}}
	v, ok := httpx.ReadObject(w, r, fields, 4096)
	if !ok {
		return
	}
	u, e := h.service.AcceptInvitationNewAccount(r.Context(), v["token"].(string), v["login"].(string), v["password"].(string))
	if e != nil {
		writeInvitationError(w, e)
		return
	}
	w.Header().Set("Location", "/api/v1/households/"+strings.ToLower(u.HouseholdID))
	httpx.WriteJSON(w, 201, InvitedAccount{UserId: u.UserID, Login: u.Login, HouseholdId: u.HouseholdID, Role: InvitedAccountRoleMember})
}
func (h *InvitationHandler) acceptExisting(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if !h.existingLimiter.admitAttempt(clientIP(r)) {
		rateLimited(w)
		return
	}
	fields := map[string]httpx.Field{"token": {Kind: httpx.KindString, Required: true}}
	v, ok := httpx.ReadObject(w, r, fields, 4096)
	if !ok {
		return
	}
	u, e := h.service.AcceptInvitationExistingAccount(r.Context(), actor, v["token"].(string))
	if errors.Is(e, identity.ErrNotFound) {
		httpx.ProblemResponse(w, 401, "unauthenticated")
		return
	}
	if e != nil {
		writeInvitationError(w, e)
		return
	}
	w.Header().Set("Location", "/api/v1/households/"+strings.ToLower(u.HouseholdID))
	httpx.WriteJSON(w, 201, Membership{UserId: u.UserID, HouseholdId: u.HouseholdID, Role: MembershipRoleMember})
}
func invitationDTO(i identity.Invitation) Invitation {
	return Invitation{Id: i.ID, Role: InvitationRoleMember, Status: InvitationStatus(i.Status), CreatedAt: i.CreatedAt.UTC(), ExpiresAt: i.ExpiresAt.UTC(), AcceptedAt: i.AcceptedAt, RevokedAt: i.RevokedAt}
}
func rateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	httpx.ProblemResponse(w, 429, "rate_limited")
}
func writeInvitationError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, household.ErrNotFound):
		httpx.ProblemResponse(w, 404, "not_found")
	case errors.Is(e, household.ErrForbidden):
		httpx.ProblemResponse(w, 403, "owner_required")
	case errors.Is(e, identity.ErrInvalidInvitation):
		httpx.ProblemResponse(w, 404, "invalid_invitation")
	case errors.Is(e, identity.ErrLoginUnavailable):
		httpx.ProblemResponse(w, 409, "login_unavailable")
	case errors.Is(e, identity.ErrHouseholdConflict):
		httpx.ProblemResponse(w, 409, "household_conflict")
	case errors.Is(e, identity.ErrAlreadyMember):
		httpx.ProblemResponse(w, 409, "already_member")
	case errors.Is(e, identity.ErrIdempotencyKeyRequired):
		httpx.ProblemResponse(w, 400, "idempotency_key_required")
	case errors.Is(e, identity.ErrInvalidIdempotencyKey):
		httpx.ProblemResponse(w, 400, "invalid_idempotency_key")
	case errors.Is(e, security.ErrPasswordWorkLimit):
		rateLimited(w)
	default:
		var v *identity.ValidationError
		if errors.As(e, &v) {
			if v.Field == "cursor" {
				httpx.QueryProblemResponse(w, "cursor")
			} else {
				w.Header().Set("Content-Type", "application/problem+json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(422)
				_ = json.NewEncoder(w).Encode(InvitationValidationProblem{Type: "about:blank", Title: "Validation Failed", Status: 422, Field: InvitationValidationProblemField(v.Field), Code: InvitationValidationProblemCode(v.Code)})
			}
		} else {
			httpx.ProblemResponse(w, 503, "unavailable")
		}
	}
}
