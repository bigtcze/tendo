package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func marshalMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGeneratedResponseModelsSerializeNullExplicitly pins the oapi-codegen
// v2.8.0 response models: every required nullable property is a single pointer
// that serializes as JSON null when unset (never omitted) and as the value
// otherwise. A stacked x-go-type pointer hint would turn these into **T.
func TestGeneratedResponseModelsSerializeNullExplicitly(t *testing.T) {
	// lastCompletedOn is required, nullable, and readOnly, which the generator
	// maps to **string with omitempty (unchanged since v2.7.2); toJSON always
	// sets the outer pointer, which TestItemResponseNullRoundTrip and
	// TestToJSONAlwaysEmitsLastCompletedOn cover.
	itemNulls := []string{"responsibleUserId", "notes", "attentionOn", "recurrence"}
	empty := marshalMap(t, Item{})
	for _, key := range itemNulls {
		if v, ok := empty[key]; !ok || v != nil {
			t.Fatalf("Item.%s: present=%v value=%v, want explicit null", key, ok, v)
		}
	}
	user, notes, attention, last := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62", "n", "2026-03-02", "2026-03-01"
	lastPtr := &last
	set := marshalMap(t, Item{ResponsibleUserId: &user, Notes: &notes, AttentionOn: &attention, Recurrence: &ItemRecurrence{IntervalValue: 2, IntervalUnit: Week, Mode: Fixed}, LastCompletedOn: &lastPtr})
	want := map[string]any{"responsibleUserId": user, "notes": notes, "attentionOn": attention, "recurrence": map[string]any{"intervalValue": float64(2), "intervalUnit": "week", "mode": "fixed"}, "lastCompletedOn": last}
	for key, value := range want {
		if !reflect.DeepEqual(set[key], value) {
			t.Fatalf("Item.%s=%v, want %v", key, set[key], value)
		}
	}

	completionNulls := []string{"cycleAttentionOn", "recurrence", "nextAttentionOn", "undoneAt", "undoneByUserId"}
	empty = marshalMap(t, Completion{})
	for _, key := range completionNulls {
		if v, ok := empty[key]; !ok || v != nil {
			t.Fatalf("Completion.%s: present=%v value=%v, want explicit null", key, ok, v)
		}
	}
	undone := time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
	set = marshalMap(t, Completion{UndoneAt: &undone, UndoneByUserId: &user, CycleAttentionOn: &attention, NextAttentionOn: &attention})
	if set["undoneAt"] != "2026-03-01T23:30:00Z" || set["undoneByUserId"] != user || set["cycleAttentionOn"] != attention || set["nextAttentionOn"] != attention {
		t.Fatalf("completion=%v", set)
	}

	for name, v := range map[string]any{"ItemList": ItemList{Items: []Item{}}, "CompletionList": CompletionList{Items: []Completion{}}} {
		if got := marshalMap(t, v); got["nextCursor"] != nil || len(got) != 2 {
			t.Fatalf("%s=%v, want explicit null nextCursor", name, got)
		}
	}
}

// TestItemResponseNullRoundTrip proves the HTTP adapter emits explicit null for
// cleared nullable fields and that the generated model decodes it to nil.
func TestItemResponseNullRoundTrip(t *testing.T) {
	f := newFixture(true)
	w := f.do("PATCH", one, `{"notes":null,"attentionOn":null,"recurrence":null,"responsibleUserId":null}`, map[string]string{"If-Match": `"1"`})
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	raw := marshalMap(t, json.RawMessage(w.Body.Bytes()))
	for _, key := range []string{"responsibleUserId", "notes", "attentionOn", "recurrence", "lastCompletedOn"} {
		if v, ok := raw[key]; !ok || v != nil {
			t.Fatalf("%s: present=%v value=%v body=%s", key, ok, v, w.Body)
		}
	}
	var got Item
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ResponsibleUserId != nil || got.Notes != nil || got.AttentionOn != nil || got.Recurrence != nil || (got.LastCompletedOn != nil && *got.LastCompletedOn != nil) {
		t.Fatalf("decoded=%+v", got)
	}
}

// TestToJSONAlwaysEmitsLastCompletedOn guards the adapter side of the readOnly
// required nullable lastCompletedOn: it is always present, null before the
// first completion and the business date afterwards.
func TestToJSONAlwaysEmitsLastCompletedOn(t *testing.T) {
	f := newFixture(true)
	before, ok := f.repo.Row(iid)
	if !ok {
		t.Fatal("seeded item missing")
	}
	if v, present := marshalMap(t, toJSON(before))["lastCompletedOn"]; !present || v != nil {
		t.Fatalf("before completion: present=%v value=%v", present, v)
	}
	date, err := schedule.NewDate(2026, time.March, 1)
	if err != nil {
		t.Fatal(err)
	}
	before.LastCompletedOn = &date
	if v := marshalMap(t, toJSON(before))["lastCompletedOn"]; v != "2026-03-01" {
		t.Fatalf("after completion: %v", v)
	}
}
