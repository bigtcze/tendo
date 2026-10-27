package item_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	repo            *itemtest.Repo
	clockCalls      int
	clock           func() time.Time
	svc             *item.Service
	zone            string
	now             time.Time
	housErr         error
	subjectErr      error
	memberErr       error
	memberAllow     map[string]bool
	subjectHit      []string
	createSeq       int
	subjectArchived bool
}

func newEnv(zone string) *env {
	e := &env{repo: itemtest.New(), zone: zone, now: fixedNow, memberAllow: map[string]bool{userID: true}}
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
			return e.subjectArchived, nil
		case archivedID:
			return true, nil
		}
		return false, item.ErrNotFound // foreignID and unknown ids
	}
	clock := func() time.Time {
		e.clockCalls++
		if e.clock != nil {
			return e.clock()
		}
		return e.now
	}
	e.svc = item.NewService(e.repo, houses, subjects, func(_ context.Context, candidate, household string) error {
		if e.memberErr != nil {
			return e.memberErr
		}
		if e.memberAllow != nil && !e.memberAllow[candidate] || household != householdID {
			return item.ErrNotFound
		}
		return nil
	}, clock)
	return e
}

func (e *env) nextKey() string { e.createSeq++; return "test-key-" + strconv.Itoa(e.createSeq) }

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
	created, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), n)
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

type creationLookupCounter struct {
	item.Repository
	repo    *itemtest.Repo
	lookups int
}

func (r *creationLookupCounter) FindCreation(ctx context.Context, householdID, actorID, key string) (item.Item, []byte, error) {
	r.lookups++
	return r.repo.FindCreation(ctx, householdID, actorID, key)
}

func TestCreateIdempotencyService(t *testing.T) {
	baseDate := "2026-02-01"
	recurrenceFixed := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
	recurrenceFluid := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}

	t.Run("replay after edit returns current item without adding a row", func(t *testing.T) {
		e := newEnv("UTC")
		key := "replay-edit"
		body := item.NewItem{SubjectID: subjectID, Title: "Original"}
		first, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil {
			t.Fatal(err)
		}
		edited, err := e.svc.Update(context.Background(), userID, householdID, first.ID, first.Version, item.Patch{Title: ptr("Edited")})
		if err != nil {
			t.Fatal(err)
		}
		count := e.repo.Count()
		replay, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil || replay.ID != first.ID || replay.Title != "Edited" || replay.Version != edited.Version || e.repo.Count() != count {
			t.Fatalf("replay=%+v edit=%+v count=%d/%d err=%v", replay, edited, e.repo.Count(), count, err)
		}
	})

	t.Run("changed intent is rejected", func(t *testing.T) {
		cases := []struct {
			name              string
			original, changed item.NewItem
		}{
			{"title", item.NewItem{SubjectID: subjectID, Title: "A"}, item.NewItem{SubjectID: subjectID, Title: "B"}},
			{"subjectId", item.NewItem{SubjectID: subjectID, Title: "A"}, item.NewItem{SubjectID: archivedID, Title: "A"}},
			{"attentionOn", item.NewItem{SubjectID: subjectID, Title: "A"}, item.NewItem{SubjectID: subjectID, Title: "A", AttentionOn: ptr("2030-01-01")}},
			{"notes content", item.NewItem{SubjectID: subjectID, Title: "A", Notes: ptr("notes one")}, item.NewItem{SubjectID: subjectID, Title: "A", Notes: ptr("notes two")}},
			{"notes whitespace is exact", item.NewItem{SubjectID: subjectID, Title: "A", Notes: ptr("notes")}, item.NewItem{SubjectID: subjectID, Title: "A", Notes: ptr(" notes ")}},
			{"recurrence mode", item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: recurrenceFixed}, item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: recurrenceFluid}},
			{"recurrence interval value", item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: recurrenceFixed}, item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}}},
			{"recurrence interval unit", item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: recurrenceFixed}, item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitWeek}, Mode: schedule.ModeFixed}}},
			{"historical date", item.NewItem{SubjectID: subjectID, Title: "A", HistoricalCompletedOn: &baseDate, Recurrence: recurrenceFixed}, item.NewItem{SubjectID: subjectID, Title: "A", HistoricalCompletedOn: ptr("2026-02-02"), Recurrence: recurrenceFixed}},
			{"historical date present versus omitted", item.NewItem{SubjectID: subjectID, Title: "A", HistoricalCompletedOn: &baseDate, Recurrence: recurrenceFixed}, item.NewItem{SubjectID: subjectID, Title: "A", Recurrence: recurrenceFixed}},
			{"notes null versus empty", item.NewItem{SubjectID: subjectID, Title: "A"}, item.NewItem{SubjectID: subjectID, Title: "A", Notes: ptr("")}},
			{"responsible member", item.NewItem{SubjectID: subjectID, Title: "A", ResponsibleUserID: ptr(userID)}, item.NewItem{SubjectID: subjectID, Title: "A", ResponsibleUserID: ptr("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62")}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := newEnv("UTC")
				key := "reuse"
				first, err := e.svc.Create(context.Background(), userID, householdID, key, tc.original)
				if err != nil {
					t.Fatal(err)
				}
				beforeCount, beforeReceipts := e.repo.Count(), e.repo.CompletionCount()
				if _, err := e.svc.Create(context.Background(), userID, householdID, key, tc.changed); !errors.Is(err, item.ErrIdempotencyKeyReused) {
					t.Fatalf("err=%v", err)
				}
				current, ok := e.repo.Row(first.ID)
				if !ok || current.Version != first.Version || e.repo.Count() != beforeCount || e.repo.CompletionCount() != beforeReceipts {
					t.Fatalf("rejected reuse changed state: current=%+v first=%+v rows=%d/%d receipts=%d/%d", current, first, e.repo.Count(), beforeCount, e.repo.CompletionCount(), beforeReceipts)
				}
			})
		}
	})

	t.Run("canonical creation intent replays", func(t *testing.T) {
		cases := []struct {
			name string
			a, b item.NewItem
		}{
			{"trimmed title", item.NewItem{SubjectID: subjectID, Title: "  Boiler "}, item.NewItem{SubjectID: subjectID, Title: "Boiler"}},
			{"disabled recurrence", item.NewItem{SubjectID: subjectID, Title: "Boiler", Recurrence: &schedule.Policy{Enabled: false, Interval: schedule.Interval{Value: 3, Unit: schedule.UnitWeek}, Mode: schedule.ModeFixed}}, item.NewItem{SubjectID: subjectID, Title: "Boiler"}},
			{"UUID case", item.NewItem{SubjectID: strings.ToUpper(subjectID), Title: "Boiler"}, item.NewItem{SubjectID: subjectID, Title: "Boiler"}},
			{"assignee UUID case", item.NewItem{SubjectID: subjectID, Title: "Boiler", ResponsibleUserID: ptr(strings.ToUpper(userID))}, item.NewItem{SubjectID: subjectID, Title: "Boiler", ResponsibleUserID: ptr(userID)}},
			{"omitted optional fields versus null", item.NewItem{SubjectID: subjectID, Title: "Boiler"}, item.NewItem{SubjectID: subjectID, Title: "Boiler", ResponsibleUserID: nil, Notes: nil, AttentionOn: nil, Recurrence: nil}},
			{"explicit-null notes and attention versus omitted", item.NewItem{SubjectID: subjectID, Title: "Boiler"}, item.NewItem{SubjectID: subjectID, Title: "Boiler", Notes: nil, AttentionOn: nil}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := newEnv("UTC")
				key := "canonical"
				first, err := e.svc.Create(context.Background(), userID, householdID, key, tc.a)
				if err != nil {
					t.Fatal(err)
				}
				replay, err := e.svc.Create(context.Background(), userID, householdID, key, tc.b)
				if err != nil || replay.ID != first.ID {
					t.Fatalf("first=%+v replay=%+v err=%v", first, replay, err)
				}
			})
		}
	})

	t.Run("replay precedes subject validation", func(t *testing.T) {
		e := newEnv("UTC")
		key := "archive-replay"
		body := item.NewItem{SubjectID: subjectID, Title: "Archived later"}
		first, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil {
			t.Fatal(err)
		}
		e.subjectArchived = true
		replay, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil || replay.ID != first.ID {
			t.Fatalf("replay=%+v err=%v", replay, err)
		}
	})

	t.Run("membership is rechecked before replay lookup", func(t *testing.T) {
		e := newEnv("UTC")
		authorized := true
		houses := func(_ context.Context, uid, hid string) (string, error) {
			if uid != userID || hid != householdID || !authorized {
				return "", item.ErrNotFound
			}
			return "UTC", nil
		}
		repository := &creationLookupCounter{Repository: e.repo, repo: e.repo}
		svc := item.NewService(repository, houses, func(context.Context, string, string, string) (bool, error) { return false, nil }, func(context.Context, string, string) error { return nil }, func() time.Time { return e.now })
		key := "membership-replay"
		body := item.NewItem{SubjectID: subjectID, Title: "member protected"}
		created, err := svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil {
			t.Fatal(err)
		}
		lookups := repository.lookups
		authorized = false
		if _, err = svc.Create(context.Background(), userID, householdID, key, body); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("revoked replay err=%v", err)
		}
		current, ok := e.repo.Row(created.ID)
		if !ok || current.Version != created.Version || e.repo.Count() != 1 || repository.lookups != lookups {
			t.Fatalf("replay lookup/state changed: current=%+v lookups=%d/%d count=%d", current, repository.lookups, lookups, e.repo.Count())
		}
	})

	t.Run("rejected create does not reserve key", func(t *testing.T) {
		e := newEnv("UTC")
		key := "rejected"
		if _, err := e.svc.Create(context.Background(), userID, householdID, key, item.NewItem{SubjectID: subjectID, Title: " "}); err == nil {
			t.Fatal("invalid title accepted")
		}
		created, err := e.svc.Create(context.Background(), userID, householdID, key, item.NewItem{SubjectID: subjectID, Title: "Valid"})
		if err != nil || created.ID == "" || e.repo.Count() != 1 {
			t.Fatalf("created=%+v count=%d err=%v", created, e.repo.Count(), err)
		}
	})

	t.Run("key scope includes actor and household", func(t *testing.T) {
		e := newEnv("UTC")
		key := "scope"
		body := item.NewItem{SubjectID: subjectID, Title: "Scoped"}
		first, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil {
			t.Fatal(err)
		}
		otherActor := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
		actorService := item.NewService(e.repo, func(_ context.Context, uid, hid string) (string, error) { return "UTC", nil }, func(context.Context, string, string, string) (bool, error) { return false, nil }, func(context.Context, string, string) error { return nil }, func() time.Time { return e.now })
		second, err := actorService.Create(context.Background(), otherActor, householdID, key, body)
		if err != nil || second.ID == first.ID {
			t.Fatalf("same-household actor scope first=%s second=%s err=%v", first.ID, second.ID, err)
		}
		third, err := actorService.Create(context.Background(), otherActor, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63", key, body)
		if err != nil || third.ID == second.ID {
			t.Fatalf("household scope second=%s third=%s err=%v", second.ID, third.ID, err)
		}
	})

	t.Run("historical initialization replay keeps one receipt", func(t *testing.T) {
		e := newEnv("UTC")
		key := "historical-replay"
		body := item.NewItem{SubjectID: subjectID, Title: "Historical", HistoricalCompletedOn: &baseDate, Recurrence: recurrenceFixed}
		first, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := e.svc.Create(context.Background(), userID, householdID, key, body)
		if err != nil || replay.ID != first.ID || e.repo.CompletionCount() != 1 {
			t.Fatalf("first=%+v replay=%+v receipts=%d err=%v", first, replay, e.repo.CompletionCount(), err)
		}
	})
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
	canonical, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "disabled maps to off", Recurrence: &falsePolicy})
	if err != nil || canonical.Recurrence != nil {
		t.Fatalf("disabled policy not canonicalized: %+v %v", canonical, err)
	}
	canonical, err = e.svc.Update(context.Background(), userID, householdID, created.ID, off.Version, item.Patch{Recurrence: item.Some(falsePolicy)})
	if err != nil || canonical.Recurrence != nil || canonical.Version != 9 {
		t.Fatalf("disabled patch not canonicalized: %+v %v", canonical, err)
	}
	if _, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "one off"}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalInitializationCreatesReceiptAndInitialAnchor(t *testing.T) {
	for _, tc := range []struct {
		mode       schedule.Mode
		unit       schedule.Unit
		date, want string
	}{
		{schedule.ModeFixed, schedule.UnitDay, "2026-03-01", "2026-03-02"},
		{schedule.ModeAfterCompletion, schedule.UnitDay, "2026-03-01", "2026-03-02"},
		{schedule.ModeFixed, schedule.UnitWeek, "2026-03-01", "2026-03-08"},
		{schedule.ModeAfterCompletion, schedule.UnitWeek, "2026-03-01", "2026-03-08"},
		{schedule.ModeFixed, schedule.UnitMonth, "2026-03-01", "2026-04-01"},
		{schedule.ModeAfterCompletion, schedule.UnitMonth, "2026-03-01", "2026-04-01"},
		{schedule.ModeFixed, schedule.UnitYear, "2026-03-01", "2027-03-01"},
		{schedule.ModeAfterCompletion, schedule.UnitYear, "2026-03-01", "2027-03-01"},
		{schedule.ModeFixed, schedule.UnitMonth, "2026-01-31", "2026-02-28"},
		{schedule.ModeFixed, schedule.UnitYear, "2024-02-29", "2025-02-28"},
		{schedule.ModeAfterCompletion, schedule.UnitYear, "2024-02-29", "2025-02-28"},
	} {
		e := newEnv("UTC")
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: tc.unit}, Mode: tc.mode}
		created := e.create(t, item.NewItem{Title: "repeat", Recurrence: &policy, HistoricalCompletedOn: ptr(tc.date)})
		if created.Version != 2 || created.AttentionOn == nil || created.AttentionOn.String() != tc.want || created.LastCompletedOn == nil || created.LastCompletedOn.String() != tc.date {
			t.Fatalf("created=%+v", created)
		}
		history, err := e.svc.ListCompletions(context.Background(), userID, householdID, created.ID, 50, "", item.CompletionOrderOldest)
		if err != nil || len(history.Items) != 1 || history.Items[0].CompletedOn.String() != tc.date || history.Items[0].CycleAttentionOn != nil || history.Items[0].NextAttentionOn == nil || history.Items[0].NextAttentionOn.String() != tc.want || history.Items[0].ItemVersionBefore != 1 {
			t.Fatalf("history=%+v err=%v", history, err)
		}
	}
}

func TestHistoricalPastAndFutureAnchorsDeriveAttention(t *testing.T) {
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	for _, tc := range []struct {
		completed, wantDate string
		want                schedule.Attention
	}{{"2026-02-27", "2026-02-28", schedule.NeedsAttention}, {"2026-03-01", "2026-03-02", schedule.Upcoming}} {
		e := newEnv("UTC")
		created := e.create(t, item.NewItem{Title: "x", HistoricalCompletedOn: ptr(tc.completed), Recurrence: &policy})
		if created.AttentionOn == nil || created.AttentionOn.String() != tc.wantDate || created.Attention != tc.want {
			t.Fatalf("created=%+v", created)
		}
		stored, ok := e.repo.Row(created.ID)
		if !ok || stored.AttentionOn == nil || stored.AttentionOn.String() != tc.wantDate || stored.Version != 2 {
			t.Fatalf("historical anchor was changed or skipped: %+v", stored)
		}
	}
}

func TestHistoricalDateUsesHouseholdLocalToday(t *testing.T) {
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	for _, tc := range []struct {
		zone, date string
		wantErr    bool
	}{{"Europe/Prague", "2026-03-02", false}, {"America/New_York", "2026-03-02", true}, {"America/New_York", "2026-03-01", false}} {
		e := newEnv(tc.zone)
		got, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x", HistoricalCompletedOn: ptr(tc.date), Recurrence: &policy})
		if tc.wantErr {
			validation(t, err, "historicalCompletedOn", "future_date")
			if e.repo.Count() != 0 {
				t.Fatal("future historical date persisted")
			}
			continue
		}
		if err != nil || got.Version != 2 {
			t.Fatalf("create=%+v err=%v", got, err)
		}
	}
}

func TestHistoricalInitializationValidation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          item.NewItem
		field, code string
	}{
		{"bad date", item.NewItem{HistoricalCompletedOn: ptr("2026-02-30")}, "historicalCompletedOn", "invalid_date"},
		{"impossible date", item.NewItem{HistoricalCompletedOn: ptr("2025-02-30"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "invalid_date"},
		{"date beats recurrence requirement", item.NewItem{HistoricalCompletedOn: ptr("2025-02-30")}, "historicalCompletedOn", "invalid_date"},
		{"conflict beats future date", item.NewItem{HistoricalCompletedOn: ptr("2026-03-03"), AttentionOn: ptr("2026-03-04"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "conflicting_fields"},
		{"invalid reference after historical checks", item.NewItem{SubjectID: foreignID, HistoricalCompletedOn: ptr("2026-03-01"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "subjectId", "invalid_reference"},
		{"archived reference after historical checks", item.NewItem{SubjectID: archivedID, HistoricalCompletedOn: ptr("2026-03-01"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "subjectId", "invalid_reference"},
		{"recurrence absent", item.NewItem{HistoricalCompletedOn: ptr("2026-01-01")}, "historicalCompletedOn", "requires_recurrence"},
		{"recurrence null", item.NewItem{HistoricalCompletedOn: ptr("2026-01-01"), Recurrence: nil}, "historicalCompletedOn", "requires_recurrence"},
		{"recurrence disabled", item.NewItem{HistoricalCompletedOn: ptr("2026-01-01"), Recurrence: &schedule.Policy{Enabled: false, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "requires_recurrence"},
		{"date overflow", item.NewItem{HistoricalCompletedOn: ptr("9999-12-31"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}}, "recurrence", "date_overflow"},
		{"attention conflict", item.NewItem{HistoricalCompletedOn: ptr("2026-01-01"), AttentionOn: ptr("2026-01-02"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "conflicting_fields"},
		{"future local date", item.NewItem{HistoricalCompletedOn: ptr("2026-03-02"), Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}}, "historicalCompletedOn", "future_date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv("UTC")
			if tc.field == "recurrence" {
				e.now = time.Date(9999, time.December, 31, 12, 0, 0, 0, time.UTC)
			}
			if tc.name == "conflict beats future date" {
				e.now = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
			}
			if tc.name != "invalid reference after historical checks" && tc.name != "archived reference after historical checks" {
				tc.in.SubjectID = subjectID
			}
			tc.in.Title = "x"
			_, err := e.svc.Create(context.Background(), userID, householdID, "test-key-"+tc.name, tc.in)
			validation(t, err, tc.field, tc.code)
			if e.repo.Count() != 0 {
				t.Fatal("invalid historical initialization persisted")
			}
		})
	}
}

func TestRecurrenceInterval999AcceptedOnCreateAndPatch(t *testing.T) {
	e := newEnv("UTC")
	policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 999, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
	created := e.create(t, item.NewItem{Title: "long recurrence", Recurrence: &policy})
	if created.Recurrence == nil || *created.Recurrence != policy {
		t.Fatalf("create did not retain interval 999: %+v", created)
	}
	updatedPolicy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 999, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}
	updated, err := e.svc.Update(context.Background(), userID, householdID, created.ID, created.Version, item.Patch{Recurrence: item.Some(updatedPolicy)})
	if err != nil || updated.Recurrence == nil || *updated.Recurrence != updatedPolicy {
		t.Fatalf("patch did not accept interval 999: %+v err=%v", updated, err)
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
			_, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "invalid", Recurrence: &tc.policy})
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
		_, err := e.svc.Create(ctx, userID, householdID, e.nextKey(), tc.in)
		validation(t, err, tc.field, tc.code)
	}
	if e.repo.Count() != 0 || e.repo.Calls != 11 {
		t.Fatalf("invalid input reached storage: rows=%d calls=%d", e.repo.Count(), e.repo.Calls)
	}
}

func TestValidationOrderIsFieldsBeforeSubjectLookup(t *testing.T) {
	e := newEnv("UTC")
	_, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: archivedID, Title: " "})
	validation(t, err, "title", "invalid_length")
	if len(e.subjectHit) != 0 {
		t.Fatalf("subject looked up despite invalid fields: %v", e.subjectHit)
	}
	_, err = e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: "nope", Title: "ok"})
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

func TestBusinessDateClockBoundaryAndCompletion(t *testing.T) {
	e := newEnv("Europe/Prague")
	e.now = time.Date(2026, 1, 15, 22, 59, 59, 0, time.UTC)
	created := e.create(t, item.NewItem{Title: "one-off", AttentionOn: ptr("2026-01-16")})
	assertAttention := func(want schedule.Attention) {
		t.Helper()
		got, err := e.svc.Get(context.Background(), userID, householdID, created.ID)
		if err != nil || got.Attention != want {
			t.Fatalf("get attention=%s err=%v want=%s", got.Attention, err, want)
		}
		page, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{})
		if err != nil || len(page.Items) != 1 || page.Items[0].Attention != want {
			t.Fatalf("list=%+v err=%v want=%s", page, err, want)
		}
	}
	assertAttention(schedule.Upcoming)
	before, _ := e.repo.Row(created.ID)
	e.now = time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC)
	assertAttention(schedule.NeedsAttention)
	after, _ := e.repo.Row(created.ID)
	if after.Version != before.Version || after.AttentionOn == nil || before.AttentionOn == nil || after.AttentionOn.String() != before.AttentionOn.String() || after.WorkflowState != before.WorkflowState || after.Done != before.Done || after.Archived != before.Archived {
		t.Fatalf("derived date changed stored item: before=%+v after=%+v", before, after)
	}
	history, err := e.svc.ListCompletions(context.Background(), userID, householdID, created.ID, 50, "", item.CompletionOrderOldest)
	if err != nil || len(history.Items) != 0 {
		t.Fatalf("unexpected completions: %+v err=%v", history, err)
	}
	future := "2026-01-17"
	if _, _, err := runCompletion(t, e, created.ID, "future-completion", created.Version, &future); err == nil {
		t.Fatal("future completion date accepted")
	} else {
		validation(t, err, "completedOn", "future_date")
	}
	unchanged, _ := e.repo.Row(created.ID)
	history, err = e.svc.ListCompletions(context.Background(), userID, householdID, created.ID, 50, "", item.CompletionOrderOldest)
	if err != nil || len(history.Items) != 0 || unchanged.Version != before.Version || unchanged.Done != before.Done || unchanged.WorkflowState != before.WorkflowState || unchanged.Archived != before.Archived || unchanged.AttentionOn == nil || unchanged.AttentionOn.String() != "2026-01-16" {
		t.Fatalf("future rejection mutated item/history: item=%+v history=%+v err=%v", unchanged, history, err)
	}
	completion, _, err := runCompletion(t, e, created.ID, "default-completion", created.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.CompletedOn.String() != "2026-01-16" {
		t.Fatalf("completedOn=%s want 2026-01-16", completion.CompletedOn)
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

func TestPatchEvaluationOrderChecksTargetBeforeValidation(t *testing.T) {
	validPolicy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 0, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	cases := []struct {
		name  string
		patch item.Patch
		field string
		code  string
	}{
		// Invalid subject references are checked against the target item's household.
		{"title", item.Patch{Title: ptr(" ")}, "title", "invalid_length"},
		{"notes", item.Patch{Notes: item.Some("")}, "notes", "invalid_length"},
		{"attentionOn", item.Patch{AttentionOn: item.Some("2026-02-30")}, "attentionOn", "invalid_date"},
		{"recurrence", item.Patch{Recurrence: item.Nullable[schedule.Policy]{Set: true, Value: validPolicy}}, "recurrence", "invalid_interval"},
		{"workflowState", item.Patch{WorkflowState: ptr("done")}, "workflowState", "invalid_workflow_state"},
		{"subjectId malformed", item.Patch{SubjectID: ptr("bad")}, "subjectId", "invalid_reference"},
		{"subjectId not found", item.Patch{SubjectID: ptr(foreignID)}, "subjectId", "invalid_reference"},
		{"subjectId archived", item.Patch{SubjectID: ptr(archivedID)}, "subjectId", "invalid_reference"},
		{"responsibleUserId malformed", item.Patch{ResponsibleUserID: item.Nullable[string]{Set: true, Value: ptr("bad")}}, "responsibleUserId", "invalid_reference"},
		{"responsibleUserId not member", item.Patch{ResponsibleUserID: item.Nullable[string]{Set: true, Value: ptr("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82")}}, "responsibleUserId", "invalid_reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, target := range []string{"missing", "foreign", "existing"} {
				t.Run(target, func(t *testing.T) {
					e := newEnv("UTC")
					created := e.create(t, item.NewItem{Title: "x"})
					targetID := created.ID
					switch target {
					case "missing":
						targetID = missingID
					case "foreign":
						foreign := created
						foreign.ID = foreignID
						foreign.HouseholdID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80"
						e.repo.Seed(foreign)
						targetID = foreignID
					}
					if target == "existing" && strings.Contains(tc.name, "subjectId") {
						if tc.name == "subjectId archived" {
							e.subjectArchived = true
						} else if tc.name == "subjectId not found" {
							e.subjectErr = item.ErrNotFound
						}
					}
					if target == "existing" && tc.name == "responsibleUserId not member" {
						e.memberErr = item.ErrNotFound
					}
					before, _ := e.repo.Row(targetID)
					_, err := e.svc.Update(context.Background(), userID, householdID, targetID, 1, tc.patch)
					if target != "existing" {
						if !errors.Is(err, item.ErrNotFound) {
							t.Fatalf("got %v, want not found", err)
						}
						return
					}
					validation(t, err, tc.field, tc.code)
					after, _ := e.repo.Row(targetID)
					if after != before {
						t.Fatalf("rejected PATCH changed state: before=%+v after=%+v", before, after)
					}
				})
			}
		})
	}
}

func TestPatchValidationProbeFailureAndSuccessfulUpdateNoExtraGet(t *testing.T) {
	e := newEnv("UTC")
	created := e.create(t, item.NewItem{Title: "x"})
	before := e.repo.Calls
	if _, err := e.svc.Update(context.Background(), userID, householdID, created.ID, 1, item.Patch{Title: ptr("updated")}); err != nil {
		t.Fatal(err)
	}
	if got := e.repo.Calls - before; got != 1 {
		t.Fatalf("successful PATCH repository calls=%d, want only Update", got)
	}
	e.repo.Err = errors.New("repository unavailable")
	_, err := e.svc.Update(context.Background(), userID, householdID, missingID, 1, item.Patch{Title: ptr(" ")})
	if !errors.Is(err, item.ErrUnavailable) {
		t.Fatalf("probe error: %v", err)
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
		if _, err := e.svc.Create(ctx, userID, bad, "bad-household-key", item.NewItem{SubjectID: subjectID, Title: "x"}); !errors.Is(err, item.ErrNotFound) {
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
	if _, err := e.svc.Create(ctx, userID, other, "other-household-key", item.NewItem{SubjectID: "nope", Title: " "}); !errors.Is(err, item.ErrNotFound) {
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
		_, err := e.svc.Create(ctx, userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x"})
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
		_, err := e.svc.Create(ctx, userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x"})
		if !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("subjects", func(t *testing.T) {
		e := newEnv("UTC")
		created := e.create(t, item.NewItem{Title: "x"})
		e.subjectErr = secret
		_, err := e.svc.Create(ctx, userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x"})
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
		if _, err := e.svc.Create(ctx, userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x"}); !errors.Is(err, item.ErrUnavailable) {
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
	_, err := e.svc.Create(context.Background(), userID, householdID, e.nextKey(), item.NewItem{SubjectID: subjectID, Title: "x"})
	validation(t, err, "subjectId", "invalid_reference")
	_, err = e.svc.Update(context.Background(), userID, householdID, created.ID, 1, item.Patch{SubjectID: ptr(subjectID)})
	validation(t, err, "subjectId", "invalid_reference")
}

func TestCursorRoundTripAndRejection(t *testing.T) {
	id := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B70"
	cursor := item.EncodeCursor(id)
	if cursor != "aTE6MDE5OGEyZjAtN2MxZS03YTUzLTliMGUtNWQzZjJjMWE0Yjcw" {
		t.Fatalf("legacy cursor bytes=%q", cursor)
	}
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

func TestListAttentionSortAndFreshnessGuard(t *testing.T) {
	e := newEnv("America/New_York")
	nullA, nullB := "0198a2f0-7c1e-7a53-9b0e-000000000001", "0198a2f0-7c1e-7a53-9b0e-000000000003"
	past, today, future := itemtest.Date(2026, time.March, 1), itemtest.Date(2026, time.March, 1), itemtest.Date(2026, time.March, 3)
	e.repo.Seed(item.Item{ID: nullA, HouseholdID: householdID, SubjectID: subjectID})
	e.repo.Seed(item.Item{ID: nullB, HouseholdID: householdID, SubjectID: subjectID})
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000002", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: past})
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000004", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: today})
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000005", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: future})
	created, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Limit: 1})
	if err != nil || len(created.Items) != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	legacy, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortCreated, Limit: 1})
	if err != nil || *created.NextCursor != *legacy.NextCursor || !strings.HasPrefix(string(decodeCursor(t, *legacy.NextCursor)), "i1:") {
		t.Fatalf("legacy cursor mismatch %v %v", created, legacy)
	}
	q := item.ListQuery{Sort: item.SortAttention, Limit: 2}
	page, err := e.svc.List(context.Background(), userID, householdID, q)
	if err != nil || page.NextCursor == nil || page.Items[0].AttentionOn != nil || page.Items[1].AttentionOn != nil {
		t.Fatalf("null page=%+v err=%v", page, err)
	}
	pos := strings.Split(string(decodeCursor(t, *page.NextCursor)), ":")
	if len(pos) != 6 || pos[4] != "-" {
		t.Fatalf("null position=%v", pos)
	}
	q.Cursor = *page.NextCursor
	more, err := e.svc.List(context.Background(), userID, householdID, q)
	if err != nil || len(more.Items) != 2 || more.Items[0].AttentionOn == nil || more.Items[0].AttentionOn.Compare(*more.Items[1].AttentionOn) > 0 {
		t.Fatalf("continuation=%+v err=%v", more, err)
	}
	pos = strings.Split(string(decodeCursor(t, *more.NextCursor)), ":")
	if pos[4] != "2026-03-01" {
		t.Fatalf("dated position=%v", pos)
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortCreated, Cursor: *page.NextCursor}); !isQuery(err, "cursor") {
		t.Fatalf("sort binding=%v", err)
	}
	for name, value := range map[string]string{"bad": "not-date", "impossible": "2026-02-30", "timestamp": "2026-03-01T00:00:00Z", "stale": "2026-03-02"} {
		before := e.repo.Calls
		_, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: value})
		if !isQuery(err, "attentionDate") || e.repo.Calls != before {
			t.Errorf("%s guard err=%v calls=%d/%d", name, err, e.repo.Calls, before)
		}
	}
	guard := "2026-03-01"
	for _, query := range []item.ListQuery{{AttentionDate: guard}, {Sort: item.SortAttention, AttentionDate: guard}, {Attention: "needs_attention", AttentionDate: guard}} {
		if _, err = e.svc.List(context.Background(), userID, householdID, query); err != nil {
			t.Errorf("guard %v: %v", query, err)
		}
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Cursor: *page.NextCursor, AttentionDate: guard}); !isQuery(err, "cursor") {
		t.Errorf("guard binding=%v", err)
	}
	// Guarded continuation gets attentionDate precedence over a stale cursor.
	guardPage, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: guard, Limit: 1})
	if err != nil || guardPage.NextCursor == nil {
		t.Fatalf("guard page=%+v err=%v", guardPage, err)
	}
	e.now = e.now.Add(24 * time.Hour)
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: guard, Cursor: "bad"}); !isQuery(err, "attentionDate") {
		t.Errorf("guard/cursor precedence=%v", err)
	}
	// Guarded attention sorting binds both the presence and value of the guard.
	e.now = fixedNow
	guardedAttention, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, AttentionDate: guard, Limit: 1})
	if err != nil || guardedAttention.NextCursor == nil {
		t.Fatalf("guarded attention page=%+v err=%v", guardedAttention, err)
	}
	guardParts := strings.Split(string(decodeCursor(t, *guardedAttention.NextCursor)), ":")
	guardParts[3] = "-"
	guardOnlyCursor := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(guardParts, ":")))
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Cursor: guardOnlyCursor}); !isQuery(err, "cursor") {
		t.Errorf("removed guard accepted: %v", err)
	}
	// Attention-sort cursors without an attention filter or freshness guard are not date-bound.
	e.now = fixedNow
	q = item.ListQuery{Sort: item.SortAttention, Limit: 1}
	freePage, err := e.svc.List(context.Background(), userID, householdID, q)
	if err != nil || freePage.NextCursor == nil {
		t.Fatalf("attention-only page=%+v err=%v", freePage, err)
	}
	e.now = e.now.Add(24 * time.Hour)
	q.Cursor = *freePage.NextCursor
	if _, err = e.svc.List(context.Background(), userID, householdID, q); err != nil {
		t.Errorf("attention-only cursor stale: %v", err)
	}
	if _, err = e.svc.List(context.Background(), "bad-user", householdID, item.ListQuery{Sort: "bad", AttentionDate: "bad"}); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("authorization precedence=%v", err)
	}
	// Use 02:00 UTC: New York is still March 1 while UTC has rolled over.
	e.now = time.Date(2026, time.March, 2, 2, 0, 0, 0, time.UTC)
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: "2026-03-01"}); err != nil {
		t.Errorf("household-local date rejected: %v", err)
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: "2026-03-02"}); !isQuery(err, "attentionDate") {
		t.Errorf("UTC date incorrectly accepted: %v", err)
	}
}

func decodeCursor(t *testing.T, cursor string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func isQuery(err error, parameter string) bool {
	var q *item.InvalidQueryError
	return errors.As(err, &q) && q.Parameter == parameter
}

func TestAttentionCursorRejectsMalformedPosition(t *testing.T) {
	e := newEnv("UTC")
	for i := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i+1), HouseholdID: householdID, SubjectID: subjectID, AttentionOn: itemtest.Date(2026, time.March, i+1)})
	}
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Limit: 1})
	if err != nil || p.NextCursor == nil {
		t.Fatalf("page=%+v err=%v", p, err)
	}
	parts := strings.Split(string(decodeCursor(t, *p.NextCursor)), ":")
	bad := map[string]string{"empty": "", "impossible": "2026-02-30", "year zero": "0000-01-01", "timestamp": "2026-03-01T00:00:00Z", "padded": "2026-03-01 ", "malformed": "not-a-date"}
	for name, pos := range bad {
		mut := append([]string(nil), parts...)
		mut[4] = pos
		cursor := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(mut, ":")))
		calls := e.repo.Calls
		_, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Cursor: cursor})
		if !isQuery(err, "cursor") || e.repo.Calls != calls {
			t.Errorf("%s err=%v repo calls %d->%d", name, err, calls, e.repo.Calls)
		}
	}
}
func TestAttentionCursorUsesReturnedNotLookaheadKey(t *testing.T) {
	e := newEnv("UTC")
	for i := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i+1), HouseholdID: householdID, SubjectID: subjectID, AttentionOn: itemtest.Date(2026, time.March, i+1)})
	}
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	if err != nil || p.NextCursor == nil {
		t.Fatalf("page=%+v err=%v", p, err)
	}
	parts := strings.Split(string(decodeCursor(t, *p.NextCursor)), ":")
	if parts[4] != "2026-03-02" || parts[5] != p.Items[1].ID {
		t.Fatalf("cursor=%v rows=%+v", parts, p.Items)
	}
}
func TestAttentionCursorRejectsCreatedFilteredCursor(t *testing.T) {
	e := newEnv("UTC")
	for i := 0; i < 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i+1), HouseholdID: householdID, SubjectID: subjectID})
	}
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Limit: 1})
	if err != nil || p.NextCursor == nil {
		t.Fatal(err)
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Sort: item.SortAttention, Cursor: *p.NextCursor}); !isQuery(err, "cursor") {
		t.Fatalf("created cursor accepted: %v", err)
	}
}
func TestAttentionCursorBindsContext(t *testing.T) {
	e := newEnv("UTC")
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Limit: 1})
	if err != nil || p.NextCursor == nil {
		t.Fatal(err)
	}
	cases := map[string]item.ListQuery{"subject": {Sort: item.SortAttention, SubjectID: subjectID, Cursor: *p.NextCursor}, "responsible": {Sort: item.SortAttention, ResponsibleUserID: "unassigned", Cursor: *p.NextCursor}, "workflow": {Sort: item.SortAttention, WorkflowState: "open", Cursor: *p.NextCursor}, "attention": {Sort: item.SortAttention, Attention: "upcoming", Cursor: *p.NextCursor}, "recurring": {Sort: item.SortAttention, Recurring: ptr(true), Cursor: *p.NextCursor}, "archived": {Sort: item.SortAttention, Archived: true, Cursor: *p.NextCursor}, "done": {Sort: item.SortAttention, Done: true, Cursor: *p.NextCursor}}
	for n, q := range cases {
		if _, err = e.svc.List(context.Background(), userID, householdID, q); !isQuery(err, "cursor") {
			t.Errorf("%s binding: %v", n, err)
		}
	}
}
func TestCreatedFilteredCursorExplicitSortEquivalent(t *testing.T) {
	e := newEnv("UTC")
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	a, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Limit: 1})
	if err != nil || a.NextCursor == nil {
		t.Fatal(err)
	}
	b, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Sort: item.SortCreated, Limit: 1})
	if err != nil || b.NextCursor == nil || *a.NextCursor != *b.NextCursor {
		t.Fatalf("omitted=%v explicit=%v err=%v", a.NextCursor, b.NextCursor, err)
	}
	for _, q := range []item.ListQuery{{SubjectID: subjectID, Cursor: *a.NextCursor}, {SubjectID: subjectID, Sort: item.SortCreated, Cursor: *b.NextCursor}} {
		if _, err = e.svc.List(context.Background(), userID, householdID, q); err != nil {
			t.Fatalf("equivalent cursor: %v", err)
		}
	}
}
func TestGuardPresenceCannotBeRemoved(t *testing.T) {
	e := newEnv("UTC")
	e.now = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	e.now = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
	guard := "2026-03-02"
	// Only guard presence differs between the two requests (same sort, no filters, same day).
	for _, sortBy := range []item.ListSort{item.SortCreated, item.SortAttention} {
		p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: sortBy, AttentionDate: guard, Limit: 1})
		if err != nil || p.NextCursor == nil {
			t.Fatalf("%s page=%+v err=%v", sortBy, p, err)
		}
		if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: sortBy, Cursor: *p.NextCursor}); !isQuery(err, "cursor") {
			t.Fatalf("%s removed guard accepted: %v", sortBy, err)
		}
	}
}
func TestGuardedCursorSameDayContinuation(t *testing.T) {
	e := newEnv("America/New_York")
	e.now = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	guard := "2026-03-02"
	for _, sortBy := range []item.ListSort{item.SortCreated, item.SortAttention} {
		q := item.ListQuery{Sort: sortBy, AttentionDate: guard, Limit: 2}
		p, err := e.svc.List(context.Background(), userID, householdID, q)
		if err != nil || p.NextCursor == nil || len(p.Items) != 2 {
			t.Fatalf("%s first page=%+v err=%v", sortBy, p, err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(*p.NextCursor)
		if err != nil || strings.Split(string(raw), ":")[3] != guard {
			t.Fatalf("%s guarded cursor not date-bound: %q err=%v", sortBy, raw, err)
		}
		q.Cursor = *p.NextCursor
		p, err = e.svc.List(context.Background(), userID, householdID, q)
		if err != nil || p.NextCursor != nil || len(p.Items) != 1 || p.Items[0].ID != "0198a2f0-7c1e-7a53-9b0e-000000000003" {
			t.Fatalf("%s continuation page=%+v err=%v", sortBy, p, err)
		}
	}
}
func TestGuardedCursorMidnightTransitions(t *testing.T) {
	e := newEnv("America/New_York")
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	e.now = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	old := "2026-03-01"
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: old, Limit: 1})
	if err != nil || p.NextCursor == nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour)
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: old, Cursor: *p.NextCursor}); !isQuery(err, "attentionDate") {
		t.Fatalf("old guard=%v", err)
	}
	newDate := "2026-03-02"
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: newDate, Cursor: *p.NextCursor}); !isQuery(err, "cursor") {
		t.Fatalf("new guard old cursor=%v", err)
	}
}
func TestAttentionFilteredSortCursorExpiresAtMidnight(t *testing.T) {
	e := newEnv("America/New_York")
	e.now = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		e.repo.Seed(item.Item{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", i), HouseholdID: householdID, SubjectID: subjectID})
	}
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Attention: "needs_attention", Limit: 1})
	if err != nil || p.NextCursor == nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour)
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Sort: item.SortAttention, Attention: "needs_attention", Cursor: *p.NextCursor}); !isQuery(err, "cursor") {
		t.Fatalf("stale attention cursor=%v", err)
	}
}
func TestSummaryPartitionsAndUsesHouseholdBusinessDate(t *testing.T) {
	e := newEnv("Europe/Prague")
	today := itemtest.Date(2026, time.March, 2)
	rows := []item.Item{
		{ID: "a", HouseholdID: householdID, AttentionOn: nil, WorkflowState: item.StateOpen},
		{ID: "b", HouseholdID: householdID, AttentionOn: itemtest.Date(2026, time.March, 1), WorkflowState: item.StateOpen},
		{ID: "c", HouseholdID: householdID, AttentionOn: today, WorkflowState: item.StateOpen},
		{ID: "d", HouseholdID: householdID, AttentionOn: itemtest.Date(2026, time.March, 3), WorkflowState: item.StateOpen},
		{ID: "e", HouseholdID: householdID, AttentionOn: today, WorkflowState: item.StateInProgress},
		{ID: "f", HouseholdID: householdID, AttentionOn: today, WorkflowState: item.StateWaiting},
		{ID: "g", HouseholdID: householdID, AttentionOn: today, WorkflowState: item.StatePaused},
		{ID: "h", HouseholdID: householdID, Done: true, Archived: true},
		{ID: "i", HouseholdID: householdID, Done: true},
	}
	for _, row := range rows {
		e.repo.Seed(row)
	}
	got, err := e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := item.Summary{EvaluatedOn: *today, SummaryCounts: item.SummaryCounts{Total: 9, NeedsAttention: 3, Upcoming: 1, InProgress: 1, Waiting: 1, Paused: 1, Done: 1, Archived: 1}}
	if got != want {
		t.Fatalf("summary=%+v want %+v", got, want)
	}
}

func TestListUsesOneBusinessDate(t *testing.T) {
	e := newEnv("America/New_York")
	e.now = time.Date(2026, time.March, 2, 4, 30, 0, 0, time.UTC)
	today := itemtest.Date(2026, time.March, 1)
	tomorrow := itemtest.Date(2026, time.March, 2)
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000001", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: today})
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000002", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: tomorrow})
	calls := 0
	e.svc = item.NewService(e.repo, func(context.Context, string, string) (string, error) { return e.zone, nil }, nil, nil, func() time.Time {
		calls++
		if calls == 1 {
			return time.Date(2026, time.March, 2, 4, 30, 0, 0, time.UTC)
		}
		return time.Date(2026, time.March, 2, 5, 30, 0, 0, time.UTC)
	})
	p, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{AttentionDate: "2026-03-01", Attention: "needs_attention"})
	if calls != 1 {
		t.Fatalf("clock called %d times", calls)
	}
	e.clockCalls = calls
	if err != nil || len(p.Items) != 1 || p.Items[0].ID != "0198a2f0-7c1e-7a53-9b0e-000000000001" || p.Items[0].Attention != schedule.NeedsAttention {
		t.Fatalf("page=%+v err=%v", p, err)
	}
}
func TestListSortAndGuardValidationDoesNotCallRepository(t *testing.T) {
	e := newEnv("UTC")
	for _, tc := range []struct {
		name  string
		q     item.ListQuery
		param string
	}{{"uppercase sort", item.ListQuery{Sort: "Created"}, "sort"}, {"padded sort", item.ListQuery{Sort: " created"}, "sort"}, {"unsupported sort", item.ListQuery{Sort: "title"}, "sort"}, {"year zero", item.ListQuery{AttentionDate: "0000-01-01"}, "attentionDate"}, {"padded guard", item.ListQuery{AttentionDate: " 2026-03-02"}, "attentionDate"}} {
		before := e.repo.Calls
		_, err := e.svc.List(context.Background(), userID, householdID, tc.q)
		if !isQuery(err, tc.param) || e.repo.Calls != before {
			t.Errorf("%s err=%v calls=%d/%d", tc.name, err, e.repo.Calls, before)
		}
	}
}

func TestListSearchValidationAndBinding(t *testing.T) {
	e := newEnv("UTC")
	for _, tc := range []struct {
		q     string
		valid bool
	}{{strings.Repeat("a", 200), true}, {strings.Repeat("😀", 200), true}, {strings.Repeat("a", 201), false}, {"  title  ", true}, {" \t ", false}, {"a\nb", false}, {"a\u200bb", false}, {string([]byte{0xff}), false}} {
		got, err := item.ValidateSearch(tc.q)
		if tc.valid && err != nil {
			t.Fatalf("valid search %q: %v", tc.q, err)
		}
		if !tc.valid && !isQuery(err, "q") {
			t.Fatalf("invalid search %q: %v", tc.q, err)
		}
		if tc.q == "  title  " && got != "title" {
			t.Fatalf("trimmed=%q", got)
		}
	}
	if _, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Q: "   "}); !isQuery(err, "q") {
		t.Fatalf("invalid q: %v", err)
	}
	if e.repo.Calls != 0 {
		t.Fatalf("repository called after invalid q: %d", e.repo.Calls)
	}
	if _, err := e.svc.Summary(context.Background(), userID, householdID, item.SummaryQuery{Q: "\n\t"}); !isQuery(err, "q") || e.repo.Calls != 0 {
		t.Fatalf("summary invalid q err=%v calls=%d", err, e.repo.Calls)
	}
	e.repo.Seed(item.Item{ID: "00000000-0000-0000-0000-000000000001", HouseholdID: householdID, Title: "title", WorkflowState: item.StateOpen})
	page, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{Q: "  title  "})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("trimmed query not passed to repository: page=%+v err=%v", page, err)
	}
	legacy := item.ListFilters{Archived: true, Done: true, SubjectID: subjectID, WorkflowState: item.StateWaiting}
	beforeQ, _ := json.Marshal([]string{"tendo.items.list.v2", "true", "true", subjectID, "", "waiting", "", "", "created", ""})
	digest := sha256.Sum256(beforeQ)
	want := fmt.Sprintf("%x", digest)
	if got := item.ListFingerprintForTest(legacy, item.SortCreated, ""); got != want {
		t.Fatalf("absent-q fingerprint=%s want pre-q fingerprint=%s", got, want)
	}
}

func TestListStructuredFiltersAndBoundCursor(t *testing.T) {
	e := newEnv("America/New_York")
	future := itemtest.Date(2026, time.March, 3)
	past := itemtest.Date(2026, time.March, 2)
	responsible := userID
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000001", HouseholdID: householdID, SubjectID: subjectID, ResponsibleUserID: &responsible, AttentionOn: future, WorkflowState: item.StatePaused, Recurrence: &schedule.Policy{Enabled: true}, CreatedAt: fixedNow})
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000002", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: past, WorkflowState: item.StatePaused, CreatedAt: fixedNow})
	for _, tc := range []struct {
		name string
		q    item.ListQuery
		want int
	}{
		{"all AND filters", item.ListQuery{SubjectID: strings.ToUpper(subjectID), ResponsibleUserID: responsible, WorkflowState: "paused", Attention: "upcoming", Recurring: ptr(true)}, 1},
		{"unassigned", item.ListQuery{ResponsibleUserID: "unassigned"}, 1},
		{"recurring false", item.ListQuery{Recurring: ptr(false)}, 1},
		{"needs attention excludes future local date", item.ListQuery{Attention: "needs_attention"}, 0},
		{"unknown id empty", item.ListQuery{SubjectID: missingID}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := e.svc.List(context.Background(), userID, householdID, tc.q)
			if err != nil || len(page.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", page.Items, err)
			}
		})
	}
	q := item.ListQuery{SubjectID: subjectID, Limit: 1}
	page, err := e.svc.List(context.Background(), userID, householdID, q)
	if err != nil || page.NextCursor == nil {
		t.Fatalf("first=%+v err=%v", page, err)
	}
	cursor := *page.NextCursor
	q.Cursor = cursor
	q.Limit = 2
	if _, err = e.svc.List(context.Background(), userID, householdID, q); err != nil {
		t.Fatalf("changed limit rejected: %v", err)
	}
	var invalid *item.InvalidQueryError
	for name, changed := range map[string]item.ListQuery{
		"archived":    {Archived: true, SubjectID: subjectID, Cursor: cursor},
		"done":        {Done: true, SubjectID: subjectID, Cursor: cursor},
		"subject":     {SubjectID: missingID, Cursor: cursor},
		"responsible": {SubjectID: subjectID, ResponsibleUserID: "unassigned", Cursor: cursor},
		"workflow":    {SubjectID: subjectID, WorkflowState: "paused", Cursor: cursor},
		"attention":   {SubjectID: subjectID, Attention: "upcoming", Cursor: cursor},
		"recurring":   {SubjectID: subjectID, Recurring: ptr(false), Cursor: cursor},
	} {
		if _, err = e.svc.List(context.Background(), userID, householdID, changed); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
			t.Errorf("%s mismatch err=%v", name, err)
		}
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{Cursor: cursor}); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("unfiltered i2 accepted: %v", err)
	}
	legacy := item.EncodeCursor("0198a2f0-7c1e-7a53-9b0e-000000000001")
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Cursor: legacy}); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("filtered i1 accepted: %v", err)
	}
	// Explicit archived=false and UUID hex casing are fingerprint-equivalent.
	firstDefault, err := e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Limit: 1})
	if err != nil || firstDefault.NextCursor == nil {
		t.Fatalf("default cursor=%+v err=%v", firstDefault, err)
	}
	if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: strings.ToUpper(subjectID), Archived: false, Cursor: *firstDefault.NextCursor}); err != nil {
		t.Fatalf("equivalent filter rejected: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(*firstDefault.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(decoded), ":")
	canonical, _ := json.Marshal([]string{"tendo.items.list.v2", "false", "false", subjectID, "", "", "", "", "created", ""})
	digest := sha256.Sum256(canonical)
	wantFingerprint := hex.EncodeToString(digest[:])
	if len(parts) != 6 || parts[0] != "i2" || parts[2] != wantFingerprint {
		t.Fatalf("cursor payload=%q fingerprint=%s want=%s", decoded, func() string {
			if len(parts) > 2 {
				return parts[2]
			}
			return ""
		}(), wantFingerprint)
	}
	validPayload := strings.Split(string(decoded), ":")
	mutations := map[string]func([]string) []string{
		"extra field":         func(p []string) []string { return append(p, "extra") },
		"missing field":       func(p []string) []string { return p[:5] },
		"wrong version":       func(p []string) []string { p[0] = "i3"; return p },
		"bad last UUID":       func(p []string) []string { p[5] = "not-a-uuid"; return p },
		"uppercase last UUID": func(p []string) []string { p[5] = strings.ToUpper(p[5]); return p },
		"changed household":   func(p []string) []string { p[1] = missingID; return p },
		"changed hash":        func(p []string) []string { p[2] = strings.Repeat("0", 64); return p },
		"changed date":        func(p []string) []string { p[3] = "2026-03-02"; return p },
		"bad sort":            func(p []string) []string { p[4] = "title"; return p },
	}
	encode := func(p []string) string { return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(p, ":"))) }
	for name, mutate := range mutations {
		p := append([]string(nil), validPayload...)
		malformed := encode(mutate(p))
		if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Cursor: malformed}); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
			t.Errorf("%s cursor accepted: %v", name, err)
		}
	}
	for name, bad := range map[string]string{"padded": *firstDefault.NextCursor + "=", "invalid base64": "%%%"} {
		if _, err = e.svc.List(context.Background(), userID, householdID, item.ListQuery{SubjectID: subjectID, Cursor: bad}); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
			t.Errorf("%s cursor accepted: %v", name, err)
		}
	}
	e.repo.Seed(item.Item{ID: "0198a2f0-7c1e-7a53-9b0e-000000000003", HouseholdID: householdID, SubjectID: subjectID, AttentionOn: future, WorkflowState: item.StatePaused, CreatedAt: fixedNow})
	q = item.ListQuery{Attention: "upcoming", Limit: 1}
	datePage, err := e.svc.List(context.Background(), userID, householdID, q)
	if err != nil || datePage.NextCursor == nil {
		t.Fatalf("attention page=%+v err=%v", datePage, err)
	}
	e.now = e.now.Add(8 * time.Hour)
	q.Cursor = *datePage.NextCursor
	if _, err = e.svc.List(context.Background(), userID, householdID, q); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("midnight binding err=%v", err)
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
