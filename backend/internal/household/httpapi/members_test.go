package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/go-chi/chi/v5"
)

type memberPageFake struct {
	items []household.Member
	next  string
	err   error
}

func (f *memberPageFake) ListMembers(_ context.Context, _, _, cursor string, _ int) ([]household.Member, string, error) {
	if cursor != "" {
		return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
	}
	return f.items, f.next, f.err
}
func TestListMembersHTTPPaginationAndErrors(t *testing.T) {
	next := "m1Y3Vyc29y"
	svc := &memberPageFake{items: []household.Member{{UserID: "u1", Login: "owner", Role: household.RoleOwner}, {UserID: "u2", Login: "member", Role: household.RoleMember}}, next: next}
	r := chi.NewRouter()
	auth := func(n http.Handler) http.Handler { return n }
	NewMembers(svc, auth, func(context.Context) (string, bool) { return "u1", true }).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/households/h/members?limit=2", nil))
	var body MemberList
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	expected := MemberList{Items: []Member{{UserId: "u1", Login: "owner", Role: MemberRoleOwner}, {UserId: "u2", Login: "member", Role: MemberRoleMember}}, NextCursor: &next}
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || !equalJSON(body, expected) {
		t.Fatalf("status=%d body=%+v", w.Code, body)
	}
	for _, query := range []string{"cursor=bad", "limit=0"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/households/h/members?"+query, nil))
		var p map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
			t.Fatal(e)
		}
		parameter := strings.SplitN(query, "=", 2)[0]
		if w.Code != 400 || p["code"] != "invalid_query" || p["parameter"] != parameter {
			t.Fatalf("query %s status=%d body=%s", query, w.Code, w.Body.String())
		}
	}
	svc.err = household.ErrNotFound
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/households/h/members", nil))
	if w.Code != 404 {
		t.Fatalf("non-member=%d", w.Code)
	}
	authRouter := chi.NewRouter()
	NewMembers(svc, auth, func(context.Context) (string, bool) { return "", false }).Register(authRouter)
	authW := httptest.NewRecorder()
	authRouter.ServeHTTP(authW, httptest.NewRequest("GET", "/api/v1/households/h/members", nil))
	var authBody map[string]any
	if e := json.Unmarshal(authW.Body.Bytes(), &authBody); e != nil || authW.Code != 401 || authBody["code"] != "unauthenticated" {
		t.Fatalf("auth=%d %s", authW.Code, authW.Body.String())
	}
}
func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func TestListMembersHTTPUnknownRoleFailsClosed(t *testing.T) {
	svc := &memberPageFake{items: []household.Member{{UserID: "u", Login: "x", Role: "admin"}}}
	r := chi.NewRouter()
	NewMembers(svc, func(n http.Handler) http.Handler { return n }, func(context.Context) (string, bool) { return "u", true }).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/households/h/members", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "admin") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
