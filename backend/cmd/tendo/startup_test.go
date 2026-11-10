package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestHealthcheckProbesSpecificAndWildcardListenAddresses pins that the
// readiness probe respects a specific bind address but falls back to loopback
// for a wildcard one, which cannot be dialled directly.
func TestHealthcheckProbesSpecificAndWildcardListenAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Errorf("healthcheck probed %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("test server address: %v", err)
	}
	for _, listenAddr := range []string{
		net.JoinHostPort("127.0.0.1", port),
		":" + port,
		net.JoinHostPort("0.0.0.0", port),
		net.JoinHostPort("::", port),
	} {
		t.Run(listenAddr, func(t *testing.T) {
			t.Setenv("TENDO_LISTEN_ADDR", listenAddr)
			if err := healthcheck(); err != nil {
				t.Fatalf("healthcheck failed for %q: %v", listenAddr, err)
			}
		})
	}
}

// TestEnsureRoleRequiresAdministratorURL pins the operator guidance: without
// administrator credentials the role-only subcommand refuses to run instead of
// falling back to the unprivileged runtime connection.
func TestEnsureRoleRequiresAdministratorURL(t *testing.T) {
	unsetEnv(t,
		"TENDO_TEST_CLOCK_NOW",
		"TENDO_TEST_CLOCK_ACK",
		"TENDO_TRUSTED_PROXY_CIDRS",
		"TENDO_DB_TIMEOUT",
		"TENDO_SHUTDOWN_TIMEOUT",
		"TENDO_SETUP_TOKEN",
		"TENDO_OIDC_ISSUER",
		"TENDO_OIDC_CLIENT_ID",
		"TENDO_OIDC_CLIENT_SECRET",
		"TENDO_OIDC_CLIENT_SECRET_FILE",
		"TENDO_OIDC_DISPLAY_NAME",
	)
	t.Setenv("DATABASE_URL", "postgres://tendo:runtime-secret@127.0.0.1:5432/tendo?sslmode=disable")
	t.Setenv("TENDO_DATABASE_ADMIN_URL", "")
	t.Setenv("TENDO_PUBLIC_URL", "http://127.0.0.1")
	t.Setenv("TENDO_LISTEN_ADDR", "127.0.0.1:8080")
	err := ensureRole()
	if err == nil || err.Error() != "TENDO_DATABASE_ADMIN_URL is required to bootstrap the runtime role" {
		t.Fatalf("err=%v want missing administrator URL refusal", err)
	}
}

// unsetEnv removes optional settings for the duration of the test so ambient
// values cannot silently change the configuration under test.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { _ = os.Setenv(key, value) })
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
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
