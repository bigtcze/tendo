package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedSweepStore returns a fixed outcome for every step. It records the
// steps it was asked to run.
type scriptedSweepStore struct {
	err   error
	steps []MCPSweepStep
}

func (s *scriptedSweepStore) SweepStep(ctx context.Context, step MCPSweepStep, _ time.Time) (int, error) {
	s.steps = append(s.steps, step)
	if s.err != nil {
		return 0, s.err
	}
	return 1, nil
}

// sweepRepository is a repository that also implements the sweep store.
type sweepRepository struct {
	registerFake
	sweep *scriptedSweepStore
}

func (r *sweepRepository) SweepStep(ctx context.Context, step MCPSweepStep, now time.Time) (int, error) {
	return r.sweep.SweepStep(ctx, step, now)
}

func newSweepService(t *testing.T, store *scriptedSweepStore) *MCPService {
	t.Helper()
	repo := &sweepRepository{sweep: store}
	svc, err := NewMCPService(repo, MCPOptions{Enabled: false}, func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestSweepPropagatesContextErrors pins that a finished context ends the run
// with its own error, so the sweeper can classify a timeout or a cancellation
// instead of seeing the fixed storage class.
func TestSweepPropagatesContextErrors(t *testing.T) {
	for _, ctxErr := range []error{context.DeadlineExceeded, context.Canceled} {
		store := &scriptedSweepStore{err: ctxErr}
		svc := newSweepService(t, store)
		_, err := svc.Sweep(context.Background(), time.Now())
		if !errors.Is(err, ctxErr) {
			t.Fatalf("Sweep err=%v, want %v", err, ctxErr)
		}
		if errors.Is(err, ErrMCPSweepFailed) {
			t.Fatalf("context error %v mapped to the storage class", ctxErr)
		}
	}
}

// TestSweepStopsWhenContextEnds proves a context already done before a step
// starts ends the run with the context error, without running any step.
func TestSweepStopsWhenContextEnds(t *testing.T) {
	store := &scriptedSweepStore{}
	svc := newSweepService(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Sweep(ctx, time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sweep err=%v, want context.Canceled", err)
	}
	if len(store.steps) != 0 {
		t.Fatalf("steps ran after the context ended: %v", store.steps)
	}
}

// TestSweepStorageFailureUsesFixedClass pins that any other failure is the
// fixed class, never the driver text, and that the run stops.
func TestSweepStorageFailureUsesFixedClass(t *testing.T) {
	store := &scriptedSweepStore{err: errors.New("pq: secret driver text")}
	svc := newSweepService(t, store)
	_, err := svc.Sweep(context.Background(), time.Now())
	if !errors.Is(err, ErrMCPSweepFailed) {
		t.Fatalf("Sweep err=%v, want ErrMCPSweepFailed", err)
	}
	if err.Error() != ErrMCPSweepFailed.Error() {
		t.Fatalf("Sweep err text %q leaked the driver error", err.Error())
	}
}

// TestSweepLockYieldContinuesRun pins that a lock yield ends only that step.
func TestSweepLockYieldContinuesRun(t *testing.T) {
	store := &scriptedSweepStore{err: ErrMCPSweepLocked}
	svc := newSweepService(t, store)
	report, err := svc.Sweep(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(report.Locked) != len(MCPSweepSteps) || len(store.steps) != len(MCPSweepSteps) {
		t.Fatalf("locked=%v steps=%v; every step must run and yield", report.Locked, store.steps)
	}
}
