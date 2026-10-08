package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

type memberLister interface {
	ListMembers(context.Context, string, string, string, int) ([]household.Member, string, error)
}
type MembersHandler struct {
	service memberLister
	auth    func(http.Handler) http.Handler
	userID  func(context.Context) (string, bool)
}

func NewMembers(s memberLister, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *MembersHandler {
	return &MembersHandler{s, auth, userID}
}
func (h *MembersHandler) Register(r chi.Router) {
	r.With(h.auth).Get("/api/v1/households/{householdId}/members", h.list)
}
func (h *MembersHandler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, 401, "unauthenticated")
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
	cursor := p.Cursor
	if _, ok := h.service.(*household.MembershipService); ok {
		decoded, err := household.DecodeMemberCursor(cursor)
		if err != nil {
			httpx.QueryProblemResponse(w, "cursor")
			return
		}
		if cursor != "" {
			cursor = household.EncodeMemberCursor(decoded)
		}
	}
	items, next, e := h.service.ListMembers(r.Context(), uid, chi.URLParam(r, "householdId"), cursor, limit)
	if e != nil {
		var validation *household.ValidationError
		if errors.As(e, &validation) && validation.Field == "cursor" {
			httpx.QueryProblemResponse(w, "cursor")
		} else if errors.Is(e, household.ErrNotFound) {
			httpx.ProblemResponse(w, 404, "not_found")
		} else {
			httpx.ProblemResponse(w, 503, "unavailable")
		}
		return
	}
	out := MemberList{Items: make([]Member, 0, len(items)), NextCursor: nil}
	for _, m := range items {
		if m.Role != household.RoleOwner && m.Role != household.RoleMember {
			httpx.ProblemResponse(w, 503, "unavailable")
			return
		}
		out.Items = append(out.Items, Member{UserId: m.UserID, Login: m.Login, Role: MemberRole(m.Role)})
	}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.WriteJSON(w, 200, out)
}
