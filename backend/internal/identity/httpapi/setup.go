package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
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
	return &Handler{service: s, token: token, limiter: newLimiter(5)}
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
	values, ok := readIdentityStrings(w, r, 8192, "login", "password", "householdName", "timezone")
	if !ok {
		return
	}
	payload := SetupRequest{Login: values.login, Password: values.password, HouseholdName: values.householdName, Timezone: values.timezone}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		problem(w, 503, "unavailable")
		return
	}
	err := h.service.CreateOwner(ctx, identity.SetupInput{
		Login:         payload.Login,
		Password:      payload.Password,
		HouseholdName: payload.HouseholdName,
		Timezone:      payload.Timezone,
	})
	if errors.Is(err, security.ErrPasswordWorkLimit) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	if errors.Is(err, identity.ErrComplete) {
		problem(w, 409, "setup_complete")
		return
	}
	var validation *identity.ValidationError
	if errors.As(err, &validation) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "about:blank", "title": "Validation Failed", "status": 422,
			"code": validation.Code, "field": validation.Field,
		})
		return
	}
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	w.Header().Set("Location", "/api/v1/auth/setup")
	writeJSON(w, 201, SetupStatus{Required: false})
}

type identityStrings struct {
	login, password, householdName, timezone string
}

func readIdentityStrings(w http.ResponseWriter, r *http.Request, limit int64, keys ...string) (identityStrings, bool) {
	fields := make(map[string]httpx.Field, len(keys))
	for _, key := range keys {
		fields[key] = httpx.Field{Kind: httpx.KindString, Required: true}
	}
	values, ok := httpx.ReadObject(w, r, fields, limit)
	if !ok {
		return identityStrings{}, false
	}
	return identityStrings{
		login:         stringField(values, "login"),
		password:      stringField(values, "password"),
		householdName: stringField(values, "householdName"),
		timezone:      stringField(values, "timezone"),
	}, true
}

func stringField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
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
