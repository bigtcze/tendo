package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

func TestParseUsersArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		login string
		ok    bool
	}{
		{name: "space separated", args: []string{"set-password", "--login", "owner_1"}, login: "owner_1", ok: true},
		{name: "empty", ok: false},
		{name: "missing verb", args: []string{"owner_1"}, ok: false},
		{name: "unknown verb", args: []string{"reset", "--login", "owner_1"}, ok: false},
		{name: "missing login", args: []string{"set-password", "--login"}, ok: false},
		{name: "empty login", args: []string{"set-password", "--login", ""}, ok: false},
		{name: "positional login", args: []string{"set-password", "owner_1"}, ok: false},
		{name: "unknown flag", args: []string{"set-password", "--login", "owner_1", "--password", "secret"}, ok: false},
		{name: "extra argument", args: []string{"set-password", "--login", "owner_1", "other"}, ok: false},
		{name: "duplicate login", args: []string{"set-password", "--login", "owner_1", "--login", "other"}, ok: false},
		{name: "equals form is not accepted", args: []string{"set-password", "--login=owner_1"}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			login, ok := parseUsersArgs(tc.args)
			if login != tc.login || ok != tc.ok {
				t.Fatalf("login=%q ok=%v, want %q/%v", login, ok, tc.login, tc.ok)
			}
		})
	}
}

// runUsersForTest captures both streams so a test can assert what an operator
// would see.
func runUsersForTest(t *testing.T, args []string, getenv func(string) string, prompt passwordPrompt) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runUsers(args, getenv, prompt, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunUsersRefusesBadArgumentsBeforePrompting(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"set-password"},
		{"reset", "--login", "owner_1"},
		{"set-password", "--login", "owner_1", "extra"},
		{"set-password", "--login=owner_1"},
	} {
		prompted := false
		code, _, stderr := runUsersForTest(t, args, func(string) string { return "postgres://unused" }, func() (string, error) {
			prompted = true
			return "a brand new long password", nil
		})
		if code != 2 || prompted {
			t.Fatalf("args=%v exit=%d prompted=%v, want 2/false", args, code, prompted)
		}
		if !strings.Contains(stderr, usersUsage) {
			t.Fatalf("args=%v stderr=%q, want the usage line", args, stderr)
		}
	}
}

func TestRunUsersStopsBeforePersistenceOnPromptOrPolicyFailure(t *testing.T) {
	getenvCalls := 0
	getenv := func(string) string {
		getenvCalls++
		return "postgres://unused"
	}
	if code, _, _ := runUsersForTest(t, []string{"set-password", "--login", "owner_1"}, getenv, func() (string, error) {
		return "", errors.New("passwords do not match")
	}); code != 1 {
		t.Fatalf("prompt failure exit=%d, want 1", code)
	}
	if code, _, _ := runUsersForTest(t, []string{"set-password", "--login", "owner_1"}, getenv, func() (string, error) {
		return "short", nil
	}); code != 1 {
		t.Fatalf("weak password exit=%d, want 1", code)
	}
	if getenvCalls != 0 {
		t.Fatalf("password was sent toward the database: getenv calls=%d", getenvCalls)
	}
}

// The prompted password must never be echoed, including on the failure paths an
// operator is most likely to hit. The value is distinctive so a substring check
// is meaningful.
func TestRunUsersNeverPrintsThePromptedPassword(t *testing.T) {
	const secret = "distinctive-prompted-secret-value"
	getenvCalled := false
	getenv := func(string) string {
		getenvCalled = true
		return ""
	}
	for _, tc := range []struct {
		name     string
		password string
		prompt   error
	}{
		{name: "prompt mismatch", password: "", prompt: errors.New("passwords do not match")},
		{name: "policy failure", password: "leaky-1"},
		{name: "valid password but no database", password: secret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenvCalled = false
			code, stdout, stderr := runUsersForTest(t, []string{"set-password", "--login", "owner_1"}, getenv, func() (string, error) {
				return tc.password, tc.prompt
			})
			if code != 1 {
				t.Fatalf("exit=%d, want 1", code)
			}
			if tc.password == "" && getenvCalled {
				t.Fatal("a failed prompt still reached the database step")
			}
			for _, stream := range []string{stdout, stderr} {
				if tc.password != "" && strings.Contains(stream, tc.password) {
					t.Fatalf("output leaked the prompted password: %q", stream)
				}
			}
			if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
				t.Fatalf("output leaked the prompted password: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

// reportPasswordResetError is the only place that turns a persistence failure
// into operator text, so the no-secret guarantee is pinned there as well.
//
// The default branch is not covered here: it logs through slog and carries only
// the wrapped error, so it prints no operator text that could fail this check.
func TestReportPasswordResetErrorNeverPrintsThePassword(t *testing.T) {
	for _, err := range []error{
		identityapp.ErrNotFound,
		identityapp.ErrNoLocalPassword,
		&identityapp.ValidationError{Field: "password", Code: "invalid_length"},
		security.ErrPasswordWorkLimit,
	} {
		var stderr bytes.Buffer
		reportPasswordResetError(err, &stderr)
		if stderr.Len() == 0 {
			t.Fatalf("error %v printed no operator message", err)
		}
		if strings.Contains(stderr.String(), "password_hash") || strings.Contains(stderr.String(), "$argon2") {
			t.Fatalf("error %v printed credential material: %q", err, stderr.String())
		}
	}
}

func TestRunUsersRequiresDatabaseForAValidPassword(t *testing.T) {
	calls := 0
	code, _, _ := runUsersForTest(t, []string{"set-password", "--login", "owner_1"}, func(string) string {
		calls++
		return ""
	}, func() (string, error) { return "a brand new long password", nil })
	if code != 1 || calls != 1 {
		t.Fatalf("missing DATABASE_URL exit=%d getenv calls=%d, want 1/1", code, calls)
	}
}
