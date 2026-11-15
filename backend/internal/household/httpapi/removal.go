package httpapi

import (
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// remove deletes one membership, which is also the "leave household" action for
// a plain member. The body is empty and success is 204 with no representation.
// If-Match is the target membership version, exactly like PATCH.
func (h *AccountsHandler) remove(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	err := h.service.RemoveMember(r.Context(), actor, chi.URLParam(r, "householdId"), chi.URLParam(r, "userId"), expected)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
