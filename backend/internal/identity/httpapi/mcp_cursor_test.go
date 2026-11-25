package httpapi

import (
	"strings"
	"testing"
)

func TestMCPCursorRoundTripsOnlyInItsOwnContext(t *testing.T) {
	const inner = "aTI6c2VydmljZS1jdXJzb3I"
	bound := BindMCPCursor(inner, "list_items", "user-a", "house-1", "q=zebra")
	if bound == "" || strings.Contains(bound, inner) {
		t.Fatalf("bound cursor %q must be opaque and non-empty", bound)
	}
	if got, ok := OpenMCPCursor(bound, "list_items", "user-a", "house-1", "q=zebra"); !ok || got != inner {
		t.Fatalf("same context: got %q ok=%v", got, ok)
	}
	refused := map[string]func() (string, bool){
		"other tool":      func() (string, bool) { return OpenMCPCursor(bound, "list_subjects", "user-a", "house-1", "q=zebra") },
		"other user":      func() (string, bool) { return OpenMCPCursor(bound, "list_items", "user-b", "house-1", "q=zebra") },
		"other household": func() (string, bool) { return OpenMCPCursor(bound, "list_items", "user-a", "house-2", "q=zebra") },
		"other filter":    func() (string, bool) { return OpenMCPCursor(bound, "list_items", "user-a", "house-1", "q=zebr") },
		"fewer fields":    func() (string, bool) { return OpenMCPCursor(bound, "list_items", "user-a", "house-1") },
		"shifted fields":  func() (string, bool) { return OpenMCPCursor(bound, "list_items", "user-a", "house-1q=zebra") },
		"raw inner":       func() (string, bool) { return OpenMCPCursor(inner, "list_items", "user-a", "house-1", "q=zebra") },
		"padded":          func() (string, bool) { return OpenMCPCursor(bound+"=", "list_items", "user-a", "house-1", "q=zebra") },
		"oversized": func() (string, bool) {
			return OpenMCPCursor(strings.Repeat("A", MaxMCPCursorLength+1), "list_items", "user-a", "house-1", "q=zebra")
		},
	}
	for name, open := range refused {
		if got, ok := open(); ok || got != "" {
			t.Fatalf("%s: got %q ok=%v, want refused", name, got, ok)
		}
	}
}

func TestMCPCursorEmptyMeansFirstAndLastPage(t *testing.T) {
	if got := BindMCPCursor("", "list_items", "user-a"); got != "" {
		t.Fatalf("no next page must bind to empty, got %q", got)
	}
	if got, ok := OpenMCPCursor("", "list_items", "user-a"); !ok || got != "" {
		t.Fatalf("empty cursor must open to the first page, got %q ok=%v", got, ok)
	}
}

func TestMCPCursorLongestServiceCursorFits(t *testing.T) {
	inner := strings.Repeat("x", 300)
	bound := BindMCPCursor(inner, "list_items", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "a", "b")
	if len(bound) > MaxMCPCursorLength {
		t.Fatalf("bound cursor length %d exceeds %d", len(bound), MaxMCPCursorLength)
	}
}
