package item_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/itemtest"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func runCompletion(t *testing.T, e *env, iid, key string, expected int64, on *string) (item.Completion, bool, error) {
	t.Helper()
	return e.svc.Complete(context.Background(), userID, householdID, iid, expected, key, item.CompletionRequest{CompletedOn: on})
}
func d(s string) *schedule.Date {
	v, err := item.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return &v
}
func completionEnv(zone, today string, seed item.Item) *env {
	e := newEnv(zone)
	date, _ := time.Parse("2006-01-02", today)
	e.now = time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, time.UTC)
	if seed.ID == "" {
		seed.ID = missingID
	}
	seed.HouseholdID = householdID
	seed.SubjectID = subjectID
	seed.CreatedAt = e.now
	seed.UpdatedAt = e.now
	seed.Version = 1
	if seed.WorkflowState == "" {
		seed.WorkflowState = item.StateOpen
	}
	e.repo.Seed(seed)
	return e
}
func TestCompletionPlansAndState(t *testing.T) {
	tests := []struct {
		name, today, completed string
		recurrence             *schedule.Policy
		anchor                 *schedule.Date
		state                  item.WorkflowState
		archived, done         bool
		wantNext               string
		wantState              item.WorkflowState
		wantDone               bool
		wantErr                string
	}{
		{name: "one-off", today: "2026-10-20", completed: "2026-10-20", wantDone: true, wantState: item.StateOpen},
		{name: "fixed late", today: "2026-10-20", completed: "2026-10-20", anchor: d("2026-09-01"), recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, wantNext: "2027-09-01", wantState: item.StateOpen},
		{name: "fixed early", today: "2026-08-20", completed: "2026-08-20", anchor: d("2026-09-01"), recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, wantNext: "2027-09-01", wantState: item.StateOpen},
		{name: "fixed missed cycles", today: "2026-03-15", completed: "2026-03-15", anchor: d("2026-01-01"), recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}, wantNext: "2026-04-01", wantState: item.StateOpen},
		{name: "fluid late", today: "2026-10-20", completed: "2026-10-20", anchor: d("2026-09-01"), recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 12, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}, wantNext: "2027-10-20", wantState: item.StateOpen},
		{name: "fluid early", today: "2026-08-20", completed: "2026-08-20", anchor: d("2026-09-01"), recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 12, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}, wantNext: "2027-08-20", wantState: item.StateOpen},
		{name: "no anchor", today: "2026-10-20", completed: "2026-10-20", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, wantNext: "2027-10-20", wantState: item.StateOpen},
		{name: "paused resets", today: "2026-10-20", completed: "2026-10-20", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, state: item.StatePaused, wantNext: "2027-10-20", wantState: item.StateOpen},
		{name: "waiting resets", today: "2026-10-20", completed: "2026-10-20", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, state: item.StateWaiting, wantNext: "2027-10-20", wantState: item.StateOpen},
		{name: "progress resets", today: "2026-10-20", completed: "2026-10-20", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, state: item.StateInProgress, wantNext: "2027-10-20", wantState: item.StateOpen},
		{name: "archived", today: "2026-10-20", completed: "2026-10-20", archived: true, wantErr: "archived"},
		{name: "already done", today: "2026-10-20", completed: "2026-10-20", done: true, wantErr: "done"},
		{name: "future", today: "2026-10-20", completed: "2026-10-21", wantErr: "future_date"},
		{name: "invalid date", today: "2026-10-20", completed: "2026-02-30", wantErr: "invalid_date"},
		{name: "stale before archived", today: "2026-10-20", completed: "2026-10-20", archived: true, wantErr: "stale"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iid := missingID
			seed := item.Item{ID: iid, AttentionOn: tc.anchor, Recurrence: tc.recurrence, WorkflowState: tc.state, Archived: tc.archived, Done: tc.done}
			e := completionEnv("UTC", tc.today, seed)
			on := tc.completed
			expected := int64(1)
			if tc.wantErr == "stale" {
				expected = 2
			}
			c, _, err := runCompletion(t, e, iid, "key", expected, &on)
			if tc.wantErr != "" {
				before, _ := e.repo.Row(iid)
				beforePage, _ := e.svc.ListCompletions(context.Background(), userID, householdID, iid, 50, "")
				if tc.wantErr == "archived" && !errors.Is(err, item.ErrArchived) {
					t.Fatalf("err=%v", err)
				}
				if tc.wantErr == "done" && !errors.Is(err, item.ErrDone) {
					t.Fatalf("err=%v", err)
				}
				if tc.wantErr == "future_date" {
					validation(t, err, "completedOn", "future_date")
				}
				if tc.wantErr == "invalid_date" {
					validation(t, err, "completedOn", "invalid_date")
				}
				if tc.wantErr == "stale" && !errors.Is(err, item.ErrVersionMismatch) {
					t.Fatalf("err=%v", err)
				}
				if err == nil {
					t.Fatalf("rejected case %s unexpectedly succeeded", tc.wantErr)
				}
				after, _ := e.repo.Row(iid)
				afterPage, _ := e.svc.ListCompletions(context.Background(), userID, householdID, iid, 50, "")
				if before.Version != after.Version || before.Done != after.Done || len(beforePage.Items) != len(afterPage.Items) {
					t.Fatalf("rejection mutated item/history before=%+v after=%+v", before, after)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, _ := e.repo.Row(iid)
			if stored.Done != tc.wantDone || stored.WorkflowState != tc.wantState || stored.Version != 2 {
				t.Fatalf("state=%+v", stored)
			}
			got := ""
			if stored.AttentionOn != nil {
				got = stored.AttentionOn.String()
			}
			if got != tc.wantNext {
				t.Fatalf("next=%s want=%s", got, tc.wantNext)
			}
			if c.CompletedOn.String() != tc.completed {
				t.Fatalf("receipt date=%s", c.CompletedOn)
			}
			if stored.LastCompletedOn == nil || stored.LastCompletedOn.String() != tc.completed {
				t.Fatalf("last completed=%v", stored.LastCompletedOn)
			}
			if tc.wantDone {
				page, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{})
				if err != nil || len(page.Items) != 0 {
					t.Fatalf("default list=%+v err=%v", page, err)
				}
			}
		})
	}
}

func TestCompletionIdempotencyAndDatePresence(t *testing.T) {
	iid := missingID
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: iid})
	var omitted *string
	first, replayed, err := runCompletion(t, e, iid, "retry", 1, omitted)
	if err != nil || replayed {
		t.Fatalf("first=%+v replay=%v err=%v", first, replayed, err)
	}
	row, _ := e.repo.Row(iid)
	e.now = e.now.Add(24 * time.Hour)
	again, replayed, err := runCompletion(t, e, iid, "retry", 1, omitted)
	if err != nil || !replayed || again.ID != first.ID || again.CompletedOn != first.CompletedOn {
		t.Fatalf("omitted replay=%+v replayed=%v err=%v first=%+v", again, replayed, err, first)
	}
	row2, _ := e.repo.Row(iid)
	if row2.Version != row.Version || row2.Done != row.Done {
		t.Fatalf("replay mutated item before=%+v after=%+v", row, row2)
	}
	// A replay remains valid despite the new local day, even with a submitted date that would be future now.
	past := "2026-10-20"
	explicitEnv := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, AttentionOn: d("2026-09-01")})
	original, _, err := runCompletion(t, explicitEnv, missingID, "explicit-retry", 1, &past)
	if err != nil {
		t.Fatal(err)
	}
	explicitEnv.now = explicitEnv.now.Add(24 * time.Hour)
	replay, replayed, err := runCompletion(t, explicitEnv, missingID, "explicit-retry", 1, &past)
	if err != nil || !replayed || replay.ID != original.ID || replay.CompletedOn != original.CompletedOn {
		t.Fatalf("explicit retry=%+v replay=%v err=%v original=%+v", replay, replayed, err, original)
	}
	stale := "2026-10-19"
	if _, _, err = runCompletion(t, explicitEnv, missingID, "explicit-retry", 99, &stale); !errors.Is(err, item.ErrIdempotencyKeyReused) {
		t.Fatalf("reused key should precede stale/date check: %v", err)
	}
	archivedID := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b85"
	archivedEnv := completionEnv("UTC", "2026-10-20", item.Item{ID: archivedID, Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, AttentionOn: d("2026-09-01")})
	day := "2026-10-20"
	receipt, _, err := runCompletion(t, archivedEnv, archivedID, "archive-replay", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	_, err = archivedEnv.svc.Update(context.Background(), userID, householdID, archivedID, 2, item.Patch{Archived: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	replayedReceipt, replayed, err := runCompletion(t, archivedEnv, archivedID, "archive-replay", 1, &day)
	if err != nil || !replayed || replayedReceipt.ID != receipt.ID || replayedReceipt.CompletedOn != receipt.CompletedOn {
		t.Fatalf("archived replay=%+v %v %v", replayedReceipt, replayed, err)
	}
}

func TestCompletionDateValidationOverflowAndSnapshot(t *testing.T) {
	iid := missingID
	e := completionEnv("UTC", "9999-12-31", item.Item{ID: iid, AttentionOn: d("9999-12-31"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}})
	_, _, err := runCompletion(t, e, iid, "overflow", 1, nil)
	validation(t, err, "recurrence", "date_overflow")
	row, _ := e.repo.Row(iid)
	history, _ := e.svc.ListCompletions(context.Background(), userID, householdID, iid, 50, "")
	if row.Version != 1 || row.Done || len(history.Items) != 0 {
		t.Fatalf("overflow persisted state=%+v history=%+v", row, history)
	}
}

func TestCompletionPolicySnapshotSurvivesLaterEdit(t *testing.T) {
	iid := missingID
	original := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: iid, AttentionOn: d("2026-09-01"), Recurrence: original})
	on := "2026-10-20"
	receipt, _, err := runCompletion(t, e, iid, "snapshot", 1, &on)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := e.repo.Row(iid)
	changed := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}
	_, err = e.svc.Update(context.Background(), userID, householdID, iid, 2, item.Patch{Recurrence: item.Some(*changed)})
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.svc.ListCompletions(context.Background(), userID, householdID, iid, 50, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Recurrence == nil || *page.Items[0].Recurrence != *original || page.Items[0].NextAttentionOn == nil || page.Items[0].NextAttentionOn.String() != "2027-09-01" {
		t.Fatalf("receipt=%+v err=%v", page, err)
	}
	now, _ := e.repo.Row(iid)
	if now.Recurrence == nil || *now.Recurrence != *changed || stored.Version != 2 || receipt.ItemVersionBefore != 1 {
		t.Fatalf("item=%+v receipt=%+v", now, receipt)
	}
}

func TestUndoLatestCompletionRestoresCycleAndChains(t *testing.T) {
	policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, AttentionOn: d("2026-09-01"), Recurrence: policy, WorkflowState: item.StateWaiting})
	day := "2026-10-20"
	first, _, err := runCompletion(t, e, missingID, "first", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := runCompletion(t, e, missingID, "second", 2, &day)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, first.ID, 3); !errors.Is(err, item.ErrCompletionNotLatest) {
		t.Fatalf("not latest err=%v", err)
	}
	undone, err := e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, second.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := e.repo.Row(missingID)
	if row.Version != 4 || row.AttentionOn == nil || row.AttentionOn.String() != "2027-09-01" || row.WorkflowState != item.StateOpen || row.LastCompletedOn == nil || row.LastCompletedOn.String() != day {
		t.Fatalf("restored=%+v", row)
	}
	// Replay is independent of the now-stale ETag and does not increment again.
	replay, err := e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, second.ID, 1)
	if err != nil || replay.UndoneAt == nil || undone.ID != replay.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	after, _ := e.repo.Row(missingID)
	if after.Version != 4 {
		t.Fatalf("replay changed version: %+v", after)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, first.ID, 4); err != nil {
		t.Fatal(err)
	}
	row, _ = e.repo.Row(missingID)
	if row.Version != 5 || row.AttentionOn == nil || row.AttentionOn.String() != "2026-09-01" || row.WorkflowState != item.StateWaiting || row.LastCompletedOn != nil {
		t.Fatalf("chained restore=%+v", row)
	}
}

func TestUndoOneOffAndPolicyUnaffected(t *testing.T) {
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, AttentionOn: d("2026-09-01"), WorkflowState: item.StateWaiting})
	day := "2026-10-20"
	receipt, _, err := runCompletion(t, e, missingID, "oneoff", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, receipt.ID, 2); err != nil {
		t.Fatal(err)
	}
	row, _ := e.repo.Row(missingID)
	if row.Done || row.Version != 3 || row.WorkflowState != item.StateWaiting || row.AttentionOn == nil || row.AttentionOn.String() != "2026-09-01" || row.LastCompletedOn != nil {
		t.Fatalf("undo state=%+v", row)
	}
}

func TestUndoPreservesCurrentEditsExceptCycleSnapshot(t *testing.T) {
	anchor := d("2026-09-01")
	fixed := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, AttentionOn: anchor, Recurrence: fixed, WorkflowState: item.StateWaiting, Title: "before"})
	day := "2026-10-20"
	receipt, _, err := runCompletion(t, e, missingID, "edit-undo", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	fluid := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}
	title, notes, state, attention := "edited title", "edited notes", string(item.StatePaused), "2030-04-05"
	_, err = e.svc.Update(context.Background(), userID, householdID, missingID, 2, item.Patch{Title: &title, Notes: item.Some(notes), WorkflowState: &state, AttentionOn: item.Some(attention), Recurrence: item.Some(fluid)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, receipt.ID, 3); err != nil {
		t.Fatal(err)
	}
	row, _ := e.repo.Row(missingID)
	if row.Title != title || row.Notes == nil || *row.Notes != notes || row.SubjectID != subjectID || row.Recurrence == nil || *row.Recurrence != fluid || row.AttentionOn == nil || row.AttentionOn.String() != "2026-09-01" || row.WorkflowState != item.StateWaiting || row.Done || row.Version != 4 {
		t.Fatalf("undo did not preserve user edits/restore snapshot: %+v", row)
	}
}

func TestUndoOverwritesAttentionEditedAfterCompletion(t *testing.T) {
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, AttentionOn: d("2026-09-01"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}, WorkflowState: item.StateWaiting})
	day := "2026-10-20"
	receipt, _, err := runCompletion(t, e, missingID, "attention-snapshot", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	newAttention := "2030-04-05"
	if _, err = e.svc.Update(context.Background(), userID, householdID, missingID, 2, item.Patch{AttentionOn: item.Some(newAttention)}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, receipt.ID, 3); err != nil {
		t.Fatal(err)
	}
	row, _ := e.repo.Row(missingID)
	if row.AttentionOn == nil || row.AttentionOn.String() != "2026-09-01" || row.WorkflowState != item.StateWaiting || row.Done || row.Version != 4 {
		t.Fatalf("attention snapshot not restored: %+v", row)
	}
}

func TestUndoRejectsStaleAndArchived(t *testing.T) {
	e := completionEnv("UTC", "2026-10-20", item.Item{ID: missingID, AttentionOn: d("2026-09-01"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}})
	day := "2026-10-20"
	c, _, err := runCompletion(t, e, missingID, "undo", 1, &day)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, c.ID, 1); !errors.Is(err, item.ErrVersionMismatch) {
		t.Fatalf("stale=%v", err)
	}
	_, err = e.svc.Update(context.Background(), userID, householdID, missingID, 2, item.Patch{Archived: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UndoCompletion(context.Background(), userID, householdID, missingID, c.ID, 3); !errors.Is(err, item.ErrArchived) {
		t.Fatalf("archived=%v", err)
	}
}

func TestListCompletionsInvalidQueryNames(t *testing.T) {
	e := newEnv("UTC")
	for _, tc := range []struct {
		limit  int
		cursor string
		param  string
	}{{101, "", "limit"}, {50, "bad", "cursor"}} {
		_, err := e.svc.ListCompletions(context.Background(), userID, householdID, missingID, tc.limit, tc.cursor)
		var invalid *item.InvalidQueryError
		if !errors.As(err, &invalid) || invalid.Parameter != tc.param {
			t.Fatalf("query=%+v err=%v", tc, err)
		}
	}
}

func TestIdempotencyKeyValidationAndFingerprintPresence(t *testing.T) {
	for _, tc := range []struct {
		key   string
		valid bool
	}{{"x", true}, {"a b", false}, {"", false}, {string(make([]byte, 129)), false}, {"!~", true}} {
		if got := item.ValidateIdempotencyKey(tc.key); got != tc.valid {
			t.Errorf("ValidateIdempotencyKey(%q)=%v want %v", tc.key, got, tc.valid)
		}
	}
	date := "2026-10-07"
	if item.CompletionFingerprint("actor", nil) == item.CompletionFingerprint("actor", &date) {
		t.Fatal("omitted and explicit dates must differ")
	}
	if item.CompletionFingerprint("actor", nil) == item.CompletionFingerprint("missingID", nil) {
		t.Fatal("actor missing from fingerprint")
	}
}

// Compile-time public-surface assertion for the in-memory completion repository.
var _ item.Repository = (*itemtest.Repo)(nil)
