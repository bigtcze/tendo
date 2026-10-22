// Package subject holds the household-scoped subject domain and application
// service. It does not import other modules or adapters; membership checks are
// injected through Authorizer.
package subject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
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

func (s *Service) Create(ctx context.Context, userID, householdID, key, name string, t Type, linkedUserID *string) (Subject, error) {
	householdID, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Subject{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	trimmedForFingerprint := strings.TrimSpace(name)
	linkedForFingerprint := linkedUserID
	if linkedForFingerprint != nil {
		canonicalLink := strings.ToLower(*linkedForFingerprint)
		linkedForFingerprint = &canonicalLink
	}
	fingerprint := creationFingerprint(strings.ToLower(userID), trimmedForFingerprint, t, linkedForFingerprint)
	if existing, stored, findErr := s.repository.FindCreation(ctx, householdID, strings.ToLower(userID), key); findErr == nil {
		return compareCreation(existing, stored, fingerprint)
	} else if !errors.Is(findErr, ErrNotFound) {
		return Subject{}, mapRepoErr(findErr)
	}
	trimmed, err := ValidateName(name)
	if err != nil {
		return Subject{}, err
	}
	if _, ok := ParseType(string(t)); !ok {
		return Subject{}, &ValidationError{"type", "invalid_type"}
	}
	linkedUserID, err = s.validateLinkedUser(ctx, householdID, t, linkedUserID)
	if err != nil {
		return Subject{}, err
	}
	created, inserted, err := s.repository.CreateIdempotent(ctx, householdID, strings.ToLower(userID), key, trimmed, t, linkedUserID, fingerprint)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	if inserted {
		return created, nil
	}
	existing, stored, err := s.repository.FindCreation(ctx, householdID, strings.ToLower(userID), key)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return compareCreation(existing, stored, fingerprint)
}

func compareCreation(existing Subject, stored, submitted []byte) (Subject, error) {
	if !bytes.Equal(stored, submitted) {
		return Subject{}, ErrIdempotencyKeyReused
	}
	return existing, nil
}

func (s *Service) validateLinkedUser(ctx context.Context, householdID string, t Type, linked *string) (*string, error) {
	if linked == nil {
		return nil, nil
	}
	if t != TypePerson {
		return nil, &ValidationError{"linkedUserId", "requires_person"}
	}
	id := strings.ToLower(*linked)
	if !validUUID(id) || s.members == nil {
		return nil, &ValidationError{"linkedUserId", "invalid_reference"}
	}
	if err := s.members(ctx, id, householdID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &ValidationError{"linkedUserId", "invalid_reference"}
		}
		return nil, ErrUnavailable
	}
	return &id, nil
}

func creationFingerprint(actor, name string, t Type, linked *string) []byte {
	var encoded []byte
	fields := []string{"tendo.subject.create.v1", actor, name, string(t)}
	if linked != nil {
		fields = append(fields, strings.ToLower(*linked))
	}
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		encoded = append(encoded, size[:]...)
		encoded = append(encoded, field...)
	}
	digest := sha256.Sum256(encoded)
	return digest[:]
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
	ids, err := CanonicalIDs(q.IDs)
	if err != nil {
		return Page{}, &InvalidQueryError{"id"}
	}
	after := nilUUID
	if q.Cursor != "" {
		var cursorErr error
		if q.IDs == nil {
			after, cursorErr = DecodeCursor(q.Cursor)
		} else {
			after, cursorErr = decodeFilteredCursor(q.Cursor, householdID, q.Archived, ids)
		}
		if cursorErr != nil {
			return Page{}, &InvalidQueryError{"cursor"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	var items []Subject
	if q.IDs == nil {
		items, err = s.repository.List(ctx, householdID, q.Archived, after, limit+1)
	} else {
		items, err = s.repository.ListByIDs(ctx, householdID, q.Archived, ids, after, limit+1)
	}
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next := EncodeCursor(items[limit-1].ID)
		if q.IDs != nil {
			next = encodeFilteredCursor(householdID, q.Archived, ids, items[limit-1].ID)
		}
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
	if p.Name == nil && p.Type == nil && p.Archived == nil && !p.LinkedUserIDSet {
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
	var normalizedLinked *string
	if p.LinkedUserIDSet && p.LinkedUserID != nil {
		checkType := TypePerson
		if p.Type != nil {
			checkType = *p.Type
		}
		normalizedLinked, err = s.validateLinkedUser(ctx, householdID, checkType, p.LinkedUserID)
		if err != nil {
			return Subject{}, err
		}
	}
	if p.LinkedUserIDSet {
		normalized.LinkedUserIDSet = true
		normalized.LinkedUserID = normalizedLinked
	}
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

// CanonicalIDs validates, lowercases, sorts, and deduplicates list filter IDs.
func CanonicalIDs(input []string) ([]string, error) {
	if input == nil {
		return nil, nil
	}
	if len(input) > 100 {
		return nil, errors.New("too many ids")
	}
	seen := make(map[string]struct{}, len(input))
	ids := make([]string, 0, len(input))
	for _, id := range input {
		if !validUUID(id) {
			return nil, errors.New("invalid id")
		}
		id = strings.ToLower(id)
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func filteredFingerprint(householdID string, archived bool, ids []string) string {
	fields := []string{"tendo.subject.list.v2", strings.ToLower(householdID), fmt.Sprint(archived)}
	fields = append(fields, ids...)
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return fmt.Sprintf("%x", digest[:16])
}

func encodeFilteredCursor(householdID string, archived bool, ids []string, lastID string) string {
	raw := fmt.Sprintf("s2:%s:%s", filteredFingerprint(householdID, archived, ids), strings.ToLower(lastID))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeFilteredCursor(cursor, householdID string, archived bool, ids []string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "s2" || parts[1] != filteredFingerprint(householdID, archived, ids) || !validUUID(parts[2]) || parts[2] != strings.ToLower(parts[2]) {
		return "", errors.New("invalid filtered cursor")
	}
	return parts[2], nil
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
