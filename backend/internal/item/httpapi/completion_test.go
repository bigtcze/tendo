package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/itemtest"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/go-chi/chi/v5"
)

func TestCompletionHTTPCreateReplayAndEvaluationOrder(t *testing.T) {
	f := newFixture(true)
	iid2 := other
	f.repo.Seed(item.Item{ID: iid2, HouseholdID: hid, SubjectID: sid, Title: "One", WorkflowState: item.StateOpen, Version: 1, CreatedAt: at, UpdatedAt: at})
	r := chi.NewRouter()
	svc := item.NewService(f.repo, func(_ context.Context, u, h string) (string, error) {
		if f.member != nil {
			return "", f.member
		}
		if h != hid {
			return "", item.ErrNotFound
		}
		return "UTC", nil
	}, func(context.Context, string, string, string) (bool, error) { return false, nil }, func() time.Time { return at })
	New(svc, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, userID)))
		})
	}, func(ctx context.Context) (string, bool) { v, ok := ctx.Value(userKey{}).(string); return v, ok }).Register(r)
	path := base + "/" + iid2 + "/completions"
	notFoundPath := base + "/0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98/completions"
	req := func(headers map[string]string, body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest("POST", path, strings.NewReader(body))
		if _, ok := headers["Content-Type"]; !ok && !(headers["If-Match"] == `"1"` && headers["Idempotency-Key"] == "x") {
			q.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			q.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	tests := []struct {
		name string
		h    map[string]string
		b    string
		want int
		code string
	}{{"missing match", map[string]string{"Idempotency-Key": "x"}, "", 428, "precondition_required"}, {"key before media", map[string]string{"If-Match": "\"1\""}, "", 400, "idempotency_key_required"}, {"invalid key before media", map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "bad key"}, "", 400, "invalid_idempotency_key"}, {"media", map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "x"}, "", 415, "unsupported_media_type"}, {"body", map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "x", "Content-Type": "application/json"}, `{"unknown":1}`, 400, "invalid_request"}, {"not found", map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "x", "Content-Type": "application/json"}, `{}`, 404, "not_found"}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "not found" {
				q := httptest.NewRequest("POST", notFoundPath, strings.NewReader(tc.b))
				q.Header.Set("Content-Type", "application/json")
				for k, v := range tc.h {
					q.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, q)
				if rec.Code != tc.want {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
				}
				return
			}
			w := req(tc.h, tc.b)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("body=%s", w.Body)
			}
		})
	}
	h := map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "receipt", "Content-Type": "application/json"}
	w := req(h, `{}`)
	if w.Code != 201 || w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Location") != "/api/v1/households/"+hid+"/items/"+iid2+"/completions/"+"0198a2f0-7c1e-7a53-9b0e-000000000001" {
		t.Fatalf("status=%d header=%v body=%s", w.Code, w.Header(), w.Body)
	}
	row, _ := f.repo.Row(iid2)
	if !row.Done || row.Version != 2 {
		t.Fatalf("stored=%+v", row)
	}
	again := req(h, `{}`)
	if again.Code != 201 || again.Body.String() != w.Body.String() {
		t.Fatalf("replay=%d %s want %s", again.Code, again.Body, w.Body)
	}
	row, _ = f.repo.Row(iid2)
	if row.Version != 2 {
		t.Fatalf("replay version=%d", row.Version)
	}
}
func TestUndoCompletionHTTP(t *testing.T) {
	f := newFixture(true)
	var staleSession bool
	svc := item.NewService(f.repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { return false, nil }, func() time.Time { return at })
	r := chi.NewRouter()
	New(svc, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := userID
			if staleSession {
				uid = ""
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, uid)))
		})
	}, func(ctx context.Context) (string, bool) {
		v, ok := ctx.Value(userKey{}).(string)
		return v, ok && v != ""
	}).Register(r)
	f.repo.Seed(item.Item{ID: other, HouseholdID: hid, SubjectID: sid, WorkflowState: item.StateWaiting, Version: 1, AttentionOn: itemtest.Date(2026, time.September, 1)})
	on := "2026-02-28"
	receipt, _, err := svc.Complete(context.Background(), userID, hid, other, 1, "http-undo", item.CompletionRequest{CompletedOn: &on})
	if err != nil {
		t.Fatal(err)
	}
	path := base + "/" + other + "/completions/" + receipt.ID
	missingPath := base + "/" + other + "/completions/not-a-receipt"
	do := func(id, etag, body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest("PATCH", base+"/"+other+"/completions/"+id, strings.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		if etag != "" {
			q.Header.Set("If-Match", etag)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	if w := do(receipt.ID, "", `{"undone":true}`); w.Code != 428 {
		t.Fatalf("missing etag %d %s", w.Code, w.Body)
	}
	badBodies := []string{`{}`, `{"undone":null}`, `{"undone":"true"}`}
	for _, body := range badBodies {
		q := httptest.NewRequest("PATCH", missingPath, strings.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("If-Match", `"1"`)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("invalid body %s => %d %s", body, w.Code, w.Body)
		}
	}
	q := httptest.NewRequest("PATCH", missingPath, strings.NewReader(`{"undone":false}`))
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("If-Match", `"1"`)
	wfalse := httptest.NewRecorder()
	r.ServeHTTP(wfalse, q)
	if wfalse.Code != 422 || !strings.Contains(wfalse.Body.String(), `"field":"undone"`) {
		t.Fatalf("false before lookup=%d %s", wfalse.Code, wfalse.Body)
	}
	if w := do(receipt.ID, `"2"`, `{"undone":false}`); w.Code != 422 || !strings.Contains(w.Body.String(), `"field":"undone"`) {
		t.Fatalf("false %d %s", w.Code, w.Body)
	}
	if w := do(receipt.ID, `"2"`, `{"undone":true,"extra":1}`); w.Code != 400 {
		t.Fatalf("unknown %d %s", w.Code, w.Body)
	}

	w := do(receipt.ID, `"2"`, `{"undone":true}`)
	wFirstBody := w.Body.String()
	var exactReceipt map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &exactReceipt); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 {
		t.Fatalf("undo=%d %s", w.Code, w.Body)
	}
	wantReceipt := map[string]any{"id": receipt.ID, "itemId": other, "completedOn": "2026-02-28", "completedByUserId": userID, "cycleAttentionOn": "2026-09-01", "recurrence": nil, "nextAttentionOn": nil, "createdAt": receipt.CreatedAt.UTC().Format(time.RFC3339Nano), "undoneAt": at.UTC().Format(time.RFC3339Nano), "undoneByUserId": userID}
	if !reflect.DeepEqual(exactReceipt, wantReceipt) {
		t.Fatalf("receipt JSON=%#v want %#v", exactReceipt, wantReceipt)
	}
	if w.Code != 200 || w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"undoneAt":"2026-03-01T23:30:00Z"`) || !strings.Contains(w.Body.String(), `"undoneByUserId":"`+userID+`"`) {
		t.Fatalf("undo=%d %s", w.Code, w.Body)
	}
	if w := do(receipt.ID, "", `{"undone":true}`); w.Code != 428 {
		t.Fatalf("replay missing etag=%d %s", w.Code, w.Body)
	}
	if w := do(receipt.ID, `W/"2"`, `{"undone":true}`); w.Code != 412 {
		t.Fatalf("replay malformed etag=%d %s", w.Code, w.Body)
	}
	if w := do(receipt.ID, `"1"`, `{"undone":true}`); w.Code != 200 || w.Body.String() != wFirstBody {
		t.Fatalf("replay=%d %s want %s", w.Code, w.Body, wFirstBody)
	}
	rowAfterReplay, _ := f.repo.Row(other)
	if rowAfterReplay.Version != 3 || rowAfterReplay.AttentionOn == nil || rowAfterReplay.AttentionOn.String() != "2026-09-01" || rowAfterReplay.WorkflowState != item.StateWaiting || rowAfterReplay.Done {
		t.Fatalf("replay changed item=%+v", rowAfterReplay)
	}
	pageAfterReplay, err := svc.ListCompletions(context.Background(), userID, hid, other, 50, "")
	if err != nil || len(pageAfterReplay.Items) != 1 || pageAfterReplay.Items[0].ID != receipt.ID {
		t.Fatalf("history replay page=%+v err=%v", pageAfterReplay, err)
	}
	if w := do("bad", `"2"`, `{"undone":true}`); w.Code != 404 {
		t.Fatalf("malformed=%d %s", w.Code, w.Body)
	}
	staleSession = true
	if w := do(receipt.ID, `"2"`, `{"undone":true}`); w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("stale session undo=%d %s", w.Code, w.Body)
	}
	staleSession = false
	list := httptest.NewRecorder()
	r.ServeHTTP(list, httptest.NewRequest("GET", base+"/"+other+"/completions", nil))
	var listed CompletionList
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if list.Code != 200 || len(listed.Items) != 1 || listed.Items[0].Id != receipt.ID || listed.Items[0].UndoneAt == nil || listed.Items[0].UndoneByUserId == nil || *listed.Items[0].UndoneByUserId != userID {
		t.Fatalf("list=%d %s %s", list.Code, list.Body, path)
	}
}

func TestCompletionHTTPListPaginationAndDoneQuery(t *testing.T) {
	f := newFixture(true)
	svc := item.NewService(f.repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { return false, nil }, func() time.Time { return at })
	r := chi.NewRouter()
	New(svc, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, userID)))
		})
	}, func(ctx context.Context) (string, bool) { v, ok := ctx.Value(userKey{}).(string); return v, ok }).Register(r)
	f.handler = r
	f.repo.Seed(item.Item{ID: other, HouseholdID: hid, SubjectID: sid, Title: "recurring", WorkflowState: item.StateOpen, Version: 1})
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	if _, err := svc.Update(context.Background(), userID, hid, other, 1, item.Patch{Recurrence: item.Some(policy)}); err != nil {
		t.Fatal(err)
	}
	dates := []string{"2026-02-27", "2026-02-26", "2026-02-25"}
	ids := make([]string, 0, 3)
	version := int64(2)
	for n, dateText := range dates {
		on := dateText
		receipt, _, err := svc.Complete(context.Background(), userID, hid, other, version, fmt.Sprintf("history-%d", n), item.CompletionRequest{CompletedOn: &on})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, receipt.ID)
		version++
	}
	path := base + "/" + other + "/completions"
	first := f.do("GET", path+"?limit=2", "", nil)
	if first.Code != 200 {
		t.Fatalf("first=%d %s", first.Code, first.Body)
	}
	var page1 CompletionList
	if err := json.Unmarshal(first.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.Items[0].Id != ids[0] || page1.Items[1].Id != ids[1] || page1.NextCursor == nil || *page1.NextCursor != item.EncodeCompletionCursor(ids[1]) {
		t.Fatalf("page1=%+v want ids=%v", page1, ids)
	}
	second := f.do("GET", path+"?limit=2&cursor="+*page1.NextCursor, "", nil)
	var page2 CompletionList
	if second.Code != 200 {
		t.Fatalf("second=%d %s", second.Code, second.Body)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 || page2.Items[0].Id != ids[2] || page2.NextCursor != nil {
		t.Fatalf("page2=%+v", page2)
	}
	seen := map[string]bool{}
	for _, c := range append(page1.Items, page2.Items...) {
		if seen[c.Id] {
			t.Fatalf("duplicate receipt %s", c.Id)
		}
		seen[c.Id] = true
	}
	if len(seen) != 3 {
		t.Fatalf("seen=%v", seen)
	}
	empty := itemtest.New()
	emptyItem := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b88"
	empty.Seed(item.Item{ID: emptyItem, HouseholdID: hid, SubjectID: sid, Version: 1})
	emptySvc := item.NewService(empty, func(context.Context, string, string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { return false, nil }, func() time.Time { return at })
	emptyRouter := chi.NewRouter()
	New(emptySvc, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, userID)))
		})
	}, func(ctx context.Context) (string, bool) { v, ok := ctx.Value(userKey{}).(string); return v, ok }).Register(emptyRouter)
	req := httptest.NewRequest("GET", base+"/"+emptyItem+"/completions", nil)
	rec := httptest.NewRecorder()
	emptyRouter.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "{\"items\":[],\"nextCursor\":null}\n" {
		t.Fatalf("empty=%d %s", rec.Code, rec.Body)
	}
	missingEmpty := httptest.NewRecorder()
	emptyRouter.ServeHTTP(missingEmpty, httptest.NewRequest("GET", base+"/0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98/completions", nil))
	if missingEmpty.Code != 404 {
		t.Fatalf("empty repo missing item=%d %s", missingEmpty.Code, missingEmpty.Body)
	}
	missing := f.do("GET", base+"/"+"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98"+"/completions", "", nil)
	if missing.Code != 404 {
		t.Fatalf("missing=%d %s", missing.Code, missing.Body)
	}
	for _, tc := range []struct{ query, param string }{{"?cursor=bad", "cursor"}, {"?limit=0", "limit"}} {
		bad := f.do("GET", path+tc.query, "", nil)
		if bad.Code != 400 || !strings.Contains(bad.Body.String(), `"parameter":"`+tc.param+`"`) {
			t.Fatalf("bad query %s: %d %s", tc.query, bad.Code, bad.Body)
		}
	}
	doneID := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b87"
	f.repo.Seed(item.Item{ID: doneID, HouseholdID: hid, SubjectID: sid, Title: "done one-off", WorkflowState: item.StateOpen, Done: true, Version: 2})
	invalidDone := f.do("GET", base+"?done=bad", "", nil)
	if invalidDone.Code != 400 || !strings.Contains(invalidDone.Body.String(), `"parameter":"done"`) {
		t.Fatalf("invalid done query=%d %s", invalidDone.Code, invalidDone.Body)
	}
	done := f.do("GET", base+"?done=true", "", nil)
	if done.Code != 200 || !strings.Contains(done.Body.String(), doneID) || !strings.Contains(done.Body.String(), `"done":true`) {
		t.Fatalf("done=%d %s", done.Code, done.Body)
	}
}
