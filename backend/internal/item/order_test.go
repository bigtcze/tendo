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

type orderedRepo struct {
	item.Repository
	calls int
}

func (r *orderedRepo) FindCreation(context.Context, string, string, string) (item.Item, []byte, error) {
	r.calls++
	return item.Item{}, nil, item.ErrNotFound
}
func (r *orderedRepo) List(context.Context, string, item.ListOptions) ([]item.Item, error) {
	r.calls++
	return nil, nil
}
func (r *orderedRepo) Get(ctx context.Context, h, id string) (item.Item, error) {
	r.calls++
	return r.Repository.Get(ctx, h, id)
}
func (r *orderedRepo) Update(context.Context, string, string, int64, item.Change) (item.Item, error) {
	r.calls++
	return item.Item{}, nil
}

func TestCreateValidationPrecedenceCharacterization(t *testing.T) {
	policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	cases := []struct {
		name        string
		in          item.NewItem
		field, code string
	}{
		{"title before notes", item.NewItem{Title: " ", Notes: ptr("")}, "title", "invalid_length"},
		{"notes before recurrence", item.NewItem{Title: "x", Notes: ptr(""), Recurrence: &schedule.Policy{}}, "notes", "invalid_length"},
		{"recurrence before attention", item.NewItem{Title: "x", Recurrence: &schedule.Policy{}, AttentionOn: ptr("bad")}, "recurrence", "invalid_interval"},
		{"attention before historical parse", item.NewItem{Title: "x", AttentionOn: ptr("bad"), HistoricalCompletedOn: ptr("bad"), Recurrence: policy}, "attentionOn", "invalid_date"},
		{"historical date requires recurrence", item.NewItem{Title: "x", HistoricalCompletedOn: ptr("2026-02-01")}, "historicalCompletedOn", "requires_recurrence"},
		{"historical conflict before future", item.NewItem{Title: "x", HistoricalCompletedOn: ptr("2026-03-03"), AttentionOn: ptr("2026-03-04"), Recurrence: policy}, "historicalCompletedOn", "conflicting_fields"},
		{"historical future before overflow", item.NewItem{Title: "x", HistoricalCompletedOn: ptr("9999-12-31"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "future_date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv("UTC")
			tc.in.SubjectID = subjectID
			_, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), tc.in)
			validation(t, err, tc.field, tc.code)
			if len(e.subjectHit) != 0 {
				t.Fatalf("subject lookup before validation finished: %v", e.subjectHit)
			}
		})
	}
}

func TestCreateReplayPrecedesInvalidTitleAndReferenceOrder(t *testing.T) {
	e := newEnv("UTC")
	key := "precedence"
	if _, err := e.svc.Create(context.Background(), userID, householdID, key, item.NewItem{SubjectID: subjectID, Title: "original"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(context.Background(), userID, householdID, key, item.NewItem{Title: " "}); !errors.Is(err, item.ErrIdempotencyKeyReused) {
		t.Fatalf("replay err=%v", err)
	}
	e.subjectHit = nil
	_, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: "bad", ResponsibleUserID: ptr("bad"), Title: "x"})
	validation(t, err, "subjectId", "invalid_reference")
	if len(e.subjectHit) != 0 {
		t.Fatalf("malformed subject reached lookup: %v", e.subjectHit)
	}
}

func TestListValidationPrecedenceAndNoRepositoryCalls(t *testing.T) {
	base := itemtest.New()
	repo := &orderedRepo{Repository: base}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	svc := item.NewService(repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { return false, nil }, func(context.Context, string, string) error { return nil }, func() time.Time { return now })
	if _, err := svc.Create(context.Background(), userID, householdID, "list-seed", item.NewItem{SubjectID: subjectID, Title: "seed"}); err != nil {
		t.Fatal(err)
	}
	repo.calls = 0
	cases := []struct {
		name      string
		q         item.ListQuery
		parameter string
	}{
		{"limit", item.ListQuery{Limit: 101, SubjectID: "bad", Q: "\x00", Sort: "bad", AttentionDate: "bad", Cursor: "bad"}, "limit"},
		{"filters", item.ListQuery{SubjectID: "bad", Q: "\x00", Sort: "bad", AttentionDate: "bad", Cursor: "bad"}, "subjectId"},
		{"search", item.ListQuery{Q: "\x00", Sort: "bad", AttentionDate: "bad", Cursor: "bad"}, "q"},
		{"sort", item.ListQuery{Sort: "bad", AttentionDate: "bad", Cursor: "bad"}, "sort"},
		{"attention date", item.ListQuery{AttentionDate: "bad", Cursor: "bad"}, "attentionDate"},
		{"cursor", item.ListQuery{Cursor: "bad"}, "cursor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.List(context.Background(), userID, householdID, tc.q)
			var got *item.InvalidQueryError
			if !errors.As(err, &got) || got.Parameter != tc.parameter {
				t.Fatalf("err=%#v parameter=%q want=%q", err, got, tc.parameter)
			}
			if repo.calls != 0 {
				t.Fatalf("repository calls=%d", repo.calls)
			}
		})
	}
}

func TestUpdateValidationOrderCharacterization(t *testing.T) {
	e := newEnv("UTC")
	existing := e.create(t, item.NewItem{Title: "existing"})
	_, precedenceErr := e.svc.Update(context.Background(), userID, householdID, existing.ID, existing.Version, item.Patch{Title: ptr(" "), Notes: item.Some(""), AttentionOn: item.Some("bad"), Recurrence: item.Some(schedule.Policy{}), WorkflowState: ptr("bad")})
	validation(t, precedenceErr, "title", "invalid_length")
	base := itemtest.New()
	repo := &orderedRepo{Repository: base}
	memberCalls, subjectCalls := 0, 0
	svc := item.NewService(repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { subjectCalls++; return false, nil }, func(context.Context, string, string) error { memberCalls++; return nil }, func() time.Time { return time.Now() })
	base.Seed(item.Item{ID: missingID, HouseholdID: householdID, SubjectID: subjectID, Title: "seed", WorkflowState: item.StateOpen, Version: 1})
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 0, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	cases := []struct {
		name        string
		patch       item.Patch
		field, code string
	}{
		{"title", item.Patch{Title: ptr(" "), Notes: item.Some(""), AttentionOn: item.Some("bad"), Recurrence: item.Some(policy), WorkflowState: ptr("bad"), SubjectID: ptr("bad"), ResponsibleUserID: item.Some("bad")}, "title", "invalid_length"},
		{"notes", item.Patch{Notes: item.Some(""), AttentionOn: item.Some("bad"), Recurrence: item.Some(policy), WorkflowState: ptr("bad")}, "notes", "invalid_length"},
		{"attention", item.Patch{AttentionOn: item.Some("bad"), Recurrence: item.Some(policy), WorkflowState: ptr("bad")}, "attentionOn", "invalid_date"},
		{"recurrence", item.Patch{Recurrence: item.Some(policy), WorkflowState: ptr("bad")}, "recurrence", "invalid_interval"},
		{"workflow", item.Patch{WorkflowState: ptr("bad"), SubjectID: ptr("bad")}, "workflowState", "invalid_workflow_state"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo.calls, subjectCalls, memberCalls = 0, 0, 0
			_, err := svc.Update(context.Background(), userID, householdID, missingID, 1, tc.patch)
			validation(t, err, tc.field, tc.code)
			if repo.calls != 1 || subjectCalls != 0 || memberCalls != 0 {
				t.Fatalf("calls repo=%d subject=%d member=%d", repo.calls, subjectCalls, memberCalls)
			}
		})
	}
	t.Run("malformed item ID precedes household lookup", func(t *testing.T) {
		householdCalls := 0
		probe := item.NewService(repo, func(context.Context, string, string) (string, error) {
			householdCalls++
			return "", item.ErrUnavailable
		}, nil, nil, func() time.Time { return time.Now() })
		_, err := probe.Update(context.Background(), userID, householdID, "bad", 1, item.Patch{})
		if err != item.ErrNotFound || householdCalls != 0 {
			t.Fatalf("err=%v household calls=%d", err, householdCalls)
		}
	})
	t.Run("household authorization precedes empty patch", func(t *testing.T) {
		householdCalls := 0
		probe := item.NewService(repo, func(context.Context, string, string) (string, error) {
			householdCalls++
			return "", item.ErrNotFound
		}, nil, nil, func() time.Time { return time.Now() })
		_, err := probe.Update(context.Background(), userID, householdID, missingID, 1, item.Patch{})
		if err != item.ErrNotFound || householdCalls != 1 {
			t.Fatalf("err=%v household calls=%d", err, householdCalls)
		}
	})
	t.Run("empty patch after authorization does not read repository", func(t *testing.T) {
		repo.calls = 0
		_, err := svc.Update(context.Background(), userID, householdID, missingID, 1, item.Patch{})
		if err != item.ErrEmptyPatch || repo.calls != 0 {
			t.Fatalf("err=%v repository calls=%d", err, repo.calls)
		}
	})
}

func TestCreateSubjectBeforeResponsibleMember(t *testing.T) {
	for _, subjectResult := range []struct {
		name, subjectID     string
		subjectErr          error
		wantField, wantCode string
	}{
		{"unknown subject stops before member", foreignID, nil, "subjectId", "invalid_reference"},
		{"member error follows valid subject", subjectID, nil, "responsibleUserId", "invalid_reference"},
	} {
		t.Run(subjectResult.name, func(t *testing.T) {
			repo := itemtest.New()
			memberCalls, subjectCalls := 0, 0
			svc := item.NewService(repo,
				func(context.Context, string, string) (string, error) { return "UTC", nil },
				func(context.Context, string, string, string) (bool, error) {
					subjectCalls++
					if subjectResult.subjectID == foreignID {
						return false, item.ErrNotFound
					}
					return false, subjectResult.subjectErr
				},
				func(context.Context, string, string) error { memberCalls++; return item.ErrNotFound },
				func() time.Time { return time.Now() },
			)
			_, err := svc.Create(context.Background(), userID, householdID, "ordering", item.NewItem{SubjectID: subjectResult.subjectID, ResponsibleUserID: ptr(userID), Title: "valid"})
			validation(t, err, subjectResult.wantField, subjectResult.wantCode)
			if subjectResult.wantField == "subjectId" && memberCalls != 0 {
				t.Fatalf("member lookup occurred: %d", memberCalls)
			}
			if subjectResult.wantField == "responsibleUserId" && (subjectCalls != 1 || memberCalls != 1) {
				t.Fatalf("lookups subject=%d member=%d", subjectCalls, memberCalls)
			}
			if repo.Calls != 1 { // Initial FindCreation idempotency lookup; no write is reached.
				t.Fatalf("repository calls=%d, want only the initial replay lookup", repo.Calls)
			}
		})
	}
}

func TestUpdateSubjectBeforeResponsibleMemberAndTargetExistence(t *testing.T) {
	for _, targetExists := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing", false: "missing"}[targetExists], func(t *testing.T) {
			repo := itemtest.New()
			targetID := missingID
			if targetExists {
				repo.Seed(item.Item{ID: targetID, HouseholdID: householdID, SubjectID: subjectID, Title: "seed", WorkflowState: item.StateOpen, Version: 1})
			}
			memberCalls := 0
			svc := item.NewService(repo,
				func(context.Context, string, string) (string, error) { return "UTC", nil },
				func(context.Context, string, string, string) (bool, error) { return false, item.ErrNotFound },
				func(context.Context, string, string) error { memberCalls++; return item.ErrNotFound },
				func() time.Time { return time.Now() },
			)
			_, err := svc.Update(context.Background(), userID, householdID, targetID, 1, item.Patch{SubjectID: ptr(foreignID), ResponsibleUserID: item.Some(userID)})
			if targetExists {
				validation(t, err, "subjectId", "invalid_reference")
			} else if err != item.ErrNotFound {
				t.Fatalf("missing target err=%v", err)
			}
			if memberCalls != 0 {
				t.Fatalf("member lookup occurred before subject error: %d", memberCalls)
			}
		})
	}
	for _, targetExists := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid subject then member error", false: "valid subject but missing target"}[targetExists], func(t *testing.T) {
			repo := itemtest.New()
			targetID := missingID
			if targetExists {
				repo.Seed(item.Item{ID: targetID, HouseholdID: householdID, SubjectID: subjectID, Title: "seed", WorkflowState: item.StateOpen, Version: 1})
			}
			subjectCalls, memberCalls := 0, 0
			svc := item.NewService(repo,
				func(context.Context, string, string) (string, error) { return "UTC", nil },
				func(context.Context, string, string, string) (bool, error) { subjectCalls++; return false, nil },
				func(context.Context, string, string) error { memberCalls++; return item.ErrNotFound },
				func() time.Time { return time.Now() },
			)
			_, err := svc.Update(context.Background(), userID, householdID, targetID, 1, item.Patch{SubjectID: ptr(subjectID), ResponsibleUserID: item.Some(userID)})
			if targetExists {
				validation(t, err, "responsibleUserId", "invalid_reference")
			} else if err != item.ErrNotFound {
				t.Fatalf("missing target err=%v", err)
			}
			if subjectCalls != 1 || memberCalls != 1 {
				t.Fatalf("lookups subject=%d member=%d", subjectCalls, memberCalls)
			}
		})
	}
}
