package identity

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

type setupRepo struct {
	hash               string
	passwordInput      SetupPersistenceInput
	bootstrapHousehold string
	called             bool
}
type setupTx struct{ repo *setupRepo }

func (tx setupTx) LockRequired(context.Context) (bool, error) { return true, nil }
func (tx setupTx) CreateUser(_ context.Context, login string) (string, error) {
	tx.repo.called = true
	return login, nil
}
func (tx setupTx) CreateCredential(_ context.Context, userID, hash string) error {
	tx.repo.passwordInput = SetupPersistenceInput{Login: userID, PasswordHash: hash}
	tx.repo.hash = hash
	return nil
}
func (tx setupTx) SetDefaultHousehold(context.Context, string, string) error { return nil }
func (tx setupTx) CompleteSetup(context.Context) error                       { return nil }

type setupHousehold struct{ repo *setupRepo }

func (h setupHousehold) CreateOwnerHousehold(_ context.Context, input household.Bootstrap, _ string) (string, error) {
	h.repo.bootstrapHousehold = input.Name
	return "household", nil
}
func (r *setupRepo) IsRequired(ctx context.Context) (bool, error) { return true, ctx.Err() }
func (r *setupRepo) WithSetupTransaction(_ context.Context, work func(SetupTransaction, OwnerHouseholdService) error) error {
	return work(setupTx{r}, setupHousehold{r})
}

func TestCreateOwnerValidatesAndHashesExactPassword(t *testing.T) {
	r := &setupRepo{}
	s := NewSetupService(r)
	in := SetupInput{Login: "owner_1", Password: "correct horse батарея", HouseholdName: " Дом семьи ", Timezone: "Europe/Prague"}
	if err := s.CreateOwner(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !r.called || r.bootstrapHousehold != "Дом семьи" {
		t.Fatalf("household=%q called=%v", r.bootstrapHousehold, r.called)
	}
	if r.hash == in.Password || r.passwordInput.PasswordHash == in.Password {
		t.Fatal("plaintext password persisted")
	}
	if _, ok := reflect.TypeOf(SetupPersistenceInput{}).FieldByName("Password"); ok {
		t.Fatal("persistence input exposes plaintext password")
	}
	if ok, err := security.VerifyPassword(r.hash, in.Password); err != nil || !ok {
		t.Fatalf("hash verification: %v %v", ok, err)
	}
}

func TestSetupValidation(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"login", "Owner"}, {"login", "ab"}, {"password", "too short"}, {"timezone", "+01:00"}, {"timezone", "Local"}, {"timezone", "../etc/passwd"}, {"timezone", ""}, {"household", "bad\nname"}, {"household", "\xff"}} {
		t.Run(tc.value, func(t *testing.T) {
			r := &setupRepo{}
			s := NewSetupService(r)
			in := SetupInput{Login: "valid_login", Password: "correct horse battery", HouseholdName: "House", Timezone: "UTC"}
			switch tc.field {
			case "login":
				in.Login = tc.value
			case "password":
				in.Password = tc.value
			case "timezone":
				in.Timezone = tc.value
			default:
				in.HouseholdName = tc.value
			}
			var validation *ValidationError
			if err := s.CreateOwner(context.Background(), in); !errors.As(err, &validation) {
				t.Fatalf("expected validation, got %v", err)
			}
			if r.called {
				t.Fatal("invalid input persisted")
			}
		})
	}
}

func TestCreateOwnerExpiredContextDoesNotAcquireTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &setupRepo{}
	err := NewSetupService(r).CreateOwner(ctx, SetupInput{Login: "owner_1", Password: "correct horse battery", HouseholdName: "House", Timezone: "UTC"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateOwner error=%v", err)
	}
	if r.called {
		t.Fatal("expired context acquired a transaction")
	}
}

func TestRequiredBoundsRepositoryContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewSetupService(&setupRepo{}).Required(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Required error=%v", err)
	}
}

type blockingSetupRepo struct{}

func (blockingSetupRepo) IsRequired(ctx context.Context) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}
func (blockingSetupRepo) WithSetupTransaction(context.Context, func(SetupTransaction, OwnerHouseholdService) error) error {
	return nil
}

func TestRequiredHasServiceTimeout(t *testing.T) {
	start := time.Now()
	_, err := NewSetupService(blockingSetupRepo{}).Required(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > setupTimeout+time.Second {
		t.Fatalf("Required error=%v duration=%s", err, time.Since(start))
	}
}
