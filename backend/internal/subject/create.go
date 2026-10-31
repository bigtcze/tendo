package subject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

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
	if !ids.IsCanonicalUUID(id) || s.members == nil {
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
