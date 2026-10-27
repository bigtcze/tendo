package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
)

const concurrencyIterations = 25

func runTogether(t *testing.T, left, right func()) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; left() }()
	go func() { defer wg.Done(); <-start; right() }()
	close(start)
	wg.Wait()
}

func TestConcurrentItemMutationInvariants(t *testing.T) {
	f := newAttentionFixture(t)
	date := mustTestDate(2026, time.March, 1)
	complete := func(i item.Item, key string) (item.Completion, bool, error) {
		return f.svc.Complete(f.ctx, f.actor, f.house, i.ID, i.Version, key, item.CompletionRequest{CompletedOn: ptrConcurrency(date.String())})
	}

	t.Run("same-version patches", func(t *testing.T) {
		for n := range concurrencyIterations {
			i := f.add(t, f.subject, date.String(), false, false, false, string(item.StateOpen), nil)
			before, err := f.repo.Get(f.ctx, f.house, i)
			if err != nil {
				t.Fatal(err)
			}
			titles := [2]string{fmt.Sprintf("left-%d", n), fmt.Sprintf("right-%d", n)}
			var errs [2]error
			runTogether(t, func() {
				_, errs[0] = f.svc.Update(f.ctx, f.actor, f.house, i, before.Version, item.Patch{Title: &titles[0]})
			}, func() {
				_, errs[1] = f.svc.Update(f.ctx, f.actor, f.house, i, before.Version, item.Patch{Title: &titles[1]})
			})
			winner := -1
			for j, e := range errs {
				if e == nil {
					if winner != -1 {
						t.Fatalf("iteration %d: both patches won", n)
					}
					winner = j
				} else if !errors.Is(e, item.ErrVersionMismatch) {
					t.Fatalf("iteration %d patch error=%v", n, e)
				}
			}
			if winner == -1 {
				t.Fatalf("iteration %d: no patch winner", n)
			}
			got, err := f.repo.Get(f.ctx, f.house, i)
			if err != nil || got.Version != before.Version+1 || got.Title != titles[winner] {
				t.Fatalf("iteration %d item=%+v err=%v", n, got, err)
			}
		}
	})

	t.Run("completion and patch", func(t *testing.T) {
		for n := range concurrencyIterations {
			i := f.add(t, f.subject, date.String(), false, false, false, string(item.StateOpen), nil)
			before, _ := f.repo.Get(f.ctx, f.house, i)
			title := fmt.Sprintf("patch-%d", n)
			var ce, pe error
			var receipt item.Completion
			runTogether(t, func() { receipt, _, ce = complete(before, fmt.Sprintf("concurrent-complete-%d", n)) }, func() { _, pe = f.svc.Update(f.ctx, f.actor, f.house, i, before.Version, item.Patch{Title: &title}) })
			if (ce == nil) == (pe == nil) {
				t.Fatalf("iteration %d expected one winner: completion=%v patch=%v", n, ce, pe)
			}
			loser := ce
			if ce == nil {
				loser = pe
			}
			if !errors.Is(loser, item.ErrVersionMismatch) {
				t.Fatalf("iteration %d loser=%v", n, loser)
			}
			got, err := f.repo.Get(f.ctx, f.house, i)
			var count int
			if e := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if err != nil || got.Version != before.Version+1 || (ce == nil && (count != 1 || !got.Done || receipt.ID == "")) || (pe == nil && (count != 0 || got.Title != title || got.Done)) {
				t.Fatalf("iteration %d final=%+v receipts=%d completion=%v patch=%v err=%v", n, got, count, ce, pe, err)
			}
		}
	})

	t.Run("two undos", func(t *testing.T) {
		for n := range concurrencyIterations {
			i := f.add(t, f.subject, date.String(), false, false, false, string(item.StateOpen), nil)
			before, _ := f.repo.Get(f.ctx, f.house, i)
			r, _, err := complete(before, fmt.Sprintf("undo-seed-%d", n))
			if err != nil {
				t.Fatal(err)
			}
			completed, _ := f.repo.Get(f.ctx, f.house, i)
			var results [2]item.Completion
			var errs [2]error
			runTogether(t, func() {
				results[0], errs[0] = f.svc.UndoCompletion(f.ctx, f.actor, f.house, i, r.ID, completed.Version)
			}, func() {
				results[1], errs[1] = f.svc.UndoCompletion(f.ctx, f.actor, f.house, i, r.ID, completed.Version)
			})
			if errs[0] != nil || errs[1] != nil {
				t.Fatalf("iteration %d undo errors=%v", n, errs)
			}
			if results[0].ID != r.ID || results[1].ID != r.ID || results[0].UndoneAt == nil || results[1].UndoneAt == nil || !results[0].UndoneAt.Equal(*results[1].UndoneAt) || results[0].UndoneByUserID == nil || results[1].UndoneByUserID == nil || *results[0].UndoneByUserID != *results[1].UndoneByUserID {
				t.Fatalf("iteration %d replay receipts differ: first=%+v second=%+v", n, results[0], results[1])
			}
			var total, undone int
			if err := f.admin.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER (WHERE undone_at IS NOT NULL) FROM item_completions WHERE item_id=$1::uuid`, i).Scan(&total, &undone); err != nil {
				t.Fatal(err)
			}
			got, err := f.repo.Get(f.ctx, f.house, i)
			if err != nil || total != 1 || undone != 1 || got.Version != completed.Version+1 || got.Done || got.AttentionOn == nil || got.AttentionOn.String() != date.String() || got.WorkflowState != item.StateOpen {
				t.Fatalf("iteration %d receipts=%d undone=%d item=%+v err=%v", n, total, undone, got, err)
			}
		}
	})

	t.Run("undo versus new completion", func(t *testing.T) {
		for n := range concurrencyIterations {
			i := f.add(t, f.subject, date.String(), false, false, true, string(item.StateOpen), nil)
			before, _ := f.repo.Get(f.ctx, f.house, i)
			first, _, err := complete(before, fmt.Sprintf("undo-new-first-%d", n))
			if err != nil {
				t.Fatal(err)
			}
			current, _ := f.repo.Get(f.ctx, f.house, i)
			var ue, ce error
			runTogether(t, func() { _, ue = f.svc.UndoCompletion(f.ctx, f.actor, f.house, i, first.ID, current.Version) }, func() { _, _, ce = complete(current, fmt.Sprintf("undo-new-second-%d", n)) })
			if (ue == nil) == (ce == nil) {
				t.Fatalf("iteration %d expected one winner undo=%v completion=%v", n, ue, ce)
			}
			loser := ue
			if ue == nil {
				loser = ce
			}
			if !errors.Is(loser, item.ErrVersionMismatch) {
				t.Fatalf("iteration %d loser=%v", n, loser)
			}
			got, err := f.repo.Get(f.ctx, f.house, i)
			var total, active, undone int
			if e := f.admin.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER (WHERE undone_at IS NULL),count(*) FILTER (WHERE undone_at IS NOT NULL) FROM item_completions WHERE item_id=$1::uuid`, i).Scan(&total, &active, &undone); e != nil {
				t.Fatal(e)
			}
			if err != nil || got.Version != current.Version+1 || (ue == nil && (total != 1 || active != 0 || undone != 1 || got.AttentionOn == nil || got.AttentionOn.String() != date.String())) || (ce == nil && (total != 2 || active != 2 || undone != 0 || got.AttentionOn == nil || got.AttentionOn.String() == date.String())) {
				t.Fatalf("iteration %d final=%+v receipts=%d active=%d undone=%d undo=%v complete=%v err=%v", n, got, total, active, undone, ue, ce, err)
			}
		}
	})

	t.Run("archive and completion", func(t *testing.T) {
		for n := range concurrencyIterations {
			i := f.add(t, f.subject, date.String(), false, false, false, string(item.StateOpen), nil)
			before, _ := f.repo.Get(f.ctx, f.house, i)
			archived := true
			var ce, ae error
			runTogether(t, func() { _, _, ce = complete(before, fmt.Sprintf("archive-race-%d", n)) }, func() {
				_, ae = f.svc.Update(f.ctx, f.actor, f.house, i, before.Version, item.Patch{Archived: &archived})
			})
			if (ce == nil) == (ae == nil) {
				t.Fatalf("iteration %d expected one winner completion=%v archive=%v", n, ce, ae)
			}
			loser := ce
			if ce == nil {
				loser = ae
			}
			if !errors.Is(loser, item.ErrVersionMismatch) && !errors.Is(loser, item.ErrArchived) {
				t.Fatalf("iteration %d loser=%v", n, loser)
			}
			got, err := f.repo.Get(f.ctx, f.house, i)
			var count int
			if e := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if err != nil || got.Version != before.Version+1 || (ce == nil && (count != 1 || got.Archived)) || (ae == nil && (count != 0 || !got.Archived)) {
				t.Fatalf("iteration %d item=%+v receipts=%d completion=%v archive=%v err=%v", n, got, count, ce, ae, err)
			}
		}
	})
}

func ptrConcurrency(s string) *string { return &s }
