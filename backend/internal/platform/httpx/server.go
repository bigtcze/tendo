package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// routeLabelKey carries a *string that handlers set to a fixed log route label.
type routeLabelKey struct{}

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
	return NewAppWithRoutes(pool, timeout, draining, policy, nil)
}
func NewAppWithRoutes(pool Pinger, timeout time.Duration, draining <-chan struct{}, policy OriginPolicy, register func(chi.Router)) http.Handler {
	return NewAppWithUI(pool, timeout, draining, policy, register, nil)
}

// NewAppWithUI is NewAppWithRoutes plus an optional UI fallback handler. The
// fallback receives every request (any method) that matches no route, except
// under /api and /health, which keep the problem+json 404/405 responses.
func NewAppWithUI(pool Pinger, timeout time.Duration, draining <-chan struct{}, policy OriginPolicy, register func(chi.Router), ui http.Handler) http.Handler {
	config, err := newOriginConfig(policy)
	return newRouterWithUI(pool, timeout, draining, originMiddlewareConfig(config, err), ui, register)
}
func newRouter(pool Pinger, timeout time.Duration, draining <-chan struct{}, origin func(http.Handler) http.Handler, registers ...func(chi.Router)) http.Handler {
	return newRouterWithUI(pool, timeout, draining, origin, nil, registers...)
}

func isReservedPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/health" || strings.HasPrefix(p, "/health/")
}

func newRouterWithUI(pool Pinger, timeout time.Duration, draining <-chan struct{}, origin func(http.Handler) http.Handler, ui http.Handler, registers ...func(chi.Router)) http.Handler {
	h := &Health{pool: pool, timeout: timeout, draining: draining}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler { return requestMiddleware(next, slog.Default()) })
	if origin != nil {
		r.Use(origin)
	}
	// Set before registering routes so mounted sub-routers inherit these handlers.
	notFound := func(w http.ResponseWriter, r *http.Request) { writeProblem(w, http.StatusNotFound, "Not Found") }
	methodNotAllowed := func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
	}
	if ui != nil {
		serveUI := func(w http.ResponseWriter, r *http.Request) {
			if label, ok := r.Context().Value(routeLabelKey{}).(*string); ok {
				*label = "webui"
			}
			ui.ServeHTTP(w, r)
		}
		inner, innerMethod := notFound, methodNotAllowed
		notFound = func(w http.ResponseWriter, r *http.Request) {
			if isReservedPath(r.URL.Path) {
				inner(w, r)
				return
			}
			serveUI(w, r)
		}
		methodNotAllowed = func(w http.ResponseWriter, r *http.Request) {
			if isReservedPath(r.URL.Path) {
				innerMethod(w, r)
				return
			}
			serveUI(w, r)
		}
	}
	r.NotFound(notFound)
	r.MethodNotAllowed(methodNotAllowed)
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { writeStatus(w, http.StatusOK, "ok") })
	r.Get("/health/ready", h.ready)
	if len(registers) > 0 && registers[0] != nil {
		registers[0](r)
	}
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

// RequestMiddlewareForTest exposes the production request logger wrapper for privacy regression tests.
func RequestMiddlewareForTest(next http.Handler, logger *slog.Logger) http.Handler {
	return requestMiddleware(next, logger)
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
		label := ""
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), routeLabelKey{}, &label)))
		route := label
		if route == "" {
			route = routePattern(r)
		}
		logger.Info("http request", "request_id", id, "method", r.Method, "route", route, "status", rw.status, "duration", time.Since(start))
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
