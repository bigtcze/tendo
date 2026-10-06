package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

type service interface {
	Required(context.Context) (bool, error)
	CreateOwner(context.Context, identity.SetupInput) error
}
type Handler struct {
	service service
	token   string
	limiter *limiter
}

func New(s service, token string) *Handler {
	return &Handler{service: s, token: token, limiter: newLimiter(5, 2)}
}
func (h *Handler) Register(r chi.Router) {
	r.Get("/api/v1/auth/setup", h.get)
	r.Put("/api/v1/auth/setup", h.put)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	required, err := h.service.Required(ctx)
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	etag := `"setup-v1-complete"`
	if required {
		etag = `"setup-v1-required"`
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, 200, SetupStatus{Required: required})
}
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	if h.token == "" {
		problem(w, 503, "setup_unavailable")
		return
	}
	if !h.limiter.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	tokenValues := r.Header.Values("X-Tendo-Setup-Token")
	supplied := ""
	if len(tokenValues) == 1 {
		supplied = tokenValues[0]
	}
	if len(supplied) > 64 || len(supplied) != len(h.token) || subtle.ConstantTimeCompare([]byte(supplied), []byte(h.token)) != 1 {
		problem(w, 401, "unauthorized")
		return
	}
	values, ok := readStrictObject(w, r, 8192, "login", "password", "householdName", "timezone")
	if !ok {
		return
	}
	payload := SetupRequest{Login: values["login"], Password: values["password"], HouseholdName: values["householdName"], Timezone: values["timezone"]}
	if !h.limiter.acquire() {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	defer h.limiter.release()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		problem(w, 503, "unavailable")
		return
	}
	err := h.service.CreateOwner(ctx, identity.SetupInput{Login: payload.Login, Password: payload.Password, HouseholdName: payload.HouseholdName, Timezone: payload.Timezone})
	if errors.Is(err, identity.ErrComplete) {
		problem(w, 409, "setup_complete")
		return
	}
	var validation *identity.ValidationError
	if errors.As(err, &validation) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": "Validation Failed", "status": 422, "code": validation.Code, "field": validation.Field})
		return
	}
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	w.Header().Set("Location", "/api/v1/auth/setup")
	writeJSON(w, 201, SetupStatus{Required: false})
}

// readStrictObject enforces JSON media type, a body size bound, and the strict
// object decoder. On failure it writes the problem response and returns false.
func readStrictObject(w http.ResponseWriter, r *http.Request, limit int64, keys ...string) (map[string]string, bool) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	charset, hasCharset := params["charset"]
	if err != nil || !strings.EqualFold(media, "application/json") || len(params) > 1 || (len(params) == 1 && (!hasCharset || !strings.EqualFold(charset, "utf-8"))) {
		problem(w, 415, "unsupported_media_type")
		return nil, false
	}
	if r.ContentLength > limit {
		problem(w, 413, "content_too_large")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		problem(w, 413, "content_too_large")
		return nil, false
	}
	values, err := decodeStrictObject(body, keys...)
	if err != nil {
		problem(w, 400, "invalid_request")
		return nil, false
	}
	return values, true
}

// decodeStrictObject decodes a JSON object whose keys are exactly the allowed
// set (no duplicates, unknown keys, nulls, or non-string values).
func decodeStrictObject(body []byte, keys ...string) (map[string]string, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	values := make(map[string]string, len(allowed))
	for dec.More() {
		keyToken, err := dec.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || !allowed[key] {
			return nil, fmt.Errorf("unexpected key")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil || string(raw) == "null" {
			return nil, fmt.Errorf("invalid value")
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("expected string")
		}
		values[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF || len(values) != len(allowed) {
		return nil, fmt.Errorf("incomplete object")
	}
	return values, nil
}

func clientIP(r *http.Request) string {
	if m, ok := httpx.MetadataFromRequest(r); ok {
		return m.ClientIP
	}
	return ""
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
