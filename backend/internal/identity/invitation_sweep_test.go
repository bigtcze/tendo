package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// scriptedInvitationSweepStore returns a fixed count and error per step and
// records the steps and cutoffs it was asked to run.
type scriptedInvitationSweepStore struct {
	counts  map[InvitationSweepStep]int
	errs    map[InvitationSweepStep]error
	steps   []InvitationSweepStep
	cutoffs []time.Time
}

func (f *scriptedInvitationSweepStore) SweepInvitationStep(_ context.Context, step InvitationSweepStep, cutoff time.Time) (int, error) {
	f.steps = append(f.steps, step)
	f.cutoffs = append(f.cutoffs, cutoff)
	return f.counts[step], f.errs[step]
}

// TestInvitationSweepStepNamesAndOrder pins the step names, which appear in
// logs, and that every step runs once per pass in the declared order.
func TestInvitationSweepStepNamesAndOrder(t *testing.T) {
	want := []InvitationSweepStep{"expired_digests_cleared", "oidc_flows_expired", "oidc_pending_logins_expired"}
	if !slices.Equal(InvitationSweepSteps, want) {
		t.Fatalf("steps=%v want %v", InvitationSweepSteps, want)
	}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	store := &scriptedInvitationSweepStore{counts: map[InvitationSweepStep]int{
		InvitationSweepExpiredDigests: 3, InvitationSweepOIDCFlowsExpired: 2, InvitationSweepOIDCPendingLoginsExpired: 1,
	}}
	report, err := SweepInvitationDigests(context.Background(), store, now)
	if err != nil || len(report.Locked) != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if !slices.Equal(store.steps, want) {
		t.Fatalf("ran %v", store.steps)
	}
	for step, n := range store.counts {
		if report.Swept[step] != n {
			t.Fatalf("swept[%s]=%d want %d", step, report.Swept[step], n)
		}
	}
	for _, cutoff := range store.cutoffs {
		if !cutoff.Equal(now) || cutoff.Location() != time.UTC {
			t.Fatalf("cutoff=%v want %v in UTC", cutoff, now)
		}
	}
}

// TestSweepInvitationDigestsClassifiesOutcomes pins the fixed result classes:
// a lock yield ends that step only, a finished context stops the run with its
// own error, and any other store failure stops the run as
// ErrInvitationSweepFailed without its text.
func TestSweepInvitationDigestsClassifiesOutcomes(t *testing.T) {
	first := InvitationSweepExpiredDigests
	for _, tc := range []struct {
		name  string
		err   error
		want  error
		ran   int
		swept int
	}{
		{name: "locked step continues", err: ErrInvitationSweepLocked, ran: len(InvitationSweepSteps), swept: 2},
		{name: "timeout stops", err: context.DeadlineExceeded, want: context.DeadlineExceeded, ran: 1, swept: 2},
		{name: "canceled stops", err: context.Canceled, want: context.Canceled, ran: 1, swept: 2},
		{name: "driver failure stops", err: errors.New("pq: secret driver text"), want: ErrInvitationSweepFailed, ran: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scriptedInvitationSweepStore{
				counts: map[InvitationSweepStep]int{first: 2},
				errs:   map[InvitationSweepStep]error{first: tc.err},
			}
			report, err := SweepInvitationDigests(context.Background(), store, time.Now())
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if err != nil && err.Error() == "pq: secret driver text" {
				t.Fatal("driver text escaped")
			}
			if len(store.steps) != tc.ran || report.Swept[first] != tc.swept {
				t.Fatalf("ran=%v report=%+v", store.steps, report)
			}
			if tc.err == ErrInvitationSweepLocked && !slices.Equal(report.Locked, []InvitationSweepStep{first}) {
				t.Fatalf("locked=%v", report.Locked)
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
	store := &scriptedInvitationSweepStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SweepInvitationDigests(ctx, store, time.Now()); !errors.Is(err, context.Canceled) || len(store.steps) != 0 {
		t.Fatalf("ended context err=%v steps=%v", err, store.steps)
	}
}
