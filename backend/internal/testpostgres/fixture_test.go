package testpostgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type scriptedExec struct {
	results []error
	calls   []string
}

func (s *scriptedExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	s.calls = append(s.calls, sql)
	if len(s.results) == 0 {
		return pgconn.CommandTag{}, errors.New("unexpected extra call")
	}
	err := s.results[0]
	s.results = s.results[1:]
	return pgconn.CommandTag{}, err
}

func pgErr(code string) error { return &pgconn.PgError{Code: code, Message: "scripted " + code} }

func TestDropOwnedDatabaseFallsBackToPlainDropWhenForceCannotTerminateSuperuserProcess(t *testing.T) {
	exec := &scriptedExec{results: []error{pgErr("42501"), nil}}
	if err := dropOwnedDatabase(context.Background(), exec, "tendo_upgrade_x"); err != nil {
		t.Fatalf("drop failed: %v", err)
	}
	want := []string{`DROP DATABASE "tendo_upgrade_x" WITH (FORCE)`, `DROP DATABASE "tendo_upgrade_x"`}
	if strings.Join(exec.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls=%q want %q", exec.calls, want)
	}
}

func TestDropOwnedDatabaseRetriesWhileDatabaseInUse(t *testing.T) {
	exec := &scriptedExec{results: []error{pgErr("42501"), pgErr("55006"), nil}}
	if err := dropOwnedDatabase(context.Background(), exec, "d"); err != nil {
		t.Fatalf("drop failed: %v", err)
	}
	if len(exec.calls) != 3 || !strings.HasSuffix(exec.calls[2], "WITH (FORCE)") {
		t.Fatalf("calls=%q", exec.calls)
	}
}

func TestDropOwnedDatabaseReturnsOtherErrorsImmediately(t *testing.T) {
	exec := &scriptedExec{results: []error{pgErr("3D000")}}
	err := dropOwnedDatabase(context.Background(), exec, "d")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "3D000" || len(exec.calls) != 1 {
		t.Fatalf("err=%v calls=%q", err, exec.calls)
	}
}

func TestDropOwnedDatabaseStopsAtDeadline(t *testing.T) {
	results := make([]error, 0, 200)
	for range 100 {
		results = append(results, pgErr("42501"), pgErr("55006"))
	}
	exec := &scriptedExec{results: results}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	err := dropOwnedDatabase(ctx, exec, "d")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "55006") {
		t.Fatalf("err=%v", err)
	}
}

func TestDropOwnedDatabaseRetriesForceWhenInUse(t *testing.T) {
	exec := &scriptedExec{results: []error{pgErr("55006"), nil}}
	if err := dropOwnedDatabase(context.Background(), exec, "d"); err != nil {
		t.Fatalf("drop failed: %v", err)
	}
	if len(exec.calls) != 2 || !strings.HasSuffix(exec.calls[0], "WITH (FORCE)") || !strings.HasSuffix(exec.calls[1], "WITH (FORCE)") {
		t.Fatalf("calls=%q", exec.calls)
	}
}

func TestDropOwnedDatabaseReturnsPlainDropErrorsImmediately(t *testing.T) {
	for _, code := range []string{"42501", "3D000"} {
		exec := &scriptedExec{results: []error{pgErr("42501"), pgErr(code)}}
		err := dropOwnedDatabase(context.Background(), exec, "d")
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != code || len(exec.calls) != 2 {
			t.Fatalf("code=%s err=%v calls=%q", code, err, exec.calls)
		}
	}
}
