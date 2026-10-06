// Package subject holds the household-scoped subject domain and application
// service. It does not import other modules or adapters; membership checks are
// injected through Authorizer.
package subject

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

// ParseType accepts only the exact lowercase wire values.
func ParseType(s string) (Type, bool) {
	switch t := Type(s); t {
	case TypePerson, TypeHome, TypeVehicle, TypePet, TypeCustom:
		return t, true
	}
	return "", false
}

// Subject is a household-scoped tracked entity. HouseholdID is populated by
// repositories but is not part of the public representation.
type Subject struct {
	ID          string
	HouseholdID string
	Type        Type
	Name        string
	Archived    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int64
}

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

// InvalidQueryError names the offending list query parameter.
type InvalidQueryError struct{ Parameter string }

func (e *InvalidQueryError) Error() string { return "invalid query parameter " + e.Parameter }

// Patch is a partial update; nil fields are left unchanged.
type Patch struct {
	Name     *string
	Type     *Type
	Archived *bool
}

type ListQuery struct {
	Archived bool
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
	Create(ctx context.Context, householdID, name string, t Type) (Subject, error)
	Get(ctx context.Context, householdID, subjectID string) (Subject, error)
	List(ctx context.Context, householdID string, archived bool, afterID string, limit int) ([]Subject, error)
	Update(ctx context.Context, householdID, subjectID string, expectedVersion int64, p Patch) (Subject, error)
}

// Authorizer returns nil when userID may act in householdID and ErrNotFound
// otherwise. Other errors are infrastructure failures.
type Authorizer func(ctx context.Context, userID, householdID string) error

type Service struct {
	repository Repository
	authorize  Authorizer
}

func NewService(repository Repository, authorize Authorizer) *Service {
	return &Service{repository: repository, authorize: authorize}
}

// check validates the household identifier, canonicalizes it to lowercase, and
// authorizes the caller. It returns the canonical identifier.
func (s *Service) check(ctx context.Context, userID, householdID string) (string, error) {
	if !validUUID(householdID) {
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
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrVersionMismatch):
		return err
	default:
		return ErrUnavailable
	}
}

func (s *Service) Create(ctx context.Context, userID, householdID, name string, t Type) (Subject, error) {
	householdID, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Subject{}, err
	}
	trimmed, err := ValidateName(name)
	if err != nil {
		return Subject{}, err
	}
	if _, ok := ParseType(string(t)); !ok {
		return Subject{}, &ValidationError{"type", "invalid_type"}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	created, err := s.repository.Create(ctx, householdID, trimmed, t)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, householdID, subjectID string) (Subject, error) {
	if !validUUID(subjectID) {
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

func (s *Service) List(ctx context.Context, userID, householdID string, q ListQuery) (Page, error) {
	householdID, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Page{}, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return Page{}, &InvalidQueryError{"limit"}
	}
	after := nilUUID
	if q.Cursor != "" {
		id, err := DecodeCursor(q.Cursor)
		if err != nil {
			return Page{}, &InvalidQueryError{"cursor"}
		}
		after = id
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	items, err := s.repository.List(ctx, householdID, q.Archived, after, limit+1)
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next := EncodeCursor(items[limit-1].ID)
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Subject{}
	}
	return page, nil
}

func (s *Service) Update(ctx context.Context, userID, householdID, subjectID string, expectedVersion int64, p Patch) (Subject, error) {
	if !validUUID(subjectID) {
		return Subject{}, ErrNotFound
	}
	subjectID = strings.ToLower(subjectID)
	householdID, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Subject{}, err
	}
	if p.Name == nil && p.Type == nil && p.Archived == nil {
		return Subject{}, ErrEmptyPatch
	}
	normalized := Patch{Type: p.Type, Archived: p.Archived}
	if p.Name != nil {
		trimmed, err := ValidateName(*p.Name)
		if err != nil {
			return Subject{}, err
		}
		normalized.Name = &trimmed
	}
	if p.Type != nil {
		if _, ok := ParseType(string(*p.Type)); !ok {
			return Subject{}, &ValidationError{"type", "invalid_type"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	updated, err := s.repository.Update(ctx, householdID, subjectID, expectedVersion, normalized)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return updated, nil
}

// ValidateName returns the trimmed name or a ValidationError.
func ValidateName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", &ValidationError{"name", "invalid_characters"}
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", &ValidationError{"name", "invalid_length"}
	}
	for _, r := range name {
		// Control, format (zero-width, bidi), and line or paragraph separator
		// characters can hide or reorder text.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return "", &ValidationError{"name", "invalid_characters"}
		}
	}
	return name, nil
}

// EncodeCursor returns the opaque, versioned cursor for the last returned id.
func EncodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strings.ToLower(id)))
}

// DecodeCursor returns the canonical lowercase id inside a cursor.
func DecodeCursor(cursor string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", errors.New("malformed cursor")
	}
	id, ok := strings.CutPrefix(string(raw), cursorPrefix)
	if !ok || !validUUID(id) {
		return "", errors.New("malformed cursor")
	}
	return strings.ToLower(id), nil
}

// validUUID accepts only the canonical 8-4-4-4-12 hexadecimal form.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
