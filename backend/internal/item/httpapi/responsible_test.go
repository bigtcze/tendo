package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestResponsibleMemberHTTPExactRepresentationAssignmentAndFailures(t *testing.T) {
	f := newFixture(true)
	candidate := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	created := f.do("POST", base, `{"title":"Assigned","subjectId":"`+sid+`","responsibleUserId":"`+candidate+`"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body)
	}
	var body Item
	if err := decodeTestJSON(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ResponsibleUserId == nil || *body.ResponsibleUserId != candidate {
		t.Fatalf("body=%s", created.Body)
	}
	row, ok := f.repo.Row(body.Id)
	if !ok || row.ResponsibleUserID == nil || *row.ResponsibleUserID != candidate {
		t.Fatalf("stored=%+v", row.ResponsibleUserID)
	}
	otherCandidate := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b65"
	assigned := f.do("PATCH", base+"/"+body.Id, `{"responsibleUserId":"`+otherCandidate+`"}`, map[string]string{"If-Match": `"1"`})
	if assigned.Code != http.StatusOK || assigned.Header().Get("ETag") != `"2"` || !contains(assigned.Body.String(), `"responsibleUserId":"`+otherCandidate+`"`) {
		t.Fatalf("assign=%d headers=%v body=%s", assigned.Code, assigned.Header(), assigned.Body)
	}
	stored, ok := f.repo.Row(body.Id)
	if !ok || stored.ResponsibleUserID == nil || *stored.ResponsibleUserID != otherCandidate || stored.Version != 2 {
		t.Fatalf("assigned stored item=%+v", stored)
	}
	replaced := f.do("PATCH", base+"/"+body.Id, `{"responsibleUserId":"`+candidate+`"}`, map[string]string{"If-Match": `"2"`})
	if replaced.Code != http.StatusOK || replaced.Header().Get("ETag") != `"3"` || !contains(replaced.Body.String(), `"responsibleUserId":"`+candidate+`"`) {
		t.Fatalf("replace=%d headers=%v body=%s", replaced.Code, replaced.Header(), replaced.Body)
	}
	stored, _ = f.repo.Row(body.Id)
	if stored.ResponsibleUserID == nil || *stored.ResponsibleUserID != candidate || stored.Version != 3 {
		t.Fatalf("replaced stored item=%+v", stored)
	}
	stale := f.do("PATCH", base+"/"+body.Id, `{"responsibleUserId":null}`, map[string]string{"If-Match": `"2"`})
	if stale.Code != http.StatusPreconditionFailed || !contains(stale.Body.String(), `"code":"precondition_failed"`) {
		t.Fatalf("stale patch=%d %s", stale.Code, stale.Body)
	}
	stored, _ = f.repo.Row(body.Id)
	if stored.ResponsibleUserID == nil || *stored.ResponsibleUserID != candidate || stored.Version != 3 {
		t.Fatalf("stale patch changed stored item=%+v", stored)
	}
	preserved := f.do("PATCH", base+"/"+body.Id, `{"title":"Renamed"}`, map[string]string{"If-Match": `"3"`})
	if preserved.Code != http.StatusOK || preserved.Header().Get("ETag") != `"4"` || !contains(preserved.Body.String(), `"responsibleUserId":"`+candidate+`"`) {
		t.Fatalf("preserve=%d headers=%v body=%s", preserved.Code, preserved.Header(), preserved.Body)
	}
	patch := f.do("PATCH", base+"/"+body.Id, `{"responsibleUserId":null}`, map[string]string{"If-Match": `"4"`})
	if patch.Code != http.StatusOK || patch.Header().Get("ETag") != `"5"` {
		t.Fatalf("patch=%d headers=%v body=%s", patch.Code, patch.Header(), patch.Body)
	}
	bodyEqual(t, patch.Body.String(), `{"id":"`+body.Id+`","subjectId":"`+sid+`","responsibleUserId":null,"title":"Renamed","notes":null,"attentionOn":null,"recurrence":null,"workflowState":"open","attention":"needs_attention","archived":false,"done":false,"lastCompletedOn":null,"createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T11:00:00Z"}`)
	stored, _ = f.repo.Row(body.Id)
	if stored.ResponsibleUserID != nil || stored.Version != 5 {
		t.Fatalf("cleared stored item=%+v", stored)
	}
	for _, tc := range []struct {
		name, raw string
		status    int
		field     string
	}{
		{"malformed", `"not-a-uuid"`, 422, `"code":"invalid_reference","field":"responsibleUserId"`},
		{"unknown", `"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"`, 422, `"code":"invalid_reference","field":"responsibleUserId"`},
		{"foreign household member", `"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64"`, 422, `"code":"invalid_reference","field":"responsibleUserId"`},
		{"wrong json type", `7`, 400, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(true)
			w := f.do("POST", base, `{"title":"Bad","subjectId":"`+sid+`","responsibleUserId":`+tc.raw+`}`, nil)
			if w.Code != tc.status || (tc.field != `` && !contains(w.Body.String(), tc.field)) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if f.repo.Count() != 1 {
				t.Fatalf("rows=%d", f.repo.Count())
			}
		})
	}
	f = newFixture(true)
	f.candidateErr = errUnavailableTest{}
	w := f.do("POST", base, `{"title":"Outage","subjectId":"`+sid+`","responsibleUserId":"`+candidate+`"}`, nil)
	if w.Code != 503 || f.repo.Count() != 1 {
		t.Fatalf("outage status=%d rows=%d body=%s", w.Code, f.repo.Count(), w.Body)
	}
	f = newFixture(false)
	w = f.do("POST", base, `{"title":"Auth","subjectId":"`+sid+`","responsibleUserId":"`+candidate+`"}`, nil)
	if w.Code != 401 || f.authHit != 1 || len(f.memberLookups) != 0 {
		t.Fatalf("status=%d auth=%d candidate lookups=%v", w.Code, f.authHit, f.memberLookups)
	}
}

type errUnavailableTest struct{}

func (errUnavailableTest) Error() string { return "unavailable" }

func decodeTestJSON(b []byte, out any) error { return json.Unmarshal(b, out) }
func contains(s, sub string) bool            { return strings.Contains(s, sub) }
