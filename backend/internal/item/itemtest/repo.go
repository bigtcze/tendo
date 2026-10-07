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
	Clock time.Time
	seq   int
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

func (r *Repo) List(_ context.Context, householdID string, archived bool, afterID string, limit int) ([]item.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	var all []item.Item
	for _, i := range r.rows {
		if i.HouseholdID == householdID && i.Archived == archived && i.ID > afterID {
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
