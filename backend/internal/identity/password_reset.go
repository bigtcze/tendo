package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

// PasswordResetLifetime is the maximum age of an admin-issued reset link. The
// database enforces the same 24-hour ceiling independently.
const PasswordResetLifetime = 24 * time.Hour

const passwordResetRepositoryBudget = 5 * time.Second

var (
	// ErrCannotResetSelf refuses an admin resetting their own password through
	// the admin path; a stolen admin session must not mint a new permanent
	// credential.
	ErrCannotResetSelf = errors.New("cannot reset self")
	// ErrPasswordResetNotPermitted refuses a reset whose target belongs to a
	// household the actor does not administer, or is an instance administrator
	// while the actor is not. A reset replaces the global credential, so one
	// household's admin role must not reach another household's member.
	ErrPasswordResetNotPermitted = errors.New("password reset not permitted")
	// ErrNoLocalPassword means the target signs in only through OIDC.
	ErrNoLocalPassword = errors.New("no local password")
	// ErrInvalidPasswordReset is the uniform answer for every unusable link:
	// malformed, unknown, used, revoked, expired, or no longer authorized.
	ErrInvalidPasswordReset = errors.New("invalid password reset")
)

// PasswordResetIssued is the one-time secret returned when an admin issues a
// link. The token is never persisted or logged in cleartext.
type PasswordResetIssued struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// PasswordResetRecord is the stored audit row used by the domain service.
type PasswordResetRecord struct {
	ID, HouseholdID, TargetUserID, CreatedByUserID string
	CreatedAt, ExpiresAt                           time.Time
	UsedAt, RevokedAt                              *time.Time
}

type CreatePasswordResetInput struct {
	HouseholdID, TargetUserID, ActorID string
	TokenHash                          []byte
	CreatedAt, ExpiresAt               time.Time
}

// PasswordResetTransaction is the transaction-scoped surface. Authorization
// goes through the shared household admin lock protocol, exactly like
// invitations, so reads and writes share one transaction.
type PasswordResetTransaction interface {
	RequireAdmin(context.Context, string, string) error
	GetMembership(context.Context, string, string) (string, error)
	FindUserLogin(context.Context, string) (string, error)
	LockPasswordResetTarget(context.Context, string) (string, error)
	// PasswordResetPermitted reports whether the actor (first argument) holds
	// reset authority over the whole target account (second argument): admin of
	// every household the target belongs to, and an instance administrator when
	// the target is one.
	PasswordResetPermitted(context.Context, string, string) (bool, error)
	HasLocalCredential(context.Context, string) (bool, error)
	RevokeOutstandingPasswordResetsForTarget(context.Context, string, time.Time) error
	RevokeOutstandingPasswordResetsInHousehold(context.Context, string, string, time.Time) error
	CreatePasswordReset(context.Context, CreatePasswordResetInput) (PasswordResetRecord, error)
	FindPasswordReset(context.Context, []byte, bool) (PasswordResetRecord, error)
	CompletePasswordReset(context.Context, string, time.Time) (int64, error)
	// LockLocalCredential takes the credential row FOR UPDATE and reports its
	// current version; ErrNotFound means the target no longer has a local
	// password. The lock is held until the transaction ends so a concurrent
	// sign-in cannot insert a session for the password being replaced.
	LockLocalCredential(context.Context, string) (int64, error)
	// UpdateLocalCredential replaces the hash, increments credential_version and
	// reports the affected row count.
	UpdateLocalCredential(context.Context, string, string) (int64, error)
	DeleteUserSessions(context.Context, string) error
}

type PasswordResetRepository interface {
	WithPasswordResetTransaction(context.Context, func(PasswordResetTransaction) error) error
	// FindPasswordReset is the non-locking prefetch. Lookups that find nothing
	// return ErrNotFound; every other error is an infrastructure failure.
	FindPasswordReset(context.Context, []byte) (PasswordResetRecord, error)
}

type PasswordResetService struct {
	repository   PasswordResetRepository
	clock        func() time.Time
	hashPassword PasswordHasher
}

func NewPasswordResetService(repository PasswordResetRepository, clock func() time.Time) (*PasswordResetService, error) {
	if repository == nil {
		return nil, errors.New("password reset repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &PasswordResetService{repository: repository, clock: clock, hashPassword: security.HashPassword}, nil
}

func (s *PasswordResetService) SetPasswordHasher(hasher PasswordHasher) {
	if hasher != nil {
		s.hashPassword = hasher
	}
}

func passwordResetNow(clock func() time.Time) time.Time {
	return clock().UTC().Truncate(time.Second)
}

func digestPasswordResetToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func validPasswordResetToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(rawToken) == 32 && base64.RawURLEncoding.EncodeToString(rawToken) == token
}

// IssuePasswordReset creates a single-use link for a local-password member of
// the household. Any earlier outstanding link for the target is revoked first,
// inside the same transaction, so a target never has two usable links.
func (s *PasswordResetService) IssuePasswordReset(ctx context.Context, actorID, householdID, targetUserID string) (PasswordResetIssued, error) {
	bounded, cancel := context.WithTimeout(ctx, passwordResetRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return PasswordResetIssued{}, err
	}
	if !ids.IsCanonicalUUID(actorID) || !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
		return PasswordResetIssued{}, household.ErrNotFound
	}
	now := passwordResetNow(s.clock)
	var issued PasswordResetIssued
	err := s.repository.WithPasswordResetTransaction(ctx, func(tx PasswordResetTransaction) error {
		if e := authorizePasswordResetIssue(ctx, tx, actorID, householdID, targetUserID); e != nil {
			return e
		}
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			return errors.New("password reset token generation failed")
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		if e := tx.RevokeOutstandingPasswordResetsForTarget(ctx, targetUserID, now); e != nil {
			return e
		}
		record, e := tx.CreatePasswordReset(ctx, CreatePasswordResetInput{
			HouseholdID: householdID, TargetUserID: targetUserID, ActorID: actorID,
			TokenHash: digestPasswordResetToken(token), CreatedAt: now, ExpiresAt: now.Add(PasswordResetLifetime),
		})
		if e != nil {
			return e
		}
		issued = PasswordResetIssued{Token: token, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt}
		return nil
	})
	return issued, err
}

// authorizePasswordResetIssue runs the issue checks in their contract order:
// actor is a household admin, not self, target exists (locked to serialize
// concurrent issuances) and is a member, the actor holds reset authority over
// the whole account (ADR 0017), and only then the local-password fact.
func authorizePasswordResetIssue(ctx context.Context, tx PasswordResetTransaction, actorID, householdID, targetUserID string) error {
	if e := tx.RequireAdmin(ctx, actorID, householdID); e != nil {
		return e
	}
	if targetUserID == actorID {
		return ErrCannotResetSelf
	}
	if _, e := tx.LockPasswordResetTarget(ctx, targetUserID); e != nil {
		return e
	}
	if _, e := tx.GetMembership(ctx, targetUserID, householdID); e != nil {
		return e
	}
	permitted, e := tx.PasswordResetPermitted(ctx, actorID, targetUserID)
	if e != nil {
		return e
	}
	if !permitted {
		return ErrPasswordResetNotPermitted
	}
	local, e := tx.HasLocalCredential(ctx, targetUserID)
	if e != nil {
		return e
	}
	if !local {
		return ErrNoLocalPassword
	}
	return nil
}

// RevokePasswordReset cancels an outstanding link for a member of this
// household. It is idempotent: cancelling when there is nothing to cancel is
// not an error.
func (s *PasswordResetService) RevokePasswordReset(ctx context.Context, actorID, householdID, targetUserID string) error {
	bounded, cancel := context.WithTimeout(ctx, passwordResetRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ids.IsCanonicalUUID(actorID) || !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
		return household.ErrNotFound
	}
	return s.repository.WithPasswordResetTransaction(ctx, func(tx PasswordResetTransaction) error {
		if e := tx.RequireAdmin(ctx, actorID, householdID); e != nil {
			return e
		}
		if targetUserID == actorID {
			return ErrCannotResetSelf
		}
		if _, e := tx.GetMembership(ctx, targetUserID, householdID); e != nil {
			return e
		}
		return tx.RevokeOutstandingPasswordResetsInHousehold(ctx, householdID, targetUserID, passwordResetNow(s.clock))
	})
}

// RedeemPasswordReset applies a new local password from a reset link. The row
// is locked FOR UPDATE, re-authorized against current membership and the
// issuer's current role, then marked used and the target's sessions deleted.
// Every unusable state maps to the same ErrInvalidPasswordReset.
func (s *PasswordResetService) RedeemPasswordReset(ctx context.Context, token, newPassword string) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, passwordResetRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validPasswordResetToken(token) {
		return "", ErrInvalidPasswordReset
	}
	digest := digestPasswordResetToken(token)
	// Early prefetch keeps the expensive hash out of obviously unusable links.
	now := passwordResetNow(s.clock)
	prefetched, err := s.repository.FindPasswordReset(ctx, digest)
	if errors.Is(err, ErrNotFound) {
		return "", ErrInvalidPasswordReset
	}
	if err != nil {
		return "", err
	}
	if !passwordResetUsable(prefetched, now) {
		return "", ErrInvalidPasswordReset
	}
	if err = validatePassword(newPassword); err != nil {
		return "", err
	}
	hash, err := s.hashPassword(newPassword)
	if errors.Is(err, ErrPasswordWorkLimit) {
		return "", ErrPasswordWorkLimit
	}
	if err != nil {
		return "", errors.New("password hashing failed")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	var login string
	err = s.repository.WithPasswordResetTransaction(ctx, func(tx PasswordResetTransaction) error {
		var e error
		login, e = s.applyPasswordReset(ctx, tx, digest, hash)
		return e
	})
	if err != nil {
		return "", err
	}
	return login, nil
}

// applyPasswordReset performs the locked, re-authorized mutation once the new
// password has been hashed outside the transaction.
func (s *PasswordResetService) applyPasswordReset(ctx context.Context, tx PasswordResetTransaction, digest []byte, hash string) (string, error) {
	lockedNow := passwordResetNow(s.clock)
	locked, err := tx.FindPasswordReset(ctx, digest, true)
	if errors.Is(err, ErrNotFound) {
		return "", ErrInvalidPasswordReset
	}
	if err != nil {
		return "", err
	}
	if !passwordResetUsable(locked, lockedNow) {
		return "", ErrInvalidPasswordReset
	}
	if err = passwordResetAuthorization(ctx, tx, locked); err != nil {
		return "", err
	}
	// Take the credential row FOR UPDATE before touching the password or the
	// sessions. A concurrent sign-in that verified the old hash either holds
	// the share lock first (and its session is deleted below) or waits here and
	// then fails its own revalidation.
	if _, err = tx.LockLocalCredential(ctx, locked.TargetUserID); errors.Is(err, ErrNotFound) {
		return "", ErrInvalidPasswordReset
	} else if err != nil {
		return "", err
	}
	login, err := tx.FindUserLogin(ctx, locked.TargetUserID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrInvalidPasswordReset
	}
	if err != nil {
		return "", err
	}
	rows, err := tx.CompletePasswordReset(ctx, locked.ID, lockedNow)
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", ErrInvalidPasswordReset
	}
	rows, err = tx.UpdateLocalCredential(ctx, locked.TargetUserID, hash)
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", ErrInvalidPasswordReset
	}
	if err = tx.DeleteUserSessions(ctx, locked.TargetUserID); err != nil {
		return "", err
	}
	return login, nil
}

func passwordResetUsable(record PasswordResetRecord, now time.Time) bool {
	return record.UsedAt == nil && record.RevokedAt == nil && record.ExpiresAt.After(now)
}

// passwordResetAuthorization re-checks the facts that can change after the
// link was issued: the target must still be a member, the issuer must still be
// an admin of the same household, and the issuer must still hold reset
// authority over every household the target belongs to.
func passwordResetAuthorization(ctx context.Context, tx PasswordResetTransaction, record PasswordResetRecord) error {
	role, err := tx.GetMembership(ctx, record.TargetUserID, record.HouseholdID)
	if errors.Is(err, household.ErrNotFound) {
		return ErrInvalidPasswordReset
	}
	if err != nil {
		return err
	}
	if role != household.RoleAdmin && role != household.RoleMember {
		return ErrInvalidPasswordReset
	}
	issuerRole, err := tx.GetMembership(ctx, record.CreatedByUserID, record.HouseholdID)
	if errors.Is(err, household.ErrNotFound) {
		return ErrInvalidPasswordReset
	}
	if err != nil {
		return err
	}
	if issuerRole != household.RoleAdmin {
		return ErrInvalidPasswordReset
	}
	// The target may have joined another household, or become an instance
	// administrator, since the link was issued; the issuer must still hold
	// authority over the whole account.
	permitted, err := tx.PasswordResetPermitted(ctx, record.CreatedByUserID, record.TargetUserID)
	if err != nil {
		return err
	}
	if !permitted {
		return ErrInvalidPasswordReset
	}
	return nil
}
