// Package subject holds the household-scoped subject domain and application
// service. It does not import other modules or adapters; membership checks are
// injected through Authorizer.
package subject

import (
	"context"
	"errors"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"strings"
	"time"
)

var (
	// ErrNotFound covers malformed identifiers, nonexistent subjects or
	// households, other households' subjects, and non-members. The cases are
	// deliberately indistinguishable.
	ErrNotFound = errors.New("subject not found")
	// ErrVersionMismatch means the subject exists but the expected version is stale.
	ErrVersionMismatch = errors.New("subject version mismatch")
	// ErrUnavailable is an infrastructure failure without internal detail.
	ErrUnavailable = errors.New("subject unavailable")
	// ErrIdempotencyKeyReused means the key was committed for another intent.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused")
	// ErrEmptyPatch means an update named no field. It is a malformed request,
	// not a field validation failure.
	ErrEmptyPatch = errors.New("empty patch")
)

const (
	DefaultLimit = 50
	MaxLimit     = 100

	operationTimeout = 5 * time.Second
	cursorPrefix     = "s1:"
	// nilUUID sorts before every generated identifier; it starts the first page.
	nilUUID = "00000000-0000-0000-0000-000000000000"
)

// Type is the kind of a tracked subject.
type Type string

const (
	TypePerson  Type = "person"
	TypeHome    Type = "home"
	TypeVehicle Type = "vehicle"
	TypePet     Type = "pet"
	TypeCustom  Type = "custom"
)

// Subject is a household-scoped tracked entity. HouseholdID is populated by
// repositories but is not part of the public representation.
type Subject struct {
	ID           string
	HouseholdID  string
	Type         Type
	Name         string
	Archived     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Version      int64
	LinkedUserID *string
}

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

// InvalidQueryError names the offending list query parameter.
type InvalidQueryError struct{ Parameter string }

func (e *InvalidQueryError) Error() string { return "invalid query parameter " + e.Parameter }

// Patch is a partial update; nil fields are left unchanged.
type Patch struct {
	Name            *string
	Type            *Type
	Archived        *bool
	LinkedUserID    *string
	LinkedUserIDSet bool
}

type ListQuery struct {
	Archived bool
	// IDs nil means unfiltered; non-nil values are canonical, unique UUIDs.
	IDs []string
	// Limit 0 selects DefaultLimit.
	Limit  int
	Cursor string
}

type Page struct {
	Items      []Subject
	NextCursor *string
}

// Repository persists subjects. Every method is scoped by household; Get and
// Update return ErrNotFound for rows outside that household. Update returns
// ErrVersionMismatch when the subject exists with another version. List returns
// subjects with id greater than afterID in ascending id order, at most limit.
// Any other error is an infrastructure failure.
type Repository interface {
	CreateIdempotent(ctx context.Context, householdID, actorID, key, name string, t Type, linkedUserID *string, fingerprint []byte) (Subject, bool, error)
	FindCreation(ctx context.Context, householdID, actorID, key string) (Subject, []byte, error)
	Get(ctx context.Context, householdID, subjectID string) (Subject, error)
	List(ctx context.Context, householdID string, archived bool, afterID string, limit int) ([]Subject, error)
	ListByIDs(ctx context.Context, householdID string, archived bool, ids []string, afterID string, limit int) ([]Subject, error)
	Update(ctx context.Context, householdID, subjectID string, expectedVersion int64, p Patch) (Subject, error)
}

// Authorizer returns nil when userID may act in householdID and ErrNotFound
// otherwise. Other errors are infrastructure failures.
type Authorizer func(ctx context.Context, userID, householdID string) error

// Members returns nil when userID is any-role member of householdID.
type Members func(ctx context.Context, userID, householdID string) error

type Service struct {
	repository Repository
	authorize  Authorizer
	members    Members
}

func NewService(repository Repository, authorize Authorizer, members Members) *Service {
	return &Service{repository: repository, authorize: authorize, members: members}
}

// check validates the household identifier, canonicalizes it to lowercase, and
// authorizes the caller. It returns the canonical identifier.
func (s *Service) check(ctx context.Context, userID, householdID string) (string, error) {
	if !ids.IsCanonicalUUID(householdID) {
		return "", ErrNotFound
	}
	householdID = strings.ToLower(householdID)
	err := s.authorize(ctx, userID, householdID)
	if err == nil {
		return householdID, nil
	}
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", ErrUnavailable
}

func mapRepoErr(err error) error {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		return err
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrVersionMismatch):
		return err
	default:
		return ErrUnavailable
	}
}

func (s *Service) Get(ctx context.Context, userID, householdID, subjectID string) (Subject, error) {
	if !ids.IsCanonicalUUID(subjectID) {
		return Subject{}, ErrNotFound
	}
	subjectID = strings.ToLower(subjectID)
	householdID, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Subject{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	found, err := s.repository.Get(ctx, householdID, subjectID)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return found, nil
}
