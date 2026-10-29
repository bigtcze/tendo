package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMemberKeysetWalkPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("real PostgreSQL URLs required")
	}
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
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var hid, owner string
	prefix := fmt.Sprintf("memberkeyset%d", time.Now().UnixNano())
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, prefix).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, prefix+"_owner").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, owner, hid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(login) SELECT $1||'_'||n FROM generate_series(1,1007)n`, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) SELECT id,$1::uuid,'member' FROM user_accounts WHERE login LIKE $2 AND id<>$3::uuid`, hid, prefix+"\\_%", owner); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.Query(ctx, `SELECT user_id::text FROM household_memberships WHERE household_id=$1::uuid ORDER BY user_id`, hid)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	svc := household.NewMembershipService(NewMembershipRepository(app), identitypostgres.New(app, nil))
	for _, limit := range []int{100, 7} {
		var got []string
		cursor := ""
		for pages := 0; pages < 200; pages++ {
			members, next, err := svc.ListMembers(ctx, owner, hid, cursor, limit)
			if err != nil {
				t.Fatal(err)
			}
			for _, member := range members {
				got = append(got, member.UserID)
			}
			if next == "" {
				break
			}
			cursor = next
			if pages == 199 {
				t.Fatal("member cursor walk did not terminate")
			}
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("limit=%d got=%d members, want=%d", limit, len(got), len(want))
		}
	}
}
