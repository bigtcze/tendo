package subject

import (
	"context"
	"errors"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

func normalizePatch(p Patch) (Patch, error) {
	normalized := Patch{Type: p.Type, Archived: p.Archived}
	if p.Name != nil {
		trimmed, err := ValidateName(*p.Name)
		if err != nil {
			return Patch{}, err
		}
		normalized.Name = &trimmed
	}
	if p.Type != nil {
		if _, ok := ParseType(string(*p.Type)); !ok {
			return Patch{}, &ValidationError{"type", "invalid_type"}
		}
	}
	return normalized, nil
}

func (s *Service) Update(ctx context.Context, userID, householdID, subjectID string, expectedVersion int64, p Patch) (Subject, error) {
	if !ids.IsCanonicalUUID(subjectID) {
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
	normalized, err := normalizePatch(p)
	if err != nil {
		return Subject{}, s.validationAfterExistenceCheck(ctx, householdID, subjectID, err)
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
			return Subject{}, s.validationAfterExistenceCheck(ctx, householdID, subjectID, err)
		}
	}
	if p.LinkedUserIDSet {
		normalized.LinkedUserIDSet = true
		normalized.LinkedUserID = normalizedLinked
	}
	updated, err := s.repository.Update(ctx, householdID, subjectID, expectedVersion, normalized)
	if err != nil {
		return Subject{}, s.staleTypeLinkError(ctx, householdID, subjectID, p, err)
	}
	return updated, nil
}

// validationAfterExistenceCheck keeps a bad PATCH on an existing subject as
// 422 while preserving 404 for missing or foreign-household targets.
func (s *Service) validationAfterExistenceCheck(ctx context.Context, householdID, subjectID string, validationErr error) error {
	var validation *ValidationError
	if !errors.As(validationErr, &validation) {
		return validationErr
	}
	probeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := s.repository.Get(probeCtx, householdID, subjectID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	return validationErr
}

// Stored type/link state matters only when the patch changes one side while
// omitting the other; explicit type+link or unlinking fully determines validity.
func subjectTypeLinkDependsOnStoredState(p Patch) bool {
	return (p.Type != nil && !p.LinkedUserIDSet) || (p.Type == nil && p.LinkedUserIDSet && p.LinkedUserID != nil)
}

func resultingType(p Patch, storedType Type) Type {
	if p.Type != nil {
		return *p.Type
	}
	return storedType
}

func resultingLink(p Patch, storedLink *string) *string {
	if p.LinkedUserIDSet {
		return p.LinkedUserID
	}
	return storedLink
}

// staleTypeLinkError resolves the stored-state invariant only when one side is
// omitted; otherwise the request already determines validity. The scoped probe
// distinguishes 404, unavailable, requires_person, and ordinary stale 412.
func (s *Service) staleTypeLinkError(ctx context.Context, householdID, subjectID string, p Patch, updateErr error) error {
	if !errors.Is(updateErr, ErrVersionMismatch) || !subjectTypeLinkDependsOnStoredState(p) {
		return mapRepoErr(updateErr)
	}
	probeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	stored, err := s.repository.Get(probeCtx, householdID, subjectID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	if resultingLink(p, stored.LinkedUserID) != nil && resultingType(p, stored.Type) != TypePerson {
		return &ValidationError{"linkedUserId", "requires_person"}
	}
	return ErrVersionMismatch
}
