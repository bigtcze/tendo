package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var migrations embed.FS

const currentVersion int64 = 10
const advisoryLock int64 = 784193214

type migrationConn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := pool.Acquire(bounded)
	if err != nil {
		return errors.New("migration database unavailable")
	}
	defer conn.Release()
	if _, err = conn.Exec(bounded, `SELECT pg_advisory_lock($1)`, advisoryLock); err != nil {
		return errors.New("migration lock failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, advisoryLock)
	}()
	if err = ensureMetadata(bounded, conn); err != nil {
		return err
	}
	var applied int64
	if err = conn.QueryRow(bounded, `SELECT COALESCE(max(version),0) FROM tendo_schema_migrations`).Scan(&applied); err != nil {
		return errors.New("migration metadata unavailable")
	}
	entries, err := migrations.ReadDir(".")
	if err != nil {
		return errors.New("migration files unavailable")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if applied > currentVersion {
		return errors.New("database schema is newer than this application")
	}
	for _, entry := range entries {
		var version int64
		if _, err = fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			return errors.New("invalid migration filename")
		}
		if version <= applied {
			continue
		}
		raw, readErr := migrations.ReadFile(entry.Name())
		if readErr != nil {
			return errors.New("migration file unavailable")
		}
		up, _, ok := strings.Cut(string(raw), "-- +goose Down")
		if !ok {
			return errors.New("invalid migration")
		}
		up = strings.ReplaceAll(up, "-- +goose Up", "")
		tx, beginErr := conn.Begin(bounded)
		if beginErr != nil {
			return errors.New("migration transaction failed")
		}
		if _, beginErr = tx.Exec(bounded, `INSERT INTO tendo_schema_migrations(version,dirty) VALUES($1,true)`, version); beginErr == nil {
			_, beginErr = tx.Exec(bounded, up)
		}
		if beginErr == nil {
			_, beginErr = tx.Exec(bounded, `UPDATE tendo_schema_migrations SET dirty=false WHERE version=$1`, version)
		}
		if beginErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = tx.Rollback(cleanup)
			cancel()
			return fmt.Errorf("migration failed: %w", beginErr)
		}
		if beginErr = tx.Commit(bounded); beginErr != nil {
			return errors.New("migration commit failed")
		}
		applied = version
	}
	return validateMetadata(bounded, conn)
}

func ValidateSchema(ctx context.Context, pool *pgxpool.Pool) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := pool.Acquire(bounded)
	if err != nil {
		return errors.New("schema database unavailable")
	}
	defer conn.Release()
	return validateMetadata(bounded, conn)
}

func ensureMetadata(ctx context.Context, conn migrationConn) error {
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS tendo_schema_migrations(version bigint PRIMARY KEY, dirty boolean NOT NULL)`); err != nil {
		return errors.New("migration metadata failed")
	}
	return nil
}

func validateMetadata(ctx context.Context, conn migrationConn) error {
	var maxVersion int64
	var dirty bool
	if err := conn.QueryRow(ctx, `SELECT COALESCE(max(version),0), COALESCE(bool_or(dirty),false) FROM tendo_schema_migrations`).Scan(&maxVersion, &dirty); err != nil {
		return errors.New("schema metadata invalid")
	}
	if dirty {
		return errors.New("database has a dirty migration")
	}
	if maxVersion > currentVersion {
		return errors.New("database schema is newer than this application")
	}
	if maxVersion != currentVersion {
		return errors.New("database schema version is unsupported")
	}
	return nil
}
