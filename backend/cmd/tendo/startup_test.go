package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func administratorConfig() config.Config {
	return config.Config{
		DatabaseURL:      "postgres://tendo:runtime-secret@127.0.0.1:1/tendo?sslmode=disable",
		DatabaseAdminURL: "postgres://admin:admin-secret@127.0.0.1:1/tendo?sslmode=disable",
		ListenAddr:       "127.0.0.1:0",
		PublicURL:        "http://127.0.0.1",
	}
}

// TestStartupDoesNotListenWhenPrivilegedPhaseFails pins that a failed role
// bootstrap or migration exits with the original actionable message and never
// opens a listener or the serving pool.
func TestStartupDoesNotListenWhenPrivilegedPhaseFails(t *testing.T) {
	for _, failure := range []error{
		errors.New("database administrator connection failed"),
		errors.New("database has a dirty migration"),
		errors.New("database schema is newer than this application"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			listened, opened := false, false
			steps := startupSteps{
				loadConfig: func() (config.Config, error) { return administratorConfig(), nil },
				prepare:    func(context.Context, config.Config) error { return failure },
				openPool: func(context.Context, config.Config) (*pgxpool.Pool, error) {
					opened = true
					return nil, errors.New("unexpected runtime pool")
				},
				verify: func(context.Context, *pgxpool.Pool) error {
					t.Error("runtime pool verified after the privileged phase failed")
					return nil
				},
				listen: func(string) (net.Listener, error) {
					listened = true
					return nil, errors.New("listener opened")
				},
			}
			err := steps.run()
			if !errors.Is(err, failure) {
				t.Fatalf("err=%v want the unmasked %v", err, failure)
			}
			if opened {
				t.Fatal("serving pool opened before the privileged phase succeeded")
			}
			if listened {
				t.Fatal("HTTP listener opened before startup succeeded")
			}
		})
	}
}

func TestStartupDoesNotListenWhenRuntimeRoleViolatesRestriction(t *testing.T) {
	prepared := false
	listened := false
	steps := startupSteps{
		loadConfig: func() (config.Config, error) {
			return config.Config{
				DatabaseURL: "postgres://tendo:runtime-secret@127.0.0.1:1/tendo?sslmode=disable",
				ListenAddr:  "127.0.0.1:0",
				PublicURL:   "http://127.0.0.1",
			}, nil
		},
		prepare: func(context.Context, config.Config) error {
			prepared = true
			return nil
		},
		openPool: func(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
			return pgxpool.New(ctx, cfg.DatabaseURL)
		},
		verify: func(context.Context, *pgxpool.Pool) error {
			return errors.New("database serving role is not restricted")
		},
		listen: func(string) (net.Listener, error) {
			listened = true
			return nil, errors.New("listener opened")
		},
	}
	err := steps.run()
	if err == nil || err.Error() != "database serving role is not restricted" {
		t.Fatalf("err=%v want serving role refusal", err)
	}
	if prepared {
		t.Fatal("privileged phase ran without TENDO_DATABASE_ADMIN_URL")
	}
	if listened {
		t.Fatal("HTTP listener opened with an unverified serving role")
	}
}

func TestPrepareDatabaseBoundsUnreachableAdministrator(t *testing.T) {
	err := prepareDatabase(context.Background(), administratorConfig())
	if err == nil || err.Error() != "database administrator connection failed" {
		t.Fatalf("err=%v want administrator connection failure", err)
	}
	for _, secret := range []string{"admin-secret", "runtime-secret", "postgres://"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestHealthcheckRequiresReadyStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "ready", status: http.StatusOK},
		{name: "unready", status: http.StatusServiceUnavailable, wantErr: true},
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health/ready" {
					t.Errorf("healthcheck probed %q", r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			t.Setenv("TENDO_LISTEN_ADDR", strings.TrimPrefix(server.URL, "http://"))
			err := healthcheck()
			if tc.wantErr && err == nil {
				t.Fatal("healthcheck accepted a non-200 response")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("healthcheck rejected readiness: %v", err)
			}
		})
	}
}

func TestHealthcheckRejectsInvalidListenAddress(t *testing.T) {
	for _, addr := range []string{"localhost", "127.0.0.1:", ":"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("TENDO_LISTEN_ADDR", addr)
			err := healthcheck()
			if err == nil || err.Error() != "invalid TENDO_LISTEN_ADDR" {
				t.Fatalf("err=%v want invalid listen address", err)
			}
		})
	}
}

func TestHealthcheckReportsUnavailableEndpoint(t *testing.T) {
	t.Setenv("TENDO_LISTEN_ADDR", "127.0.0.1:1")
	if err := healthcheck(); err == nil || err.Error() != "health endpoint unavailable" {
		t.Fatalf("err=%v want unavailable endpoint", err)
	}
}
