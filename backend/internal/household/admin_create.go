package household

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// creationFingerprintDomain tags the create-intent fingerprint so the same
// fields can never collide with another operation's fingerprint.
const creationFingerprintDomain = "tendo.admin.household.create.v1"

var (
	// ErrIdempotencyKeyReused means an Idempotency-Key was already used for a
	// different create intent. The original household is not returned.
	ErrIdempotencyKeyReused = errors.New("idempotency_key_reused")
	// ErrCreationKeyRaced means a concurrent transaction committed the same
	// actor/key pair first. The service answers it with exactly one replay
	// transaction, never by retrying the insert.
	ErrCreationKeyRaced = errors.New("household_creation_key_raced")
	// errAdminAccountUnavailable hides any not-found raised while creating the
	// household: the create path never answers 404, only 503.
	errAdminAccountUnavailable = errors.New("admin account unavailable")
)

// HouseholdCreation is the replay record of one instance-admin household
// creation, scoped to (actor, Idempotency-Key) for the installation.
type HouseholdCreation struct {
	HouseholdID        string
	ActorUserID        string
	CreationKey        string
	RequestFingerprint []byte
	CreatedAt          time.Time
}

// CreateHousehold creates a household together with its first admin
// membership under the instance-admin grant (ADR 0021). The Idempotency-Key is
// scoped to the actor: an identical retry returns the original household, while
// a different intent with the same key is ErrIdempotencyKeyReused. Order inside
// the transaction: grant, replay lookup, field validation, one timestamp,
// household insert, creation record, then the admin assignment.
func (s *InstanceAdminCenter) CreateHousehold(ctx context.Context, actorID, key, name, timezone, adminUserID string) (AdminHousehold, int64, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	fingerprint := householdCreationFingerprint(actorID, name, timezone, adminUserID)
	house, version, err := s.createHousehold(bounded, actorID, key, name, timezone, adminUserID, fingerprint)
	if !errors.Is(err, ErrCreationKeyRaced) {
		return house, version, err
	}
	// A concurrent create committed the same actor/key first. Read its result in
	// one fresh transaction; never retry the insert (that would loop forever).
	return s.replayHouseholdCreation(bounded, actorID, key, fingerprint)
}

func (s *InstanceAdminCenter) createHousehold(ctx context.Context, actorID, key, name, timezone, adminUserID string, fingerprint []byte) (AdminHousehold, int64, error) {
	var house AdminHousehold
	var version int64
	err := s.repository.WithInstanceAdminTransaction(ctx, func(tx InstanceAdminTransaction, effects InstanceAdminEffects) error {
		if err := s.gate(ctx, tx, actorID); err != nil {
			return err
		}
		existing, existingVersion, stored, err := tx.FindHouseholdCreation(ctx, actorID, key)
		if err == nil {
			house, version, err = compareHouseholdCreation(existing, existingVersion, stored, fingerprint)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		trimmed, err := validateHouseholdCreation(name, timezone)
		if err != nil {
			return err
		}
		normalizedAdmin := strings.ToLower(adminUserID)
		if !ids.IsCanonicalUUID(normalizedAdmin) {
			return &ValidationError{Field: "adminUserId", Code: "invalid_reference"}
		}
		now := s.clock().UTC()
		house, version, err = tx.InsertHousehold(ctx, trimmed, timezone, now)
		if err != nil {
			return err
		}
		if err = tx.InsertHouseholdCreation(ctx, HouseholdCreation{
			HouseholdID: house.ID, ActorUserID: strings.ToLower(actorID), CreationKey: key,
			RequestFingerprint: fingerprint, CreatedAt: now,
		}); err != nil {
			return err
		}
		if _, _, err = s.assignLocked(ctx, tx, effects.Assignment, house.ID, normalizedAdmin, actorID, RoleAdmin, now); err != nil {
			if errors.Is(err, ErrNotFound) {
				return errAdminAccountUnavailable
			}
			return err
		}
		return nil
	})
	if err != nil {
		return AdminHousehold{}, 0, err
	}
	return house, version, nil
}

// replayHouseholdCreation reads the result of the transaction that won the
// actor/key race. Exactly one attempt is made, so a lost race never loops.
func (s *InstanceAdminCenter) replayHouseholdCreation(ctx context.Context, actorID, key string, fingerprint []byte) (AdminHousehold, int64, error) {
	var house AdminHousehold
	var version int64
	err := s.repository.WithInstanceAdminTransaction(ctx, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(ctx, tx, actorID); err != nil {
			return err
		}
		existing, existingVersion, stored, err := tx.FindHouseholdCreation(ctx, actorID, key)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return errAdminAccountUnavailable
			}
			return err
		}
		house, version, err = compareHouseholdCreation(existing, existingVersion, stored, fingerprint)
		return err
	})
	if err != nil {
		return AdminHousehold{}, 0, err
	}
	return house, version, nil
}

// GetHousehold reads one household and its version under the instance-admin
// grant. A malformed or unknown identifier is ErrNotFound.
func (s *InstanceAdminCenter) GetHousehold(ctx context.Context, actorID, householdID string) (AdminHousehold, int64, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var house AdminHousehold
	var version int64
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if !ids.IsCanonicalUUID(householdID) {
			return ErrNotFound
		}
		var err error
		house, version, err = tx.GetHousehold(bounded, householdID)
		return err
	})
	if err != nil {
		return AdminHousehold{}, 0, err
	}
	return house, version, nil
}

func compareHouseholdCreation(existing AdminHousehold, version int64, stored, submitted []byte) (AdminHousehold, int64, error) {
	if !bytes.Equal(stored, submitted) {
		return AdminHousehold{}, 0, ErrIdempotencyKeyReused
	}
	return existing, version, nil
}

// validateHouseholdCreation reuses the household validation rules but names the
// field "name" as the create contract does. Order is name, then timezone; the
// caller validates adminUserId next.
func validateHouseholdCreation(name, timezone string) (string, error) {
	trimmed, err := Validate(name, timezone)
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) && validation.Field == "householdName" {
			return "", &ValidationError{Field: "name", Code: validation.Code}
		}
		return "", err
	}
	return trimmed, nil
}

// householdCreationFingerprint hashes the create intent over length-prefixed
// fields. The actor is lowercased, the name is trimmed exactly like the stored
// value, and the timezone is hashed as submitted.
func householdCreationFingerprint(actorID, name, timezone, adminUserID string) []byte {
	fields := []string{
		creationFingerprintDomain,
		strings.ToLower(actorID),
		strings.TrimSpace(name),
		timezone,
		strings.ToLower(adminUserID),
	}
	var encoded []byte
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		encoded = append(encoded, size[:]...)
		encoded = append(encoded, field...)
	}
	digest := sha256.Sum256(encoded)
	return digest[:]
}
