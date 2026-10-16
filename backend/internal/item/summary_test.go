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

const summaryMember = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"

func TestSummaryFilterAndPartitionMatrix(t *testing.T) {
	e := newEnv("Pacific/Kiritimati")
	// Local date is March 2 although the injected UTC instant is March 1.
	e.now = fixedNow
	monday := itemtest.Date(2026, time.March, 1)
	today := itemtest.Date(2026, time.March, 2)
	tomorrow := itemtest.Date(2026, time.March, 3)
	rows := []item.Item{
		{ID: "00000000-0000-0000-0000-000000000001", HouseholdID: householdID, SubjectID: subjectID, WorkflowState: item.StateOpen},
		{ID: "00000000-0000-0000-0000-000000000002", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: monday, WorkflowState: item.StateOpen},
		{ID: "00000000-0000-0000-0000-000000000003", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: today, WorkflowState: item.StateOpen},
		{ID: "00000000-0000-0000-0000-000000000004", HouseholdID: householdID, SubjectID: archivedID, AttentionOn: tomorrow, WorkflowState: item.StateOpen},
		{ID: "00000000-0000-0000-0000-000000000005", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: today, WorkflowState: item.StateInProgress},
		{ID: "00000000-0000-0000-0000-000000000006", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: tomorrow, WorkflowState: item.StateInProgress, Done: true},
		{ID: "00000000-0000-0000-0000-000000000007", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: monday, WorkflowState: item.StateWaiting},
		{ID: "00000000-0000-0000-0000-000000000008", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: tomorrow, WorkflowState: item.StatePaused},
		{ID: "00000000-0000-0000-0000-000000000009", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: today, WorkflowState: item.StatePaused, Done: true},
		{ID: "00000000-0000-0000-0000-000000000010", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: tomorrow, WorkflowState: item.StateOpen, Done: true, Archived: true},
		{ID: "00000000-0000-0000-0000-000000000011", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: tomorrow, WorkflowState: item.StateOpen, ResponsibleUserID: ptr(summaryMember), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}},
		{ID: "00000000-0000-0000-0000-000000000012", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: monday, WorkflowState: item.StateOpen, ResponsibleUserID: ptr(userID)},
	}
	for _, row := range rows {
		e.repo.Seed(row)
	}
	got, err := e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := item.Summary{EvaluatedOn: *today, SummaryCounts: item.SummaryCounts{Total: 12, NeedsAttention: 4, InProgress: 1, Waiting: 1, Upcoming: 2, Paused: 1, Done: 2, Archived: 1}}
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	if got.Total != got.NeedsAttention+got.InProgress+got.Waiting+got.Upcoming+got.Paused+got.Done+got.Archived {
		t.Fatalf("partition sum mismatch: %+v", got.SummaryCounts)
	}

	cases := []struct {
		name  string
		q     item.SummaryQuery
		total int64
	}{
		{"subject", item.SummaryQuery{SubjectID: subjectID}, 11},
		{"mixed subject UUID", item.SummaryQuery{SubjectID: "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B70"}, 11},
		{"unknown subject", item.SummaryQuery{SubjectID: missingID}, 0},
		{"responsible UUID", item.SummaryQuery{ResponsibleUserID: summaryMember}, 1},
		{"mixed responsible UUID", item.SummaryQuery{ResponsibleUserID: "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B62"}, 1},
		{"unassigned", item.SummaryQuery{ResponsibleUserID: "unassigned"}, 10},
		{"unknown responsible", item.SummaryQuery{ResponsibleUserID: missingID}, 0},
		{"workflow", item.SummaryQuery{WorkflowState: "paused"}, 2},
		{"attention", item.SummaryQuery{Attention: "upcoming"}, 5},
		{"recurring true", item.SummaryQuery{Recurring: ptr(true)}, 1},
		{"recurring false", item.SummaryQuery{Recurring: ptr(false)}, 11},
		{"AND", item.SummaryQuery{SubjectID: subjectID, WorkflowState: "open", Attention: "needs_attention", Recurring: ptr(false)}, 4},
		{"empty matching none", item.SummaryQuery{SubjectID: missingID, Recurring: ptr(true)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, e2 := e.svc.Summary(context.Background(), userID, householdID, tc.q)
			if e2 != nil || v.Total != tc.total {
				t.Fatalf("summary=%+v err=%v want total=%d", v, e2, tc.total)
			}
		})
	}
	for _, state := range []string{"open", "in_progress", "waiting", "paused"} {
		for _, lifecycle := range []struct {
			name           string
			done, archived bool
		}{{"active", false, false}, {"done", true, false}, {"archived", false, true}, {"archivedDone", true, true}} {
			for _, date := range []struct {
				name string
				d    *schedule.Date
			}{{"null", nil}, {"yesterday", monday}, {"today", today}, {"tomorrow", tomorrow}} {
				t.Run(state+"/"+lifecycle.name+"/"+date.name, func(t *testing.T) {
					caseEnv := newEnv("Pacific/Kiritimati")
					caseEnv.now = fixedNow
					r := item.Item{ID: "matrix", HouseholdID: householdID, WorkflowState: item.WorkflowState(state), Done: lifecycle.done, Archived: lifecycle.archived, AttentionOn: date.d}
					caseEnv.repo.Seed(r)
					got, e2 := caseEnv.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{})
					if e2 != nil {
						t.Fatalf("summary=%+v err=%v", got, e2)
					}
					want := item.SummaryCounts{Total: 1}
					switch {
					case lifecycle.archived:
						want.Archived = 1
					case lifecycle.done:
						want.Done = 1
					case state == "paused":
						want.Paused = 1
					case state == "in_progress":
						want.InProgress = 1
					case state == "waiting":
						want.Waiting = 1
					case date.d == nil || date.d.Compare(*today) <= 0:
						want.NeedsAttention = 1
					default:
						want.Upcoming = 1
					}
					if got.SummaryCounts != want || got.EvaluatedOn != *today {
						t.Fatalf("counts=%+v evaluatedOn=%s want=%+v date=%s", got.SummaryCounts, got.EvaluatedOn, want, today)
					}
				})
			}
		}
	}
}

func TestSummaryGuardAuthorizationAndRepositoryErrors(t *testing.T) {
	e := newEnv("America/New_York")
	for _, guard := range []string{"2026-03-01", "2026-03-02", "2026-02-30", "2026-03-020"} {
		_, err := e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{AttentionDate: guard})
		if guard == "2026-03-01" {
			if err != nil {
				t.Fatalf("good local guard: %v", err)
			}
		} else {
			var q *item.InvalidQueryError
			if !errors.As(err, &q) || q.Parameter != "attentionDate" {
				t.Fatalf("guard %q err=%v", guard, err)
			}
		}
	}
	e.housErr = item.ErrNotFound
	_, err := e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{WorkflowState: "OPEN"})
	if !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("membership must precede semantics, got %v", err)
	}
	e.housErr = nil
	e.repo.Err = errors.New("private database detail")
	_, err = e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{})
	if !errors.Is(err, item.ErrUnavailable) || err.Error() == "private database detail" {
		t.Fatalf("repo error was not sanitized: %v", err)
	}
}
