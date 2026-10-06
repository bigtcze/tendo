package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimePool interface {
	httpx.Pinger
	Close()
}

type runtimeResources struct {
	server *http.Server
	pool   runtimePool
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "migrate" {
		if err := migrate(); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("runtime failed", "error", err)
		os.Exit(1)
	}
}

func migrate() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return errors.New("migration database initialization failed")
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("migration database unavailable")
	}
	return database.Migrate(ctx, pool)
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return errors.New("database pool initialization failed")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := database.ValidateSchema(startupCtx, pool); err != nil {
		startupCancel()
		pool.Close()
		return errors.New("database schema validation failed")
	}
	startupCancel()
	draining := make(chan struct{})
	routes := chi.NewRouter()
	identityRepository := identitypostgres.New(pool, func(queries *householddb.Queries) identityapp.OwnerHouseholdService {
		return household.NewBootstrapService(householdpostgres.NewBootstrapRepository(queries))
	})
	identityhttp.New(identityapp.NewSetupService(identityRepository), cfg.SetupToken).Register(routes)
	sessions, err := identityapp.NewSessionService(identityRepository, time.Now)
	if err != nil {
		pool.Close()
		return err
	}
	identityhttp.NewSession(sessions, cfg.PublicURL).Register(routes)
	app := httpx.NewAppWithRoutes(pool, cfg.DBTimeout, draining, httpx.OriginPolicy{PublicURL: cfg.PublicURL, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, func(r chi.Router) { r.Mount("/", routes) })
	srv := newRuntimeServer(cfg.ListenAddr, app, cfg.DBTimeout)
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		pool.Close()
		return err
	}
	return serve(ctx, listener, runtimeResources{server: srv, pool: pool}, draining, cfg.ShutdownTimeout)
}

func newRuntimeServer(addr string, handler http.Handler, serviceTimeout time.Duration) *http.Server {
	writeTimeout := 15*time.Second + serviceTimeout + 5*time.Second
	if writeTimeout < 25*time.Second {
		writeTimeout = 25 * time.Second
	}
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}

func serve(ctx context.Context, listener net.Listener, resources runtimeResources, draining chan struct{}, shutdownTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- resources.server.Serve(listener) }()
	select {
	case <-ctx.Done():
		close(draining)
		deadline := time.Now().Add(shutdownTimeout)
		shutdownCtx, cancel := context.WithDeadline(context.Background(), deadline)
		shutdownErr := resources.server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = resources.server.Close()
		}
		shutdownErr = boundedPoolClose(resources.pool, time.Until(deadline), shutdownErr)
		if shutdownErr != nil {
			return errors.New("runtime shutdown failed")
		}
		select {
		case err := <-serveErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return errors.New("HTTP server stopped unexpectedly")
			}
		default:
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return boundedPoolClose(resources.pool, shutdownTimeout, nil)
		}
		_ = resources.server.Close()
		return boundedPoolClose(resources.pool, shutdownTimeout, errors.New("HTTP server stopped unexpectedly"))
	}
}

func boundedPoolClose(pool runtimePool, timeout time.Duration, primary error) error {
	closed := make(chan struct{})
	go func() { pool.Close(); close(closed) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return primary
	case <-timer.C:
		if primary != nil {
			return primary
		}
		return errors.New("runtime shutdown failed")
	}
}
