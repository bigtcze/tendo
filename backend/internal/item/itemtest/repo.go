// Package itemtest provides an in-memory item.Repository for tests. It follows
// the repository contract: every operation is household-scoped, updates are
// version-checked, and listing is ordered by id.
package itemtest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

type Repo struct {
	mu    sync.Mutex
	rows  map[string]item.Item
	Calls int
	// Err, when set, is returned by every operation.
	Err error
	// Clock stamps created and updated times.
	Clock       time.Time
	seq         int
	completions []item.Completion
}

func New() *Repo {
	return &Repo{rows: map[string]item.Item{}, Clock: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)}
}

// Seed stores a row as-is.
func (r *Repo) Seed(i item.Item) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[i.ID] = i
}

// Row returns the stored row, bypassing the service.
func (r *Repo) Row(id string) (item.Item, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.rows[id]
	return i, ok
}

func (r *Repo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

func (r *Repo) CompletionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.completions)
}

func (r *Repo) Complete(_ context.Context, householdID, itemID, key string, fingerprint [32]byte, decide item.CompletionDecider) (item.Completion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.rows[itemID]
	if !ok || current.HouseholdID != householdID {
		return item.Completion{}, false, item.ErrNotFound
	}
	var existing *item.Completion
	for i := range r.completions {
		if r.completions[i].ItemID == itemID && r.completions[i].IdempotencyKey == key {
			if r.completions[i].UndoneAt != nil {
				original := r.completions[i]
				if original.CycleAttentionOn != nil {
					if original.Recurrence != nil {
						next, e := schedule.NextCycle(original.CycleAttentionOn, original.CompletedOn, *original.Recurrence)
						if e == nil {
							original.NextAttentionOn = next
						}
					} else {
						original.NextAttentionOn = nil
					}
				}
				existing = &original
			} else {
				existing = &r.completions[i]
			}
			break
		}
	}
	plan, err := decide(current, existing)
	if err != nil {
		return item.Completion{}, false, err
	}
	if existing != nil {
		return *existing, true, nil
	}
	r.seq++
	plan.Receipt.ID = "0198a2f0-7c1e-7a53-9b0e-" + pad(r.seq)
	plan.Receipt.Fingerprint = fingerprint
	plan.Receipt.CreatedAt = r.Clock
	plan.Item.UpdatedAt = r.Clock.Add(time.Hour)
	r.rows[itemID] = plan.Item
	r.completions = append(r.completions, plan.Receipt)
	return plan.Receipt, false, nil
}
func (r *Repo) UndoCompletion(_ context.Context, householdID, itemID, completionID string, decide item.CompletionUndoDecider) (item.Completion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.rows[itemID]
	if !ok || current.HouseholdID != householdID {
		return item.Completion{}, item.ErrNotFound
	}
	idx := -1
	for i := range r.completions {
		if r.completions[i].ID == completionID && r.completions[i].ItemID == itemID && r.completions[i].HouseholdID == householdID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return item.Completion{}, item.ErrNotFound
	}
	var latest *int64
	for i := range r.completions {
		c := r.completions[i]
		if c.ItemID == itemID && c.UndoneAt == nil && (latest == nil || c.ItemVersionBefore > *latest) {
			v := c.ItemVersionBefore
			latest = &v
		}
	}
	plan, err := decide(current, r.completions[idx], latest)
	if err != nil {
		return item.Completion{}, err
	}
	if plan.Apply {
		plan.Receipt.UndoneAt = ptrTime(plan.UndoneAt)
		userID := plan.UndoneByUserID
		plan.Receipt.UndoneByUserID = &userID
		plan.Item.UpdatedAt = r.Clock
		r.rows[itemID] = plan.Item
		r.completions[idx] = plan.Receipt
		current.LastCompletedOn = nil
		var lastVersion int64
		for i := range r.completions {
			c := r.completions[i]
			if c.ItemID == itemID && c.UndoneAt == nil && c.ItemVersionBefore > lastVersion {
				date := c.CompletedOn
				current.LastCompletedOn = &date
				lastVersion = c.ItemVersionBefore
			}
		}
		plan.Item.LastCompletedOn = current.LastCompletedOn
		r.rows[itemID] = plan.Item
	}
	return plan.Receipt, nil
}

func ptrTime(t time.Time) *time.Time { return &t }

func (r *Repo) ListCompletions(_ context.Context, householdID, itemID, after string, limit int) ([]item.Completion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.rows[itemID]; !ok || i.HouseholdID != householdID {
		return nil, item.ErrNotFound
	}
	var out []item.Completion
	for _, v := range r.completions {
		if v.ItemID == itemID && v.HouseholdID == householdID && v.ID > after {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *Repo) Create(_ context.Context, householdID string, d item.Draft) (item.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return item.Item{}, r.Err
	}
	r.seq++
	id := "0198a2f0-7c1e-7a53-9b0e-" + pad(r.seq)
	i := item.Item{ID: id, HouseholdID: householdID, SubjectID: d.SubjectID, Title: d.Title, Notes: d.Notes, AttentionOn: d.AttentionOn, Recurrence: d.Recurrence, WorkflowState: item.StateOpen, CreatedAt: r.Clock, UpdatedAt: r.Clock, Version: 1}
	if d.InitializationReceipt != nil {
		i.Version = 2
		r.seq++
		receipt := *d.InitializationReceipt
		receipt.ID = "0198a2f0-7c1e-7a53-9b0e-" + pad(r.seq)
		receipt.HouseholdID, receipt.ItemID = householdID, id
		receipt.IdempotencyKey = "0198a2f0-7c1e-7a53-9b0e-000000000001"
		receipt.CreatedAt = r.Clock
		completed := receipt.CompletedOn
		i.LastCompletedOn = &completed
		r.completions = append(r.completions, receipt)
	}
	r.rows[id] = i
	return i, nil
}

func (r *Repo) Get(_ context.Context, householdID, itemID string) (item.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return item.Item{}, r.Err
	}
	i, ok := r.rows[itemID]
	if !ok || i.HouseholdID != householdID {
		return item.Item{}, item.ErrNotFound
	}
	return i, nil
}

func (r *Repo) List(_ context.Context, householdID string, archived, done bool, afterID string, limit int) ([]item.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	var all []item.Item
	for _, i := range r.rows {
		if i.HouseholdID == householdID && i.Archived == archived && i.Done == done && i.ID > afterID {
			all = append(all, i)
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].ID < all[b].ID })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *Repo) Update(_ context.Context, householdID, itemID string, expected int64, c item.Change) (item.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return item.Item{}, r.Err
	}
	i, ok := r.rows[itemID]
	if !ok || i.HouseholdID != householdID {
		return item.Item{}, item.ErrNotFound
	}
	if i.Version != expected {
		return item.Item{}, item.ErrVersionMismatch
	}
	if c.Title != nil {
		i.Title = *c.Title
	}
	if c.SubjectID != nil {
		i.SubjectID = *c.SubjectID
	}
	if c.Notes.Set {
		i.Notes = c.Notes.Value
	}
	if c.AttentionOn.Set {
		i.AttentionOn = c.AttentionOn.Value
	}
	if c.Recurrence.Set {
		i.Recurrence = c.Recurrence.Value
	}
	if c.WorkflowState != nil {
		i.WorkflowState = *c.WorkflowState
	}
	if c.Archived != nil {
		i.Archived = *c.Archived
	}
	// Done is server-owned and is not writable through Change.
	i.Version++
	i.UpdatedAt = r.Clock.Add(time.Hour)
	r.rows[itemID] = i
	return i, nil
}

// Date builds a validated schedule date for fixtures.
func Date(year int, month time.Month, day int) *schedule.Date {
	d, err := schedule.NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return &d
}

func pad(n int) string {
	const digits = "0123456789"
	b := []byte("000000000000")
	for i := len(b) - 1; i >= 0 && n > 0; i-- {
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b)
}
