package httpapi

import (
	"net/http"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
)

func TestRemoveMemberHTTPContractAndErrors(t *testing.T) {
	service := &accountsFake{}
	r := accountsRouter(service, "u1", true)
	const target = "/api/v1/households/h/accounts/u2"

	if w := serve(t, r, http.MethodDelete, target, "", nil); w.Code != 428 || problemCode(t, w) != "precondition_required" {
		t.Fatalf("missing if-match status=%d body=%s", w.Code, w.Body)
	}
	for _, value := range []string{`W/"1"`, `"1", "2"`, `"0"`, `abc`, `"1" `} {
		if w := serve(t, r, http.MethodDelete, target, "", map[string]string{"If-Match": value}); w.Code != 412 || problemCode(t, w) != "precondition_failed" {
			t.Fatalf("if-match %q status=%d body=%s", value, w.Code, w.Body)
		}
	}
	if service.calls != 0 {
		t.Fatalf("service called before If-Match was accepted: %d", service.calls)
	}

	w := serve(t, r, http.MethodDelete, target, "", map[string]string{"If-Match": `"3"`})
	if w.Code != 204 || w.Header().Get("Cache-Control") != "no-store" || w.Body.Len() != 0 {
		t.Fatalf("success status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	if service.lastRemoved != "u2" || service.lastExp != 3 {
		t.Fatalf("service target=%q version=%d", service.lastRemoved, service.lastExp)
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
		w = serve(t, r, http.MethodDelete, target, "", map[string]string{"If-Match": `"1"`})
		if w.Code != tc.status || problemCode(t, w) != tc.code {
			t.Fatalf("err=%v status=%d body=%s", tc.err, w.Code, w.Body)
		}
	}
	service.err = nil

	if w = serve(t, accountsRouter(service, "", false), http.MethodDelete, target, "", map[string]string{"If-Match": `"1"`}); w.Code != 401 || problemCode(t, w) != "unauthenticated" {
		t.Fatalf("unauthenticated status=%d body=%s", w.Code, w.Body)
	}
}
