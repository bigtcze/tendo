package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
)

// TestCompletionContractGoldenPostgres drives every V5_RECURRENCE_CONTRACT golden row through the item
// service on real PostgreSQL and asserts the returned receipt, the committed item row, the stored
// receipt row and one ledger entry per Done. The household clock is injected per step.
func TestCompletionContractGoldenPostgres(t *testing.T) {
	ctx := context.Background()
	appURL, adminURL := integrationURLs(t)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newCompletionFixture(t, ctx, admin, app)
	var now time.Time
	houses := func(context.Context, string, string) (string, error) { return "Europe/Prague", nil }
	svc := item.NewService(f.repo, houses, nil, nil, func() time.Time { return now })
	for _, tc := range scheduletest.Golden() {
		t.Run(tc.Name, func(t *testing.T) {
			due := scheduletest.Date(tc.Due)
			created := f.create(t, "contract "+tc.Name, &due, tc.Policy)
			dueText := tc.Due
			for index, step := range tc.Steps {
				completed := scheduletest.Date(step.Completion)
				now = time.Date(completed.Year(), completed.Month(), completed.Day(), 12, 0, 0, 0, time.UTC)
				receipt, replayed, err := svc.Complete(ctx, f.actor, f.household, created.ID, int64(index+1), fmt.Sprintf("contract-%d", index+1), item.CompletionRequest{})
				if err != nil || replayed || receipt.CompletedOn != completed {
					t.Fatalf("step %d: completed=%s replayed=%v err=%v", index+1, receipt.CompletedOn, replayed, err)
				}
				var persisted, stored string
				var done bool
				var version int64
				if err := admin.QueryRow(ctx, `SELECT coalesce(attention_on::text,''), done, version FROM items WHERE id=$1::uuid`, created.ID).Scan(&persisted, &done, &version); err != nil {
					t.Fatal(err)
				}
				if err := admin.QueryRow(ctx, `SELECT coalesce(next_attention_on::text,'') FROM item_completions WHERE id=$1::uuid`, receipt.ID).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				returned := ""
				if receipt.NextAttentionOn != nil {
					returned = receipt.NextAttentionOn.String()
				}
				if version != int64(index+2) || done != (step.Want == "") {
					t.Fatalf("step %d: version=%d done=%v", index+1, version, done)
				}
				if step.Want == "" {
					persisted = ""
				}
				scheduletest.Check(t, tc.Name+" (returned)", index, dueText, step, returned)
				scheduletest.Check(t, tc.Name+" (stored receipt)", index, dueText, step, stored)
				scheduletest.Check(t, tc.Name+" (persisted item)", index, dueText, step, persisted)
				dueText = persisted
			}
			var entries int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, created.ID).Scan(&entries); err != nil || entries != len(tc.Steps) {
				t.Fatalf("ledger entries=%d err=%v, want %d", entries, err, len(tc.Steps))
			}
		})
	}
}
