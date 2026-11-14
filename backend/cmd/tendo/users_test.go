package main

import (
	"errors"
	"testing"
)

func TestParseUsersArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		login string
		ok    bool
	}{
		{name: "space separated", args: []string{"set-password", "--login", "owner_1"}, login: "owner_1", ok: true},
		{name: "equals form", args: []string{"set-password", "--login=owner_1"}, login: "owner_1", ok: true},
		{name: "empty", ok: false},
		{name: "missing verb", args: []string{"owner_1"}, ok: false},
		{name: "unknown verb", args: []string{"reset", "--login", "owner_1"}, ok: false},
		{name: "missing login", args: []string{"set-password", "--login"}, ok: false},
		{name: "empty login", args: []string{"set-password", "--login="}, ok: false},
		{name: "positional login", args: []string{"set-password", "owner_1"}, ok: false},
		{name: "unknown flag", args: []string{"set-password", "--login", "owner_1", "--password", "secret"}, ok: false},
		{name: "extra argument", args: []string{"set-password", "--login", "owner_1", "other"}, ok: false},
		{name: "duplicate login", args: []string{"set-password", "--login", "owner_1", "--login=other"}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			login, ok := parseUsersArgs(tc.args)
			if login != tc.login || ok != tc.ok {
				t.Fatalf("login=%q ok=%v, want %q/%v", login, ok, tc.login, tc.ok)
			}
		})
	}
}

func TestRunUsersRefusesBadArgumentsBeforePrompting(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"set-password"},
		{"reset", "--login", "owner_1"},
		{"set-password", "--login", "owner_1", "extra"},
	} {
		prompted := false
		code := runUsers(args, func(string) string { return "postgres://unused" }, func() (string, error) {
			prompted = true
			return "a brand new long password", nil
		})
		if code != 2 || prompted {
			t.Fatalf("args=%v exit=%d prompted=%v, want 2/false", args, code, prompted)
		}
	}
}

func TestRunUsersStopsBeforePersistenceOnPromptOrPolicyFailure(t *testing.T) {
	getenvCalls := 0
	getenv := func(string) string {
		getenvCalls++
		return "postgres://unused"
	}
	if code := runUsers([]string{"set-password", "--login", "owner_1"}, getenv, func() (string, error) {
		return "", errors.New("passwords do not match")
	}); code != 1 {
		t.Fatalf("prompt failure exit=%d, want 1", code)
	}
	if code := runUsers([]string{"set-password", "--login", "owner_1"}, getenv, func() (string, error) {
		return "short", nil
	}); code != 1 {
		t.Fatalf("weak password exit=%d, want 1", code)
	}
	if getenvCalls != 0 {
		t.Fatalf("password was sent toward the database: getenv calls=%d", getenvCalls)
	}
}

func TestRunUsersRequiresDatabaseForAValidPassword(t *testing.T) {
	calls := 0
	code := runUsers([]string{"set-password", "--login", "owner_1"}, func(string) string {
		calls++
		return ""
	}, func() (string, error) { return "a brand new long password", nil })
	if code != 1 || calls != 1 {
		t.Fatalf("missing DATABASE_URL exit=%d getenv calls=%d, want 1/1", code, calls)
	}
}
