package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRuntimeRolePassword pins the credential contract the bootstrap accepts:
// only the fixed serving role, only with a password, and never by returning
// the password in an error.
func TestRuntimeRolePassword(t *testing.T) {
	for _, tc := range []struct {
		name    string
		url     string
		want    string
		wantErr string
	}{
		{name: "valid", url: "postgres://tendo:secret@db.internal:5432/tendo?sslmode=disable", want: "secret"},
		{name: "escaped password", url: "postgres://tendo:p%40ss%2Fword@db.internal:5432/tendo?sslmode=disable", want: "p@ss/word"},
		{
			name:    "other role",
			url:     "postgres://tendo_r_1:secret@db.internal:5432/tendo?sslmode=disable",
			wantErr: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name:    "missing password",
			url:     "postgres://tendo@db.internal:5432/tendo?sslmode=disable",
			wantErr: "DATABASE_URL must include the role tendo password when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name:    "invalid URL",
			url:     "::not-a-url",
			wantErr: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			password, err := runtimeRolePassword(tc.url)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				if password != "" {
					t.Fatalf("password returned with error: %q", password)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if password != tc.want {
				t.Fatalf("password=%q want %q", password, tc.want)
			}
		})
	}
}

// TestRuntimeRoleCreateStatementQuotesServerSide runs the real statement
// builder against PostgreSQL. The integration harness cannot create roles, so
// this pins the creation SQL itself: it must be accepted by the server (format
// parameters need explicit types) and must quote a hostile password literally.
func TestRuntimeRoleCreateStatementQuotesServerSide(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_ADMIN_URL")
	if raw == "" {
		if os.Getenv("TEST_DATABASE_URL") == "" {
			t.Skip("real PostgreSQL integration environment is not configured")
		}
		t.Fatal("TEST_DATABASE_ADMIN_URL is required for real PostgreSQL bootstrap tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	password := `it's a "pass"; DROP ROLE x; -- \`
	statement, err := runtimeRoleCreateStatement(ctx, pool, password)
	if err != nil {
		t.Fatalf("build create statement: %v", err)
	}
	want := `CREATE ROLE tendo LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD E'it''s a "pass"; DROP ROLE x; -- \\'`
	if statement != want {
		t.Fatalf("create statement:\n got %s\nwant %s", statement, want)
	}
}
