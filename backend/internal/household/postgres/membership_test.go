package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMembershipServiceAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("real PostgreSQL URLs required")
	}
	admin, e := pgxpool.New(ctx, adminURL)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	app, e := pgxpool.New(ctx, appURL)
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	if e = database.Migrate(ctx, admin); e != nil {
		t.Fatal(e)
	}
	_, e = admin.Exec(ctx, `TRUNCATE user_accounts,household_memberships,households CASCADE`)
	if e != nil {
		t.Fatal(e)
	}
	var hid, owner string
	e = admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('Member list','UTC') RETURNING id::text`).Scan(&hid)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		var uid string
		e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("member_list_%d", i)).Scan(&uid)
		if e != nil {
			t.Fatal(e)
		}
		role := "member"
		if i == 0 {
			owner = uid
			role = "owner"
		}
		if _, e = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,$3)`, uid, hid, role); e != nil {
			t.Fatal(e)
		}
	}
	svc := household.NewMembershipService(NewMembershipRepository(app))
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 2; page++ {
		rows, next, e := svc.ListMembers(ctx, owner, hid, cursor, 2)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range rows {
			if seen[r.UserID] {
				t.Fatalf("duplicate member %s", r.UserID)
			}
			seen[r.UserID] = true
		}
		cursor = next
	}
	if len(seen) != 4 {
		t.Fatalf("members=%d", len(seen))
	}
	if _, _, e = svc.ListMembers(ctx, owner, hid, "bad", 2); e == nil {
		t.Fatal("malformed cursor accepted")
	}
	var nonmember string
	if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('membership_nonmember') RETURNING id::text`).Scan(&nonmember); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.ListMembers(ctx, nonmember, hid, "", 10); e != household.ErrNotFound {
		t.Fatalf("non-member list=%v", e)
	}
}
