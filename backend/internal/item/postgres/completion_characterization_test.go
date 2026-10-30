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
	repo := f.repo
	i := f.create(t, "completion characterize replay", nil, nil)
	key, fingerprint := "completion-characterize", [32]byte{7}
	decide := func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
		if existing != nil {
			if existing.Fingerprint != fingerprint {
				return item.CompletionPlan{}, item.ErrIdempotencyKeyReused
			}
			return item.CompletionPlan{Receipt: *existing, Item: current}, nil
		}
		receipt := item.Completion{HouseholdID: f.household, ItemID: i.ID, CompletedByUserID: f.actor, CompletedOn: f.today, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: key}
		changed := current
		changed.Done = true
		changed.Version++
		return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
	}
	first, replayed, err := repo.Complete(ctx, f.household, i.ID, key, fingerprint, decide)
	if err != nil || replayed {
		t.Fatalf("first=%+v replayed=%v err=%v", first, replayed, err)
	}
	replay, replayed, err := repo.Complete(ctx, f.household, i.ID, key, fingerprint, decide)
	if err != nil || !replayed || replay.ID != first.ID {
		t.Fatalf("replay=%+v replayed=%v err=%v", replay, replayed, err)
	}
	fingerprint = [32]byte{8}
	_, replayed, err = repo.Complete(ctx, f.household, i.ID, key, fingerprint, decide)
	if !errors.Is(err, item.ErrIdempotencyKeyReused) || replayed {
		t.Fatalf("different-body conflict replayed=%v err=%v", replayed, err)
	}

	concurrent := f.create(t, "completion characterize version race", nil, nil)
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

func TestCompletionItemLockSerializesVersionDecisionsPostgres(t *testing.T) {
	ctx := context.Background()
	appURL, adminURL := integrationURLs(t)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newCompletionFixture(t, ctx, admin, app)
	i := f.create(t, "completion lock serialization", nil, nil)
	repo := f.repo
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{}, 1)
	secondProceed := make(chan struct{})
	releaseFirst := make(chan struct{})
	decider := func(name string) item.CompletionDecider {
		return func(current item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			if name == "first" {
				close(firstEntered)
				<-releaseFirst
			} else {
				secondEntered <- struct{}{}
				<-secondProceed
			}
			if current.Version != 1 {
				return item.CompletionPlan{}, item.ErrVersionMismatch
			}
			receipt := item.Completion{HouseholdID: f.household, ItemID: i.ID, CompletedByUserID: f.actor, CompletedOn: f.today, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: name}
			updated := current
			updated.Done = true
			updated.Version++
			return item.CompletionPlan{Receipt: receipt, Item: updated}, nil
		}
	}
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := repo.Complete(ctx, f.household, i.ID, "first", [32]byte{1}, decider("first"))
		firstResult <- err
	}()
	<-firstEntered
	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		_, _, err := repo.Complete(ctx, f.household, i.ID, "second", [32]byte{2}, decider("second"))
		secondResult <- err
	}()
	<-secondStarted
	select {
	case <-secondEntered:
		close(secondProceed)
		close(releaseFirst)
		<-firstResult
		<-secondResult
		t.Fatal("second version decision entered before the first transaction released the item lock")
	case <-time.After(time.Second):
	}
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first completion: %v", err)
	}
	close(secondProceed)
	if err := <-secondResult; !errors.Is(err, item.ErrVersionMismatch) {
		t.Fatalf("second completion=%v, want version conflict", err)
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
	repo := f.repo
	anchor, _ := schedule.NewDate(2026, time.March, 1)
	next, _ := schedule.NewDate(2027, time.March, 1)
	policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	i := f.create(t, "undo characterize", &anchor, policy)
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
	first, second := complete("undo-first"), complete("undo-second")
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
	current, err := repo.Get(ctx, f.household, i.ID)
	if err != nil || current.AttentionOn == nil || current.AttentionOn.String() != "2027-03-01" || current.LastCompletedOn == nil || current.LastCompletedOn.String() != f.today.String() {
		t.Fatalf("latest undo state=%+v err=%v", current, err)
	}
}

type completionFixture struct {
	household, actor, subject string
	today                     schedule.Date
	repo                      *Repository
	ctx                       context.Context
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
	return &completionFixture{household: household, actor: actor, subject: subjectID, today: today, repo: NewRepository(app), ctx: ctx}
}

func (f *completionFixture) create(t *testing.T, title string, attention *schedule.Date, recurrence *schedule.Policy) item.Item {
	t.Helper()
	got, err := createForTest(f.ctx, f.repo, f.household, f.actor, item.Draft{SubjectID: f.subject, Title: title, AttentionOn: attention, Recurrence: recurrence})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
