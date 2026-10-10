package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// contractOperation is one OpenAPI operation and its declared security scheme.
// scheme is "" for operations declared with `security: []`.
type contractOperation struct{ method, path, scheme string }

var (
	contractPathLine     = regexp.MustCompile(`^  (/\S*):$`)
	contractMethodLine   = regexp.MustCompile(`^    (get|put|post|patch|delete):$`)
	contractSecurityLine = regexp.MustCompile(`^      security:\s*(.*)$`)
	contractSchemeLine   = regexp.MustCompile(`^        - ([A-Za-z]+): \[\]$`)
	contractInlineScheme = regexp.MustCompile(`^\[\{ ([A-Za-z]+): \[\] \}\]$`)
	contractPathKeyLine  = regexp.MustCompile(`^    ([A-Za-z$-]+):`)
	contractListEntry    = regexp.MustCompile(`^        - `)
)

// readContractSecurity extracts the per-operation security requirement from
// api/openapi.yaml. It fails closed: every operation must declare security
// explicitly with a single known scheme or `[]`, so a new operation or a
// compound requirement cannot silently escape this test.
func readContractSecurity(t *testing.T) []contractOperation {
	t.Helper()
	f, err := os.Open("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ops []contractOperation
	var path string
	current := -1
	pendingList, consumedList := false, false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "components:" {
			break
		}
		if pendingList {
			pendingList = false
			m := contractSchemeLine.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("%s %s: unsupported security list entry %q", ops[current].method, ops[current].path, line)
			}
			ops[current].scheme = m[1]
			consumedList = true
			continue
		}
		if consumedList {
			consumedList = false
			if contractListEntry.MatchString(line) {
				t.Fatalf("%s %s: alternative security requirements are not supported by this test: %q", ops[current].method, ops[current].path, line)
			}
		}
		if m := contractPathLine.FindStringSubmatch(line); m != nil {
			path, current = m[1], -1
			continue
		}
		if m := contractMethodLine.FindStringSubmatch(line); m != nil {
			ops = append(ops, contractOperation{method: strings.ToUpper(m[1]), path: path, scheme: "?"})
			current = len(ops) - 1
			continue
		}
		if m := contractPathKeyLine.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "summary", "description", "parameters", "servers":
			default:
				t.Fatalf("%s: unsupported path item key %q", path, m[1])
			}
			current = -1
			continue
		}
		if m := contractSecurityLine.FindStringSubmatch(line); m != nil && current >= 0 {
			switch value := strings.TrimSpace(m[1]); {
			case value == "[]":
				ops[current].scheme = ""
			case value == "":
				pendingList = true
			case contractInlineScheme.MatchString(value):
				ops[current].scheme = contractInlineScheme.FindStringSubmatch(value)[1]
			default:
				t.Fatalf("%s %s: unsupported security requirement %q", ops[current].method, ops[current].path, value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		switch op.scheme {
		case "", "SessionCookie", "SetupToken":
		default:
			t.Fatalf("%s %s: missing or unknown security scheme %q", op.method, op.path, op.scheme)
		}
	}
	if len(ops) < 25 {
		t.Fatalf("parsed only %d contract operations; parser out of sync with api/openapi.yaml", len(ops))
	}
	return ops
}

var contractPathParam = regexp.MustCompile(`\{[A-Za-z]+\}`)

// TestComposedRoutesEnforceContractSecurity replaces the generated
// SessionCookieScopes/SetupTokenScopes constants that oapi-codegen v2.8.0 no
// longer emits. Those constants never enforced anything; this test checks the
// real production composition instead: every /api/v1 contract operation is
// routed, SessionCookie operations reject a missing or duplicated session cookie
// with 401 before any persistence and authenticate a well-formed cookie against
// the session store before the handler runs, SetupToken operations reject a missing token
// with 401, and public operations are not guarded by session authentication.
func TestComposedRoutesEnforceContractSecurity(t *testing.T) {
	// The pool connects lazily to a closed port, so any handler that reaches
	// persistence fails fast with 503 instead of a security status.
	pool, err := pgxpool.New(t.Context(), "postgres://tendo@127.0.0.1:1/tendo?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := config.Config{
		PublicURL:        "http://127.0.0.1",
		SetupToken:       strings.Repeat("A", 43) + "=",
		OIDCIssuer:       "http://127.0.0.1:1",
		OIDCClientID:     "client",
		OIDCClientSecret: "secret",
		OIDCDisplayName:  "Contract IdP",
	}
	routes := chi.NewRouter()
	if err := registerAPI(routes, cfg, pool); err != nil {
		t.Fatal(err)
	}

	registered := map[string]bool{}
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	const validSetup = `{"login":"owner_contract","password":"correct horse battery","householdName":"Home","timezone":"Europe/Prague"}`
	// Expected anonymous outcomes for public operations with a valid request.
	// Persistence is unreachable, so reaching the service yields 503.
	publicOutcomes := map[string]struct {
		body   string
		status int
		code   string
	}{
		"GET /api/v1/auth/setup":               {"", http.StatusServiceUnavailable, "unavailable"},
		"GET /api/v1/auth/oidc":                {"", http.StatusOK, ""},
		"POST /api/v1/auth/oidc/start":         {`{"purpose":"login"}`, http.StatusServiceUnavailable, "oidc_unavailable"},
		"GET /api/v1/auth/oidc/callback":       {"", http.StatusSeeOther, ""},
		"POST /api/v1/session":                 {`{"login":"owner_contract","password":"correct horse battery"}`, http.StatusServiceUnavailable, "unavailable"},
		"DELETE /api/v1/session":               {"", http.StatusNoContent, ""},
		"POST /api/v1/auth/invitations/accept": {`{"token":"` + strings.Repeat("A", 43) + `","login":"member_contract","password":"correct horse battery"}`, http.StatusServiceUnavailable, "unavailable"},
	}
	client := 0

	contract := map[string]bool{}
	for _, op := range readContractSecurity(t) {
		if !strings.HasPrefix(op.path, "/api/v1/") {
			continue
		}
		key := op.method + " " + op.path
		contract[key] = true
		if !registered[key] {
			t.Errorf("%s: declared in the contract but not routed", key)
			continue
		}
		target := contractPathParam.ReplaceAllString(op.path, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61")
		sendWith := func(body string, header http.Header, cookies ...string) *httptest.ResponseRecorder {
			client++
			r := httptest.NewRequest(op.method, target, strings.NewReader(body))
			r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", client%250+1)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", cfg.PublicURL)
			r.Header.Set("Idempotency-Key", "contract-security")
			r.Header.Set("If-Match", `"1"`)
			for name, values := range header {
				r.Header[name] = values
			}
			for _, c := range cookies {
				r.AddCookie(&http.Cookie{Name: "tendo_session", Value: c})
			}
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, r)
			return w
		}
		send := func(cookies ...string) *httptest.ResponseRecorder { return sendWith("{}", nil, cookies...) }
		code := func(w *httptest.ResponseRecorder) string {
			var body struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			return body.Code
		}
		t.Run(key, func(t *testing.T) {
			switch op.scheme {
			case "SessionCookie":
				token := strings.Repeat("A", 43)
				for name, w := range map[string]*httptest.ResponseRecorder{"missing cookie": send(), "duplicated cookie": send(token, token)} {
					if w.Code != http.StatusUnauthorized || code(w) != "unauthenticated" || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("%s: status=%d body=%s headers=%v", name, w.Code, w.Body, w.Header())
					}
				}
				// Handlers also fail closed with 401 when no principal is present,
				// so a 401 alone cannot prove the session middleware is wired. A
				// well-formed cookie must reach the (unreachable) session store and
				// fail with 503; a handler without the middleware would answer 401.
				if w := send(token); w.Code != http.StatusServiceUnavailable || code(w) != "unavailable" {
					t.Fatalf("well-formed cookie did not reach session authentication: status=%d body=%s", w.Code, w.Body)
				}
			case "SetupToken":
				for name, header := range map[string]http.Header{
					"missing token":    nil,
					"wrong token":      {"X-Tendo-Setup-Token": {strings.Repeat("B", 43) + "="}},
					"duplicated token": {"X-Tendo-Setup-Token": {cfg.SetupToken, cfg.SetupToken}},
				} {
					if w := sendWith(validSetup, header); w.Code != http.StatusUnauthorized || code(w) != "unauthorized" {
						t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body)
					}
				}
				// The configured token must pass authentication and reach the
				// service, which fails on the unreachable database.
				if w := sendWith(validSetup, http.Header{"X-Tendo-Setup-Token": {cfg.SetupToken}}); w.Code != http.StatusServiceUnavailable {
					t.Fatalf("correct setup token: status=%d body=%s", w.Code, w.Body)
				}
			default:
				// Public operations get a valid request and must reach their
				// handler's own outcome without any credential.
				want, ok := publicOutcomes[key]
				if !ok {
					t.Fatalf("no expected outcome for public operation; add it to publicOutcomes")
				}
				if w := sendWith(want.body, nil); w.Code != want.status || code(w) != want.code {
					t.Fatalf("anonymous request: status=%d body=%s, want %d %q", w.Code, w.Body, want.status, want.code)
				}
			}
		})
	}
	for key := range registered {
		if !contract[key] {
			t.Errorf("%s: routed but not declared in the contract", key)
		}
	}
}
