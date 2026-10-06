// Package subjecttest provides an in-memory subject.Repository for tests. It
// follows the repository contract: every operation is household-scoped, updates
// are version-checked, and listing is ordered by id.
package subjecttest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bigtcze/tendo/backend/internal/subject"
)

type Repo struct {
	mu    sync.Mutex
	rows  map[string]subject.Subject
	Calls int
	// Err, when set, is returned by every operation.
	Err error
	// Clock stamps created and updated times.
	Clock time.Time
	seq   int
}

func New() *Repo {
	return &Repo{rows: map[string]subject.Subject{}, Clock: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)}
}

// Seed stores a row as-is.
func (r *Repo) Seed(s subject.Subject) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[s.ID] = s
}

// Row returns the stored row, bypassing the service.
func (r *Repo) Row(id string) (subject.Subject, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rows[id]
	return s, ok
}

func (r *Repo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

func (r *Repo) Create(_ context.Context, householdID, name string, t subject.Type) (subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, r.Err
	}
	r.seq++
	id := "0198a2f0-7c1e-7a53-9b0e-" + pad(r.seq)
	s := subject.Subject{ID: id, HouseholdID: householdID, Type: t, Name: name, CreatedAt: r.Clock, UpdatedAt: r.Clock, Version: 1}
	r.rows[id] = s
	return s, nil
}

func (r *Repo) Get(_ context.Context, householdID, subjectID string) (subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, r.Err
	}
	s, ok := r.rows[subjectID]
	if !ok || s.HouseholdID != householdID {
		return subject.Subject{}, subject.ErrNotFound
	}
	return s, nil
}

func (r *Repo) List(_ context.Context, householdID string, archived bool, afterID string, limit int) ([]subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	var all []subject.Subject
	for _, s := range r.rows {
		if s.HouseholdID == householdID && s.Archived == archived && s.ID > afterID {
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *Repo) Update(_ context.Context, householdID, subjectID string, expected int64, p subject.Patch) (subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, r.Err
	}
	s, ok := r.rows[subjectID]
	if !ok || s.HouseholdID != householdID {
		return subject.Subject{}, subject.ErrNotFound
	}
	if s.Version != expected {
		return subject.Subject{}, subject.ErrVersionMismatch
	}
	if p.Name != nil {
		s.Name = *p.Name
	}
	if p.Type != nil {
		s.Type = *p.Type
	}
	if p.Archived != nil {
		s.Archived = *p.Archived
	}
	s.Version++
	s.UpdatedAt = r.Clock.Add(time.Hour)
	r.rows[subjectID] = s
	return s, nil
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
