package identity

import (
	"errors"
	"strings"
	"testing"
)

const mcpTestPublicHost = "tendo.example.test"

// TestValidateRedirectURIPolicy is the table of every accept and reject case in
// the redirect policy (decisions "Redirect policy", ADR 0023).
func TestValidateRedirectURIPolicy(t *testing.T) {
	long := "https://client.example.test/" + strings.Repeat("a", MCPMaxRedirectURILength)
	cases := []struct {
		name string
		uri  string
		ok   bool
	}{
		// https: any non-empty host, any port, never the public host.
		{"https plain", "https://client.example.test/callback", true},
		{"https with port", "https://client.example.test:8443/callback", true},
		{"https claude.ai", "https://claude.ai/api/mcp/auth_callback", true},
		{"https chatgpt", "https://chatgpt.com/connector_platform_oauth_redirect", true},
		{"https cursor", "https://www.cursor.com/agents/mcp/oauth/callback", true},
		{"https self redirect refused", "https://" + mcpTestPublicHost + "/callback", false},
		{"https self redirect uppercase refused", "https://TENDO.example.test/callback", false},
		{"https empty host", "https:///callback", false},
		{"https userinfo", "https://user@client.example.test/callback", false},

		// http: exact loopback literals only, any or no port.
		{"http 127.0.0.1", "http://127.0.0.1/callback", true},
		{"http 127.0.0.1 port", "http://127.0.0.1:8787/callback", true},
		{"http localhost", "http://localhost/callback", true},
		{"http localhost port", "http://localhost:54123/callback", true},
		{"http ipv6 loopback", "http://[::1]/callback", true},
		{"http ipv6 loopback port", "http://[::1]:9000/callback", true},
		{"http 127.1 refused", "http://127.1/callback", false},
		{"http hex loopback refused", "http://0x7f.0.0.1/callback", false},
		{"http other loopback refused", "http://127.0.0.2/callback", false},
		{"http mapped ipv6 refused", "http://[::ffff:127.0.0.1]/callback", false},
		{"http localhost trailing dot refused", "http://localhost./callback", false},
		{"http uppercase localhost refused", "http://LOCALHOST/callback", false},
		{"http example refused", "http://example.com/callback", false},

		// Private-use schemes: lowercase, hierarchical, no port, not denied.
		{"cursor scheme", "cursor://anysphere.cursor-mcp/oauth/callback", true},
		{"private-use no dot", "myapp:/callback", true},
		{"private-use with host", "myapp://callback/path", true},
		{"private-use uppercase refused", "MyApp:/callback", false},
		{"private-use single letter refused", "a:/callback", false},
		{"private-use opaque refused", "myapp:foo", false},
		{"private-use with port refused", "myapp://host:8080/callback", false},
		{"private-use empty host refused", "myapp:///callback", false},
		{"javascript refused", "javascript:/alert", false},
		{"data refused", "data:/text", false},
		{"file refused", "file:///etc/passwd", false},
		{"chrome prefix refused", "chrome-extension://abc/callback", false},
		{"chrome exact refused", "chrome:/callback", false},
		{"ms prefix refused", "ms-app:/callback", false},
		{"search-ms refused", "search-ms:/callback", false},
		{"http scheme handled separately", "HTTP://127.0.0.1/callback", false},

		// Shape: count is checked by ValidateRedirectURIs; here the per-URI shape.
		{"space refused", "https://client.example.test/call back", false},
		{"backslash refused", "https://client.example.test/a\\b", false},
		{"angle bracket refused", "https://client.example.test/<x>", false},
		{"non ascii refused", "https://client.example.test/caf\u00e9", false},
		{"control refused", "https://client.example.test/a\x01b", false},
		{"wildcard refused", "https://client.example.test/*", false},
		{"fragment refused", "https://client.example.test/callback#frag", false},
		{"empty refused", "", false},
		{"over 2048 bytes refused", long, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRedirectURI(tc.uri, mcpTestPublicHost)
			if tc.ok && err != nil {
				t.Fatalf("ValidateRedirectURI(%q) = %v, want accepted", tc.uri, err)
			}
			if !tc.ok && !errors.Is(err, ErrMCPInvalidRedirectURI) {
				t.Fatalf("ValidateRedirectURI(%q) = %v, want ErrMCPInvalidRedirectURI", tc.uri, err)
			}
		})
	}
}

// TestValidateRedirectURIsWholeRegistration covers the count bound, duplicates
// and whole-registration rejection: one invalid URI refuses everything.
func TestValidateRedirectURIsWholeRegistration(t *testing.T) {
	valid := "https://client.example.test/callback"
	cases := []struct {
		name string
		uris []string
		ok   bool
	}{
		{"one valid", []string{valid}, true},
		{"five valid", []string{valid + "1", valid + "2", valid + "3", valid + "4", valid + "5"}, true},
		{"empty list", nil, false},
		{"six refused", []string{valid + "1", valid + "2", valid + "3", valid + "4", valid + "5", valid + "6"}, false},
		{"duplicate refused", []string{valid, valid}, false},
		{"one invalid refuses all", []string{valid, "http://example.com/callback"}, false},
		{"self redirect among valid refuses all", []string{valid, "https://" + mcpTestPublicHost + "/x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRedirectURIs(tc.uris, mcpTestPublicHost)
			if tc.ok != (err == nil) {
				t.Fatalf("ValidateRedirectURIs(%v) = %v, want accepted=%v", tc.uris, err, tc.ok)
			}
		})
	}
}

// TestNormalizeClientName covers the client_name rules: trimmed, default when
// absent, refused for control and format characters, and 1..100 code points.
func TestNormalizeClientName(t *testing.T) {
	ptr := func(s string) *string { return &s }
	cases := []struct {
		name string
		in   *string
		want string
		ok   bool
	}{
		{"absent defaults", nil, MCPDefaultClientName, true},
		{"empty defaults", ptr(""), MCPDefaultClientName, true},
		{"blank defaults", ptr("   "), MCPDefaultClientName, true},
		{"trimmed", ptr("  Claude Code  "), "Claude Code", true},
		{"100 code points", ptr(strings.Repeat("a", 100)), strings.Repeat("a", 100), true},
		{"101 code points refused", ptr(strings.Repeat("a", 101)), "", false},
		{"multibyte counted by code point", ptr(strings.Repeat("é", 100)), strings.Repeat("é", 100), true},
		{"right to left override refused", ptr("evil\u202Egnp.exe"), "", false},
		{"zero width space refused", ptr("Cur\u200Bsor"), "", false},
		{"bidi isolate refused", ptr("a\u2066b"), "", false},
		{"nul control refused", ptr("a\x00b"), "", false},
		{"newline control refused", ptr("a\nb"), "", false},
		{"tab control refused", ptr("a\tb"), "", false},
		{"invalid utf8 refused", ptr("a\xffb"), "", false},
		// NFC: a decomposed name is stored in the composed form, and the length
		// is counted after normalization (decisions, "Client metadata").
		{"decomposed e acute stored composed", ptr("Cafe\u0301"), "Caf\u00e9", true},
		{"composed input unchanged", ptr("Caf\u00e9"), "Caf\u00e9", true},
		{"100 decomposed e acute accepted after NFC", ptr(strings.Repeat("e\u0301", 100)), strings.Repeat("\u00e9", 100), true},
		{"101 decomposed e acute refused after NFC", ptr(strings.Repeat("e\u0301", 101)), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeClientName(tc.in)
			if tc.ok {
				if err != nil || got != tc.want {
					t.Fatalf("NormalizeClientName = %q, %v; want %q", got, err, tc.want)
				}
				return
			}
			if !errors.Is(err, ErrMCPInvalidClientMetadata) {
				t.Fatalf("NormalizeClientName = %q, %v; want ErrMCPInvalidClientMetadata", got, err)
			}
		})
	}
}

// TestDecodeClientRegistrationAndMetadata covers the lenient RFC 7591 decoder
// and the metadata policy: unknown keys ignored, scope ignored, the wrong JSON
// types refused, and grant and response types checked.
func TestDecodeClientRegistrationAndMetadata(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"minimal", `{"redirect_uris":["https://client.example.test/cb"]}`, true},
		{"unknown keys ignored", `{"redirect_uris":["https://client.example.test/cb"],"logo_uri":"https://x/y","jwks":{"keys":[]},"contacts":5}`, true},
		{"scope ignored", `{"redirect_uris":["https://client.example.test/cb"],"scope":"anything at all"}`, true},
		{"auth method substituted", `{"redirect_uris":["https://client.example.test/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, true},
		{"grant types with authorization_code", `{"redirect_uris":["https://client.example.test/cb"],"grant_types":["authorization_code","refresh_token"]}`, true},
		{"grant types without authorization_code refused", `{"redirect_uris":["https://client.example.test/cb"],"grant_types":["client_credentials"]}`, false},
		{"response types with code", `{"redirect_uris":["https://client.example.test/cb"],"response_types":["code"]}`, true},
		{"response types without code refused", `{"redirect_uris":["https://client.example.test/cb"],"response_types":["token"]}`, false},
		{"redirect_uris missing refused", `{"client_name":"x"}`, false},
		{"redirect_uris wrong type refused", `{"redirect_uris":"https://client.example.test/cb"}`, false},
		{"client_name wrong type refused", `{"redirect_uris":["https://client.example.test/cb"],"client_name":5}`, false},
		{"grant_types wrong type refused", `{"redirect_uris":["https://client.example.test/cb"],"grant_types":"authorization_code"}`, false},
		{"duplicate key refused", `{"redirect_uris":["https://client.example.test/cb"],"redirect_uris":["https://client.example.test/x"]}`, false},
		{"two objects refused", `{"redirect_uris":["https://client.example.test/cb"]}{}`, false},
		{"array body refused", `["https://client.example.test/cb"]`, false},
		{"invalid utf8 refused", "{\"redirect_uris\":[\"https://client.example.test/\xff\"]}", false},
		{"over 16 KiB refused", `{"redirect_uris":["https://client.example.test/cb"],"pad":"` + strings.Repeat("a", MCPMaxRegistrationBytes) + `"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, err := DecodeClientRegistration([]byte(tc.body))
			if err == nil {
				_, err = ValidateClientRegistration(reg, mcpTestPublicHost)
			}
			if tc.ok && err != nil {
				t.Fatalf("registration refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("registration accepted, want refused")
			}
		})
	}
}

// TestRealClientRegistrationBodiesAccepted replays representative bodies from
// real MCP clients through the decode and policy functions. Each must be
// accepted. The HTTP-level 201 for the same bodies is pinned in
// internal/identity/httpapi.
func TestRealClientRegistrationBodiesAccepted(t *testing.T) {
	bodies := map[string]string{
		"claude.ai":    `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"scope":"claudeai","token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`,
		"claude code":  `{"client_name":"Claude Code","redirect_uris":["http://localhost:54123/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
		"cursor":       `{"client_name":"Cursor","redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback","https://www.cursor.com/agents/mcp/oauth/callback","http://localhost:8787/callback"],"application_type":"web","scope":"mcp offline_access","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"client_secret_post"}`,
		"chatgpt":      `{"client_name":"ChatGPT","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"client_secret_post"}`,
		"chatgpt none": `{"client_name":"ChatGPT","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"none"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			reg, err := DecodeClientRegistration([]byte(body))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if _, err := ValidateClientRegistration(reg, mcpTestPublicHost); err != nil {
				t.Fatalf("real body refused: %v", err)
			}
		})
	}
}
