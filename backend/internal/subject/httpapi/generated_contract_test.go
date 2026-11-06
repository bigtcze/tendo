package httpapi

import (
	"encoding/json"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"testing"
)

// TestValidationProblemCodesMatchGeneratedEnum pins the 422 codes the subject
// API emits to the generated SubjectValidationProblemCode enum. oapi-codegen
// v2.8.0 renamed these constants (InvalidLength became
// SubjectValidationProblemCodeInvalidLength); the wire values must not change.
func TestValidationProblemCodesMatchGeneratedEnum(t *testing.T) {
	for _, tc := range []struct {
		body string
		want apimodel.SubjectValidationProblemCode
	}{
		{`{"name":"   ","type":"home"}`, apimodel.SubjectValidationProblemCodeInvalidLength},
		{`{"name":"a\u0000b","type":"home"}`, apimodel.SubjectValidationProblemCodeInvalidCharacters},
		{`{"name":"ok","type":"robot"}`, apimodel.SubjectValidationProblemCodeInvalidType},
	} {
		if !tc.want.Valid() {
			t.Fatalf("%q is not a valid generated enum member", tc.want)
		}
		w := newFixture(true).do("POST", base, tc.body, map[string]string{"Idempotency-Key": "contract-test"})
		var problem apimodel.SubjectValidationProblem
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if w.Code != 422 || problem.Code != tc.want || !problem.Code.Valid() || !problem.Field.Valid() || problem.Status != 422 {
			t.Fatalf("%s: status=%d body=%s", tc.body, w.Code, w.Body)
		}
	}
	for value, want := range map[apimodel.SubjectValidationProblemCode]string{
		apimodel.SubjectValidationProblemCodeInvalidLength:     "invalid_length",
		apimodel.SubjectValidationProblemCodeInvalidCharacters: "invalid_characters",
		apimodel.SubjectValidationProblemCodeInvalidType:       "invalid_type",
	} {
		if string(value) != want {
			t.Fatalf("wire value %q, want %q", value, want)
		}
	}
	if apimodel.SubjectValidationProblemCode("invalid_value").Valid() {
		t.Fatal("unknown code accepted by generated enum")
	}
}

// TestSubjectListNextCursorNullability pins the required nullable nextCursor to
// a single pointer that always serializes: null on the last page, a string
// otherwise, and never omitted.
func TestSubjectListNextCursorNullability(t *testing.T) {
	cursor := "next"
	for _, tc := range []struct {
		in   apimodel.SubjectList
		want string
	}{
		{apimodel.SubjectList{Items: []apimodel.Subject{}}, `{"items":[],"nextCursor":null}`},
		{apimodel.SubjectList{Items: []apimodel.Subject{}, NextCursor: &cursor}, `{"items":[],"nextCursor":"next"}`},
	} {
		got, err := json.Marshal(tc.in)
		if err != nil || string(got) != tc.want {
			t.Fatalf("got %s err=%v, want %s", got, err, tc.want)
		}
	}
}
