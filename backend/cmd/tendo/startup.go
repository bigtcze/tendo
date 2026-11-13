package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/webui"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// startupSteps is the boot sequence: configuration, the privileged database
// phase, the restricted pool, its isolation check, the instance-administrator
// warning, then the HTTP listener. Each step is replaceable so tests can pin
// that ordering without a database.
type startupSteps struct {
	loadConfig        func() (config.Config, error)
	prepare           func(context.Context, config.Config) error
	openPool          func(context.Context, config.Config) (*pgxpool.Pool, error)
	verify            func(context.Context, *pgxpool.Pool) error
	warnInstanceAdmin func(context.Context, *pgxpool.Pool)
	listen            func(string) (net.Listener, error)
}

func defaultStartupSteps() startupSteps {
	return startupSteps{
		loadConfig:        config.Load,
		prepare:           prepareDatabase,
		openPool:          openRuntimePool,
		verify:            verifyRuntimePool,
		warnInstanceAdmin: warnMissingInstanceAdmin,
		listen:            func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
	}
}

func run() error { return defaultStartupSteps().run() }

// run opens no listener until every earlier step succeeded, and serving uses
// only the restricted pool returned by openPool.
func (s startupSteps) run() error {
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.DatabaseAdminURL != "" {
		if err := s.prepare(ctx, cfg); err != nil {
			return err
		}
	}
	pool, err := s.openPool(ctx, cfg)
	if err != nil {
		return err
	}
	if err := s.verify(ctx, pool); err != nil {
		pool.Close()
		return err
	}
	if s.warnInstanceAdmin != nil {
		s.warnInstanceAdmin(ctx, pool)
	}
	return s.serve(ctx, cfg, pool)
}

func (s startupSteps) serve(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) error {
	draining := make(chan struct{})
	routes := chi.NewRouter()
	if err := registerAPI(routes, cfg, pool); err != nil {
		pool.Close()
		return err
	}
	app := httpx.NewAppWithUI(pool, cfg.DBTimeout, draining, httpx.OriginPolicy{PublicURL: cfg.PublicURL, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, func(r chi.Router) { r.Mount("/", routes) }, webui.Handler())
	srv := newRuntimeServer(cfg.ListenAddr, app, cfg.DBTimeout)
	listener, err := s.listen(cfg.ListenAddr)
	if err != nil {
		pool.Close()
		return err
	}
	return serve(ctx, listener, runtimeResources{server: srv, pool: pool}, draining, cfg.ShutdownTimeout)
}

// prepareDatabase runs the two privileged steps exactly once at startup:
// runtime role bootstrap (idempotent, never alters an existing role) and the
// embedded migrations. Migration errors keep their existing actionable
// messages so operators see a dirty or newer schema instead of a generic one.
func prepareDatabase(ctx context.Context, cfg config.Config) error {
	return withAdministratorPool(ctx, cfg, func(pool *pgxpool.Pool) error {
		if err := database.BootstrapRole(ctx, pool, cfg.DatabaseURL); err != nil {
			return err
		}
		return database.Migrate(ctx, pool)
	})
}

// ensureRole is the role-only subcommand used before restoring a backup into
// an empty database. It requires administrator credentials and never migrates.
func ensureRole() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseAdminURL == "" {
		return errors.New("TENDO_DATABASE_ADMIN_URL is required to bootstrap the runtime role")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return withAdministratorPool(ctx, cfg, func(pool *pgxpool.Pool) error {
		return database.BootstrapRole(ctx, pool, cfg.DatabaseURL)
	})
}

func withAdministratorPool(ctx context.Context, cfg config.Config, upgrade func(*pgxpool.Pool) error) error {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseAdminURL)
	if err != nil {
		return errors.New("invalid database administrator configuration")
	}
	poolCfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return errors.New("database administrator pool initialization failed")
	}
	defer pool.Close()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := pool.Ping(bounded); err != nil {
		return errors.New("database administrator connection failed")
	}
	return upgrade(pool)
}

func openRuntimePool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, errors.New("database pool initialization failed")
	}
	return pool, nil
}

// verifyRuntimePool refuses to serve with a role that is not restricted, then
// checks the schema version. The role check comes first so an elevated
// connection is reported as such rather than as a schema problem.
func verifyRuntimePool(ctx context.Context, pool *pgxpool.Pool) error {
	if err := database.ValidateServingRole(ctx, pool); err != nil {
		return err
	}
	if err := database.ValidateSchema(ctx, pool); err != nil {
		return errors.New("database schema validation failed")
	}
	return nil
}

// healthcheck backs the Compose healthcheck: the distroless image has no shell
// or curl, so the binary probes its own readiness endpoint over loopback.
// A wildcard listen address cannot be dialled, so the probe falls back to
// loopback while a specific bind address is honoured, including IPv6.
func healthcheck() error {
	listenAddr := ":8080"
	if raw := strings.TrimSpace(os.Getenv("TENDO_LISTEN_ADDR")); raw != "" {
		listenAddr = raw
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return errors.New("invalid TENDO_LISTEN_ADDR")
	}
	probeHost := host
	if probeHost == "" || unspecifiedHost(probeHost) {
		probeHost = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(probeHost, port) + "/health/ready")
	if err != nil {
		return errors.New("health endpoint unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned status %d", response.StatusCode)
	}
	return nil
}

// unspecifiedHost reports whether a listen host is an unspecified IP such as
// 0.0.0.0 or ::, which is bound but not a dialable destination.
func unspecifiedHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}
