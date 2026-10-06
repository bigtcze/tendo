package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/go-chi/chi/v5"
)

type service interface {
	Get(ctx context.Context, userID, householdID string) (household.Household, error)
}

// Handler serves household resources. Authentication is injected so this
// package does not depend on the identity adapter.
type Handler struct {
	service service
	auth    func(http.Handler) http.Handler
	userID  func(context.Context) (string, bool)
}

// New builds the handler. auth must reject unauthenticated requests and make the
// caller identity available to userID.
func New(s service, auth func(http.Handler) http.Handler, userID func(context.Context) (string, bool)) *Handler {
	return &Handler{service: s, auth: auth, userID: userID}
}

func (h *Handler) Register(r chi.Router) {
	r.With(h.auth).Get("/api/v1/households/{householdId}", h.get)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	found, err := h.service.Get(r.Context(), userID, chi.URLParam(r, "householdId"))
	if errors.Is(err, household.ErrNotFound) {
		problem(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(found.Version, 10)+`"`)
	writeJSON(w, http.StatusOK, Household{Id: found.ID, Name: found.Name, Timezone: found.Timezone, CreatedAt: found.CreatedAt.UTC()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}
