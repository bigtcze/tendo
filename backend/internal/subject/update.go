package subject

import (
	"context"
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
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	// Existence first: a missing or foreign target is uniformly 404 even when
	// the patch itself has field or link-reference errors.
	stored, err := s.repository.Get(ctx, householdID, subjectID)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	// Field/link and member-person semantics are evaluated against the current
	// stored row before staleness, so a stale invalid patch still reports the
	// documented 422/409 result. The version guard below then prevents every
	// write when the row changed: the guarded UPDATE can only modify this exact
	// version, so a concurrent transfer cannot make a guessed future version
	// unlink, archive or retype the new canonical person.
	normalized, err := s.normalizeAndValidatePatch(ctx, householdID, stored, p)
	if err != nil {
		return Subject{}, err
	}
	if stored.Version != expectedVersion {
		return Subject{}, ErrVersionMismatch
	}
	updated, err := s.repository.Update(ctx, householdID, subjectID, expectedVersion, normalized)
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return updated, nil
}

// normalizeAndValidatePatch applies field, reference and member-person checks
// against the stored row. Field validation precedes reference validation, and
// both precede member-person state conflicts, matching the documented order.
func (s *Service) normalizeAndValidatePatch(ctx context.Context, householdID string, stored Subject, p Patch) (Patch, error) {
	normalized, err := normalizePatch(p)
	if err != nil {
		return Patch{}, err
	}
	var normalizedLinked *string
	if p.LinkedUserIDSet && p.LinkedUserID != nil {
		checkType := TypePerson
		if p.Type != nil {
			checkType = *p.Type
		}
		normalizedLinked, err = s.validateLinkedUser(ctx, householdID, checkType, p.LinkedUserID)
		if err != nil {
			return Patch{}, err
		}
	}
	if p.LinkedUserIDSet {
		normalized.LinkedUserIDSet = true
		normalized.LinkedUserID = normalizedLinked
	}
	if err := validateMemberPersonPatch(stored, normalized); err != nil {
		return Patch{}, err
	}
	return normalized, nil
}

// validateMemberPersonPatch enforces the canonical member-person invariants.
// A linked subject belongs to its membership: it cannot be unlinked, relinked,
// retyped away from person, or archived. Same-value no-ops and ordinary name
// changes remain allowed. An unlinked subject cannot be linked directly to a
// member because that member necessarily already has a person; the transfer
// operation is the explicit reassociation path.
func validateMemberPersonPatch(stored Subject, p Patch) error {
	if stored.LinkedUserID == nil {
		if p.LinkedUserIDSet && p.LinkedUserID != nil {
			resultType := stored.Type
			if p.Type != nil {
				resultType = *p.Type
			}
			if resultType != TypePerson {
				return &ValidationError{Field: "linkedUserId", Code: "requires_person"}
			}
			return ErrMemberPersonExists
		}
		return nil
	}
	if p.LinkedUserIDSet && (p.LinkedUserID == nil || *p.LinkedUserID != *stored.LinkedUserID) {
		return ErrMemberPersonLocked
	}
	if p.Type != nil && *p.Type != TypePerson {
		return ErrMemberPersonLocked
	}
	if p.Archived != nil && *p.Archived {
		return ErrMemberPersonLocked
	}
	return nil
}
