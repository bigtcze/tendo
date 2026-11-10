package database_test

import (
	"context"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests run against the dedicated PostgreSQL test service from
// scripts/setup-integration.sh. Its pre-provisioned NOLOGIN role tendo is the
// only role the bootstrap may create in production, but the harness creates it
// once and forbids CREATEROLE, so the creation branch is exercised by the
// operator smoke suite rather than this process.

func TestBootstrapRoleIsIdempotentAndAppliesGrants(t *testing.T) {
	admin := openPool(t, "TEST_DATABASE_ADMIN_URL")
	ctx := testContext(t)
	runtimeURL := runtimeRoleURL(t, "harness-bootstrap-password")
	before := runtimeRoleState(t, ctx, admin)
	for run := 1; run <= 2; run++ {
		if err := database.BootstrapRole(ctx, admin, runtimeURL); err != nil {
			t.Fatalf("bootstrap run %d: %v", run, err)
		}
		assertRuntimeGrants(t, ctx, admin)
		if after := runtimeRoleState(t, ctx, admin); after != before {
			t.Fatalf("bootstrap run %d altered the existing role: state %q -> %q", run, before, after)
		}
	}
}

func TestBootstrapRoleSerializesConcurrentInstances(t *testing.T) {
	ctx := testContext(t)
	first, second := openPool(t, "TEST_DATABASE_ADMIN_URL"), openPool(t, "TEST_DATABASE_ADMIN_URL")
	runtimeURL := runtimeRoleURL(t, "harness-concurrent-password")
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, pool := range []*pgxpool.Pool{first, second} {
		wg.Add(1)
		go func(index int, pool *pgxpool.Pool) {
			defer wg.Done()
			<-start
			errs[index] = database.BootstrapRole(ctx, pool, runtimeURL)
		}(i, pool)
	}
	close(start)
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent bootstrap %d: %v", index, err)
		}
	}
	assertRuntimeGrants(t, ctx, first)
}

func TestBootstrapRoleRefusesOtherRolesAndMissingPassword(t *testing.T) {
	admin := openPool(t, "TEST_DATABASE_ADMIN_URL")
	ctx := testContext(t)
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{
			name: "runtime role URL",
			url:  os.Getenv("TEST_DATABASE_URL"),
			want: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name: "missing password",
			url:  roleURLWithoutPassword(t),
			want: "DATABASE_URL must include the role tendo password when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name: "invalid URL",
			url:  "not-a-url",
			want: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := database.BootstrapRole(ctx, admin, tc.url)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func openPool(t *testing.T, env string) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv(env)
	if raw == "" {
		// The unit-only gate runs this package without PostgreSQL; the required
		// integration gate (scripts/setup-integration.sh) always supplies both
		// variables, so a partial environment is still a failure.
		if os.Getenv("TEST_DATABASE_URL") == "" && os.Getenv("TEST_DATABASE_ADMIN_URL") == "" {
			t.Skip("real PostgreSQL integration environment is not configured")
		}
		t.Fatalf("%s is required for real PostgreSQL bootstrap and serving role tests", env)
	}
	pool, err := pgxpool.New(testContext(t), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// runtimeRoleURL keeps the test database and credentials but names the fixed
// serving role, so bootstrap sees a valid configuration without connecting.
func runtimeRoleURL(t *testing.T, password string) string {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Fatal("TEST_DATABASE_URL is required for real PostgreSQL role bootstrap tests")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("tendo", password)
	return u.String()
}

func roleURLWithoutPassword(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(runtimeRoleURL(t, "unused"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("tendo")
	return u.String()
}

// runtimeRoleState captures every role attribute plus membership count, so a
// bootstrap run that rewrote attributes or granted a membership is visible.
func runtimeRoleState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `
		SELECT format('%s|%s|%s|%s|%s|%s|%s',
			rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolbypassrls, rolreplication,
			(SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid))
		FROM pg_roles r WHERE rolname = 'tendo'`).Scan(&state); err != nil {
		t.Fatalf("read harness runtime role: %v", err)
	}
	return state
}

func assertRuntimeGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var connect, usage, create, publicCreate bool
	if err := pool.QueryRow(ctx, `
		SELECT has_database_privilege('tendo', current_database(), 'CONNECT'),
		       has_schema_privilege('tendo', 'public', 'USAGE'),
		       has_schema_privilege('tendo', 'public', 'CREATE'),
		       EXISTS (
		           SELECT 1 FROM pg_namespace n,
		               aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) acl
		           WHERE n.nspname = 'public' AND acl.grantee = 0 AND acl.privilege_type = 'CREATE'
		       )`).Scan(&connect, &usage, &create, &publicCreate); err != nil {
		t.Fatalf("read runtime grants: %v", err)
	}
	if !connect || !usage {
		t.Fatalf("runtime role grants missing: connect=%v usage=%v", connect, usage)
	}
	if create || publicCreate {
		t.Fatalf("public schema still grants CREATE: tendo=%v PUBLIC=%v", create, publicCreate)
	}
}
