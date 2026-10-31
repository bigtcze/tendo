package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const setupStrictToken = "a-secret-of-at-least-thirty-two-bytes"

const setupStrictValid = `{"login":"owner_1","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}`

func serveSetupStrict(t *testing.T, body string, mutate func(*http.Request)) (*fakeService, *httptest.ResponseRecorder) {
	t.Helper()
	f := &fakeService{required: true}
	mux := chi.NewRouter()
	New(f, setupStrictToken).Register(mux)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tendo-Setup-Token", setupStrictToken)
	r.RemoteAddr = "192.0.2.1:1234"
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return f, w
}

// TestSetupStrictRequestHandling characterizes the setup body decoder so the
// shared httpx.ReadObject path keeps the exact problem status and code.
func TestSetupStrictRequestHandling(t *testing.T) {
	setContentType := func(value string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Content-Type", value) }
	}
	for _, tc := range []struct {
		name   string
		body   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{"missing media type", setupStrictValid, func(r *http.Request) { r.Header.Del("Content-Type") }, 415, "unsupported_media_type"},
		{"wrong charset", setupStrictValid, setContentType("application/json; charset=latin1"), 415, "unsupported_media_type"},
		{"extra media parameter", setupStrictValid, setContentType("application/json; charset=utf-8; x=y"), 415, "unsupported_media_type"},
		{"non charset parameter", setupStrictValid, setContentType("application/json; x=y"), 415, "unsupported_media_type"},
		{"malformed media type", setupStrictValid, setContentType("application/json; charset"), 415, "unsupported_media_type"},
		{"declared length over limit", setupStrictValid, func(r *http.Request) { r.ContentLength = 8193 }, 413, "content_too_large"},
		{"streamed body over limit", `{"login":"` + strings.Repeat("a", 8200) + `"}`, func(r *http.Request) { r.ContentLength = -1 }, 413, "content_too_large"},
		{"empty", ``, nil, 400, "invalid_request"},
		{"empty object", `{}`, nil, 400, "invalid_request"},
		{"array", `[]`, nil, 400, "invalid_request"},
		{"null document", `null`, nil, 400, "invalid_request"},
		{"string document", `"x"`, nil, 400, "invalid_request"},
		{"truncated", `{"login":"a"`, nil, 400, "invalid_request"},
		{"invalid utf8", "{\"login\":\"a\xff\",\"password\":\"p\",\"householdName\":\"H\",\"timezone\":\"UTC\"}", nil, 400, "invalid_request"},
		{"missing household", `{"login":"a","password":"p","timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"number value", `{"login":1,"password":"p","householdName":"H","timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"boolean value", `{"login":"a","password":true,"householdName":"H","timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"object value", `{"login":"a","password":"p","householdName":{},"timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"null household", `{"login":"a","password":"p","householdName":null,"timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"duplicate timezone", `{"login":"a","password":"p","householdName":"H","timezone":"UTC","timezone":"UTC"}`, nil, 400, "invalid_request"},
		{"trailing garbage", setupStrictValid + `x`, nil, 400, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, w := serveSetupStrict(t, tc.body, tc.mutate)
			assertSetupProblem(t, w, tc.status, tc.code)
			if f.calls != 0 {
				t.Fatalf("rejected request invoked service %d times", f.calls)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		body   string
		mutate func(*http.Request)
	}{
		{"case-insensitive media type with utf-8 charset", setupStrictValid, setContentType("Application/JSON; charset=UTF-8")},
		{"reordered keys with whitespace", " {\n\"timezone\":\"UTC\", \"householdName\":\"Home\",\"password\":\"a sufficiently long password\",\"login\":\"owner_1\"}\n ", nil},
		{"empty strings reach domain validation", `{"login":"","password":"","householdName":"","timezone":""}`, nil},
		{"body exactly at limit", `{"login":"owner_1","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}` + strings.Repeat(" ", 8192-len(setupStrictValid)), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, w := serveSetupStrict(t, tc.body, tc.mutate)
			if w.Code != http.StatusCreated || f.calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body)
			}
		})
	}
}
