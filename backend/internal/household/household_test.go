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
}

func (f *fakeRepository) FindForMember(ctx context.Context, userID, householdID string) (Household, error) {
	f.calls++
	f.gotHouse, f.gotUser = householdID, userID
	_, f.hasTimeout = ctx.Deadline()
	return f.result, f.err
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
	repo := &fakeRepository{result: Household{ID: validHousehold}}
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
	want := Household{ID: validHousehold, Name: "Veselí 家族", Timezone: "Europe/Prague", CreatedAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC), Version: 7}
	repo := &fakeRepository{result: want}
	got, err := NewService(repo).Get(context.Background(), validUser, validHousehold)
	if err != nil || got != want {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if !repo.hasTimeout {
		t.Fatal("repository context has no deadline")
	}
}
