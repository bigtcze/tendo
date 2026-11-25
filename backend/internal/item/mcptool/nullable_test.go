package mcptool

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

type nullableProbe struct {
	Notes Nullable[string] `json:"notes,omitempty"`
}

func TestNullableDistinguishesAbsentNullAndValue(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		set   bool
		value *string
	}{
		{"absent", `{}`, false, nil},
		{"null", `{"notes":null}`, true, nil},
		{"value", `{"notes":"milk"}`, true, ptr("milk")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var in nullableProbe
			if err := json.Unmarshal([]byte(tc.body), &in); err != nil {
				t.Fatal(err)
			}
			if in.Notes.Set != tc.set {
				t.Fatalf("Set=%v want %v", in.Notes.Set, tc.set)
			}
			if (in.Notes.Value == nil) != (tc.value == nil) ||
				(tc.value != nil && *in.Notes.Value != *tc.value) {
				t.Fatalf("Value=%v want %v", in.Notes.Value, tc.value)
			}
		})
	}
}

// TestNullableStringSchemaValidatesTheWireValues runs the real schema check the
// SDK applies before decoding: a string or null is accepted, anything else is
// refused. An inferred struct schema would refuse a plain string.
func TestNullableStringSchemaValidatesTheWireValues(t *testing.T) {
	schema := identityhttpSchema(t, stringOrNull)
	for body, ok := range map[string]bool{
		`{"notes":"milk"}`: true,
		`{"notes":null}`:   true,
		`{}`:               true,
		`{"notes":5}`:      false,
		`{"notes":{}}`:     false,
	} {
		if got := validates(t, schema, body); got != ok {
			t.Fatalf("%s: accepted=%v want %v", body, got, ok)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// stringOrNull is the property schema for a nullable string: string or null.
func stringOrNull(s *jsonschema.Schema) {
	s.Types = []string{"string", "null"}
	s.Type = ""
}

func identityhttpSchema(t *testing.T, constrain func(*jsonschema.Schema)) *jsonschema.Resolved {
	t.Helper()
	root := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"notes": {},
	}}
	constrain(root.Properties["notes"])
	resolved, err := root.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func validates(t *testing.T, resolved *jsonschema.Resolved, body string) bool {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return resolved.Validate(&v) == nil
}
