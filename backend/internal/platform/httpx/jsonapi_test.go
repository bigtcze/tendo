package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testFields = map[string]Field{
	"a": {Kind: KindString, Required: true},
	"n": {Kind: KindNullableString},
	"b": {Kind: KindBool},
	"i": {Kind: KindInteger},
	"o": {Kind: KindNullableObject, Fields: map[string]Field{"n": {Kind: KindInteger, Required: true}, "s": {Kind: KindString, Required: true}}},
}

func TestDecodeObjectStrictness(t *testing.T) {
	for name, body := range map[string]string{
		"empty":            `{}`,
		"missing required": `{"n":"x"}`,
		"unknown key":      `{"a":"x","z":1}`,
		"duplicate":        `{"a":"x","a":"y"}`,
		"duplicate null":   `{"a":"x","n":null,"n":"y"}`,
		"null required":    `{"a":null}`,
		"null bool":        `{"a":"x","b":null}`,
		"wrong string":     `{"a":1}`,
		"wrong nullable":   `{"a":"x","n":1}`,
		"wrong bool":       `{"a":"x","b":"true"}`,
		"array":            `[]`,
		"trailing":         `{"a":"x"} {}`,
		"not json":         `nope`,
		"invalid utf8":     "{\"a\":\"\xff\"}",
	} {
		if _, err := DecodeObject([]byte(body), testFields); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestDecodeObjectNullableDistinguishesNullFromOmitted(t *testing.T) {
	got, err := DecodeObject([]byte(`{"a":"x","n":null}`), testFields)
	if err != nil {
		t.Fatal(err)
	}
	if v, present := got["n"]; !present || v != nil {
		t.Fatalf("null must be present and nil: %#v", got)
	}
	got, err = DecodeObject([]byte(`{"a":"x"}`), testFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["n"]; present {
		t.Fatalf("omitted key present: %#v", got)
	}
	got, err = DecodeObject([]byte(`{"a":"x","n":"s","b":true}`), testFields)
	if err != nil || got["n"] != "s" || got["b"] != true || got["a"] != "x" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestDecodeIntegerAndNullableObject(t *testing.T) {
	for _, body := range []string{`{"a":"x","i":12}`, `{"a":"x","o":null}`} {
		if _, err := DecodeObject([]byte(body), testFields); err != nil {
			t.Fatalf("valid %s: %v", body, err)
		}
	}
	got, err := DecodeObject([]byte(`{"a":"x","o":{"n":12,"s":"nested"}}`), testFields)
	if err != nil {
		t.Fatal(err)
	}
	nested, ok := got["o"].(map[string]any)
	if !ok {
		t.Fatalf("nested=%#v", got["o"])
	}
	if n, ok := nested["n"].(int64); !ok || n != 12 || nested["s"] != "nested" {
		t.Fatalf("nested=%#v", nested)
	}
	for _, body := range []string{`{"a":"x","i":1.5}`, `{"a":"x","i":1e2}`, `{"a":"x","i":"12"}`, `{"a":"x","i":true}`, `{"a":"x","o":{"n":1,"s":"x","extra":true}}`, `{"a":"x","o":{"n":1,"s":"x","n":2}}`, `{"a":"x","o":{"n":1}}`, `{"a":"x","o":{"n":"1","s":"x"}}`, `{"a":"x","o":{"n":null,"s":"x"}}`} {
		if _, err := DecodeObject([]byte(body), testFields); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestParseIfMatch(t *testing.T) {
	do := func(values ...string) (int64, int, bool) {
		r := httptest.NewRequest("PATCH", "/", nil)
		for _, v := range values {
			r.Header.Add("If-Match", v)
		}
		w := httptest.NewRecorder()
		n, ok := ParseIfMatch(w, r)
		return n, w.Code, ok
	}
	if n, _, ok := do(`"7"`); !ok || n != 7 {
		t.Fatalf("n=%d ok=%v", n, ok)
	}
	if _, code, ok := do(); ok || code != 428 {
		t.Fatalf("missing: %d %v", code, ok)
	}
	for _, bad := range []string{`7`, `W/"7"`, `"0"`, `"01"`, `*`, `"1", "2"`, `""`, `"99999999999999999999"`} {
		if _, code, ok := do(bad); ok || code != 412 {
			t.Fatalf("%q: %d %v", bad, code, ok)
		}
	}
	if _, code, ok := do(`"1"`, `"2"`); ok || code != 412 {
		t.Fatalf("repeated header: %d %v", code, ok)
	}
}

func TestParseListParams(t *testing.T) {
	parse := func(query string) (ListParams, string) {
		w := httptest.NewRecorder()
		p, ok := ParseListParams(w, httptest.NewRequest("GET", "/?"+query, nil), 100)
		if ok {
			return p, ""
		}
		var body struct{ Code, Parameter string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 400 || body.Code != "invalid_query" {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		return p, body.Parameter
	}
	if p, bad := parse("limit=100&cursor=abc&archived=true"); bad != "" || p != (ListParams{Limit: 100, Cursor: "abc", Archived: true}) {
		t.Fatalf("p=%+v bad=%s", p, bad)
	}
	if p, bad := parse("other=1"); bad != "" || p != (ListParams{}) {
		t.Fatalf("p=%+v bad=%s", p, bad)
	}
	if p, ok := ParseListParamsFor(httptest.NewRecorder(), httptest.NewRequest("GET", "/?done=bad", nil), 100, ListFilterArchived); !ok || p.Done {
		t.Fatalf("subjects filter parsed done: %+v ok=%v", p, ok)
	}
	for query, want := range map[string]string{
		"limit=0": "limit", "limit=101": "limit", "limit=1&limit=2": "limit", "limit=%2B5": "limit", "limit=": "limit",
		"cursor=": "cursor", "cursor=a&cursor=b": "cursor",
		"archived=1": "archived", "archived=TRUE": "archived", "archived=": "archived",
		"archived=x&limit=0": "limit",
	} {
		if _, bad := parse(query); bad != want {
			t.Fatalf("%s: got %q want %q", query, bad, want)
		}
	}
}

func TestReadObjectMediaTypeAndLimit(t *testing.T) {
	do := func(contentType, body string, limit int64) (map[string]any, int) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		v, ok := ReadObject(w, r, testFields, limit)
		if ok {
			return v, 0
		}
		return nil, w.Code
	}
	if v, code := do("application/json; charset=utf-8", `{"a":"x"}`, 100); code != 0 || v["a"] != "x" {
		t.Fatalf("v=%v code=%d", v, code)
	}
	for ct, want := range map[string]int{"": 415, "text/plain": 415, "application/json; charset=latin1": 415, "application/json; x=y": 415} {
		if _, code := do(ct, `{"a":"x"}`, 100); code != want {
			t.Fatalf("%q: %d", ct, code)
		}
	}
	if _, code := do("application/json", `{"a":"`+strings.Repeat("x", 50)+`"}`, 20); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d", code)
	}
	if _, code := do("application/json", `{"a":1}`, 100); code != 400 {
		t.Fatalf("bad body: %d", code)
	}
}

func TestProblemWriters(t *testing.T) {
	w := httptest.NewRecorder()
	ValidationProblemResponse(w, "title", "invalid_length")
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 422 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" || got["field"] != "title" || got["code"] != "invalid_length" || got["title"] != "Validation Failed" || got["type"] != "about:blank" || got["status"] != float64(422) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	if ETag(12) != `"12"` {
		t.Fatal(ETag(12))
	}
}
