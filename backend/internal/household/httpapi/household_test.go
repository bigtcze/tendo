package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/go-chi/chi/v5"
)

const (
	memberID  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	callerID  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	otherID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
	secretErr = "pq: password authentication failed for user tendo at 10.1.2.3"
)

// fakeService mimics the real service's observable behavior: only (caller,
// memberID) is a membership; everything else is not found.
type fakeService struct {
	calls int
	err   error
}

func (f *fakeService) Get(_ context.Context, userID, householdID string) (household.Household, error) {
	f.calls++
	if f.err != nil {
		return household.Household{}, f.err
	}
	if userID != callerID || householdID != memberID {
		return household.Household{}, household.ErrNotFound
	}
	return household.Household{ID: memberID, Name: "Veselí 家族", Timezone: "Europe/Prague", CreatedAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC), Version: 42}, nil
}

type fixture struct {
	svc      *fakeService
	handler  http.Handler
	authHits int
	inner    int
}

func newFixture(authorize bool) *fixture {
	f := &fixture{svc: &fakeService{}}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.authHits++
			if !authorize {
				problem(w, 401, "unauthenticated")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, callerID)))
		})
	}
	f.handler = func() http.Handler {
		r := chi.NewRouter()
		New(f.svc, auth, func(ctx context.Context) (string, bool) {
			v, ok := ctx.Value(userKey{}).(string)
			return v, ok
		}).Register(r)
		return r
	}()
	return f
}

type userKey struct{}

func (f *fixture) get(path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestGetHouseholdReturnsExactBodyAndHeaders(t *testing.T) {
	f := newFixture(true)
	w := f.get("/api/v1/households/" + memberID)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if got, want := strings.TrimSpace(w.Body.String()), `{"createdAt":"2026-10-06T10:00:00Z","id":"`+memberID+`","name":"Veselí 家族","timezone":"Europe/Prague"}`; !jsonEqual(t, got, want) {
		t.Fatalf("body=%s want=%s", got, want)
	}
	var keys map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &keys)
	if len(keys) != 4 {
		t.Fatalf("unexpected fields (version must not leak): %v", keys)
	}
	if w.Header().Get("ETag") != `"42"` || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("headers=%v", w.Header())
	}
}

func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatal(err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func TestNotFoundResponsesAreIdentical(t *testing.T) {
	f := newFixture(true)
	var first string
	for name, id := range map[string]string{
		"non member": otherID,
		"malformed":  "not-a-uuid",
		"empty-ish":  "%20",
	} {
		w := f.get("/api/v1/households/" + id)
		if w.Code != 404 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("ETag") != "" {
			t.Fatalf("%s: status=%d headers=%v", name, w.Code, w.Header())
		}
		if want := `{"code":"not_found","status":404,"title":"Not Found","type":"about:blank"}`; !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("%s: body=%s", name, w.Body)
		}
		if first == "" {
			first = w.Body.String()
		} else if w.Body.String() != first {
			t.Fatalf("%s: body differs: %s vs %s", name, w.Body, first)
		}
	}
}

func TestUnauthenticatedShortCircuitsBeforeService(t *testing.T) {
	f := newFixture(false)
	w := f.get("/api/v1/households/" + memberID)
	if w.Code != 401 || f.svc.calls != 0 || f.authHits != 1 {
		t.Fatalf("status=%d serviceCalls=%d authHits=%d", w.Code, f.svc.calls, f.authHits)
	}
	if strings.Contains(w.Body.String(), memberID) || w.Header().Get("ETag") != "" {
		t.Fatalf("leaked household data: %s", w.Body)
	}
}

func TestMissingPrincipalAfterAuthIsUnauthorizedNotPanic(t *testing.T) {
	svc := &fakeService{}
	r := chi.NewRouter()
	New(svc, func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) { return "", false }).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/households/"+memberID, nil))
	if w.Code != 401 || svc.calls != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, svc.calls)
	}
}

func TestPersistenceFailureIs503WithoutInternals(t *testing.T) {
	f := newFixture(true)
	f.svc.err = errors.New(secretErr)
	w := f.get("/api/v1/households/" + memberID)
	if w.Code != 503 || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if strings.Contains(w.Body.String(), "pq") || strings.Contains(w.Body.String(), "10.1.2.3") {
		t.Fatalf("internal text leaked: %s", w.Body)
	}
	if want := `{"code":"unavailable","status":503,"title":"Service Unavailable","type":"about:blank"}`; !jsonEqual(t, w.Body.String(), want) {
		t.Fatalf("body=%s", w.Body)
	}
}
