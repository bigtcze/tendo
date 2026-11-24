package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeInvitationSweepStore struct {
	cleared int
	err     error
	cutoff  time.Time
	calls   int
}

func (f *fakeInvitationSweepStore) SweepExpiredInvitationDigests(_ context.Context, cutoff time.Time) (int, error) {
	f.calls++
	f.cutoff = cutoff
	return f.cleared, f.err
}

// TestSweepInvitationDigestsClassifiesOutcomes pins the fixed result classes:
// a lock yield is a partial success, a finished context returns itself, and any
// other store failure collapses to ErrInvitationSweepFailed without its text.
func TestSweepInvitationDigestsClassifiesOutcomes(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	for _, tc := range []struct {
		name    string
		err     error
		want    error
		locked  bool
		cleared int
	}{
		{name: "ok", cleared: 3},
		{name: "locked", err: ErrInvitationSweepLocked, locked: true, cleared: 2},
		{name: "timeout", err: context.DeadlineExceeded, want: context.DeadlineExceeded, cleared: 1},
		{name: "canceled", err: context.Canceled, want: context.Canceled},
		{name: "driver", err: errors.New("pq: secret driver text"), want: ErrInvitationSweepFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeInvitationSweepStore{cleared: tc.cleared, err: tc.err}
			report, err := SweepInvitationDigests(context.Background(), store, now)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if err != nil && err.Error() == "pq: secret driver text" {
				t.Fatal("driver text escaped")
			}
			if report.Locked != tc.locked || report.Cleared != tc.cleared {
				t.Fatalf("report=%+v", report)
			}
			if !store.cutoff.Equal(now) || store.cutoff.Location() != time.UTC {
				t.Fatalf("cutoff=%v want %v in UTC", store.cutoff, now)
			}
		})
	}
}

// TestSweepInvitationDigestsFailsClosed pins that a missing store and an ended
// context never reach the store.
func TestSweepInvitationDigestsFailsClosed(t *testing.T) {
	if _, err := SweepInvitationDigests(context.Background(), nil, time.Now()); !errors.Is(err, ErrInvitationSweepFailed) {
		t.Fatalf("nil store err=%v", err)
	}
	store := &fakeInvitationSweepStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SweepInvitationDigests(ctx, store, time.Now()); !errors.Is(err, context.Canceled) || store.calls != 0 {
		t.Fatalf("ended context err=%v calls=%d", err, store.calls)
	}
}
