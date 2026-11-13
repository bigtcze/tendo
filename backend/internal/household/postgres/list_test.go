package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
)

// TestListForMemberAgainstPostgres proves that the caller's own membership is
// the only authorization, that paging is keyset-ordered and lossless across
// more than one page, that a forged cursor never reaches persistence, and that
// an unknown stored role fails closed instead of leaking a partial page.
func TestListForMemberAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL household list tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
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
	join := func(user, hh, role string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, $3)`, user, hh, role); err != nil {
			t.Fatal(err)
		}
	}

	alice, bob := newUser("list_alice"), newUser("list_bob")
	const total = 120
	aliceHomes := make([]string, 0, total)
	for i := 0; i < total; i++ {
		hh := newHousehold(fmt.Sprintf("Alice home %03d", i))
		aliceHomes = append(aliceHomes, hh)
		role := household.RoleMember
		if i == 0 {
			role = household.RoleAdmin
		}
		join(alice, hh, role)
	}
	bobOnly := newHousehold("Bob only home")
	join(bob, bobOnly, household.RoleAdmin)

	service := household.NewService(NewRepository(app))

	var got []string
	cursor := ""
	pages := 0
	for {
		rows, next, err := service.ListForMember(ctx, alice, cursor, 50)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		for _, row := range rows {
			got = append(got, row.ID)
		}
		if next == "" {
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatal("cursor walk did not terminate")
		}
	}
	if pages != 3 || len(got) != total {
		t.Fatalf("pages=%d rows=%d", pages, len(got))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatal("households are not ordered by id")
	}
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("duplicate household %s across pages", id)
		}
		seen[id] = true
	}
	expected := append([]string(nil), aliceHomes...)
	sort.Strings(expected)
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("row %d = %s, want %s", i, got[i], expected[i])
		}
	}

	// Isolation: another user's households never appear, and Bob only sees his.
	bobRows, bobNext, err := service.ListForMember(ctx, bob, "", 100)
	if err != nil || bobNext != "" || len(bobRows) != 1 {
		t.Fatalf("bob rows=%+v next=%q err=%v", bobRows, bobNext, err)
	}
	if bobRows[0].ID != bobOnly || bobRows[0].ViewerRole != household.RoleAdmin {
		t.Fatalf("bob row=%+v", bobRows[0])
	}

	// The hard cap is 100 even when the request asks for more.
	clamped, clampedNext, err := service.ListForMember(ctx, alice, "", 500)
	if err != nil || len(clamped) != 100 || clampedNext == "" {
		t.Fatalf("clamped rows=%d next=%q err=%v", len(clamped), clampedNext, err)
	}

	// A forged cursor is rejected by the cursor decoder before any query.
	if _, _, err := service.ListForMember(ctx, alice, "h1Zm9yZ2Vk", 50); err == nil {
		t.Fatal("forged cursor was accepted")
	} else {
		var validation *household.ValidationError
		if !errors.As(err, &validation) || validation.Field != "cursor" {
			t.Fatalf("forged cursor err=%v", err)
		}
	}

	// A stored role outside the contract fails closed for the whole page.
	if _, err := admin.Exec(ctx, `ALTER TABLE household_memberships DROP CONSTRAINT household_memberships_role_check`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `UPDATE household_memberships SET role='member' WHERE role NOT IN ('admin','member')`)
		_, _ = admin.Exec(c, `ALTER TABLE household_memberships ADD CONSTRAINT household_memberships_role_check CHECK (role IN ('admin','member'))`)
	})
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='owner' WHERE user_id=$1::uuid AND household_id=$2::uuid`, alice, aliceHomes[5]); err != nil {
		t.Fatal(err)
	}
	rows, next, err := service.ListForMember(ctx, alice, "", 50)
	if err == nil || errors.Is(err, household.ErrNotFound) || rows != nil || next != "" {
		t.Fatalf("corrupt role rows=%+v next=%q err=%v, want unavailable", rows, next, err)
	}
}
