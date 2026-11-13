package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
)

// TestAccountsDualMemberHouseholdIsolation covers an actor who administers both
// households. Membership in the household named by the path is not enough: the
// account it names must belong to that household, so an H1-only member read
// through H2 is uniformly not found and nothing changes.
func TestAccountsDualMemberHouseholdIsolation(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL account tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households, household_role_changes CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()

	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	service := AccountServiceFor(app, identitypostgres.New(app, nil), identitypostgres.New(app, nil), emptyResetLookup{}, clock)

	newUser := func(login string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newHousehold := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	join := func(userID, householdID, role string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, $3)`, userID, householdID, role); err != nil {
			t.Fatal(err)
		}
	}
	memberships := func(userID string) map[string]string {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT household_id::text, role FROM household_memberships WHERE user_id=$1::uuid`, userID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var house, role string
			if err := rows.Scan(&house, &role); err != nil {
				t.Fatal(err)
			}
			out[house] = role
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	dualAdmin, userOnlyInH1 := newUser("dual_admin"), newUser("dual_h1_only")
	h1, h2 := newHousehold("dual home one"), newHousehold("dual home two")
	join(dualAdmin, h1, household.RoleAdmin)
	join(dualAdmin, h2, household.RoleAdmin)
	join(userOnlyInH1, h1, household.RoleMember)

	// The actor may read the H1 account through H1: the resource exists, so a
	// not-found through H2 can only come from the household scoping.
	own, err := service.GetAccount(ctx, dualAdmin, h1, userOnlyInH1)
	if err != nil || own.UserID != userOnlyInH1 || own.Role != household.RoleMember || own.Version != 1 {
		t.Fatalf("H1 read of the H1-only account=%+v err=%v", own, err)
	}
	before := memberships(userOnlyInH1)

	if _, err := service.GetAccount(ctx, dualAdmin, h2, userOnlyInH1); !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("H2 read of the H1-only account err=%v, want %v", err, household.ErrNotFound)
	}

	// No row moved, was duplicated into H2 or changed role.
	after := memberships(userOnlyInH1)
	if len(after) != len(before) {
		t.Fatalf("memberships changed before=%v after=%v", before, after)
	}
	for house, role := range before {
		if after[house] != role {
			t.Fatalf("membership %s changed %q -> %q", house, role, after[house])
		}
	}
	if _, ok := after[h2]; ok {
		t.Fatalf("H1-only account gained an H2 membership: %v", after)
	}
}
