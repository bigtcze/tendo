package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func connect(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestFindForMemberAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL household tests")
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
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()

	var currentUser string
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil || currentUser != "tendo" {
		t.Fatalf("reads must use the restricted runtime role, got %q err=%v", currentUser, err)
	}

	newUser := func(login string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newHousehold := func(name, tz string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, $2) RETURNING id::text`, name, tz).Scan(&id); err != nil {
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

	alice, bob, carol := newUser("alice"), newUser("bob"), newUser("carol")
	homeA, homeB := newHousehold("Alice Home", "Europe/Prague"), newHousehold("Bob 家", "UTC")
	join(alice, homeA, "owner")
	join(bob, homeB, "owner")
	repo := NewRepository(app)

	t.Run("owner reads own household", func(t *testing.T) {
		got, err := repo.FindForMember(ctx, alice, homeA)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != homeA || got.Name != "Alice Home" || got.Timezone != "Europe/Prague" || got.Version != 1 {
			t.Fatalf("got=%+v", got)
		}
		if got.CreatedAt.Location() != time.UTC || time.Since(got.CreatedAt) > time.Minute || got.CreatedAt.IsZero() {
			t.Fatalf("createdAt=%v", got.CreatedAt)
		}
	})

	t.Run("version reflects the stored column", func(t *testing.T) {
		if _, err := admin.Exec(ctx, `UPDATE households SET version=7 WHERE id=$1::uuid`, homeA); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = admin.Exec(ctx, `UPDATE households SET version=1 WHERE id=$1::uuid`, homeA) }()
		got, err := repo.FindForMember(ctx, alice, homeA)
		if err != nil || got.Version != 7 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("cross-household isolation", func(t *testing.T) {
		if got, err := repo.FindForMember(ctx, bob, homeA); !errors.Is(err, household.ErrNotFound) || got != (household.Household{}) {
			t.Fatalf("bob reading Alice's household: got=%+v err=%v", got, err)
		}
		if got, err := repo.FindForMember(ctx, alice, homeB); !errors.Is(err, household.ErrNotFound) || got != (household.Household{}) {
			t.Fatalf("alice reading Bob's household: got=%+v err=%v", got, err)
		}
		if got, err := repo.FindForMember(ctx, bob, homeB); err != nil || got.Name != "Bob 家" {
			t.Fatalf("bob own household: got=%+v err=%v", got, err)
		}
	})

	t.Run("user without any membership", func(t *testing.T) {
		if _, err := repo.FindForMember(ctx, carol, homeA); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("default household requires membership (FK)", func(t *testing.T) {
		// A default_household_id requires a membership via FK, so a user can only
		// default to a household they belong to; membership remains the sole gate.
		if _, err := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, carol, homeA); err == nil {
			t.Fatal("default household without membership accepted")
		}
	})

	t.Run("nonexistent household", func(t *testing.T) {
		if _, err := repo.FindForMember(ctx, alice, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("malformed identifiers are not found", func(t *testing.T) {
		if _, err := repo.FindForMember(ctx, alice, "nope"); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("multi-household member reads both with any role", func(t *testing.T) {
		join(carol, homeA, "member")
		join(carol, homeB, "owner")
		for id, name := range map[string]string{homeA: "Alice Home", homeB: "Bob 家"} {
			got, err := repo.FindForMember(ctx, carol, id)
			if err != nil || got.Name != name || got.ID != id {
				t.Fatalf("household %s: got=%+v err=%v", id, got, err)
			}
		}
	})

	t.Run("persistence failure is not reported as not found", func(t *testing.T) {
		cancelled, stop := context.WithCancel(ctx)
		stop()
		_, err := repo.FindForMember(cancelled, alice, homeA)
		if err == nil || errors.Is(err, household.ErrNotFound) {
			t.Fatalf("err=%v, want infrastructure error", err)
		}
	})
}
