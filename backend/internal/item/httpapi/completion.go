package httpapi

import (
	"errors"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
)

const completionBodyLimit = 4 << 10

var completionFields = map[string]httpx.Field{"completedOn": {Kind: httpx.KindString}}

func (h *Handler) RegisterCompletionRoutes(r chi.Router) {
	r.With(h.auth).Post("/api/v1/households/{householdId}/items/{itemId}/completions", h.createCompletion)
	r.With(h.auth).Get("/api/v1/households/{householdId}/items/{itemId}/completions", h.listCompletions)
}
func (h *Handler) createCompletion(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, 401, "unauthenticated")
		return
	}
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) == 0 {
		httpx.ProblemResponse(w, 400, "idempotency_key_required")
		return
	}
	if len(keys) != 1 || !item.ValidateIdempotencyKey(keys[0]) {
		httpx.ProblemResponse(w, 400, "invalid_idempotency_key")
		return
	}
	values, ok := httpx.ReadObjectAllowEmpty(w, r, completionFields, completionBodyLimit)
	if !ok {
		return
	}
	req := item.CompletionRequest{}
	if v, ok := values["completedOn"]; ok {
		s := v.(string)
		req.CompletedOn = &s
	}
	result, _, err := h.service.Complete(r.Context(), uid, chi.URLParam(r, "householdId"), chi.URLParam(r, "itemId"), expected, keys[0], req)
	if err != nil {
		writeCompletionError(w, err)
		return
	}
	hID := strings.ToLower(chi.URLParam(r, "householdId"))
	iID := strings.ToLower(chi.URLParam(r, "itemId"))
	w.Header().Set("Location", "/api/v1/households/"+hID+"/items/"+iID+"/completions/"+strings.ToLower(result.ID))
	httpx.WriteJSON(w, 201, completionJSON(result))
}
func (h *Handler) listCompletions(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, 401, "unauthenticated")
		return
	}
	p, ok := httpx.ParseListParamsFor(w, r, item.MaxLimit, httpx.ListFilter(0))
	if !ok {
		return
	}

	page, err := h.service.ListCompletions(r.Context(), uid, chi.URLParam(r, "householdId"), chi.URLParam(r, "itemId"), p.Limit, p.Cursor)
	if err != nil {
		writeCompletionError(w, err)
		return
	}
	out := CompletionList{Items: make([]Completion, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, c := range page.Items {
		out.Items = append(out.Items, completionJSON(c))
	}
	httpx.WriteJSON(w, 200, out)
}
func completionJSON(c item.Completion) Completion {
	out := Completion{Id: c.ID, ItemId: c.ItemID, CompletedByUserId: c.CompletedByUserID, CompletedOn: c.CompletedOn.String(), CreatedAt: c.CreatedAt.UTC(), Recurrence: recurrenceJSON(c.Recurrence)}
	if c.CycleAttentionOn != nil {
		s := c.CycleAttentionOn.String()
		out.CycleAttentionOn = &s
	}
	if c.NextAttentionOn != nil {
		s := c.NextAttentionOn.String()
		out.NextAttentionOn = &s
	}
	return out
}
func writeCompletionError(w http.ResponseWriter, err error) {
	var v *item.ValidationError
	var q *item.InvalidQueryError
	switch {
	case errors.Is(err, item.ErrNotFound):
		httpx.ProblemResponse(w, 404, "not_found")
	case errors.Is(err, item.ErrVersionMismatch):
		httpx.ProblemResponse(w, 412, "precondition_failed")
	case errors.Is(err, item.ErrArchived):
		httpx.ProblemResponse(w, 409, "item_archived")
	case errors.Is(err, item.ErrDone):
		httpx.ProblemResponse(w, 409, "item_done")
	case errors.Is(err, item.ErrIdempotencyKeyReused):
		httpx.ProblemResponse(w, 422, "idempotency_key_reused")
	case errors.As(err, &v):
		if v.Field == "" {
			httpx.ProblemResponse(w, 400, v.Code)
		} else {
			httpx.ValidationProblemResponse(w, v.Field, v.Code)
		}
	case errors.As(err, &q):
		httpx.QueryProblemResponse(w, q.Parameter)
	default:
		httpx.ProblemResponse(w, 503, "unavailable")
	}
}
