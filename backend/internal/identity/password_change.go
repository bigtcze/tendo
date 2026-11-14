package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

// This file owns local password replacement: the self-service change, the
// shared persistence protocol used by admin reset redemption, and the operator
// CLI reset. All three must leave the account in the same state, so the write
// itself has exactly one implementation (see CredentialReplacementTransaction).

const passwordChangeBudget = 10 * time.Second

var (
	// ErrLocalSessionRequired refuses a local password change from a session
	// that was not established by a local password sign-in, and from an account
	// that has no local password at all. An OIDC session must never replace a
	// local password, even when the account also has one.
	ErrLocalSessionRequired = errors.New("local session required")
	// ErrCurrentPasswordIncorrect reports a wrong current password. It is a
	// field validation answer, not an authentication failure, so the client
	// stays signed in and only the form shows the error.
	ErrCurrentPasswordIncorrect = errors.New("current password incorrect")
)

// LocalCredential is the stored local password state of one account.
type LocalCredential struct {
	PasswordHash string
	Version      int64
}

func (c LocalCredential) equal(other LocalCredential) bool {
	return c.PasswordHash == other.PasswordHash && c.Version == other.Version
}

// Revocation reports the sessions deleted by a password replacement.
type Revocation struct {
	// unexpired holds the token hashes of the deleted sessions that had not
	// expired yet: those are the sign-outs the account actually loses.
	unexpired [][]byte
}

// Sessions reports how many live sessions the replacement signed out.
func (r Revocation) Sessions() int { return len(r.unexpired) }

// Includes reports whether the given token hash was a live session at
// replacement time.
func (r Revocation) Includes(tokenHash []byte) bool {
	for _, candidate := range r.unexpired {
		if string(candidate) == string(tokenHash) {
			return true
		}
	}
	return false
}

// NewRevocation reports a replacement's deleted sessions. Adapters pass the
// token hashes of the sessions that were still unexpired when they were deleted.
func NewRevocation(unexpired [][]byte) Revocation { return Revocation{unexpired: unexpired} }

// CredentialReplacementTransaction is the persistence surface of "the password
// changed, sign the account out everywhere". The methods must run in one
// transaction and in this order: reset links, then the credential row, then the
// replacement. Admin reset redemption takes password_reset_tokens FOR UPDATE
// before local_credentials, so taking them in the opposite order here would
// deadlock the two flows against each other.
type CredentialReplacementTransaction interface {
	// RevokeOutstandingPasswordResets revokes every usable admin-issued reset
	// link of the account, so a link issued before the change cannot replace
	// the new password afterwards.
	RevokeOutstandingPasswordResets(context.Context, string, time.Time) error
	// LockLocalCredential takes the credential row FOR UPDATE and reports its
	// current hash and version. ErrNotFound means the account has no local
	// password. The lock is held until the transaction ends.
	LockLocalCredential(context.Context, string) (LocalCredential, error)
	// ReplaceLocalPassword replaces the hash, increments credential_version and
	// deletes every session of the account. It reports the sessions that were
	// still unexpired, and returns ErrNotFound when the row is gone. Future MCP
	// OAuth token revocation belongs here as well.
	ReplaceLocalPassword(context.Context, string, string, time.Time) (Revocation, error)
}

// PasswordChangeTransaction adds the caller's own session to the shared
// replacement protocol: the flow must prove the caller's session still existed
// when the replacement deleted it, and issue the caller a fresh one.
type PasswordChangeTransaction interface {
	CredentialReplacementTransaction
	// ActiveSessionExists reports whether the account still has that session.
	ActiveSessionExists(context.Context, string, []byte, time.Time) (bool, error)
	// CreateSession inserts a new session row for the account.
	CreateSession(context.Context, NewSession) (string, error)
}

type PasswordChangeRepository interface {
	// ReadLocalCredential is the unlocked read used to verify the current
	// password before the slow hash and the transaction.
	ReadLocalCredential(context.Context, string) (LocalCredential, error)
	WithPasswordChangeTransaction(context.Context, func(PasswordChangeTransaction) error) error
}

// ChangePasswordInput is one self-service change request. SessionToken is the
// caller's own cookie value; it is never logged.
type ChangePasswordInput struct {
	Principal       Principal
	SignInMethod    SignInMethod
	SessionToken    string
	CurrentPassword string
	NewPassword     string
}

type PasswordChangeService struct {
	repository     PasswordChangeRepository
	clock          func() time.Time
	hashPassword   PasswordHasher
	verifyPassword func(string, string) (bool, error)
}

func NewPasswordChangeService(repository PasswordChangeRepository, clock func() time.Time) (*PasswordChangeService, error) {
	if repository == nil {
		return nil, errors.New("password change repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &PasswordChangeService{
		repository:     repository,
		clock:          clock,
		hashPassword:   security.HashPassword,
		verifyPassword: security.VerifyPassword,
	}, nil
}

func (s *PasswordChangeService) SetPasswordHasher(hasher PasswordHasher) {
	if hasher != nil {
		s.hashPassword = hasher
	}
}

func (s *PasswordChangeService) SetPasswordVerifier(verifier func(string, string) (bool, error)) {
	if verifier != nil {
		s.verifyPassword = verifier
	}
}

func (s *PasswordChangeService) now() time.Time { return s.clock().UTC().Truncate(time.Second) }

// ChangePassword replaces the caller's local password and signs the account out
// everywhere except the session returned here. The refusal order is part of the
// contract: the session kind is checked before any body password work, the new
// password policy before the current password, and the current password before
// the transaction.
//
// A wrong sign-in kind or a non-canonical user ID means "this browser is not
// signed in with a usable local session" (403). A malformed session token means
// the cookie itself is not a session at all, which is an authentication failure
// (401) so the browser is told to sign in again.
func (s *PasswordChangeService) ChangePassword(ctx context.Context, in ChangePasswordInput) (Session, error) {
	if in.SignInMethod != SignInPassword || !ids.IsCanonicalUUID(in.Principal.UserID) {
		return Session{}, ErrLocalSessionRequired
	}
	if !validToken(in.SessionToken) {
		return Session{}, ErrUnauthenticated
	}
	if err := validatePassword(in.NewPassword); err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			return Session{}, &ValidationError{Field: "newPassword", Code: validation.Code}
		}
		return Session{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, passwordChangeBudget)
	defer cancel()
	verified, err := s.repository.ReadLocalCredential(ctx, in.Principal.UserID)
	if errors.Is(err, ErrNotFound) {
		// The account lost its local credential: nothing can be changed and the
		// browser is not holding the credential this flow is about.
		return Session{}, ErrLocalSessionRequired
	}
	if err != nil {
		return Session{}, errors.New("password persistence failed")
	}
	if err = s.verifyCurrent(verified.PasswordHash, in.CurrentPassword); err != nil {
		return Session{}, err
	}
	hash, err := hashNewPassword(ctx, s.hashPassword, in.NewPassword)
	if err != nil {
		return Session{}, err
	}
	return s.replace(ctx, in, verified, hash)
}

// verifyCurrent maps a stored-hash verification to this flow's answers. A wrong
// password and a corrupt stored hash both reject the field, but the corrupt
// hash stays recognizable with errors.Is so handlers report the integrity fault
// operationally.
func (s *PasswordChangeService) verifyCurrent(storedHash, current string) error {
	if !validPasswordInput(current) {
		return ErrCurrentPasswordIncorrect
	}
	valid, err := s.verifyPassword(storedHash, current)
	switch {
	case errors.Is(err, ErrPasswordWorkLimit):
		return ErrPasswordWorkLimit
	case errors.Is(err, security.ErrInvalidPasswordHash):
		return fmt.Errorf("%w: %w", ErrCurrentPasswordIncorrect, security.ErrInvalidPasswordHash)
	case err != nil || !valid:
		return ErrCurrentPasswordIncorrect
	}
	return nil
}

// hashNewPassword hashes outside any transaction. Argon2 is slow, so the work
// is done before the locks are taken; a cancelled request never writes.
func hashNewPassword(ctx context.Context, hasher PasswordHasher, password string) (string, error) {
	hash, err := hasher(password)
	if errors.Is(err, ErrPasswordWorkLimit) {
		return "", ErrPasswordWorkLimit
	}
	if err != nil {
		return "", errors.New("password hashing failed")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	return hash, nil
}

// replace runs the locked mutation. The verified credential is re-read under
// FOR UPDATE and must still be exactly what was verified: if another change
// won, the caller's current password is stale. A caller whose own session is
// already gone is signed out instead, because the password it verified no
// longer belongs to anyone signed in with that session.
func (s *PasswordChangeService) replace(ctx context.Context, in ChangePasswordInput, verified LocalCredential, hash string) (Session, error) {
	now := s.now()
	tokenHash := tokenDigest(in.SessionToken)
	var session Session
	err := s.repository.WithPasswordChangeTransaction(ctx, func(tx PasswordChangeTransaction) error {
		// Revoke outstanding reset links before locking the credential. Admin
		// reset redemption takes the reset token row before the credential row,
		// so the same order here keeps the two flows from deadlocking; a link
		// issued before this change can then never replace the new password.
		if err := tx.RevokeOutstandingPasswordResets(ctx, in.Principal.UserID, now); err != nil {
			return err
		}
		credential, err := tx.LockLocalCredential(ctx, in.Principal.UserID)
		if errors.Is(err, ErrNotFound) {
			return ErrLocalSessionRequired
		}
		if err != nil {
			return err
		}
		if !credential.equal(verified) {
			return s.staleCredential(ctx, tx, in.Principal.UserID, tokenHash, now)
		}
		revocation, err := tx.ReplaceLocalPassword(ctx, in.Principal.UserID, hash, now)
		if err != nil {
			return err
		}
		if !revocation.Includes(tokenHash) {
			// The caller's row disappeared between authentication and here, so
			// there is no session left to rotate into.
			return ErrUnauthenticated
		}
		token, expires, err := newSessionToken(now)
		if err != nil {
			return err
		}
		id, err := tx.CreateSession(ctx, NewSession{
			UserID: in.Principal.UserID, Method: SignInPassword, TokenHash: tokenDigest(token), CreatedAt: now, ExpiresAt: expires,
		})
		if err != nil {
			return err
		}
		session = Session{
			Token: token, ID: id, ExpiresAt: expires, SignInMethod: SignInPassword,
			Principal: in.Principal,
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

// staleCredential decides between "you are signed out" and "that password was
// wrong" when the stored credential moved under the caller.
func (s *PasswordChangeService) staleCredential(ctx context.Context, tx PasswordChangeTransaction, userID string, tokenHash []byte, now time.Time) error {
	active, err := tx.ActiveSessionExists(ctx, userID, tokenHash, now)
	if err != nil {
		return err
	}
	if !active {
		return ErrUnauthenticated
	}
	return ErrCurrentPasswordIncorrect
}

// newSessionToken mints a fresh session secret. The stored row is identified by
// its token digest; the plaintext never leaves this call.
func newSessionToken(now time.Time) (string, time.Time, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, errors.New("session token generation failed")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), now.Add(SessionLifetime), nil
}

// replaceCredentialAndRevoke is the shared local-password write used by the
// self-service change and the operator CLI reset: revoke outstanding reset
// links, lock the credential row, then replace the hash and sign the account
// out everywhere. Admin reset redemption calls the same three transaction
// methods itself because it must keep the link it is redeeming usable.
func replaceCredentialAndRevoke(ctx context.Context, tx CredentialReplacementTransaction, userID, passwordHash string, now time.Time) (Revocation, error) {
	if err := tx.RevokeOutstandingPasswordResets(ctx, userID, now); err != nil {
		return Revocation{}, err
	}
	if _, err := tx.LockLocalCredential(ctx, userID); err != nil {
		return Revocation{}, err
	}
	return tx.ReplaceLocalPassword(ctx, userID, passwordHash, now)
}

// AdminPasswordRepository is the operator CLI's persistence surface. It uses
// the same credential replacement transaction as the browser flows.
type AdminPasswordRepository interface {
	FindUserIDByLogin(context.Context, string) (string, error)
	WithCredentialReplacement(context.Context, func(CredentialReplacementTransaction) error) error
}

// AdminPasswordReset reports the account and how many live sessions the
// operator reset signed out.
type AdminPasswordReset struct {
	UserID   string
	Sessions int
}

// AdminPasswordService is the `tendo users set-password` implementation. It
// runs with the runtime database role inside the application container, so the
// authority boundary is host access, exactly like instance-admin grant.
type AdminPasswordService struct {
	repository   AdminPasswordRepository
	clock        func() time.Time
	hashPassword PasswordHasher
}

func NewAdminPasswordService(repository AdminPasswordRepository, clock func() time.Time) (*AdminPasswordService, error) {
	if repository == nil {
		return nil, errors.New("admin password repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &AdminPasswordService{repository: repository, clock: clock, hashPassword: security.HashPassword}, nil
}

func (s *AdminPasswordService) SetPasswordHasher(hasher PasswordHasher) {
	if hasher != nil {
		s.hashPassword = hasher
	}
}

func (s *AdminPasswordService) now() time.Time { return s.clock().UTC().Truncate(time.Second) }

// SetPassword replaces one account's local password by login and deletes every
// session of the account. The new password is validated before the account is
// looked up, so a policy violation is reported even for an unknown login and
// never depends on account existence. An account without a local credential is
// refused instead of silently gaining a new sign-in method.
func (s *AdminPasswordService) SetPassword(ctx context.Context, login, password string) (AdminPasswordReset, error) {
	if !utf8.ValidString(login) || !loginPattern.MatchString(login) {
		return AdminPasswordReset{}, ErrNotFound
	}
	if err := validatePassword(password); err != nil {
		return AdminPasswordReset{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, passwordChangeBudget)
	defer cancel()
	userID, err := s.repository.FindUserIDByLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		return AdminPasswordReset{}, ErrNotFound
	}
	if err != nil {
		return AdminPasswordReset{}, errors.New("password persistence failed")
	}
	hash, err := hashNewPassword(ctx, s.hashPassword, password)
	if err != nil {
		return AdminPasswordReset{}, err
	}
	var reset AdminPasswordReset
	err = s.repository.WithCredentialReplacement(ctx, func(tx CredentialReplacementTransaction) error {
		revocation, e := replaceCredentialAndRevoke(ctx, tx, userID, hash, s.now())
		if errors.Is(e, ErrNotFound) {
			return ErrNoLocalPassword
		}
		if e != nil {
			return e
		}
		reset = AdminPasswordReset{UserID: userID, Sessions: revocation.Sessions()}
		return nil
	})
	if err != nil {
		return AdminPasswordReset{}, err
	}
	return reset, nil
}

// ValidateLocalPassword exposes the shared 15-128 character local-password
// policy to the operator CLI, which checks the interactive input before it
// hands the secret to the service.
func ValidateLocalPassword(password string) error { return validatePassword(password) }

// validPasswordInput is the shape check applied to a password that is about to
// be verified rather than created: any non-empty UTF-8 value within the
// verifier's input bound.
func validPasswordInput(password string) bool {
	return utf8.ValidString(password) && len(password) > 0 && len(password) <= maxPasswordBytes
}
