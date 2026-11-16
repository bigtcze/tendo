package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

// IDs are opaque to the transport layer; the service owns validation. They are
// deliberately realistic so a route mix-up would show up as a wrong
// path-parameter assertion.
const (
	adminHouseholdID  = "6f0b6f1e-0f9a-4b1e-9e0f-1f2e3d4c5b6a"
	adminTargetUserID = "8a7b6c5d-4e3f-4a1b-8c8d-7e6f5a4b3c2d"
	adminAccountPath  = "/api/v1/admin/households/" + adminHouseholdID + "/accounts/" + adminTargetUserID
	adminDeletionPath = "/api/v1/admin/account-deletions"
)

type instanceAdminFake struct {
	gateErr error
	err     error
	next    string
	created bool

	households []household.AdminHousehold
	directory  []household.DirectoryAccount
	accounts   []household.AdminAccount
	account    household.AdminAccount
	household  household.AdminHousehold
	version    int64

	calls []string

	lastActor     string
	lastHousehold string
	lastTarget    string
	lastSearch    string
	lastCursor    string
	lastRole      string
	lastKey       string
	lastName      string
	lastTimezone  string
	lastConfirm   string
	lastLimit     int
	lastExpected  int64
}

func (f *instanceAdminFake) record(method string) { f.calls = append(f.calls, method) }

func (f *instanceAdminFake) RequireInstanceAdmin(_ context.Context, actor string) error {
	f.record("RequireInstanceAdmin")
	f.lastActor = actor
	return f.gateErr
}

func (f *instanceAdminFake) ListHouseholds(_ context.Context, _ string, cursor string, limit int) ([]household.AdminHousehold, string, error) {
	f.record("ListHouseholds")
	f.lastCursor, f.lastLimit = cursor, limit
	return f.households, f.next, f.err
}

func (f *instanceAdminFake) CreateHousehold(_ context.Context, _ string, key, name, timezone, adminUserID string) (household.AdminHousehold, int64, error) {
	f.record("CreateHousehold")
	f.lastKey, f.lastName, f.lastTimezone, f.lastTarget = key, name, timezone, adminUserID
	return f.household, f.version, f.err
}

func (f *instanceAdminFake) GetHousehold(_ context.Context, _ string, householdID string) (household.AdminHousehold, int64, error) {
	f.record("GetHousehold")
	f.lastHousehold = householdID
	return f.household, f.version, f.err
}

func (f *instanceAdminFake) ListDirectory(_ context.Context, _ string, search, cursor string, limit int) ([]household.DirectoryAccount, string, error) {
	f.record("ListDirectory")
	f.lastSearch, f.lastCursor, f.lastLimit = search, cursor, limit
	return f.directory, f.next, f.err
}

func (f *instanceAdminFake) ListAccounts(_ context.Context, _ string, householdID, cursor string, limit int) ([]household.AdminAccount, string, error) {
	f.record("ListAccounts")
	f.lastHousehold, f.lastCursor, f.lastLimit = householdID, cursor, limit
	return f.accounts, f.next, f.err
}

func (f *instanceAdminFake) GetAccount(_ context.Context, _ string, householdID, targetUserID string) (household.AdminAccount, error) {
	f.record("GetAccount")
	f.lastHousehold, f.lastTarget = householdID, targetUserID
	return f.account, f.err
}

func (f *instanceAdminFake) AssignAccount(_ context.Context, _ string, householdID, targetUserID, role string) (household.AdminAccount, bool, error) {
	f.record("AssignAccount")
	f.lastHousehold, f.lastTarget, f.lastRole = householdID, targetUserID, role
	return f.account, f.created, f.err
}

func (f *instanceAdminFake) ChangeRole(_ context.Context, _ string, householdID, targetUserID, role string, expected int64) (household.AdminAccount, error) {
	f.record("ChangeRole")
	f.lastHousehold, f.lastTarget, f.lastRole, f.lastExpected = householdID, targetUserID, role, expected
	return f.account, f.err
}

func (f *instanceAdminFake) RemoveAccount(_ context.Context, _ string, householdID, targetUserID string, expected int64) error {
	f.record("RemoveAccount")
	f.lastHousehold, f.lastTarget, f.lastExpected = householdID, targetUserID, expected
	return f.err
}

func (f *instanceAdminFake) DeleteAccount(_ context.Context, actorID, targetUserID, confirmLogin string) error {
	f.record("DeleteAccount")
	f.lastActor, f.lastTarget, f.lastConfirm = actorID, targetUserID, confirmLogin
	return f.err
}

func adminRouter(service instanceAdminService, userID string, present bool) *chi.Mux {
	r := chi.NewRouter()
	NewAdmin(service, func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) { return userID, present }).Register(r)
	return r
}

// adminRouterRejectingAuth mirrors the production session middleware: it
// answers 401 and never reaches the next handler.
func adminRouterRejectingAuth(service instanceAdminService) *chi.Mux {
	r := chi.NewRouter()
	reject := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httpx.ProblemResponse(w, http.StatusUnauthorized, "unauthenticated")
		})
	}
	NewAdmin(service, reject, func(context.Context) (string, bool) { return "", false }).Register(r)
	return r
}

type adminRequest struct {
	name    string
	method  string
	target  string
	body    string
	headers map[string]string
}

// adminRequests returns one well-formed request per route.
func adminRequests() []adminRequest {
	return []adminRequest{
		{name: "list households", method: http.MethodGet, target: "/api/v1/admin/households"},
		{name: "create household", method: http.MethodPost, target: "/api/v1/admin/households", body: `{"name":"Home","timezone":"Europe/Prague","adminUserId":"` + adminTargetUserID + `"}`, headers: map[string]string{"Idempotency-Key": "admin-create"}},
		{name: "get household", method: http.MethodGet, target: "/api/v1/admin/households/" + adminHouseholdID},
		{name: "list directory", method: http.MethodGet, target: "/api/v1/admin/accounts"},
		{name: "list accounts", method: http.MethodGet, target: "/api/v1/admin/households/" + adminHouseholdID + "/accounts"},
		{name: "get account", method: http.MethodGet, target: adminAccountPath},
		{name: "assign account", method: http.MethodPut, target: adminAccountPath, body: `{"role":"admin"}`},
		{name: "change role", method: http.MethodPatch, target: adminAccountPath, body: `{"role":"member"}`, headers: map[string]string{"If-Match": `"2"`}},
		{name: "remove account", method: http.MethodDelete, target: adminAccountPath, headers: map[string]string{"If-Match": `"2"`}},
		{name: "delete account", method: http.MethodPost, target: adminDeletionPath, body: `{"userId":"` + adminTargetUserID + `","confirmLogin":"bob"}`},
	}
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control=%q want=no-store body=%s", got, w.Body)
	}
}

func requireProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	requireStatus(t, w, status)
	if got := problemCode(t, w); got != code {
		t.Fatalf("code=%q want=%q body=%s", got, code, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content-type=%q want=application/problem+json", got)
	}
	var problem map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("problem body: %v %s", err, w.Body)
	}
	return problem
}

func jsonKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("object body: %v %s", err, data)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func listItemKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Items) != 1 {
		t.Fatalf("list body: %v %s", err, data)
	}
	return jsonKeys(t, envelope.Items[0])
}

func requireNullNextCursor(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("list body: %v %s", err, w.Body)
	}
	raw, present := body["nextCursor"]
	if !present || string(raw) != "null" {
		t.Fatalf("nextCursor=%s present=%t body=%s", raw, present, w.Body)
	}
}

func requireCalls(t *testing.T, service *instanceAdminFake, want ...string) {
	t.Helper()
	if !slices.Equal(service.calls, want) {
		t.Fatalf("service calls=%v want=%v", service.calls, want)
	}
}

func TestAdminRoutesRequireSession(t *testing.T) {
	for _, tc := range adminRequests() {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{}
			w := serve(t, adminRouterRejectingAuth(service), tc.method, tc.target, tc.body, tc.headers)
			requireProblem(t, w, http.StatusUnauthorized, "unauthenticated")
			requireCalls(t, service)
		})
	}
}

// A request that cleared the session middleware but carries no principal must
// still be rejected before the grant or any validation runs.
func TestAdminRoutesRequirePrincipalContext(t *testing.T) {
	for _, tc := range adminRequests() {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{}
			w := serve(t, adminRouter(service, "", false), tc.method, tc.target, tc.body, tc.headers)
			requireProblem(t, w, http.StatusUnauthorized, "unauthenticated")
			requireCalls(t, service)
		})
	}
}

// The instance-admin grant is evaluated before bodies, If-Match, query
// parameters and path identifiers, so a non-admin learns nothing at all.
func TestAdminRoutesInstanceAdminGatePrecedesValidation(t *testing.T) {
	for _, tc := range []adminRequest{
		{name: "list households bad cursor and limit", method: http.MethodGet, target: "/api/v1/admin/households?cursor=bad&limit=0"},
		{name: "create household invalid key and body", method: http.MethodPost, target: "/api/v1/admin/households", body: `{"name":`, headers: map[string]string{"Idempotency-Key": " "}},
		{name: "get household malformed id", method: http.MethodGet, target: "/api/v1/admin/households/not-a-uuid"},
		{name: "list directory repeated q", method: http.MethodGet, target: "/api/v1/admin/accounts?q=a&q=b"},
		{name: "list accounts malformed id and oversized limit", method: http.MethodGet, target: "/api/v1/admin/households/not-a-uuid/accounts?limit=500"},
		{name: "get account malformed ids", method: http.MethodGet, target: "/api/v1/admin/households/not-a-uuid/accounts/not-a-uuid"},
		{name: "assign invalid json", method: http.MethodPut, target: adminAccountPath, body: `{"role":`},
		{name: "assign invalid role", method: http.MethodPut, target: adminAccountPath, body: `{"role":"wizard"}`},
		{name: "change role without if-match", method: http.MethodPatch, target: adminAccountPath, body: `{"role":"wizard"}`},
		{name: "remove without if-match", method: http.MethodDelete, target: adminAccountPath},
		{name: "delete account invalid body", method: http.MethodPost, target: adminDeletionPath, body: `{"userId":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{gateErr: household.ErrInstanceAdminRequired}
			w := serve(t, adminRouter(service, "instance-admin", true), tc.method, tc.target, tc.body, tc.headers)
			problem := requireProblem(t, w, http.StatusForbidden, "instance_admin_required")
			if problem["status"] != float64(http.StatusForbidden) {
				t.Fatalf("status field=%v body=%s", problem["status"], w.Body)
			}
			requireCalls(t, service, "RequireInstanceAdmin")
			if service.lastActor != "instance-admin" {
				t.Fatalf("gate actor=%q", service.lastActor)
			}
		})
	}
}

func TestAdminErrorMappingContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		header map[string]string
		err    error
		status int
		code   string
		field  string
	}{
		{name: "not found", method: http.MethodGet, target: adminAccountPath, err: household.ErrNotFound, status: 404, code: "not_found"},
		{name: "precondition failed", method: http.MethodGet, target: adminAccountPath, err: household.ErrPreconditionFailed, status: 412, code: "precondition_failed"},
		{name: "last admin", method: http.MethodGet, target: adminAccountPath, err: household.ErrLastAdmin, status: 409, code: "last_admin"},
		{name: "already member", method: http.MethodPut, target: adminAccountPath, body: `{"role":"member"}`, err: household.ErrAlreadyMember, status: 409, code: "already_member"},
		{name: "household requires admin", method: http.MethodPut, target: adminAccountPath, body: `{"role":"member"}`, err: household.ErrHouseholdRequiresAdmin, status: 409, code: "household_requires_admin"},
		{name: "grant revoked inside service", method: http.MethodGet, target: adminAccountPath, err: household.ErrInstanceAdminRequired, status: 403, code: "instance_admin_required"},
		{name: "cursor validation", method: http.MethodGet, target: "/api/v1/admin/households?cursor=bad", err: &household.ValidationError{Field: "cursor", Code: "invalid_format"}, status: 400, code: "invalid_query"},
		{name: "q validation", method: http.MethodGet, target: "/api/v1/admin/accounts?q=bad", err: &household.ValidationError{Field: "q", Code: "invalid_value"}, status: 400, code: "invalid_query"},
		{name: "role validation", method: http.MethodPatch, target: adminAccountPath, body: `{"role":"wizard"}`, header: map[string]string{"If-Match": `"1"`}, err: &household.ValidationError{Field: "role", Code: "invalid_value"}, status: 422, code: "invalid_value"},
		{name: "delete self", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: household.ErrCannotDeleteSelf, status: 409, code: "cannot_delete_self"},
		{name: "delete last instance admin", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: household.ErrLastInstanceAdmin, status: 409, code: "last_instance_admin"},
		{name: "delete last household admin", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: household.ErrLastAdmin, status: 409, code: "last_admin"},
		{name: "delete contended", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: household.ErrAccountDeletionContended, status: 503, code: "unavailable"},
		{name: "delete unknown account", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: household.ErrNotFound, status: 404, code: "not_found"},
		{name: "delete target reference", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: &household.ValidationError{Field: "userId", Code: "invalid_reference"}, status: 422, code: "invalid_reference", field: "userId"},
		{name: "delete confirmation", method: http.MethodPost, target: adminDeletionPath, body: deletionBody("bob"), err: &household.ValidationError{Field: "confirmLogin", Code: "mismatch"}, status: 422, code: "mismatch", field: "confirmLogin"},
		{name: "unknown failure", method: http.MethodGet, target: adminAccountPath, err: errors.New("identity lookup unavailable"), status: 503, code: "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{err: tc.err}
			w := serve(t, adminRouter(service, "instance-admin", true), tc.method, tc.target, tc.body, tc.header)
			problem := requireProblem(t, w, tc.status, tc.code)
			if tc.code == "invalid_query" && problem["parameter"] == nil {
				t.Fatalf("missing parameter body=%s", w.Body)
			}
			if tc.code == "invalid_value" && (problem["field"] != "role" || problem["title"] != "Validation Failed") {
				t.Fatalf("validation body=%s", w.Body)
			}
			if tc.field != "" && (problem["field"] != tc.field || problem["title"] != "Validation Failed") {
				t.Fatalf("validation body=%s", w.Body)
			}
			if len(service.calls) == 0 || service.calls[len(service.calls)-1] == "RequireInstanceAdmin" {
				t.Fatalf("service method was not exercised body=%s", w.Body)
			}
		})
	}
}

func TestAdminAssignAccountStatusBodyAndETag(t *testing.T) {
	service := &instanceAdminFake{
		account: household.AdminAccount{UserID: adminTargetUserID, Login: "bob", Role: household.RoleAdmin, Version: 4, SignInMethods: household.SignInMethods{Password: true}},
		created: true,
	}
	r := adminRouter(service, "instance-admin", true)
	w := serve(t, r, http.MethodPut, adminAccountPath, `{"role":"admin"}`, nil)
	requireStatus(t, w, http.StatusCreated)
	if got := w.Header().Get("ETag"); got != `"4"` {
		t.Fatalf("etag=%q body=%s", got, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type=%q", got)
	}
	want := apimodel.AdminAccount{
		UserId: adminTargetUserID, Login: "bob", Role: apimodel.MemberRoleAdmin,
		SignInMethods: apimodel.AccountSignInMethods{Password: true},
	}
	var body apimodel.AdminAccount
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"login", "role", "signInMethods", "userId"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
	var envelope struct {
		SignInMethods map[string]any `json:"signInMethods"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	methodKeys := make([]string, 0, len(envelope.SignInMethods))
	for key := range envelope.SignInMethods {
		methodKeys = append(methodKeys, key)
	}
	slices.Sort(methodKeys)
	if !slices.Equal(methodKeys, []string{"oidc", "password"}) {
		t.Fatalf("signInMethods keys=%v body=%s", methodKeys, w.Body)
	}
	if service.lastHousehold != adminHouseholdID || service.lastTarget != adminTargetUserID || service.lastRole != "admin" {
		t.Fatalf("service args household=%q target=%q role=%q", service.lastHousehold, service.lastTarget, service.lastRole)
	}

	// created=false is an idempotent replay of the same role.
	service.created = false
	w = serve(t, r, http.MethodPut, adminAccountPath, `{"role":"admin"}`, nil)
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("ETag"); got != `"4"` {
		t.Fatalf("replay etag=%q body=%s", got, w.Body)
	}
}

func TestAdminGetAccountBodyShape(t *testing.T) {
	service := &instanceAdminFake{account: household.AdminAccount{
		UserID: adminTargetUserID, Login: "bob", Role: household.RoleMember, Version: 7,
		SignInMethods: household.SignInMethods{OIDC: true},
	}}
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodGet, adminAccountPath, "", nil)
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("ETag"); got != `"7"` {
		t.Fatalf("etag=%q body=%s", got, w.Body)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"login", "role", "signInMethods", "userId"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
	if service.lastHousehold != adminHouseholdID || service.lastTarget != adminTargetUserID {
		t.Fatalf("service args household=%q target=%q", service.lastHousehold, service.lastTarget)
	}
	want := `{"userId":"` + adminTargetUserID + `","login":"bob","role":"member","signInMethods":{"password":false,"oidc":true}}`
	var got, expected map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !equalJSON(got, expected) {
		t.Fatalf("body=%s want=%s", w.Body, want)
	}
}

func TestAdminChangeRoleIfMatchAndSuccess(t *testing.T) {
	service := &instanceAdminFake{account: household.AdminAccount{
		UserID: adminTargetUserID, Login: "bob", Role: household.RoleMember, Version: 3,
	}}
	r := adminRouter(service, "instance-admin", true)

	w := serve(t, r, http.MethodPatch, adminAccountPath, `{"role":"member"}`, nil)
	requireProblem(t, w, http.StatusPreconditionRequired, "precondition_required")
	requireCalls(t, service, "RequireInstanceAdmin")

	w = serve(t, r, http.MethodPatch, adminAccountPath, `{"role":"member"}`, map[string]string{"If-Match": `"2"`})
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("ETag"); got != `"3"` {
		t.Fatalf("etag=%q body=%s", got, w.Body)
	}
	if service.lastRole != "member" || service.lastExpected != 2 {
		t.Fatalf("service role=%q expected=%d", service.lastRole, service.lastExpected)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"login", "role", "signInMethods", "userId"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
}

func TestAdminRemoveAccountNoContent(t *testing.T) {
	service := &instanceAdminFake{}
	r := adminRouter(service, "instance-admin", true)

	w := serve(t, r, http.MethodDelete, adminAccountPath, "", nil)
	requireProblem(t, w, http.StatusPreconditionRequired, "precondition_required")
	requireCalls(t, service, "RequireInstanceAdmin")

	w = serve(t, r, http.MethodDelete, adminAccountPath, "", map[string]string{"If-Match": `"5"`})
	requireStatus(t, w, http.StatusNoContent)
	if w.Body.Len() != 0 {
		t.Fatalf("body=%q", w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != "" {
		t.Fatalf("unexpected etag=%q", got)
	}
	if service.lastHousehold != adminHouseholdID || service.lastTarget != adminTargetUserID || service.lastExpected != 5 {
		t.Fatalf("service args household=%q target=%q expected=%d", service.lastHousehold, service.lastTarget, service.lastExpected)
	}
}

func TestAdminListHouseholdsAdminShape(t *testing.T) {
	created := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	service := &instanceAdminFake{households: []household.AdminHousehold{{
		ID: adminHouseholdID, Name: "Home", Timezone: "Europe/Berlin",
		CreatedAt: created.In(time.FixedZone("UTC+2", 2*60*60)),
	}}}
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodGet, "/api/v1/admin/households", "", nil)
	requireStatus(t, w, http.StatusOK)
	want := apimodel.AdminHouseholdList{Items: []apimodel.AdminHousehold{{
		Id: adminHouseholdID, Name: "Home", Timezone: "Europe/Berlin", CreatedAt: created,
	}}}
	var body apimodel.AdminHouseholdList
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	if !body.Items[0].CreatedAt.Equal(created) || body.Items[0].CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt=%s", body.Items[0].CreatedAt)
	}
	if keys := listItemKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"createdAt", "id", "name", "timezone"}) {
		t.Fatalf("item keys=%v body=%s", keys, w.Body)
	}
	requireNullNextCursor(t, w)
	if service.lastLimit != 50 || service.lastCursor != "" {
		t.Fatalf("service limit=%d cursor=%q", service.lastLimit, service.lastCursor)
	}
}

func TestAdminListAccountsAdminShape(t *testing.T) {
	next := "bjF1c2Vy"
	service := &instanceAdminFake{
		accounts: []household.AdminAccount{
			{UserID: "u1", Login: "alice", Role: household.RoleAdmin, Version: 1, SignInMethods: household.SignInMethods{Password: true}},
			{UserID: "u2", Login: "bob", Role: household.RoleMember, Version: 2, SignInMethods: household.SignInMethods{Password: true, OIDC: true}},
		},
		next: next,
	}
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodGet, "/api/v1/admin/households/"+adminHouseholdID+"/accounts?limit=7", "", nil)
	requireStatus(t, w, http.StatusOK)
	var body apimodel.AdminAccountList
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := apimodel.AdminAccountList{Items: []apimodel.AdminAccount{
		{UserId: "u1", Login: "alice", Role: apimodel.MemberRoleAdmin, SignInMethods: apimodel.AccountSignInMethods{Password: true}},
		{UserId: "u2", Login: "bob", Role: apimodel.MemberRoleMember, SignInMethods: apimodel.AccountSignInMethods{Password: true, Oidc: true}},
	}, NextCursor: &next}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	if body.NextCursor == nil || *body.NextCursor != next {
		t.Fatalf("nextCursor=%v body=%s", body.NextCursor, w.Body)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"items", "nextCursor"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
	if service.lastHousehold != adminHouseholdID || service.lastLimit != 7 {
		t.Fatalf("service household=%q limit=%d", service.lastHousehold, service.lastLimit)
	}
}

func TestAdminListDirectorySearchAndPaging(t *testing.T) {
	service := &instanceAdminFake{directory: []household.DirectoryAccount{{UserID: "u1", Login: "alice"}}}
	r := adminRouter(service, "instance-admin", true)

	w := serve(t, r, http.MethodGet, "/api/v1/admin/accounts", "", nil)
	requireStatus(t, w, http.StatusOK)
	var body apimodel.DirectoryAccountList
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := apimodel.DirectoryAccountList{Items: []apimodel.DirectoryAccount{{UserId: "u1", Login: "alice"}}}
	if !equalJSON(body, want) {
		t.Fatalf("body=%s", w.Body)
	}
	if keys := listItemKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"login", "userId"}) {
		t.Fatalf("item keys=%v body=%s", keys, w.Body)
	}
	requireNullNextCursor(t, w)
	if service.lastSearch != "" || service.lastLimit != 50 {
		t.Fatalf("service search=%q limit=%d", service.lastSearch, service.lastLimit)
	}

	// The search term is passed through unchanged.
	w = serve(t, r, http.MethodGet, "/api/v1/admin/accounts?q=ali&limit=10", "", nil)
	requireStatus(t, w, http.StatusOK)
	if service.lastSearch != "ali" || service.lastLimit != 10 {
		t.Fatalf("service search=%q limit=%d", service.lastSearch, service.lastLimit)
	}

	// A repeated q is ambiguous and rejected by the transport.
	service.calls = nil
	w = serve(t, r, http.MethodGet, "/api/v1/admin/accounts?q=a&q=b", "", nil)
	problem := requireProblem(t, w, http.StatusBadRequest, "invalid_query")
	if problem["parameter"] != "q" {
		t.Fatalf("parameter=%v body=%s", problem["parameter"], w.Body)
	}
	requireCalls(t, service, "RequireInstanceAdmin")

	// An oversized limit is a query problem, not a service call.
	service.calls = nil
	w = serve(t, r, http.MethodGet, "/api/v1/admin/accounts?limit=101", "", nil)
	problem = requireProblem(t, w, http.StatusBadRequest, "invalid_query")
	if problem["parameter"] != "limit" {
		t.Fatalf("parameter=%v body=%s", problem["parameter"], w.Body)
	}
	requireCalls(t, service, "RequireInstanceAdmin")
}

func TestAdminCreateHouseholdStatusLocationAndETag(t *testing.T) {
	created := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	service := &instanceAdminFake{
		household: household.AdminHousehold{ID: adminHouseholdID, Name: "Veselí", Timezone: "Europe/Prague", CreatedAt: created.In(time.FixedZone("UTC+2", 2*60*60))},
		version:   1,
	}
	body := `{"name":"  Veselí  ","timezone":"Europe/Prague","adminUserId":"` + adminTargetUserID + `"}`
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", body,
		map[string]string{"Idempotency-Key": "create-key"})
	requireStatus(t, w, http.StatusCreated)
	if got := w.Header().Get("Location"); got != "/api/v1/admin/households/"+adminHouseholdID {
		t.Fatalf("location=%q body=%s", got, w.Body)
	}
	if got := w.Header().Get("ETag"); got != `"1"` {
		t.Fatalf("etag=%q body=%s", got, w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type=%q", got)
	}
	var got apimodel.AdminHousehold
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := apimodel.AdminHousehold{Id: adminHouseholdID, Name: "Veselí", Timezone: "Europe/Prague", CreatedAt: created}
	if !equalJSON(got, want) {
		t.Fatalf("body=%s", w.Body)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt=%s", got.CreatedAt)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"createdAt", "id", "name", "timezone"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
	if service.lastKey != "create-key" || service.lastName != "  Veselí  " || service.lastTimezone != "Europe/Prague" || service.lastTarget != adminTargetUserID {
		t.Fatalf("service key=%q name=%q timezone=%q admin=%q", service.lastKey, service.lastName, service.lastTimezone, service.lastTarget)
	}
	requireCalls(t, service, "RequireInstanceAdmin", "CreateHousehold")
}

func TestAdminCreateHouseholdIdempotencyKeyErrors(t *testing.T) {
	body := `{"name":"Home","timezone":"Europe/Prague","adminUserId":"` + adminTargetUserID + `"}`
	t.Run("missing", func(t *testing.T) {
		service := &instanceAdminFake{}
		w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", body, nil)
		requireProblem(t, w, http.StatusBadRequest, "idempotency_key_required")
		requireCalls(t, service, "RequireInstanceAdmin")
	})
	t.Run("repeated", func(t *testing.T) {
		service := &instanceAdminFake{}
		r := adminRouter(service, "instance-admin", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/households", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Add("Idempotency-Key", "one")
		req.Header.Add("Idempotency-Key", "two")
		r.ServeHTTP(w, req)
		requireProblem(t, w, http.StatusBadRequest, "invalid_idempotency_key")
		requireCalls(t, service, "RequireInstanceAdmin")
	})
	for _, key := range []string{" ", "", strings.Repeat("a", 129), "café"} {
		t.Run("invalid "+key, func(t *testing.T) {
			service := &instanceAdminFake{}
			w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", body,
				map[string]string{"Idempotency-Key": key})
			requireProblem(t, w, http.StatusBadRequest, "invalid_idempotency_key")
			requireCalls(t, service, "RequireInstanceAdmin")
		})
	}
	// The key is evaluated before the body: malformed JSON with no key is a key
	// error, and with a valid key it is a body error.
	service := &instanceAdminFake{}
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", `{"name":`,
		map[string]string{"Idempotency-Key": "valid-key"})
	requireProblem(t, w, http.StatusBadRequest, "invalid_request")
	requireCalls(t, service, "RequireInstanceAdmin")
}

func TestAdminCreateHouseholdRequiredBodyFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing admin user", body: `{"name":"Home","timezone":"UTC"}`},
		{name: "null admin user", body: `{"name":"Home","timezone":"UTC","adminUserId":null}`},
		{name: "missing name", body: `{"timezone":"UTC","adminUserId":"` + adminTargetUserID + `"}`},
		{name: "null timezone", body: `{"name":"Home","timezone":null,"adminUserId":"` + adminTargetUserID + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{}
			w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", tc.body,
				map[string]string{"Idempotency-Key": "required-fields"})
			requireProblem(t, w, http.StatusBadRequest, "invalid_request")
			requireCalls(t, service, "RequireInstanceAdmin")
		})
	}
}

func TestAdminGetHouseholdBodyShape(t *testing.T) {
	created := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	service := &instanceAdminFake{household: household.AdminHousehold{ID: adminHouseholdID, Name: "Home", Timezone: "UTC", CreatedAt: created}, version: 3}
	w := serve(t, adminRouter(service, "instance-admin", true), http.MethodGet, "/api/v1/admin/households/"+adminHouseholdID, "", nil)
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("ETag"); got != `"3"` {
		t.Fatalf("etag=%q body=%s", got, w.Body)
	}
	if keys := jsonKeys(t, w.Body.Bytes()); !slices.Equal(keys, []string{"createdAt", "id", "name", "timezone"}) {
		t.Fatalf("keys=%v body=%s", keys, w.Body)
	}
	if service.lastHousehold != adminHouseholdID {
		t.Fatalf("service household=%q", service.lastHousehold)
	}
	requireCalls(t, service, "RequireInstanceAdmin", "GetHousehold")
}

func TestAdminCreateHouseholdErrorMapping(t *testing.T) {
	body := `{"name":"Home","timezone":"UTC","adminUserId":"` + adminTargetUserID + `"}`
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
		field  string
	}{
		{name: "name", err: &household.ValidationError{Field: "name", Code: "invalid_length"}, status: 422, code: "invalid_length", field: "name"},
		{name: "timezone", err: &household.ValidationError{Field: "timezone", Code: "invalid_iana_timezone"}, status: 422, code: "invalid_iana_timezone", field: "timezone"},
		{name: "admin user", err: &household.ValidationError{Field: "adminUserId", Code: "invalid_reference"}, status: 422, code: "invalid_reference", field: "adminUserId"},
		{name: "key reused", err: household.ErrIdempotencyKeyReused, status: 422, code: "idempotency_key_reused"},
		{name: "raced escape", err: household.ErrCreationKeyRaced, status: 503, code: "unavailable"},
		{name: "unknown", err: errors.New("account lookup unavailable"), status: 503, code: "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{err: tc.err}
			w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, "/api/v1/admin/households", body,
				map[string]string{"Idempotency-Key": "mapping-key"})
			problem := requireProblem(t, w, tc.status, tc.code)
			if tc.field == "" {
				if _, present := problem["field"]; present {
					t.Fatalf("unexpected field body=%s", w.Body)
				}
			} else if problem["field"] != tc.field || problem["title"] != "Validation Failed" {
				t.Fatalf("validation body=%s", w.Body)
			}
		})
	}
}

func deletionBody(confirmLogin string) string {
	return `{"userId":"` + adminTargetUserID + `","confirmLogin":"` + confirmLogin + `"}`
}

// TestAdminDeleteAccountNoContent pins the 204 shape and that the transport
// hands both values through untouched: the service owns identifier shape and
// the exact login comparison.
func TestAdminDeleteAccountNoContent(t *testing.T) {
	service := &instanceAdminFake{}
	r := adminRouter(service, "instance-admin", true)

	w := serve(t, r, http.MethodPost, adminDeletionPath, deletionBody("bob"), nil)
	requireStatus(t, w, http.StatusNoContent)
	if w.Body.Len() != 0 {
		t.Fatalf("body=%q", w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != "" {
		t.Fatalf("unexpected etag=%q", got)
	}
	requireCalls(t, service, "RequireInstanceAdmin", "DeleteAccount")
	if service.lastActor != "instance-admin" || service.lastTarget != adminTargetUserID || service.lastConfirm != "bob" {
		t.Fatalf("service actor=%q target=%q confirm=%q", service.lastActor, service.lastTarget, service.lastConfirm)
	}

	// No case folding, trimming or parsing on the way to the service.
	service.calls = nil
	verbatim := `{"userId":"` + strings.ToUpper(adminTargetUserID) + `","confirmLogin":" Bob "}`
	w = serve(t, r, http.MethodPost, adminDeletionPath, verbatim, nil)
	requireStatus(t, w, http.StatusNoContent)
	if service.lastTarget != strings.ToUpper(adminTargetUserID) || service.lastConfirm != " Bob " {
		t.Fatalf("service target=%q confirm=%q", service.lastTarget, service.lastConfirm)
	}
}

// TestAdminDeleteAccountRejectsBodies pins the strict body rules: every
// malformed shape is a 400 before the service runs.
func TestAdminDeleteAccountRejectsBodies(t *testing.T) {
	valid := deletionBody("bob")
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"userId":`},
		{name: "not an object", body: `["bob"]`},
		{name: "empty object", body: `{}`},
		{name: "unknown key", body: `{"userId":"` + adminTargetUserID + `","confirmLogin":"bob","force":true}`},
		{name: "duplicate key", body: `{"userId":"` + adminTargetUserID + `","userId":"` + adminTargetUserID + `","confirmLogin":"bob"}`},
		{name: "missing user id", body: `{"confirmLogin":"bob"}`},
		{name: "missing confirmation", body: `{"userId":"` + adminTargetUserID + `"}`},
		{name: "null user id", body: `{"userId":null,"confirmLogin":"bob"}`},
		{name: "null confirmation", body: `{"userId":"` + adminTargetUserID + `","confirmLogin":null}`},
		{name: "numeric confirmation", body: `{"userId":"` + adminTargetUserID + `","confirmLogin":7}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &instanceAdminFake{}
			w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, adminDeletionPath, tc.body, nil)
			requireProblem(t, w, http.StatusBadRequest, "invalid_request")
			requireCalls(t, service, "RequireInstanceAdmin")
		})
	}

	t.Run("unsupported media type", func(t *testing.T) {
		service := &instanceAdminFake{}
		r := adminRouter(service, "instance-admin", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, adminDeletionPath, strings.NewReader(valid))
		req.Header.Set("Content-Type", "text/plain")
		r.ServeHTTP(w, req)
		requireProblem(t, w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		requireCalls(t, service, "RequireInstanceAdmin")
	})

	t.Run("body too large", func(t *testing.T) {
		service := &instanceAdminFake{}
		r := adminRouter(service, "instance-admin", true)
		body := `{"userId":"` + adminTargetUserID + `","confirmLogin":"` + strings.Repeat("a", accountBodyLimit) + `"}`
		w := serve(t, r, http.MethodPost, adminDeletionPath, body, nil)
		requireProblem(t, w, http.StatusRequestEntityTooLarge, "content_too_large")
		requireCalls(t, service, "RequireInstanceAdmin")
	})

	t.Run("grant precedes body", func(t *testing.T) {
		service := &instanceAdminFake{gateErr: household.ErrInstanceAdminRequired}
		w := serve(t, adminRouter(service, "instance-admin", true), http.MethodPost, adminDeletionPath, `{"userId":`, nil)
		requireProblem(t, w, http.StatusForbidden, "instance_admin_required")
		requireCalls(t, service, "RequireInstanceAdmin")
	})
}
