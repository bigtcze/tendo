package mcptool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

const (
	subjectOne   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b93"
	completionA  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b92"
	clientKey    = "k-2026-0001"
	driverSecret = "pq: password=secret host=10.0.0.9"
)

// fakeWriter records the arguments of each write call and answers fixed values.
// calls counts every service call, so a refusal can be checked to never reach it.
type fakeWriter struct {
	calls        int
	getCalls     int
	userID       string
	household    string
	itemID       string
	completionID string
	version      int64
	key          string
	newItem      item.NewItem
	patch        item.Patch
	request      item.CompletionRequest
	err          error
	getErr       error
	replayed     bool
	completion   item.Completion
	current      item.Item
}

func (f *fakeWriter) Create(_ context.Context, userID, householdID, key string, n item.NewItem) (item.Item, error) {
	f.calls++
	f.userID, f.household, f.key, f.newItem = userID, householdID, key, n
	if f.err != nil {
		return item.Item{}, f.err
	}
	return sampleItem(), nil
}

func (f *fakeWriter) Update(_ context.Context, userID, householdID, itemID string, expectedVersion int64, p item.Patch) (item.Item, error) {
	f.calls++
	f.userID, f.household, f.itemID, f.version, f.patch = userID, householdID, itemID, expectedVersion, p
	if f.err != nil {
		return item.Item{}, f.err
	}
	return sampleItem(), nil
}

func (f *fakeWriter) Complete(_ context.Context, userID, householdID, itemID string, expectedVersion int64, key string, request item.CompletionRequest) (item.Completion, bool, error) {
	f.calls++
	f.userID, f.household, f.itemID, f.version, f.key, f.request = userID, householdID, itemID, expectedVersion, key, request
	if f.err != nil {
		return item.Completion{}, false, f.err
	}
	return f.completion, f.replayed, nil
}

func (f *fakeWriter) UndoCompletion(_ context.Context, userID, householdID, itemID, completionID string, expectedVersion int64) (item.Completion, error) {
	f.calls++
	f.userID, f.household, f.itemID, f.completionID, f.version = userID, householdID, itemID, completionID, expectedVersion
	if f.err != nil {
		return item.Completion{}, f.err
	}
	return f.completion, nil
}

func (f *fakeWriter) Get(_ context.Context, userID, householdID, itemID string) (item.Item, error) {
	f.calls++
	f.getCalls++
	f.userID, f.household, f.itemID = userID, householdID, itemID
	if f.getErr != nil {
		return item.Item{}, f.getErr
	}
	return f.current, nil
}

func writeSession(t *testing.T, user string, scopes []string, grant string, service Writer) *session {
	t.Helper()
	return newSessionGrant(t, user, scopes, grant, WriteTools(service)...)
}

// writeWith calls one write tool as the write-scoped user of grant g1.
func writeWith(t *testing.T, service Writer, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	return writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", service).call(name, args)
}

func createArgs() map[string]any {
	return map[string]any{"householdId": houseA, "subjectId": subjectOne, "title": "Change filter", "idempotencyKey": clientKey}
}

func updateArgs() map[string]any {
	return map[string]any{"householdId": houseA, "itemId": itemA, "version": 4, "title": "Change filter"}
}

func completeArgs() map[string]any {
	return map[string]any{"householdId": houseA, "itemId": itemA, "version": 4, "idempotencyKey": clientKey}
}

func undoArgs() map[string]any {
	return map[string]any{"householdId": houseA, "itemId": itemA, "completionId": completionA, "version": 4}
}

// monthlyRule is a valid repeat rule argument: every 3 months, fixed.
func monthlyRule() map[string]any {
	return map[string]any{"intervalValue": 3, "intervalUnit": "month", "mode": "fixed"}
}

// without returns a copy of m with key removed.
func without(m map[string]any, key string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// keyOf returns a key of n visible ASCII bytes.
func keyOf(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'k'
	}
	return string(b)
}

// nullState names the wire state of a nullable patch field.
func nullState[T any](n item.Nullable[T]) string {
	switch {
	case !n.Set:
		return "absent"
	case n.Value == nil:
		return "null"
	default:
		return "value"
	}
}

func TestItemWriteToolsDeclareWriteScopeAndAnnotations(t *testing.T) {
	destructive := map[string]bool{CreateItemName: false, UpdateItemName: true, CompleteItemName: false, UndoCompletionName: true}
	tools := WriteTools(&fakeWriter{})
	if len(tools) != len(destructive) {
		t.Fatalf("got %d tools, want %d", len(tools), len(destructive))
	}
	for _, tool := range tools {
		a := tool.Tool.Annotations
		want, known := destructive[tool.Tool.Name]
		if !known || tool.Scope != identity.MCPScopeWrite || a == nil || a.ReadOnlyHint || a.IdempotentHint ||
			a.DestructiveHint == nil || *a.DestructiveHint != want {
			t.Fatalf("tool %q: scope=%q annotations=%+v want destructive=%v", tool.Tool.Name, tool.Scope, a, want)
		}
	}
}

func TestItemWriteToolsRefuseMissingPrincipalAndReadOnlyTokenBeforeTheService(t *testing.T) {
	cases := map[string]map[string]any{
		CreateItemName: createArgs(), UpdateItemName: updateArgs(), CompleteItemName: completeArgs(), UndoCompletionName: undoArgs(),
	}
	fake := &fakeWriter{}
	noPrincipal := writeSession(t, "", nil, "g1", fake)
	readOnly := writeSession(t, userA, []string{identity.MCPScopeRead}, "g1", fake)
	for name, args := range cases {
		for label, s := range map[string]*session{"no principal": noPrincipal, "read-only token": readOnly} {
			if _, msg := s.call(name, args); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
				t.Fatalf("%s with %s: got %q", name, label, msg)
			}
		}
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times for refused callers", fake.calls)
	}
}

func TestItemWriteSchemasRefuseBeforeTheService(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"create null title", CreateItemName, clone(createArgs(), "title", nil)},
		{"create unknown property", CreateItemName, clone(createArgs(), "owner", userB)},
		{"create missing key", CreateItemName, without(createArgs(), "idempotencyKey")},
		{"create bad household", CreateItemName, clone(createArgs(), "householdId", "../x")},
		{"create bad subject", CreateItemName, clone(createArgs(), "subjectId", "nope")},
		{"create recurrence missing field", CreateItemName, clone(createArgs(), "recurrence", without(monthlyRule(), "mode"))},
		{"create recurrence extra property", CreateItemName, clone(createArgs(), "recurrence", clone(monthlyRule(), "x", 1))},
		{"create recurrence interval 0", CreateItemName, clone(createArgs(), "recurrence", clone(monthlyRule(), "intervalValue", 0))},
		{"create recurrence interval 1000", CreateItemName, clone(createArgs(), "recurrence", clone(monthlyRule(), "intervalValue", 1000))},
		{"create recurrence bad unit", CreateItemName, clone(createArgs(), "recurrence", clone(monthlyRule(), "intervalUnit", "fortnight"))},
		{"create bad attentionOn", CreateItemName, clone(createArgs(), "attentionOn", "2026-6-1")},
		{"create bad historical date", CreateItemName, clone(createArgs(), "historicalCompletedOn", "20260601")},
		{"create key bad chars", CreateItemName, clone(createArgs(), "idempotencyKey", "bad key")},
		{"create key 129 bytes", CreateItemName, clone(createArgs(), "idempotencyKey", keyOf(129))},
		{"update null title", UpdateItemName, clone(updateArgs(), "title", nil)},
		{"update null subjectId", UpdateItemName, clone(updateArgs(), "subjectId", nil)},
		{"update null workflowState", UpdateItemName, clone(updateArgs(), "workflowState", nil)},
		{"update null archived", UpdateItemName, clone(updateArgs(), "archived", nil)},
		{"update bad itemId", UpdateItemName, clone(updateArgs(), "itemId", "x")},
		{"update version 0", UpdateItemName, clone(updateArgs(), "version", 0)},
		{"update missing version", UpdateItemName, without(updateArgs(), "version")},
		{"update bad workflow enum", UpdateItemName, clone(updateArgs(), "workflowState", "done")},
		{"update bad attentionOn", UpdateItemName, clone(updateArgs(), "attentionOn", "2026-6-1")},
		{"update recurrence missing field", UpdateItemName, clone(updateArgs(), "recurrence", without(monthlyRule(), "intervalUnit"))},
		{"update recurrence interval 1000", UpdateItemName, clone(updateArgs(), "recurrence", clone(monthlyRule(), "intervalValue", 1000))},
		{"update unknown property", UpdateItemName, clone(updateArgs(), "owner", userB)},
		{"update empty notes refused", UpdateItemName, clone(updateArgs(), "notes", "")},
		{"complete missing key", CompleteItemName, without(completeArgs(), "idempotencyKey")},
		{"complete key 129 bytes", CompleteItemName, clone(completeArgs(), "idempotencyKey", keyOf(129))},
		{"complete bad date", CompleteItemName, clone(completeArgs(), "completedOn", "2026/06/01")},
		{"complete null date", CompleteItemName, clone(completeArgs(), "completedOn", nil)},
		{"complete version 0", CompleteItemName, clone(completeArgs(), "version", 0)},
		{"complete unknown property", CompleteItemName, clone(completeArgs(), "owner", userB)},
		{"undo missing version", UndoCompletionName, without(undoArgs(), "version")},
		{"undo bad completionId", UndoCompletionName, clone(undoArgs(), "completionId", "x")},
		{"undo version 0", UndoCompletionName, clone(undoArgs(), "version", 0)},
		{"undo unknown property", UndoCompletionName, clone(undoArgs(), "owner", userB)},
	}
	fake := &fakeWriter{}
	s := writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", fake)
	for _, tc := range cases {
		if _, msg := s.call(tc.tool, tc.args); msg == "" {
			t.Fatalf("%s: accepted", tc.name)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times for refused arguments", fake.calls)
	}
}

func TestCreateItemPassesTheArgumentsAndTheDerivedKey(t *testing.T) {
	fake := &fakeWriter{}
	args := clone(createArgs(), "responsibleUserId", userB)
	args = clone(args, "notes", "milk")
	args = clone(args, "attentionOn", "2026-06-01")
	args = clone(args, "recurrence", monthlyRule())
	out, msg := writeWith(t, fake, CreateItemName, args)
	if msg != "" {
		t.Fatal(msg)
	}
	n := fake.newItem
	if fake.userID != userA || fake.household != houseA || n.SubjectID != subjectOne || n.Title != "Change filter" {
		t.Fatalf("service got user=%q household=%q subject=%q title=%q", fake.userID, fake.household, n.SubjectID, n.Title)
	}
	if n.ResponsibleUserID == nil || *n.ResponsibleUserID != userB || n.Notes == nil || *n.Notes != "milk" ||
		n.AttentionOn == nil || *n.AttentionOn != "2026-06-01" || n.HistoricalCompletedOn != nil {
		t.Fatalf("draft=%+v", n)
	}
	if n.Recurrence == nil || !n.Recurrence.Enabled || n.Recurrence.Interval.Value != 3 ||
		n.Recurrence.Interval.Unit != schedule.UnitMonth || n.Recurrence.Mode != schedule.ModeFixed {
		t.Fatalf("recurrence=%+v", n.Recurrence)
	}
	if fake.key == "" || fake.key == clientKey {
		t.Fatalf("service received the raw client key or no key: %q", fake.key)
	}
	if out["id"] != itemA {
		t.Fatalf("output=%v", out)
	}
}

func TestItemWriteDerivedKeyIsNotTheClientKeyAndFollowsTheGrant(t *testing.T) {
	run := func(grant string) (string, string) {
		fake := &fakeWriter{}
		s := writeSession(t, userA, []string{identity.MCPScopeWrite}, grant, fake)
		if _, msg := s.call(CreateItemName, createArgs()); msg != "" {
			t.Fatal(msg)
		}
		createKey := fake.key
		if _, msg := s.call(CompleteItemName, completeArgs()); msg != "" {
			t.Fatal(msg)
		}
		return createKey, fake.key
	}
	createG1, completeG1 := run("g1")
	createAgain, completeAgain := run("g1")
	createG2, completeG2 := run("g2")
	if createG1 != createAgain || completeG1 != completeAgain {
		t.Fatal("same grant and client key must derive the same service key")
	}
	if createG1 == createG2 || completeG1 == completeG2 {
		t.Fatal("another grant must derive a different service key")
	}
	if createG1 == clientKey || completeG1 == clientKey {
		t.Fatal("service received the raw client key")
	}
}

func TestCompleteItemPassesTheRequestAndReturnsReplayAndItemVersion(t *testing.T) {
	on, _ := schedule.NewDate(2026, time.June, 1)
	completion := item.Completion{ID: completionA, ItemID: itemA, CompletedByUserID: userA, CompletedOn: on, ItemVersionBefore: 4}
	for _, replayed := range []bool{false, true} {
		fake := &fakeWriter{completion: completion, replayed: replayed}
		args := clone(completeArgs(), "completedOn", "2026-06-01")
		out, msg := writeWith(t, fake, CompleteItemName, args)
		if msg != "" {
			t.Fatal(msg)
		}
		if fake.version != 4 || fake.itemID != itemA || fake.request.CompletedOn == nil || *fake.request.CompletedOn != "2026-06-01" {
			t.Fatalf("service got version=%d item=%q request=%+v", fake.version, fake.itemID, fake.request)
		}
		if out["itemVersion"] != float64(5) || out["replayed"] != replayed {
			t.Fatalf("replayed=%v: output=%v", replayed, out)
		}
		if c, ok := out["completion"].(map[string]any); !ok || c["id"] != completionA {
			t.Fatalf("completion=%v", out["completion"])
		}
	}
}

// TestUndoCompletionReturnsTheItemReadAfterTheUndo pins that the item is the
// one Get returned after the undo: its content and its version come from the
// same read, so a version is never reported without the content it belongs to.
func TestUndoCompletionReturnsTheItemReadAfterTheUndo(t *testing.T) {
	current := sampleItem()
	current.Version = 7
	current.Title = "Read after undo"
	fake := &fakeWriter{completion: item.Completion{ID: completionA, ItemID: itemA}, current: current}
	out, msg := writeWith(t, fake, UndoCompletionName, undoArgs())
	if msg != "" {
		t.Fatal(msg)
	}
	if fake.completionID != completionA || fake.version != 4 || fake.getCalls != 1 || fake.household != houseA || fake.itemID != itemA {
		t.Fatalf("service got completion=%q version=%d gets=%d", fake.completionID, fake.version, fake.getCalls)
	}
	if _, legacy := out["itemVersion"]; legacy {
		t.Fatalf("itemVersion must not be a top-level field: %v", out)
	}
	it, ok := out["item"].(map[string]any)
	if !ok {
		t.Fatalf("item=%v want an object", out["item"])
	}
	if it["version"] != float64(7) || it["title"] != "Read after undo" || it["id"] != itemA {
		t.Fatalf("item does not match the Get read: %v", it)
	}
	if c, ok := out["completion"].(map[string]any); !ok || c["id"] != completionA {
		t.Fatalf("completion=%v", out["completion"])
	}
}

func TestUndoCompletionReadsNothingAfterARefusedUndo(t *testing.T) {
	fake := &fakeWriter{err: item.ErrVersionMismatch}
	if _, msg := writeWith(t, fake, UndoCompletionName, undoArgs()); msg != identityhttp.ErrMCPToolVersionChanged.Error() {
		t.Fatalf("msg=%q", msg)
	}
	if fake.getCalls != 0 {
		t.Fatalf("Get called %d times after a refused undo", fake.getCalls)
	}
}

// TestUndoCompletionKeepsTheCommittedUndoWhenTheReadAfterFails pins that a
// failed read after a committed undo never reports an error: the caller gets the
// completion and a null item, and retries the read on its own.
func TestUndoCompletionKeepsTheCommittedUndoWhenTheReadAfterFails(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"not found", item.ErrNotFound},
		{"unavailable", item.ErrUnavailable},
		{"driver text", errors.New(driverSecret)},
	}
	for _, tc := range cases {
		fake := &fakeWriter{completion: item.Completion{ID: completionA, ItemID: itemA}, getErr: tc.err}
		out, msg := writeWith(t, fake, UndoCompletionName, undoArgs())
		if msg != "" {
			t.Fatalf("%s: a committed undo reported %q", tc.name, msg)
		}
		if c, ok := out["completion"].(map[string]any); !ok || c["id"] != completionA {
			t.Fatalf("%s: completion=%v", tc.name, out["completion"])
		}
		if got, present := out["item"]; !present || got != nil {
			t.Fatalf("%s: item=%v present=%v want null", tc.name, got, present)
		}
	}
}

func TestUpdateItemDistinguishesAbsentNullAndValueThroughTheSDK(t *testing.T) {
	fields := []struct {
		name    string
		value   any
		state   func(item.Patch) string
		valueOK func(item.Patch) bool
	}{
		{"responsibleUserId", userB,
			func(p item.Patch) string { return nullState(p.ResponsibleUserID) },
			func(p item.Patch) bool {
				return p.ResponsibleUserID.Value != nil && *p.ResponsibleUserID.Value == userB
			}},
		{"notes", "milk",
			func(p item.Patch) string { return nullState(p.Notes) },
			func(p item.Patch) bool { return p.Notes.Value != nil && *p.Notes.Value == "milk" }},
		{"attentionOn", "2026-06-01",
			func(p item.Patch) string { return nullState(p.AttentionOn) },
			func(p item.Patch) bool { return p.AttentionOn.Value != nil && *p.AttentionOn.Value == "2026-06-01" }},
		{"recurrence", monthlyRule(),
			func(p item.Patch) string { return nullState(p.Recurrence) },
			func(p item.Patch) bool {
				r := p.Recurrence.Value
				return r != nil && r.Enabled && r.Interval.Value == 3 && r.Interval.Unit == schedule.UnitMonth && r.Mode == schedule.ModeFixed
			}},
	}
	for _, f := range fields {
		variants := []struct {
			label string
			args  map[string]any
			want  string
		}{
			{"absent", without(updateArgs(), f.name), "absent"},
			{"null", clone(updateArgs(), f.name, nil), "null"},
			{"value", clone(updateArgs(), f.name, f.value), "value"},
		}
		for _, v := range variants {
			fake := &fakeWriter{}
			if _, msg := writeWith(t, fake, UpdateItemName, v.args); msg != "" {
				t.Fatalf("%s %s: %s", f.name, v.label, msg)
			}
			if got := f.state(fake.patch); got != v.want {
				t.Fatalf("%s %s: patch state=%q want %q (patch=%+v)", f.name, v.label, got, v.want, fake.patch)
			}
			if v.want == "value" && !f.valueOK(fake.patch) {
				t.Fatalf("%s value not received: %+v", f.name, fake.patch)
			}
		}
	}
}

func TestUpdateItemKeepsTitleAndArchivedWhenAbsent(t *testing.T) {
	fake := &fakeWriter{}
	if _, msg := writeWith(t, fake, UpdateItemName, without(updateArgs(), "title")); msg != "" {
		t.Fatal(msg)
	}
	if fake.patch.Title != nil || fake.patch.Archived != nil {
		t.Fatalf("absent title/archived must stay nil: %+v", fake.patch)
	}
	fake = &fakeWriter{}
	if _, msg := writeWith(t, fake, UpdateItemName, clone(without(updateArgs(), "title"), "archived", true)); msg != "" {
		t.Fatal(msg)
	}
	if fake.patch.Archived == nil || !*fake.patch.Archived || fake.patch.Title != nil {
		t.Fatalf("archived=true must reach the patch alone: %+v", fake.patch)
	}
	if fake.version != 4 || fake.itemID != itemA {
		t.Fatalf("service got version=%d item=%q", fake.version, fake.itemID)
	}
}

func TestItemWriteToolsMapServiceErrorsToFixedTexts(t *testing.T) {
	driver := errors.New(driverSecret)
	cases := []struct {
		name string
		tool string
		args map[string]any
		err  error
		want string
	}{
		{"create not found", CreateItemName, createArgs(), item.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{"create key reused", CreateItemName, createArgs(), item.ErrIdempotencyKeyReused, identityhttp.ErrMCPToolKeyReused.Error()},
		{"create title refused", CreateItemName, createArgs(), &item.ValidationError{Field: "title", Code: "invalid_length"}, identityhttp.MCPInvalidArgument("title").Error()},
		{"create historical refused", CreateItemName, createArgs(), &item.ValidationError{Field: "historicalCompletedOn", Code: "requires_recurrence"}, identityhttp.MCPInvalidArgument("historicalCompletedOn").Error()},
		{"create date overflow", CreateItemName, createArgs(), &item.ValidationError{Field: "recurrence", Code: "date_overflow"}, ErrMCPToolDateOverflow.Error()},
		{"create recurrence invalid", CreateItemName, createArgs(), &item.ValidationError{Field: "recurrence", Code: "invalid"}, identityhttp.MCPInvalidArgument("recurrence").Error()},
		{"update date overflow", UpdateItemName, updateArgs(), &item.ValidationError{Field: "recurrence", Code: "date_overflow"}, ErrMCPToolDateOverflow.Error()},
		{"create subject reference", CreateItemName, createArgs(), &item.ValidationError{Field: "subjectId", Code: "invalid_reference"}, identityhttp.ErrMCPToolNotFound.Error()},
		{"create responsible reference", CreateItemName, createArgs(), &item.ValidationError{Field: "responsibleUserId", Code: "invalid_reference"}, identityhttp.ErrMCPToolNotFound.Error()},
		{"create raw subject reference", CreateItemName, createArgs(), item.ErrInvalidReference, identityhttp.ErrMCPToolNotFound.Error()},
		{"create raw responsible reference", CreateItemName, createArgs(), item.ErrInvalidResponsibleReference, identityhttp.ErrMCPToolNotFound.Error()},
		{"create bad key", CreateItemName, createArgs(), &item.ValidationError{Code: "invalid_idempotency_key"}, identityhttp.ErrMCPToolBadKey.Error()},
		{"create driver text", CreateItemName, createArgs(), driver, identityhttp.ErrMCPToolUnavailable.Error()},
		{"create unavailable", CreateItemName, createArgs(), item.ErrUnavailable, identityhttp.ErrMCPToolUnavailable.Error()},
		{"update not found", UpdateItemName, updateArgs(), item.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{"update version changed", UpdateItemName, updateArgs(), item.ErrVersionMismatch, identityhttp.ErrMCPToolVersionChanged.Error()},
		{"update no change", UpdateItemName, updateArgs(), item.ErrEmptyPatch, identityhttp.ErrMCPToolNoChange.Error()},
		{"update archived", UpdateItemName, updateArgs(), item.ErrArchived, ErrMCPToolArchived.Error()},
		{"update workflow refused", UpdateItemName, updateArgs(), &item.ValidationError{Field: "workflowState", Code: "invalid"}, identityhttp.MCPInvalidArgument("workflowState").Error()},
		{"update attentionOn refused", UpdateItemName, updateArgs(), &item.ValidationError{Field: "attentionOn", Code: "invalid_date"}, identityhttp.MCPInvalidArgument("attentionOn").Error()},
		{"update create-only field unmapped", UpdateItemName, updateArgs(), &item.ValidationError{Field: "historicalCompletedOn", Code: "requires_recurrence"}, identityhttp.ErrMCPToolUnavailable.Error()},
		{"update responsible reference", UpdateItemName, updateArgs(), item.ErrInvalidResponsibleReference, identityhttp.ErrMCPToolNotFound.Error()},
		{"update driver text", UpdateItemName, updateArgs(), driver, identityhttp.ErrMCPToolUnavailable.Error()},
		{"complete not found", CompleteItemName, completeArgs(), item.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{"complete version changed", CompleteItemName, completeArgs(), item.ErrVersionMismatch, identityhttp.ErrMCPToolVersionChanged.Error()},
		{"complete key reused", CompleteItemName, completeArgs(), item.ErrIdempotencyKeyReused, identityhttp.ErrMCPToolKeyReused.Error()},
		{"complete archived", CompleteItemName, completeArgs(), item.ErrArchived, ErrMCPToolArchived.Error()},
		{"complete done", CompleteItemName, completeArgs(), item.ErrDone, ErrMCPToolDone.Error()},
		{"complete future date", CompleteItemName, completeArgs(), &item.ValidationError{Field: "completedOn", Code: "future_date"}, identityhttp.MCPInvalidArgument("completedOn").Error()},
		{"complete bad key", CompleteItemName, completeArgs(), &item.ValidationError{Code: "invalid_idempotency_key"}, identityhttp.ErrMCPToolBadKey.Error()},
		{"complete date overflow", CompleteItemName, completeArgs(), &item.ValidationError{Field: "recurrence", Code: "date_overflow"}, ErrMCPToolDateOverflow.Error()},
		{"complete driver text", CompleteItemName, completeArgs(), driver, identityhttp.ErrMCPToolUnavailable.Error()},
		{"undo not found", UndoCompletionName, undoArgs(), item.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{"undo version changed", UndoCompletionName, undoArgs(), item.ErrVersionMismatch, identityhttp.ErrMCPToolVersionChanged.Error()},
		{"undo archived", UndoCompletionName, undoArgs(), item.ErrArchived, ErrMCPToolArchived.Error()},
		{"undo not latest", UndoCompletionName, undoArgs(), item.ErrCompletionNotLatest, ErrMCPToolNotLatest.Error()},
		{"undo superseded", UndoCompletionName, undoArgs(), item.ErrCompletionSuperseded, ErrMCPToolSuperseded.Error()},
		{"undo unavailable", UndoCompletionName, undoArgs(), item.ErrUnavailable, identityhttp.ErrMCPToolUnavailable.Error()},
		{"undo driver text", UndoCompletionName, undoArgs(), driver, identityhttp.ErrMCPToolUnavailable.Error()},
	}
	for _, tc := range cases {
		fake := &fakeWriter{err: tc.err}
		if _, msg := writeWith(t, fake, tc.tool, tc.args); msg != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, msg, tc.want)
		}
	}
}

func TestInvalidReferenceTextIsByteIdenticalToNotFound(t *testing.T) {
	for _, field := range []string{"subjectId", "responsibleUserId"} {
		fake := &fakeWriter{err: &item.ValidationError{Field: field, Code: "invalid_reference"}}
		_, msg := writeWith(t, fake, CreateItemName, createArgs())
		if msg != identityhttp.ErrMCPToolNotFound.Error() {
			t.Fatalf("%s invalid_reference: got %q", field, msg)
		}
	}
}
