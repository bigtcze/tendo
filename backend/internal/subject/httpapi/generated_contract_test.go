package httpapi

import (
	"encoding/json"
	"testing"
)

// TestValidationProblemCodesMatchGeneratedEnum pins the 422 codes the subject
// API emits to the generated SubjectValidationProblemCode enum. oapi-codegen
// v2.8.0 renamed these constants (InvalidLength became
// SubjectValidationProblemCodeInvalidLength); the wire values must not change.
func TestValidationProblemCodesMatchGeneratedEnum(t *testing.T) {
	for _, tc := range []struct {
		body string
		want SubjectValidationProblemCode
	}{
		{`{"name":"   ","type":"home"}`, SubjectValidationProblemCodeInvalidLength},
		{`{"name":"a\u0000b","type":"home"}`, SubjectValidationProblemCodeInvalidCharacters},
		{`{"name":"ok","type":"robot"}`, SubjectValidationProblemCodeInvalidType},
	} {
		if !tc.want.Valid() {
			t.Fatalf("%q is not a valid generated enum member", tc.want)
		}
		w := newFixture(true).do("POST", base, tc.body, nil)
		var problem SubjectValidationProblem
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if w.Code != 422 || problem.Code != tc.want || !problem.Code.Valid() || !problem.Field.Valid() || problem.Status != 422 {
			t.Fatalf("%s: status=%d body=%s", tc.body, w.Code, w.Body)
		}
	}
	for value, want := range map[SubjectValidationProblemCode]string{
		SubjectValidationProblemCodeInvalidLength:     "invalid_length",
		SubjectValidationProblemCodeInvalidCharacters: "invalid_characters",
		SubjectValidationProblemCodeInvalidType:       "invalid_type",
	} {
		if string(value) != want {
			t.Fatalf("wire value %q, want %q", value, want)
		}
	}
	if SubjectValidationProblemCode("invalid_value").Valid() {
		t.Fatal("unknown code accepted by generated enum")
	}
}

// TestSubjectListNextCursorNullability pins the required nullable nextCursor to
// a single pointer that always serializes: null on the last page, a string
// otherwise, and never omitted.
func TestSubjectListNextCursorNullability(t *testing.T) {
	cursor := "next"
	for _, tc := range []struct {
		in   SubjectList
		want string
	}{
		{SubjectList{Items: []Subject{}}, `{"items":[],"nextCursor":null}`},
		{SubjectList{Items: []Subject{}, NextCursor: &cursor}, `{"items":[],"nextCursor":"next"}`},
	} {
		got, err := json.Marshal(tc.in)
		if err != nil || string(got) != tc.want {
			t.Fatalf("got %s err=%v, want %s", got, err, tc.want)
		}
	}
}
