package household

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	calls      int
	gotHouse   string
	gotUser    string
	result     Household
	err        error
	hasTimeout bool
	listCalls  int
	gotAfter   string
	gotLimit   int
	listResult []ListedHousehold
	listNext   string
	listErr    error
}

func (f *fakeRepository) FindForMember(ctx context.Context, userID, householdID string) (Household, error) {
	f.calls++
	f.gotHouse, f.gotUser = householdID, userID
	_, f.hasTimeout = ctx.Deadline()
	return f.result, f.err
}

func (f *fakeRepository) ListForMember(ctx context.Context, userID, afterID string, limit int) ([]ListedHousehold, string, error) {
	f.listCalls++
	f.gotUser, f.gotAfter, f.gotLimit = userID, afterID, limit
	_, f.hasTimeout = ctx.Deadline()
	return f.listResult, f.listNext, f.listErr
}

const (
	validHousehold = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	validUser      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
)

func TestGetRejectsMalformedIdentifiersWithoutRepositoryCall(t *testing.T) {
	for name, id := range map[string]string{
		"empty":            "",
		"too short":        "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b6",
		"too long":         validHousehold + "0",
		"braces":           "{" + validHousehold + "}",
		"urn":              "urn:uuid:" + validHousehold,
		"no hyphens":       "0198a2f07c1e7a539b0e5d3f2c1a4b61",
		"misplaced hyphen": "0198a2f-07c1e-7a53-9b0e-5d3f2c1a4b61",
		"non hex":          "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b6g",
		"whitespace":       " " + validHousehold[:35],
		"path traversal":   "../../../../../etc/passwd/../../../x",
		"sql":              "' OR 1=1 --0000000000000000000000000",
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{result: Household{ID: validHousehold}}
			_, err := NewService(repo).Get(context.Background(), validUser, id)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err=%v, want ErrNotFound", err)
			}
			if repo.calls != 0 {
				t.Fatalf("repository called %d times for malformed id", repo.calls)
			}
		})
	}
}

func TestGetRejectsMalformedUserWithoutRepositoryCall(t *testing.T) {
	repo := &fakeRepository{}
	if _, err := NewService(repo).Get(context.Background(), "not-a-uuid", validHousehold); !errors.Is(err, ErrNotFound) || repo.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, repo.calls)
	}
}

func TestGetAcceptsUppercaseUUID(t *testing.T) {
	repo := &fakeRepository{result: Household{ID: validHousehold, ViewerRole: RoleMember}}
	upper := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B61"
	if _, err := NewService(repo).Get(context.Background(), validUser, upper); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.gotHouse != upper {
		t.Fatalf("calls=%d household=%q", repo.calls, repo.gotHouse)
	}
}

func TestGetPassesRepositoryNotFoundThrough(t *testing.T) {
	repo := &fakeRepository{err: ErrNotFound}
	got, err := NewService(repo).Get(context.Background(), validUser, validHousehold)
	if !errors.Is(err, ErrNotFound) || got != (Household{}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if repo.gotHouse != validHousehold || repo.gotUser != validUser {
		t.Fatalf("repository received household=%q user=%q", repo.gotHouse, repo.gotUser)
	}
}

func TestGetMapsInfrastructureErrorsToGenericError(t *testing.T) {
	cause := errors.New("pq: connection to 10.0.0.5 refused")
	got, err := NewService(&fakeRepository{err: cause}).Get(context.Background(), validUser, validHousehold)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, cause) || got != (Household{}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err.Error() == cause.Error() {
		t.Fatal("infrastructure error text leaked")
	}
}

func TestGetReturnsRepositoryDataWithBoundedContext(t *testing.T) {
	want := Household{ID: validHousehold, Name: "Veselí 家族", Timezone: "Europe/Prague", CreatedAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC), Version: 7, ViewerRole: RoleAdmin}
	repo := &fakeRepository{result: want}
	got, err := NewService(repo).Get(context.Background(), validUser, validHousehold)
	if err != nil || got != want {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if !repo.hasTimeout {
		t.Fatal("repository context has no deadline")
	}
}

func TestGetFailsClosedOnUnknownViewerRole(t *testing.T) {
	for _, role := range []string{"", "owner", "superuser"} {
		repo := &fakeRepository{result: Household{ID: validHousehold, ViewerRole: role}}
		got, err := NewService(repo).Get(context.Background(), validUser, validHousehold)
		if err == nil || errors.Is(err, ErrNotFound) || got != (Household{}) {
			t.Fatalf("role %q: got=%+v err=%v, want unavailable", role, got, err)
		}
	}
}

func TestListForMemberValidatesCallerAndCursorBeforeRepository(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	if _, _, err := service.ListForMember(context.Background(), "not-a-uuid", "", 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed caller err=%v", err)
	}
	for name, cursor := range map[string]string{
		"wrong prefix":       "m1MDE5OGEyZjAtN2MxZS03YTUzLTliMGUtNWQzZjJjMWE0YjYx",
		"invalid base64":     "h1!!!",
		"non canonical id":   "h1" + "bm90LWEtdXVpZA",
		"short payload":      "h1" + "MDE5OA",
		"empty after prefix": "h1",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := service.ListForMember(context.Background(), validUser, cursor, 50)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != "cursor" {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if repo.listCalls != 0 {
		t.Fatalf("repository called %d times", repo.listCalls)
	}
}

func TestListForMemberLimitDefaultsToFiftyAndClampsToOneHundred(t *testing.T) {
	for name, tc := range map[string]struct {
		limit int
		want  int
	}{"absent": {0, 50}, "negative": {-3, 50}, "over max": {500, 100}, "max": {100, 100}, "one": {1, 1}} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{}
			got, next, err := NewService(repo).ListForMember(context.Background(), validUser, "", tc.limit)
			if err != nil || got != nil || next != "" || repo.gotLimit != tc.want {
				t.Fatalf("limit=%d rows=%+v next=%q err=%v", repo.gotLimit, got, next, err)
			}
		})
	}
}

func TestListForMemberDecodesCursorAndBoundsContext(t *testing.T) {
	repo := &fakeRepository{listResult: []ListedHousehold{{ID: validHousehold, Name: "Home", Timezone: "UTC", CreatedAt: time.Now().UTC(), ViewerRole: RoleMember}}}
	cursor := EncodeHouseholdCursor(validHousehold)
	got, next, err := NewService(repo).ListForMember(context.Background(), validUser, cursor, 10)
	if err != nil || len(got) != 1 || next != "" {
		t.Fatalf("got=%+v next=%q err=%v", got, next, err)
	}
	if repo.gotAfter != validHousehold || repo.gotUser != validUser {
		t.Fatalf("after=%q user=%q", repo.gotAfter, repo.gotUser)
	}
	if !repo.hasTimeout {
		t.Fatal("repository context has no deadline")
	}
}

func TestListForMemberFailsClosedOnUnknownRole(t *testing.T) {
	for _, role := range []string{"", "owner", "superuser"} {
		repo := &fakeRepository{listResult: []ListedHousehold{{ID: validHousehold, ViewerRole: role}}}
		got, _, err := NewService(repo).ListForMember(context.Background(), validUser, "", 50)
		if err == nil || errors.Is(err, ErrNotFound) || got != nil {
			t.Fatalf("role %q: got=%+v err=%v, want unavailable", role, got, err)
		}
	}
}

func TestListForMemberMapsRepositoryFailures(t *testing.T) {
	repo := &fakeRepository{listErr: errors.New("pool exhausted at 10.1.2.3")}
	got, _, err := NewService(repo).ListForMember(context.Background(), validUser, "", 50)
	if err == nil || errors.Is(err, ErrNotFound) || got != nil || err.Error() == "pool exhausted at 10.1.2.3" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	repo = &fakeRepository{listErr: ErrNotFound}
	if _, _, err := NewService(repo).ListForMember(context.Background(), validUser, "", 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}
