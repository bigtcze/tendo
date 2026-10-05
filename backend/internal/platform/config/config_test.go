package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadFrom(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/tendo?sslmode=verify-full"}
	for _, tc := range []struct {
		name string
		set  map[string]string
		bad  bool
	}{
		{name: "defaults"},
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(base)+len(tc.set))
			for k, v := range base {
				values[k] = v
			}
			for k, v := range tc.set {
				values[k] = v
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
			if cfg.ListenAddr == "" || cfg.DatabaseURL == "" {
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
