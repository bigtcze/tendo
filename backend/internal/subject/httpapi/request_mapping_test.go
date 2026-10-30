package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/go-chi/chi/v5"
)

type recordingService struct {
	createKey, createName         string
	createType                    subject.Type
	createLink                    *string
	patch                         subject.Patch
	list                          subject.ListQuery
	calls                         int
	createErr, updateErr, listErr error
}

func (s *recordingService) Create(_ context.Context, _, _, key, name string, typ subject.Type, link *string) (subject.Subject, error) {
	s.calls++
	s.createKey, s.createName, s.createType, s.createLink = key, name, typ, link
	return subject.Subject{ID: subjectID, Version: 2}, s.createErr
}
func (s *recordingService) Get(context.Context, string, string, string) (subject.Subject, error) {
	s.calls++
	return subject.Subject{}, nil
}
func (s *recordingService) List(_ context.Context, _, _ string, q subject.ListQuery) (subject.Page, error) {
	s.calls++
	s.list = q
	return subject.Page{Items: []subject.Subject{}}, s.listErr
}
func (s *recordingService) Update(_ context.Context, _, _, _ string, _ int64, p subject.Patch) (subject.Subject, error) {
	s.calls++
	s.patch = p
	return subject.Subject{Version: 3}, s.updateErr
}

func requestHandler(s *recordingService) http.Handler {
	r := chi.NewRouter()
	New(s, func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) { return callerID, true }).Register(r)
	return r
}
func invoke(s *recordingService, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	requestHandler(s).ServeHTTP(w, req)
	return w
}

func TestCreateRequestMappingAndHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       *string
	}{
		{"omitted", `{"name":"N","type":"person"}`, nil},
		{"null", `{"name":"N","type":"person","linkedUserId":null}`, nil},
		{"value", `{"name":"N","type":"person","linkedUserId":"user"}`, ptrHTTP("user")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingService{}
			w := invoke(s, "POST", base, tc.body, map[string]string{"Idempotency-Key": "key"})
			if w.Code != http.StatusCreated || s.calls != 1 || s.createKey != "key" || s.createName != "N" || s.createType != subject.TypePerson || !reflect.DeepEqual(s.createLink, tc.want) {
				t.Fatalf("status=%d calls=%d service=%+v", w.Code, s.calls, s)
			}
			if w.Header().Get("Location") != base+"/"+subjectID || w.Header().Get("ETag") != `"2"` {
				t.Fatalf("headers=%v", w.Header())
			}
		})
	}
}

func TestPatchRequestMappingOmittedNullAndValue(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		set        bool
		want       *string
	}{
		{"omitted", `{"archived":false}`, false, nil}, {"null", `{"linkedUserId":null}`, true, nil}, {"value", `{"linkedUserId":"user"}`, true, ptrHTTP("user")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingService{}
			w := invoke(s, "PATCH", one, tc.body, map[string]string{"If-Match": `"1"`})
			if w.Code != http.StatusOK || s.calls != 1 || s.patch.LinkedUserIDSet != tc.set || !reflect.DeepEqual(s.patch.LinkedUserID, tc.want) {
				t.Fatalf("status=%d calls=%d patch=%+v", w.Code, s.calls, s.patch)
			}
		})
	}
}

func TestListRequestMappingPreservesNilAndRepeatedIDs(t *testing.T) {
	for _, tc := range []struct {
		query string
		ids   []string
	}{
		{"?archived=true&limit=7&cursor=c", nil}, {"?id=b&id=a&id=b&limit=8", []string{"b", "a", "b"}},
	} {
		s := &recordingService{}
		w := invoke(s, "GET", base+tc.query, "", nil)
		if w.Code != 200 || s.calls != 1 || !reflect.DeepEqual(s.list.IDs, tc.ids) {
			t.Fatalf("query=%s status=%d calls=%d got=%+v", tc.query, w.Code, s.calls, s.list)
		}
		if tc.ids == nil && (!s.list.Archived || s.list.Limit != 7 || s.list.Cursor != "c") {
			t.Fatalf("list=%+v", s.list)
		}
		if tc.ids != nil && (s.list.Limit != 8 || s.list.Archived) {
			t.Fatalf("list=%+v", s.list)
		}
	}
}

func TestListInvalidIDQueryDoesNotCallService(t *testing.T) {
	for _, query := range []string{"?id=", "?" + strings.Repeat("id=x&", 100) + "id=x"} {
		s := &recordingService{}
		w := invoke(s, "GET", base+query, "", nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_query"`) || !strings.Contains(w.Body.String(), `"parameter":"id"`) || s.calls != 0 {
			t.Fatalf("query=%s status=%d body=%s calls=%d", query, w.Code, w.Body, s.calls)
		}
	}
}

func TestWriteServiceErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		err                    error
		status                 int
		code, field, parameter string
	}{
		{"idempotency", subject.ErrIdempotencyKeyReused, 422, "idempotency_key_reused", "", ""},
		{"not found", subject.ErrNotFound, 404, "not_found", "", ""}, {"empty patch", subject.ErrEmptyPatch, 400, "invalid_request", "", ""},
		{"version", subject.ErrVersionMismatch, 412, "precondition_failed", "", ""},
		{"validation", &subject.ValidationError{Field: "name", Code: "invalid_length"}, 422, "invalid_length", "name", ""},
		{"query", &subject.InvalidQueryError{Parameter: "id"}, 400, "invalid_query", "", "id"},
		{"unavailable", errors.New("private"), 503, "unavailable", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeServiceError(w, tc.err)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) || tc.field != "" && !strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`) || tc.parameter != "" && !strings.Contains(w.Body.String(), `"parameter":"`+tc.parameter+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}
func ptrHTTP(value string) *string { return &value }
