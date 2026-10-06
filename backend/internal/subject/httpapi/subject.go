package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/go-chi/chi/v5"
)

const bodyLimit = 4096

var strongETag = regexp.MustCompile(`^"[1-9][0-9]*"$`)

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
	values, ok := readObject(w, r, map[string]string{"name": "string", "type": "string"}, true)
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
	w.Header().Set("ETag", etag(created.Version))
	writeJSON(w, http.StatusCreated, toJSON(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	query := subject.ListQuery{}
	values := r.URL.Query()
	if v, present := values["limit"]; present {
		n, ok := parseLimit(v)
		if !ok {
			queryProblem(w, "limit")
			return
		}
		query.Limit = n
	}
	if v, present := values["cursor"]; present {
		if len(v) != 1 || v[0] == "" {
			queryProblem(w, "cursor")
			return
		}
		query.Cursor = v[0]
	}
	if v, present := values["archived"]; present {
		if len(v) != 1 || (v[0] != "true" && v[0] != "false") {
			queryProblem(w, "archived")
			return
		}
		query.Archived = v[0] == "true"
	}
	page, err := h.service.List(r.Context(), userID, chi.URLParam(r, "householdId"), query)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	items := make([]Subject, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toJSON(item))
	}
	writeJSON(w, http.StatusOK, SubjectList{Items: items, NextCursor: page.NextCursor})
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
	w.Header().Set("ETag", etag(found.Version))
	writeJSON(w, http.StatusOK, toJSON(found))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(r.Context())
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	matches := r.Header.Values("If-Match")
	if len(matches) == 0 {
		problem(w, http.StatusPreconditionRequired, "precondition_required")
		return
	}
	if len(matches) != 1 || !strongETag.MatchString(matches[0]) {
		problem(w, http.StatusPreconditionFailed, "precondition_failed")
		return
	}
	expected, err := strconv.ParseInt(strings.Trim(matches[0], `"`), 10, 64)
	if err != nil {
		problem(w, http.StatusPreconditionFailed, "precondition_failed")
		return
	}
	values, ok := readObject(w, r, map[string]string{"name": "string", "type": "string", "archived": "bool"}, false)
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
	w.Header().Set("ETag", etag(updated.Version))
	writeJSON(w, http.StatusOK, toJSON(updated))
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
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": "Validation Failed", "status": 422, "code": validation.Code, "field": validation.Field})
	case errors.As(err, &query):
		queryProblem(w, query.Parameter)
	default:
		problem(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func parseLimit(values []string) (int, bool) {
	if len(values) != 1 || values[0] == "" || len(values[0]) > 4 {
		return 0, false
	}
	for _, c := range values[0] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n < 1 || n > subject.MaxLimit {
		return 0, false
	}
	return n, true
}

func etag(version int64) string { return `"` + strconv.FormatInt(version, 10) + `"` }

func toJSON(s subject.Subject) Subject {
	return Subject{Id: s.ID, Type: SubjectType(s.Type), Name: s.Name, Archived: s.Archived, CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC()}
}

// readObject enforces JSON media type, a body size bound, and the strict object
// decoder. kinds maps allowed keys to "string" or "bool". When all is true every
// key is required; otherwise at least one key is required. On failure it writes
// the problem response and returns false.
func readObject(w http.ResponseWriter, r *http.Request, kinds map[string]string, all bool) (map[string]any, bool) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	charset, hasCharset := params["charset"]
	if err != nil || !strings.EqualFold(media, "application/json") || len(params) > 1 || (len(params) == 1 && (!hasCharset || !strings.EqualFold(charset, "utf-8"))) {
		problem(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return nil, false
	}
	if r.ContentLength > bodyLimit {
		problem(w, http.StatusRequestEntityTooLarge, "content_too_large")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
	if err != nil {
		problem(w, http.StatusRequestEntityTooLarge, "content_too_large")
		return nil, false
	}
	values, err := decodeObject(body, kinds, all)
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	return values, true
}

// decodeObject decodes a JSON object whose keys come from kinds, without
// duplicates, unknown keys, nulls, or wrongly typed values.
func decodeObject(body []byte, kinds map[string]string, all bool) (map[string]any, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	values := make(map[string]any, len(kinds))
	for dec.More() {
		keyToken, err := dec.Token()
		key, ok := keyToken.(string)
		kind, allowed := kinds[key]
		if err != nil || !ok || !allowed {
			return nil, fmt.Errorf("unexpected key")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil || string(raw) == "null" {
			return nil, fmt.Errorf("invalid value")
		}
		switch kind {
		case "string":
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("expected string")
			}
			values[key] = v
		case "bool":
			var v bool
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("expected boolean")
			}
			values[key] = v
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing data")
	}
	if len(values) == 0 || (all && len(values) != len(kinds)) {
		return nil, fmt.Errorf("incomplete object")
	}
	return values, nil
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

func queryProblem(w http.ResponseWriter, parameter string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusBadRequest), "status": http.StatusBadRequest, "code": "invalid_query", "parameter": parameter})
}
