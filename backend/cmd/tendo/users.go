package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

const usersUsage = "usage: tendo users set-password --login <login>"

// parseUsersArgs accepts exactly `set-password --login <login>`. Like the
// instance-admin parser it is deliberate instead of the flag package, so no
// argument other than the login can influence the command and a password can
// never be read from argv.
func parseUsersArgs(args []string) (string, bool) {
	if len(args) == 0 || args[0] != "set-password" {
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

// passwordPrompt reads the new password interactively. It is injected so the
// command's argument, error and reporting behavior is testable without a
// terminal.
type passwordPrompt func() (string, error)

// terminalPasswordPrompt asks twice on the controlling terminal and never
// echoes. A non-interactive stdin is refused instead of reading a password from
// a pipe that might be recorded.
func terminalPasswordPrompt() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("an interactive terminal is required")
	}
	state, err := term.GetState(fd)
	if err != nil {
		return "", errors.New("password input failed")
	}
	stop := restoreTerminalOnSignal(fd, state)
	defer stop()
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("password input failed")
	}
	fmt.Fprint(os.Stderr, "Repeat new password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("password input failed")
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}

// interruptedExitCode is the conventional 128+SIGINT status.
const interruptedExitCode = 130

// restoreTerminalOnSignal puts the terminal back into its saved mode when the
// operator interrupts or the process is terminated while echo is disabled, so
// an aborted prompt never leaves the shell without echo. The returned func
// stops watching once the prompt finished normally.
func restoreTerminalOnSignal(fd int, state *term.State) func() {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
			_ = term.Restore(fd, state)
			fmt.Fprintln(os.Stderr)
			os.Exit(interruptedExitCode)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// runUsers is the `users` subcommand entry point. It returns the process exit
// code so argument parsing and prompt failures are testable without a database.
func runUsers(args []string, getenv func(string) string, prompt passwordPrompt) int {
	login, ok := parseUsersArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, usersUsage)
		return 2
	}
	password, err := prompt()
	if err != nil {
		fmt.Fprintf(os.Stderr, "password reset failed: %v\n", err)
		return 1
	}
	if err = identityapp.ValidateLocalPassword(password); err != nil {
		fmt.Fprintln(os.Stderr, "password reset failed: the password must be 15-128 characters")
		return 1
	}
	url := getenv("DATABASE_URL")
	if url == "" {
		slog.Error("password reset failed", "error", "DATABASE_URL is required")
		return 1
	}
	if err = setUserPassword(url, login, password); err != nil {
		reportPasswordResetError(err)
		return 1
	}
	return 0
}

// setUserPassword uses the runtime role only: the recovery command runs inside
// the application container and must never need administrator credentials.
func setUserPassword(url, login, password string) error {
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
	service, err := identityapp.NewAdminPasswordService(identitypostgres.NewPasswordChangeRepository(pool), time.Now)
	if err != nil {
		return err
	}
	service.SetPasswordHasher(security.HashPassword)
	reset, err := service.SetPassword(ctx, login, password)
	if err != nil {
		return err
	}
	slog.Info("local password reset", "user_id", reset.UserID, "sessions_revoked", reset.Sessions)
	fmt.Printf("updated password for %s and signed out %d session(s)\n", login, reset.Sessions)
	return nil
}

// reportPasswordResetError prints the operator-actionable cause. It never
// includes the password or a hash.
func reportPasswordResetError(err error) {
	var validation *identityapp.ValidationError
	switch {
	case errors.Is(err, identityapp.ErrNotFound):
		fmt.Fprintln(os.Stderr, "password reset failed: no account with that login")
	case errors.Is(err, identityapp.ErrNoLocalPassword):
		fmt.Fprintln(os.Stderr, "password reset failed: the account signs in with OIDC only and has no local password")
	case errors.As(err, &validation):
		fmt.Fprintln(os.Stderr, "password reset failed: the password must be 15-128 characters")
	case errors.Is(err, security.ErrPasswordWorkLimit):
		fmt.Fprintln(os.Stderr, "password reset failed: password hashing is busy, try again")
	default:
		slog.Error("password reset failed", "error", err)
	}
}
