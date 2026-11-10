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
	EnsureMemberPerson(context.Context, string, string, string) error
	CompleteSetup(context.Context) error
}
type SetupRepository interface {
	IsRequired(context.Context) (bool, error)
	WithSetupTransaction(context.Context, func(SetupTransaction, OwnerHouseholdService) error) error
}
type OwnerHouseholdService interface {
	CreateOwnerHousehold(context.Context, household.Bootstrap, string) (string, error)
}
type SetupService struct {
	repository   SetupRepository
	hashPassword func(string) (string, error)
}

func NewSetupService(repository SetupRepository) *SetupService {
	return &SetupService{repository: repository, hashPassword: security.HashPassword}
}
func (s *SetupService) SetPasswordHasher(hasher func(string) (string, error)) {
	if hasher != nil {
		s.hashPassword = hasher
	}
}

var ErrComplete = errors.New("setup is already complete")

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

func (s *SetupService) Required(ctx context.Context) (bool, error) {
	bounded, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	return s.repository.IsRequired(bounded)
}

func validateCredentials(login, password string) error {
	if !loginPattern.MatchString(login) {
		return &ValidationError{"login", "invalid_format"}
	}
	return validatePassword(password)
}

// validatePassword applies the shared local-password policy. It is the single
// source of the 15-128 character rule for setup, invitation acceptance and
// password reset.
func validatePassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 15 || utf8.RuneCountInString(password) > 128 || len(password) > 512 {
		return &ValidationError{"password", "invalid_length"}
	}
	return nil
}

func (s *SetupService) CreateOwner(ctx context.Context, input SetupInput) error {
	persistence, bootstrap, err := setupInputs(input)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return err
	}
	hash, err := s.hashSetupPassword(bounded, input.Password)
	if err != nil {
		return err
	}
	persistence.PasswordHash = hash
	return s.repository.WithSetupTransaction(bounded, func(tx SetupTransaction, households OwnerHouseholdService) error {
		return persistSetupOwner(bounded, tx, households, persistence, bootstrap)
	})
}

// setupInputs validates the owner input before any persistence work and
// converts household validation errors to the identity wire representation.
func setupInputs(input SetupInput) (SetupPersistenceInput, household.Bootstrap, error) {
	if err := validateCredentials(input.Login, input.Password); err != nil {
		return SetupPersistenceInput{}, household.Bootstrap{}, err
	}
	name, err := household.Validate(input.HouseholdName, input.Timezone)
	if err != nil {
		var validation *household.ValidationError
		if errors.As(err, &validation) {
			return SetupPersistenceInput{}, household.Bootstrap{}, &ValidationError{validation.Field, validation.Code}
		}
		return SetupPersistenceInput{}, household.Bootstrap{}, err
	}
	return SetupPersistenceInput{Login: input.Login}, household.Bootstrap{Name: name, Timezone: input.Timezone}, nil
}

// hashSetupPassword owns the password-gate error mapping and keeps the setup
// deadline checked immediately after the slow hash.
func (s *SetupService) hashSetupPassword(ctx context.Context, password string) (string, error) {
	hash, err := s.hashPassword(password)
	if errors.Is(err, ErrPasswordWorkLimit) {
		return "", ErrPasswordWorkLimit
	}
	if err != nil {
		return "", errors.New("password hashing failed")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hash, nil
}

// persistSetupOwner writes the whole first-run graph in the caller's
// transaction: setup lock, account, credential, household, admin membership,
// canonical member person, default household and completion.
func persistSetupOwner(ctx context.Context, tx SetupTransaction, households OwnerHouseholdService, persistence SetupPersistenceInput, bootstrap household.Bootstrap) error {
	required, err := tx.LockRequired(ctx)
	if err != nil {
		return errors.New("setup state unavailable")
	}
	if !required {
		return ErrComplete
	}
	userID, err := tx.CreateUser(ctx, persistence.Login)
	if err != nil {
		return errors.New("setup persistence failed")
	}
	if err = tx.CreateCredential(ctx, userID, persistence.PasswordHash); err != nil {
		return errors.New("setup persistence failed")
	}
	householdID, err := households.CreateOwnerHousehold(ctx, bootstrap, userID)
	if err != nil {
		return errors.New("setup persistence failed")
	}
	if err = tx.EnsureMemberPerson(ctx, householdID, userID, persistence.Login); err != nil {
		return errors.New("setup persistence failed")
	}
	if err = tx.SetDefaultHousehold(ctx, userID, householdID); err != nil {
		return errors.New("setup persistence failed")
	}
	if err = tx.CompleteSetup(ctx); err != nil {
		return errors.New("setup persistence failed")
	}
	return nil
}
