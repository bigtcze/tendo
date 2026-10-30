package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

var (
	ErrInvalidInvitation      = errors.New("invalid invitation")
	ErrLoginUnavailable       = errors.New("login unavailable")
	ErrAlreadyMember          = errors.New("already a member")
	ErrHouseholdConflict      = errors.New("household conflict")
	ErrIdempotencyKeyRequired = errors.New("idempotency key required")
	ErrInvalidIdempotencyKey  = errors.New("invalid idempotency key")
)

const InvitationLifetime = 7 * 24 * time.Hour
const invitationRepositoryBudget = 5 * time.Second

type Invitation struct {
	ID, HouseholdID, CreatedByUserID string
	CreatedAt, ExpiresAt             time.Time
	AcceptedAt, RevokedAt            *time.Time
	Status                           string
}
type InvitationResult struct {
	Invitation Invitation
	Token      string
	Created    bool
}
type InvitationUser struct{ UserID, Login, HouseholdID, Role string }
type CreateInvitationInput struct {
	HouseholdID, ActorID, CreationKey string
	TokenHash                         []byte
	CreatedAt, ExpiresAt              time.Time
}
type InvitationTransaction interface {
	RequireOwner(context.Context, string, string) error
	GetMembership(context.Context, string, string) (string, error)
	CreateInvitation(context.Context, CreateInvitationInput) (Invitation, bool, error)
	ListInvitations(context.Context, string, string, int) ([]Invitation, string, error)
	RevokeInvitation(context.Context, string, string, time.Time) error
	FindInvitation(context.Context, []byte, bool) (Invitation, error)
	FindLogin(context.Context, string) error
	LockUser(context.Context, string) (string, error)
	HasOtherMembership(context.Context, string, string) (bool, error)
	CreateUser(context.Context, string) (string, error)
	CreateCredential(context.Context, string, string) error
	AddInvitedMember(context.Context, string, string) error
	SetDefaultHousehold(context.Context, string, string) error
	AcceptInvitation(context.Context, string, time.Time) error
}
type InvitationRepository interface {
	WithInvitationTransaction(context.Context, func(InvitationTransaction) error) error
	FindInvitation(context.Context, []byte) (Invitation, error)
}
type PasswordHasher func(string) (string, error)
type InvitationService struct {
	repository   InvitationRepository
	clock        func() time.Time
	hashPassword PasswordHasher
}

func (s *InvitationService) SetPasswordHasher(hasher PasswordHasher) {
	if hasher != nil {
		s.hashPassword = hasher
	}
}

func NewInvitationService(repository InvitationRepository, clock func() time.Time) (*InvitationService, error) {
	if repository == nil {
		return nil, errors.New("invitation repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &InvitationService{
		repository:   repository,
		clock:        clock,
		hashPassword: security.HashPassword,
	}, nil
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		character := value[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func invitationNow(clock func() time.Time) time.Time {
	return clock().UTC().Truncate(time.Second)
}

func digestInvitationToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func validInvitationToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(rawToken) == 32 && base64.RawURLEncoding.EncodeToString(rawToken) == token
}

func invitationStatus(invitation Invitation, now time.Time) string {
	if invitation.AcceptedAt != nil {
		return "accepted"
	}
	if invitation.RevokedAt != nil {
		return "revoked"
	}
	if !invitation.ExpiresAt.After(now) {
		return "expired"
	}
	return "pending"
}

func ValidCreationKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for index := range key {
		if key[index] < '!' || key[index] > '~' {
			return false
		}
	}
	return true
}
