package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidIdempotencyKey(t *testing.T) {
	for key, want := range map[string]bool{
		"":                         false,
		"a":                        true,
		"!":                        true,
		"~":                        true,
		"Key-1":                    true,
		strings.Repeat("k", 128):   true,
		strings.Repeat("k", 129):   false,
		"has space":                false,
		"tab\tkey":                 false,
		"del\x7f":                  false,
		"ctl\x1f":                  false,
		"é":                        false,
		"\x80":                     false,
		" leading":                 false,
		"trailing ":                false,
		strings.Repeat("~", 64):    true,
		"MiXeD-Case_0123456789.:/": true,
	} {
		if got := ValidIdempotencyKey(key); got != want {
			t.Errorf("ValidIdempotencyKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestIdempotencyKeyHeader(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		code   string
		key    string
	}{
		{name: "missing", values: nil, code: "idempotency_key_required"},
		{name: "empty value", values: []string{""}, code: "invalid_idempotency_key"},
		{name: "repeated", values: []string{"one", "two"}, code: "invalid_idempotency_key"},
		{name: "repeated identical", values: []string{"one", "one"}, code: "invalid_idempotency_key"},
		{name: "invalid byte", values: []string{"has space"}, code: "invalid_idempotency_key"},
		{name: "too long", values: []string{strings.Repeat("k", 129)}, code: "invalid_idempotency_key"},
		{name: "valid", values: []string{"Key-1"}, key: "Key-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			for _, v := range tc.values {
				r.Header.Add("Idempotency-Key", v)
			}
			w := httptest.NewRecorder()
			key, ok := IdempotencyKey(w, r)
			if tc.code == "" {
				if !ok || key != tc.key || w.Body.Len() != 0 {
					t.Fatalf("got key=%q ok=%v body=%q, want key=%q", key, ok, w.Body.String(), tc.key)
				}
				return
			}
			if ok || key != "" || w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got key=%q ok=%v status=%d body=%q, want 400 %s", key, ok, w.Code, w.Body.String(), tc.code)
			}
		})
	}
}
