package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type Pinger interface{ Ping(context.Context) error }

type Health struct {
	pool     Pinger
	timeout  time.Duration
	draining <-chan struct{}
}

func NewHealth(pool Pinger, timeout time.Duration, draining <-chan struct{}) http.Handler {
	return newRouter(pool, timeout, draining, nil)
}
func NewApp(pool Pinger, timeout time.Duration, draining <-chan struct{}, policy OriginPolicy) http.Handler {
	config, err := newOriginConfig(policy)
	return newRouter(pool, timeout, draining, originMiddlewareConfig(config, err))
}
func newRouter(pool Pinger, timeout time.Duration, draining <-chan struct{}, origin func(http.Handler) http.Handler) http.Handler {
	h := &Health{pool: pool, timeout: timeout, draining: draining}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler { return requestMiddleware(next, slog.Default()) })
	if origin != nil {
		r.Use(origin)
	}
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { writeStatus(w, http.StatusOK, "ok") })
	r.Get("/health/ready", h.ready)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeProblem(w, http.StatusNotFound, "Not Found") })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
	})
	return r
}

func (h *Health) ready(w http.ResponseWriter, r *http.Request) {
	select {
	case <-h.draining:
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	default:
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if h.pool == nil || h.pool.Ping(ctx) != nil {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	select {
	case <-h.draining:
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeStatus(w, http.StatusOK, "ok")
	}
}
func writeStatus(w http.ResponseWriter, code int, value string) {
	if code == http.StatusServiceUnavailable {
		writeProblem(w, code, "Service Unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(StatusResponse{Status: value})
}

func writeProblem(w http.ResponseWriter, code int, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(Problem{Type: "about:blank", Title: title, Status: code})
}
func requestMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !safeID.MatchString(id) {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-ID", id)
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rw, r)
		logger.Info("http request", "request_id", id, "method", r.Method, "route", routePattern(r), "status", rw.status, "duration", time.Since(start))
	})
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
