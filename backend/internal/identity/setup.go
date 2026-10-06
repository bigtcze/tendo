package identity

import (
	"context"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

var loginPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

const setupTimeout = 5 * time.Second

type SetupInput struct{ Login, Password, HouseholdName, Timezone string }
type SetupPersistenceInput struct{ Login, PasswordHash string }
type SetupTransaction interface {
	LockRequired(context.Context) (bool, error)
	CreateUser(context.Context, string) (string, error)
	CreateCredential(context.Context, string, string) error
	SetDefaultHousehold(context.Context, string, string) error
	CompleteSetup(context.Context) error
}
type SetupRepository interface {
	IsRequired(context.Context) (bool, error)
	WithSetupTransaction(context.Context, func(SetupTransaction, OwnerHouseholdService) error) error
}
type OwnerHouseholdService interface {
	CreateOwnerHousehold(context.Context, household.Bootstrap, string) (string, error)
}
type SetupService struct{ repository SetupRepository }

func NewSetupService(repository SetupRepository) *SetupService {
	return &SetupService{repository: repository}
}

var ErrComplete = errors.New("setup is already complete")

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

func (s *SetupService) Required(ctx context.Context) (bool, error) {
	bounded, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	return s.repository.IsRequired(bounded)
}

func (s *SetupService) CreateOwner(ctx context.Context, input SetupInput) error {
	if !loginPattern.MatchString(input.Login) {
		return &ValidationError{"login", "invalid_format"}
	}
	if !utf8.ValidString(input.Password) || utf8.RuneCountInString(input.Password) < 15 || utf8.RuneCountInString(input.Password) > 128 || len(input.Password) > 512 {
		return &ValidationError{"password", "invalid_length"}
	}
	name, err := household.Validate(input.HouseholdName, input.Timezone)
	if err != nil {
		var validation *household.ValidationError
		if errors.As(err, &validation) {
			return &ValidationError{validation.Field, validation.Code}
		}
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return err
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		return errors.New("password hashing failed")
	}
	if err := bounded.Err(); err != nil {
		return err
	}
	persistence := SetupPersistenceInput{Login: input.Login, PasswordHash: hash}
	bootstrap := household.Bootstrap{Name: name, Timezone: input.Timezone}
	return s.repository.WithSetupTransaction(bounded, func(tx SetupTransaction, households OwnerHouseholdService) error {
		required, err := tx.LockRequired(bounded)
		if err != nil {
			return errors.New("setup state unavailable")
		}
		if !required {
			return ErrComplete
		}
		userID, err := tx.CreateUser(bounded, persistence.Login)
		if err != nil {
			return errors.New("setup persistence failed")
		}
		if err = tx.CreateCredential(bounded, userID, persistence.PasswordHash); err != nil {
			return errors.New("setup persistence failed")
		}
		householdID, err := households.CreateOwnerHousehold(bounded, bootstrap, userID)
		if err != nil {
			return errors.New("setup persistence failed")
		}
		if err = tx.SetDefaultHousehold(bounded, userID, householdID); err != nil {
			return errors.New("setup persistence failed")
		}
		if err = tx.CompleteSetup(bounded); err != nil {
			return errors.New("setup persistence failed")
		}
		return nil
	})
}
