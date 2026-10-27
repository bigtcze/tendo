package httpx

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Characterization of the strict object decoder shared by every
// household-scoped resource handler: exact decoded values, integer range
// saturation (callers turn out-of-range values into 422 invalid_interval),
// nested-object strictness and the empty-object policy.

func TestDecodeObjectExactValues(t *testing.T) {
	got, err := DecodeObject([]byte(`{"a":"\u00e9\ud83d\ude00","n":"","b":false,"i":-0,"o":{"s":"x","n":999}}`), testFields)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": "é😀", "n": "", "b": false, "i": int64(0), "o": map[string]any{"s": "x", "n": int64(999)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestDecodeObjectIntegerRangeSaturates(t *testing.T) {
	for body, want := range map[string]int64{
		`{"a":"x","i":9223372036854775807}`:     1<<63 - 1,
		`{"a":"x","i":9223372036854775808}`:     1<<63 - 1,
		`{"a":"x","i":99999999999999999999999}`: 1<<63 - 1,
		`{"a":"x","i":-9223372036854775808}`:    -1 << 63,
		`{"a":"x","i":-9223372036854775809}`:    -1 << 63,
		`{"a":"x","i":-1}`:                      -1,
	} {
		got, err := DecodeObject([]byte(body), testFields)
		if err != nil || got["i"] != want {
			t.Fatalf("%s: got=%#v err=%v", body, got["i"], err)
		}
	}
	for _, body := range []string{`{"a":"x","i":1E2}`, `{"a":"x","i":0.0}`, `{"a":"x","i":null}`, `{"a":"x","i":[1]}`, `{"a":"x","i":{}}`, `{"a":"x","i":-}`, `{"a":"x","i":01}`,
		// Overflowing digits before a fraction or exponent must not saturate.
		`{"a":"x","i":99999999999999999999.5}`, `{"a":"x","i":-99999999999999999999e1}`} {
		if got, err := DecodeObject([]byte(body), testFields); err == nil {
			t.Fatalf("accepted %s as %#v", body, got)
		}
	}
}

func TestDecodeObjectNestedStrictness(t *testing.T) {
	for name, body := range map[string]string{
		"nested empty":         `{"a":"x","o":{}}`,
		"nested array":         `{"a":"x","o":[]}`,
		"nested string":        `{"a":"x","o":"x"}`,
		"nested trailing data": `{"a":"x","o":{"n":1,"s":"x"} 1}`,
		"nested invalid kind":  `{"a":"x","o":{"n":1,"s":null}}`,
		"unterminated":         `{"a":"x"`,
		"unterminated nested":  `{"a":"x","o":{"n":1,"s":"x"}`,
		"non-string key":       `{1:"x"}`,
		"leading bom":          "\ufeff{\"a\":\"x\"}",
	} {
		if got, err := DecodeObject([]byte(body), testFields); err == nil {
			t.Fatalf("%s accepted: %#v", name, got)
		}
	}
	got, err := DecodeObject([]byte(` {"a":"x","o":null} `), testFields)
	if err != nil || len(got) != 2 || got["o"] != nil {
		t.Fatalf("surrounding whitespace and null object: got=%#v err=%v", got, err)
	}
	if _, present := got["o"]; !present {
		t.Fatal("null object must be present")
	}
	missingSchema := map[string]Field{"o": {Kind: KindNullableObject}}
	if _, err := DecodeObject([]byte(`{"o":{"x":1}}`), missingSchema); err == nil {
		t.Fatal("nested object without schema accepted")
	}
	if got, err := DecodeObject([]byte(`{"o":null}`), missingSchema); err != nil || got["o"] != nil {
		t.Fatalf("null for schemaless object: got=%#v err=%v", got, err)
	}
}

// A nested object is never allowed to be empty, even when every nested key is
// optional and the outer read permits an empty object.
func TestNestedObjectEmptyRejectedWithOptionalFields(t *testing.T) {
	fields := map[string]Field{"o": {Kind: KindNullableObject, Fields: map[string]Field{"s": {Kind: KindString}}}}
	if got, err := DecodeObject([]byte(`{"o":{}}`), fields); err == nil {
		t.Fatalf("empty nested object accepted: %#v", got)
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"o":{}}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if _, ok := ReadObjectAllowEmpty(w, r, fields, 100); ok || w.Code != 400 {
		t.Fatalf("allow-empty read accepted empty nested object: ok=%v code=%d", ok, w.Code)
	}
	got, err := DecodeObject([]byte(`{"o":{"s":"x"}}`), fields)
	if err != nil || !reflect.DeepEqual(got, map[string]any{"o": map[string]any{"s": "x"}}) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

// Every decode error returns a nil map, including presence errors raised after
// the values were decoded.
func TestDecodeObjectNilMapOnError(t *testing.T) {
	for _, body := range []string{`{}`, `{"n":"x"}`, `{"a":"x","z":1}`} {
		if got, err := DecodeObject([]byte(body), testFields); err == nil || got != nil {
			t.Fatalf("%s: got=%#v err=%v", body, got, err)
		}
	}
}

// A field declared with an unknown Kind fails closed even when optional and
// accompanied by a valid field.
func TestDecodeObjectUnknownKindFailsClosed(t *testing.T) {
	fields := map[string]Field{"a": {Kind: KindString}, "x": {Kind: Kind(99)}}
	if got, err := DecodeObject([]byte(`{"a":"y","x":"z"}`), fields); err == nil {
		t.Fatalf("unknown kind accepted: %#v", got)
	}
}

func TestReadObjectAllowEmpty(t *testing.T) {
	optional := map[string]Field{"s": {Kind: KindString}}
	read := func(allowEmpty bool, body string) (map[string]any, int) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		var v map[string]any
		var ok bool
		if allowEmpty {
			v, ok = ReadObjectAllowEmpty(w, r, optional, 100)
		} else {
			v, ok = ReadObject(w, r, optional, 100)
		}
		if ok {
			return v, 0
		}
		return nil, w.Code
	}
	if v, code := read(true, `{}`); code != 0 || v == nil || len(v) != 0 {
		t.Fatalf("allow empty: v=%#v code=%d", v, code)
	}
	if _, code := read(false, `{}`); code != 400 {
		t.Fatalf("empty rejected by default: code=%d", code)
	}
	for _, body := range []string{``, `null`, `[]`, `{"x":1}`} {
		if _, code := read(true, body); code != 400 {
			t.Fatalf("allow empty %q: code=%d", body, code)
		}
	}
	required := map[string]Field{"s": {Kind: KindString, Required: true}}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if _, ok := ReadObjectAllowEmpty(w, r, required, 100); ok || w.Code != 400 {
		t.Fatalf("allow empty still enforces required keys: ok=%v code=%d", ok, w.Code)
	}
}
