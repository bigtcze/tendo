package postgres

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func columnDefinition(t *testing.T, ctx context.Context, admin *pgxpool.Pool, table, column string) (defaultValue *string, nullable string) {
	t.Helper()
	if err := admin.QueryRow(ctx, `SELECT column_default, is_nullable FROM information_schema.columns WHERE table_name=$1 AND column_name=$2`, table, column).Scan(&defaultValue, &nullable); err != nil {
		t.Fatalf("read column definition %s.%s: %v", table, column, err)
	}
	return defaultValue, nullable
}

// TestMigrationTwentyThreeSessionSignInMethodBackfillsLegacy runs migration 023
// over a version-22 fixture that already holds sessions: every pre-existing row
// becomes 'legacy' (fail closed, it cannot be classified), the backfill default
// is dropped so later inserts must name the kind, and the Down section removes
// the column again.
func TestMigrationTwentyThreeSessionSignInMethodBackfillsLegacy(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion23SQL); err != nil {
		t.Fatalf("rewind to version 22: %v", err)
	}
	var version int64
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 22 {
		t.Fatalf("version after rewind=%d err=%v", version, err)
	}
	var columnAt22 int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='user_sessions' AND column_name='sign_in_method'`).Scan(&columnAt22); err != nil || columnAt22 != 0 {
		t.Fatalf("sign_in_method column at version 22=%d err=%v", columnAt22, err)
	}

	var userID string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('signin_method_migration') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1::uuid,decode(repeat('ab',32),'hex'),now(),now()+interval '1 day')`, userID); err != nil {
		t.Fatalf("version 22 session insert: %v", err)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 23: %v", err)
	}

	var backfilled string
	if err := admin.QueryRow(ctx, `SELECT sign_in_method FROM user_sessions WHERE user_id=$1::uuid`, userID).Scan(&backfilled); err != nil || backfilled != "legacy" {
		t.Fatalf("pre-existing session kind=%q err=%v, want legacy", backfilled, err)
	}
	var sessions int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, userID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions after migration=%d err=%v", sessions, err)
	}

	// The column is required and has no default any more: a forgotten value must
	// be a failed insert, never a silently mislabelled session.
	if defaultValue, nullable := columnDefinition(t, ctx, admin, "user_sessions", "sign_in_method"); defaultValue != nil || nullable != "NO" {
		t.Fatalf("sign_in_method default=%v nullable=%q, want no default and NOT NULL", defaultValue, nullable)
	}
	_, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1::uuid,decode(repeat('cd',32),'hex'),now(),now()+interval '1 day')`, userID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Fatalf("insert without sign_in_method error=%v, want not-null violation 23502", err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES($1::uuid,decode(repeat('ce',32),'hex'),now(),now()+interval '1 day','mfa')`, userID)
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert with an unknown kind error=%v, want check violation 23514", err)
	}
	for i, kind := range []string{"password", "oidc", "legacy"} {
		if _, err = admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES($1::uuid,$2,now(),now()+interval '1 day',$3)`,
			userID, bytes.Repeat([]byte{byte(0x90 + i)}, 32), kind); err != nil {
			t.Fatalf("insert with kind %q: %v", kind, err)
		}
	}

	// The runtime role still writes the table through its table-level grants;
	// the new column was never granted UPDATE.
	for _, check := range []struct {
		privilege, column string
		want              bool
	}{
		{"SELECT", "", true},
		{"INSERT", "", true},
		{"DELETE", "", true},
		{"UPDATE", "", false},
		{"SELECT", "sign_in_method", true},
		{"INSERT", "sign_in_method", true},
		{"UPDATE", "sign_in_method", false},
	} {
		var granted bool
		var checkErr error
		if check.column == "" {
			checkErr = admin.QueryRow(ctx, `SELECT has_table_privilege('tendo','user_sessions',$1)`, check.privilege).Scan(&granted)
		} else {
			checkErr = admin.QueryRow(ctx, `SELECT has_column_privilege('tendo','user_sessions',$1,$2)`, check.column, check.privilege).Scan(&granted)
		}
		if checkErr != nil || granted != check.want {
			t.Errorf("%s user_sessions.%s=%v want %v (err=%v)", check.privilege, check.column, granted, check.want, checkErr)
		}
	}

	// Down drops the recorded kind; Up again re-derives 'legacy' for the same
	// sessions, so a downgrade/upgrade cycle never grants a password-change
	// right to a session that was not classified.
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion23SQL); err != nil {
		t.Fatalf("migration 023 Down: %v", err)
	}
	var columnAfterDown int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='user_sessions' AND column_name='sign_in_method'`).Scan(&columnAfterDown); err != nil || columnAfterDown != 0 {
		t.Fatalf("sign_in_method column after Down=%d err=%v", columnAfterDown, err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-migrate after Down: %v", err)
	}
	var reclassified int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE sign_in_method<>'legacy'`).Scan(&reclassified); err != nil || reclassified != 0 {
		t.Fatalf("sessions reclassified as password/oidc by the round trip=%d err=%v", reclassified, err)
	}
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 24 {
		t.Fatalf("ledger after round trip=%d err=%v", version, err)
	}
}
