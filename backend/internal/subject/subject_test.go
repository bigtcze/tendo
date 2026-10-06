package subject

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	userID      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	householdID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	subjectID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
)

// memRepo is a household-scoped in-memory repository with the same observable
// contract as the PostgreSQL adapter.
type memRepo struct {
	calls    int
	err      error
	rows     []Subject
	lastList struct {
		after string
		limit int
	}
}

func (m *memRepo) Create(_ context.Context, hid, name string, t Type) (Subject, error) {
	m.calls++
	if m.err != nil {
		return Subject{}, m.err
	}
	s := Subject{ID: subjectID, HouseholdID: hid, Type: t, Name: name, Version: 1, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}
	m.rows = append(m.rows, s)
	return s, nil
}

func (m *memRepo) Get(_ context.Context, hid, sid string) (Subject, error) {
	m.calls++
	if m.err != nil {
		return Subject{}, m.err
	}
	for _, r := range m.rows {
		if r.HouseholdID == hid && r.ID == sid {
			return r, nil
		}
	}
	return Subject{}, ErrNotFound
}

func (m *memRepo) List(_ context.Context, hid string, archived bool, after string, limit int) ([]Subject, error) {
	m.calls++
	m.lastList.after, m.lastList.limit = after, limit
	if m.err != nil {
		return nil, m.err
	}
	var out []Subject
	for _, r := range m.rows {
		if r.HouseholdID == hid && r.Archived == archived && r.ID > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRepo) Update(_ context.Context, hid, sid string, expected int64, p Patch) (Subject, error) {
	m.calls++
	if m.err != nil {
		return Subject{}, m.err
	}
	for i, r := range m.rows {
		if r.HouseholdID != hid || r.ID != sid {
			continue
		}
		if r.Version != expected {
			return Subject{}, ErrVersionMismatch
		}
		if p.Name != nil {
			r.Name = *p.Name
		}
		if p.Type != nil {
			r.Type = *p.Type
		}
		if p.Archived != nil {
			r.Archived = *p.Archived
		}
		r.Version++
		m.rows[i] = r
		return r, nil
	}
	return Subject{}, ErrNotFound
}

func allow(context.Context, string, string) error { return nil }

func TestValidateName(t *testing.T) {
	for _, tc := range []struct {
		name, input, want, code string
	}{
		{"ascii", "Octavia", "Octavia", ""},
		{"unicode", "  Veselí 家族 🚗 ", "Veselí 家族 🚗", ""},
		{"100 runes", strings.Repeat("家", 100), strings.Repeat("家", 100), ""},
		{"101 runes", strings.Repeat("家", 101), "", "invalid_length"},
		{"padded 100 runes", " " + strings.Repeat("a", 100) + " ", strings.Repeat("a", 100), ""},
		{"empty", "", "", "invalid_length"},
		{"blank", " \t\n ", "", "invalid_length"},
		{"control inside", "a\x00b", "", "invalid_characters"},
		{"newline inside", "a\nb", "", "invalid_characters"},
		{"invalid utf8", "a\xffb", "", "invalid_characters"},
		{"zero width space", "a\u200bb", "", "invalid_characters"},
		{"zero width joiner", "a\u200db", "", "invalid_characters"},
		{"left-to-right mark", "a\u200eb", "", "invalid_characters"},
		{"bidi override", "a\u202eb", "", "invalid_characters"},
		{"bidi isolate", "a\u2066b", "", "invalid_characters"},
		{"bidi isolate pop", "a\u2069b", "", "invalid_characters"},
		{"byte order mark", "\ufeffabc", "", "invalid_characters"},
		{"line separator", "a\u2028b", "", "invalid_characters"},
		{"paragraph separator", "a\u2029b", "", "invalid_characters"},
		{"nbsp only", "\u00a0\u00a0", "", "invalid_length"},
		{"nbsp trimmed", "\u00a0Flat\u00a0", "Flat", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateName(tc.input)
			if tc.code == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got=%q err=%v want=%q", got, err, tc.want)
				}
				return
			}
			var v *ValidationError
			if !errors.As(err, &v) || v.Field != "name" || v.Code != tc.code {
				t.Fatalf("err=%v want name/%s", err, tc.code)
			}
		})
	}
}

func TestParseType(t *testing.T) {
	for _, ok := range []string{"person", "home", "vehicle", "pet", "custom"} {
		if got, valid := ParseType(ok); !valid || string(got) != ok {
			t.Fatalf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"", "Person", "PERSON", "robot", " person", "person "} {
		if _, valid := ParseType(bad); valid {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestCreateStoresTrimmedNameAndRejectsBadInput(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo, allow)
	created, err := svc.Create(context.Background(), userID, householdID, "  Rex 🐕  ", TypePet)
	if err != nil || created.Name != "Rex 🐕" || created.Type != TypePet || created.Version != 1 || len(repo.rows) != 1 || repo.rows[0].Name != "Rex 🐕" {
		t.Fatalf("created=%+v err=%v rows=%+v", created, err, repo.rows)
	}
	var v *ValidationError
	if _, err = svc.Create(context.Background(), userID, householdID, "ok", Type("robot")); !errors.As(err, &v) || v.Field != "type" || v.Code != "invalid_type" {
		t.Fatalf("type err=%v", err)
	}
	if _, err = svc.Create(context.Background(), userID, householdID, " ", TypeHome); !errors.As(err, &v) || v.Field != "name" {
		t.Fatalf("name err=%v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("invalid input persisted: %+v", repo.rows)
	}
}

func TestMalformedIdentifiersAreNotFoundWithoutRepositoryOrAuthorizer(t *testing.T) {
	repo := &memRepo{}
	authCalls := 0
	svc := NewService(repo, func(context.Context, string, string) error { authCalls++; return nil })
	ctx := context.Background()
	patch := Patch{Archived: new(bool)}
	for _, bad := range []string{"", "not-a-uuid", "0198a2f07c1e7a539b0e5d3f2c1a4b70", "{0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70}", "urn:uuid:0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"} {
		if _, err := svc.Create(ctx, userID, bad, "x", TypeHome); !errors.Is(err, ErrNotFound) {
			t.Fatalf("create %q: %v", bad, err)
		}
		if _, err := svc.List(ctx, userID, bad, ListQuery{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("list %q: %v", bad, err)
		}
		if _, err := svc.Get(ctx, userID, bad, subjectID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get household %q: %v", bad, err)
		}
		if _, err := svc.Get(ctx, userID, householdID, bad); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get subject %q: %v", bad, err)
		}
		if _, err := svc.Update(ctx, userID, householdID, bad, 1, patch); !errors.Is(err, ErrNotFound) {
			t.Fatalf("update subject %q: %v", bad, err)
		}
	}
	if repo.calls != 0 || authCalls != 0 {
		t.Fatalf("repo calls=%d auth calls=%d", repo.calls, authCalls)
	}
}

func TestAuthorizationDenialReturnsNotFoundAndLeavesRepositoryUntouched(t *testing.T) {
	repo := &memRepo{rows: []Subject{{ID: subjectID, HouseholdID: householdID, Type: TypeHome, Name: "Flat", Version: 1}}}
	svc := NewService(repo, func(context.Context, string, string) error { return ErrNotFound })
	ctx := context.Background()
	name := "Hacked"
	if _, err := svc.Create(ctx, userID, householdID, "x", TypeHome); !errors.Is(err, ErrNotFound) {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.List(ctx, userID, householdID, ListQuery{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("list: %v", err)
	}
	if _, err := svc.Get(ctx, userID, householdID, subjectID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if _, err := svc.Update(ctx, userID, householdID, subjectID, 1, Patch{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update: %v", err)
	}
	if repo.calls != 0 || len(repo.rows) != 1 || repo.rows[0].Name != "Flat" || repo.rows[0].Version != 1 {
		t.Fatalf("repository touched: calls=%d rows=%+v", repo.calls, repo.rows)
	}
}

func TestAuthorizerInfrastructureFailureIsUnavailable(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo, func(context.Context, string, string) error { return errors.New("pq: secret") })
	_, err := svc.Get(context.Background(), userID, householdID, subjectID)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") || repo.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, repo.calls)
	}
}

func TestRepositoryFailureIsUnavailable(t *testing.T) {
	repo := &memRepo{err: errors.New("pq: password authentication failed")}
	svc := NewService(repo, allow)
	ctx := context.Background()
	if _, err := svc.Create(ctx, userID, householdID, "x", TypeHome); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Get(ctx, userID, householdID, subjectID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("get: %v", err)
	}
	if _, err := svc.List(ctx, userID, householdID, ListQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list: %v", err)
	}
	if _, err := svc.Update(ctx, userID, householdID, subjectID, 1, Patch{Archived: new(bool)}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("update: %v", err)
	}
}

func TestCursorRoundTripAndRejection(t *testing.T) {
	id := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B70"
	cursor := EncodeCursor(id)
	if strings.ContainsAny(cursor, "=+/") || cursor == id {
		t.Fatalf("cursor not opaque base64url without padding: %s", cursor)
	}
	got, err := DecodeCursor(cursor)
	if err != nil || got != strings.ToLower(id) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	b64 := func(s string) string { return strings.TrimRight(base64URL(s), "=") }
	for name, bad := range map[string]string{
		"empty":          "",
		"not base64":     "***",
		"padded":         cursor + "=",
		"raw uuid":       id,
		"wrong version":  b64("s2:" + id),
		"no prefix":      b64(id),
		"bad uuid":       b64("s1:not-a-uuid"),
		"trailing bytes": b64("s1:" + id + "x"),
	} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Fatalf("%s accepted: %q", name, bad)
		}
	}
}

func TestListLimitBoundsAndCursorErrors(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo, allow)
	ctx := context.Background()
	for _, limit := range []int{-1, 101, 1000} {
		var q *InvalidQueryError
		if _, err := svc.List(ctx, userID, householdID, ListQuery{Limit: limit}); !errors.As(err, &q) || q.Parameter != "limit" {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	var q *InvalidQueryError
	if _, err := svc.List(ctx, userID, householdID, ListQuery{Cursor: "garbage!"}); !errors.As(err, &q) || q.Parameter != "cursor" {
		t.Fatalf("cursor: %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("invalid queries reached repository: %d", repo.calls)
	}
	for limit, want := range map[int]int{0: DefaultLimit + 1, 1: 2, 100: 101} {
		if _, err := svc.List(ctx, userID, householdID, ListQuery{Limit: limit}); err != nil || repo.lastList.limit != want {
			t.Fatalf("limit %d fetched %d want %d err=%v", limit, repo.lastList.limit, want, err)
		}
	}
	if repo.lastList.after != nilUUID {
		t.Fatalf("first page after=%s", repo.lastList.after)
	}
}

func TestListPaginatesWithoutGapsOrDuplicates(t *testing.T) {
	repo := &memRepo{}
	var ids []string
	for _, id := range []string{"0198a2f0-0000-7000-8000-000000000001", "0198a2f0-0000-7000-8000-000000000002", "0198a2f0-0000-7000-8000-000000000003", "0198a2f0-0000-7000-8000-000000000004", "0198a2f0-0000-7000-8000-000000000005"} {
		repo.rows = append(repo.rows, Subject{ID: id, HouseholdID: householdID, Name: id, Type: TypeCustom, Version: 1})
		ids = append(ids, id)
	}
	svc := NewService(repo, allow)
	var seen []string
	cursor := ""
	pages := 0
	for {
		page, err := svc.List(context.Background(), userID, householdID, ListQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, item := range page.Items {
			seen = append(seen, item.ID)
		}
		if page.NextCursor == nil {
			if len(page.Items) != 1 {
				t.Fatalf("last page has %d items", len(page.Items))
			}
			break
		}
		if len(page.Items) != 2 {
			t.Fatalf("full page has %d items", len(page.Items))
		}
		cursor = *page.NextCursor
	}
	if pages != 3 || strings.Join(seen, ",") != strings.Join(ids, ",") {
		t.Fatalf("pages=%d seen=%v", pages, seen)
	}
	page, err := svc.List(context.Background(), userID, householdID, ListQuery{Archived: true})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatalf("empty page=%+v err=%v", page, err)
	}
}

func TestUpdateValidatesPatchAndAppliesVersioning(t *testing.T) {
	repo := &memRepo{rows: []Subject{{ID: subjectID, HouseholdID: householdID, Type: TypeHome, Name: "Flat", Version: 1}}}
	svc := NewService(repo, allow)
	ctx := context.Background()
	var v *ValidationError
	blank, long, bad := " ", strings.Repeat("a", 101), Type("robot")
	if _, err := svc.Update(ctx, userID, householdID, subjectID, 1, Patch{}); !errors.Is(err, ErrEmptyPatch) || errors.As(err, &v) {
		t.Fatalf("empty patch must be a plain error, not a validation error: %v", err)
	}
	for name, p := range map[string]Patch{"blank name": {Name: &blank}, "long name": {Name: &long}, "bad type": {Type: &bad}} {
		if _, err := svc.Update(ctx, userID, householdID, subjectID, 1, p); !errors.As(err, &v) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if repo.rows[0].Version != 1 || repo.rows[0].Name != "Flat" {
		t.Fatalf("invalid patch changed state: %+v", repo.rows[0])
	}
	name, archived, typ := "  House  ", true, TypeCustom
	updated, err := svc.Update(ctx, userID, householdID, subjectID, 1, Patch{Name: &name, Type: &typ, Archived: &archived})
	if err != nil || updated.Name != "House" || updated.Type != TypeCustom || !updated.Archived || updated.Version != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if _, err = svc.Update(ctx, userID, householdID, subjectID, 1, Patch{Archived: &archived}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale: %v", err)
	}
	if _, err = svc.Update(ctx, userID, householdID, subjectID, 0, Patch{Archived: &archived}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("zero version on existing subject: %v", err)
	}
	if _, err = svc.Update(ctx, userID, householdID, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98", 0, Patch{Archived: &archived}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero version on missing subject must be not found: %v", err)
	}
	if _, err = svc.Update(ctx, userID, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98", 2, Patch{Archived: &archived}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if repo.rows[0].Version != 2 {
		t.Fatalf("version=%d", repo.rows[0].Version)
	}
}
