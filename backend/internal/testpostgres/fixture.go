package testpostgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateDatabase creates a uniquely named upgrade-fixture database owned by the
// supplied migrator connection and registers ownership-checked cleanup.
func CreateDatabase(t *testing.T, ctx context.Context, admin *pgxpool.Pool) string {
	t.Helper()
	var role string
	if err := admin.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if configured := os.Getenv("TEST_DATABASE_MIGRATOR_ROLE"); configured == "" || configured != role {
		t.Fatalf("upgrade fixtures require TEST_DATABASE_MIGRATOR_ROLE to match current_user; current_user=%q configured=%q", role, configured)
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	var migratorOID uint32
	if err := admin.QueryRow(ctx, `SELECT oid FROM pg_roles WHERE rolname=$1`, role).Scan(&migratorOID); err != nil {
		t.Fatalf("capture upgrade fixture migrator OID: %v", err)
	}
	dbName := "tendo_upgrade_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()+` TEMPLATE template0`); err != nil {
		t.Fatalf("create upgrade fixture database: %v", err)
	}
	var oid uint32
	if err := admin.QueryRow(ctx, `SELECT oid FROM pg_database WHERE datname=$1 AND datdba=(SELECT oid FROM pg_roles WHERE rolname=current_user)`, dbName).Scan(&oid); err != nil {
		t.Fatalf("verify upgrade fixture ownership: %v", err)
	}
	adminConfig := admin.Config().Copy()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cleanupAdmin, err := pgxpool.NewWithConfig(cleanupCtx, adminConfig)
		if err != nil {
			t.Errorf("connect for cleanup of owned fixture %s: %v", dbName, err)
			return
		}
		defer cleanupAdmin.Close()
		var currentRoleOID uint32
		if err := cleanupAdmin.QueryRow(cleanupCtx, `SELECT oid FROM pg_roles WHERE rolname=current_user`).Scan(&currentRoleOID); err != nil {
			t.Errorf("verify fixture cleanup migrator: %v", err)
			return
		}
		if currentRoleOID != migratorOID {
			t.Errorf("refusing cleanup for %s: migrator OID changed from %d to %d", dbName, migratorOID, currentRoleOID)
			return
		}
		var currentOID uint32
		if err := cleanupAdmin.QueryRow(cleanupCtx, `SELECT oid FROM pg_database WHERE datname=$1`, dbName).Scan(&currentOID); err != nil {
			t.Errorf("lookup owned fixture %s during cleanup: %v", dbName, err)
			return
		}
		if currentOID != oid {
			t.Errorf("refusing to drop fixture %s: database OID changed from %d to %d", dbName, oid, currentOID)
			return
		}
		var ownerOID uint32
		if err := cleanupAdmin.QueryRow(cleanupCtx, `SELECT datdba FROM pg_database WHERE oid=$1`, oid).Scan(&ownerOID); err != nil || ownerOID != migratorOID {
			t.Errorf("refusing to drop fixture %s: database ownership changed (owner oid %d, expected %d, lookup error %v)", dbName, ownerOID, migratorOID, err)
			return
		}
		if _, err := cleanupAdmin.Exec(cleanupCtx, `DROP DATABASE `+pgx.Identifier{dbName}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Errorf("drop owned fixture %s failed: %v", dbName, err)
		}
	})
	fixtureConfig := admin.Config().Copy()
	fixtureConfig.ConnConfig.Database = dbName
	fixture, err := pgxpool.NewWithConfig(ctx, fixtureConfig)
	if err != nil {
		t.Fatalf("connect upgrade fixture database: %v", err)
	}
	if _, err := fixture.Exec(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO tendo`); err != nil {
		fixture.Close()
		t.Fatalf("restrict upgrade fixture public schema: %v", err)
	}
	fixture.Close()
	return dbName
}

func PoolConfigForDatabase(t *testing.T, url, database string) *pgxpool.Config {
	t.Helper()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	return config
}

func MustRuntimeRole(t *testing.T) string {
	t.Helper()
	role := os.Getenv("TEST_DATABASE_RUNTIME_ROLE")
	if role == "" {
		t.Fatal("TEST_DATABASE_RUNTIME_ROLE is required")
	}
	return role
}
