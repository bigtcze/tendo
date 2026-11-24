package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestMigrationTwentySixUpDownAndPrivileges covers migration 026 (ADR 0021):
// the household creation replay table, its key and fingerprint CHECK
// constraints, the actor/key unique constraint, referential behaviour and the
// runtime role privileges.
func TestMigrationTwentySixUpDownAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}

	requireSchemaVersion(t, ctx, admin, 32)
	if !migrationRelationExists(t, ctx, admin, "household_creations") {
		t.Fatal("household_creations missing after migrate to 31")
	}
	for _, check := range []struct {
		privilege string
		want      bool
	}{
		{"SELECT", true},
		{"INSERT", true},
		{"UPDATE", false},
		{"DELETE", false},
		{"TRUNCATE", false},
	} {
		var got bool
		if err := admin.QueryRow(ctx,
			`SELECT has_table_privilege('tendo','household_creations',$1)`, check.privilege).Scan(&got); err != nil || got != check.want {
			t.Errorf("tendo %s household_creations = %v, want %v (err=%v)", check.privilege, got, check.want, err)
		}
	}
	var publicGrants int
	if err := admin.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.role_table_grants WHERE table_name='household_creations' AND grantee='PUBLIC'`).Scan(&publicGrants); err != nil || publicGrants != 0 {
		t.Fatalf("PUBLIC table grants=%d err=%v", publicGrants, err)
	}
	var constraintName string
	if err := admin.QueryRow(ctx, `SELECT conname FROM pg_constraint
		WHERE conname='household_creations_actor_key_unique' AND contype='u'`).Scan(&constraintName); err != nil {
		t.Fatalf("actor/key unique constraint missing: %v", err)
	}

	house := insertMigrationHousehold(t, ctx, admin, "migration 026 house")
	other := insertMigrationHousehold(t, ctx, admin, "migration 026 other")
	actor := insertMigrationAccount(t, ctx, admin, "migration_026_actor")
	fingerprint := make([]byte, 32)
	for i := range fingerprint {
		fingerprint[i] = byte(i)
	}

	// Key and fingerprint CHECK constraints reject malformed rows.
	for name, tc := range map[string]struct {
		key  string
		fp   []byte
		want string
	}{
		"empty key":     {"", fingerprint, "23514"},
		"long key":      {strings.Repeat("a", 129), fingerprint, "23514"},
		"space in key":  {"bad key", fingerprint, "23514"},
		"non-ascii key": {"café", fingerprint, "23514"},
		"short digest":  {"short", fingerprint[:31], "23514"},
	} {
		_, err := admin.Exec(ctx,
			`INSERT INTO household_creations(household_id,actor_user_id,creation_key,request_fingerprint,created_at)
			 VALUES ($1::uuid,$2::uuid,$3,$4,now())`, house, actor, tc.key, tc.fp)
		t.Run(name, func(t *testing.T) { requireSQLState(t, tc.want, err) })
	}
	for name, tc := range map[string]struct {
		query string
		args  []any
	}{
		"unknown actor": {`INSERT INTO household_creations(household_id,actor_user_id,creation_key,request_fingerprint,created_at) VALUES ($1::uuid,gen_random_uuid(),'orphan',$2,now())`, []any{house, fingerprint}},
		"unknown house": {`INSERT INTO household_creations(household_id,actor_user_id,creation_key,request_fingerprint,created_at) VALUES (gen_random_uuid(),$1::uuid,'orphan',$2,now())`, []any{actor, fingerprint}},
	} {
		_, err := admin.Exec(ctx, tc.query, tc.args...)
		t.Run(name, func(t *testing.T) { requireSQLState(t, "23503", err) })
	}

	if _, err := admin.Exec(ctx,
		`INSERT INTO household_creations(household_id,actor_user_id,creation_key,request_fingerprint,created_at)
		 VALUES ($1::uuid,$2::uuid,'valid-key',$3,now())`, house, actor, fingerprint); err != nil {
		t.Fatalf("valid creation row: %v", err)
	}
	// Same actor and key from another household is the race signal.
	_, err := admin.Exec(ctx,
		`INSERT INTO household_creations(household_id,actor_user_id,creation_key,request_fingerprint,created_at)
		 VALUES ($1::uuid,$2::uuid,'valid-key',$3,now())`, other, actor, fingerprint)
	requireSQLState(t, "23505", err)

	// The actor reference is NO ACTION: an account that still owns a creation
	// row cannot be deleted, and the offending constraint is the creation FK.
	_, err = admin.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1::uuid`, actor)
	var actorFK *pgconn.PgError
	if !errors.As(err, &actorFK) || actorFK.Code != "23503" || actorFK.ConstraintName != "household_creations_actor_user_id_fkey" {
		t.Fatalf("delete actor with creation row err=%v want 23503 household_creations_actor_user_id_fkey", err)
	}

	// The row goes with its household; the actor reference never cascades.
	if _, err := admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, other); err != nil {
		t.Fatal(err)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_creations WHERE household_id=$1::uuid`, other); got != 0 {
		t.Fatalf("creation rows for deleted household=%d want 0", got)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_creations WHERE household_id=$1::uuid`, house); got != 1 {
		t.Fatalf("creation rows for surviving household=%d want 1", got)
	}

	// Down removes the table and its ledger row.
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion26SQL); err != nil {
		t.Fatalf("reverse 026: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 25)
	if migrationRelationExists(t, ctx, admin, "household_creations") {
		t.Fatal("household_creations survived Down")
	}
	// Up again over the populated version 25 schema.
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 026: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if !migrationRelationExists(t, ctx, admin, "household_creations") {
		t.Fatal("household_creations missing after re-apply")
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_creations`); got != 0 {
		t.Fatalf("creation rows after lossy Down/Up=%d want 0", got)
	}
}
