package httpapi

import (
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
)

// TestMCPIdempotencyKeyIsPerGrantAndFailClosed pins the B3 key derivation: one
// grant and client key give one stable service key, another grant or key gives
// another, the client key never appears in it, and a call without a verified
// principal holding the scope, or with a key outside the HTTP rule, gets none.
func TestMCPIdempotencyKeyIsPerGrantAndFailClosed(t *testing.T) {
	write := identity.MCPScopeWrite
	key := func(grant, client string) string {
		t.Helper()
		got, ok := MCPIdempotencyKey(mcpPrincipalRequest(testMCPUser, grant, write), write, client)
		if !ok {
			t.Fatalf("grant %q key %q refused", grant, client)
		}
		return got
	}
	first := key("g1", "create-1")
	if first != key("g1", "create-1") || first != key("G1", "create-1") {
		t.Fatal("same grant and key must derive the same service key")
	}
	if first == key("g2", "create-1") || first == key("g1", "create-2") || first == key("g1", "Create-1") {
		t.Fatal("another grant or client key must derive another service key")
	}
	if strings.Contains(first, "create-1") || strings.Contains(first, "g1") || !strings.HasPrefix(first, "mcp1.") || !httpx.ValidIdempotencyKey(first) {
		t.Fatalf("derived key %q", first)
	}
	refused := map[string]struct {
		grant, scope, client string
	}{
		"empty key":       {"g1", write, ""},
		"key too long":    {"g1", write, strings.Repeat("k", 129)},
		"key with space":  {"g1", write, "a b"},
		"key non-ASCII":   {"g1", write, "klíč"},
		"no grant":        {"", write, "k"},
		"read scope only": {"g1", identity.MCPScopeRead, "k"},
	}
	for name, tc := range refused {
		req := mcpPrincipalRequest(testMCPUser, tc.grant, tc.scope)
		if tc.grant == "" {
			req = withoutGrant(req)
		}
		if got, ok := MCPIdempotencyKey(req, write, tc.client); ok || got != "" {
			t.Fatalf("%s: got %q ok=%v", name, got, ok)
		}
	}
	if _, ok := MCPIdempotencyKey(nil, write, "k"); ok {
		t.Fatal("nil request must be refused")
	}
	if got, ok := MCPIdempotencyKey(mcpPrincipalRequest(testMCPUser, "g1", write), write, strings.Repeat("k", 128)); !ok || len(got) > 128 {
		t.Fatalf("longest client key: %q %v", got, ok)
	}
}
