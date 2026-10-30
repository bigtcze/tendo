package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompletionIdempotencyReplayConflictAndConcurrentVersions(t *testing.T) {
	ctx := context.Background()
	appURL, adminURL := integrationURLs(t)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newCompletionFixture(t, ctx, admin, app)
	repo := NewRepository(app)
	itemRow := f.create("completion characterize replay", nil, nil)
	key, fingerprint := "completion-characterize", [32]byte{7}
	decide := func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
		if existing != nil {
			return item.CompletionPlan{Receipt: *existing, Item: current}, nil
		}
		receipt := item.Completion{HouseholdID: f.household, ItemID: itemRow.ID, CompletedByUserID: f.actor, CompletedOn: f.today, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: key}
		changed := current
		changed.Done = true
		changed.Version++
		return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
	}
	first, replayed, err := repo.Complete(ctx, f.household, itemRow.ID, key, fingerprint, decide)
	if err != nil || replayed {
		t.Fatalf("first=%+v replayed=%v err=%v", first, replayed, err)
	}
	replay, replayed, err := repo.Complete(ctx, f.household, itemRow.ID, key, fingerprint, decide)
	if err != nil || !replayed || replay.ID != first.ID {
		t.Fatalf("replay=%+v replayed=%v err=%v", replay, replayed, err)
	}
	conflictFingerprint := [32]byte{8}
	_, replayed, err = repo.Complete(ctx, f.household, itemRow.ID, key, conflictFingerprint, func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
		if existing == nil {
			return item.CompletionPlan{}, errors.New("expected existing receipt")
		}
		return item.CompletionPlan{Receipt: *existing, Item: current}, nil
	})
	if err != nil || !replayed {
		t.Fatalf("repository defers fingerprint conflict to service decider: replayed=%v err=%v", replayed, err)
	}

	concurrent := f.create("completion characterize version race", nil, nil)
	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("completion-race-%d", n)
			_, _, e := repo.Complete(ctx, f.household, concurrent.ID, key, [32]byte{byte(n + 1)}, func(current item.Item, _ *item.Completion) (item.CompletionPlan, error) {
				if current.Version != 1 {
					return item.CompletionPlan{}, item.ErrVersionMismatch
				}
				receipt := item.Completion{HouseholdID: f.household, ItemID: concurrent.ID, CompletedByUserID: f.actor, CompletedOn: f.today, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: key}
				changed := current
				changed.Done = true
				changed.Version++
				return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
			})
			results <- e
		}(n)
	}
	close(start)
	wg.Wait()
	wins, conflicts := 0, 0
	for range workers {
		err := <-results
		switch err {
		case nil:
			wins++
		case item.ErrVersionMismatch:
			conflicts++
		default:
			t.Fatalf("concurrent error: %v", err)
		}
	}
	if wins != 1 || conflicts != workers-1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestUndoLatestVersusNonLatestCompletionPostgres(t *testing.T) {
	ctx := context.Background()
	appURL, adminURL := integrationURLs(t)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newCompletionFixture(t, ctx, admin, app)
	repo := NewRepository(app)
	anchor, _ := schedule.NewDate(2026, time.March, 1)
	next, _ := schedule.NewDate(2027, time.March, 1)
	i := f.create("undo characterize", &anchor, &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed})
	complete := func(key string) item.Completion {
		t.Helper()
		c, _, err := repo.Complete(ctx, f.household, i.ID, key, [32]byte{byte(len(key))}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			r := item.Completion{HouseholdID: f.household, ItemID: i.ID, CompletedByUserID: f.actor, CompletedOn: f.today, CycleAttentionOn: cur.AttentionOn, Recurrence: cur.Recurrence, PriorWorkflowState: cur.WorkflowState, NextAttentionOn: &next, ItemVersionBefore: cur.Version, IdempotencyKey: key}
			changed := cur
			changed.AttentionOn = &next
			changed.Version++
			return item.CompletionPlan{Receipt: r, Item: changed}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := complete("undo-first")
	second := complete("undo-second")
	undo := func(current item.Item, receipt item.Completion, version *int64) (item.CompletionUndoPlan, error) {
		if version == nil || *version != receipt.ItemVersionBefore {
			return item.CompletionUndoPlan{}, item.ErrCompletionNotLatest
		}
		changed := current
		changed.AttentionOn = receipt.CycleAttentionOn
		changed.WorkflowState = receipt.PriorWorkflowState
		changed.Version++
		return item.CompletionUndoPlan{Receipt: receipt, Item: changed, Apply: true, UndoneAt: time.Now().UTC(), UndoneByUserID: f.actor}, nil
	}
	_, err := repo.UndoCompletion(ctx, f.household, i.ID, first.ID, undo)
	if !errors.Is(err, item.ErrCompletionNotLatest) {
		t.Fatalf("non-latest undo err=%v", err)
	}
	latest, err := repo.UndoCompletion(ctx, f.household, i.ID, second.ID, undo)
	if err != nil || latest.UndoneAt == nil {
		t.Fatalf("latest undo=%+v err=%v", latest, err)
	}
}

type completionFixture struct {
	household, actor, subject string
	today                     schedule.Date
	repo                      *Repository
	ctx                       context.Context
	admin                     *pgxpool.Pool
}

func newCompletionFixture(t *testing.T, ctx context.Context, admin, app *pgxpool.Pool) *completionFixture {
	t.Helper()
	var household, actor string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('completion characterization','UTC') RETURNING id::text`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("completion-characterization-%d", time.Now().UnixNano())).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, actor, household); err != nil {
		t.Fatal(err)
	}
	var subjectID string
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','completion characterization') RETURNING id::text`, household).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	today, _ := schedule.NewDate(2026, time.March, 1)
	return &completionFixture{household: household, actor: actor, subject: subjectID, today: today, repo: NewRepository(app), ctx: ctx, admin: admin}
}
func (f *completionFixture) create(title string, attention *schedule.Date, recurrence *schedule.Policy) item.Item {
	got, err := createForTest(f.ctx, f.repo, f.household, f.actor, item.Draft{SubjectID: f.subject, Title: title, AttentionOn: attention, Recurrence: recurrence})
	if err != nil {
		panic(err)
	}
	return got
}
