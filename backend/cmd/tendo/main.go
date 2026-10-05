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

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
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
	if err := run(); err != nil {
		slog.Error("runtime failed", "error", err)
		os.Exit(1)
	}
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
	draining := make(chan struct{})
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: httpx.NewHealth(pool, cfg.DBTimeout, draining), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: cfg.DBTimeout + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		pool.Close()
		return err
	}
	return serve(ctx, listener, runtimeResources{server: srv, pool: pool}, draining, cfg.ShutdownTimeout)
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
