package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
)

func TestCompletionHistoryKeysetWalkPostgres(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var householdID, subjectID, itemID, userID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('completion history property','UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('completion_history_property') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, userID, householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','history') RETURNING id::text`, householdID).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'history') RETURNING id::text`, householdID, subjectID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO item_completions(id,household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint)
		SELECT ('0198a3f1-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,$1::uuid,$2::uuid,DATE '2020-01-01'+n,$3::uuid,'open',n,'history-'||n::text,decode(repeat('aa',32),'hex') FROM generate_series(1,1007) n`, householdID, itemID, userID); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)
	service := item.NewService(repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, nil, nil, func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) })
	for _, order := range []string{item.CompletionOrderOldest, item.CompletionOrderNewest} {
		t.Run(order, func(t *testing.T) {
			var walked []string
			cursor := ""
			for pageNo := 0; ; pageNo++ {
				if pageNo > 50 {
					t.Fatal("completion cursor did not terminate")
				}
				page, err := service.ListCompletions(ctx, userID, householdID, itemID, 23, cursor, order)
				if err != nil {
					t.Fatal(err)
				}
				for _, completion := range page.Items {
					walked = append(walked, completion.ID)
				}
				if page.NextCursor == nil {
					break
				}
				cursor = *page.NextCursor
			}
			if len(walked) != 1007 {
				t.Fatalf("walk returned %d receipts; want 1007", len(walked))
			}
			seen := make(map[string]bool, len(walked))
			for _, id := range walked {
				if seen[id] {
					t.Fatalf("duplicate receipt %s", id)
				}
				seen[id] = true
			}
			for n := 1; n <= 1007; n++ {
				want := fmt.Sprintf("0198a3f1-0000-7000-8000-%012d", n)
				got := walked[n-1]
				if order == item.CompletionOrderNewest {
					got = walked[len(walked)-n]
				}
				if got != want {
					t.Fatalf("receipt position %d = %s; want %s", n, got, want)
				}
			}
		})
	}
}
