package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
	"unicode/utf8"
)

// OIDCPendingLifetime bounds how long an unauthenticated browser may continue a
// first-login decision. The database enforces the same maximum.
const OIDCPendingLifetime = 10 * time.Minute

// oidcPendingAttemptLimit is the number of failed local-password confirmations
// after which the pending continuation is deleted.
const oidcPendingAttemptLimit = 5

// OIDCInvitationOutcome reports what happened to an invitation carried across
// the provider round trip.
type OIDCInvitationOutcome string

const (
	OIDCInvitationNone     OIDCInvitationOutcome = "none"
	OIDCInvitationAccepted OIDCInvitationOutcome = "accepted"
	OIDCInvitationInvalid  OIDCInvitationOutcome = "invalid"
)

// OIDCLoginResolution is the outcome of evaluating the first-login decision
// table for an unlinked callback.
type OIDCLoginResolution int

const (
	OIDCLoginSignedIn OIDCLoginResolution = iota + 1
	OIDCLoginNeedsPending
	OIDCLoginConflict
)

// OIDCPendingStatus is the read-only projection of a pending continuation. The
// handle itself is never exposed by the API.
type OIDCPendingStatus struct {
	ExpiresAt     time.Time
	EmailVerified bool
	Invitation    bool
}

// OIDCLoginRequest asks the repository to evaluate the first-login decision
// table in one transaction. A pending handle is generated up front so rows 3/5
// can store it without a second write path.
type OIDCLoginRequest struct {
	Verified          OIDCVerifiedIdentity
	Session           NewSession
	PendingHandleHash []byte
	ClientID          string
	InviteTokenHash   []byte
	Now               time.Time
	PendingExpiresAt  time.Time
}

type OIDCLoginOutcome struct {
	Resolution OIDCLoginResolution
	Invitation OIDCInvitationOutcome
}

type OIDCPendingAccountParams struct {
	HandleHash       []byte
	Issuer, ClientID string
	Login            string
	Session          NewSession
	Now              time.Time
}

type OIDCPendingAccountOutcome struct {
	UserID, Login, DefaultHouseholdID string
	// InstanceAdmin mirrors the persisted grant of a pre-existing account; a
	// freshly created JIT account never has one.
	InstanceAdmin bool
	Invitation    OIDCInvitationOutcome
}

type OIDCPendingLinkParams struct {
	HandleHash           []byte
	Issuer, ClientID     string
	UserID               string
	VerifiedPasswordHash string
	Session              NewSession
	Now                  time.Time
}

type OIDCPendingLinkOutcome struct {
	// Conflict reports row 7a: the pending row was consumed but nothing was
	// bound because the subject or the target account changed concurrently.
	Conflict                          bool
	UserID, Login, DefaultHouseholdID string
	// InstanceAdmin mirrors the target account's persisted grant.
	InstanceAdmin bool
	Invitation    OIDCInvitationOutcome
	EmailSaved    bool
}

type OIDCDiscardParams struct {
	HandleHash       []byte
	UserID           string
	Issuer, ClientID string
	Now              time.Time
	ExpiresAt        time.Time
}

// OIDCPendingAccountResult is the successful "continue as a new account" answer.
type OIDCPendingAccountResult struct {
	Session    Session
	Invitation OIDCInvitationOutcome
}

// OIDCPendingLinkResult is the successful "confirm with Tendo sign-in" answer.
type OIDCPendingLinkResult struct {
	Session    Session
	Invitation OIDCInvitationOutcome
	EmailSaved bool
}

func pendingHandleDigest(token string) ([]byte, bool) {
	raw, ok := decodeToken(token)
	if !ok {
		return nil, false
	}
	return digest(raw), true
}

// Pending returns the continuation status without consuming it.
func (s *OIDCService) Pending(ctx context.Context, token string) (OIDCPendingStatus, error) {
	handleHash, ok := pendingHandleDigest(token)
	if !ok {
		return OIDCPendingStatus{}, ErrOIDCPendingNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	return s.repo.ReadPendingLogin(ctx, handleHash, s.clock().UTC().Truncate(time.Second), s.issuer, s.clientID)
}

// CreatePendingAccount implements row 6: a JIT account with a generated login,
// no local credential and no household unless the carried invitation is valid.
func (s *OIDCService) CreatePendingAccount(ctx context.Context, token string) (OIDCPendingAccountResult, error) {
	handleHash, ok := pendingHandleDigest(token)
	if !ok {
		return OIDCPendingAccountResult{}, ErrOIDCPendingNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	now := s.clock().UTC().Truncate(time.Second)
	for attempt := 0; attempt < 3; attempt++ {
		login, err := s.generatedMemberLogin()
		if err != nil {
			return OIDCPendingAccountResult{}, ErrOIDCUnavailable
		}
		sessionToken, session, err := s.newSession(now)
		if err != nil {
			return OIDCPendingAccountResult{}, ErrOIDCUnavailable
		}
		outcome, err := s.repo.CreatePendingAccount(ctx, OIDCPendingAccountParams{HandleHash: handleHash, Issuer: s.issuer, ClientID: s.clientID, Login: login, Session: session, Now: now})
		if errors.Is(err, ErrLoginUnavailable) {
			continue
		}
		if err != nil {
			return OIDCPendingAccountResult{}, err
		}
		return OIDCPendingAccountResult{
			Session: Session{
				Token: sessionToken,
				Principal: Principal{
					UserID: outcome.UserID, Login: outcome.Login, DefaultHouseholdID: outcome.DefaultHouseholdID,
					InstanceAdmin: outcome.InstanceAdmin,
				},
				ExpiresAt: session.ExpiresAt,
			},
			Invitation: outcome.Invitation,
		}, nil
	}
	return OIDCPendingAccountResult{}, ErrOIDCUnavailable
}

// LinkPendingAccount implements row 7: prove ownership of an existing local
// account with its password, then bind the pending subject in one transaction.
func (s *OIDCService) LinkPendingAccount(ctx context.Context, token, login, password string) (OIDCPendingLinkResult, error) {
	handleHash, ok := pendingHandleDigest(token)
	if !ok {
		return OIDCPendingLinkResult{}, ErrOIDCPendingNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	now := s.clock().UTC().Truncate(time.Second)
	// Resolve the continuation before touching any credential: a fabricated or
	// expired handle must not reveal whether a login exists, and it must not
	// consume password-verification work or the attempt budget.
	if _, err := s.repo.ReadPendingLogin(ctx, handleHash, now, s.issuer, s.clientID); err != nil {
		if errors.Is(err, ErrOIDCPendingNotFound) {
			return OIDCPendingLinkResult{}, ErrOIDCPendingNotFound
		}
		return OIDCPendingLinkResult{}, ErrOIDCUnavailable
	}
	record, err := s.verifyLinkCredentials(ctx, handleHash, login, password, now)
	if err != nil {
		return OIDCPendingLinkResult{}, err
	}
	sessionToken, session, err := s.newSession(now)
	if err != nil {
		return OIDCPendingLinkResult{}, ErrOIDCUnavailable
	}
	outcome, err := s.repo.LinkPendingAccount(ctx, OIDCPendingLinkParams{
		HandleHash: handleHash, Issuer: s.issuer, ClientID: s.clientID, UserID: record.Principal.UserID,
		VerifiedPasswordHash: record.PasswordHash, Session: session, Now: now,
	})
	if err != nil {
		return OIDCPendingLinkResult{}, err
	}
	if outcome.Conflict {
		return OIDCPendingLinkResult{}, ErrOIDCConflict
	}
	return OIDCPendingLinkResult{
		Session: Session{
			Token: sessionToken,
			Principal: Principal{
				UserID: outcome.UserID, Login: outcome.Login, DefaultHouseholdID: outcome.DefaultHouseholdID,
				InstanceAdmin: outcome.InstanceAdmin,
			},
			ExpiresAt: session.ExpiresAt,
		},
		Invitation: outcome.Invitation,
		EmailSaved: outcome.EmailSaved,
	}, nil
}

// verifyLinkCredentials mirrors createSession: unknown login and a wrong
// password are indistinguishable, a failed attempt is counted, and the
// password-work limit passes through as its own error.
func (s *OIDCService) verifyLinkCredentials(ctx context.Context, handleHash []byte, login, password string, now time.Time) (LoginRecord, error) {
	if !utf8.ValidString(login) || !loginPattern.MatchString(login) || !utf8.ValidString(password) || len(password) == 0 || len(password) > maxPasswordBytes {
		return LoginRecord{}, ErrInvalidCredentials
	}
	record, err := s.repo.FindLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		if verifyErr := s.verifyPassword(s.dummyPasswordHash, password); errors.Is(verifyErr, ErrPasswordWorkLimit) {
			return LoginRecord{}, ErrPasswordWorkLimit
		}
		return LoginRecord{}, s.pendingFailureOutcome(ctx, handleHash, now)
	}
	if err != nil {
		return LoginRecord{}, ErrOIDCUnavailable
	}
	if err := s.verifyPassword(record.PasswordHash, password); err != nil {
		if errors.Is(err, ErrPasswordWorkLimit) {
			return LoginRecord{}, ErrPasswordWorkLimit
		}
		return LoginRecord{}, s.pendingFailureOutcome(ctx, handleHash, now)
	}
	return record, nil
}

func (s *OIDCService) pendingFailureOutcome(ctx context.Context, handleHash []byte, now time.Time) error {
	if s.recordPendingFailure(ctx, handleHash, now) {
		return ErrOIDCPendingExhausted
	}
	return ErrInvalidCredentials
}

// CancelPending deletes the continuation if it exists; it is idempotent.
func (s *OIDCService) CancelPending(ctx context.Context, token string) error {
	handleHash, ok := pendingHandleDigest(token)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	return s.repo.CancelPendingLogin(ctx, handleHash)
}

// DiscardAccount implements row 8: a JIT account with nothing to lose may be
// discarded and replaced by a fresh continuation for row 7.
func (s *OIDCService) DiscardAccount(ctx context.Context, userID string) (string, OIDCPendingStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	now := s.clock().UTC().Truncate(time.Second)
	handle, _, err := s.randomToken()
	if err != nil {
		return "", OIDCPendingStatus{}, ErrOIDCUnavailable
	}
	raw, _ := base64.RawURLEncoding.DecodeString(handle)
	status, err := s.repo.DiscardOIDCAccount(ctx, OIDCDiscardParams{
		HandleHash: digest(raw), UserID: userID, Issuer: s.issuer, ClientID: s.clientID,
		Now: now, ExpiresAt: now.Add(OIDCPendingLifetime),
	})
	if err != nil {
		return "", OIDCPendingStatus{}, err
	}
	return handle, status, nil
}

// recordPendingFailure counts a failed local-password attempt. Counting is a
// separate committed statement so it survives the rejected request; the
// repository deletes the row at the limit.
func (s *OIDCService) recordPendingFailure(ctx context.Context, handleHash []byte, now time.Time) bool {
	deleted, err := s.repo.RecordPendingFailure(ctx, handleHash, now)
	if err != nil {
		return false
	}
	return deleted
}

func (s *OIDCService) newSession(now time.Time) (string, NewSession, error) {
	token, _, err := s.randomToken()
	if err != nil {
		return "", NewSession{}, err
	}
	return token, NewSession{TokenHash: digest([]byte(token)), CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}, nil
}

func (s *OIDCService) randomToken() (string, []byte, error) {
	var raw [32]byte
	if err := s.random(raw[:]); err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), raw[:], nil
}

const generatedLoginPrefix = "member-"
const generatedLoginAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// generatedMemberLogin derives a login from crypto randomness only. It is never
// derived from the email local part, so the login cannot leak the address.
func (s *OIDCService) generatedMemberLogin() (string, error) {
	// 36 * 7 = 252, so bytes below 252 map onto the alphabet without modulo bias.
	var buffer [32]byte
	filled := len(buffer)
	login := make([]byte, 0, len(generatedLoginPrefix)+8)
	login = append(login, generatedLoginPrefix...)
	for len(login) < len(generatedLoginPrefix)+8 {
		if filled == len(buffer) {
			if err := s.random(buffer[:]); err != nil {
				return "", err
			}
			filled = 0
		}
		if b := buffer[filled]; b < 252 {
			filled++
			login = append(login, generatedLoginAlphabet[int(b)%len(generatedLoginAlphabet)])
		} else {
			filled++
		}
	}
	return string(login), nil
}
