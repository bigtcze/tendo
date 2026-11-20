package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// TestMCPRequestLogsCarryNoBearerToken runs /mcp refusals through the production
// composition with a request logger that writes to a buffer. The bearer token
// sent by the client must never appear in the log. Each request here is refused
// before a tool runs (a ping, a token in the query, a malformed Authorization);
// no successful tools/call is made, so the log covers refusals only.
func TestMCPRequestLogsCarryNoBearerToken(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	app, cfg := newEnabledMCPApp(t)
	const token = "BEARERSECRETAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	target := cfg.PublicURL + "/mcp"
	serveMCPRequest(app, http.MethodPost, target, "", cfg.PublicURL, "Bearer "+token, mcpBodyPing)
	serveMCPRequest(app, http.MethodPost, target+"?access_token="+token, "", cfg.PublicURL, "", mcpBodyPing)
	serveMCPRequest(app, http.MethodPost, target, "", cfg.PublicURL, "Bearer "+token+" extra", mcpBodyPing)

	out := logs.String()
	if out == "" {
		t.Fatal("no request log was written")
	}
	if strings.Contains(out, token) {
		t.Fatalf("log leaks the bearer token:\n%s", out)
	}
	if !strings.Contains(out, "route=/mcp") {
		t.Fatalf("log lacks the /mcp route:\n%s", out)
	}
}
