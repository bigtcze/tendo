package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

type accountsFake struct {
	items    []household.Account
	next     string
	account  household.Account
	err      error
	lastRole string
	lastExp  int64
	calls    int
}

func (f *accountsFake) ListAccounts(_ context.Context, _, _, cursor string, _ int) ([]household.Account, string, error) {
	f.calls++
	if cursor != "" {
		return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
	}
	return f.items, f.next, f.err
}
func (f *accountsFake) GetAccount(context.Context, string, string, string) (household.Account, error) {
	f.calls++
	return f.account, f.err
}
func (f *accountsFake) ChangeRole(_ context.Context, _, _, _, role string, expected int64) (household.Account, error) {
	f.calls++
	f.lastRole, f.lastExp = role, expected
	return f.account, f.err
}

func accountsRouter(service accountService, userID string, ok bool) *chi.Mux {
	r := chi.NewRouter()
	NewAccounts(service, func(n http.Handler) http.Handler { return n }, func(context.Context) (string, bool) { return userID, ok }).Register(r)
	return r
}

func serve(t *testing.T, r http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	r.ServeHTTP(w, req)
	return w
}

func problemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var problem map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("problem body: %v %s", err, w.Body.String())
	}
	code, _ := problem["code"].(string)
	return code
}

func TestListAccountsHTTPContractAndErrors(t *testing.T) {
	next := "a1Y3Vyc29y"
	service := &accountsFake{items: []household.Account{
		{UserID: "u1", Login: "alice", Role: household.RoleAdmin, SignInMethods: household.SignInMethods{Password: true}},
		{UserID: "u2", Login: "bob", Role: household.RoleMember, SignInMethods: household.SignInMethods{OIDC: true}},
	}, next: next}
	r := accountsRouter(service, "u1", true)
	w := serve(t, r, http.MethodGet, "/api/v1/households/h/accounts", "", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	var body apimodel.AccountList
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := apimodel.AccountList{Items: []apimodel.Account{
		{UserId: "u1", Login: "alice", Role: apimodel.AccountRoleAdmin, SignInMethods: apimodel.AccountSignInMethods{Password: true}},
		{UserId: "u2", Login: "bob", Role: apimodel.AccountRoleMember, SignInMethods: apimodel.AccountSignInMethods{Oidc: true}},
	}, NextCursor: &next}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{household.ErrAdminRequired, 403, "admin_required"},
		{household.ErrNotFound, 404, "not_found"},
		{household.ErrPreconditionFailed, 412, "precondition_failed"},
		{household.ErrLastAdmin, 409, "last_admin"},
		{errors.New("identity lookup unavailable"), 503, "unavailable"},
	} {
		service.err = tc.err
		w = serve(t, r, http.MethodGet, "/api/v1/households/h/accounts", "", nil)
		if w.Code != tc.status || problemCode(t, w) != tc.code {
			t.Fatalf("err=%v status=%d code=%s", tc.err, w.Code, problemCode(t, w))
		}
	}
	service.err = nil
	w = serve(t, r, http.MethodGet, "/api/v1/households/h/accounts?cursor=bad", "", nil)
	if w.Code != 400 || problemCode(t, w) != "invalid_query" {
		t.Fatalf("bad cursor status=%d body=%s", w.Code, w.Body)
	}
	w = serve(t, accountsRouter(service, "", false), http.MethodGet, "/api/v1/households/h/accounts", "", nil)
	if w.Code != 401 || problemCode(t, w) != "unauthenticated" {
		t.Fatalf("unauthenticated status=%d body=%s", w.Code, w.Body)
	}
}

func TestGetAccountHTTPETagAndNotFound(t *testing.T) {
	service := &accountsFake{account: household.Account{UserID: "u2", Login: "bob", Role: household.RoleMember, Version: 3, SignInMethods: household.SignInMethods{Password: true}}}
	r := accountsRouter(service, "u1", true)
	w := serve(t, r, http.MethodGet, "/api/v1/households/h/accounts/u2", "", nil)
	if w.Code != 200 || w.Header().Get("ETag") != `"3"` {
		t.Fatalf("status=%d etag=%q body=%s", w.Code, w.Header().Get("ETag"), w.Body)
	}
	var body apimodel.Account
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := apimodel.Account{UserId: "u2", Login: "bob", Role: apimodel.AccountRoleMember, SignInMethods: apimodel.AccountSignInMethods{Password: true}}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	service.err = household.ErrNotFound
	w = serve(t, r, http.MethodGet, "/api/v1/households/h/accounts/u2", "", nil)
	if w.Code != 404 || problemCode(t, w) != "not_found" {
		t.Fatalf("not found status=%d body=%s", w.Code, w.Body)
	}
	service.err = household.ErrAdminRequired
	w = serve(t, r, http.MethodGet, "/api/v1/households/h/accounts/u2", "", nil)
	if w.Code != 403 || problemCode(t, w) != "admin_required" {
		t.Fatalf("member status=%d body=%s", w.Code, w.Body)
	}
}

func TestUpdateAccountRoleHTTPIfMatchStrictBodyAndErrors(t *testing.T) {
	service := &accountsFake{account: household.Account{UserID: "u2", Login: "bob", Role: household.RoleAdmin, Version: 2, SignInMethods: household.SignInMethods{OIDC: true}}}
	r := accountsRouter(service, "u1", true)
	const target = "/api/v1/households/h/accounts/u2"

	if w := serve(t, r, http.MethodPatch, target, `{"role":"admin"}`, nil); w.Code != 428 || problemCode(t, w) != "precondition_required" {
		t.Fatalf("missing if-match status=%d body=%s", w.Code, w.Body)
	}
	for _, value := range []string{`W/"1"`, `"1", "2"`, `"0"`, `abc`, `"1" `} {
		if w := serve(t, r, http.MethodPatch, target, `{"role":"admin"}`, map[string]string{"If-Match": value}); w.Code != 412 || problemCode(t, w) != "precondition_failed" {
			t.Fatalf("if-match %q status=%d body=%s", value, w.Code, w.Body)
		}
	}
	for name, body := range map[string]string{
		"unknown key":  `{"role":"admin","extra":1}`,
		"duplicate":    `{"role":"admin","role":"member"}`,
		"null":         `{"role":null}`,
		"missing role": `{}`,
		"array":        `[]`,
	} {
		if w := serve(t, r, http.MethodPatch, target, body, map[string]string{"If-Match": `"1"`}); w.Code != 400 || problemCode(t, w) != "invalid_request" {
			t.Fatalf("%s status=%d body=%s", name, w.Code, w.Body)
		}
	}
	w := serve(t, r, http.MethodPatch, target, `{"role":"admin"}`, map[string]string{"If-Match": `"1"`})
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` || service.lastRole != "admin" || service.lastExp != 1 {
		t.Fatalf("success status=%d etag=%q role=%q expected=%d body=%s", w.Code, w.Header().Get("ETag"), service.lastRole, service.lastExp, w.Body)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{household.ErrPreconditionFailed, 412, "precondition_failed"},
		{household.ErrLastAdmin, 409, "last_admin"},
		{household.ErrNotFound, 404, "not_found"},
		{household.ErrAdminRequired, 403, "admin_required"},
	} {
		service.err = tc.err
		w = serve(t, r, http.MethodPatch, target, `{"role":"member"}`, map[string]string{"If-Match": `"1"`})
		if w.Code != tc.status || problemCode(t, w) != tc.code {
			t.Fatalf("err=%v status=%d body=%s", tc.err, w.Code, w.Body)
		}
	}
	service.err = &household.ValidationError{Field: "role", Code: "invalid_value"}
	w = serve(t, r, http.MethodPatch, target, `{"role":"admin"}`, map[string]string{"If-Match": `"1"`})
	var problem map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &problem)
	if w.Code != 422 || problem["code"] != "invalid_value" || problem["field"] != "role" {
		t.Fatalf("validation status=%d body=%s", w.Code, w.Body)
	}
	w = serve(t, accountsRouter(service, "", false), http.MethodPatch, target, `{"role":"admin"}`, map[string]string{"If-Match": `"1"`})
	if w.Code != 401 || problemCode(t, w) != "unauthenticated" {
		t.Fatalf("unauthenticated status=%d body=%s", w.Code, w.Body)
	}
}
