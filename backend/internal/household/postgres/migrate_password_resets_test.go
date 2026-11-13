package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/testpostgres"
)

func TestMigrationTwentyPasswordResetsUpDownAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var tableExists bool
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.password_reset_tokens') IS NOT NULL`).Scan(&tableExists); err != nil || !tableExists {
		t.Fatalf("password_reset_tokens table exists=%v err=%v", tableExists, err)
	}
	for _, check := range []struct {
		privilege, column, table string
		want                     bool
	}{
		{"SELECT", "", "password_reset_tokens", true},
		{"INSERT", "", "password_reset_tokens", true},
		{"UPDATE", "", "password_reset_tokens", false},
		{"DELETE", "", "password_reset_tokens", false},
		{"UPDATE", "used_at", "password_reset_tokens", true},
		{"UPDATE", "revoked_at", "password_reset_tokens", true},
		{"UPDATE", "token_hash", "password_reset_tokens", false},
		{"UPDATE", "", "local_credentials", true},
		{"UPDATE", "credential_version", "local_credentials", true},
	} {
		var got bool
		var err error
		if check.column == "" {
			err = admin.QueryRow(ctx, `SELECT has_table_privilege('tendo',$1,$2)`, check.table, check.privilege).Scan(&got)
		} else {
			err = admin.QueryRow(ctx, `SELECT has_column_privilege('tendo',$1,$2,$3)`, check.table, check.column, check.privilege).Scan(&got)
		}
		if err != nil || got != check.want {
			t.Errorf("%s %s.%s = %v, want %v (err=%v)", check.privilege, check.table, check.column, got, check.want, err)
		}
	}

	var credentialVersionDefault string
	var credentialVersionNullable string
	if err := admin.QueryRow(ctx, `SELECT column_default, is_nullable FROM information_schema.columns WHERE table_name='local_credentials' AND column_name='credential_version'`).Scan(&credentialVersionDefault, &credentialVersionNullable); err != nil || credentialVersionDefault != "1" || credentialVersionNullable != "NO" {
		t.Fatalf("credential_version default=%q nullable=%q err=%v", credentialVersionDefault, credentialVersionNullable, err)
	}

	var householdID, ownerID, targetID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('reset migration','UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_migration_owner') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_migration_target') RETURNING id::text`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,decode(repeat('ab',32),'hex'),now(),now()+interval '1 hour')`, householdID, targetID, ownerID); err != nil {
		t.Fatal(err)
	}

	down, err := database.MigrationDownSQL(20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion21SQL+" ; "+down+"; DELETE FROM tendo_schema_migrations WHERE version=20"); err != nil {
		t.Fatalf("execute migration 020 Down on populated schema: %v", err)
	}
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.password_reset_tokens') IS NULL`).Scan(&tableExists); err != nil || !tableExists {
		t.Fatalf("table removed after Down=%v err=%v", tableExists, err)
	}
	var version int64
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 19 {
		t.Fatalf("ledger after Down=%d err=%v", version, err)
	}
	var credentialVersionColumns int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='local_credentials' AND column_name='credential_version'`).Scan(&credentialVersionColumns); err != nil || credentialVersionColumns != 0 {
		t.Fatalf("credential_version survived Down: %d err=%v", credentialVersionColumns, err)
	}

	if _, err := admin.Exec(ctx, `DELETE FROM tendo_schema_migrations WHERE version=20`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-migrate after Down: %v", err)
	}
	var rows int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("recreated table rows=%d err=%v", rows, err)
	}
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 22 {
		t.Fatalf("ledger after re-migrate=%d err=%v", version, err)
	}
}
