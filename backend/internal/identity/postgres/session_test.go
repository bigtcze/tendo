package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	householdapp "github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householdpostgresdb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ownerFactory(queries *householdpostgresdb.Queries) identity.OwnerHouseholdService {
	return householdapp.NewBootstrapService(householdpostgres.NewBootstrapRepository(queries))
}

func connect(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testURLs(t *testing.T) (string, string) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL session tests")
	}
	return appURL, adminURL
}

func TestSessionsPostgresLifecycle(t *testing.T) {
	appURL, adminURL := testURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, app); err != nil {
		t.Fatalf("runtime role rejects schema v3: %v", err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()
	repo := New(app, ownerFactory)
	const password = "correct horse battery"
	if err := identity.NewSetupService(repo).CreateOwner(ctx, identity.SetupInput{Login: "owner_sessions", Password: password, HouseholdName: "Home", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	var userID, householdID string
	if err := admin.QueryRow(ctx, `SELECT id::text, default_household_id::text FROM user_accounts WHERE login='owner_sessions'`).Scan(&userID, &householdID); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	service, err := identity.NewSessionService(repo, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}

	t.Run("login stores only the digest", func(t *testing.T) {
		session, err := service.Login(ctx, "owner_sessions", password)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(session.Token))
		var stored []byte
		var createdAt, expiresAt time.Time
		var rowUser string
		if err := admin.QueryRow(ctx, `SELECT token_hash, created_at, expires_at, user_id::text FROM user_sessions WHERE id=$1::uuid`, session.ID).Scan(&stored, &createdAt, &expiresAt, &rowUser); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stored, sum[:]) || rowUser != userID {
			t.Fatalf("stored hash mismatch or wrong user %s", rowUser)
		}
		// The service stores whole seconds so POST and GET report the same expiresAt.
		truncated := clock.Truncate(time.Second)
		if !createdAt.Equal(truncated) || !expiresAt.Equal(truncated.Add(30*24*time.Hour)) || !session.ExpiresAt.Equal(expiresAt) {
			t.Fatalf("created=%v expires=%v", createdAt, expiresAt)
		}
		raw, err := base64.RawURLEncoding.DecodeString(session.Token)
		if err != nil {
			t.Fatal(err)
		}
		var leaks int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions s WHERE s.token_hash=$1 OR s.token_hash=convert_to($2,'UTF8') OR s::text LIKE '%'||$2||'%' OR s::text LIKE '%'||encode($1,'base64')||'%'`, raw, session.Token).Scan(&leaks); err != nil || leaks != 0 {
			t.Fatalf("raw token found in user_sessions: %d err=%v", leaks, err)
		}
		principal, err := service.Authenticate(ctx, session.Token)
		if err != nil || principal.UserID != userID || principal.Login != "owner_sessions" || principal.DefaultHouseholdID != householdID {
			t.Fatalf("principal=%+v err=%v", principal, err)
		}
		if _, err := service.Login(ctx, "owner_sessions", "wrong password value"); err != identity.ErrInvalidCredentials {
			t.Fatalf("wrong password err=%v", err)
		}
		if _, err := service.Login(ctx, "unknown_user", password); err != identity.ErrInvalidCredentials {
			t.Fatalf("unknown login err=%v", err)
		}
	})

	t.Run("expiry, logout, and pruning", func(t *testing.T) {
		reset := clock
		defer func() { clock = reset }()
		old, err := service.Login(ctx, "owner_sessions", password)
		if err != nil {
			t.Fatal(err)
		}
		clock = reset.Add(30*24*time.Hour - time.Second)
		if _, err := service.Authenticate(ctx, old.Token); err != nil {
			t.Fatalf("session before expiry rejected: %v", err)
		}
		clock = reset.Add(30 * 24 * time.Hour)
		if _, err := service.Authenticate(ctx, old.Token); err != identity.ErrUnauthenticated {
			t.Fatalf("expired session err=%v", err)
		}
		var before int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid AND expires_at <= $2`, userID, clock).Scan(&before); err != nil || before == 0 {
			t.Fatalf("expected expired rows before login, got %d err=%v", before, err)
		}
		fresh, err := service.Login(ctx, "owner_sessions", password)
		if err != nil {
			t.Fatal(err)
		}
		var after int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid AND expires_at <= $2`, userID, clock).Scan(&after); err != nil || after != 0 {
			t.Fatalf("expired rows after login = %d err=%v", after, err)
		}
		var total int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&total); err != nil || total != 1 {
			t.Fatalf("remaining rows=%d err=%v", total, err)
		}
		if _, err := service.Authenticate(ctx, fresh.Token); err != nil {
			t.Fatal(err)
		}
		if err := service.Logout(ctx, fresh.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, fresh.Token); err != identity.ErrUnauthenticated {
			t.Fatalf("revoked session err=%v", err)
		}
		if err := service.Logout(ctx, fresh.Token); err != nil {
			t.Fatalf("idempotent logout: %v", err)
		}
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&total); err != nil || total != 0 {
			t.Fatalf("rows after logout=%d err=%v", total, err)
		}
	})

	t.Run("runtime role privileges and cascade", func(t *testing.T) {
		session, err := service.Login(ctx, "owner_sessions", password)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`UPDATE user_sessions SET expires_at = expires_at + interval '1 day'`,
			`UPDATE user_sessions SET token_hash = token_hash`,
			`TRUNCATE user_sessions`,
		} {
			_, err := app.Exec(ctx, statement)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("%q error=%v, want 42501", statement, err)
			}
		}
		if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at) VALUES ($1::uuid, $2, now(), now() - interval '1 hour')`, userID, bytes.Repeat([]byte{1}, 32)); err == nil {
			t.Fatal("expires_at <= created_at accepted")
		}
		if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at) VALUES ($1::uuid, $2, now(), now() + interval '1 hour')`, userID, []byte{1, 2, 3}); err == nil {
			t.Fatal("short token_hash accepted")
		}
		var publicPrivileges int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.role_table_grants WHERE table_name='user_sessions' AND grantee='PUBLIC'`).Scan(&publicPrivileges); err != nil || publicPrivileges != 0 {
			t.Fatalf("PUBLIC privileges=%d err=%v", publicPrivileges, err)
		}
		if _, err := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=NULL WHERE id=$1::uuid`, userID); err == nil {
			// default membership FK permits NULL; this keeps the principal valid without a household.
			p, authErr := service.Authenticate(ctx, session.Token)
			if authErr != nil || p.DefaultHouseholdID != "" {
				t.Fatalf("principal without default household: %+v err=%v", p, authErr)
			}
		} else {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1::uuid`, userID); err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatalf("sessions after user delete=%d err=%v", remaining, err)
		}
		if _, err := service.Authenticate(ctx, session.Token); err != identity.ErrUnauthenticated {
			t.Fatalf("deleted user's session err=%v", err)
		}
	})
}

func TestMigrationUpgradeFromVersionOnePreservesData(t *testing.T) {
	appURL, adminURL := testURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	const dbName = "tendo_upgrade_test"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 20*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	}()
	adminCfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.ConnConfig.Database = dbName
	upgradeAdmin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upgradeAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upgradeApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upgradeApp.Close()
	if err := database.Migrate(ctx, upgradeAdmin); err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSetupService(New(upgradeApp, ownerFactory)).CreateOwner(ctx, identity.SetupInput{Login: "upgrade_owner", Password: "correct horse battery", HouseholdName: "Upgrade Home", Timezone: "Europe/Prague"}); err != nil {
		t.Fatal(err)
	}
	// Rewind to the exact version 1 schema: no sessions table, no households.version column, no subjects table, and no version 2/3/4 metadata rows.
	if _, err := upgradeAdmin.Exec(ctx, `DROP TABLE items; DROP TABLE subjects; DROP TABLE user_sessions; ALTER TABLE households DROP COLUMN version; DELETE FROM tendo_schema_migrations WHERE version IN (2,3,4,5)`); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upgradeAdmin); err == nil {
		t.Fatal("version 1 database accepted by version 5 application")
	}
	if err := database.Migrate(ctx, upgradeAdmin); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := database.ValidateSchema(ctx, upgradeApp); err != nil {
		t.Fatalf("validate after upgrade: %v", err)
	}
	var version int64
	var dirty bool
	if err := upgradeAdmin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 5 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	var login, name, tz, role string
	if err := upgradeAdmin.QueryRow(ctx, `SELECT u.login, h.name, h.timezone, m.role FROM user_accounts u JOIN household_memberships m ON m.user_id=u.id JOIN households h ON h.id=m.household_id WHERE u.default_household_id=h.id`).Scan(&login, &name, &tz, &role); err != nil {
		t.Fatal(err)
	}
	if login != "upgrade_owner" || name != "Upgrade Home" || tz != "Europe/Prague" || role != "owner" {
		t.Fatalf("data changed: %s %s %s %s", login, name, tz, role)
	}
	service, err := identity.NewSessionService(New(upgradeApp, ownerFactory), nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Login(ctx, "upgrade_owner", "correct horse battery")
	if err != nil {
		t.Fatalf("login after upgrade: %v", err)
	}
	if _, err := service.Authenticate(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := upgradeApp.Exec(ctx, `UPDATE user_sessions SET expires_at = expires_at`); err == nil {
		t.Fatal("runtime role can update sessions after upgrade")
	}
}

func TestMigrationUpgradeFromVersionTwoAddsHouseholdVersion(t *testing.T) {
	appURL, adminURL := testURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v2_test"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 20*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	}()
	adminCfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.ConnConfig.Database = dbName
	upgradeAdmin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upgradeAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upgradeApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upgradeApp.Close()
	if err := database.Migrate(ctx, upgradeAdmin); err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSetupService(New(upgradeApp, ownerFactory)).CreateOwner(ctx, identity.SetupInput{Login: "v2_owner", Password: "correct horse battery", HouseholdName: "V2 Home", Timezone: "Europe/Prague"}); err != nil {
		t.Fatal(err)
	}
	// Rewind to the exact version 2 schema.
	if _, err := upgradeAdmin.Exec(ctx, `DROP TABLE items; DROP TABLE subjects; ALTER TABLE households DROP COLUMN version; DELETE FROM tendo_schema_migrations WHERE version IN (3,4,5)`); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upgradeAdmin); err == nil {
		t.Fatal("version 2 database accepted by version 5 application")
	}
	if err := database.Migrate(ctx, upgradeAdmin); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := database.ValidateSchema(ctx, upgradeApp); err != nil {
		t.Fatalf("validate after upgrade: %v", err)
	}
	var name, tz string
	var version int64
	if err := upgradeApp.QueryRow(ctx, `SELECT name, timezone, version FROM households`).Scan(&name, &tz, &version); err != nil {
		t.Fatalf("runtime role cannot read upgraded households: %v", err)
	}
	if name != "V2 Home" || tz != "Europe/Prague" || version != 1 {
		t.Fatalf("data after upgrade: %q %q version=%d", name, tz, version)
	}
	var login string
	if err := upgradeAdmin.QueryRow(ctx, `SELECT u.login FROM user_accounts u JOIN household_memberships m ON m.user_id=u.id WHERE u.default_household_id=m.household_id`).Scan(&login); err != nil || login != "v2_owner" {
		t.Fatalf("membership lost: %q err=%v", login, err)
	}
	if _, err := upgradeAdmin.Exec(ctx, `UPDATE households SET version=0`); err == nil {
		t.Fatal("version below 1 accepted")
	}
}
