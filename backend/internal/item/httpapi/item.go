package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

type service interface {
	Create(ctx context.Context, userID, householdID, key string, n item.NewItem) (item.Item, error)
	Get(ctx context.Context, userID, householdID, itemID string) (item.Item, error)
	List(ctx context.Context, userID, householdID string, q item.ListQuery) (item.Page, error)
	Summary(ctx context.Context, userID, householdID string, q item.SummaryQuery) (item.Summary, error)
	Update(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, p item.Patch) (item.Item, error)
	Complete(
		ctx context.Context, userID, householdID, itemID string, expectedVersion int64, key string,
		request item.CompletionRequest,
	) (item.Completion, bool, error)
	UndoCompletion(ctx context.Context, userID, householdID, itemID, completionID string, expectedVersion int64) (item.Completion, error)
	ListCompletions(ctx context.Context, userID, householdID, itemID string, limit int, cursor, order string) (item.CompletionPage, error)
}

// Handler serves item resources. Authentication is injected so this package
// does not depend on the identity adapter.
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
	r.With(h.auth).Post("/api/v1/households/{householdId}/items", h.create)
	r.With(h.auth).Get("/api/v1/households/{householdId}/items", h.list)
	r.With(h.auth).Get("/api/v1/households/{householdId}/items/summary", h.summary)
	r.With(h.auth).Get("/api/v1/households/{householdId}/items/{itemId}", h.get)
	r.With(h.auth).Patch("/api/v1/households/{householdId}/items/{itemId}", h.update)
	h.RegisterCompletionRoutes(r)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, createFields, bodyLimit)
	if !ok {
		return
	}
	n, err := newItemFromBody(values)
	if err != nil {
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
		return
	}
	householdID := chi.URLParam(r, "householdId")
	created, err := h.service.Create(r.Context(), userID, householdID, key, n)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	location := "/api/v1/households/" + strings.ToLower(householdID) + "/items/" + strings.ToLower(created.ID)
	w.Header().Set("Location", location)
	w.Header().Set("ETag", httpx.ETag(created.Version))
	httpx.WriteJSON(w, http.StatusCreated, toJSON(created))
}

// idempotencyKey reads the single required Idempotency-Key header shared by
// item create and completion create. On failure it writes the 400 problem.
func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) == 0 {
		httpx.ProblemResponse(w, http.StatusBadRequest, "idempotency_key_required")
		return "", false
	}
	if len(keys) != 1 || !item.ValidateIdempotencyKey(keys[0]) {
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_idempotency_key")
		return "", false
	}
	return keys[0], true
}

// list validates transport structure in the documented order: shared list
// parameters (limit, cursor, archived, done), then filters, sort,
// attentionDate and q. Membership and semantic checks follow in the service.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	params, ok := httpx.ParseListParamsFor(w, r, item.MaxLimit, httpx.ListFilterArchivedDone)
	if !ok {
		return
	}
	q := newQueryReader(r.URL.Query())
	filters := q.collectionFilters()
	sortBy := item.ListSort(q.scalar("sort"))
	attentionDate := q.scalar("attentionDate")
	search := q.scalar("q")
	if q.invalid != "" {
		httpx.QueryProblemResponse(w, q.invalid)
		return
	}
	page, err := h.service.List(r.Context(), userID, chi.URLParam(r, "householdId"), item.ListQuery{
		Sort:              sortBy,
		AttentionDate:     attentionDate,
		Archived:          params.Archived,
		Done:              params.Done,
		Limit:             params.Limit,
		Cursor:            params.Cursor,
		SubjectID:         filters.subjectID,
		ResponsibleUserID: filters.responsibleUserID,
		WorkflowState:     filters.workflowState,
		Attention:         filters.attention,
		Recurring:         filters.recurring,
		Q:                 search,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listJSON(page))
}

// summary counts the whole filtered collection, so paging, lifecycle and sort
// parameters are rejected before the shared filters are read.
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	q := newQueryReader(r.URL.Query())
	q.forbid("limit", "cursor", "archived", "done", "sort")
	filters := q.collectionFilters()
	attentionDate := q.scalar("attentionDate")
	search := q.scalar("q")
	if q.invalid != "" {
		httpx.QueryProblemResponse(w, q.invalid)
		return
	}
	result, err := h.service.Summary(r.Context(), userID, chi.URLParam(r, "householdId"), filters.summaryQuery(attentionDate, search))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summaryJSON(result))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	found, err := h.service.Get(r.Context(), userID, chi.URLParam(r, "householdId"), chi.URLParam(r, "itemId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(found.Version))
	httpx.WriteJSON(w, http.StatusOK, toJSON(found))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, updateFields, bodyLimit)
	if !ok {
		return
	}
	patch, err := patchFromBody(values)
	if err != nil {
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
		return
	}
	householdID, itemID := chi.URLParam(r, "householdId"), chi.URLParam(r, "itemId")
	updated, err := h.service.Update(r.Context(), userID, householdID, itemID, expected, patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(updated.Version))
	httpx.WriteJSON(w, http.StatusOK, toJSON(updated))
}
