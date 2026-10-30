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
	"github.com/bigtcze/tendo/backend/internal/testpostgres"
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

func waitForSignal[T any](t *testing.T, signal <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func waitForError(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func waitForDatabaseLock(ctx context.Context, observer *pgxpool.Pool, runtimeRole string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var waiting bool
		err := observer.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity a
				WHERE a.usename = $1 AND a.wait_event_type = 'Lock'
			)
		`, runtimeRole).Scan(&waiting)
		if err != nil {
			return err
		}
		if waiting {
			return nil
		}
		select {
		case <-deadline.C:
			return errors.New("timed out waiting for PostgreSQL row-lock wait")
		case <-poll.C:
		}
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
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	releaseLock := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	t.Cleanup(releaseLock)
	decider := func(name string) item.CompletionDecider {
		return func(current item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			if name == "first" {
				close(firstEntered)
				<-releaseFirst
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
	secondResult := make(chan error, 1)
	go func() {
		_, _, err := repo.Complete(ctx, f.household, i.ID, "first", [32]byte{1}, decider("first"))
		firstResult <- err
	}()
	waitForSignal(t, firstEntered, "first completion to enter its decider")

	waitLock := make(chan struct{})
	secondDone := make(chan error, 1)
	secondStarted := make(chan struct{})
	go func() {
		defer close(waitLock)
		secondStarted <- struct{}{}
		secondDone <- waitForDatabaseLock(ctx, app, testpostgres.MustRuntimeRole(t))
	}()
	waitForSignal(t, secondStarted, "lock observer to start")
	go func() {
		_, _, err := repo.Complete(ctx, f.household, i.ID, "second", [32]byte{2}, decider("second"))
		secondResult <- err
	}()
	if err := waitForError(t, secondDone, "PostgreSQL lock observation"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, waitLock, "lock observer completion")
	releaseLock()
	if err := waitForError(t, firstResult, "first completion"); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if err := waitForError(t, secondResult, "second completion"); !errors.Is(err, item.ErrVersionMismatch) {
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
	firstNext, _ := schedule.NewDate(2027, time.March, 1)
	secondNext, _ := schedule.NewDate(2028, time.March, 1)
	policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	initialState := item.StateWaiting
	i := f.create(t, "undo characterize", &anchor, policy)
	state := initialState
	updated, err := repo.Update(ctx, f.household, i.ID, i.Version, item.Change{WorkflowState: &state})
	if err != nil {
		t.Fatal(err)
	}
	i = updated
	complete := func(key string, next schedule.Date, state item.WorkflowState) item.Completion {
		t.Helper()
		c, _, err := repo.Complete(ctx, f.household, i.ID, key, [32]byte{byte(len(key))}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			r := item.Completion{HouseholdID: f.household, ItemID: i.ID, CompletedByUserID: f.actor, CompletedOn: f.today, CycleAttentionOn: cur.AttentionOn, Recurrence: cur.Recurrence, PriorWorkflowState: cur.WorkflowState, NextAttentionOn: &next, ItemVersionBefore: cur.Version, IdempotencyKey: key}
			changed := cur
			changed.AttentionOn = &next
			changed.WorkflowState = item.StateOpen
			changed.Version++
			return item.CompletionPlan{Receipt: r, Item: changed}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := complete("undo-first", firstNext, item.StatePaused)
	middleState := item.StateInProgress
	updated, err = repo.Update(ctx, f.household, i.ID, i.Version+1, item.Change{WorkflowState: &middleState})
	if err != nil {
		t.Fatal(err)
	}
	i = updated
	second := complete("undo-second", secondNext, item.StateInProgress)
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
	_, err = repo.UndoCompletion(ctx, f.household, i.ID, first.ID, undo)
	if !errors.Is(err, item.ErrCompletionNotLatest) {
		t.Fatalf("non-latest undo err=%v", err)
	}
	latest, err := repo.UndoCompletion(ctx, f.household, i.ID, second.ID, undo)
	if err != nil || latest.UndoneAt == nil {
		t.Fatalf("latest undo=%+v err=%v", latest, err)
	}
	current, err := repo.Get(ctx, f.household, i.ID)
	if err != nil || current.AttentionOn == nil || *current.AttentionOn != firstNext || current.WorkflowState != item.StateInProgress || current.LastCompletedOn == nil || *current.LastCompletedOn != f.today {
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
