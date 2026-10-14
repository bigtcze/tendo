// Package subjecttest provides an in-memory subject repository for tests.
package subjecttest

import (
	"context"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"sort"
	"sync"
	"time"
)

type Repo struct {
	mu          sync.Mutex
	rows        map[string]subject.Subject
	Calls       int
	FindCalls   int
	Err         error
	Clock       time.Time
	seq         int
	creations   map[string][]byte
	creationIDs map[string]string
}

func New() *Repo {
	return &Repo{rows: map[string]subject.Subject{}, creations: map[string][]byte{}, creationIDs: map[string]string{}, Clock: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)}
}
func (r *Repo) Seed(s subject.Subject) { r.mu.Lock(); defer r.mu.Unlock(); r.rows[s.ID] = s }
func (r *Repo) Row(id string) (subject.Subject, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rows[id]
	return s, ok
}
func (r *Repo) Count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.rows) }
func (r *Repo) CreateIdempotent(_ context.Context, hid, actor, key, name string, t subject.Type, fp []byte) (subject.Subject, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, false, r.Err
	}
	ck := hid + "/" + actor + "/" + key
	if _, ok := r.creations[ck]; ok {
		return subject.Subject{}, false, nil
	}
	r.seq++
	id := "0198a2f0-7c1e-7a53-9b0e-" + pad(r.seq)
	s := subject.Subject{ID: id, HouseholdID: hid, Type: t, Name: name, CreatedAt: r.Clock, UpdatedAt: r.Clock, Version: 1}
	r.rows[id] = s
	r.creations[ck] = append([]byte(nil), fp...)
	r.creationIDs[ck] = id
	return s, true, nil
}
func (r *Repo) FindCreation(_ context.Context, hid, actor, key string) (subject.Subject, []byte, error) {
	r.mu.Lock()
	r.FindCalls++
	defer r.mu.Unlock()
	if r.Err != nil {
		return subject.Subject{}, nil, r.Err
	}
	ck := hid + "/" + actor + "/" + key
	fp, ok := r.creations[ck]
	if !ok {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	s, ok := r.rows[r.creationIDs[ck]]
	if !ok {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	return s, append([]byte(nil), fp...), nil
}
func (r *Repo) Get(_ context.Context, hid, sid string) (subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, r.Err
	}
	s, ok := r.rows[sid]
	if !ok || s.HouseholdID != hid {
		return subject.Subject{}, subject.ErrNotFound
	}
	return s, nil
}
func (r *Repo) List(_ context.Context, hid string, archived bool, after string, limit int) ([]subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	var all []subject.Subject
	for _, s := range r.rows {
		if s.HouseholdID == hid && s.Archived == archived && s.ID > after {
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
func (r *Repo) Update(_ context.Context, hid, sid string, expected int64, p subject.Patch) (subject.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls++
	if r.Err != nil {
		return subject.Subject{}, r.Err
	}
	s, ok := r.rows[sid]
	if !ok || s.HouseholdID != hid {
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
	r.rows[sid] = s
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
