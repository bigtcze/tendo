package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// memMCPStore is an in-memory registration store that also satisfies
// MCPRepository, so identity.NewMCPService finds the client store through its
// own type assertion, as it does in production. Issuance is never reached by
// these tests, so its methods fail closed.
type memMCPStore struct {
	mu      sync.Mutex
	clients []identity.MCPClient
}

func (m *memMCPStore) CreateClientWithinCap(_ context.Context, client identity.MCPClient, _ time.Time, cap int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.clients) >= cap {
		return false, nil
	}
	m.clients = append(m.clients, client)
	return true, nil
}

func (m *memMCPStore) WithMCPTransaction(context.Context, func(identity.MCPTransaction) error) error {
	return errors.New("registration tests must not open an issuance transaction")
}

func (m *memMCPStore) FindActiveAccessToken(context.Context, []byte, time.Time) (identity.MCPGrantRecord, error) {
	return identity.MCPGrantRecord{}, identity.ErrNotFound
}

// realMCPRouter builds the production MCP service (enabled, resource on the
// public host) over the in-memory store and mounts the real HTTP adapter.
func realMCPRouter(t *testing.T) http.Handler {
	t.Helper()
	service, err := identity.NewMCPService(&memMCPStore{}, identity.MCPOptions{Enabled: true, Resource: mcpPublicURL + "/mcp"}, func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return newMCPRouter(t, service, mcpPublicURL)
}

// TestRealClientBodiesRegisterOverHTTP replays the four real client bodies that
// the identity policy accepts through the HTTP adapter and the real service.
// Each must answer 201 with the RFC 7591 shape, the client's own name and
// redirects, and Tendo's fixed policy fields.
func TestRealClientBodiesRegisterOverHTTP(t *testing.T) {
	bodies := map[string]struct {
		body      string
		name      string
		redirects []any
	}{
		"claude.ai": {
			body:      `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"scope":"claudeai","token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`,
			name:      "Claude",
			redirects: []any{"https://claude.ai/api/mcp/auth_callback"},
		},
		"claude code": {
			body:      `{"client_name":"Claude Code","redirect_uris":["http://localhost:54123/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
			name:      "Claude Code",
			redirects: []any{"http://localhost:54123/callback"},
		},
		"cursor": {
			body:      `{"client_name":"Cursor","redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback","https://www.cursor.com/agents/mcp/oauth/callback","http://localhost:8787/callback"],"application_type":"web","scope":"mcp offline_access","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"client_secret_post"}`,
			name:      "Cursor",
			redirects: []any{"cursor://anysphere.cursor-mcp/oauth/callback", "https://www.cursor.com/agents/mcp/oauth/callback", "http://localhost:8787/callback"},
		},
		"chatgpt": {
			body:      `{"client_name":"ChatGPT","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"client_secret_post"}`,
			name:      "ChatGPT",
			redirects: []any{"https://chatgpt.com/connector_platform_oauth_redirect"},
		},
	}
	for name, tc := range bodies {
		t.Run(name, func(t *testing.T) {
			w := mcpServe(realMCPRouter(t), http.MethodPost, "/oauth/register", tc.body, jsonHeader(), "192.0.2.10:1000")
			if w.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["client_name"] != tc.name {
				t.Errorf("client_name=%v want %q", got["client_name"], tc.name)
			}
			if !jsonEqual(got["redirect_uris"], tc.redirects) {
				t.Errorf("redirect_uris=%v want %v", got["redirect_uris"], tc.redirects)
			}
			if id, _ := got["client_id"].(string); id == "" {
				t.Error("client_id missing")
			}
			if got["token_endpoint_auth_method"] != "none" || got["scope"] != "tendo:read tendo:write" {
				t.Errorf("fixed policy fields = %v", got)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control=%q", w.Header().Get("Cache-Control"))
			}
		})
	}
}

// TestRealRegistrationRefusalsOverHTTP pins the RFC error bodies that the real
// policy produces: a bad redirect and malformed JSON.
func TestRealRegistrationRefusalsOverHTTP(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		status  int
		errCode string
	}{
		{
			name:    "plain http redirect is refused",
			body:    `{"client_name":"Bad","redirect_uris":["http://client.example.test/cb"]}`,
			status:  http.StatusBadRequest,
			errCode: "invalid_redirect_uri",
		},
		{
			name:    "malformed json is refused",
			body:    `{"redirect_uris":[`,
			status:  http.StatusBadRequest,
			errCode: "invalid_client_metadata",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := mcpServe(realMCPRouter(t), http.MethodPost, "/oauth/register", tc.body, jsonHeader(), "192.0.2.11:1000")
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != tc.errCode || body["error_description"] == "" {
				t.Fatalf("body=%s, want RFC error %q", w.Body, tc.errCode)
			}
		})
	}
}
