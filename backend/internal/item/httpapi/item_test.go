package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/itemtest"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/go-chi/chi/v5"
)

const (
	userID           = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	hid              = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	sid              = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	iid              = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80"
	other            = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
	missingIDForHTTP = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98"
)

var at = time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
var base = "/api/v1/households/" + hid + "/items"
var one = base + "/" + iid

type userKey struct{}
type fixture struct {
	repo          *itemtest.Repo
	handler       http.Handler
	member        error
	subject       error
	candidateErr  error
	memberLookups []string
	authHit       int
	svcHit        int
	zone          string
	now           time.Time
}

func newFixture(authenticate bool) *fixture {
	f := &fixture{repo: itemtest.New(), zone: "Europe/Prague", now: at}
	d := itemtest.Date(2026, time.March, 2)
	f.repo.Seed(item.Item{ID: iid, HouseholdID: hid, SubjectID: sid, Title: "Renew insurance", Notes: ptr("Quotes\n"), AttentionOn: d, WorkflowState: item.StateOpen, CreatedAt: at, UpdatedAt: at, Version: 1})
	houses := func(_ context.Context, uid, household string) (string, error) {
		f.svcHit++
		if f.member != nil {
			return "", f.member
		}
		if uid != userID || household != hid {
			return "", item.ErrNotFound
		}
		return f.zone, nil
	}
	subjects := func(_ context.Context, uid, household, subjectID string) (bool, error) {
		if f.subject != nil {
			return false, f.subject
		}
		if uid != userID || household != hid || subjectID != sid {
			return false, item.ErrNotFound
		}
		return false, nil
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.authHit++
			if !authenticate {
				httpxProblem(w, 401, "unauthenticated")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, userID)))
		})
	}
	r := chi.NewRouter()
	New(item.NewService(f.repo, houses, subjects, func(_ context.Context, candidate, household string) error {
		f.memberLookups = append(f.memberLookups, candidate+"/"+household)
		if f.candidateErr != nil {
			return f.candidateErr
		}
		if household != hid || (candidate != userID && candidate != "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62" && candidate != "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b65") {
			return item.ErrNotFound
		}
		return nil
	}, func() time.Time { return f.now }), auth, func(ctx context.Context) (string, bool) { v, ok := ctx.Value(userKey{}).(string); return v, ok }).Register(r)
	f.handler = r
	return f
}

func ptr[T any](v T) *T { return &v }
func httpxProblem(w http.ResponseWriter, status int, code string) {
	httpx.ProblemResponse(w, status, code)
}
func (f *fixture) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.HasSuffix(path, "/items") && method == http.MethodPost {
		sum := sha256.Sum256([]byte(body))
		req.Header.Set("Idempotency-Key", hex.EncodeToString(sum[:16]))
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}
func bodyEqual(t *testing.T, got, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal([]byte(got), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) != string(y) {
		t.Fatalf("got=%s want=%s", x, y)
	}
}
func problem(status int, code string) string {
	return `{"type":"about:blank","title":"` + http.StatusText(status) + `","status":` + strconv.Itoa(status) + `,"code":"` + code + `"}`
}
func stored(t *testing.T, f *fixture) item.Item {
	t.Helper()
	i, ok := f.repo.Row(iid)
	if !ok {
		t.Fatal("missing stored item")
	}
	return i
}

func (f *fixture) createWithKeys(body string, keys ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, base, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, key := range keys {
		req.Header.Add("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func TestSummaryHTTPResponseAndStructuralValidation(t *testing.T) {
	f := newFixture(true)
	for _, query := range []string{"q", "q=", "q=one&q=two"} {
		w := f.do("GET", base+"/summary?"+query, "", nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"q"`) {
			t.Fatalf("query=%q status=%d body=%s", query, w.Code, w.Body)
		}
	}
	w := f.do("GET", base+"/summary?q=%20%20%20", "", nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"q"`) {
		t.Fatalf("whitespace search status=%d body=%s", w.Code, w.Body)
	}

	f = newFixture(true)
	w = f.do(http.MethodGet, base+"/summary", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	bodyEqual(t, w.Body.String(), `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":1,"inProgress":0,"waiting":0,"upcoming":0,"paused":0,"done":0,"archived":0}`)
	if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatalf("headers=%v", w.Header())
	}
	memberID := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", HouseholdID: hid, SubjectID: sid, ResponsibleUserID: ptr(memberID), Title: "assigned", WorkflowState: item.StateWaiting, AttentionOn: itemtest.Date(2026, time.March, 3), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}})
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82", HouseholdID: hid, SubjectID: other, Title: "other subject", WorkflowState: item.StatePaused})
	for _, tc := range []struct{ query, body string }{
		{"?subjectId=" + sid, `{"evaluatedOn":"2026-03-02","total":2,"needsAttention":1,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?responsibleUserId=" + memberID, `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?responsibleUserId=unassigned", `{"evaluatedOn":"2026-03-02","total":2,"needsAttention":1,"inProgress":0,"waiting":0,"upcoming":0,"paused":1,"done":0,"archived":0}`},
		{"?workflowState=waiting", `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?attention=upcoming", `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?recurring=true", `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?recurring=false", `{"evaluatedOn":"2026-03-02","total":2,"needsAttention":1,"inProgress":0,"waiting":0,"upcoming":0,"paused":1,"done":0,"archived":0}`},
		{"?subjectId=" + sid + "&workflowState=waiting&attention=upcoming&recurring=true&responsibleUserId=" + memberID, `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?attentionDate=2026-03-02", `{"evaluatedOn":"2026-03-02","total":3,"needsAttention":1,"inProgress":0,"waiting":1,"upcoming":0,"paused":1,"done":0,"archived":0}`},
		{"?q=assigned", `{"evaluatedOn":"2026-03-02","total":1,"needsAttention":0,"inProgress":0,"waiting":1,"upcoming":0,"paused":0,"done":0,"archived":0}`},
		{"?q=comma%2Cterm", `{"evaluatedOn":"2026-03-02","total":0,"needsAttention":0,"inProgress":0,"waiting":0,"upcoming":0,"paused":0,"done":0,"archived":0}`},
	} {
		w = f.do(http.MethodGet, base+"/summary"+tc.query, "", nil)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
			t.Errorf("query %s status=%d headers=%v body=%s", tc.query, w.Code, w.Header(), w.Body)
			continue
		}
		bodyEqual(t, w.Body.String(), tc.body)
	}
	for _, tc := range []struct{ query, param string }{
		{"?limit=", "limit"}, {"?cursor=x", "cursor"}, {"?archived=false", "archived"}, {"?done=true", "done"}, {"?sort=created", "sort"},
		{"?sort=created&limit=5", "limit"}, {"?subjectId=", "subjectId"}, {"?subjectId=" + sid + "&subjectId=" + sid, "subjectId"}, {"?responsibleUserId=", "responsibleUserId"}, {"?workflowState=", "workflowState"}, {"?attention=", "attention"}, {"?recurring=", "recurring"}, {"?recurring=true&recurring=true", "recurring"}, {"?attentionDate=", "attentionDate"}, {"?q", "q"}, {"?q=", "q"}, {"?q=one&q=two", "q"}, {"?q=%20%20", "q"}, {"?recurring=True", "recurring"}, {"?workflowState=OPEN", "workflowState"},
		{"?subjectId=bad", "subjectId"}, {"?responsibleUserId=bad", "responsibleUserId"}, {"?attentionDate=2026-02-30", "attentionDate"}, {"?attentionDate=2026-03-02&attentionDate=2026-03-02", "attentionDate"}, {"?workflowState=open,paused", "workflowState"}, {"?workflowState=%20open", "workflowState"},
	} {
		w = f.do(http.MethodGet, base+"/summary"+tc.query, "", nil)
		assertQueryProblem(t, w, tc.param)
	}
	unauth := newFixture(false)
	w = unauth.do(http.MethodGet, base+"/summary?workflowState=OPEN", "", nil)
	if w.Code != 401 || unauth.svcHit != 0 {
		t.Fatalf("auth status=%d service calls=%d", w.Code, unauth.svcHit)
	}
	f.member = item.ErrNotFound
	structural := f.do(http.MethodGet, "/api/v1/households/"+other+"/items/summary?q=", "", nil)
	semantic := f.do(http.MethodGet, "/api/v1/households/"+other+"/items/summary?q=%20", "", nil)
	if structural.Code != 400 || semantic.Code != 404 {
		t.Fatalf("structural=%d %s semantic=%d %s", structural.Code, structural.Body, semantic.Code, semantic.Body)
	}
	bodies := []string{semantic.Body.String()}
	f.member = nil
	for _, path := range []string{other, missingIDForHTTP, "not-a-uuid"} {
		res := f.do(http.MethodGet, "/api/v1/households/"+path+"/items/summary?q=%20", "", nil)
		if res.Code != 404 {
			t.Fatalf("household %q status=%d %s", path, res.Code, res.Body)
		}
		bodies = append(bodies, res.Body.String())
	}
	for i, body := range bodies {
		if body != bodies[0] {
			t.Fatalf("nonuniform not-found body %d: %q", i, bodies)
		}
	}
	// The literal summary route returns summary fields, not an Item representation.
	f.member = nil
	w = f.do(http.MethodGet, base+"/summary", "", nil)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["total"] == nil || got["id"] != nil {
		t.Fatalf("summary dispatched as item: %s err=%v", w.Body, err)
	}
}

func TestCreateIdempotencyHeaderValidation(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		code string
	}{
		{"missing", nil, "idempotency_key_required"}, {"empty", []string{""}, "invalid_idempotency_key"}, {"repeated", []string{"one", "two"}, "invalid_idempotency_key"}, {"non ascii", []string{"café"}, "invalid_idempotency_key"}, {"too long", []string{strings.Repeat("x", 129)}, "invalid_idempotency_key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			w := f.createWithKeys("{", tc.keys...)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
	f := newFixture(true)
	w := f.createWithKeys(`{"title":"Valid","subjectId":"`+sid+`"}`, strings.Repeat("x", 128))
	if w.Code != 201 {
		t.Fatalf("128-byte key status=%d body=%s", w.Code, w.Body)
	}
	f = newFixture(true)
	w = f.createWithKeys("{")
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"idempotency_key_required"`) {
		t.Fatalf("key must precede body decoding: status=%d body=%s", w.Code, w.Body)
	}
}

func TestCreateIdempotencyOmittedOptionalFieldsReplayAsNull(t *testing.T) {
	f := newFixture(true)
	key := "optional-null-equivalence"
	baseline := f.repo.Count()
	first := f.createWithKeys(`{"title":"Optional","subjectId":"`+sid+`"}`, key)
	if first.Code != 201 {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body)
	}
	replay := f.createWithKeys(`{"title":"Optional","subjectId":"`+sid+`","notes":null,"attentionOn":null,"responsibleUserId":null,"recurrence":null}`, key)
	if replay.Code != 201 {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body)
	}
	var a, b map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if a["id"] != b["id"] || f.repo.Count() != baseline+1 {
		t.Fatalf("omitted/null did not replay: first=%v replay=%v rows=%d", a, b, f.repo.Count())
	}
}

func TestCreateIdempotencyReplayAndReuseResponses(t *testing.T) {
	f := newFixture(true)
	body := `{"title":"Idempotent","subjectId":"` + sid + `"}`
	first := f.do(http.MethodPost, base, body, nil)
	if first.Code != 201 {
		t.Fatalf("first=%d %s", first.Code, first.Body)
	}
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	createdID := firstBody["id"].(string)
	patched := f.do(http.MethodPatch, base+"/"+createdID, `{"title":"Edited"}`, map[string]string{"If-Match": "\"1\""})
	if patched.Code != 200 {
		t.Fatalf("patch=%d %s", patched.Code, patched.Body)
	}
	replay := f.do(http.MethodPost, base, body, nil)
	if replay.Code != 201 || replay.Header().Get("Location") != first.Header().Get("Location") || replay.Header().Get("ETag") != `"2"` {
		t.Fatalf("replay status=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(replay.Body.Bytes(), &got); err != nil || got["title"] != "Edited" {
		t.Fatalf("replay body=%v err=%v", got, err)
	}
	// Use the deterministic fixture key explicitly, as the helper derives it from the body by default.
	var reused *httptest.ResponseRecorder
	sum := sha256.Sum256([]byte(body))
	key := hex.EncodeToString(sum[:16])
	reused = f.createWithKeys(`{"title":"Different","subjectId":"`+sid+`"}`, key)
	want := problem(422, "idempotency_key_reused")
	bodyEqual(t, reused.Body.String(), want)
	var problemBody map[string]any
	_ = json.Unmarshal(reused.Body.Bytes(), &problemBody)
	if reused.Code != 422 || reused.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("reuse status=%d headers=%v body=%s", reused.Code, reused.Header(), reused.Body)
	}
	if _, ok := problemBody["field"]; ok {
		t.Fatalf("unexpected field in reuse problem: %v", problemBody)
	}
	_ = first
}

func TestListStructuredQueryValidationAndFilteredSuccess(t *testing.T) {
	for _, tc := range []struct{ query, parameter string }{
		{"subjectId=", "subjectId"}, {"subjectId=" + sid + "&subjectId=" + sid, "subjectId"}, {"subjectId=" + sid + "," + other, "subjectId"}, {"subjectId=bad", "subjectId"},
		{"responsibleUserId=", "responsibleUserId"}, {"responsibleUserId=unassigned&responsibleUserId=unassigned", "responsibleUserId"}, {"responsibleUserId=unassigned,unassigned", "responsibleUserId"}, {"responsibleUserId=NoNe", "responsibleUserId"},
		{"workflowState=", "workflowState"}, {"workflowState=open&workflowState=open", "workflowState"}, {"workflowState=open,paused", "workflowState"}, {"workflowState=OPEN", "workflowState"},
		{"attention=", "attention"}, {"attention=upcoming&attention=upcoming", "attention"}, {"attention=upcoming,needs_attention", "attention"}, {"attention=UPCOMING", "attention"},
		{"recurring=", "recurring"}, {"recurring=true&recurring=true", "recurring"}, {"recurring=true,false", "recurring"}, {"recurring=1", "recurring"}, {"q", "q"}, {"q=", "q"}, {"q=x&q=y", "q"}, {"q=%20%20", "q"},
	} {
		t.Run(tc.parameter+"/"+tc.query, func(t *testing.T) {
			f := newFixture(true)
			w := f.do("GET", base+"?"+tc.query, "", nil)
			if w.Code != 400 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
			}
			bodyEqual(t, w.Body.String(), `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_query","parameter":"`+tc.parameter+`"}`)
		})
	}
	f := newFixture(true)
	assigned := "0198a2f0-7c1e-7a53-9b0e-000000000062"
	future := itemtest.Date(2026, time.March, 3)
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", HouseholdID: hid, SubjectID: sid, ResponsibleUserID: &assigned, AttentionOn: future, WorkflowState: item.StatePaused, Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}, CreatedAt: at, UpdatedAt: at, Version: 1})
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82", HouseholdID: hid, SubjectID: other, ResponsibleUserID: &assigned, AttentionOn: future, WorkflowState: item.StatePaused, CreatedAt: at, UpdatedAt: at, Version: 1})
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83", HouseholdID: hid, SubjectID: sid, AttentionOn: future, WorkflowState: item.StateOpen, CreatedAt: at, UpdatedAt: at, Version: 1})
	decodeIDs := func(t *testing.T, w *httptest.ResponseRecorder) []string {
		t.Helper()
		var got struct {
			Items      []Item  `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(got.Items))
		for i, v := range got.Items {
			ids[i] = v.Id
		}
		return ids
	}
	cases := []struct {
		name, query string
		want        []string
		limit       int
	}{
		{"subject", "subjectId=" + strings.ToUpper(sid), []string{iid, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83"}, 0},
		{"responsible uuid", "responsibleUserId=" + assigned, []string{"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82"}, 0},
		{"unassigned", "responsibleUserId=unassigned", []string{iid, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83"}, 0},
		{"workflow", "workflowState=paused", []string{"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82"}, 0},
		{"attention", "attention=needs_attention", []string{iid}, 0},
		{"recurring true", "recurring=true", []string{"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"}, 0},
		{"recurring false", "recurring=false", []string{iid, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83"}, 0},
		{"paged subject", "subjectId=" + sid + "&limit=1", []string{iid}, 1},
		{"literal comma q", "q=,", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := base + "?" + tc.query
			w := f.do("GET", path, "", nil)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			got := decodeIDs(t, w)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("IDs=%v want=%v", got, tc.want)
			}
			var body struct {
				NextCursor *string `json:"nextCursor"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if tc.limit == 0 && body.NextCursor != nil {
				t.Fatalf("cursor=%v want null", *body.NextCursor)
			}
			if tc.limit == 1 {
				if body.NextCursor == nil {
					t.Fatal("expected i2 cursor")
				}
				raw, err := base64.RawURLEncoding.DecodeString(*body.NextCursor)
				if err != nil || !strings.HasPrefix(string(raw), "i2:") {
					t.Fatalf("cursor=%q decoded=%q err=%v", *body.NextCursor, raw, err)
				}
			}
		})
	}
	for _, unknown := range []string{missingIDForHTTP, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b97"} {
		w := f.do("GET", base+"?subjectId="+unknown, "", nil)
		if w.Code != 200 {
			t.Fatalf("unknown subject status=%d body=%s", w.Code, w.Body)
		}
		var empty struct {
			Items      []Item  `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil {
			t.Fatal(err)
		}
		if len(empty.Items) != 0 || empty.NextCursor != nil {
			t.Fatalf("unknown subject response=%s", w.Body)
		}
	}
}

func TestListQueryPrecedenceUnknownParametersAndLegacyCursor(t *testing.T) {
	unauth := newFixture(false)
	if w := unauth.do("GET", base+"?subjectId=", "", nil); w.Code != 401 {
		t.Fatalf("anonymous=%d %s", w.Code, w.Body)
	}
	for _, tc := range []struct {
		query  string
		status int
	}{{"subjectId=", 400}, {"workflowState=open&workflowState=open", 400}, {"subjectId=bad", 404}, {"workflowState=OPEN", 404}, {"subjectId=" + sid + "&cursor=bad", 404}} {
		f := newFixture(true)
		f.member = item.ErrNotFound
		w := f.do("GET", "/api/v1/households/"+other+"/items?"+tc.query, "", nil)
		if w.Code != tc.status {
			t.Errorf("query %s status=%d want=%d body=%s", tc.query, w.Code, tc.status, w.Body)
		}
	}
	f := newFixture(true)
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", HouseholdID: hid, SubjectID: sid, Title: "second", WorkflowState: item.StateOpen, CreatedAt: at, UpdatedAt: at, Version: 1})
	w := f.do("GET", base+"?futureFlag=x&limit=1", "", nil)
	if w.Code != 200 {
		t.Fatalf("unknown params status=%d body=%s", w.Code, w.Body)
	}
	var got struct {
		Items      []Item  `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Id != iid || got.NextCursor == nil {
		t.Fatalf("legacy page=%+v", got)
	}
	raw, err := base64.RawURLEncoding.DecodeString(*got.NextCursor)
	if err != nil || !strings.HasPrefix(string(raw), "i1:") {
		t.Fatalf("legacy cursor=%q decoded=%q err=%v", *got.NextCursor, raw, err)
	}
}

func TestListAttentionSortGuardAndQueryContracts(t *testing.T) {
	for _, raw := range []string{"sort=", "sort=created&sort=attention_on", "sort=created,attention_on", "sort=CREATED", "sort=%20created", "sort=title", "attentionDate=", "attentionDate=2026-02-30", "attentionDate=2026-03-01T00%3A00%3A00Z", "attentionDate=0001-01-01"} {
		f := newFixture(true)
		w := f.do("GET", base+"?"+raw, "", nil)
		var got map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		want := "attentionDate"
		if strings.HasPrefix(raw, "sort=") {
			want = "sort"
		}
		if w.Code != 400 || got["code"] != "invalid_query" || got["parameter"] != want || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s response=%d %v headers=%v", raw, w.Code, got, w.Header())
		}
	}
	f := newFixture(true)
	f.now = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	d := itemtest.Date(2026, time.March, 2)
	f.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", HouseholdID: hid, SubjectID: sid, AttentionOn: d})
	for _, tc := range []struct{ query, prefix string }{{"sort=created&limit=1", "i1:"}, {"sort=attention_on&limit=1", "i2:"}, {"attentionDate=2026-03-01&limit=1", "i2:"}} {
		w := f.do("GET", base+"?"+tc.query, "", nil)
		var got struct {
			Items      []Item  `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if w.Code != 200 || got.NextCursor == nil {
			t.Errorf("%s: %d %s", tc.query, w.Code, w.Body)
			continue
		}
		raw, _ := base64.RawURLEncoding.DecodeString(*got.NextCursor)
		if !strings.HasPrefix(string(raw), tc.prefix) {
			t.Errorf("%s cursor=%q", tc.query, raw)
		}
	}
	unauth := newFixture(false)
	if w := unauth.do("GET", base+"?sort=&attentionDate=bad", "", nil); w.Code != 401 {
		t.Errorf("auth precedence=%d", w.Code)
	}
	f = newFixture(true)
	f.member = item.ErrNotFound
	if w := f.do("GET", "/api/v1/households/"+other+"/items?sort=title&attentionDate=bad", "", nil); w.Code != 404 {
		t.Errorf("membership precedence=%d %s", w.Code, w.Body)
	}
}

func TestHTTPAttentionSortPreservesOrderAcrossPages(t *testing.T) {
	f := newFixture(true)
	rows := []item.Item{
		{ID: "0198a2f0-7c1e-7a53-9b0e-000000000001", HouseholdID: hid, SubjectID: sid, Title: "future-low", AttentionOn: itemtest.Date(2026, time.March, 10)},
		{ID: "0198a2f0-7c1e-7a53-9b0e-000000000003", HouseholdID: hid, SubjectID: sid, Title: "past", AttentionOn: itemtest.Date(2026, time.March, 1)},
		{ID: "0198a2f0-7c1e-7a53-9b0e-000000000002", HouseholdID: hid, SubjectID: sid, Title: "past-low", AttentionOn: itemtest.Date(2026, time.March, 1)},
		{ID: "0198a2f0-7c1e-7a53-9b0e-000000000005", HouseholdID: hid, SubjectID: sid, Title: "null-high"},
		{ID: iid, HouseholdID: hid, SubjectID: sid, Title: "dated-last", AttentionOn: itemtest.Date(2026, time.March, 11)},
		{ID: "0198a2f0-7c1e-7a53-9b0e-000000000004", HouseholdID: hid, SubjectID: sid, Title: "future", AttentionOn: itemtest.Date(2026, time.March, 10)},
	}
	for _, row := range rows {
		f.repo.Seed(row)
	}
	want := []string{rows[3].ID, rows[2].ID, rows[1].ID, rows[0].ID, rows[5].ID, rows[4].ID}
	url := base + "?sort=attention_on&limit=1"
	var got []string
	for page := 0; page < 8; page++ {
		w := f.do("GET", url, "", nil)
		var body struct {
			Items []Item  `json:"items"`
			Next  *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(body.Items) != 1 {
			t.Fatalf("response=%d %s", w.Code, w.Body)
		}
		got = append(got, body.Items[0].Id)
		wantAttention := body.Items[0].AttentionOn
		if page == 0 && wantAttention != nil {
			t.Errorf("null first page=%+v", body.Items[0])
		}
		if page == 1 && (wantAttention == nil || *wantAttention != "2026-03-01") {
			t.Errorf("past page=%+v", body.Items[0])
		}
		if page == 4 && (wantAttention == nil || *wantAttention != "2026-03-10") {
			t.Errorf("dated page=%+v", body.Items[0])
		}
		if page == 5 && (wantAttention == nil || *wantAttention != "2026-03-11") {
			t.Errorf("last dated page=%+v", body.Items[0])
		}
		if body.Next == nil {
			break
		}
		if page == 7 {
			t.Fatal("expected final page before bound")
		}
		url = base + "?sort=attention_on&limit=1&cursor=" + *body.Next
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestHTTPAttentionDateScalarValidation(t *testing.T) {
	for _, query := range []string{"attentionDate=2026-03-01&attentionDate=2026-03-01", "attentionDate=2026-03-01,2026-03-01", "attentionDate=%202026-03-01", "attentionDate=2026-03-03"} {
		f := newFixture(true)
		w := f.do("GET", base+"?"+query, "", nil)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 400 || body["code"] != "invalid_query" || body["parameter"] != "attentionDate" || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s => %d %v headers=%v", query, w.Code, body, w.Header())
		}
	}
}

func TestHTTPListCursorContextErrors(t *testing.T) {
	f := newFixture(true)
	second := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"
	f.repo.Seed(item.Item{ID: second, HouseholdID: hid, SubjectID: sid, Title: "second"})
	filtered := f.do("GET", base+"?subjectId="+sid+"&sort=created&limit=1", "", nil)
	var list struct {
		Next *string `json:"nextCursor"`
	}
	_ = json.Unmarshal(filtered.Body.Bytes(), &list)
	if filtered.Code != 200 || list.Next == nil {
		t.Fatalf("filtered=%d %s", filtered.Code, filtered.Body)
	}
	attention := f.do("GET", base+"?sort=attention_on&limit=1&cursor="+*list.Next, "", nil)
	assertQueryProblem(t, attention, "cursor")
	f.now = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	guarded := f.do("GET", base+"?attentionDate=2026-03-01&limit=1", "", nil)
	_ = json.Unmarshal(guarded.Body.Bytes(), &list)
	if guarded.Code != 200 || list.Next == nil {
		t.Fatalf("guard=%d %s", guarded.Code, guarded.Body)
	}
	mismatched := f.do("GET", base+"?limit=1&cursor="+*list.Next, "", nil)
	assertQueryProblem(t, mismatched, "cursor")
	stale := f.do("GET", base+"?attentionDate=2026-03-02&cursor=bad", "", nil)
	assertQueryProblem(t, stale, "attentionDate")
}
func assertQueryProblem(t *testing.T, w *httptest.ResponseRecorder, param string) {
	t.Helper()
	want := `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_query","parameter":"` + param + `"}`
	bodyEqual(t, w.Body.String(), want)
	if w.Code != 400 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
}
func TestHTTPListNewParameterPrecedence(t *testing.T) {
	for _, raw := range []string{"sort=", "sort=created&sort=created", "attentionDate=", "attentionDate=2026-03-01&attentionDate=2026-03-01"} {
		f := newFixture(true)
		f.member = item.ErrNotFound
		w := f.do("GET", "/api/v1/households/"+other+"/items?"+raw, "", nil)
		var b map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		param := "attentionDate"
		if strings.HasPrefix(raw, "sort=") {
			param = "sort"
		}
		if w.Code != 400 || b["parameter"] != param {
			t.Errorf("transport %s => %d %s", raw, w.Code, w.Body)
		}
	}
	f := newFixture(true)
	f.member = item.ErrNotFound
	w := f.do("GET", "/api/v1/households/"+other+"/items?sort=title&attentionDate=2026-02-30", "", nil)
	if w.Code != 404 || w.Body.String() != `{"code":"not_found","status":404,"title":"Not Found","type":"about:blank"}`+"\n" {
		t.Fatalf("semantic membership precedence=%d %s", w.Code, w.Body)
	}
}

func TestListDoneQueryValidation(t *testing.T) {
	f := newFixture(true)
	w := f.do("GET", base+"?done=bad", "", nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"parameter":"done"`) {
		t.Fatalf("bad done query status=%d body=%s", w.Code, w.Body)
	}
}

func TestCreateGetListAndPatchPersistence(t *testing.T) {
	f := newFixture(true)
	created := f.do("POST", base, `{"title":"  New item  ","subjectId":"`+sid+`","notes":"  keep\n ","attentionOn":"2026-03-02"}`, nil)
	var createdBody map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id, ok := createdBody["id"].(string)
	if !ok || id == "" {
		t.Fatalf("create response has no string id: %s", created.Body)
	}
	row, persisted := f.repo.Row(id)
	if !persisted {
		t.Fatalf("created item %s was not persisted", id)
	}
	if created.Code != 201 || created.Header().Get("ETag") != `"1"` || created.Header().Get("Location") != base+"/"+id || created.Header().Get("Content-Type") != "application/json" || created.Header().Get("Cache-Control") != "no-store" || row.Title != "New item" || row.Notes == nil || *row.Notes != "  keep\n " || row.Version != 1 || row.WorkflowState != item.StateOpen {
		t.Fatalf("status=%d headers=%v body=%s row=%+v", created.Code, created.Header(), created.Body, row)
	}
	want := `{"id":"` + id + `","subjectId":"` + sid + `","responsibleUserId":null,"title":"New item","notes":"  keep\n ","attentionOn":"2026-03-02","workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z","recurrence":null}`
	bodyEqual(t, created.Body.String(), want)
	get := f.do("GET", base+"/"+id, "", nil)
	if get.Code != 200 || get.Header().Get("ETag") != `"1"` || get.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("get=%d headers=%v %s", get.Code, get.Header(), get.Body)
	}
	bodyEqual(t, get.Body.String(), want)
	list := f.do("GET", base, "", nil)
	if list.Code != 200 {
		t.Fatalf("list=%d %s", list.Code, list.Body)
	}
	seeded := `{"id":"` + iid + `","subjectId":"` + sid + `","responsibleUserId":null,"title":"Renew insurance","notes":"Quotes\n","attentionOn":"2026-03-02","workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-03-01T23:30:00Z","updatedAt":"2026-03-01T23:30:00Z","recurrence":null}`
	bodyEqual(t, list.Body.String(), `{"items":[`+want+`,`+seeded+`],"nextCursor":null}`)
	patched := f.do("PATCH", base+"/"+id, `{"notes":null,"attentionOn":null,"workflowState":"paused"}`, map[string]string{"If-Match": `"1"`})
	updated, exists := f.repo.Row(id)
	if !exists || patched.Code != 200 || patched.Header().Get("ETag") != `"2"` || updated.Version != 2 || updated.Notes != nil || updated.AttentionOn != nil || updated.WorkflowState != item.StatePaused {
		t.Fatalf("patch=%d %s row=%+v exists=%v", patched.Code, patched.Body, updated, exists)
	}
	bodyEqual(t, patched.Body.String(), `{"id":"`+id+`","subjectId":"`+sid+`","responsibleUserId":null,"title":"New item","notes":null,"attentionOn":null,"recurrence":null,"workflowState":"paused","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
}

func TestPatchInvalidInputMissingTargetIsNotFound(t *testing.T) {
	f := newFixture(true)
	missing := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
	w := f.do("PATCH", base+"/"+missing, `{"title":" "}`, map[string]string{"If-Match": `"1"`})
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestRecurrenceStrictDecodingAndValidation(t *testing.T) {
	f := newFixture(true)
	for name, policy := range map[string]string{
		"unknown nested":    `{"intervalValue":1,"intervalUnit":"day","mode":"fixed","extra":1}`,
		"missing nested":    `{"intervalValue":1,"intervalUnit":"day"}`,
		"string interval":   `{"intervalValue":"1","intervalUnit":"day","mode":"fixed"}`,
		"float interval":    `{"intervalValue":1.0,"intervalUnit":"day","mode":"fixed"}`,
		"null nested field": `{"intervalValue":null,"intervalUnit":"day","mode":"fixed"}`,
	} {
		w := f.do("POST", base, `{"title":"x","subjectId":"`+sid+`","recurrence":`+policy+`}`, nil)
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	for _, test := range []struct{ policy, code string }{
		{`{"intervalValue":0,"intervalUnit":"day","mode":"fixed"}`, "invalid_interval"},
		{`{"intervalValue":1000,"intervalUnit":"day","mode":"fixed"}`, "invalid_interval"},
		{`{"intervalValue":1,"intervalUnit":"fortnight","mode":"fixed"}`, "invalid_interval_unit"},
		{`{"intervalValue":1,"intervalUnit":"day","mode":"weird"}`, "invalid_mode"},
	} {
		w := f.do("POST", base, `{"title":"x","subjectId":"`+sid+`","recurrence":`+test.policy+`}`, nil)
		if w.Code != 422 {
			t.Fatalf("%s: %d %s", test.code, w.Code, w.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if got["field"] != "recurrence" || got["code"] != test.code || f.repo.Count() != 1 {
			t.Fatalf("%s response=%v stored=%d", test.code, got, f.repo.Count())
		}
	}
	w := f.do("PATCH", one, `{"recurrence":null}`, map[string]string{"If-Match": `"1"`})
	if w.Code != 200 {
		t.Fatalf("clear recurrence: %d %s", w.Code, w.Body)
	}
	bodyEqual(t, w.Body.String(), `{"id":"`+iid+`","subjectId":"`+sid+`","responsibleUserId":null,"title":"Renew insurance","notes":"Quotes\n","attentionOn":"2026-03-02","recurrence":null,"workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-03-01T23:30:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
}

func TestRecurrenceIntegerRangeErrorsDoNotPersist(t *testing.T) {
	for _, literal := range []string{"4294967297", "-4294967295", "99999999999999999999"} {
		t.Run(literal, func(t *testing.T) {
			f := newFixture(true)
			before := stored(t, f)
			policy := `{"intervalValue":` + literal + `,"intervalUnit":"day","mode":"fixed"}`
			post := f.do("POST", base, `{"title":"bad","subjectId":"`+sid+`","recurrence":`+policy+`}`, nil)
			if post.Code != 422 {
				t.Fatalf("POST status=%d body=%s", post.Code, post.Body)
			}
			bodyEqual(t, post.Body.String(), `{"type":"about:blank","title":"Validation Failed","status":422,"code":"invalid_interval","field":"recurrence"}`)
			if f.repo.Count() != 1 {
				t.Fatalf("POST persisted invalid policy: count=%d", f.repo.Count())
			}
			patch := f.do("PATCH", one, `{"recurrence":`+policy+`}`, map[string]string{"If-Match": `"1"`})
			if patch.Code != 422 {
				t.Fatalf("PATCH status=%d body=%s", patch.Code, patch.Body)
			}
			bodyEqual(t, patch.Body.String(), `{"type":"about:blank","title":"Validation Failed","status":422,"code":"invalid_interval","field":"recurrence"}`)
			if got := stored(t, f); got != before {
				t.Fatalf("PATCH persisted invalid policy: before=%+v after=%+v", before, got)
			}
		})
	}
}

func TestRecurrenceGetListAndClearResponses(t *testing.T) {
	f := newFixture(true)
	fixed := `{"intervalValue":1,"intervalUnit":"year","mode":"fixed"}`
	fluid := `{"intervalValue":2,"intervalUnit":"month","mode":"after_completion"}`
	w := f.do("POST", base, `{"title":"Fixed","subjectId":"`+sid+`","attentionOn":"2099-01-01","recurrence":`+fixed+`}`, nil)
	if w.Code != 201 {
		t.Fatalf("create fixed: %d %s", w.Code, w.Body)
	}
	var fixedBody Item
	if err := json.Unmarshal(w.Body.Bytes(), &fixedBody); err != nil {
		t.Fatal(err)
	}
	fixedWant := `{"id":"` + fixedBody.Id + `","subjectId":"` + sid + `","responsibleUserId":null,"title":"Fixed","notes":null,"attentionOn":"2099-01-01","recurrence":{"intervalValue":1,"intervalUnit":"year","mode":"fixed"},"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
	bodyEqual(t, w.Body.String(), fixedWant)
	got := f.do("GET", base+"/"+fixedBody.Id, "", nil)
	if got.Code != 200 {
		t.Fatalf("get=%d %s", got.Code, got.Body)
	}
	bodyEqual(t, got.Body.String(), fixedWant)
	w = f.do("POST", base, `{"title":"Fluid","subjectId":"`+sid+`","attentionOn":"2099-02-01","recurrence":`+fluid+`}`, nil)
	if w.Code != 201 {
		t.Fatalf("create fluid: %d %s", w.Code, w.Body)
	}
	var fluidBody Item
	if err := json.Unmarshal(w.Body.Bytes(), &fluidBody); err != nil {
		t.Fatal(err)
	}
	fluidWant := `{"id":"` + fluidBody.Id + `","subjectId":"` + sid + `","responsibleUserId":null,"title":"Fluid","notes":null,"attentionOn":"2099-02-01","recurrence":{"intervalValue":2,"intervalUnit":"month","mode":"after_completion"},"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
	bodyEqual(t, w.Body.String(), fluidWant)
	list := f.do("GET", base, "", nil)
	if list.Code != 200 {
		t.Fatalf("list=%d %s", list.Code, list.Body)
	}
	var listBody struct {
		Items      []Item  `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Items) != 3 || listBody.Items[0].Recurrence == nil || *listBody.Items[0].Recurrence != (ItemRecurrence{IntervalValue: 1, IntervalUnit: Year, Mode: Fixed}) || listBody.Items[1].Recurrence == nil || *listBody.Items[1].Recurrence != (ItemRecurrence{IntervalValue: 2, IntervalUnit: Month, Mode: AfterCompletion}) {
		t.Fatalf("list lost recurrence policies: %s", list.Body)
	}
	clear := f.do("PATCH", base+"/"+fixedBody.Id, `{"recurrence":null}`, map[string]string{"If-Match": `"1"`})
	if clear.Code != 200 {
		t.Fatalf("clear=%d %s", clear.Code, clear.Body)
	}
	bodyEqual(t, clear.Body.String(), `{"id":"`+fixedBody.Id+`","subjectId":"`+sid+`","responsibleUserId":null,"title":"Fixed","notes":null,"attentionOn":"2099-01-01","recurrence":null,"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
	storedRow, _ := f.repo.Row(fixedBody.Id)
	if storedRow.Recurrence != nil || storedRow.AttentionOn == nil || storedRow.AttentionOn.String() != "2099-01-01" || storedRow.Version != 2 {
		t.Fatalf("clear row=%+v", storedRow)
	}
}

func TestInvalidRecurrencePatchPreservesEnabledPolicyAndCycle(t *testing.T) {
	f := newFixture(true)
	policy := `{"intervalValue":1,"intervalUnit":"year","mode":"fixed"}`
	created := f.do("POST", base, `{"title":"Fixed","subjectId":"`+sid+`","attentionOn":"2099-01-01","recurrence":`+policy+`}`, nil)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body)
	}
	var response Item
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	before, _ := f.repo.Row(response.Id)
	bad := f.do("PATCH", base+"/"+response.Id, `{"recurrence":{"intervalValue":1,"intervalUnit":"fortnight","mode":"fixed"}}`, map[string]string{"If-Match": `"1"`})
	if bad.Code != 422 {
		t.Fatalf("patch=%d %s", bad.Code, bad.Body)
	}
	bodyEqual(t, bad.Body.String(), `{"type":"about:blank","title":"Validation Failed","status":422,"code":"invalid_interval_unit","field":"recurrence"}`)
	after, _ := f.repo.Row(response.Id)
	if after != before {
		t.Fatalf("invalid recurrence patch changed stored item: before=%+v after=%+v", before, after)
	}
}

func TestBodyLimitAllowsMaximumUnicodeAndRejectsOversize(t *testing.T) {
	for _, tc := range []struct {
		name, title, notes, wantTitle, wantNotes string
	}{
		{name: "literal supplementary characters", title: strings.Repeat("😀", 200), notes: strings.Repeat("𐐷", 4000), wantTitle: strings.Repeat("😀", 200), wantNotes: strings.Repeat("𐐷", 4000)},
		{name: "escaped supplementary characters", title: strings.Repeat(`\ud83d\ude00`, 200), notes: strings.Repeat(`\ud801\udc37`, 4000), wantTitle: strings.Repeat("😀", 200), wantNotes: strings.Repeat("𐐷", 4000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			body := `{"title":"` + tc.title + `","subjectId":"` + sid + `","notes":"` + tc.notes + `"}`
			w := f.do("POST", base, body, nil)
			if w.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s request bytes=%d", w.Code, w.Body, len(body))
			}
			var response Item
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			row, ok := f.repo.Row(response.Id)
			if !ok || row.Title != tc.wantTitle || row.Notes == nil || *row.Notes != tc.wantNotes || utf8.RuneCountInString(row.Title) != 200 || utf8.RuneCountInString(*row.Notes) != 4000 {
				t.Fatalf("max fields not persisted: ok=%v titleRunes=%d notesRunes=%d", ok, utf8.RuneCountInString(row.Title), func() int {
					if row.Notes == nil {
						return 0
					}
					return utf8.RuneCountInString(*row.Notes)
				}())
			}
		})
	}
	f := newFixture(true)
	body := `{"title":"x","subjectId":"` + sid + `","notes":"` + strings.Repeat("x", bodyLimit) + `"}`
	if w := f.do("POST", base, body, nil); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status=%d body=%s", w.Code, w.Body)
	}
}

func TestHistoricalCreateForeignSubjectAndNonMemberLeaveNoRows(t *testing.T) {
	policy := `"recurrence":{"intervalValue":1,"intervalUnit":"day","mode":"fixed"}`
	for _, tc := range []struct {
		name, household, subject string
		member                   error
		wantStatus               int
		wantCode                 string
	}{
		{"unknown subject", hid, other, nil, 422, "invalid_reference"},
		{"nonmember", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62", sid, item.ErrNotFound, 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			f.member = tc.member
			path := "/api/v1/households/" + tc.household + "/items"
			body := `{"title":"x","subjectId":"` + tc.subject + `","historicalCompletedOn":"2026-03-01",` + policy + `}`
			w := f.do("POST", path, body, nil)
			if w.Code != tc.wantStatus || !strings.Contains(w.Body.String(), `"code":"`+tc.wantCode+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if f.repo.Count() != 1 || f.repo.CompletionCount() != 0 {
				t.Fatalf("rows=%d receipts=%d", f.repo.Count(), f.repo.CompletionCount())
			}
		})
	}
}

func TestHistoricalCreateValidationAndStrictDecoding(t *testing.T) {
	policy := `"recurrence":{"intervalValue":1,"intervalUnit":"day","mode":"fixed"}`
	for _, tc := range []struct {
		name, body, field, code string
		status                  int
	}{
		{"bad date", `"historicalCompletedOn":"2026-02-30",` + policy, "historicalCompletedOn", "invalid_date", 422},
		{"impossible date", `"historicalCompletedOn":"2025-02-30",` + policy, "historicalCompletedOn", "invalid_date", 422},
		{"date beats recurrence requirement", `"historicalCompletedOn":"2025-02-30"`, "historicalCompletedOn", "invalid_date", 422},
		{"conflict beats future date", `"historicalCompletedOn":"2026-03-03","attentionOn":"2026-03-04",` + policy, "historicalCompletedOn", "conflicting_fields", 422},
		{"requires recurrence omitted", `"historicalCompletedOn":"2026-03-01"`, "historicalCompletedOn", "requires_recurrence", 422},
		{"requires recurrence null", `"historicalCompletedOn":"2026-03-01","recurrence":null`, "historicalCompletedOn", "requires_recurrence", 422},
		{"subject reference after historical checks", `"historicalCompletedOn":"2026-03-01",` + policy, "subjectId", "invalid_reference", 422},
		{"archived reference after historical checks", `"historicalCompletedOn":"2026-03-01",` + policy, "subjectId", "invalid_reference", 422},
		{"date overflow", `"historicalCompletedOn":"9999-12-31","recurrence":{"intervalValue":1,"intervalUnit":"year","mode":"fixed"}`, "recurrence", "date_overflow", 422},
		{"conflicting attention", `"historicalCompletedOn":"2026-03-01","attentionOn":"2026-03-01",` + policy, "historicalCompletedOn", "conflicting_fields", 422},
		{"future local date", `"historicalCompletedOn":"2026-03-03",` + policy, "historicalCompletedOn", "future_date", 422},
		{"future at midnight boundary", `"historicalCompletedOn":"2026-03-02",` + policy, "historicalCompletedOn", "future_date", 422},
		{"today at midnight boundary accepted", `"historicalCompletedOn":"2026-03-01",` + policy, "", "", 201},
		{"ordinary create", `"attentionOn":null`, "", "", 201},
		{"null attention accepted", `"historicalCompletedOn":"2026-03-01","attentionOn":null,` + policy, "", "", 201},
		{"null", `"historicalCompletedOn":null`, "", "invalid_request", 400},
		{"wrong type", `"historicalCompletedOn":4`, "", "invalid_request", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			subject := sid
			if tc.name == "subject reference after historical checks" {
				subject = other
			}
			if tc.name == "archived reference after historical checks" {
				subject = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
			}
			body := `{"title":"x","subjectId":"` + subject + `",` + tc.body + `}`
			before := f.repo.Count()
			if tc.name == "future at midnight boundary" || tc.name == "today at midnight boundary accepted" {
				f.zone = "America/New_York"
				f.now = time.Date(2026, 3, 2, 4, 59, 59, 0, time.UTC)
			}
			if tc.name == "date overflow" {
				f.now = time.Date(9999, 12, 31, 12, 0, 0, 0, time.UTC)
			}
			if tc.name == "conflict beats future date" {
				f.now = time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
			}
			w := f.do("POST", base, body, nil)
			if w.Code != tc.status || (tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`)) || (tc.field != "" && !strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`)) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			wantCount := before
			if tc.status == 201 {
				wantCount++
			}
			wantReceipts := 0
			if tc.status == 201 && tc.name != "ordinary create" {
				wantReceipts = 1
			}
			if f.repo.Count() != wantCount || f.repo.CompletionCount() != wantReceipts {
				t.Fatalf("request stored item count %d (want %d), receipt count %d (want %d)", f.repo.Count(), wantCount, f.repo.CompletionCount(), wantReceipts)
			}
		})
	}
}

func TestHistoricalCreatePersistsReceiptResponseAndStrictField(t *testing.T) {
	f := newFixture(true)
	body := `{"title":"Historical","subjectId":"` + sid + `","historicalCompletedOn":"2026-03-01","recurrence":{"intervalValue":1,"intervalUnit":"week","mode":"fixed"}}`
	w := f.do("POST", base, body, nil)
	if w.Code != 201 || w.Header().Get("ETag") != `"2"` || w.Header().Get("Location") == "" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["attentionOn"] != "2026-03-08" || got["lastCompletedOn"] != "2026-03-01" {
		t.Fatalf("body=%v", got)
	}
	id := got["id"].(string)
	row, ok := f.repo.Row(id)
	if !ok || row.Version != 2 || row.AttentionOn == nil || row.AttentionOn.String() != "2026-03-08" {
		t.Fatalf("row=%+v exists=%v", row, ok)
	}
	historyResponse := f.do("GET", base+"/"+id+"/completions", "", nil)
	if historyResponse.Code != 200 {
		t.Fatalf("history status=%d body=%s", historyResponse.Code, historyResponse.Body)
	}
	var historyBody map[string]any
	if err := json.Unmarshal(historyResponse.Body.Bytes(), &historyBody); err != nil {
		t.Fatal(err)
	}
	historyItems, ok := historyBody["items"].([]any)
	if !ok || len(historyItems) != 1 {
		t.Fatalf("history body=%v", historyBody)
	}
	receipt, ok := historyItems[0].(map[string]any)
	if !ok || receipt["completedOn"] != "2026-03-01" || receipt["cycleAttentionOn"] != nil || receipt["nextAttentionOn"] != "2026-03-08" || receipt["undoneAt"] != nil || receipt["recurrence"].(map[string]any)["intervalUnit"] != "week" {
		t.Fatalf("receipt=%v", historyItems[0])
	}
	patch := f.do("PATCH", base+"/"+id, `{"historicalCompletedOn":"2026-03-01"}`, map[string]string{"If-Match": `"2"`})
	if patch.Code != 400 || !strings.Contains(patch.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("patch=%d %s", patch.Code, patch.Body)
	}
}

func TestStrictBodiesValidationAndNullSemantics(t *testing.T) {
	f := newFixture(true)
	original := stored(t, f)
	for name, body := range map[string]string{"empty": `{}`, "unknown": `{"title":"x","subjectId":"` + sid + `","extra":1}`, "duplicate": `{"title":"x","title":"y","subjectId":"` + sid + `"}`, "wrong": `{"title":1,"subjectId":"` + sid + `"}`, "null subject": `{"title":"x","subjectId":null}`, "null title": `{"title":null,"subjectId":"` + sid + `"}`} {
		w := f.do("POST", base, body, nil)
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	for name, body := range map[string]string{"empty": `{}`, "null title": `{"title":null}`, "null subject": `{"subjectId":null}`, "bad attention null number": `{"attentionOn":1}`, "bad notes": `{"notes":[]}`} {
		w := f.do("PATCH", one, body, map[string]string{"If-Match": `"1"`})
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ body, field, code string }{
		{`{"title":"  ","subjectId":"` + sid + `"}`, "title", "invalid_length"},
		{`{"title":"a\u0000b","subjectId":"` + sid + `"}`, "title", "invalid_characters"},
		{`{"title":"x","subjectId":"` + sid + `","notes":""}`, "notes", "invalid_length"},
		{`{"title":"x","subjectId":"` + sid + `","attentionOn":"2026-02-30"}`, "attentionOn", "invalid_date"},
		{`{"title":"x","subjectId":"` + other + `"}`, "subjectId", "invalid_reference"},
		{`{"title":"x","subjectId":"not-a-uuid"}`, "subjectId", "invalid_reference"},
	} {
		w := f.do("POST", base, tc.body, nil)
		want := `{"type":"about:blank","title":"Validation Failed","status":422,"code":"` + tc.code + `","field":"` + tc.field + `"}`
		if w.Code != 422 {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body)
		}
		bodyEqual(t, w.Body.String(), want)
	}
	for _, tc := range []struct{ body, field, code string }{
		{`{"workflowState":"done"}`, "workflowState", "invalid_workflow_state"},
		{`{"done":true}`, "done", "invalid_request"},
		{`{"lastCompletedOn":"2026-01-01"}`, "lastCompletedOn", "invalid_request"},
		{`{"title":" "}`, "title", "invalid_length"},
		{`{"attentionOn":"2026-02-30"}`, "attentionOn", "invalid_date"},
	} {
		w := f.do("PATCH", one, tc.body, map[string]string{"If-Match": `"1"`})
		wantStatus := 422
		if tc.code == "invalid_request" {
			wantStatus = 400
		}
		if w.Code != wantStatus || (wantStatus == 422 && (!strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`) || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`))) || (wantStatus == 400 && !strings.Contains(w.Body.String(), `"code":"invalid_request"`)) {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body)
		}
	}
	if got := stored(t, f); got != original {
		t.Fatalf("invalid bodies changed state: %+v", got)
	}
}

func TestPreconditionEvaluationOrderAndState(t *testing.T) {
	f := newFixture(true)
	original := stored(t, f)
	if w := f.do("PATCH", one, `{`, nil); w.Code != 428 {
		t.Fatalf("missing: %d", w.Code)
	}
	for _, bad := range []string{`W/"1"`, `*`, `"1", "2"`, `"0"`, `"01"`, `1`} {
		if w := f.do("PATCH", one, `{"title":"x"}`, map[string]string{"If-Match": bad}); w.Code != 412 {
			t.Fatalf("If-Match %s => %d", bad, w.Code)
		}
	}
	if w := f.do("PATCH", one, `{"title":"x"}`, map[string]string{"If-Match": `"99"`}); w.Code != 412 {
		t.Fatalf("stale: %d %s", w.Code, w.Body)
	}
	if got := stored(t, f); got != original {
		t.Fatalf("preconditions changed row: %+v", got)
	}
	if w := f.do("PATCH", one, `{}`, map[string]string{"If-Match": `"99"`}); w.Code != 400 {
		t.Fatalf("malformed body precedence=%d", w.Code)
	}
	if w := f.do("PATCH", one, `{"title":" "}`, map[string]string{"If-Match": `"99"`}); w.Code != 422 {
		t.Fatalf("validation precedence=%d", w.Code)
	}
}

func TestBodyLimitsMediaTypeAndQueryProblems(t *testing.T) {
	f := newFixture(true)
	for _, tc := range []struct {
		contentType string
		size        int
		want        int
	}{{"text/plain", 1, 415}, {"application/json", bodyLimit + 1, 413}} {
		body := `{"title":"` + strings.Repeat("x", tc.size) + `","subjectId":"` + sid + `"}`
		w := f.do("POST", base, body, map[string]string{"Content-Type": tc.contentType})
		if w.Code != tc.want {
			t.Fatalf("got %d want %d", w.Code, tc.want)
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "cursor=", "archived=yes"} {
		w := f.do("GET", base+"?"+query, "", nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_query"`) {
			t.Fatalf("%s => %d %s", query, w.Code, w.Body)
		}
	}
}

func TestNotFoundUnavailableAndAuthenticationAreUniform(t *testing.T) {
	f := newFixture(true)
	want404 := problem(404, "not_found")
	for _, path := range []string{base + "/" + other, "/api/v1/households/" + other + "/items/" + iid, base + "/nope"} {
		w := f.do("GET", path, "", nil)
		if w.Code != 404 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		bodyEqual(t, w.Body.String(), want404)
	}
	f.member = errors.New("database secret")
	w := f.do("GET", one, "", nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("unavailable %d %s", w.Code, w.Body)
	}
	unauth := newFixture(false)
	w = unauth.do("GET", one, "", nil)
	if w.Code != 401 || unauth.svcHit != 0 || unauth.repo.Calls != 0 {
		t.Fatalf("auth %d svc=%d repo=%d", w.Code, unauth.svcHit, unauth.repo.Calls)
	}
}

func TestCreateAndUpdatePrecedenceForNonMembers(t *testing.T) {
	f := newFixture(true)
	foreign := "/api/v1/households/" + other + "/items/" + iid
	if w := f.do("PATCH", foreign, `{"title":"x"}`, nil); w.Code != 428 {
		t.Fatalf("missing If-Match precedes member: %d", w.Code)
	}
	if w := f.do("PATCH", foreign, `{}`, map[string]string{"If-Match": `"1"`}); w.Code != 400 {
		t.Fatalf("body precedes member: %d", w.Code)
	}
	if w := f.do("PATCH", foreign, `{"title":" "}`, map[string]string{"If-Match": `"1"`}); w.Code != 404 {
		t.Fatalf("member precedes validation: %d", w.Code)
	}
	if w := f.do("PATCH", one, `{"title":"ok"}`, map[string]string{"If-Match": `"1"`}); w.Code != 200 {
		t.Fatalf("valid patch: %d %s", w.Code, w.Body)
	}
}
