package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/go-chi/chi/v5"
)

const bodyLimit = 64 << 10

var (
	createFields = map[string]httpx.Field{
		"title":       {Kind: httpx.KindString, Required: true},
		"subjectId":   {Kind: httpx.KindString, Required: true},
		"notes":       {Kind: httpx.KindNullableString},
		"attentionOn": {Kind: httpx.KindNullableString},
		"recurrence":  {Kind: httpx.KindNullableObject, Fields: recurrenceFields},
	}

	recurrenceFields = map[string]httpx.Field{
		"intervalValue": {Kind: httpx.KindInteger, Required: true},
		"intervalUnit":  {Kind: httpx.KindString, Required: true},
		"mode":          {Kind: httpx.KindString, Required: true},
	}
	updateFields = map[string]httpx.Field{
		"title":         {Kind: httpx.KindString},
		"subjectId":     {Kind: httpx.KindString},
		"notes":         {Kind: httpx.KindNullableString},
		"attentionOn":   {Kind: httpx.KindNullableString},
		"recurrence":    {Kind: httpx.KindNullableObject, Fields: recurrenceFields},
		"workflowState": {Kind: httpx.KindString},
		"archived":      {Kind: httpx.KindBool},
	}
)

type service interface {
	Create(ctx context.Context, userID, householdID string, n item.NewItem) (item.Item, error)
	Get(ctx context.Context, userID, householdID, itemID string) (item.Item, error)
	List(ctx context.Context, userID, householdID string, q item.ListQuery) (item.Page, error)
	Update(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, p item.Patch) (item.Item, error)
	Complete(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, key string, request item.CompletionRequest) (item.Completion, bool, error)
	UndoCompletion(ctx context.Context, userID, householdID, itemID, completionID string, expectedVersion int64) (item.Completion, error)
	ListCompletions(ctx context.Context, userID, householdID, itemID string, limit int, cursor string) (item.CompletionPage, error)
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
	values, ok := httpx.ReadObject(w, r, createFields, bodyLimit)
	if !ok {
		return
	}
	n := item.NewItem{SubjectID: values["subjectId"].(string), Title: values["title"].(string)}
	if v, present := values["notes"]; present && v != nil {
		s := v.(string)
		n.Notes = &s
	}
	if v, present := values["attentionOn"]; present && v != nil {
		s := v.(string)
		n.AttentionOn = &s
	}
	if v, present := values["recurrence"]; present && v != nil {
		policy, err := recurrence(v)
		if err != nil {
			httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
			return
		}
		n.Recurrence = policy
	}
	householdID := chi.URLParam(r, "householdId")
	created, err := h.service.Create(r.Context(), userID, householdID, n)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/households/"+strings.ToLower(householdID)+"/items/"+strings.ToLower(created.ID))
	w.Header().Set("ETag", httpx.ETag(created.Version))
	httpx.WriteJSON(w, http.StatusCreated, toJSON(created))
}

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
	page, err := h.service.List(r.Context(), userID, chi.URLParam(r, "householdId"), item.ListQuery{Archived: params.Archived, Done: params.Done, Limit: params.Limit, Cursor: params.Cursor})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	items := make([]Item, 0, len(page.Items))
	for _, found := range page.Items {
		items = append(items, toJSON(found))
	}
	httpx.WriteJSON(w, http.StatusOK, ItemList{Items: items, NextCursor: page.NextCursor})
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
	var patch item.Patch
	if v, present := values["title"]; present {
		s := v.(string)
		patch.Title = &s
	}
	if v, present := values["subjectId"]; present {
		s := v.(string)
		patch.SubjectID = &s
	}
	if v, present := values["notes"]; present {
		patch.Notes = nullable(v)
	}
	if v, present := values["attentionOn"]; present {
		patch.AttentionOn = nullable(v)
	}
	if v, present := values["recurrence"]; present {
		if v == nil {
			patch.Recurrence = item.Null[schedule.Policy]()
		} else {
			policy, err := recurrence(v)
			if err != nil {
				httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
				return
			}
			patch.Recurrence = item.Some(*policy)
		}
	}
	if v, present := values["workflowState"]; present {
		s := v.(string)
		patch.WorkflowState = &s
	}
	if v, present := values["archived"]; present {
		b := v.(bool)
		patch.Archived = &b
	}
	updated, err := h.service.Update(r.Context(), userID, chi.URLParam(r, "householdId"), chi.URLParam(r, "itemId"), expected, patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(updated.Version))
	httpx.WriteJSON(w, http.StatusOK, toJSON(updated))
}

// nullable converts a decoded string-or-null value.
func nullable(v any) item.Nullable[string] {
	if v == nil {
		return item.Null[string]()
	}
	return item.Some(v.(string))
}

func recurrence(v any) (*schedule.Policy, error) {
	fields := v.(map[string]any)
	value, ok := fields["intervalValue"].(int64)
	if !ok {
		return nil, errors.New("invalid interval")
	}
	unit, ok := fields["intervalUnit"].(string)
	if !ok {
		return nil, errors.New("invalid unit")
	}
	mode, ok := fields["mode"].(string)
	if !ok {
		return nil, errors.New("invalid mode")
	}
	interval := 0
	if value >= 1 && value <= 999 {
		interval = int(value)
	}
	return &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: interval, Unit: schedule.Unit(unit)}, Mode: schedule.Mode(mode)}, nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	var validation *item.ValidationError
	var query *item.InvalidQueryError
	switch {
	case errors.Is(err, item.ErrNotFound):
		httpx.ProblemResponse(w, http.StatusNotFound, "not_found")
	case errors.Is(err, item.ErrEmptyPatch):
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, item.ErrVersionMismatch):
		httpx.ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	case errors.As(err, &query):
		httpx.QueryProblemResponse(w, query.Parameter)
	default:
		httpx.ProblemResponse(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func recurrenceJSON(policy *schedule.Policy) *ItemRecurrence {
	if policy == nil {
		return nil
	}
	return &ItemRecurrence{IntervalValue: policy.Interval.Value, IntervalUnit: RecurrenceIntervalUnit(policy.Interval.Unit), Mode: RecurrenceMode(policy.Mode)}
}

func toJSON(i item.Item) Item {
	done := i.Done
	out := Item{Id: i.ID, SubjectId: i.SubjectID, Title: i.Title, Notes: i.Notes, Recurrence: recurrenceJSON(i.Recurrence), WorkflowState: WorkflowState(i.WorkflowState), Attention: ItemAttention(i.Attention), Archived: i.Archived, Done: &done, CreatedAt: i.CreatedAt.UTC(), UpdatedAt: i.UpdatedAt.UTC()}
	if i.AttentionOn != nil {
		s := i.AttentionOn.String()
		out.AttentionOn = &s
	}
	var last *string
	if i.LastCompletedOn != nil {
		s := i.LastCompletedOn.String()
		last = &s
	}
	out.LastCompletedOn = &last
	return out
}
