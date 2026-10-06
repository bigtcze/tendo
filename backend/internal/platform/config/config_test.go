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
