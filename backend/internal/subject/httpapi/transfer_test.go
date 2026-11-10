package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/subject"
)

const transferPath = "/api/v1/households/" + householdID + "/members/" + callerID + "/person"

func seedTransfer(t *testing.T, currentLinked bool) *fixture {
	t.Helper()
	f := newFixture(true)
	var link *string
	if currentLinked {
		link = strPtr(callerID)
	}
	f.repo.Seed(subject.Subject{ID: subjectID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Member person", LinkedUserID: link, Version: 3, CreatedAt: created, UpdatedAt: created})
	f.repo.Seed(subject.Subject{ID: otherID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Tomáš", Version: 1, CreatedAt: created, UpdatedAt: created})
	return f
}

func transferBody(target, targetVersion, current, currentVersion string) string {
	return `{"subjectId":"` + target + `","subjectVersion":` + targetVersion + `,"currentSubjectId":"` + current + `","currentSubjectVersion":` + currentVersion + `}`
}

func TestGetMemberPersonReturnsLinkedSubjectWithETag(t *testing.T) {
	f := seedTransfer(t, true)
	w := f.doAs("GET", transferPath, "", nil, callerID)
	if w.Code != 200 || w.Header().Get("ETag") != `"3"` {
		t.Fatalf("status=%d etag=%q body=%s", w.Code, w.Header().Get("ETag"), w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != subjectID || body["type"] != "person" || body["linkedUserId"] != callerID || body["name"] != "Member person" {
		t.Fatalf("body=%v", body)
	}
}

func TestGetMemberPersonNotFoundPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		linked bool
		userID string
	}{
		{"no linked person", false, callerID},
		{"non-member user", true, otherID},
		{"malformed user", true, "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedTransfer(t, tc.linked)
			w := f.doAs("GET", "/api/v1/households/"+householdID+"/members/"+tc.userID+"/person", "", nil, callerID)
			if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if w.Header().Get("ETag") != "" {
				t.Fatalf("404 carried ETag %q", w.Header().Get("ETag"))
			}
		})
	}
	// A user linked only in another household is not visible here.
	f := seedTransfer(t, false)
	foreignUser := otherID
	f.repo.Seed(subject.Subject{ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98", HouseholdID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b97", Type: subject.TypePerson, Name: "Foreign", LinkedUserID: &foreignUser, Version: 1, CreatedAt: created, UpdatedAt: created})
	w := f.doAs("GET", "/api/v1/households/"+householdID+"/members/"+foreignUser+"/person", "", nil, callerID)
	if w.Code != 404 {
		t.Fatalf("foreign household user status=%d body=%s", w.Code, w.Body)
	}
}

func TestGetMemberPersonAfterTransferReturnsNewPerson(t *testing.T) {
	f := seedTransfer(t, true)
	transfer := f.doAs("PUT", transferPath, transferBody(otherID, "1", subjectID, "3"), nil, callerID)
	if transfer.Code != 200 {
		t.Fatalf("transfer status=%d body=%s", transfer.Code, transfer.Body)
	}
	w := f.doAs("GET", transferPath, "", nil, callerID)
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` {
		t.Fatalf("status=%d etag=%q body=%s", w.Code, w.Header().Get("ETag"), w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != otherID || body["linkedUserId"] != callerID || body["name"] != "Tomáš" {
		t.Fatalf("body=%v", body)
	}
}

func TestTransferMemberPersonSwapsLinkAndVersions(t *testing.T) {
	f := seedTransfer(t, true)
	w := f.doAs("PUT", transferPath, transferBody(otherID, "1", subjectID, "3"), nil, callerID)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var body struct {
		Person          map[string]any `json:"person"`
		Previous        map[string]any `json:"previous"`
		PersonVersion   int            `json:"personVersion"`
		PreviousVersion int            `json:"previousVersion"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Person["id"] != otherID || body.Person["linkedUserId"] != callerID {
		t.Fatalf("person=%v", body.Person)
	}
	if body.Previous["id"] != subjectID || body.Previous["linkedUserId"] != nil || body.Previous["archived"] != false {
		t.Fatalf("previous=%v", body.Previous)
	}
	if body.PersonVersion != 2 || body.PreviousVersion != 4 {
		t.Fatalf("versions=%+v", body)
	}
	current, _ := f.repo.Row(subjectID)
	target, _ := f.repo.Row(otherID)
	if current.Version != 4 || current.LinkedUserID != nil || target.Version != 2 || target.LinkedUserID == nil || *target.LinkedUserID != callerID {
		t.Fatalf("stored current=%+v target=%+v", current, target)
	}
	if current.Name != "Member person" || target.Name != "Tomáš" {
		t.Fatalf("names changed: current=%q target=%q", current.Name, target.Name)
	}
}

func TestTransferStaleOrChangedReferences(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
		code string
	}{
		{"stale target", transferBody(otherID, "9", subjectID, "3"), 412, "precondition_failed"},
		{"stale current", transferBody(otherID, "1", subjectID, "9"), 412, "precondition_failed"},
		{"wrong current", transferBody(otherID, "1", otherID, "1"), 409, "member_person_changed"},
		{"malformed target", transferBody("not-a-uuid", "1", subjectID, "3"), 404, "not_found"},
		{"malformed current", transferBody(otherID, "1", "nope", "3"), 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedTransfer(t, true)
			w := f.doAs("PUT", transferPath, tc.body, nil, callerID)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			current, _ := f.repo.Row(subjectID)
			target, _ := f.repo.Row(otherID)
			if current.Version != 3 || target.Version != 1 || current.LinkedUserID == nil || target.LinkedUserID != nil {
				t.Fatalf("rejected transfer mutated rows current=%+v target=%+v", current, target)
			}
		})
	}
}

func TestTransferTargetEligibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fixture)
	}{
		{"archived", func(f *fixture) {
			row, _ := f.repo.Row(otherID)
			row.Archived = true
			f.repo.Seed(row)
		}},
		{"not person", func(f *fixture) {
			row, _ := f.repo.Row(otherID)
			row.Type = subject.TypeVehicle
			f.repo.Seed(row)
		}},
		{"already linked", func(f *fixture) {
			row, _ := f.repo.Row(otherID)
			row.LinkedUserID = strPtr("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99")
			f.repo.Seed(row)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedTransfer(t, true)
			tc.mutate(f)
			w := f.doAs("PUT", transferPath, transferBody(otherID, "1", subjectID, "3"), nil, callerID)
			want := `{"type":"about:blank","title":"Validation Failed","status":422,"code":"not_eligible","field":"subjectId"}`
			if w.Code != 422 || !jsonEqual(t, w.Body.String(), want) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			current, _ := f.repo.Row(subjectID)
			if current.LinkedUserID == nil || *current.LinkedUserID != callerID || current.Version != 3 {
				t.Fatalf("rejected transfer unlinked current=%+v", current)
			}
		})
	}
}

func TestTransferForeignSubjectIsNotFound(t *testing.T) {
	f := seedTransfer(t, true)
	foreign := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98"
	f.repo.Seed(subject.Subject{ID: foreign, HouseholdID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b97", Type: subject.TypePerson, Name: "Foreign", Version: 1, CreatedAt: created, UpdatedAt: created})
	w := f.doAs("PUT", transferPath, transferBody(foreign, "1", subjectID, "3"), nil, callerID)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
