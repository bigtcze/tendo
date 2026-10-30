package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var migrations embed.FS

const currentVersion int64 = 16
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
	if applied > currentVersion {
		return errors.New("database schema is newer than this application")
	}
	if err := applyMigrations(migrations, entries, applied, func(step migrationStep) error {
		return applyMigration(bounded, conn, step)
	}); err != nil {
		return err
	}
	return validateMetadata(bounded, conn)
}

type migrationStep struct {
	version int64
	up      string
}

func applyMigrations(source fs.FS, entries []fs.DirEntry, applied int64, apply func(migrationStep) error) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		version, err := migrationVersion(entry)
		if err != nil {
			return err
		}
		if version <= applied {
			continue
		}
		step, err := migrationForEntry(source, entry, version)
		if err != nil {
			return err
		}
		if err := apply(step); err != nil {
			return err
		}
		applied = step.version
	}
	return nil
}

func migrationVersion(entry fs.DirEntry) (int64, error) {
	var version int64
	if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
		return 0, errors.New("invalid migration filename")
	}
	return version, nil
}

func migrationForEntry(source fs.FS, entry fs.DirEntry, version int64) (migrationStep, error) {
	raw, err := fs.ReadFile(source, entry.Name())
	if err != nil {
		return migrationStep{}, errors.New("migration file unavailable")
	}
	up, _, ok := strings.Cut(string(raw), "-- +goose Down")
	if !ok {
		return migrationStep{}, errors.New("invalid migration")
	}
	return migrationStep{version: version, up: strings.ReplaceAll(up, "-- +goose Up", "")}, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, step migrationStep) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("migration transaction failed")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO tendo_schema_migrations(version,dirty) VALUES($1,true)`, step.version); err == nil {
		_, err = tx.Exec(ctx, step.up)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE tendo_schema_migrations SET dirty=false WHERE version=$1`, step.version)
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = tx.Rollback(cleanup)
		cancel()
		return fmt.Errorf("migration failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("migration commit failed")
	}
	return nil
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
	if err := conn.QueryRow(ctx,
		`SELECT COALESCE(max(version),0), COALESCE(bool_or(dirty),false) FROM tendo_schema_migrations`,
	).Scan(&maxVersion, &dirty); err != nil {
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
	if err := validateItemSearch(ctx, conn); err != nil {
		return err
	}
	return nil
}
