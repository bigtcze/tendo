package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	// ErrInvalidReference means a referenced resource is malformed, unknown, or
	// outside the caller's membership. It maps to the uniform invalid_reference
	// validation response.
	ErrInvalidReference = errors.New("invalid reference")
	// ErrNotFound is returned by a SessionRepository when no matching row exists.
	// Every other repository error is an infrastructure failure.
	ErrNotFound = errors.New("not found")
)

// SessionLifetime is the fixed absolute lifetime of a session. It is also the
// cookie Max-Age; there is no sliding renewal.
const SessionLifetime = 30 * 24 * time.Hour

const (
	DummyPassword    = "tendo fixed dummy password"
	maxPasswordBytes = 512
	repositoryBudget = 5 * time.Second
)

// Principal identifies the authenticated user. DefaultHouseholdID is a UI
// convenience only and may be empty; it is never an authorization grant.
// Household access must always be validated against memberships.
//
// InstanceAdmin reports whether the account currently holds the
// installation-wide administrator capability. It is resolved from
// instance_admins on every request and is never cached in the session row. In
// responses it is a UI hint only; backend authorization must inspect this field
// instead of re-implementing the grant lookup.
type Principal struct {
	UserID, Login, DefaultHouseholdID string
	InstanceAdmin                     bool
}

// Session is the result of a successful login. Token is the only copy of the
// secret and is carried to the client in the cookie.
type Session struct {
	Token     string
	ID        string
	Principal Principal
	ExpiresAt time.Time
}

// SessionInfo describes an active stored session.
type SessionInfo struct {
	ID        string
	Principal Principal
	ExpiresAt time.Time
}

type LoginRecord struct {
	Principal    Principal
	PasswordHash string
	// CredentialVersion is the stored version of PasswordHash at the time of
	// the read. Login revalidates both under a share lock before inserting the
	// session, so a password reset cannot be outrun by an in-flight sign-in.
	CredentialVersion int64
}

type NewSession struct {
	UserID               string
	TokenHash            []byte
	CreatedAt, ExpiresAt time.Time
}

// VerifiedSession is a session insert bound to the exact credential state that
// produced the password verification.
type VerifiedSession struct {
	Session           NewSession
	PasswordHash      string
	CredentialVersion int64
}

// SessionRepository is the single persistence port of the session service.
// Lookups that find nothing return ErrNotFound; any other error is treated as
// an infrastructure failure. CreateVerifiedSession returns ErrInvalidCredentials
// when the stored credential no longer matches the verified one.
type SessionRepository interface {
	FindLogin(ctx context.Context, login string) (LoginRecord, error)
	PruneExpired(ctx context.Context, userID string, now time.Time) error
	CreateVerifiedSession(ctx context.Context, s VerifiedSession) (string, error)
	FindActive(ctx context.Context, tokenHash []byte, now time.Time) (SessionInfo, error)
	DeleteByTokenHash(ctx context.Context, tokenHash []byte) error
	// SetDefaultHousehold selects a household the user already belongs to. It
	// returns ErrInvalidReference for a malformed, unknown, or non-member
	// household and ErrUnauthenticated when the user row no longer exists.
	SetDefaultHousehold(ctx context.Context, userID, householdID string) error
}

type SessionService struct {
	repository     SessionRepository
	clock          func() time.Time
	dummyHash      string
	verifyPassword func(string, string) (bool, error)
}

// NewSessionService builds the service. A nil clock means time.Now.
func NewSessionService(repository SessionRepository, clock func() time.Time) (*SessionService, error) {
	if repository == nil {
		return nil, errors.New("session repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	dummy, err := security.HashPassword(DummyPassword)
	if err != nil {
		return nil, err
	}
	return &SessionService{repository: repository, clock: clock, dummyHash: dummy, verifyPassword: security.VerifyPassword}, nil
}

func (s *SessionService) SetPasswordVerifier(verifier func(string, string) (bool, error)) {
	if verifier != nil {
		s.verifyPassword = verifier
	}
}

// now is UTC truncated to whole seconds so the value persisted at login and
// the value later read back and serialized are identical.
func (s *SessionService) now() time.Time { return s.clock().UTC().Truncate(time.Second) }

func tokenDigest(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// VerifyCurrentPassword verifies a known local credential without performing login or creating a session.
func (s *SessionService) VerifyCurrentPassword(hash, password string) error {
	if !utf8.ValidString(password) || len(password) == 0 || len(password) > maxPasswordBytes {
		return ErrInvalidCredentials
	}
	valid, err := s.verifyPassword(hash, password)
	return credentialOutcome(valid, err)
}

// credentialOutcome maps a stored-hash verification. A corrupt stored hash
// stays externally indistinguishable from a mismatch (ErrInvalidCredentials)
// but also matches security.ErrInvalidPasswordHash so adapters can surface
// the integrity fault operationally.
func credentialOutcome(valid bool, err error) error {
	switch {
	case errors.Is(err, ErrPasswordWorkLimit):
		return ErrPasswordWorkLimit
	case errors.Is(err, security.ErrInvalidPasswordHash):
		return fmt.Errorf("%w: %w", ErrInvalidCredentials, security.ErrInvalidPasswordHash)
	case err != nil || !valid:
		return ErrInvalidCredentials
	}
	return nil
}

// Login verifies the password without holding any lock (Argon2 is slow), then
// re-reads the credential under a share lock in one transaction with the
// session insert. A password reset that lands in between makes the revalidation
// fail exactly like a wrong password instead of leaving a session that outlives
// the reset.
func (s *SessionService) Login(ctx context.Context, login, password string) (Session, error) {
	if !utf8.ValidString(login) || !loginPattern.MatchString(login) || !utf8.ValidString(password) || len(password) == 0 || len(password) > maxPasswordBytes {
		return Session{}, ErrInvalidCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryBudget)
	defer cancel()
	record, err := s.repository.FindLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		_, verifyErr := s.verifyPassword(s.dummyHash, password)
		if errors.Is(verifyErr, ErrPasswordWorkLimit) {
			return Session{}, ErrPasswordWorkLimit
		}
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, errors.New("session persistence failed")
	}
	if err := credentialOutcome(s.verifyPassword(record.PasswordHash, password)); err != nil {
		return Session{}, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Session{}, errors.New("session token generation failed")
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.now()
	expires := now.Add(SessionLifetime)
	if err := s.repository.PruneExpired(ctx, record.Principal.UserID, now); err != nil {
		return Session{}, errors.New("session persistence failed")
	}
	id, err := s.repository.CreateVerifiedSession(ctx, VerifiedSession{
		Session:           NewSession{UserID: record.Principal.UserID, TokenHash: tokenDigest(token), CreatedAt: now, ExpiresAt: expires},
		PasswordHash:      record.PasswordHash,
		CredentialVersion: record.CredentialVersion,
	})
	if errors.Is(err, ErrInvalidCredentials) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, errors.New("session persistence failed")
	}
	return Session{Token: token, ID: id, Principal: record.Principal, ExpiresAt: expires}, nil
}

// Lookup returns the active session for a cookie token. It returns
// ErrUnauthenticated only when the token is malformed, unknown, or expired;
// repository failures are returned as other errors.
func (s *SessionService) Lookup(ctx context.Context, token string) (SessionInfo, error) {
	if !validToken(token) {
		return SessionInfo{}, ErrUnauthenticated
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryBudget)
	defer cancel()
	info, err := s.repository.FindActive(ctx, tokenDigest(token), s.now())
	if errors.Is(err, ErrNotFound) {
		return SessionInfo{}, ErrUnauthenticated
	}
	if err != nil {
		return SessionInfo{}, errors.New("session persistence failed")
	}
	return info, nil
}

// Authenticate is Lookup reduced to the principal.
func (s *SessionService) Authenticate(ctx context.Context, token string) (Principal, error) {
	info, err := s.Lookup(ctx, token)
	return info.Principal, err
}

// SetDefaultHousehold records the caller's account-wide default household. The
// value is a navigation hint, never an authorization grant: only an existing
// membership in the referenced household is accepted, and every protected
// operation re-authorizes against memberships. The last write wins; there is no
// precondition or If-Match.
func (s *SessionService) SetDefaultHousehold(ctx context.Context, userID, householdID string) error {
	if !ids.IsCanonicalUUID(userID) {
		return ErrUnauthenticated
	}
	if !ids.IsCanonicalUUID(householdID) {
		return ErrInvalidReference
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryBudget)
	defer cancel()
	switch err := s.repository.SetDefaultHousehold(ctx, userID, householdID); {
	case errors.Is(err, ErrUnauthenticated):
		return ErrUnauthenticated
	case errors.Is(err, ErrInvalidReference):
		return ErrInvalidReference
	case err != nil:
		return errors.New("session persistence failed")
	}
	return nil
}

// Logout deletes the session; it is idempotent and ignores malformed tokens.
func (s *SessionService) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryBudget)
	defer cancel()
	if err := s.repository.DeleteByTokenHash(ctx, tokenDigest(token)); err != nil {
		return errors.New("session persistence failed")
	}
	return nil
}

func validToken(t string) bool {
	_, ok := decodeToken(t)
	return ok
}

func decodeToken(t string) ([]byte, bool) {
	if len(t) != 43 {
		return nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(t)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != t {
		return nil, false
	}
	return b, true
}
