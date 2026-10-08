package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
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
	return &InvitationService{repository: repository, clock: clock, hashPassword: security.HashPassword}, nil
}
func validUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if v[i] != '-' {
				return false
			}
			continue
		}
		c := v[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func invitationNow(c func() time.Time) time.Time { return c().UTC().Truncate(time.Second) }
func digestInvitationToken(token string) []byte  { s := sha256.Sum256([]byte(token)); return s[:] }
func validInvitationToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(t)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == t
}
func invitationStatus(i Invitation, now time.Time) string {
	if i.AcceptedAt != nil {
		return "accepted"
	}
	if i.RevokedAt != nil {
		return "revoked"
	}
	if !i.ExpiresAt.After(now) {
		return "expired"
	}
	return "pending"
}
func ValidCreationKey(k string) bool {
	if len(k) < 1 || len(k) > 128 {
		return false
	}
	for i := range k {
		if k[i] < '!' || k[i] > '~' {
			return false
		}
	}
	return true
}
func (s *InvitationService) CreateInvitation(ctx context.Context, actor, householdID, key string) (InvitationResult, error) {
	if !validUUID(actor) || !validUUID(householdID) {
		return InvitationResult{}, household.ErrNotFound
	}
	if key == "" {
		return InvitationResult{}, ErrIdempotencyKeyRequired
	}
	if !ValidCreationKey(key) {
		return InvitationResult{}, ErrInvalidIdempotencyKey
	}
	now := invitationNow(s.clock)
	var result InvitationResult
	err := s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if e := tx.RequireOwner(ctx, actor, householdID); e != nil {
			return e
		}
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			return errors.New("invitation token generation failed")
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		inv, created, e := tx.CreateInvitation(ctx, CreateInvitationInput{HouseholdID: householdID, ActorID: actor, CreationKey: key, TokenHash: digestInvitationToken(token), CreatedAt: now, ExpiresAt: now.Add(InvitationLifetime)})
		if e != nil {
			return e
		}
		inv.Status = invitationStatus(inv, now)
		result = InvitationResult{Invitation: inv, Created: created}
		if created {
			result.Token = token
		}
		return nil
	})
	return result, err
}
func encodeInvitationCursor(id string) string {
	return "n1" + base64.RawURLEncoding.EncodeToString([]byte(id))
}
func decodeInvitationCursor(c string) (string, error) {
	if c == "" {
		return "", nil
	}
	if len(c) < 3 || c[:2] != "n1" {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	b, e := base64.RawURLEncoding.DecodeString(c[2:])
	if e != nil || len(b) != 36 || !validUUID(string(b)) {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	return string(b), nil
}
func (s *InvitationService) ListInvitations(ctx context.Context, actor, householdID string, limit int, cursor string) ([]Invitation, string, error) {
	if !validUUID(actor) || !validUUID(householdID) {
		return nil, "", household.ErrNotFound
	}
	id, e := decodeInvitationCursor(cursor)
	if e != nil {
		return nil, "", e
	}
	dbCursor := id
	if id != "" {
		dbCursor = "n1" + base64.RawURLEncoding.EncodeToString([]byte(id))
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var out []Invitation
	e = s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if x := tx.RequireOwner(ctx, actor, householdID); x != nil {
			return x
		}
		var x error
		out, _, x = tx.ListInvitations(ctx, householdID, dbCursor, limit+1)
		return x
	})
	if e != nil {
		return nil, "", e
	}
	now := invitationNow(s.clock)
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = encodeInvitationCursor(out[len(out)-1].ID)
	}
	for i := range out {
		out[i].Status = invitationStatus(out[i], now)
	}
	return out, next, nil
}
func (s *InvitationService) RevokeInvitation(ctx context.Context, actor, householdID, id string) error {
	if !validUUID(actor) || !validUUID(householdID) || !validUUID(id) {
		return household.ErrNotFound
	}
	return s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if e := tx.RequireOwner(ctx, actor, householdID); e != nil {
			return e
		}
		return tx.RevokeInvitation(ctx, householdID, id, invitationNow(s.clock))
	})
}
func (s *InvitationService) AcceptInvitationNewAccount(ctx context.Context, token, login, password string) (InvitationUser, error) {
	if !validInvitationToken(token) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	if err := validateCredentials(login, password); err != nil {
		return InvitationUser{}, err
	}
	digest := digestInvitationToken(token)
	now := invitationNow(s.clock)
	inv, e := s.repository.FindInvitation(ctx, digest)
	if errors.Is(e, ErrNotFound) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	if e != nil {
		return InvitationUser{}, e
	}
	if invitationStatus(inv, now) != "pending" {
		return InvitationUser{}, ErrInvalidInvitation
	}
	hash, e := s.hashPassword(password)
	if errors.Is(e, ErrPasswordWorkLimit) {
		return InvitationUser{}, ErrPasswordWorkLimit
	}
	if e != nil {
		return InvitationUser{}, e
	}
	var result InvitationUser
	e = s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		locked, x := tx.FindInvitation(ctx, digest, true)
		now = invitationNow(s.clock)
		if errors.Is(x, ErrNotFound) {
			return ErrInvalidInvitation
		}
		if x != nil {
			return x
		}
		if invitationStatus(locked, now) != "pending" {
			return ErrInvalidInvitation
		}
		if x = tx.FindLogin(ctx, login); x == nil {
			return ErrLoginUnavailable
		} else if !errors.Is(x, ErrNotFound) {
			return x
		}
		uid, x := tx.CreateUser(ctx, login)
		if x != nil {
			return x
		}
		if x = tx.CreateCredential(ctx, uid, hash); x != nil {
			return x
		}
		if x = tx.AddInvitedMember(ctx, uid, locked.HouseholdID); x != nil {
			return x
		}
		if x = tx.SetDefaultHousehold(ctx, uid, locked.HouseholdID); x != nil {
			return x
		}
		if x = tx.AcceptInvitation(ctx, locked.ID, now); x != nil {
			return x
		}
		result = InvitationUser{uid, login, locked.HouseholdID, household.RoleMember}
		return nil
	})
	return result, e
}
func (s *InvitationService) AcceptInvitationExistingAccount(ctx context.Context, userID, token string) (InvitationUser, error) {
	if !validUUID(userID) || !validInvitationToken(token) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	digest := digestInvitationToken(token)
	now := invitationNow(s.clock)
	var result InvitationUser
	e := s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		inv, x := tx.FindInvitation(ctx, digest, true)
		now = invitationNow(s.clock)
		if errors.Is(x, ErrNotFound) {
			return ErrInvalidInvitation
		}
		if x != nil {
			return x
		}
		if invitationStatus(inv, now) != "pending" {
			return ErrInvalidInvitation
		}
		def, x := tx.LockUser(ctx, userID)
		if errors.Is(x, ErrNotFound) {
			return ErrNotFound
		}
		if x != nil {
			return x
		}
		role, x := tx.GetMembership(ctx, userID, inv.HouseholdID)
		if x == nil {
			_ = role
			return ErrAlreadyMember
		}
		if !errors.Is(x, household.ErrNotFound) {
			return x
		}
		other, x := tx.HasOtherMembership(ctx, userID, inv.HouseholdID)
		if x != nil {
			return x
		}
		if def != "" || other {
			return ErrHouseholdConflict
		}
		if x = tx.AddInvitedMember(ctx, userID, inv.HouseholdID); x != nil {
			return x
		}
		if x = tx.SetDefaultHousehold(ctx, userID, inv.HouseholdID); x != nil {
			return x
		}
		if x = tx.AcceptInvitation(ctx, inv.ID, now); x != nil {
			return x
		}
		result = InvitationUser{UserID: userID, HouseholdID: inv.HouseholdID, Role: household.RoleMember}
		return nil
	})
	return result, e
}
