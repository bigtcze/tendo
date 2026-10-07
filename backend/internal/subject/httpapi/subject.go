package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/go-chi/chi/v5"
)

const bodyLimit = 4096

var (
	createFields = map[string]httpx.Field{"name": {Kind: httpx.KindString, Required: true}, "type": {Kind: httpx.KindString, Required: true}}
	updateFields = map[string]httpx.Field{"name": {Kind: httpx.KindString}, "type": {Kind: httpx.KindString}, "archived": {Kind: httpx.KindBool}}
)

type service interface {
	Create(ctx context.Context, userID, householdID, name string, t subject.Type) (subject.Subject, error)
	Get(ctx context.Context, userID, householdID, subjectID string) (subject.Subject, error)
	List(ctx context.Context, userID, householdID string, q subject.ListQuery) (subject.Page, error)
	Update(ctx context.Context, userID, householdID, subjectID string, expectedVersion int64, p subject.Patch) (subject.Subject, error)
}

// Handler serves subject resources. Authentication is injected so this package
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
	r.With(h.auth).Post("/api/v1/households/{householdId}/subjects", h.create)
	r.With(h.auth).Get("/api/v1/households/{householdId}/subjects", h.list)
	r.With(h.auth).Get("/api/v1/households/{householdId}/subjects/{subjectId}", h.get)
	r.With(h.auth).Patch("/api/v1/households/{householdId}/subjects/{subjectId}", h.update)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	values, ok := httpx.ReadObject(w, r, createFields, bodyLimit)
	if !ok {
		return
	}
	householdID := chi.URLParam(r, "householdId")
	created, err := h.service.Create(r.Context(), userID, householdID, values["name"].(string), subject.Type(values["type"].(string)))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/households/"+strings.ToLower(householdID)+"/subjects/"+strings.ToLower(created.ID))
	w.Header().Set("ETag", httpx.ETag(created.Version))
	httpx.WriteJSON(w, http.StatusCreated, toJSON(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	params, ok := httpx.ParseListParams(w, r, subject.MaxLimit)
	if !ok {
		return
	}
	query := subject.ListQuery{Archived: params.Archived, Limit: params.Limit, Cursor: params.Cursor}
	page, err := h.service.List(r.Context(), userID, chi.URLParam(r, "householdId"), query)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	items := make([]Subject, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toJSON(item))
	}
	httpx.WriteJSON(w, http.StatusOK, SubjectList{Items: items, NextCursor: page.NextCursor})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	found, err := h.service.Get(r.Context(), userID, chi.URLParam(r, "householdId"), chi.URLParam(r, "subjectId"))
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
		problem(w, http.StatusUnauthorized, "unauthenticated")
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
	var patch subject.Patch
	if v, present := values["name"]; present {
		s := v.(string)
		patch.Name = &s
	}
	if v, present := values["type"]; present {
		t := subject.Type(v.(string))
		patch.Type = &t
	}
	if v, present := values["archived"]; present {
		b := v.(bool)
		patch.Archived = &b
	}
	updated, err := h.service.Update(r.Context(), userID, chi.URLParam(r, "householdId"), chi.URLParam(r, "subjectId"), expected, patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("ETag", httpx.ETag(updated.Version))
	httpx.WriteJSON(w, http.StatusOK, toJSON(updated))
}

func writeServiceError(w http.ResponseWriter, err error) {
	var validation *subject.ValidationError
	var query *subject.InvalidQueryError
	switch {
	case errors.Is(err, subject.ErrNotFound):
		problem(w, http.StatusNotFound, "not_found")
	case errors.Is(err, subject.ErrEmptyPatch):
		problem(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, subject.ErrVersionMismatch):
		problem(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	case errors.As(err, &query):
		httpx.QueryProblemResponse(w, query.Parameter)
	default:
		problem(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func toJSON(s subject.Subject) Subject {
	return Subject{Id: s.ID, Type: SubjectType(s.Type), Name: s.Name, Archived: s.Archived, CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC()}
}

// problem keeps the handler's call sites short.
func problem(w http.ResponseWriter, status int, code string) { httpx.ProblemResponse(w, status, code) }
