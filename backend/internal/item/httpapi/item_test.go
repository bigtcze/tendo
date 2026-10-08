package httpapi

import (
	"context"
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
	"github.com/go-chi/chi/v5"
)

const (
	userID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	hid    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	sid    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	iid    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80"
	other  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
)

var at = time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
var base = "/api/v1/households/" + hid + "/items"
var one = base + "/" + iid

type userKey struct{}
type fixture struct {
	repo    *itemtest.Repo
	handler http.Handler
	member  error
	subject error
	authHit int
	svcHit  int
	zone    string
	now     time.Time
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
	New(item.NewService(f.repo, houses, subjects, func() time.Time { return f.now }), auth, func(ctx context.Context) (string, bool) { v, ok := ctx.Value(userKey{}).(string); return v, ok }).Register(r)
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
	want := `{"id":"` + id + `","subjectId":"` + sid + `","title":"New item","notes":"  keep\n ","attentionOn":"2026-03-02","workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z","recurrence":null}`
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
	seeded := `{"id":"` + iid + `","subjectId":"` + sid + `","title":"Renew insurance","notes":"Quotes\n","attentionOn":"2026-03-02","workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-03-01T23:30:00Z","updatedAt":"2026-03-01T23:30:00Z","recurrence":null}`
	bodyEqual(t, list.Body.String(), `{"items":[`+want+`,`+seeded+`],"nextCursor":null}`)
	patched := f.do("PATCH", base+"/"+id, `{"notes":null,"attentionOn":null,"workflowState":"paused"}`, map[string]string{"If-Match": `"1"`})
	updated, exists := f.repo.Row(id)
	if !exists || patched.Code != 200 || patched.Header().Get("ETag") != `"2"` || updated.Version != 2 || updated.Notes != nil || updated.AttentionOn != nil || updated.WorkflowState != item.StatePaused {
		t.Fatalf("patch=%d %s row=%+v exists=%v", patched.Code, patched.Body, updated, exists)
	}
	bodyEqual(t, patched.Body.String(), `{"id":"`+id+`","subjectId":"`+sid+`","title":"New item","notes":null,"attentionOn":null,"recurrence":null,"workflowState":"paused","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
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
	bodyEqual(t, w.Body.String(), `{"id":"`+iid+`","subjectId":"`+sid+`","title":"Renew insurance","notes":"Quotes\n","attentionOn":"2026-03-02","recurrence":null,"workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-03-01T23:30:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
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
	fixedWant := `{"id":"` + fixedBody.Id + `","subjectId":"` + sid + `","title":"Fixed","notes":null,"attentionOn":"2099-01-01","recurrence":{"intervalValue":1,"intervalUnit":"year","mode":"fixed"},"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
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
	fluidWant := `{"id":"` + fluidBody.Id + `","subjectId":"` + sid + `","title":"Fluid","notes":null,"attentionOn":"2099-02-01","recurrence":{"intervalValue":2,"intervalUnit":"month","mode":"after_completion"},"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z"}`
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
	bodyEqual(t, clear.Body.String(), `{"id":"`+fixedBody.Id+`","subjectId":"`+sid+`","title":"Fixed","notes":null,"attentionOn":"2099-01-01","recurrence":null,"workflowState":"open","attention":"upcoming","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
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
		{"foreign subject", hid, other, nil, 422, "invalid_reference"},
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
