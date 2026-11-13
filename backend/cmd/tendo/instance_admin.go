package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

const instanceAdminUsage = "usage: tendo instance-admin grant --login <login>"

// parseInstanceAdminArgs accepts exactly `grant --login <login>`. It is a
// deliberate parser instead of the flag package so an unknown flag or a
// positional argument can never be mistaken for a login.
func parseInstanceAdminArgs(args []string) (string, bool) {
	if len(args) == 0 || args[0] != "grant" {
		return "", false
	}
	rest := args[1:]
	login := ""
	for len(rest) > 0 {
		switch {
		case rest[0] == "--login":
			if login != "" || len(rest) < 2 {
				return "", false
			}
			login, rest = rest[1], rest[2:]
		case strings.HasPrefix(rest[0], "--login="):
			if login != "" {
				return "", false
			}
			login, rest = strings.TrimPrefix(rest[0], "--login="), rest[1:]
		default:
			return "", false
		}
	}
	return login, login != ""
}

// runInstanceAdmin is the `instance-admin` subcommand entry point. It returns
// the process exit code so argument parsing is testable without a database.
func runInstanceAdmin(args []string, getenv func(string) string) int {
	login, ok := parseInstanceAdminArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, instanceAdminUsage)
		return 2
	}
	url := getenv("DATABASE_URL")
	if url == "" {
		slog.Error("instance administrator grant failed", "error", "DATABASE_URL is required")
		return 1
	}
	if err := grantInstanceAdmin(url, login); err != nil {
		slog.Error("instance administrator grant failed", "error", err)
		return 1
	}
	return 0
}

// grantInstanceAdmin uses the runtime role only: the recovery command runs
// inside the application container and must never need administrator
// credentials.
func grantInstanceAdmin(url, login string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return errors.New("database initialization failed")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("database unavailable")
	}
	if err = database.ValidateSchema(ctx, pool); err != nil {
		return errors.New("database schema validation failed")
	}
	result, err := household.NewInstanceAdmins(householdpostgres.NewInstanceAdminRepository(pool)).Grant(ctx, login)
	if err != nil {
		return err
	}
	if result.Granted {
		slog.Info("instance administrator granted", "user_id", result.UserID)
		fmt.Printf("granted instance administrator to %s\n", login)
		return nil
	}
	fmt.Printf("%s is already an instance administrator\n", login)
	return nil
}

// instanceAdminHealth is the startup read model. It is an interface so the boot
// sequence can be tested without a database.
type instanceAdminHealth interface {
	SetupCompleteWithoutInstanceAdmin(context.Context) (bool, error)
}

// warnMissingInstanceAdmin tells the operator how to recover an installation
// that completed setup without any instance administrator. It never fails
// startup: a lookup error is only a warning diagnostic without error detail.
func warnMissingInstanceAdmin(ctx context.Context, pool *pgxpool.Pool) {
	warnInstanceAdminHealth(ctx, household.NewInstanceAdmins(householdpostgres.NewInstanceAdminRepository(pool)))
}

func warnInstanceAdminHealth(ctx context.Context, health instanceAdminHealth) {
	missing, err := health.SetupCompleteWithoutInstanceAdmin(ctx)
	if err != nil {
		slog.Warn("instance administrator state unavailable")
		return
	}
	if missing {
		slog.Warn("no instance administrator is configured; run `tendo instance-admin grant --login <login>` inside the app container")
	}
}
