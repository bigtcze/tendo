package config

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// These characterization tests pin the fail-fast validation order and the
// exact operator-facing messages of LoadFrom, so that the staged loaders keep
// reporting the first invalid setting in the documented order.

const validDatabaseURL = "postgres://user:db-secret@localhost:5432/tendo?sslmode=verify-full"

func lookupFrom(values map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
}

func TestLoadFromErrorMessagesInValidationOrder(t *testing.T) {
	// Each case starts from every earlier stage being valid and every later
	// stage being invalid, so only the stage under test may report.
	allLaterInvalid := map[string]string{
		"TENDO_PUBLIC_URL":          "ftp://bad",
		"TENDO_OIDC_CLIENT_ID":      "orphan",
		"TENDO_TRUSTED_PROXY_CIDRS": "bad",
		"TENDO_TEST_CLOCK_NOW":      "bad",
		"TENDO_TEST_CLOCK_ACK":      "isolated-e2e-only",
		"TENDO_LISTEN_ADDR":         "bad",
		"TENDO_DB_TIMEOUT":          "bad",
		"TENDO_SHUTDOWN_TIMEOUT":    "bad",
		"TENDO_SETUP_TOKEN":         "bad",
	}
	stages := []struct {
		name  string
		valid map[string]string
		bad   map[string]string
		want  string
	}{
		{"database missing", nil, map[string]string{}, "DATABASE_URL is required"},
		{"database blank", nil, map[string]string{"DATABASE_URL": " "}, "DATABASE_URL is required"},
		{"database scheme", nil, map[string]string{"DATABASE_URL": "mysql://u:p@h/db?sslmode=disable"}, "DATABASE_URL must be a valid PostgreSQL URI with credentials and database"},
		{"database no path", nil, map[string]string{"DATABASE_URL": "postgres://u:p@h/?sslmode=disable"}, "DATABASE_URL must be a valid PostgreSQL URI with credentials and database"},
		{"database empty user", nil, map[string]string{"DATABASE_URL": "postgres://:p@h/db?sslmode=disable"}, "DATABASE_URL must be a valid PostgreSQL URI with credentials and database"},
		{"database query", nil, map[string]string{"DATABASE_URL": "postgres://u:p@h/db?sslmode=%zz"}, "DATABASE_URL must contain a valid query"},
		{"database ssl alias", nil, map[string]string{"DATABASE_URL": "postgres://u:p@h/db?sslmode=disable&ssl=1"}, "DATABASE_URL must set exactly one sslmode and must not use ssl"},
		{"database sslmode value", nil, map[string]string{"DATABASE_URL": "postgres://u:p@h/db?sslmode=strict"}, "DATABASE_URL must set a valid sslmode"},
		{"public missing", map[string]string{"DATABASE_URL": validDatabaseURL}, map[string]string{"TENDO_PUBLIC_URL": ""}, "TENDO_PUBLIC_URL is required"},
		{"public origin", map[string]string{"DATABASE_URL": validDatabaseURL}, nil, "TENDO_PUBLIC_URL must be an absolute HTTP(S) root origin"},
		{"oidc orphan", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost"}, nil, "OIDC settings require TENDO_OIDC_ISSUER"},
		{"proxies", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": ""}, nil, "invalid TENDO_TRUSTED_PROXY_CIDRS"},
		{"test clock", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": "", "TENDO_TRUSTED_PROXY_CIDRS": ""}, nil, "TENDO_TEST_CLOCK_NOW must be canonical whole-second UTC RFC3339 ending in Z"},
		{"listen", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": "", "TENDO_TRUSTED_PROXY_CIDRS": "", "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z"}, nil, "invalid TENDO_LISTEN_ADDR"},
		{"db timeout", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": "", "TENDO_TRUSTED_PROXY_CIDRS": "", "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_LISTEN_ADDR": ":8080"}, nil, "invalid TENDO_DB_TIMEOUT"},
		{"shutdown timeout", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": "", "TENDO_TRUSTED_PROXY_CIDRS": "", "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_LISTEN_ADDR": ":8080", "TENDO_DB_TIMEOUT": "3s"}, nil, "invalid TENDO_SHUTDOWN_TIMEOUT"},
		{"setup token", map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost", "TENDO_OIDC_CLIENT_ID": "", "TENDO_TRUSTED_PROXY_CIDRS": "", "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_LISTEN_ADDR": ":8080", "TENDO_DB_TIMEOUT": "3s", "TENDO_SHUTDOWN_TIMEOUT": "20"}, nil, "TENDO_SETUP_TOKEN must be canonical standard base64 encoding of 32 bytes"},
	}
	for _, tc := range stages {
		t.Run(tc.name, func(t *testing.T) {
			values := merge(allLaterInvalid, tc.valid)
			for k, v := range tc.bad {
				values[k] = v
			}
			if tc.name == "database missing" {
				delete(values, "DATABASE_URL")
			}
			_, err := LoadFrom(lookupFrom(values))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestLoadFromTestClockMessages(t *testing.T) {
	base := map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost"}
	const pair = "TENDO_TEST_CLOCK_NOW and TENDO_TEST_CLOCK_ACK must both be set and nonempty"
	for _, tc := range []struct {
		name string
		set  map[string]string
		want string
	}{
		{"ack only", map[string]string{"TENDO_TEST_CLOCK_ACK": "isolated-e2e-only"}, pair},
		{"now only", map[string]string{"TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z"}, pair},
		{"blank now", map[string]string{"TENDO_TEST_CLOCK_NOW": " ", "TENDO_TEST_CLOCK_ACK": "wrong"}, pair},
		{"blank ack", map[string]string{"TENDO_TEST_CLOCK_NOW": "bad", "TENDO_TEST_CLOCK_ACK": " "}, pair},
		{"wrong ack before bad instant", map[string]string{"TENDO_TEST_CLOCK_NOW": "bad", "TENDO_TEST_CLOCK_ACK": "wrong"}, "TENDO_TEST_CLOCK_ACK must equal isolated-e2e-only"},
		{"lowercase z", map[string]string{"TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59z", "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only"}, "TENDO_TEST_CLOCK_NOW must be canonical whole-second UTC RFC3339 ending in Z"},
		{"bad instant before public URL", map[string]string{"TENDO_TEST_CLOCK_NOW": "bad", "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only", "TENDO_PUBLIC_URL": "https://tendo.test"}, "TENDO_TEST_CLOCK_NOW must be canonical whole-second UTC RFC3339 ending in Z"},
		{"public URL before proxies", map[string]string{"TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only", "TENDO_PUBLIC_URL": "http://192.0.2.1", "TENDO_TRUSTED_PROXY_CIDRS": "127.0.0.1/32"}, "TENDO_PUBLIC_URL must use HTTP and localhost or a literal loopback IP when the test clock is enabled"},
		{"proxies", map[string]string{"TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only", "TENDO_TRUSTED_PROXY_CIDRS": "127.0.0.1/32"}, "TENDO_TRUSTED_PROXY_CIDRS must be empty when the test clock is enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFrom(lookupFrom(merge(base, tc.set)))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestLoadFromDurationsAndListen(t *testing.T) {
	base := map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "http://localhost"}
	for _, tc := range []struct {
		name string
		set  map[string]string
		want string
	}{
		{"zero duration", map[string]string{"TENDO_DB_TIMEOUT": "0"}, "TENDO_DB_TIMEOUT must be positive and no greater than 10s"},
		{"fractional plain seconds", map[string]string{"TENDO_DB_TIMEOUT": "1.5"}, "invalid TENDO_DB_TIMEOUT"},
		{"negative duration", map[string]string{"TENDO_DB_TIMEOUT": "-1s"}, "TENDO_DB_TIMEOUT must be positive and no greater than 10s"},
		{"plain seconds over max", map[string]string{"TENDO_DB_TIMEOUT": "11"}, "invalid TENDO_DB_TIMEOUT"},
		{"shutdown over max", map[string]string{"TENDO_SHUTDOWN_TIMEOUT": "61s"}, "TENDO_SHUTDOWN_TIMEOUT must be positive and no greater than 1m0s"},
		{"listen spaced host", map[string]string{"TENDO_LISTEN_ADDR": " localhost:80"}, "invalid TENDO_LISTEN_ADDR"},
		{"listen empty port", map[string]string{"TENDO_LISTEN_ADDR": "localhost:"}, "invalid TENDO_LISTEN_ADDR"},
		{"listen port zero", map[string]string{"TENDO_LISTEN_ADDR": ":0"}, "invalid TENDO_LISTEN_ADDR"},
		{"listen named port", map[string]string{"TENDO_LISTEN_ADDR": ":http"}, "invalid TENDO_LISTEN_ADDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFrom(lookupFrom(merge(base, tc.set)))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestLoadFromCompleteValidConfiguration(t *testing.T) {
	secretFile := t.TempDir() + "/secret"
	if err := os.WriteFile(secretFile, []byte("file-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL":                  validDatabaseURL,
		"TENDO_PUBLIC_URL":              "https://Tendo.Test:443/",
		"TENDO_OIDC_ISSUER":             "https://issuer.test/realm",
		"TENDO_OIDC_CLIENT_ID":          "client",
		"TENDO_OIDC_CLIENT_SECRET_FILE": secretFile,
		"TENDO_OIDC_DISPLAY_NAME":       "Pocket ID",
		"TENDO_TRUSTED_PROXY_CIDRS":     " 192.0.2.7/24 , 2001:db8::1/32",
		"TENDO_LISTEN_ADDR":             "127.0.0.1:9000",
		"TENDO_DB_TIMEOUT":              "1500ms",
		"TENDO_SHUTDOWN_TIMEOUT":        "60",
		"TENDO_SETUP_TOKEN":             "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		DatabaseURL:       validDatabaseURL,
		ListenAddr:        "127.0.0.1:9000",
		PublicURL:         "https://tendo.test",
		TrustedProxyCIDRs: []string{"192.0.2.0/24", "2001:db8::/32"},
		DBTimeout:         1500 * time.Millisecond,
		ShutdownTimeout:   60 * time.Second,
		SetupToken:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		OIDCIssuer:        "https://issuer.test/realm",
		OIDCClientID:      "client",
		OIDCClientSecret:  "file-secret",
		OIDCDisplayName:   "Pocket ID",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config=%+v\nwant  =%+v", cfg, want)
	}
}

func TestOIDCMessagesInValidationOrder(t *testing.T) {
	base := map[string]string{"DATABASE_URL": validDatabaseURL, "TENDO_PUBLIC_URL": "https://tendo.test"}
	enabled := map[string]string{"TENDO_OIDC_ISSUER": "https://issuer.test", "TENDO_OIDC_CLIENT_ID": "client", "TENDO_OIDC_CLIENT_SECRET": "s"}
	for _, tc := range []struct {
		name   string
		set    map[string]string
		public string
		want   string
	}{
		{"display without issuer", map[string]string{"TENDO_OIDC_ISSUER": "", "TENDO_OIDC_CLIENT_ID": "", "TENDO_OIDC_CLIENT_SECRET": "", "TENDO_OIDC_DISPLAY_NAME": "x"}, "", "OIDC settings require TENDO_OIDC_ISSUER"},
		{"secret file without issuer", map[string]string{"TENDO_OIDC_ISSUER": "", "TENDO_OIDC_CLIENT_ID": "", "TENDO_OIDC_CLIENT_SECRET": "", "TENDO_OIDC_CLIENT_SECRET_FILE": "/nonexistent"}, "", "OIDC settings require TENDO_OIDC_ISSUER"},
		{"issuer without scheme prefix", map[string]string{"TENDO_OIDC_ISSUER": "HTTPS://issuer.test"}, "", "TENDO_OIDC_ISSUER must be an absolute HTTP(S) URL without userinfo, query, or fragment"},
		{"issuer before client id", map[string]string{"TENDO_OIDC_ISSUER": "https://u@issuer.test", "TENDO_OIDC_CLIENT_ID": ""}, "", "TENDO_OIDC_ISSUER must be an absolute HTTP(S) URL without userinfo, query, or fragment"},
		{"https issuer with http public", map[string]string{}, "http://localhost", ""},
		{"http issuer with https public", map[string]string{"TENDO_OIDC_ISSUER": "http://issuer.test"}, "", "TENDO_OIDC_ISSUER scheme is incompatible with TENDO_PUBLIC_URL"},
		{"scheme before client id", map[string]string{"TENDO_OIDC_ISSUER": "http://issuer.test", "TENDO_OIDC_CLIENT_ID": ""}, "", "TENDO_OIDC_ISSUER scheme is incompatible with TENDO_PUBLIC_URL"},
		{"blank client id", map[string]string{"TENDO_OIDC_CLIENT_ID": " "}, "", "TENDO_OIDC_CLIENT_ID is required and must be at most 512 bytes"},
		{"client id before secret", map[string]string{"TENDO_OIDC_CLIENT_ID": "", "TENDO_OIDC_CLIENT_SECRET": ""}, "", "TENDO_OIDC_CLIENT_ID is required and must be at most 512 bytes"},
		{"both secrets", map[string]string{"TENDO_OIDC_CLIENT_SECRET_FILE": "/nonexistent"}, "", "set only one of TENDO_OIDC_CLIENT_SECRET and TENDO_OIDC_CLIENT_SECRET_FILE"},
		{"unreadable file", map[string]string{"TENDO_OIDC_CLIENT_SECRET": "", "TENDO_OIDC_CLIENT_SECRET_FILE": "/nonexistent/secret"}, "", "TENDO_OIDC_CLIENT_SECRET_FILE must be readable and nonempty"},
		{"missing secret", map[string]string{"TENDO_OIDC_CLIENT_SECRET": ""}, "", "OIDC client secret is required and must be at most 4096 bytes"},
		{"blank secret", map[string]string{"TENDO_OIDC_CLIENT_SECRET": " \n"}, "", "OIDC client secret is required and must be at most 4096 bytes"},
		{"secret before display", map[string]string{"TENDO_OIDC_CLIENT_SECRET": "", "TENDO_OIDC_DISPLAY_NAME": ""}, "", "OIDC client secret is required and must be at most 4096 bytes"},
		{"blank display", map[string]string{"TENDO_OIDC_DISPLAY_NAME": "  "}, "", "TENDO_OIDC_DISPLAY_NAME must be printable and at most 80 bytes"},
		{"invalid UTF-8 display", map[string]string{"TENDO_OIDC_DISPLAY_NAME": "a\xffb"}, "", "TENDO_OIDC_DISPLAY_NAME must be printable and at most 80 bytes"},
		{"display at byte bound", map[string]string{"TENDO_OIDC_DISPLAY_NAME": "ěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěě"}, "", ""},
		{"display over byte bound", map[string]string{"TENDO_OIDC_DISPLAY_NAME": "ěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěěa"}, "", "TENDO_OIDC_DISPLAY_NAME must be printable and at most 80 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := merge(merge(base, enabled), tc.set)
			if tc.public != "" {
				values["TENDO_PUBLIC_URL"] = tc.public
			}
			_, err := LoadFrom(lookupFrom(values))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	cfg, err := LoadFrom(lookupFrom(merge(base, enabled)))
	if err != nil || cfg.OIDCDisplayName != "OpenID Connect" || cfg.OIDCClientSecret != "s" {
		t.Fatalf("default display: cfg=%+v err=%v", cfg, err)
	}
	cfg, err = LoadFrom(lookupFrom(merge(base, map[string]string{"TENDO_OIDC_DISPLAY_NAME": ""})))
	if err != nil || cfg.OIDCIssuer != "" || cfg.OIDCDisplayName != "" {
		t.Fatalf("disabled with empty display: cfg=%+v err=%v", cfg, err)
	}
}
