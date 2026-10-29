package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

func TestSubjectKeysetWalkPostgres(t *testing.T) {
	appURL, adminURL := urls(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var householdID, userID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('subject keyset property','UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("subject_keyset_%d", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, userID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO subjects(household_id,type,name,archived) SELECT $1::uuid,'person','keyset-'||n::text,n%2=0 FROM generate_series(1,1007) n`, householdID); err != nil {
		t.Fatal(err)
	}
	ids := func(archived bool, filtered bool) []string {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT id::text FROM subjects WHERE household_id=$1::uuid AND archived=$2 ORDER BY id`, householdID, archived)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			result = append(result, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if filtered {
			selected := make([]string, 0, (len(result)+1)/2)
			for i := 0; i < len(result) && len(selected) < 100; i += 3 {
				selected = append(selected, result[i])
			}
			result = selected
		}
		return result
	}
	service := subject.NewService(NewRepository(app), func(context.Context, string, string) error { return nil }, func(context.Context, string, string) error { return nil })
	for _, archived := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			filtered bool
		}{{"partition", false}, {"repeated_ids", true}} {
			t.Run(fmt.Sprintf("archived_%t/%s", archived, tc.name), func(t *testing.T) {
				want := ids(archived, tc.filtered)
				walk := func(limit int) []string {
					var got []string
					q := subject.ListQuery{Archived: archived, Limit: limit}
					if tc.filtered {
						q.IDs = want
					}
					for pages := 0; pages < 200; pages++ {
						p, err := service.List(ctx, userID, householdID, q)
						if err != nil {
							t.Fatal(err)
						}
						for _, row := range p.Items {
							got = append(got, row.ID)
						}
						if p.NextCursor == nil {
							return got
						}
						q.Cursor = *p.NextCursor
					}
					t.Fatal("cursor walk did not terminate")
					return nil
				}
				for _, limit := range []int{subject.MaxLimit, 7} {
					got := walk(limit)
					if strings.Join(got, "\n") != strings.Join(want, "\n") {
						t.Fatalf("limit=%d got=%d rows, want=%d; duplicates/omissions or order mismatch", limit, len(got), len(want))
					}
				}
			})
		}
	}
}
