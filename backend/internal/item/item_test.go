package item_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/itemtest"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

const (
	userID      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	householdID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	subjectID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	archivedID  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	foreignID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
	missingID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
)

// fixed instant: 2026-03-01 23:30 UTC is already 2026-03-02 in Prague and still
// 2026-03-01 in New York.
var fixedNow = time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)

type env struct {
	repo       *itemtest.Repo
	svc        *item.Service
	zone       string
	now        time.Time
	housErr    error
	subjectErr error
	subjectHit []string
}

func newEnv(zone string) *env {
	e := &env{repo: itemtest.New(), zone: zone, now: fixedNow}
	houses := func(_ context.Context, uid, hid string) (string, error) {
		if e.housErr != nil {
			return "", e.housErr
		}
		if uid != userID || hid != householdID {
			return "", item.ErrNotFound
		}
		return e.zone, nil
	}
	subjects := func(_ context.Context, uid, hid, sid string) (bool, error) {
		e.subjectHit = append(e.subjectHit, sid)
		if e.subjectErr != nil {
			return false, e.subjectErr
		}
		if uid != userID || hid != householdID {
			return false, item.ErrNotFound
		}
		switch sid {
		case subjectID:
			return false, nil
		case archivedID:
			return true, nil
		}
		return false, item.ErrNotFound // foreignID and unknown ids
	}
	e.svc = item.NewService(e.repo, houses, subjects, func() time.Time { return e.now })
	return e
}

func ptr[T any](v T) *T { return &v }

func validation(t *testing.T, err error, field, code string) {
	t.Helper()
	var v *item.ValidationError
	if !errors.As(err, &v) || v.Field != field || v.Code != code {
		t.Fatalf("err=%v want %s/%s", err, field, code)
	}
}

func (e *env) create(t *testing.T, n item.NewItem) item.Item {
	t.Helper()
	if n.SubjectID == "" {
		n.SubjectID = subjectID
	}
	created, err := e.svc.Create(context.Background(), userID, householdID, n)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return created
}

func TestValidateTitle(t *testing.T) {
	for _, tc := range []struct{ name, in, want, code string }{
		{"ascii", "Pay rent", "Pay rent", ""},
		{"trimmed", "  Pay rent \t", "Pay rent", ""},
		{"200 runes", strings.Repeat("家", 200), strings.Repeat("家", 200), ""},
		{"201 runes", strings.Repeat("家", 201), "", "invalid_length"},
		{"padded 200", " " + strings.Repeat("a", 200) + " ", strings.Repeat("a", 200), ""},
		{"empty", "", "", "invalid_length"},
		{"blank", " \n ", "", "invalid_length"},
		{"nul", "a\x00b", "", "invalid_characters"},
		{"newline", "a\nb", "", "invalid_characters"},
		{"tab inside", "a\tb", "", "invalid_characters"},
		{"zero width", "a\u200bb", "", "invalid_characters"},
		{"bidi override", "a\u202eb", "", "invalid_characters"},
		{"line separator", "a\u2028b", "", "invalid_characters"},
		{"paragraph separator", "a\u2029b", "", "invalid_characters"},
		{"invalid utf8", "a\xffb", "", "invalid_characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := item.ValidateTitle(tc.in)
			if tc.code == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got=%q err=%v", got, err)
				}
				return
			}
			validation(t, err, "title", tc.code)
		})
	}
}

func TestValidateNotes(t *testing.T) {
	for _, tc := range []struct{ name, in, code string }{
		{"plain", "call the broker", ""},
		{"newlines tabs", "line1\nline2\r\n\tindented", ""},
		{"untrimmed kept", "  spaced  ", ""},
		{"whitespace only is allowed", " ", ""},
		{"4000 runes", strings.Repeat("家", 4000), ""},
		{"4001 runes", strings.Repeat("家", 4001), "invalid_length"},
		{"empty", "", "invalid_length"},
		{"nul", "a\x00b", "invalid_characters"},
		{"escape", "a\x1bb", "invalid_characters"},
		{"vertical tab", "a\vb", "invalid_characters"},
		{"form feed", "a\fb", "invalid_characters"},
		{"zero width", "a\u200bb", "invalid_characters"},
		{"bom", "\ufeffa", "invalid_characters"},
		{"line separator", "a\u2028b", "invalid_characters"},
		{"paragraph separator", "a\u2029b", "invalid_characters"},
		{"invalid utf8", "a\xffb", "invalid_characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := item.ValidateNotes(tc.in)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
				return
			}
			validation(t, err, "notes", tc.code)
		})
	}
}

func TestParseDate(t *testing.T) {
	for _, in := range []string{"2026-03-02", "2024-02-29", "0001-01-01", "9999-12-31"} {
		d, err := item.ParseDate(in)
		if err != nil || d.String() != in {
			t.Fatalf("%s: %v %v", in, d, err)
		}
	}
	for _, in := range []string{"", "2026-02-30", "2026-02-29", "2026-13-01", "2026-00-10", "2026-01-00", "2026-1-01", "2026-01-1", "26-01-01", "2026/01/01", "20260101", "2026-01-01T00:00:00Z", " 2026-01-01", "2026-01-01 ", "+026-01-01", "2026-0a-01", "0000-01-01", "２０２６-01-01"} {
		_, err := item.ParseDate(in)
		validation(t, err, "attentionOn", "invalid_date")
	}
}

func TestCreateStoresValidatedStateAndStartsOpen(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "  File taxes  ", Notes: ptr("  keep\nspacing "), AttentionOn: ptr("2026-04-15")})
	if created.Title != "File taxes" || created.Notes == nil || *created.Notes != "  keep\nspacing " || created.AttentionOn == nil || created.AttentionOn.String() != "2026-04-15" || created.WorkflowState != item.StateOpen || created.Version != 1 || created.Archived || created.SubjectID != subjectID || created.HouseholdID != householdID {
		t.Fatalf("created=%+v", created)
	}
	stored, _ := e.repo.Row(created.ID)
	if stored.Title != "File taxes" || stored.WorkflowState != item.StateOpen || stored.Version != 1 {
		t.Fatalf("stored=%+v", stored)
	}
	// Uppercase subject ids are canonicalized before they reach the repository.
	upper := e.create(t, item.NewItem{Title: "x", SubjectID: strings.ToUpper(subjectID)})
	if upper.SubjectID != subjectID {
		t.Fatalf("subject id not canonical: %s", upper.SubjectID)
	}
	bare := e.create(t, item.NewItem{Title: "bare"})
	if bare.Notes != nil || bare.AttentionOn != nil {
		t.Fatalf("absent optional fields must stay null: %+v", bare)
	}
}

func TestRecurrenceCreatePatchAndAttentionIndependence(t *testing.T) {
	e := newEnv("UTC")
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	created := e.create(t, item.NewItem{Title: "repeat", AttentionOn: ptr("2099-01-01"), Recurrence: &policy})
	if created.Recurrence == nil || created.Recurrence.Mode != schedule.ModeFixed || created.AttentionOn == nil || created.AttentionOn.String() != "2099-01-01" || created.Attention != schedule.Upcoming {
		t.Fatalf("create: %+v", created)
	}
	attention := created.Attention
	if attention != schedule.Upcoming {
		t.Fatalf("attention=%q", attention)
	}
	changed := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}
	updated, err := e.svc.Update(context.Background(), userID, householdID, created.ID, created.Version, item.Patch{Recurrence: item.Some(changed)})
	if err != nil || updated.Version != 2 || updated.AttentionOn == nil || updated.AttentionOn.String() != "2099-01-01" || updated.Attention != attention || updated.Recurrence == nil || *updated.Recurrence != changed {
		t.Fatalf("replace: %+v %v", updated, err)
	}
	cleared, err := e.svc.Update(context.Background(), userID, householdID, created.ID, updated.Version, item.Patch{Recurrence: item.Null[schedule.Policy]()})
	if err != nil || cleared.Version != 3 || cleared.Recurrence != nil || cleared.AttentionOn == nil || cleared.AttentionOn.String() != "2099-01-01" || cleared.Attention != attention {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	omitted, err := e.svc.Update(context.Background(), userID, householdID, created.ID, cleared.Version, item.Patch{Title: ptr("still one-off")})
	if err != nil || omitted.Version != 4 || omitted.Recurrence != nil || omitted.AttentionOn == nil || omitted.AttentionOn.String() != "2099-01-01" || omitted.Attention != attention {
		t.Fatalf("omit: %+v %v", omitted, err)
	}
	policyAgain := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	enabled, err := e.svc.Update(context.Background(), userID, householdID, created.ID, omitted.Version, item.Patch{Recurrence: item.Some(policyAgain)})
	if err != nil || enabled.Version != 5 || enabled.Recurrence == nil {
		t.Fatalf("reenable: %+v %v", enabled, err)
	}
	titled, err := e.svc.Update(context.Background(), userID, householdID, created.ID, enabled.Version, item.Patch{Title: ptr("renamed")})
	if err != nil || titled.Version != 6 || titled.Recurrence == nil || *titled.Recurrence != policyAgain || titled.AttentionOn == nil || titled.AttentionOn.String() != "2099-01-01" || titled.Attention != attention {
		t.Fatalf("title update changed recurrence or cycle: %+v %v", titled, err)
	}
	fluid, err := e.svc.Update(context.Background(), userID, householdID, created.ID, titled.Version, item.Patch{Recurrence: item.Some(changed)})
	if err != nil || fluid.Version != 7 || fluid.Recurrence == nil || *fluid.Recurrence != changed || fluid.AttentionOn == nil || fluid.AttentionOn.String() != "2099-01-01" || fluid.Attention != attention {
		t.Fatalf("fluid switch: %+v %v", fluid, err)
	}
	off, err := e.svc.Update(context.Background(), userID, householdID, created.ID, fluid.Version, item.Patch{Recurrence: item.Null[schedule.Policy]()})
	if err != nil || off.Version != 8 || off.Recurrence != nil || off.AttentionOn == nil || off.AttentionOn.String() != "2099-01-01" || off.Attention != attention {
		t.Fatalf("off: %+v %v", off, err)
	}
	falsePolicy := schedule.Policy{Enabled: false, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	canonical, err := e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "disabled maps to off", Recurrence: &falsePolicy})
	if err != nil || canonical.Recurrence != nil {
		t.Fatalf("disabled policy not canonicalized: %+v %v", canonical, err)
	}
	canonical, err = e.svc.Update(context.Background(), userID, householdID, created.ID, off.Version, item.Patch{Recurrence: item.Some(falsePolicy)})
	if err != nil || canonical.Recurrence != nil || canonical.Version != 9 {
		t.Fatalf("disabled patch not canonicalized: %+v %v", canonical, err)
	}
	if _, err := e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "one off"}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRecurrenceDoesNotPersist(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy schedule.Policy
		code   string
	}{
		{"zero", schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 0, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}, "invalid_interval"},
		{"high", schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1000, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}, "invalid_interval"},
		{"unit", schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: "fortnight"}, Mode: schedule.ModeFixed}, "invalid_interval_unit"},
		{"mode", schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: "mystery"}, "invalid_mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv("UTC")
			_, err := e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "invalid", Recurrence: &tc.policy})
			validation(t, err, "recurrence", tc.code)
			if e.repo.Count() != 0 {
				t.Fatal("invalid policy persisted")
			}
		})
	}
}

func TestCreateRejectsInvalidInputWithoutPersisting(t *testing.T) {
	e := newEnv("UTC")
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		in          item.NewItem
		field, code string
	}{
		{"blank title", item.NewItem{SubjectID: subjectID, Title: " "}, "title", "invalid_length"},
		{"title control", item.NewItem{SubjectID: subjectID, Title: "a\x00"}, "title", "invalid_characters"},
		{"empty notes", item.NewItem{SubjectID: subjectID, Title: "x", Notes: ptr("")}, "notes", "invalid_length"},
		{"notes control", item.NewItem{SubjectID: subjectID, Title: "x", Notes: ptr("a\x07")}, "notes", "invalid_characters"},
		{"bad date", item.NewItem{SubjectID: subjectID, Title: "x", AttentionOn: ptr("2026-02-30")}, "attentionOn", "invalid_date"},
		{"empty date", item.NewItem{SubjectID: subjectID, Title: "x", AttentionOn: ptr("")}, "attentionOn", "invalid_date"},
		{"malformed subject", item.NewItem{SubjectID: "nope", Title: "x"}, "subjectId", "invalid_reference"},
		{"empty subject", item.NewItem{SubjectID: "", Title: "x"}, "subjectId", "invalid_reference"},
		{"unknown subject", item.NewItem{SubjectID: missingID, Title: "x"}, "subjectId", "invalid_reference"},
		{"foreign subject", item.NewItem{SubjectID: foreignID, Title: "x"}, "subjectId", "invalid_reference"},
		{"archived subject", item.NewItem{SubjectID: archivedID, Title: "x"}, "subjectId", "invalid_reference"},
	} {
		_, err := e.svc.Create(ctx, userID, householdID, tc.in)
		validation(t, err, tc.field, tc.code)
	}
	if e.repo.Count() != 0 || e.repo.Calls != 0 {
		t.Fatalf("invalid input reached storage: rows=%d calls=%d", e.repo.Count(), e.repo.Calls)
	}
}

func TestValidationOrderIsFieldsBeforeSubjectLookup(t *testing.T) {
	e := newEnv("UTC")
	_, err := e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: archivedID, Title: " "})
	validation(t, err, "title", "invalid_length")
	if len(e.subjectHit) != 0 {
		t.Fatalf("subject looked up despite invalid fields: %v", e.subjectHit)
	}
	_, err = e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: "nope", Title: "ok"})
	validation(t, err, "subjectId", "invalid_reference")
	if len(e.subjectHit) != 0 {
		t.Fatalf("malformed subject id must not reach the adapter: %v", e.subjectHit)
	}
}

func TestAttentionDerivationUsesHouseholdTimezoneBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		attention  *string
		want       schedule.Attention
	}{
		{"prague is already march 2", "Europe/Prague", ptr("2026-03-02"), schedule.NeedsAttention},
		{"new york is still march 1", "America/New_York", ptr("2026-03-02"), schedule.Upcoming},
		{"utc is still march 1", "UTC", ptr("2026-03-02"), schedule.Upcoming},
		{"prague today", "Europe/Prague", ptr("2026-03-03"), schedule.Upcoming},
		{"new york today is due", "America/New_York", ptr("2026-03-01"), schedule.NeedsAttention},
		{"past date", "America/New_York", ptr("2020-01-01"), schedule.NeedsAttention},
		{"null date", "America/New_York", nil, schedule.NeedsAttention},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(tc.zone)
			created := e.create(t, item.NewItem{Title: "x", AttentionOn: tc.attention})
			if created.Attention != tc.want {
				t.Fatalf("create attention=%s want %s", created.Attention, tc.want)
			}
			got, err := e.svc.Get(context.Background(), userID, householdID, created.ID)
			if err != nil || got.Attention != tc.want {
				t.Fatalf("get attention=%s err=%v", got.Attention, err)
			}
			page, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{})
			if err != nil || len(page.Items) != 1 || page.Items[0].Attention != tc.want {
				t.Fatalf("list=%+v err=%v", page, err)
			}
			stored, _ := e.repo.Row(created.ID)
			if stored.Attention != "" {
				t.Fatalf("derived attention must not be persisted: %q", stored.Attention)
			}
		})
	}
}

func TestPatchChangesAttentionDerivation(t *testing.T) {
	e := newEnv("America/New_York")
	created := e.create(t, item.NewItem{Title: "x", AttentionOn: ptr("2026-03-05")})
	if created.Attention != schedule.Upcoming {
		t.Fatal(created.Attention)
	}
	updated, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 1, item.Patch{AttentionOn: item.Some("2026-03-01")})
	if err != nil || updated.Attention != schedule.NeedsAttention || updated.AttentionOn.String() != "2026-03-01" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	back, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 2, item.Patch{AttentionOn: item.Some("2026-03-02")})
	if err != nil || back.Attention != schedule.Upcoming {
		t.Fatalf("back=%+v err=%v", back, err)
	}
}

func TestPatchNullClearVersusOmitted(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x", Notes: ptr("keep me"), AttentionOn: ptr("2030-01-01")})
	ctx := context.Background()
	// Omitted notes/attentionOn stay untouched.
	got, err := e.svc.Update(ctx, userID, householdID, created.ID, 1, item.Patch{Title: ptr("renamed")})
	if err != nil || got.Notes == nil || *got.Notes != "keep me" || got.AttentionOn == nil || got.AttentionOn.String() != "2030-01-01" || got.Title != "renamed" || got.Attention != schedule.Upcoming {
		t.Fatalf("omitted fields changed: %+v err=%v", got, err)
	}
	// Null clears only the named field.
	got, err = e.svc.Update(ctx, userID, householdID, created.ID, 2, item.Patch{Notes: item.Null[string]()})
	if err != nil || got.Notes != nil || got.AttentionOn == nil {
		t.Fatalf("notes null: %+v err=%v", got, err)
	}
	got, err = e.svc.Update(ctx, userID, householdID, created.ID, 3, item.Patch{AttentionOn: item.Null[string]()})
	if err != nil || got.AttentionOn != nil || got.Attention != schedule.NeedsAttention || got.Title != "renamed" {
		t.Fatalf("attention null: %+v err=%v", got, err)
	}
	stored, _ := e.repo.Row(created.ID)
	if stored.Notes != nil || stored.AttentionOn != nil || stored.Version != 4 {
		t.Fatalf("stored=%+v", stored)
	}
	// Setting a value after clearing works.
	got, err = e.svc.Update(ctx, userID, householdID, created.ID, 4, item.Patch{Notes: item.Some("again")})
	if err != nil || got.Notes == nil || *got.Notes != "again" {
		t.Fatalf("set after clear: %+v err=%v", got, err)
	}
}

func TestPatchValidationAndEmpty(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	original, _ := e.repo.Row(created.ID)
	ctx := context.Background()
	var v *item.ValidationError
	if _, err := e.svc.Update(ctx, userID, householdID, created.ID, 1, item.Patch{}); !errors.Is(err, item.ErrEmptyPatch) || errors.As(err, &v) {
		t.Fatalf("empty patch must be a plain error: %v", err)
	}
	for _, tc := range []struct {
		name        string
		p           item.Patch
		field, code string
	}{
		{"title", item.Patch{Title: ptr(" ")}, "title", "invalid_length"},
		{"notes empty", item.Patch{Notes: item.Some("")}, "notes", "invalid_length"},
		{"notes control", item.Patch{Notes: item.Some("\x00")}, "notes", "invalid_characters"},
		{"date", item.Patch{AttentionOn: item.Some("2026-02-30")}, "attentionOn", "invalid_date"},
		{"state", item.Patch{WorkflowState: ptr("done")}, "workflowState", "invalid_workflow_state"},
		{"state case", item.Patch{WorkflowState: ptr("Open")}, "workflowState", "invalid_workflow_state"},
		{"subject malformed", item.Patch{SubjectID: ptr("nope")}, "subjectId", "invalid_reference"},
		{"subject archived", item.Patch{SubjectID: ptr(archivedID)}, "subjectId", "invalid_reference"},
		{"subject foreign", item.Patch{SubjectID: ptr(foreignID)}, "subjectId", "invalid_reference"},
	} {
		_, err := e.svc.Update(ctx, userID, householdID, created.ID, 1, tc.p)
		validation(t, err, tc.field, tc.code)
	}
	if got, _ := e.repo.Row(created.ID); got.Version != original.Version || got.Title != original.Title {
		t.Fatalf("invalid patch changed state: %+v", got)
	}
}

func TestSubjectCheckedOnlyWhenPatched(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	hits := len(e.subjectHit)
	// Subject becomes archived later; unrelated edits must still succeed.
	if _, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 1, item.Patch{Title: ptr("y")}); err != nil {
		t.Fatal(err)
	}
	if len(e.subjectHit) != hits {
		t.Fatalf("subject looked up for unrelated patch: %v", e.subjectHit)
	}
	row, _ := e.repo.Row(created.ID)
	row.SubjectID = archivedID
	e.repo.Seed(row)
	if got, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 2, item.Patch{Archived: ptr(true)}); err != nil || !got.Archived || got.SubjectID != archivedID {
		t.Fatalf("archive item with archived subject: %+v err=%v", got, err)
	}
	moved, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 3, item.Patch{SubjectID: ptr(strings.ToUpper(subjectID))})
	if err != nil || moved.SubjectID != subjectID || len(e.subjectHit) != hits+1 {
		t.Fatalf("moved=%+v err=%v hits=%v", moved, err, e.subjectHit)
	}
}

func TestPatchWorkflowStateArchiveAndVersioning(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	ctx := context.Background()
	for i, state := range []string{"in_progress", "waiting", "paused", "open"} {
		got, err := e.svc.Update(ctx, userID, householdID, created.ID, int64(i+1), item.Patch{WorkflowState: ptr(state)})
		if err != nil || string(got.WorkflowState) != state || got.Version != int64(i+2) {
			t.Fatalf("%s: %+v err=%v", state, got, err)
		}
	}
	got, err := e.svc.Update(ctx, userID, householdID, created.ID, 5, item.Patch{Archived: ptr(true), Title: ptr("  final  ")})
	if err != nil || !got.Archived || got.Title != "final" || got.Version != 6 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// Stale and zero versions.
	before, _ := e.repo.Row(created.ID)
	for _, v := range []int64{1, 5, 0, 7} {
		if _, err := e.svc.Update(ctx, userID, householdID, created.ID, v, item.Patch{Title: ptr("stale")}); !errors.Is(err, item.ErrVersionMismatch) {
			t.Fatalf("version %d: %v", v, err)
		}
	}
	if after, _ := e.repo.Row(created.ID); after != before {
		t.Fatalf("stale update changed state: %+v", after)
	}
	if _, err := e.svc.Update(ctx, userID, householdID, missingID, 0, item.Patch{Title: ptr("x")}); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestMalformedIdentifiersAndNonMembersAreNotFound(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	calls := e.repo.Calls
	ctx := context.Background()
	patch := item.Patch{Archived: ptr(true)}
	for _, bad := range []string{"", "nope", "0198a2f07c1e7a539b0e5d3f2c1a4b70", "{0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70}"} {
		if _, err := e.svc.Create(ctx, userID, bad, item.NewItem{SubjectID: subjectID, Title: "x"}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("create %q: %v", bad, err)
		}
		if _, err := e.svc.List(ctx, userID, bad, item.ListQuery{}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("list %q: %v", bad, err)
		}
		if _, err := e.svc.Get(ctx, userID, bad, created.ID); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("get household %q: %v", bad, err)
		}
		if _, err := e.svc.Get(ctx, userID, householdID, bad); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("get item %q: %v", bad, err)
		}
		if _, err := e.svc.Update(ctx, userID, householdID, bad, 1, patch); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("update item %q: %v", bad, err)
		}
	}
	other := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	if _, err := e.svc.Get(ctx, userID, other, created.ID); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("non-member get: %v", err)
	}
	if _, err := e.svc.Update(ctx, userID, other, created.ID, 1, patch); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("non-member update: %v", err)
	}
	// Non-member validation does not leak: membership precedes validation.
	if _, err := e.svc.Create(ctx, userID, other, item.NewItem{SubjectID: "nope", Title: " "}); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("non-member create: %v", err)
	}
	if e.repo.Calls != calls {
		t.Fatalf("repository touched: %d -> %d", calls, e.repo.Calls)
	}
	if row, _ := e.repo.Row(created.ID); row.Version != 1 || row.Archived {
		t.Fatalf("state changed: %+v", row)
	}
}

func TestAdapterInfrastructureFailureIsUnavailableNotNotFound(t *testing.T) {
	secret := errors.New("pq: password authentication failed")
	ctx := context.Background()
	t.Run("households", func(t *testing.T) {
		e := newEnv("UTC")
		created := e.create(t, item.NewItem{Title: "x"})
		calls := e.repo.Calls
		e.housErr = secret
		check := func(name string, err error) {
			t.Helper()
			if !errors.Is(err, item.ErrUnavailable) || errors.Is(err, item.ErrNotFound) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "pq") {
				t.Fatalf("%s: %v", name, err)
			}
		}
		_, err := e.svc.Create(ctx, userID, householdID, item.NewItem{SubjectID: subjectID, Title: "x"})
		check("create", err)
		_, err = e.svc.Get(ctx, userID, householdID, created.ID)
		check("get", err)
		_, err = e.svc.List(ctx, userID, householdID, item.ListQuery{})
		check("list", err)
		_, err = e.svc.Update(ctx, userID, householdID, created.ID, 1, item.Patch{Archived: ptr(true)})
		check("update", err)
		if e.repo.Calls != calls {
			t.Fatal("repository touched")
		}
	})
	t.Run("invalid timezone", func(t *testing.T) {
		e := newEnv("Not/AZone")
		_, err := e.svc.Create(ctx, userID, householdID, item.NewItem{SubjectID: subjectID, Title: "x"})
		if !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("subjects", func(t *testing.T) {
		e := newEnv("UTC")
		created := e.create(t, item.NewItem{Title: "x"})
		e.subjectErr = secret
		_, err := e.svc.Create(ctx, userID, householdID, item.NewItem{SubjectID: subjectID, Title: "x"})
		if !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("create: %v", err)
		}
		var v *item.ValidationError
		_, err = e.svc.Update(ctx, userID, householdID, created.ID, 1, item.Patch{SubjectID: ptr(subjectID)})
		if !errors.Is(err, item.ErrUnavailable) || errors.As(err, &v) {
			t.Fatalf("update: %v", err)
		}
		if e.repo.Count() != 1 {
			t.Fatal("create persisted despite adapter failure")
		}
	})
	t.Run("repository", func(t *testing.T) {
		e := newEnv("UTC")
		e.repo.Err = secret
		if _, err := e.svc.Create(ctx, userID, householdID, item.NewItem{SubjectID: subjectID, Title: "x"}); !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("create: %v", err)
		}
		if _, err := e.svc.Get(ctx, userID, householdID, subjectID); !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("get: %v", err)
		}
		if _, err := e.svc.List(ctx, userID, householdID, item.ListQuery{}); !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("list: %v", err)
		}
		if _, err := e.svc.Update(ctx, userID, householdID, subjectID, 1, item.Patch{Archived: ptr(true)}); !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("update: %v", err)
		}
	})
}

func TestRepositoryInvalidReferenceBecomesSubjectValidationError(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	e.repo.Err = item.ErrInvalidReference
	_, err := e.svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "x"})
	validation(t, err, "subjectId", "invalid_reference")
	_, err = e.svc.Update(context.Background(), userID, householdID, created.ID, 1, item.Patch{SubjectID: ptr(subjectID)})
	validation(t, err, "subjectId", "invalid_reference")
}

func TestCursorRoundTripAndRejection(t *testing.T) {
	id := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B70"
	cursor := item.EncodeCursor(id)
	if strings.ContainsAny(cursor, "=+/") || cursor == id {
		t.Fatalf("cursor not opaque base64url: %s", cursor)
	}
	got, err := item.DecodeCursor(cursor)
	if err != nil || got != strings.ToLower(id) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	b64 := func(s string) string { return strings.TrimRight(base64URL(s), "=") }
	for name, bad := range map[string]string{
		"empty": "", "not base64": "***", "padded": cursor + "=", "raw uuid": id,
		"subject prefix": b64("s1:" + id), "wrong version": b64("i2:" + id), "no prefix": b64(id),
		"bad uuid": b64("i1:not-a-uuid"), "trailing": b64("i1:" + id + "x"),
	} {
		if _, err := item.DecodeCursor(bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestListLimitsCursorErrorsAndPagination(t *testing.T) {
	e := newEnv("UTC")
	ctx := context.Background()
	for _, limit := range []int{-1, 101} {
		var q *item.InvalidQueryError
		if _, err := e.svc.List(ctx, userID, householdID, item.ListQuery{Limit: limit}); !errors.As(err, &q) || q.Parameter != "limit" {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	var q *item.InvalidQueryError
	if _, err := e.svc.List(ctx, userID, householdID, item.ListQuery{Cursor: "garbage!"}); !errors.As(err, &q) || q.Parameter != "cursor" {
		t.Fatalf("cursor: %v", err)
	}
	if e.repo.Calls != 0 {
		t.Fatal("invalid query reached repository")
	}
	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, e.create(t, item.NewItem{Title: "item"}).ID)
	}
	// An archived item never appears in the default pages.
	if _, err := e.svc.Update(ctx, userID, householdID, ids[4], 1, item.Patch{Archived: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	cursor, pages := "", 0
	for {
		page, err := e.svc.List(ctx, userID, householdID, item.ListQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, it := range page.Items {
			seen = append(seen, it.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if pages != 2 || strings.Join(seen, ",") != strings.Join(ids[:4], ",") {
		t.Fatalf("pages=%d seen=%v", pages, seen)
	}
	archived, err := e.svc.List(ctx, userID, householdID, item.ListQuery{Archived: true})
	if err != nil || len(archived.Items) != 1 || archived.Items[0].ID != ids[4] || archived.NextCursor != nil {
		t.Fatalf("archived=%+v err=%v", archived, err)
	}
	empty, err := newEnv("UTC").svc.List(ctx, userID, householdID, item.ListQuery{})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestParseWorkflowState(t *testing.T) {
	for _, s := range []string{"open", "in_progress", "waiting", "paused"} {
		if got, ok := item.ParseWorkflowState(s); !ok || string(got) != s {
			t.Fatalf("%s rejected", s)
		}
	}
	for _, s := range []string{"", "OPEN", "in-progress", "done", " open", "needs_attention"} {
		if _, ok := item.ParseWorkflowState(s); ok {
			t.Fatalf("%q accepted", s)
		}
	}
}
