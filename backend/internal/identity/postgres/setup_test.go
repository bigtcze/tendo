package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	householdapp "github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householdpostgresdb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSetupPostgresAtomicAndSingleWinner(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL setup tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	app, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, admin); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	if err = database.ValidateSchema(ctx, admin); err != nil {
		t.Fatalf("schema validation: %v", err)
	}
	for _, tc := range []struct {
		name  string
		set   string
		reset string
	}{
		{name: "empty metadata", set: `DELETE FROM tendo_schema_migrations`, reset: `INSERT INTO tendo_schema_migrations(version,dirty) VALUES (1,false)`},
		{name: "version zero", set: `UPDATE tendo_schema_migrations SET version=0`, reset: `UPDATE tendo_schema_migrations SET version=1`},
		{name: "dirty", set: `UPDATE tendo_schema_migrations SET dirty=true`, reset: `UPDATE tendo_schema_migrations SET dirty=false`},
		{name: "newer version", set: `UPDATE tendo_schema_migrations SET version=99`, reset: `UPDATE tendo_schema_migrations SET version=1`},
	} {
		t.Run("schema "+tc.name, func(t *testing.T) {
			if _, err := admin.Exec(ctx, tc.set); err != nil {
				t.Fatal(err)
			}
			validationErr := database.ValidateSchema(ctx, admin)
			if validationErr == nil {
				t.Fatal("invalid migration metadata accepted")
			}
			if _, err := admin.Exec(ctx, tc.reset); err != nil {
				t.Fatal(err)
			}
		})
	}
	var canRead bool
	if err = app.QueryRow(ctx, `SELECT setup_required FROM installation_state WHERE singleton=true`).Scan(&canRead); err != nil || !canRead {
		t.Fatalf("runtime metadata read=%v err=%v", canRead, err)
	}
	if err = database.ValidateSchema(ctx, app); err != nil {
		t.Fatalf("runtime schema validation: %v", err)
	}
	if _, err = app.Exec(ctx, `CREATE TABLE forbidden_runtime_table(id integer)`); err == nil {
		t.Fatal("runtime role altered schema")
	}
	for _, statement := range []string{
		`DELETE FROM installation_state`,
		`INSERT INTO installation_state(singleton, setup_required) VALUES (true, true)`,
		`CREATE ROLE tendo_test_forbidden`,
	} {
		if _, err = app.Exec(ctx, statement); err == nil {
			t.Fatalf("runtime role unexpectedly executed %q", statement)
		} else {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("runtime role error for %q = %v, want insufficient privilege 42501", statement, err)
			}
		}
	}
	factory := func(queries *householdpostgresdb.Queries) identity.OwnerHouseholdService {
		return householdapp.NewBootstrapService(householdpostgres.NewBootstrapRepository(queries))
	}
	repo := New(app, factory)
	service := identity.NewSetupService(repo)
	fixtures := func() {
		t.Helper()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset setup fixtures: %v", err)
		}
	}
	defer fixtures()
	fixtures()
	input := identity.SetupInput{Login: "owner_test", Password: "correct horse battery", HouseholdName: " Veselí 家族 ", Timezone: "Europe/Prague"}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- service.CreateOwner(ctx, input) }()
	}
	wins, completed := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if err == identity.ErrComplete {
			completed++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || completed != 1 {
		t.Fatalf("winners=%d completed=%d", wins, completed)
	}
	var hash string
	var name, timezone string
	var count int
	if err = admin.QueryRow(ctx, `SELECT c.password_hash,h.name,h.timezone FROM user_accounts u JOIN local_credentials c ON c.user_id=u.id JOIN household_memberships m ON m.user_id=u.id JOIN households h ON h.id=m.household_id WHERE u.default_household_id=h.id AND m.role='owner'`).Scan(&hash, &name, &timezone); err != nil {
		t.Fatal(err)
	}
	if name != "Veselí 家族" || timezone != "Europe/Prague" {
		t.Fatalf("household name=%q timezone=%q", name, timezone)
	}
	if valid, verifyErr := security.VerifyPassword(hash, input.Password); verifyErr != nil || !valid {
		t.Fatalf("password hash verification valid=%v err=%v", valid, verifyErr)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts u JOIN local_credentials c ON c.user_id=u.id JOIN household_memberships m ON m.user_id=u.id JOIN households h ON h.id=m.household_id`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("setup graph count=%d err=%v", count, err)
	}
	var defaultHousehold string
	if err = admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts`).Scan(&defaultHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO households(name, timezone) VALUES ('Second Home', 'UTC')`); err != nil {
		t.Fatal(err)
	}
	var secondHousehold string
	if err = admin.QueryRow(ctx, `SELECT id::text FROM households WHERE name='Second Home'`).Scan(&secondHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) SELECT id, $1, 'member' FROM user_accounts`, secondHousehold); err != nil {
		t.Fatalf("second membership rejected: %v", err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("membership count=%d err=%v", count, err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO households(name, timezone) VALUES ('Unlinked Home', 'UTC')`); err != nil {
		t.Fatal(err)
	}
	var unlinkedHousehold string
	if err = admin.QueryRow(ctx, `SELECT id::text FROM households WHERE name='Unlinked Home'`).Scan(&unlinkedHousehold); err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$1`, unlinkedHousehold)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("unlinked default error=%v, want foreign key violation 23503", err)
	}
	if err = admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts`).Scan(&secondHousehold); err != nil || secondHousehold != defaultHousehold {
		t.Fatalf("invalid default update changed value=%q err=%v", secondHousehold, err)
	}
	fixtures()
	if _, err = admin.Exec(ctx, `ALTER TABLE households ADD CONSTRAINT force_setup_failure CHECK (name <> 'Trigger Failure')`); err != nil {
		t.Fatal(err)
	}
	failure := identity.SetupInput{Login: "rollback_user", Password: "correct horse battery", HouseholdName: "Trigger Failure", Timezone: "UTC"}
	if err = service.CreateOwner(ctx, failure); err == nil {
		t.Fatal("expected forced persistence failure")
	}
	if _, err = admin.Exec(ctx, `ALTER TABLE households DROP CONSTRAINT force_setup_failure`); err != nil {
		t.Fatal(err)
	}
	var required bool
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts)+(SELECT count(*) FROM households)+(SELECT count(*) FROM local_credentials)+(SELECT count(*) FROM household_memberships)`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT setup_required FROM installation_state`).Scan(&required); err != nil {
		t.Fatal(err)
	}
	if count != 0 || !required {
		t.Fatalf("rollback left rows=%d required=%v", count, required)
	}
	fixtures()
}
