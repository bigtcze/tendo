package config

import (
	"testing"
)

// mcpBase is a valid installation whose only MCP-relevant setting is the
// public origin; each case adds TENDO_MCP_ENABLED on top.
func mcpBase(public string) map[string]string {
	return map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": public}
}

func TestLoadMCPEnabledDefaultsToFalse(t *testing.T) {
	for _, values := range []map[string]string{
		mcpBase("https://tendo.test"),
		merge(mcpBase("https://tendo.test"), map[string]string{"TENDO_MCP_ENABLED": ""}),
	} {
		cfg, err := LoadFrom(lookupFrom(values))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MCPEnabled {
			t.Fatal("MCPEnabled must default to false")
		}
	}
}

func TestLoadMCPEnabledAcceptsExactBooleans(t *testing.T) {
	cases := []struct {
		public string
		value  string
		want   bool
	}{
		{"https://tendo.test", "true", true},
		{"https://tendo.test", "false", false},
		{"http://localhost:8080", "true", true},
		{"http://127.0.0.1", "true", true},
		{"http://[::1]", "true", true},
		{"http://localhost:8080", "false", false},
	}
	for _, tc := range cases {
		values := merge(mcpBase(tc.public), map[string]string{"TENDO_MCP_ENABLED": tc.value})
		cfg, err := LoadFrom(lookupFrom(values))
		if err != nil {
			t.Fatalf("%s %q: %v", tc.public, tc.value, err)
		}
		if cfg.MCPEnabled != tc.want {
			t.Fatalf("%s %q: MCPEnabled=%v want %v", tc.public, tc.value, cfg.MCPEnabled, tc.want)
		}
	}
}

func TestLoadMCPEnabledRejectsNonCanonicalValues(t *testing.T) {
	for _, value := range []string{"TRUE", "1", "yes", " true", "true "} {
		values := merge(mcpBase("https://tendo.test"), map[string]string{"TENDO_MCP_ENABLED": value})
		_, err := LoadFrom(lookupFrom(values))
		if err == nil || err.Error() != "TENDO_MCP_ENABLED must be true or false" {
			t.Fatalf("value %q: err=%v", value, err)
		}
	}
}

func TestLoadMCPEnabledRequiresSecureOrLoopbackOrigin(t *testing.T) {
	values := merge(mcpBase("http://tendo.lan"), map[string]string{"TENDO_MCP_ENABLED": "true"})
	_, err := LoadFrom(lookupFrom(values))
	want := "TENDO_MCP_ENABLED requires an HTTPS TENDO_PUBLIC_URL or a loopback HTTP origin"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
}

func TestLoadMCPEnabledFalseAllowsPlainHTTPOrigin(t *testing.T) {
	values := merge(mcpBase("http://tendo.lan"), map[string]string{"TENDO_MCP_ENABLED": "false"})
	if _, err := LoadFrom(lookupFrom(values)); err != nil {
		t.Fatalf("disabled MCP must not constrain the origin: %v", err)
	}
}

func TestLoadMCPEnabledStageOrder(t *testing.T) {
	// A later stage's invalid setting must not mask the MCP stage, and the MCP
	// stage must run after the public URL stage: an invalid public URL is
	// reported first even when MCP is also invalid.
	values := merge(mcpBase("http://tendo.lan"), map[string]string{
		"TENDO_MCP_ENABLED": "true",
		"TENDO_LISTEN_ADDR": "bad",
		"TENDO_DB_TIMEOUT":  "bad",
		"TENDO_SETUP_TOKEN": "bad",
	})
	_, err := LoadFrom(lookupFrom(values))
	want := "TENDO_MCP_ENABLED requires an HTTPS TENDO_PUBLIC_URL or a loopback HTTP origin"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want the MCP stage error before later stages", err)
	}
	invalidPublic := merge(mcpBase("ftp://bad"), map[string]string{"TENDO_MCP_ENABLED": "bad"})
	_, err = LoadFrom(lookupFrom(invalidPublic))
	if err == nil || err.Error() != "TENDO_PUBLIC_URL must be an absolute HTTP(S) root origin" {
		t.Fatalf("err=%v want the public URL error before the MCP stage", err)
	}
}
