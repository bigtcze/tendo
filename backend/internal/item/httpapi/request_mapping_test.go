package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/go-chi/chi/v5"
)

// recordingService captures the exact values the transport passes to the
// item service, so request decoding is pinned independently of domain rules.
type recordingService struct {
	calls   int
	key     string
	created item.NewItem
	patch   item.Patch
	version int64
	list    item.ListQuery
	summary item.SummaryQuery
	err     error
}

func (s *recordingService) Create(_ context.Context, _, _, key string, n item.NewItem) (item.Item, error) {
	s.calls++
	s.key, s.created = key, n
	return item.Item{ID: iid, Version: 1}, s.err
}
func (s *recordingService) Get(context.Context, string, string, string) (item.Item, error) {
	s.calls++
	return item.Item{}, s.err
}
func (s *recordingService) List(_ context.Context, _, _ string, q item.ListQuery) (item.Page, error) {
	s.calls++
	s.list = q
	return item.Page{}, s.err
}
func (s *recordingService) Summary(_ context.Context, _, _ string, q item.SummaryQuery) (item.Summary, error) {
	s.calls++
	s.summary = q
	return item.Summary{}, s.err
}
func (s *recordingService) Update(_ context.Context, _, _, _ string, version int64, p item.Patch) (item.Item, error) {
	s.calls++
	s.version, s.patch = version, p
	return item.Item{ID: iid, Version: version + 1}, s.err
}
func (s *recordingService) Complete(context.Context, string, string, string, int64, string, item.CompletionRequest) (item.Completion, bool, error) {
	s.calls++
	return item.Completion{}, false, s.err
}
func (s *recordingService) UndoCompletion(context.Context, string, string, string, string, int64) (item.Completion, error) {
	s.calls++
	return item.Completion{}, s.err
}
func (s *recordingService) ListCompletions(context.Context, string, string, string, int, string, string) (item.CompletionPage, error) {
	s.calls++
	return item.CompletionPage{}, s.err
}

func recordingRouter(s *recordingService) http.Handler {
	r := chi.NewRouter()
	pass := func(next http.Handler) http.Handler { return next }
	New(s, pass, func(context.Context) (string, bool) { return userID, true }).Register(r)
	return r
}

func serveRecorded(s *recordingService, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	recordingRouter(s).ServeHTTP(w, req)
	return w
}

func TestListQueryMapsEveryParameterToService(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		query string
		want  item.ListQuery
	}{
		{"", item.ListQuery{}},
		{"limit=7&cursor=abc&archived=true&done=true", item.ListQuery{Limit: 7, Cursor: "abc", Archived: true, Done: true}},
		{"archived=false&done=false", item.ListQuery{}},
		{"subjectId=S&responsibleUserId=unassigned&workflowState=W&attention=A", item.ListQuery{SubjectID: "S", ResponsibleUserID: "unassigned", WorkflowState: "W", Attention: "A"}},
		{"recurring=true", item.ListQuery{Recurring: &yes}},
		{"recurring=false", item.ListQuery{Recurring: &no}},
		{"sort=anything&attentionDate=D&q=%20term%20", item.ListQuery{Sort: "anything", AttentionDate: "D", Q: " term "}},
		{"futureFlag=x", item.ListQuery{}},
	} {
		s := &recordingService{}
		w := serveRecorded(s, http.MethodGet, base+"?"+tc.query, "", nil)
		if w.Code != http.StatusOK || s.calls != 1 {
			t.Fatalf("%q: status=%d calls=%d body=%s", tc.query, w.Code, s.calls, w.Body)
		}
		if !reflect.DeepEqual(s.list, tc.want) {
			t.Errorf("%q: got %+v want %+v", tc.query, s.list, tc.want)
		}
		bodyEqual(t, w.Body.String(), `{"items":[],"nextCursor":null}`)
	}
}

// Each case puts two invalid parameters in reverse of their evaluation order;
// the problem must name the parameter that is checked first.
func TestListQueryFirstOffenderOrder(t *testing.T) {
	order := []string{"limit=0", "cursor=", "archived=x", "done=x", "subjectId=", "responsibleUserId=", "workflowState=", "attention=", "recurring=1", "sort=", "attentionDate=", "q="}
	for i := 0; i < len(order)-1; i++ {
		for _, later := range order[i+1:] {
			query := later + "&" + order[i]
			want := strings.SplitN(order[i], "=", 2)[0]
			s := &recordingService{}
			w := serveRecorded(s, http.MethodGet, base+"?"+query, "", nil)
			assertQueryProblem(t, w, want)
			if s.calls != 0 {
				t.Fatalf("%q reached service", query)
			}
		}
	}
}

func TestSummaryQueryMapsParametersAndFirstOffenderOrder(t *testing.T) {
	yes := true
	s := &recordingService{}
	w := serveRecorded(s, http.MethodGet, base+"/summary?subjectId=S&responsibleUserId=R&workflowState=W&attention=A&recurring=true&attentionDate=D&q=Q&futureFlag=x", "", nil)
	want := item.SummaryQuery{SubjectID: "S", ResponsibleUserID: "R", WorkflowState: "W", Attention: "A", Recurring: &yes, AttentionDate: "D", Q: "Q"}
	if w.Code != http.StatusOK || !reflect.DeepEqual(s.summary, want) {
		t.Fatalf("status=%d got %+v want %+v body=%s", w.Code, s.summary, want, w.Body)
	}
	bodyEqual(t, w.Body.String(), `{"evaluatedOn":"","total":0,"needsAttention":0,"inProgress":0,"waiting":0,"upcoming":0,"paused":0,"done":0,"archived":0}`)
	order := []string{"limit=5", "cursor=c", "archived=false", "done=false", "sort=created", "subjectId=", "responsibleUserId=", "workflowState=", "attention=", "recurring=1", "attentionDate=", "q="}
	for i := 0; i < len(order)-1; i++ {
		for _, later := range order[i+1:] {
			query := later + "&" + order[i]
			s := &recordingService{}
			w := serveRecorded(s, http.MethodGet, base+"/summary?"+query, "", nil)
			assertQueryProblem(t, w, strings.SplitN(order[i], "=", 2)[0])
			if s.calls != 0 {
				t.Fatalf("%q reached service", query)
			}
		}
	}
}

func TestCreateBodyMapsEveryFieldToService(t *testing.T) {
	str := func(v string) *string { return &v }
	for _, tc := range []struct {
		name, body string
		want       item.NewItem
	}{
		{"required only", `{"title":"T","subjectId":"S"}`, item.NewItem{Title: "T", SubjectID: "S"}},
		{"explicit nulls", `{"title":"T","subjectId":"S","responsibleUserId":null,"notes":null,"attentionOn":null,"recurrence":null}`, item.NewItem{Title: "T", SubjectID: "S"}},
		{"all values", `{"title":" T ","subjectId":"S","responsibleUserId":"R","notes":" n ","attentionOn":"2026-03-02","historicalCompletedOn":"2026-01-01","recurrence":{"intervalValue":999,"intervalUnit":"week","mode":"after_completion"}}`,
			item.NewItem{Title: " T ", SubjectID: "S", ResponsibleUserID: str("R"), Notes: str(" n "), AttentionOn: str("2026-03-02"), HistoricalCompletedOn: str("2026-01-01"),
				Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 999, Unit: "week"}, Mode: "after_completion"}}},
		{"empty strings kept", `{"title":"","subjectId":"","notes":"","attentionOn":"","historicalCompletedOn":""}`, item.NewItem{Notes: str(""), AttentionOn: str(""), HistoricalCompletedOn: str("")}},
		{"interval below range", `{"title":"T","subjectId":"S","recurrence":{"intervalValue":0,"intervalUnit":"x","mode":"y"}}`,
			item.NewItem{Title: "T", SubjectID: "S", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 0, Unit: "x"}, Mode: "y"}}},
		{"interval above range", `{"title":"T","subjectId":"S","recurrence":{"intervalValue":1000,"intervalUnit":"day","mode":"fixed"}}`,
			item.NewItem{Title: "T", SubjectID: "S", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 0, Unit: "day"}, Mode: "fixed"}}},
		{"interval lower bound", `{"title":"T","subjectId":"S","recurrence":{"intervalValue":1,"intervalUnit":"day","mode":"fixed"}}`,
			item.NewItem{Title: "T", SubjectID: "S", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: "day"}, Mode: "fixed"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingService{}
			w := serveRecorded(s, http.MethodPost, "/api/v1/households/"+strings.ToUpper(hid)+"/items", tc.body, map[string]string{"Idempotency-Key": "k-1"})
			if w.Code != http.StatusCreated || s.calls != 1 || s.key != "k-1" {
				t.Fatalf("status=%d calls=%d key=%q body=%s", w.Code, s.calls, s.key, w.Body)
			}
			if !reflect.DeepEqual(s.created, tc.want) {
				t.Errorf("got %+v want %+v", s.created, tc.want)
			}
			if w.Header().Get("Location") != one || w.Header().Get("ETag") != `"1"` {
				t.Errorf("headers=%v", w.Header())
			}
		})
	}
}

func TestPatchBodyMapsPresenceAndNullToService(t *testing.T) {
	str := func(v string) *string { return &v }
	yes := true
	weekly := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: "week"}, Mode: "fixed"}
	for _, tc := range []struct {
		name, body string
		want       item.Patch
	}{
		{"nulls", `{"responsibleUserId":null,"notes":null,"attentionOn":null,"recurrence":null}`,
			item.Patch{ResponsibleUserID: item.Null[string](), Notes: item.Null[string](), AttentionOn: item.Null[string](), Recurrence: item.Null[schedule.Policy]()}},
		{"values", `{"title":"T","subjectId":"S","responsibleUserId":"R","notes":"","attentionOn":"D","recurrence":{"intervalValue":2,"intervalUnit":"week","mode":"fixed"},"workflowState":"W","archived":true}`,
			item.Patch{Title: str("T"), SubjectID: str("S"), ResponsibleUserID: item.Some("R"), Notes: item.Some(""), AttentionOn: item.Some("D"), Recurrence: item.Some(weekly), WorkflowState: str("W"), Archived: &yes}},
		{"archived false", `{"archived":false}`, item.Patch{Archived: new(bool)}},
		{"interval out of range", `{"recurrence":{"intervalValue":-1,"intervalUnit":"day","mode":"fixed"}}`,
			item.Patch{Recurrence: item.Some(schedule.Policy{Enabled: true, Interval: schedule.Interval{Unit: "day"}, Mode: "fixed"})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingService{}
			w := serveRecorded(s, http.MethodPatch, one, tc.body, map[string]string{"If-Match": `"4"`})
			if w.Code != http.StatusOK || s.calls != 1 || s.version != 4 {
				t.Fatalf("status=%d calls=%d version=%d body=%s", w.Code, s.calls, s.version, w.Body)
			}
			if !reflect.DeepEqual(s.patch, tc.want) {
				t.Errorf("got %+v want %+v", s.patch, tc.want)
			}
			if w.Header().Get("ETag") != `"5"` {
				t.Errorf("ETag=%q", w.Header().Get("ETag"))
			}
		})
	}
}

func TestPatchEmptyObjectRejectedBeforeService(t *testing.T) {
	s := &recordingService{}
	w := serveRecorded(s, http.MethodPatch, one, `{}`, map[string]string{"If-Match": `"4"`})
	if w.Code != http.StatusBadRequest || s.calls != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, s.calls)
	}
	bodyEqual(t, w.Body.String(), problem(400, "invalid_request"))
}

func TestItemServiceErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{
		{item.ErrIdempotencyKeyReused, 422, problem(422, "idempotency_key_reused")},
		{item.ErrNotFound, 404, problem(404, "not_found")},
		{item.ErrEmptyPatch, 400, problem(400, "invalid_request")},
		{item.ErrVersionMismatch, 412, problem(412, "precondition_failed")},
		{&item.ValidationError{Field: "title", Code: "required"}, 422, `{"type":"about:blank","title":"Validation Failed","status":422,"code":"required","field":"title"}`},
		{&item.InvalidQueryError{Parameter: "cursor"}, 400, `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_query","parameter":"cursor"}`},
		{item.ErrArchived, 503, problem(503, "unavailable")},
		{context.Canceled, 503, problem(503, "unavailable")},
	} {
		for _, req := range []struct{ method, path, body string }{
			{http.MethodGet, base, ""}, {http.MethodGet, base + "/summary", ""}, {http.MethodGet, one, ""},
			{http.MethodPost, base, `{"title":"T","subjectId":"S"}`}, {http.MethodPatch, one, `{"title":"T"}`},
		} {
			s := &recordingService{err: tc.err}
			w := serveRecorded(s, req.method, req.path, req.body, map[string]string{"Idempotency-Key": "k", "If-Match": `"1"`})
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("ETag") != "" || w.Header().Get("Location") != "" {
				t.Fatalf("%v %s %s: status=%d headers=%v body=%s", tc.err, req.method, req.path, w.Code, w.Header(), w.Body)
			}
			bodyEqual(t, w.Body.String(), tc.body)
		}
	}
}
