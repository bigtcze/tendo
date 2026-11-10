package httpapi

import (
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/subject"
)

func TestMemberPersonPatchIsLockedButRenameAndNoOpAreAllowed(t *testing.T) {
	f := newFixture(true)
	f.repo.Seed(subject.Subject{
		ID: subjectID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Member",
		LinkedUserID: strPtr(callerID), Version: 1, CreatedAt: created, UpdatedAt: created,
	})
	path := base + "/" + subjectID
	noop := f.doAs("PATCH", path, `{"linkedUserId":"`+callerID+`"}`, map[string]string{"If-Match": `"1"`}, callerID)
	if noop.Code != 200 || !strings.Contains(noop.Body.String(), `"linkedUserId":"`+callerID+`"`) {
		t.Fatalf("same-link no-op status=%d body=%s", noop.Code, noop.Body)
	}
	renamed := f.doAs("PATCH", path, `{"name":"Member renamed"}`, map[string]string{"If-Match": `"2"`}, callerID)
	if renamed.Code != 200 || !strings.Contains(renamed.Body.String(), `"name":"Member renamed"`) {
		t.Fatalf("rename status=%d body=%s", renamed.Code, renamed.Body)
	}
	for _, tc := range []struct{ name, body string }{
		{"unlink", `{"linkedUserId":null}`},
		{"retype", `{"type":"vehicle"}`},
		{"retype-and-clear", `{"type":"vehicle","linkedUserId":null}`},
		{"archive", `{"archived":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := f.repo.Row(subjectID)
			w := f.doAs("PATCH", path, tc.body, map[string]string{"If-Match": `"3"`}, callerID)
			want := problemBody(409, "Conflict", "member_person_locked")
			if w.Code != 409 || !jsonEqual(t, w.Body.String(), want) {
				t.Fatalf("status=%d body=%s want=%s", w.Code, w.Body, want)
			}
			if after, _ := f.repo.Row(subjectID); after != before {
				t.Fatalf("rejected patch changed subject: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPatchLinkOnUnlinkedPersonReturnsConflict(t *testing.T) {
	f := newFixture(true)
	f.repo.Seed(subject.Subject{ID: subjectID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Unlinked", Version: 1, CreatedAt: created, UpdatedAt: created})
	w := f.doAs("PATCH", base+"/"+subjectID, `{"linkedUserId":"`+callerID+`"}`, map[string]string{"If-Match": `"1"`}, callerID)
	want := problemBody(409, "Conflict", "member_person_exists")
	if w.Code != 409 || !jsonEqual(t, w.Body.String(), want) {
		t.Fatalf("status=%d body=%s want=%s", w.Code, w.Body, want)
	}
	row, _ := f.repo.Row(subjectID)
	if row.LinkedUserID != nil || row.Version != 1 {
		t.Fatalf("rejected link mutated row=%+v", row)
	}
}

func TestCreateWithMemberLinkReturnsConflict(t *testing.T) {
	f := newFixture(true)
	before := f.repo.Count()
	w := f.doAs("POST", base, `{"name":"Duplicate member","type":"person","linkedUserId":"`+callerID+`"}`, map[string]string{"Idempotency-Key": "member-link"}, callerID)
	want := problemBody(409, "Conflict", "member_person_exists")
	if w.Code != 409 || !jsonEqual(t, w.Body.String(), want) || f.repo.Count() != before {
		t.Fatalf("create=%d %s rows=%d want=%s", w.Code, w.Body, f.repo.Count(), want)
	}
}

func TestStaleLinkedSubjectInvariantAndPreconditionOrder(t *testing.T) {
	f := newFixture(true)
	f.repo.Seed(subject.Subject{
		ID: subjectID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Member",
		LinkedUserID: strPtr(callerID), Version: 2, CreatedAt: created, UpdatedAt: created,
	})
	path := base + "/" + subjectID
	before, _ := f.repo.Row(subjectID)
	// Member-person invariants are evaluated against the stored row before the
	// stale version check, so illegal transitions are 409 rather than 412.
	for _, body := range []string{`{"type":"vehicle"}`, `{"linkedUserId":null}`, `{"archived":true}`} {
		w := f.doAs("PATCH", path, body, map[string]string{"If-Match": `"1"`}, callerID)
		want := problemBody(409, "Conflict", "member_person_locked")
		if w.Code != 409 || !jsonEqual(t, w.Body.String(), want) {
			t.Fatalf("body=%s stale member-person patch=%d %s want=%s", body, w.Code, w.Body, want)
		}
	}
	// A valid same-value patch is only stale, so it reports 412.
	valid := f.doAs("PATCH", path, `{"type":"person"}`, map[string]string{"If-Match": `"1"`}, callerID)
	if valid.Code != 412 || !jsonEqual(t, valid.Body.String(), problemBody(412, "Precondition Failed", "precondition_failed")) {
		t.Fatalf("valid stale patch=%d %s", valid.Code, valid.Body)
	}
	// A stale link-only patch on an unlinked non-person reports requires_person
	// 422 before 412.
	f.repo.Seed(subject.Subject{
		ID: otherID, HouseholdID: householdID, Type: subject.TypeVehicle, Name: "Car", Version: 2, CreatedAt: created, UpdatedAt: created,
	})
	requiresPerson := f.doAs("PATCH", base+"/"+otherID, `{"linkedUserId":"`+callerID+`"}`, map[string]string{"If-Match": `"1"`}, callerID)
	wantValidation := `{"type":"about:blank","title":"Validation Failed","status":422,"code":"requires_person","field":"linkedUserId"}`
	if requiresPerson.Code != 422 || !jsonEqual(t, requiresPerson.Body.String(), wantValidation) {
		t.Fatalf("stale link-only non-person=%d %s", requiresPerson.Code, requiresPerson.Body)
	}
	if after, _ := f.repo.Row(subjectID); after != before {
		t.Fatalf("rejected stale PATCH changed subject: before=%+v after=%+v", before, after)
	}
	if after, _ := f.repo.Row(otherID); after.Version != 2 || after.LinkedUserID != nil {
		t.Fatalf("rejected stale link changed subject: %+v", after)
	}
}

func TestLinkedUserWrongJSONTypesAre400AndDoNotMutate(t *testing.T) {
	for _, raw := range []string{`7`, `true`, `{"id":"x"}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			f := newFixture(true)
			before := f.repo.Count()
			post := f.doAs("POST", base, `{"name":"Wrong type","type":"person","linkedUserId":`+raw+`}`, map[string]string{"Idempotency-Key": "bad-" + raw}, callerID)
			if post.Code != 400 || !strings.Contains(post.Body.String(), `"code":"invalid_request"`) || f.repo.Count() != before {
				t.Fatalf("POST=%d %s count=%d", post.Code, post.Body, f.repo.Count())
			}
			f.repo.Seed(subject.Subject{
				ID: subjectID, HouseholdID: householdID, Type: subject.TypePerson, Name: "Member",
				LinkedUserID: strPtr(callerID), Version: 1, CreatedAt: created, UpdatedAt: created,
			})
			patch := f.doAs("PATCH", base+"/"+subjectID, `{"linkedUserId":`+raw+`}`, map[string]string{"If-Match": `"1"`}, callerID)
			row, _ := f.repo.Row(subjectID)
			if patch.Code != 400 || !strings.Contains(patch.Body.String(), `"code":"invalid_request"`) || row.Version != 1 || row.LinkedUserID == nil || *row.LinkedUserID != callerID {
				t.Fatalf("PATCH=%d %s row=%+v", patch.Code, patch.Body, row)
			}
		})
	}
}

func TestCreateWithNonmemberLinkedUserIsRejected(t *testing.T) {
	f := newFixture(true)
	nonmember := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b88"
	before := f.repo.Count()
	bad := f.doAs("POST", base, `{"name":"Nonmember link","type":"person","linkedUserId":"`+nonmember+`"}`, map[string]string{"Idempotency-Key": "nonmember-link"}, callerID)
	want := `{"type":"about:blank","title":"Validation Failed","status":422,"code":"invalid_reference","field":"linkedUserId"}`
	if bad.Code != 422 || !jsonEqual(t, bad.Body.String(), want) || f.repo.Count() != before {
		t.Fatalf("nonmember create=%d %s rows=%d want=%s", bad.Code, bad.Body, f.repo.Count(), want)
	}
}

func TestCreateOmittedAndNullLinkedUserReplayEquivalently(t *testing.T) {
	f := newFixture(true)
	key := "omitted-null-equivalent"
	before := f.repo.Count()
	first := f.doAs("POST", base, `{"name":"Optional","type":"person"}`, map[string]string{"Idempotency-Key": key}, callerID)
	second := f.doAs("POST", base, `{"name":"Optional","type":"person","linkedUserId":null}`, map[string]string{"Idempotency-Key": key}, callerID)
	if first.Code != 201 || second.Code != 201 || !jsonEqual(t, first.Body.String(), second.Body.String()) || f.repo.Count() != before+1 {
		t.Fatalf("omitted=%d %s null=%d %s count=%d", first.Code, first.Body, second.Code, second.Body, f.repo.Count())
	}
}

func strPtr(v string) *string { return &v }
