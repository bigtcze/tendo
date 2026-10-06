package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	// ErrNotFound is returned by a SessionRepository when no matching row exists.
	// Every other repository error is an infrastructure failure.
	ErrNotFound = errors.New("not found")
)

// SessionLifetime is the fixed absolute lifetime of a session. It is also the
// cookie Max-Age; there is no sliding renewal.
const SessionLifetime = 30 * 24 * time.Hour

const (
	maxPasswordBytes = 512
	repositoryBudget = 5 * time.Second
)

// Principal identifies the authenticated user. DefaultHouseholdID is a UI
// convenience only and may be empty; it is never an authorization grant.
// Household access must always be validated against memberships.
type Principal struct{ UserID, Login, DefaultHouseholdID string }

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
}

type NewSession struct {
	UserID               string
	TokenHash            []byte
	CreatedAt, ExpiresAt time.Time
}

// SessionRepository is the single persistence port of the session service.
// Lookups that find nothing return ErrNotFound; any other error is treated as
// an infrastructure failure.
type SessionRepository interface {
	FindLogin(ctx context.Context, login string) (LoginRecord, error)
	PruneExpired(ctx context.Context, userID string, now time.Time) error
	CreateSession(ctx context.Context, s NewSession) (string, error)
	FindActive(ctx context.Context, tokenHash []byte, now time.Time) (SessionInfo, error)
	DeleteByTokenHash(ctx context.Context, tokenHash []byte) error
}

type SessionService struct {
	repository SessionRepository
	clock      func() time.Time
	dummyHash  string
}

// NewSessionService builds the service. A nil clock means time.Now.
func NewSessionService(repository SessionRepository, clock func() time.Time) (*SessionService, error) {
	if repository == nil {
		return nil, errors.New("session repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	dummy, err := security.HashPassword("tendo fixed dummy password")
	if err != nil {
		return nil, err
	}
	return &SessionService{repository: repository, clock: clock, dummyHash: dummy}, nil
}

// now is UTC truncated to whole seconds so the value persisted at login and
// the value later read back and serialized are identical.
func (s *SessionService) now() time.Time { return s.clock().UTC().Truncate(time.Second) }

func tokenDigest(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (s *SessionService) Login(ctx context.Context, login, password string) (Session, error) {
	if !utf8.ValidString(login) || !loginPattern.MatchString(login) || !utf8.ValidString(password) || len(password) == 0 || len(password) > maxPasswordBytes {
		return Session{}, ErrInvalidCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryBudget)
	defer cancel()
	record, err := s.repository.FindLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		_, _ = security.VerifyPassword(s.dummyHash, password)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, errors.New("session persistence failed")
	}
	if valid, verifyErr := security.VerifyPassword(record.PasswordHash, password); verifyErr != nil || !valid {
		return Session{}, ErrInvalidCredentials
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
	id, err := s.repository.CreateSession(ctx, NewSession{UserID: record.Principal.UserID, TokenHash: tokenDigest(token), CreatedAt: now, ExpiresAt: expires})
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
	if len(t) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == t
}
