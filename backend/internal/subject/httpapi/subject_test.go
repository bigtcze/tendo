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

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/bigtcze/tendo/backend/internal/subject/subjecttest"
	"github.com/go-chi/chi/v5"
)

const (
	callerID    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	householdID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	subjectID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	otherID     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
	secretErr   = "pq: password authentication failed for user tendo at 10.1.2.3"
)

var created = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

// Only the repository and the membership authorizer are fakes; the real
// subject.Service runs behind the handler.
const authorizedUser = callerID

func newRepo() *subjecttest.Repo {
	repo := subjecttest.New()
	repo.Seed(subject.Subject{ID: subjectID, HouseholdID: householdID, Type: subject.TypeVehicle, Name: "Octavia", CreatedAt: created, UpdatedAt: created, Version: 1})
	return repo
}

type userKey struct{}

type fixture struct {
	repo     *subjecttest.Repo
	authErr  error
	denyUser string
	authHits int
	svcHits  int
	handler  http.Handler
}

func newFixture(authenticate bool) *fixture {
	f := &fixture{repo: newRepo()}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.authHits++
			if !authenticate {
				problem(w, 401, "unauthenticated")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, callerID)))
		})
	}
	// Membership: only (caller, householdID) is a member.
	membership := func(_ context.Context, userID, hid string) error {
		f.svcHits++
		if f.authErr != nil {
			return f.authErr
		}
		if userID == f.denyUser || userID != authorizedUser || hid != householdID {
			return subject.ErrNotFound
		}
		return nil
	}
	r := chi.NewRouter()
	New(subject.NewService(f.repo, membership, func(ctx context.Context, userID, hid string) error {
		if userID == callerID && hid == householdID {
			return nil
		}
		return subject.ErrNotFound
	}), auth, func(ctx context.Context) (string, bool) {
		v, ok := ctx.Value(userKey{}).(string)
		return v, ok
	}).Register(r)
	f.handler = r
	return f
}

func (f *fixture) doAs(method, path, body string, headers map[string]string, user string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req = req.WithContext(context.WithValue(req.Context(), userKey{}, user))
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func (f *fixture) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	return f.doAs(method, path, body, headers, "")
}

func jsonEqual(t *testing.T, got, want string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(got), &x); err != nil {
		t.Fatalf("got is not JSON: %q", got)
	}
	if err := json.Unmarshal([]byte(want), &y); err != nil {
		t.Fatal(err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func problemBody(status int, title, code string) string {
	b, _ := json.Marshal(map[string]any{"type": "about:blank", "title": title, "status": status, "code": code})
	return string(b)
}

var (
	base        = "/api/v1/households/" + householdID + "/subjects"
	one         = base + "/" + subjectID
	wantSubject = `{"id":"` + subjectID + `","type":"vehicle","name":"Octavia","linkedUserId":null,"archived":false,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
	ifMatch1    = map[string]string{"If-Match": `"1"`}
)

func (f *fixture) stored(t *testing.T, id string) subject.Subject {
	t.Helper()
	s, ok := f.repo.Row(id)
	if !ok {
		t.Fatalf("row %s missing", id)
	}
	return s
}

func TestCreateReturns201WithLocationETagAndPersistsTrimmedName(t *testing.T) {
	f := newFixture(true)
	w := f.do("POST", base, `{"name":"  Rex 🐕  ","type":"pet"}`, map[string]string{"Idempotency-Key": "creation-test"})
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	id, _ := got["id"].(string)
	if len(got) != 7 || got["linkedUserId"] != nil || got["name"] != "Rex 🐕" || got["type"] != "pet" || got["archived"] != false || id == "" {
		t.Fatalf("body=%v (householdId/version must not leak)", got)
	}
	h := w.Header()
	if h.Get("Location") != base+"/"+id || h.Get("ETag") != `"1"` || h.Get("Cache-Control") != "no-store" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers=%v", h)
	}
	row := f.stored(t, id)
	if row.Name != "Rex 🐕" || row.Type != subject.TypePet || row.HouseholdID != householdID || row.Version != 1 || row.Archived {
		t.Fatalf("stored=%+v", row)
	}
	if f.repo.Count() != 2 {
		t.Fatalf("rows=%d", f.repo.Count())
	}
}

func TestCreateIdempotencyKeyErrorsAndEvaluationOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		code   string
	}{
		{"missing", nil, "idempotency_key_required"}, {"empty", []string{""}, "invalid_idempotency_key"}, {"repeated", []string{"one", "two"}, "invalid_idempotency_key"}, {"oversized", []string{strings.Repeat("a", 129)}, "invalid_idempotency_key"}, {"nonascii", []string{"café"}, "invalid_idempotency_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			req := httptest.NewRequest("POST", base, strings.NewReader("not-json"))
			req.Header.Set("Content-Type", "application/json")
			for _, v := range tc.values {
				req.Header.Add("Idempotency-Key", v)
			}
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, req)
			want := problemBody(400, "Bad Request", tc.code)
			if w.Code != 400 || !jsonEqual(t, w.Body.String(), want) || f.repo.Calls != 0 {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body, f.repo.Calls)
			}
		})
	}
	f := newFixture(false)
	w := f.do("POST", base, "not-json", map[string]string{"Idempotency-Key": "auth-order"})
	if w.Code != 401 || !jsonEqual(t, w.Body.String(), problemBody(401, "Unauthorized", "unauthenticated")) {
		t.Fatalf("auth priority: %d %s", w.Code, w.Body)
	}
	f = newFixture(true)
	req := httptest.NewRequest("POST", base, strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", " ")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != 400 || !jsonEqual(t, w.Body.String(), problemBody(400, "Bad Request", "invalid_idempotency_key")) {
		t.Fatalf("key priority: %d %s", w.Code, w.Body)
	}
}

func TestCreateReplayReturnsCurrentStateAndReuseProblem(t *testing.T) {
	f := newFixture(true)
	key := map[string]string{"Idempotency-Key": "replay-key"}
	first := f.do("POST", base, `{"name":"  Rex  ","type":"pet"}`, key)
	if first.Code != 201 {
		t.Fatalf("first=%d %s", first.Code, first.Body)
	}
	var firstBody Subject
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	row := f.stored(t, firstBody.Id)
	row.Name = "Renamed"
	row.Archived = true
	row.Version = 4
	f.repo.Seed(row)
	replay := f.do("POST", base, `{"type":"pet","name":"Rex"}`, key)
	want := `{"id":"` + row.ID + `","type":"pet","name":"Renamed","linkedUserId":null,"archived":true,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
	if replay.Code != 201 || !jsonEqual(t, replay.Body.String(), want) || replay.Header().Get("ETag") != "\"4\"" || replay.Header().Get("Location") != first.Header().Get("Location") || f.stored(t, row.ID).Version != 4 {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body)
	}
	reused := f.do("POST", base, `{"name":" ","type":"pet"}`, key)
	wantProblem := `{"type":"about:blank","title":"Unprocessable Entity","status":422,"code":"idempotency_key_reused"}`
	if reused.Code != 422 || !jsonEqual(t, reused.Body.String(), wantProblem) || strings.Contains(reused.Body.String(), "field") {
		t.Fatalf("reuse status=%d body=%s", reused.Code, reused.Body)
	}
}

func TestCreateReplayStillRequiresMembership(t *testing.T) {
	f := newFixture(true)
	key := map[string]string{"Idempotency-Key": "membership-replay"}
	created := f.do("POST", base, `{"name":"Member subject","type":"person"}`, key)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body)
	}
	var body Subject
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	rowBefore := f.stored(t, body.Id)
	f.denyUser = callerID
	replay := f.doAs("POST", base, `{"name":"Member subject","type":"person"}`, key, callerID)
	if replay.Code != 404 || !jsonEqual(t, replay.Body.String(), problemBody(404, "Not Found", "not_found")) || f.stored(t, body.Id) != rowBefore {
		t.Fatalf("denied replay status=%d body=%s row=%+v", replay.Code, replay.Body, f.stored(t, body.Id))
	}
}

func TestLocationUsesCanonicalLowercaseIDs(t *testing.T) {
	f := newFixture(true)
	upper := strings.ToUpper(householdID)
	// Uppercase household in the path is the same household for the service.
	w := f.do("POST", "/api/v1/households/"+upper+"/subjects", `{"name":"x","type":"home"}`, map[string]string{"Idempotency-Key": "location-test"})
	// The fake membership compares canonical lowercase ids.
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	loc := w.Header().Get("Location")
	if loc != strings.ToLower(loc) || !strings.HasPrefix(loc, base+"/") || strings.Contains(loc, upper) {
		t.Fatalf("Location=%s", loc)
	}
}

func TestGetReturnsSubjectAndETag(t *testing.T) {
	f := newFixture(true)
	row := f.stored(t, subjectID)
	row.Version = 7
	f.repo.Seed(row)
	w := f.do("GET", one, "", nil)
	if w.Code != 200 || !jsonEqual(t, w.Body.String(), wantSubject) || w.Header().Get("ETag") != `"7"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
}

func TestListShapeAndNextCursorNullVersusString(t *testing.T) {
	f := newFixture(true)
	w := f.do("GET", base, "", nil)
	if w.Code != 200 || !jsonEqual(t, w.Body.String(), `{"items":[`+wantSubject+`],"nextCursor":null}`) || !strings.Contains(w.Body.String(), `"nextCursor":null`) || w.Header().Get("ETag") != "" {
		t.Fatalf("status=%d body=%s headers=%v", w.Code, w.Body, w.Header())
	}
	second := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	f.repo.Seed(subject.Subject{ID: second, HouseholdID: householdID, Type: subject.TypeHome, Name: "Flat", CreatedAt: created, UpdatedAt: created, Version: 1})
	w = f.do("GET", base+"?limit=1", "", nil)
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"nextCursor"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if w.Code != 200 || len(page.Items) != 1 || page.Items[0]["id"] != subjectID || page.NextCursor == nil {
		t.Fatalf("page 1: status=%d body=%s", w.Code, w.Body)
	}
	w = f.do("GET", base+"?limit=1&cursor="+*page.NextCursor, "", nil)
	page.NextCursor = nil
	page.Items = nil
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if w.Code != 200 || len(page.Items) != 1 || page.Items[0]["id"] != second || page.NextCursor != nil || !strings.Contains(w.Body.String(), `"nextCursor":null`) {
		t.Fatalf("page 2: status=%d body=%s", w.Code, w.Body)
	}
	w = f.do("GET", base+"?archived=true", "", nil)
	if !jsonEqual(t, w.Body.String(), `{"items":[],"nextCursor":null}`) {
		t.Fatalf("empty archived page body=%s", w.Body)
	}
}

func TestListArchivedFilterAndUnknownParamsIgnored(t *testing.T) {
	f := newFixture(true)
	row := f.stored(t, subjectID)
	row.Archived = true
	f.repo.Seed(row)
	if w := f.do("GET", base+"?unknown=zzz", "", nil); !jsonEqual(t, w.Body.String(), `{"items":[],"nextCursor":null}`) {
		t.Fatalf("default list must exclude archived: %s", w.Body)
	}
	w := f.do("GET", base+"?archived=true&limit=100", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), subjectID) {
		t.Fatalf("archived list: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", base+"?archived=false", "", nil); strings.Contains(w.Body.String(), subjectID) {
		t.Fatal("archived=false listed an archived subject")
	}
}

func TestInvalidQueryNamesParameter(t *testing.T) {
	f := newFixture(true)
	for _, tc := range []struct{ query, parameter string }{
		{"limit=0", "limit"}, {"limit=101", "limit"}, {"limit=-1", "limit"}, {"limit=abc", "limit"}, {"limit=1.5", "limit"}, {"limit=", "limit"}, {"limit=1&limit=2", "limit"}, {"limit=%2B5", "limit"}, {"limit=99999999999999999999", "limit"},
		{"cursor=", "cursor"}, {"cursor=***", "cursor"}, {"cursor=" + subjectID, "cursor"},
		{"archived=yes", "archived"}, {"archived=1", "archived"}, {"archived=TRUE", "archived"}, {"archived=", "archived"},
	} {
		w := f.do("GET", base+"?"+tc.query, "", nil)
		want := `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_query","parameter":"` + tc.parameter + `"}`
		if w.Code != 400 || w.Header().Get("Content-Type") != "application/problem+json" || !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("%s: status=%d body=%s", tc.query, w.Code, w.Body)
		}
	}
}

func TestListIDsRepeatedAndValidationPrecedence(t *testing.T) {
	f := newFixture(true)
	second := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	f.repo.Seed(subject.Subject{ID: second, HouseholdID: householdID, Type: subject.TypeHome, Name: "Flat", CreatedAt: created, UpdatedAt: created, Version: 1})
	w := f.do("GET", base+"?id="+second, "", nil)
	var subset struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &subset); err != nil || w.Code != 200 || len(subset.Items) != 1 || subset.Items[0]["id"] != second {
		t.Fatalf("strict subset id filter: %d %s err=%v", w.Code, w.Body, err)
	}
	w = f.do("GET", base+"?id="+strings.ToUpper(subjectID)+"&id="+second+"&id="+strings.ToUpper(subjectID), "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), subjectID) || !strings.Contains(w.Body.String(), second) {
		t.Fatalf("repeated id filter: %d %s", w.Code, w.Body)
	}
	duplicateQuery := "?" + strings.Repeat("id="+subjectID+"&", 100) + "id=" + subjectID
	if w = f.do("GET", base+duplicateQuery, "", nil); w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"id"`) {
		t.Fatalf("101 duplicate valid ids: %d %s", w.Code, w.Body)
	}
	for _, value := range []string{"", "malformed"} {
		w = f.do("GET", base+"?id="+value, "", nil)
		want := `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_query","parameter":"id"}`
		if w.Code != 400 || !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("id=%q: %d %s", value, w.Code, w.Body)
		}
	}
	unauth := newFixture(false)
	if w := unauth.do("GET", base+"?id=", "", nil); w.Code != 401 {
		t.Fatalf("unauth invalid id status=%d body=%s", w.Code, w.Body)
	}
	f.denyUser = callerID
	tooManyIDs := "?" + strings.Repeat("id="+subjectID+"&", 100) + "id=" + subjectID
	if w := f.do("GET", base+tooManyIDs, "", nil); w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"id"`) {
		t.Fatalf("101 ids structural check precedes membership: status=%d body=%s", w.Code, w.Body)
	}
	if w := f.do("GET", base+"?id=malformed", "", nil); w.Code != 404 || !jsonEqual(t, w.Body.String(), problemBody(404, "Not Found", "not_found")) {
		t.Fatalf("semantic validation must follow membership: status=%d body=%s", w.Code, w.Body)
	}
	f.denyUser = ""
	if w := f.do("GET", base+"?id=malformed", "", nil); w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"id"`) {
		t.Fatalf("member malformed id: status=%d body=%s", w.Code, w.Body)
	}
	f.denyUser = callerID
	if w := f.do("GET", base+"?id=", "", nil); w.Code != 400 {
		t.Fatalf("empty structural id check precedes membership: status=%d body=%s", w.Code, w.Body)
	}
}

func TestPatchArchiveIncrementsStoredVersion(t *testing.T) {
	f := newFixture(true)
	w := f.do("PATCH", one, `{"name":"  Škoda  ","archived":true,"type":"custom"}`, ifMatch1)
	want := `{"id":"` + subjectID + `","type":"custom","name":"Škoda","linkedUserId":null,"archived":true,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`
	if w.Code != 200 || !jsonEqual(t, w.Body.String(), want) || w.Header().Get("ETag") != `"2"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	row := f.stored(t, subjectID)
	if row.Name != "Škoda" || row.Type != subject.TypeCustom || !row.Archived || row.Version != 2 {
		t.Fatalf("stored=%+v", row)
	}
	// Partial patch keeps untouched fields; unarchive.
	w = f.do("PATCH", one, `{"archived":false}`, map[string]string{"If-Match": `"2"`})
	row = f.stored(t, subjectID)
	if w.Code != 200 || w.Header().Get("ETag") != `"3"` || row.Archived || row.Name != "Škoda" || row.Type != subject.TypeCustom || row.Version != 3 {
		t.Fatalf("status=%d stored=%+v", w.Code, row)
	}
}

func TestPatchPreconditionsLeaveStateUntouched(t *testing.T) {
	f := newFixture(true)
	original := f.stored(t, subjectID)
	w := f.do("PATCH", one, `{"archived":true}`, nil)
	if w.Code != 428 || !jsonEqual(t, w.Body.String(), problemBody(428, "Precondition Required", "precondition_required")) {
		t.Fatalf("missing If-Match: status=%d body=%s", w.Code, w.Body)
	}
	for _, bad := range []string{`1`, `W/"1"`, `"0"`, `"01"`, `"-1"`, `"1", "2"`, `*`, `"abc"`, `""`, ` "1"`, `"1" `, `"99999999999999999999"`} {
		w := f.do("PATCH", one, `{"archived":true}`, map[string]string{"If-Match": bad})
		if w.Code != 412 || !jsonEqual(t, w.Body.String(), problemBody(412, "Precondition Failed", "precondition_failed")) {
			t.Fatalf("If-Match %q: status=%d body=%s", bad, w.Code, w.Body)
		}
	}
	if f.stored(t, subjectID) != original || f.repo.Calls != 0 {
		t.Fatalf("rejected preconditions touched state: %+v calls=%d", f.stored(t, subjectID), f.repo.Calls)
	}
	// Stale version.
	row := original
	row.Version = 5
	f.repo.Seed(row)
	w = f.do("PATCH", one, `{"archived":true,"name":"Changed"}`, map[string]string{"If-Match": `"4"`})
	if w.Code != 412 || !jsonEqual(t, w.Body.String(), problemBody(412, "Precondition Failed", "precondition_failed")) || w.Header().Get("ETag") != "" {
		t.Fatalf("stale: status=%d body=%s", w.Code, w.Body)
	}
	if f.stored(t, subjectID) != row {
		t.Fatalf("stale update changed the stored row: %+v", f.stored(t, subjectID))
	}
	w = f.do("PATCH", one, `{"archived":true}`, map[string]string{"If-Match": `"5"`})
	if got := f.stored(t, subjectID); w.Code != 200 || w.Header().Get("ETag") != `"6"` || got.Version != 6 || !got.Archived {
		t.Fatalf("current: status=%d stored=%+v", w.Code, got)
	}
}

func TestPatchRejectsMalformedBodies(t *testing.T) {
	f := newFixture(true)
	original := f.stored(t, subjectID)
	for name, body := range map[string]string{
		"empty object":    `{}`,
		"unknown key":     `{"name":"a","color":"red"}`,
		"duplicate key":   `{"name":"a","name":"b"}`,
		"null name":       `{"name":null}`,
		"null archived":   `{"archived":null}`,
		"number name":     `{"name":5}`,
		"string archived": `{"archived":"true"}`,
		"number type":     `{"type":1}`,
		"array":           `[]`,
		"trailing data":   `{"name":"a"} {}`,
		"not json":        `nope`,
	} {
		w := f.do("PATCH", one, body, ifMatch1)
		if w.Code != 400 || !jsonEqual(t, w.Body.String(), problemBody(400, "Bad Request", "invalid_request")) {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body)
		}
	}
	if f.stored(t, subjectID) != original || f.repo.Calls != 0 {
		t.Fatalf("malformed bodies touched state: calls=%d", f.repo.Calls)
	}
}

func TestCreateRejectsMalformedBodies(t *testing.T) {
	f := newFixture(true)
	for name, body := range map[string]string{
		"missing type":  `{"name":"a"}`,
		"missing name":  `{"type":"home"}`,
		"unknown key":   `{"name":"a","type":"home","archived":true}`,
		"duplicate key": `{"name":"a","name":"b","type":"home"}`,
		"null":          `{"name":"a","type":null}`,
		"wrong type":    `{"name":1,"type":"home"}`,
		"empty":         `{}`,
	} {
		w := f.do("POST", base, body, map[string]string{"Idempotency-Key": "malformed-" + strings.ReplaceAll(name, " ", "-")})
		if w.Code != 400 || !jsonEqual(t, w.Body.String(), problemBody(400, "Bad Request", "invalid_request")) {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body)
		}
	}
	if f.repo.Count() != 1 {
		t.Fatalf("malformed bodies persisted rows: %d", f.repo.Count())
	}
}

func TestValidationFailureIs422WithFieldAndCode(t *testing.T) {
	f := newFixture(true)
	for _, tc := range []struct{ body, field, code string }{
		{`{"name":"   ","type":"home"}`, "name", "invalid_length"},
		{`{"name":"` + strings.Repeat("a", 101) + `","type":"home"}`, "name", "invalid_length"},
		{`{"name":"a\u0000b","type":"home"}`, "name", "invalid_characters"},
		{`{"name":"a\u200bb","type":"home"}`, "name", "invalid_characters"},
		{`{"name":"ok","type":"robot"}`, "type", "invalid_type"},
	} {
		w := f.do("POST", base, tc.body, map[string]string{"Idempotency-Key": "validation-" + tc.code})
		want := `{"type":"about:blank","title":"Validation Failed","status":422,"code":"` + tc.code + `","field":"` + tc.field + `"}`
		if w.Code != 422 || w.Header().Get("Content-Type") != "application/problem+json" || !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("%s: status=%d body=%s", tc.body, w.Code, w.Body)
		}
	}
	if f.repo.Count() != 1 {
		t.Fatalf("invalid create persisted a row: %d", f.repo.Count())
	}
	original := f.stored(t, subjectID)
	for _, tc := range []struct{ body, field string }{{`{"type":"robot"}`, "type"}, {`{"name":" "}`, "name"}} {
		w := f.do("PATCH", one, tc.body, ifMatch1)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`) {
			t.Fatalf("patch %s: status=%d body=%s", tc.body, w.Code, w.Body)
		}
	}
	if f.stored(t, subjectID) != original {
		t.Fatalf("invalid patch changed row: %+v", f.stored(t, subjectID))
	}
}

func TestMediaTypeAndSizeLimits(t *testing.T) {
	f := newFixture(true)
	w := f.do("POST", base, `{"name":"a","type":"home"}`, map[string]string{"Content-Type": "text/plain", "Idempotency-Key": "media-type"})
	if w.Code != 415 || !jsonEqual(t, w.Body.String(), problemBody(415, "Unsupported Media Type", "unsupported_media_type")) {
		t.Fatalf("415: status=%d body=%s", w.Code, w.Body)
	}
	w = f.do("PATCH", one, `{"name":"a"}`, map[string]string{"Content-Type": "application/xml", "If-Match": `"1"`})
	if w.Code != 415 {
		t.Fatalf("patch 415: %d", w.Code)
	}
	big := `{"name":"` + strings.Repeat("a", 5000) + `","type":"home"}`
	w = f.do("POST", base, big, map[string]string{"Idempotency-Key": "large-body"})
	if w.Code != 413 || !jsonEqual(t, w.Body.String(), problemBody(413, "Request Entity Too Large", "content_too_large")) {
		t.Fatalf("413: status=%d body=%s", w.Code, w.Body)
	}
	req := httptest.NewRequest("POST", base, strings.NewReader(big))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "chunked-test")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("chunked 413: %d", rec.Code)
	}
	if f.repo.Calls != 0 || f.repo.Count() != 1 {
		t.Fatalf("repo touched: calls=%d rows=%d", f.repo.Calls, f.repo.Count())
	}
}

func TestNotFoundBodiesAreIdentical(t *testing.T) {
	f := newFixture(true)
	foreign := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	f.repo.Seed(subject.Subject{ID: foreign, HouseholdID: otherID, Type: subject.TypeHome, Name: "Foreign", CreatedAt: created, UpdatedAt: created, Version: 1})
	want := problemBody(404, "Not Found", "not_found")
	for name, tc := range map[string]struct{ method, path, body string }{
		"get non-member household":      {"GET", "/api/v1/households/" + otherID + "/subjects/" + foreign, ""},
		"get malformed household":       {"GET", "/api/v1/households/nope/subjects/" + subjectID, ""},
		"get nonexistent subject":       {"GET", base + "/" + otherID, ""},
		"get foreign subject via own":   {"GET", base + "/" + foreign, ""},
		"get malformed subject":         {"GET", base + "/nope", ""},
		"list non-member household":     {"GET", "/api/v1/households/" + otherID + "/subjects", ""},
		"list malformed household":      {"GET", "/api/v1/households/nope/subjects", ""},
		"post non-member household":     {"POST", "/api/v1/households/" + otherID + "/subjects", `{"name":"a","type":"home"}`},
		"patch non-member household":    {"PATCH", "/api/v1/households/" + otherID + "/subjects/" + foreign, `{"archived":true}`},
		"patch nonexistent subject":     {"PATCH", base + "/" + otherID, `{"archived":true}`},
		"patch foreign subject via own": {"PATCH", base + "/" + foreign, `{"archived":true}`},
		"patch malformed subject":       {"PATCH", base + "/nope", `{"archived":true}`},
	} {
		headers := ifMatch1
		if tc.method == "POST" {
			headers = map[string]string{"Idempotency-Key": "non-member-test"}
		}
		w := f.do(tc.method, tc.path, tc.body, headers)
		if w.Code != 404 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("ETag") != "" || !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body)
		}
	}
	if got := f.stored(t, foreign); got.Version != 1 || got.Archived || got.Name != "Foreign" {
		t.Fatalf("foreign subject changed: %+v", got)
	}
	if f.repo.Count() != 2 {
		t.Fatalf("non-member create persisted a row: %d", f.repo.Count())
	}
}

// Evaluation order: 401, origin (global), If-Match syntax (428/412), body
// (415/413/400), membership/existence (404), validation (422), stale version (412).
func TestEvaluationOrderForNonMembers(t *testing.T) {
	f := newFixture(true)
	nonMember := "/api/v1/households/" + otherID + "/subjects/" + subjectID
	w := f.do("PATCH", nonMember, `{"archived":true}`, nil)
	if w.Code != 428 || !jsonEqual(t, w.Body.String(), problemBody(428, "Precondition Required", "precondition_required")) {
		t.Fatalf("non-member without If-Match: status=%d body=%s", w.Code, w.Body)
	}
	w = f.do("PATCH", nonMember, `{"archived":true}`, ifMatch1)
	missing := f.do("PATCH", base+"/"+otherID, `{"archived":true}`, ifMatch1)
	if w.Code != 404 || missing.Code != 404 || w.Body.String() != missing.Body.String() {
		t.Fatalf("non-member with If-Match: %d %s vs nonexistent %d %s", w.Code, w.Body, missing.Code, missing.Body)
	}
	// Malformed body outranks membership; validation (422) does not.
	if w := f.do("PATCH", nonMember, `{}`, ifMatch1); w.Code != 400 {
		t.Fatalf("body check precedes membership: %d", w.Code)
	}
	if w := f.do("PATCH", nonMember, `{"name":" "}`, ifMatch1); w.Code != 404 {
		t.Fatalf("membership precedes validation: %d", w.Code)
	}
	// Validation outranks the stale-version check.
	if w := f.do("PATCH", one, `{"name":" "}`, map[string]string{"If-Match": `"9"`}); w.Code != 422 {
		t.Fatalf("validation precedes stale version: %d", w.Code)
	}
	if w := f.do("PATCH", one, `{"name":"ok"}`, map[string]string{"If-Match": `"9"`}); w.Code != 412 {
		t.Fatalf("stale: %d", w.Code)
	}
}

func TestUnauthenticatedShortCircuitsBeforeService(t *testing.T) {
	f := newFixture(false)
	original := f.stored(t, subjectID)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", base, ""}, {"GET", one, ""}, {"POST", base, `{"name":"a","type":"home"}`}, {"PATCH", one, `{"archived":true}`},
	} {
		headers := ifMatch1
		if tc.method == "POST" {
			headers = map[string]string{"Idempotency-Key": "anonymous-test"}
		}
		w := f.do(tc.method, tc.path, tc.body, headers)
		if w.Code != 401 || strings.Contains(w.Body.String(), subjectID) {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if f.svcHits != 0 || f.repo.Calls != 0 || f.authHits != 4 || f.repo.Count() != 1 || f.stored(t, subjectID) != original {
		t.Fatalf("membership=%d repo=%d authHits=%d", f.svcHits, f.repo.Calls, f.authHits)
	}
}

func TestMissingPrincipalAfterAuthIs401(t *testing.T) {
	repo := newRepo()
	hits := 0
	r := chi.NewRouter()
	New(subject.NewService(repo, func(context.Context, string, string) error { hits++; return nil }, nil), func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) { return "", false }).Register(r)
	for _, tc := range []struct{ method, path, body string }{{"GET", base, ""}, {"GET", one, ""}, {"POST", base, `{"name":"a","type":"home"}`}, {"PATCH", one, `{"archived":true}`}} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"1"`)
		if tc.method == "POST" {
			req.Header.Set("Idempotency-Key", "principal-test")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("%s: %d", tc.method, w.Code)
		}
	}
	if hits != 0 || repo.Calls != 0 || repo.Count() != 1 {
		t.Fatalf("hits=%d calls=%d rows=%d", hits, repo.Calls, repo.Count())
	}
}

func TestInfrastructureFailureIs503WithoutInternals(t *testing.T) {
	for name, setup := range map[string]func(*fixture){
		"repository":    func(f *fixture) { f.repo.Err = errors.New(secretErr) },
		"authorizer":    func(f *fixture) { f.authErr = errors.New(secretErr) },
		"repo sentinel": func(f *fixture) { f.repo.Err = subject.ErrUnavailable },
	} {
		f := newFixture(true)
		setup(f)
		for _, tc := range []struct{ method, path, body string }{
			{"GET", base, ""}, {"GET", one, ""}, {"POST", base, `{"name":"a","type":"home"}`}, {"PATCH", one, `{"archived":true}`},
		} {
			headers := ifMatch1
			if tc.method == "POST" {
				headers = map[string]string{"Idempotency-Key": "failure-test"}
			}
			w := f.do(tc.method, tc.path, tc.body, headers)
			if w.Code != 503 || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "pq") || strings.Contains(w.Body.String(), "10.1.2.3") {
				t.Fatalf("%s %s: status=%d body=%s", name, tc.method, w.Code, w.Body)
			}
			if !jsonEqual(t, w.Body.String(), problemBody(503, "Service Unavailable", "unavailable")) {
				t.Fatalf("%s %s: body=%s", name, tc.method, w.Body)
			}
		}
		if f.stored(t, subjectID).Version != 1 {
			t.Fatalf("%s: state changed", name)
		}
	}
}
