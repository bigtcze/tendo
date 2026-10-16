package subject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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
	calls        int
	creationIDs  map[string]string
	findCalls    int
	err          error
	createErr    error
	updateErr    error
	rows         []Subject
	creations    map[string]Subject
	fingerprints map[string][]byte
	lastList     struct {
		after string
		limit int
	}
}

func (m *memRepo) CreateIdempotent(_ context.Context, hid, actor, key, name string, t Type, linkedUserID *string, fingerprint []byte) (Subject, bool, error) {
	m.calls++
	if m.err != nil {
		return Subject{}, false, m.err
	}
	if m.createErr != nil {
		return Subject{}, false, m.createErr
	}
	if linkedUserID != nil && t != TypePerson {
		return Subject{}, false, &ValidationError{Field: "linkedUserId", Code: "requires_person"}
	}
	if m.creations == nil {
		m.creations = map[string]Subject{}
		m.creationIDs = map[string]string{}
		m.fingerprints = map[string][]byte{}
	}
	creationKey := hid + "/" + actor + "/" + key
	if existing, ok := m.creations[creationKey]; ok {
		if !bytes.Equal(m.fingerprints[creationKey], fingerprint) {
			return Subject{}, false, nil
		}
		for _, current := range m.rows {
			if current.ID == m.creationIDs[creationKey] {
				return current, false, nil
			}
		}
		return existing, false, nil
	}
	s := Subject{ID: fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b%02d", len(m.rows)+70), HouseholdID: hid, Type: t, Name: name, LinkedUserID: linkedUserID, Version: 1, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}
	m.rows = append(m.rows, s)
	m.creations[creationKey] = s
	m.creationIDs[creationKey] = s.ID
	m.fingerprints[creationKey] = append([]byte(nil), fingerprint...)
	return s, true, nil
}

func (m *memRepo) FindCreation(_ context.Context, hid, actor, key string) (Subject, []byte, error) {
	m.findCalls++
	if m.err != nil {
		return Subject{}, nil, m.err
	}
	creationKey := hid + "/" + actor + "/" + key
	s, ok := m.creations[creationKey]
	if !ok {
		return Subject{}, nil, ErrNotFound
	}
	for _, current := range m.rows {
		if current.ID == s.ID {
			s = current
			break
		}
	}
	return s, append([]byte(nil), m.fingerprints[creationKey]...), nil
}

func (m *memRepo) Row(id string) (Subject, bool) {
	for _, row := range m.rows {
		if row.ID == id {
			return row, true
		}
	}
	return Subject{}, false
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
	if m.updateErr != nil {
		return Subject{}, m.updateErr
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
		if p.LinkedUserIDSet {
			r.LinkedUserID = p.LinkedUserID
		}
		if r.LinkedUserID != nil && r.Type != TypePerson {
			return Subject{}, &ValidationError{Field: "linkedUserId", Code: "requires_person"}
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
	svc := NewService(repo, allow, nil)
	created, err := svc.Create(context.Background(), userID, householdID, "key-1", "  Rex 🐕  ", TypePet, nil)
	if err != nil || created.Name != "Rex 🐕" || created.Type != TypePet || created.Version != 1 || len(repo.rows) != 1 || repo.rows[0].Name != "Rex 🐕" {
		t.Fatalf("created=%+v err=%v rows=%+v", created, err, repo.rows)
	}
	var v *ValidationError
	if _, err = svc.Create(context.Background(), userID, householdID, "key-2", "ok", Type("robot"), nil); !errors.As(err, &v) || v.Field != "type" || v.Code != "invalid_type" {
		t.Fatalf("type err=%v", err)
	}
	if _, err = svc.Create(context.Background(), userID, householdID, "key-3", " ", TypeHome, nil); !errors.As(err, &v) || v.Field != "name" {
		t.Fatalf("name err=%v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("invalid input persisted: %+v", repo.rows)
	}
}

func TestCreateFingerprintReplayAndScope(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo, allow, nil)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID, householdID, "same", "  Name ", TypeHome, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Create(ctx, userID, householdID, "same", "Name", TypeHome, nil)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("trim replay=%+v err=%v", replay, err)
	}
	for _, tc := range []struct {
		actor, hid, name, key string
		typ                   Type
	}{{userID, householdID, "Other", "field-name", TypeHome}, {userID, householdID, "Name", "field-type", TypePet}, {"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62", householdID, "Name", "actor", TypeHome}, {userID, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63", "Name", "household", TypeHome}} {
		if bytes.Equal(creationFingerprint(userID, "Name", TypeHome, nil), creationFingerprint(tc.actor, tc.name, tc.typ, nil)) && tc.actor != userID {
			t.Fatal("actor missing from fingerprint")
		}
		got, e := svc.Create(ctx, tc.actor, tc.hid, tc.key, tc.name, tc.typ, nil)
		if e != nil || got.ID == first.ID && (got.Name != "Other" || tc.typ != TypeHome) {
			t.Fatalf("independent scope got=%+v err=%v", got, e)
		}
	}
	name := "Renamed"
	_, err = svc.Update(ctx, userID, householdID, first.ID, 1, Patch{Name: &name, Archived: new(bool)})
	if err != nil {
		t.Fatal(err)
	}
	archived := true
	_, err = svc.Update(ctx, userID, householdID, first.ID, 2, Patch{Archived: &archived})
	if err != nil {
		t.Fatal(err)
	}
	current, err := svc.Create(ctx, userID, householdID, "same", "Name", TypeHome, nil)
	if err != nil || current.Name != "Renamed" || !current.Archived || current.Version != 3 {
		t.Fatalf("current replay=%+v err=%v", current, err)
	}
	deniedActor := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64"
	membership := true
	badSvc := NewService(repo, func(_ context.Context, actor, hid string) error {
		if actor == deniedActor && !membership {
			return ErrNotFound
		}
		return nil
	}, nil)
	createdByActor, err := badSvc.Create(ctx, deniedActor, householdID, "member-replay", "Actor subject", TypeHome, nil)
	if err != nil {
		t.Fatal(err)
	}
	findCalls := repo.findCalls
	creationBefore := repo.creations[householdID+"/"+deniedActor+"/member-replay"]
	membership = false
	if _, err = badSvc.Create(ctx, deniedActor, householdID, "member-replay", "Actor subject", TypeHome, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("denied replay=%v", err)
	}
	if repo.findCalls != findCalls {
		t.Fatalf("membership denial ran lookup: before=%d after=%d", findCalls, repo.findCalls)
	}
	storedRow, _ := repo.Row(createdByActor.ID)
	if storedRow != createdByActor {
		t.Fatalf("denied replay changed stored row: before=%+v after=%+v", createdByActor, storedRow)
	}
	if repo.creations[householdID+"/"+deniedActor+"/member-replay"] != creationBefore || createdByActor.ID == "" {
		t.Fatal("denied replay changed creation record")
	}
	if _, err = svc.Create(ctx, userID, householdID, "invalid", " ", TypeHome, nil); err == nil {
		t.Fatal("invalid create accepted")
	}
	valid, err := svc.Create(ctx, userID, householdID, "invalid", "Valid", TypeHome, nil)
	if err != nil || valid.ID == "" {
		t.Fatalf("invalid key was reserved: %+v %v", valid, err)
	}
	if _, err = svc.Create(ctx, userID, householdID, "same", " ", TypeHome, nil); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("stored key with invalid changed name: %v", err)
	}
	for name, pair := range map[string][2][]byte{"actor": {creationFingerprint(userID, "n", TypeHome, nil), creationFingerprint("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b65", "n", TypeHome, nil)}, "name": {creationFingerprint(userID, "n", TypeHome, nil), creationFingerprint(userID, "m", TypeHome, nil)}, "type": {creationFingerprint(userID, "n", TypeHome, nil), creationFingerprint(userID, "n", TypePet, nil)}} {
		if bytes.Equal(pair[0], pair[1]) {
			t.Fatalf("%s omitted from fingerprint", name)
		}
	}
	if len(creationFingerprint(userID, "n", TypeHome, nil)) != 32 {
		t.Fatal("fingerprint length is not SHA-256")
	}
	oldV1 := func(actor, name string, t Type) []byte {
		var encoded []byte
		for _, field := range []string{"tendo.subject.create.v1", actor, name, string(t)} {
			var size [4]byte
			binary.BigEndian.PutUint32(size[:], uint32(len(field)))
			encoded = append(encoded, size[:]...)
			encoded = append(encoded, field...)
		}
		digest := sha256.Sum256(encoded)
		return digest[:]
	}
	if !bytes.Equal(creationFingerprint(userID, "n", TypeHome, nil), oldV1(userID, "n", TypeHome)) {
		t.Fatal("nil link changed v1 fingerprint")
	}
}

func TestMembersLookupReceivesOperationDeadline(t *testing.T) {
	id := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	calls := 0
	members := func(ctx context.Context, _, _ string) error {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("Members lookup context has no deadline")
		}
		return nil
	}
	for _, update := range []bool{false, true} {
		repo := &memRepo{}
		svc := NewService(repo, allow, members)
		if update {
			linked := &id
			repo.rows = []Subject{{ID: subjectID, HouseholdID: householdID, Type: TypePerson, Name: "Child", LinkedUserID: linked, Version: 1}}
			if _, err := svc.Update(context.Background(), userID, householdID, subjectID, 1, Patch{LinkedUserID: &id, LinkedUserIDSet: true}); err != nil {
				t.Fatalf("Update: %v", err)
			}
		} else if _, err := svc.Create(context.Background(), userID, householdID, "deadline", "Child", TypePerson, &id); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("Members calls=%d, want Create and Update", calls)
	}
}

func TestLinkedUserChangeReusesIdempotencyKeyAsDifferentIntent(t *testing.T) {
	repo := &memRepo{}
	memberLookup := func(context.Context, string, string) error { return nil }
	svc := NewService(repo, allow, memberLookup)
	firstLink := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	secondLink := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"
	if _, err := svc.Create(context.Background(), userID, householdID, "link-key", "Child", TypePerson, &firstLink); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), userID, householdID, "link-key", "Child", TypePerson, &secondLink); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("different link replay err=%v", err)
	}
	uppercase := strings.ToUpper(firstLink)
	if _, err := svc.Create(context.Background(), userID, householdID, "link-key", "Child", TypePerson, &uppercase); err != nil {
		t.Fatalf("uppercase replay err=%v", err)
	}
	if _, err := svc.Create(context.Background(), userID, householdID, "link-key", "Child", TypePerson, nil); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("link-to-null replay err=%v", err)
	}
}

func TestLinkedUserReplayAfterEditSkipsMembershipLookup(t *testing.T) {
	repo := &memRepo{}
	candidate := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	lookupFails := false
	members := func(context.Context, string, string) error {
		if lookupFails {
			return errors.New("membership service unavailable")
		}
		return nil
	}
	svc := NewService(repo, allow, members)
	first, err := svc.Create(context.Background(), userID, householdID, "linked-replay", "Child", TypePerson, &candidate)
	if err != nil {
		t.Fatal(err)
	}
	vehicle := TypeVehicle
	current, err := svc.Update(context.Background(), userID, householdID, first.ID, 1, Patch{Type: &vehicle, LinkedUserIDSet: true})
	if err != nil || current.LinkedUserID != nil {
		t.Fatalf("unlink and retype=%+v err=%v", current, err)
	}
	calls, count := repo.findCalls, len(repo.rows)
	lookupFails = true
	replayed, err := svc.Create(context.Background(), userID, householdID, "linked-replay", "Child", TypePerson, &candidate)
	if err != nil || replayed.Version != 2 || replayed.Type != TypeVehicle || replayed.LinkedUserID != nil || repo.findCalls != calls+1 || len(repo.rows) != count {
		t.Fatalf("current replay=%+v err=%v calls=%d rows=%d", replayed, err, repo.findCalls, len(repo.rows))
	}
}

func TestLinkedUserValidationAndPatchSemantics(t *testing.T) {
	candidate := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B62"
	foreign := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"
	memberLookup := func(_ context.Context, id, hid string) error {
		if hid != householdID || id == foreign {
			return ErrNotFound
		}
		return nil
	}
	repo := &memRepo{}
	svc := NewService(repo, allow, memberLookup)
	created, err := svc.Create(context.Background(), userID, householdID, "linked", "Child", TypePerson, &candidate)
	if err != nil || created.LinkedUserID == nil || *created.LinkedUserID != strings.ToLower(candidate) {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	unchanged, err := svc.Update(context.Background(), userID, householdID, created.ID, 1, Patch{Name: ptr("Renamed")})
	if err != nil || unchanged.LinkedUserID == nil || *unchanged.LinkedUserID != strings.ToLower(candidate) {
		t.Fatalf("unchanged=%+v err=%v", unchanged, err)
	}
	unlinked, err := svc.Update(context.Background(), userID, householdID, created.ID, 2, Patch{LinkedUserIDSet: true})
	if err != nil || unlinked.LinkedUserID != nil {
		t.Fatalf("unlinked=%+v err=%v", unlinked, err)
	}
	for _, tc := range []struct {
		id   string
		typ  Type
		code string
	}{{"bad", TypePerson, "invalid_reference"}, {foreign, TypePerson, "invalid_reference"}, {candidate, TypeVehicle, "requires_person"}} {
		_, err = svc.Create(context.Background(), userID, householdID, "bad-"+tc.code+tc.id, "Bad", tc.typ, &tc.id)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != "linkedUserId" || validation.Code != tc.code {
			t.Fatalf("id=%q type=%s err=%v", tc.id, tc.typ, err)
		}
	}
	vehicle := TypeVehicle
	_, err = svc.Update(context.Background(), userID, householdID, created.ID, 3, Patch{Type: &vehicle, LinkedUserIDSet: true})
	if err != nil {
		t.Fatalf("change type and clear link: %v", err)
	}
	outageRepo := &memRepo{}
	outageSvc := NewService(outageRepo, allow, func(context.Context, string, string) error { return errors.New("membership service unavailable") })
	if _, err = outageSvc.Create(context.Background(), userID, householdID, "outage", "Bad", TypePerson, &candidate); !errors.Is(err, ErrUnavailable) || len(outageRepo.rows) != 0 {
		t.Fatalf("outage err=%v rows=%d", err, len(outageRepo.rows))
	}
}

func ptr(v string) *string { return &v }

func TestRepositoryValidationErrorPassesThrough(t *testing.T) {
	cause := &ValidationError{Field: "linkedUserId", Code: "requires_person"}
	repo := &memRepo{createErr: cause}
	svc := NewService(repo, allow, func(context.Context, string, string) error { return nil })
	_, err := svc.Create(context.Background(), userID, householdID, "repo-validation", "person", TypePerson, ptr("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"))
	assertValidationError(t, err, cause)

	repo = &memRepo{updateErr: cause, rows: []Subject{{ID: subjectID, HouseholdID: householdID, Type: TypePerson, Name: "person", Version: 1}}}
	svc = NewService(repo, allow, func(context.Context, string, string) error { return nil })
	_, err = svc.Update(context.Background(), userID, householdID, subjectID, 1, Patch{LinkedUserID: ptr("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"), LinkedUserIDSet: true})
	assertValidationError(t, err, cause)
}

func assertValidationError(t *testing.T, err error, want *ValidationError) {
	t.Helper()
	var got *ValidationError
	if !errors.As(err, &got) || got.Field != want.Field || got.Code != want.Code {
		t.Fatalf("error=%v; want %s/%s", err, want.Field, want.Code)
	}
}

func TestMalformedIdentifiersAreNotFoundWithoutRepositoryOrAuthorizer(t *testing.T) {
	repo := &memRepo{}
	authCalls := 0
	svc := NewService(repo, func(context.Context, string, string) error { authCalls++; return nil }, nil)
	ctx := context.Background()
	patch := Patch{Archived: new(bool)}
	for _, bad := range []string{"", "not-a-uuid", "0198a2f07c1e7a539b0e5d3f2c1a4b70", "{0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70}", "urn:uuid:0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"} {
		if _, err := svc.Create(ctx, userID, bad, "key", "x", TypeHome, nil); !errors.Is(err, ErrNotFound) {
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
	svc := NewService(repo, func(context.Context, string, string) error { return ErrNotFound }, nil)
	ctx := context.Background()
	name := "Hacked"
	if _, err := svc.Create(ctx, userID, householdID, "key", "x", TypeHome, nil); !errors.Is(err, ErrNotFound) {
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
	svc := NewService(repo, func(context.Context, string, string) error { return errors.New("pq: secret") }, nil)
	_, err := svc.Get(context.Background(), userID, householdID, subjectID)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") || repo.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, repo.calls)
	}
}

func TestRepositoryFailureIsUnavailable(t *testing.T) {
	repo := &memRepo{err: errors.New("pq: password authentication failed")}
	svc := NewService(repo, allow, nil)
	ctx := context.Background()
	if _, err := svc.Create(ctx, userID, householdID, "key", "x", TypeHome, nil); !errors.Is(err, ErrUnavailable) {
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
	svc := NewService(repo, allow, nil)
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
	svc := NewService(repo, allow, nil)
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
	svc := NewService(repo, allow, nil)
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
