package identity

import "testing"

// TestMCPRedirectURIMatches pins the redirect comparison used by Begin,
// Decide and IssueAuthorizationCode. Only loopback http ignores the port
// (RFC 8252 section 8.3); everything else is exact.
func TestMCPRedirectURIMatches(t *testing.T) {
	cases := []struct {
		name       string
		registered []string
		requested  string
		want       bool
	}{
		{"exact https", []string{"https://client.test/cb"}, "https://client.test/cb", true},
		{"exact private-use", []string{"cursor://anysphere.cursor-mcp/oauth/callback"}, "cursor://anysphere.cursor-mcp/oauth/callback", true},
		{"exact private-use among several", []string{"https://client.test/cb", "myapp:/cb"}, "myapp:/cb", true},
		{"https different port refused", []string{"https://client.test/cb"}, "https://client.test:8443/cb", false},
		{"https registered port requested none refused", []string{"https://client.test:8443/cb"}, "https://client.test/cb", false},
		{"https other host refused", []string{"https://client.test/cb"}, "https://evil.test/cb", false},
		{"https loopback is exact only", []string{"https://127.0.0.1/cb"}, "https://127.0.0.1:9000/cb", false},
		{"private-use different path refused", []string{"myapp:/cb"}, "myapp:/cb/", false},
		{"private-use with port refused", []string{"myapp:/cb"}, "myapp://host:8080/cb", false},
		{"private-use opaque request refused", []string{"myapp:/cb"}, "myapp:foo", false},
		{"loopback 127.0.0.1 different port", []string{"http://127.0.0.1:8787/cb"}, "http://127.0.0.1:9000/cb", true},
		{"loopback 127.0.0.1 registered no port", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1:9000/cb", true},
		{"loopback 127.0.0.1 requested no port", []string{"http://127.0.0.1:8787/cb"}, "http://127.0.0.1/cb", true},
		{"loopback localhost different port", []string{"http://localhost:8787/cb"}, "http://localhost:54123/cb", true},
		{"loopback localhost no port either side", []string{"http://localhost/cb"}, "http://localhost/cb", true},
		{"loopback ipv6 different port", []string{"http://[::1]:8787/cb"}, "http://[::1]:9000/cb", true},
		{"loopback ipv6 no port either side", []string{"http://[::1]/cb"}, "http://[::1]/cb", true},
		{"localhost vs 127.0.0.1 refused", []string{"http://localhost:8787/cb"}, "http://127.0.0.1:8787/cb", false},
		{"127.0.0.1 vs localhost refused", []string{"http://127.0.0.1/cb"}, "http://localhost/cb", false},
		{"LOCALHOST refused", []string{"http://localhost/cb"}, "http://LOCALHOST/cb", false},
		{"loopback path trailing slash refused", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1/cb/", false},
		{"loopback path different refused", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1:9000/other", false},
		{"loopback extra query refused", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1/cb?x=1", false},
		{"loopback query exact with other port", []string{"http://127.0.0.1/cb?x=1"}, "http://127.0.0.1:9000/cb?x=1", true},
		{"loopback query value differs refused", []string{"http://127.0.0.1/cb?x=1"}, "http://127.0.0.1/cb?x=2", false},
		{"loopback fragment refused", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1/cb#x", false},
		{"loopback http to https refused", []string{"http://127.0.0.1/cb"}, "https://127.0.0.1/cb", false},
		{"loopback non-loopback host refused", []string{"http://127.0.0.1/cb"}, "http://example.test/cb", false},
		{"loopback userinfo refused", []string{"http://127.0.0.1/cb"}, "http://user@127.0.0.1/cb", false},
		{"no registered uris refused", nil, "http://127.0.0.1/cb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MCPRedirectURIMatches(tc.registered, tc.requested); got != tc.want {
				t.Fatalf("MCPRedirectURIMatches(%v, %q) = %v, want %v", tc.registered, tc.requested, got, tc.want)
			}
		})
	}
}
