package config

import (
	"strings"
	"testing"
	"time"
)

func TestCanonicalPublicURL(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		bad         bool
	}{
		{"http://LOCALHOST:80/", "http://localhost", false},
		{"https://EXAMPLE.test:443", "https://example.test", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", false},
		{"https://[2001:DB8::1]:443", "https://[2001:db8::1]", false},
		{"https://[2001:0DB8:0:0:0:0:0:1]", "https://[2001:db8::1]", false},
		{"https://[::ffff:192.0.2.1]", "https://[::ffff:192.0.2.1]", false},
		{"http://[::1]:8080", "http://[::1]:8080", false},
		{"", "", true},
		{"ftp://example.test", "", true},
		{"https://user@example.test", "", true},
		{"https://example.test/path", "", true},
		{"https://example.test?", "", true},
		{"https://example.test?x", "", true},
		{"https://example.test#", "", true},
		{"https://example.test/#", "", true},
		{"https://example.test:0", "", true},
		{"https://example.test:65536", "", true},
		{"https://example.test:", "", true},
		{"https://example.test:\t80", "", true},
		{"https://[::1%25eth0]", "", true},
		{"https://[::1", "", true},
		{"https://::1", "", true},
		{"https://[]", "", true},
		{"https://[example.test]", "", true},
		{"https://exa mple.test", "", true},
		{"https://example.test\\t", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := canonicalOrigin(tc.input)
			if tc.bad && err == nil || !tc.bad && (err != nil || got != tc.want) {
				t.Fatalf("canonicalOrigin(%q)=(%q,%v)", tc.input, got, err)
			}
		})
	}
}

func TestLoadFromProxyCIDRs(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/tendo?sslmode=verify-full", "TENDO_PUBLIC_URL": "http://localhost"}
	for _, tc := range []struct {
		input string
		want  int
		bad   bool
	}{
		{"", 0, false},
		{" \t ", 0, false},
		{"192.0.2.0/24,2001:db8::/32", 2, false},
		{",", 0, true},
		{"192.0.2.1", 0, true},
		{"invalid", 0, true},
	} {
		values := map[string]string{"DATABASE_URL": base["DATABASE_URL"], "TENDO_PUBLIC_URL": base["TENDO_PUBLIC_URL"], "TENDO_TRUSTED_PROXY_CIDRS": tc.input}
		cfg, err := LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
		if tc.bad && err == nil || !tc.bad && (err != nil || len(cfg.TrustedProxyCIDRs) != tc.want) {
			t.Fatalf("CIDRs %q: config=%+v err=%v", tc.input, cfg, err)
		}
	}
}

func TestTestClockConfiguration(t *testing.T) {
	const instant = "2026-01-15T22:59:59Z"
	base := map[string]string{"DATABASE_URL": "postgres://user:db-password@localhost:5432/tendo?sslmode=verify-full", "TENDO_PUBLIC_URL": "http://localhost:8080", "TENDO_SETUP_TOKEN": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	tests := []struct {
		name, public, now, ack, proxies string
		clockSet, ackSet, proxiesSet    bool
		want, wantErr                   string
	}{
		{name: "default nil"},
		{name: "localhost accepted", public: "http://localhost:8080", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, want: instant},
		{name: "ipv4 accepted", public: "http://127.0.0.1:8080", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, want: instant},
		{name: "ipv6 accepted", public: "http://[::1]:8080", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, want: instant},
		{name: "missing ack", now: instant, clockSet: true, wantErr: "TENDO_TEST_CLOCK_ACK"},
		{name: "missing instant", ack: "isolated-e2e-only", ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "empty instant", now: "", ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "empty ack", now: instant, ack: "", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_ACK"},
		{name: "wrong ack", now: instant, ack: "wrong", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_ACK"},
		{name: "invalid timestamp", now: "not-a-time", ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "fractional", now: "2026-01-15T22:59:59.1Z", ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "offset", now: "2026-01-15T23:59:59+01:00", ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "year zero", now: "0000-01-15T22:59:59Z", ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_TEST_CLOCK_NOW"},
		{name: "https localhost", public: "https://localhost", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_PUBLIC_URL"},
		{name: "https remote", public: "https://tendo.example", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_PUBLIC_URL"},
		{name: "remote http", public: "http://tendo.example", now: instant, ack: "isolated-e2e-only", clockSet: true, ackSet: true, wantErr: "TENDO_PUBLIC_URL"},
		{name: "trusted proxies", public: "http://localhost:8080", now: instant, ack: "isolated-e2e-only", proxies: "127.0.0.1/32", clockSet: true, ackSet: true, proxiesSet: true, wantErr: "TENDO_TRUSTED_PROXY_CIDRS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			if tc.public != "" {
				values["TENDO_PUBLIC_URL"] = tc.public
			}
			if tc.clockSet {
				values["TENDO_TEST_CLOCK_NOW"] = tc.now
			}
			if tc.ackSet {
				values["TENDO_TEST_CLOCK_ACK"] = tc.ack
			}
			if tc.proxiesSet {
				values["TENDO_TRUSTED_PROXY_CIDRS"] = tc.proxies
			}
			cfg, err := LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want error naming %s", err, tc.wantErr)
				}
				if strings.Contains(err.Error(), "db-password") || strings.Contains(err.Error(), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") {
					t.Fatalf("secret leaked: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if cfg.TestClockNow != nil {
					t.Fatalf("default clock=%v", cfg.TestClockNow)
				}
				return
			}
			want, _ := time.Parse(time.RFC3339, tc.want)
			if cfg.TestClockNow == nil || !cfg.TestClockNow.Equal(want) {
				t.Fatalf("clock=%v want %v", cfg.TestClockNow, want)
			}
		})
	}
}

func TestLoadFromDatabaseAdminURL(t *testing.T) {
	const runtimeURL = "postgres://tendo:runtime-secret@db.internal:5432/tendo?sslmode=verify-full"
	sameTarget := "TENDO_DATABASE_ADMIN_URL must target the same host, port and database as DATABASE_URL"
	for _, tc := range []struct {
		name  string
		admin string
		want  string
	}{
		{name: "absent"},
		{name: "blank", admin: " \t "},
		{name: "valid", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=verify-full"},
		{name: "postgresql scheme", admin: "postgresql://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=disable"},
		{name: "default port", admin: "postgres://tendo_admin:admin-secret@db.internal/tendo?sslmode=disable"},
		{name: "host case", admin: "postgres://tendo_admin:admin-secret@DB.Internal:5432/tendo?sslmode=disable"},
		{name: "different database", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/other?sslmode=disable", want: sameTarget},
		{name: "different host", admin: "postgres://tendo_admin:admin-secret@other.internal:5432/tendo?sslmode=disable", want: sameTarget},
		{name: "different port", admin: "postgres://tendo_admin:admin-secret@db.internal:6432/tendo?sslmode=disable", want: sameTarget},
		{name: "same user", admin: "postgres://tendo:admin-secret@db.internal:5432/tendo?sslmode=verify-full", want: "TENDO_DATABASE_ADMIN_URL must use a different user than DATABASE_URL"},
		{name: "missing credentials", admin: "postgres://db.internal:5432/tendo?sslmode=disable", want: "TENDO_DATABASE_ADMIN_URL must be a valid PostgreSQL URI with credentials and database"},
		{name: "missing database", admin: "postgres://tendo_admin:admin-secret@db.internal:5432?sslmode=disable", want: "TENDO_DATABASE_ADMIN_URL must be a valid PostgreSQL URI with credentials and database"},
		{name: "invalid scheme", admin: "mysql://tendo_admin:admin-secret@db.internal/tendo?sslmode=disable", want: "TENDO_DATABASE_ADMIN_URL must be a valid PostgreSQL URI with credentials and database"},
		{name: "missing sslmode", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo", want: "TENDO_DATABASE_ADMIN_URL must set exactly one sslmode and must not use ssl"},
		{name: "duplicate sslmode", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=disable&sslmode=verify-full", want: "TENDO_DATABASE_ADMIN_URL must set exactly one sslmode and must not use ssl"},
		{name: "ssl alias", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=disable&ssl=true", want: "TENDO_DATABASE_ADMIN_URL must set exactly one sslmode and must not use ssl"},
		{name: "invalid query", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=%zz", want: "TENDO_DATABASE_ADMIN_URL must contain a valid query"},
		{name: "invalid sslmode", admin: "postgres://tendo_admin:admin-secret@db.internal:5432/tendo?sslmode=strict", want: "TENDO_DATABASE_ADMIN_URL must set a valid sslmode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"DATABASE_URL": runtimeURL, "TENDO_PUBLIC_URL": "https://tendo.test"}
			if tc.admin != "" {
				values["TENDO_DATABASE_ADMIN_URL"] = tc.admin
			}
			cfg, err := LoadFrom(lookupFrom(values))
			if tc.want != "" {
				if err == nil || err.Error() != tc.want {
					t.Fatalf("err=%v want %q", err, tc.want)
				}
				if strings.Contains(err.Error(), "admin-secret") || strings.Contains(err.Error(), "runtime-secret") {
					t.Fatalf("secret leaked: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantAdmin := tc.admin
			if strings.TrimSpace(wantAdmin) == "" {
				wantAdmin = ""
			}
			if cfg.DatabaseAdminURL != wantAdmin {
				t.Fatalf("admin URL=%q want %q", cfg.DatabaseAdminURL, wantAdmin)
			}
		})
	}
}

func TestLoadFrom(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/tendo?sslmode=verify-full", "TENDO_PUBLIC_URL": "https://tendo.test"}
	for _, tc := range []struct {
		name           string
		set            map[string]string
		unsetPublicURL bool
		bad            bool
	}{
		{name: "defaults"},
		{name: "canonical setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}},
		{name: "empty setup token allowed", set: map[string]string{"TENDO_SETUP_TOKEN": ""}},
		{name: "short setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "AAAA"}, bad: true},
		{name: "malformed setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "not-base64"}, bad: true},
		{name: "noncanonical setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, bad: true},
		{name: "nonzero padding bits setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB="}, bad: true},
		{name: "33 byte setup token", set: map[string]string{"TENDO_SETUP_TOKEN": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, bad: true},
		{name: "listen ipv4", set: map[string]string{"TENDO_LISTEN_ADDR": "127.0.0.1:8081"}},
		{name: "listen ipv6", set: map[string]string{"TENDO_LISTEN_ADDR": "[::1]:8081"}},
		{name: "postgresql scheme", set: map[string]string{"DATABASE_URL": "postgresql://user:pass@localhost/db?sslmode=disable"}},
		{name: "allowed ssl", set: map[string]string{"DATABASE_URL": "postgres://user:pass@localhost/db?sslmode=disable"}},
		{name: "duplicate sslmode", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=verify-full&sslmode=disable"}, bad: true},
		{name: "ssl alias", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=verify-full&ssl=true"}, bad: true},
		{name: "missing sslmode", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db"}, bad: true},
		{name: "invalid query escape", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=%zz"}, bad: true},
		{name: "malformed additional query", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=disable&extra=%zz"}, bad: true},
		{name: "duplicate sslmode after malformed query", set: map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=disable&extra=%zz&sslmode=require"}, bad: true},
		{name: "invalid scheme", set: map[string]string{"DATABASE_URL": "mysql://u:p@localhost/db?sslmode=disable"}, bad: true},
		{name: "missing credentials", set: map[string]string{"DATABASE_URL": "postgres://localhost/db?sslmode=disable"}, bad: true},
		{name: "invalid port", set: map[string]string{"TENDO_LISTEN_ADDR": ":65536"}, bad: true},
		{name: "invalid listen", set: map[string]string{"TENDO_LISTEN_ADDR": "localhost"}, bad: true},
		{name: "timeout plain seconds", set: map[string]string{"TENDO_DB_TIMEOUT": "5"}},
		{name: "timeout overflow", set: map[string]string{"TENDO_DB_TIMEOUT": "9223372036854775807h"}, bad: true},
		{name: "timeout over max", set: map[string]string{"TENDO_SHUTDOWN_TIMEOUT": "61s"}, bad: true},
		{name: "empty public URL", set: map[string]string{"TENDO_PUBLIC_URL": ""}, bad: true},
		{name: "missing public URL variable", unsetPublicURL: true, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(base)+len(tc.set))
			for k, v := range base {
				values[k] = v
			}
			for k, v := range tc.set {
				values[k] = v
			}
			if tc.unsetPublicURL {
				delete(values, "TENDO_PUBLIC_URL")
			}
			cfg, err := LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
			if tc.bad {
				if err == nil {
					t.Fatal("expected invalid configuration")
				}
				if strings.Contains(err.Error(), "pass") {
					t.Fatal("error leaked database credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ListenAddr == "" || cfg.DatabaseURL == "" || cfg.PublicURL == "" {
				t.Fatalf("missing configuration: %+v", cfg)
			}
			if tc.name == "defaults" && (cfg.ListenAddr != ":8080" || cfg.DBTimeout != 2*time.Second || cfg.ShutdownTimeout != 10*time.Second) {
				t.Fatalf("unexpected defaults: %+v", cfg)
			}
			if tc.name == "timeout plain seconds" && cfg.DBTimeout != 5*time.Second {
				t.Fatalf("timeout=%s", cfg.DBTimeout)
			}
		})
	}
}
