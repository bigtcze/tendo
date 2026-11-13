package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestParseInstanceAdminArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		login string
		ok    bool
	}{
		{name: "space separated", args: []string{"grant", "--login", "owner_1"}, login: "owner_1", ok: true},
		{name: "equals form", args: []string{"grant", "--login=owner_1"}, login: "owner_1", ok: true},
		{name: "empty", ok: false},
		{name: "missing verb", args: []string{"owner_1"}, ok: false},
		{name: "unknown verb", args: []string{"revoke", "--login", "owner_1"}, ok: false},
		{name: "missing login", args: []string{"grant", "--login"}, ok: false},
		{name: "empty login", args: []string{"grant", "--login="}, ok: false},
		{name: "positional login", args: []string{"grant", "owner_1"}, ok: false},
		{name: "unknown flag", args: []string{"grant", "--login", "owner_1", "--force"}, ok: false},
		{name: "extra argument", args: []string{"grant", "--login", "owner_1", "other"}, ok: false},
		{name: "duplicate login", args: []string{"grant", "--login", "owner_1", "--login=other"}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			login, ok := parseInstanceAdminArgs(tc.args)
			if login != tc.login || ok != tc.ok {
				t.Fatalf("login=%q ok=%v, want %q/%v", login, ok, tc.login, tc.ok)
			}
		})
	}
}

func TestRunInstanceAdminExitCodes(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"grant"},
		{"revoke", "--login", "owner_1"},
		{"grant", "--login", "owner_1", "extra"},
	} {
		if code := runInstanceAdmin(args, func(string) string { return "postgres://unused" }); code != 2 {
			t.Fatalf("args=%v exit=%d, want 2", args, code)
		}
	}
	calls := 0
	code := runInstanceAdmin([]string{"grant", "--login", "owner_1"}, func(string) string {
		calls++
		return ""
	})
	if code != 1 || calls != 1 {
		t.Fatalf("missing DATABASE_URL exit=%d getenv calls=%d, want 1/1", code, calls)
	}
}

type fakeInstanceAdminHealth struct {
	missing bool
	err     error
}

func (f fakeInstanceAdminHealth) SetupCompleteWithoutInstanceAdmin(context.Context) (bool, error) {
	return f.missing, f.err
}

// TestWarnInstanceAdminHealth pins that the startup check warns for a missing
// administrator and for an unavailable lookup, and that the lookup failure
// warning carries no error detail and never fails startup.
func TestWarnInstanceAdminHealth(t *testing.T) {
	for _, tc := range []struct {
		name            string
		health          fakeInstanceAdminHealth
		wantMissing     bool
		wantUnavailable bool
	}{
		{name: "missing administrator", health: fakeInstanceAdminHealth{missing: true}, wantMissing: true},
		{name: "administrator present", health: fakeInstanceAdminHealth{}},
		{name: "lookup failed", health: fakeInstanceAdminHealth{err: errors.New("unavailable")}, wantUnavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })

			warnInstanceAdminHealth(context.Background(), tc.health)

			output := logs.String()
			if got := strings.Contains(output, "no instance administrator is configured"); got != tc.wantMissing {
				t.Fatalf("missing warning present=%v logs=%s", got, output)
			}
			if got := strings.Contains(output, "instance administrator state unavailable"); got != tc.wantUnavailable {
				t.Fatalf("unavailable warning present=%v logs=%s", got, output)
			}
			if strings.Contains(output, "error=") || strings.Contains(output, "err=") {
				t.Fatalf("lookup warning leaked error detail: %s", output)
			}
		})
	}
}
