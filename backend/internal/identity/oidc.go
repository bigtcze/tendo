package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"
)

type OIDCPurpose string

const (
	OIDCPurposeLogin OIDCPurpose = "login"
	OIDCPurposeLink  OIDCPurpose = "link"
)

type OIDCCode string

const (
	OIDCCancelled            OIDCCode = "cancelled"
	OIDCInvalidFlow          OIDCCode = "invalid_flow"
	OIDCIdentityNotLinked    OIDCCode = "identity_not_linked"
	OIDCIdentityConflict     OIDCCode = "identity_conflict"
	OIDCSessionChanged       OIDCCode = "session_changed"
	OIDCAuthenticationFailed OIDCCode = "authentication_failed"
	OIDCUnavailable          OIDCCode = "unavailable"
)

var (
	ErrOIDCDisabled        = errors.New("oidc disabled")
	ErrOIDCUnavailable     = errors.New("oidc unavailable")
	ErrOIDCConflict        = errors.New("oidc identity conflict")
	ErrOIDCInvalidPurpose  = errors.New("invalid OIDC purpose")
	ErrOIDCAlreadySignedIn = errors.New("already signed in")
	// ErrOIDCSessionChanged: the initiating session ended before the link committed.
	ErrOIDCSessionChanged = errors.New("oidc initiating session changed")
)

type OIDCVerifiedIdentity struct{ Issuer, Subject string }
type OIDCFlow struct {
	StateHash, BrowserTokenHash           []byte
	Issuer, ClientID, Nonce, PKCEVerifier string
	Purpose                               OIDCPurpose
	UserID, SessionID                     string
	CreatedAt, ExpiresAt                  time.Time
}
type OIDCStartParams struct {
	Purpose         OIDCPurpose
	Principal       Principal
	Session         SessionInfo
	CurrentPassword string
}
type OIDCAuthorizationParams struct {
	Issuer, ClientID, RedirectURL, State, Nonce, PKCEVerifier string
	Purpose                                                   OIDCPurpose
}
type OIDCExchangeParams struct{ Code, RedirectURL, PKCEVerifier, Nonce, CallbackIssuer string }
type OIDCProvider interface {
	AuthorizationURL(context.Context, OIDCAuthorizationParams) (string, error)
	Exchange(context.Context, OIDCExchangeParams) (OIDCVerifiedIdentity, error)
}
type OIDCRepository interface {
	InsertFlow(context.Context, OIDCFlow) error
	ConsumeFlow(context.Context, []byte, []byte, time.Time, string, string) (OIDCFlow, error)
	FindIdentity(context.Context, string, string) (string, error)
	FindSession(context.Context, []byte, time.Time) (SessionInfo, error)
	FindCredential(context.Context, string) (string, error)
	CreateOIDCLoginSession(context.Context, NewSession) (string, error)
	LinkAndCreateSession(context.Context, OIDCFlow, OIDCVerifiedIdentity, NewSession, []byte) (string, error)
	Linked(context.Context, string, string) (bool, error)
}
type OIDCService struct {
	repo                                     OIDCRepository
	provider                                 OIDCProvider
	clock                                    func() time.Time
	random                                   func([]byte) error
	issuer, clientID, displayName, publicURL string
	verifyPassword                           func(string, string) error
}
type OIDCStartResult struct{ AuthorizationURL, BrowserToken string }
type OIDCCallbackResult struct {
	Destination  string
	Code         OIDCCode
	SessionToken string
}

// A service is constructed only when OIDC is enabled; disabled mode is represented by a nil service.
func NewOIDCService(repo OIDCRepository, provider OIDCProvider, issuer, clientID, displayName, publicURL string, clock func() time.Time, random func([]byte) error, verifyPassword func(string, string) error) (*OIDCService, error) {
	if repo == nil || provider == nil || issuer == "" || clientID == "" {
		return nil, errors.New("OIDC dependencies and configuration are required")
	}
	if clock == nil {
		clock = time.Now
	}
	if random == nil {
		random = func(b []byte) error { _, e := rand.Read(b); return e }
	}
	if verifyPassword == nil {
		return nil, errors.New("password verifier is required")
	}
	return &OIDCService{repo: repo, provider: provider, issuer: issuer, clientID: clientID, displayName: displayName, publicURL: publicURL, clock: clock, random: random, verifyPassword: verifyPassword}, nil
}
func (s *OIDCService) Status() (bool, string) { return true, s.displayName }
func (s *OIDCService) Linked(ctx context.Context, userID string) (bool, error) {
	return s.repo.Linked(ctx, userID, s.issuer)
}
func (s *OIDCService) Start(ctx context.Context, p OIDCStartParams) (OIDCStartResult, error) {
	if p.Purpose != OIDCPurposeLogin && p.Purpose != OIDCPurposeLink {
		return OIDCStartResult{}, ErrOIDCInvalidPurpose
	}
	if p.Purpose == OIDCPurposeLogin && p.Session.ID != "" {
		return OIDCStartResult{}, ErrOIDCAlreadySignedIn
	}
	flow := OIDCFlow{Issuer: s.issuer, ClientID: s.clientID, Purpose: p.Purpose, CreatedAt: s.clock().UTC().Truncate(time.Second)}
	if p.Purpose == OIDCPurposeLink {
		if p.Principal.UserID == "" || p.Session.ID == "" || p.Session.Principal.UserID != p.Principal.UserID {
			return OIDCStartResult{}, ErrUnauthenticated
		}
		hash, err := s.repo.FindCredential(ctx, p.Principal.UserID)
		if err != nil {
			_ = s.verifyPassword("", p.CurrentPassword)
			if errors.Is(err, ErrNotFound) {
				return OIDCStartResult{}, ErrInvalidCredentials
			}
			return OIDCStartResult{}, ErrOIDCUnavailable
		}
		if err = s.verifyPassword(hash, p.CurrentPassword); err != nil {
			return OIDCStartResult{}, err
		}
		flow.UserID, flow.SessionID = p.Principal.UserID, p.Session.ID
	}
	var state, browser, nonce, verifier [32]byte
	for _, b := range [][]byte{state[:], browser[:], nonce[:], verifier[:]} {
		if err := s.random(b); err != nil {
			return OIDCStartResult{}, ErrOIDCUnavailable
		}
	}
	flow.StateHash = digest(state[:])
	flow.BrowserTokenHash = digest(browser[:])
	flow.Nonce = base64.RawURLEncoding.EncodeToString(nonce[:])
	flow.PKCEVerifier = base64.RawURLEncoding.EncodeToString(verifier[:])
	flow.ExpiresAt = flow.CreatedAt.Add(10 * time.Minute)
	redirect := strings.TrimRight(s.publicURL, "/") + "/api/v1/auth/oidc/callback"
	auth, err := s.provider.AuthorizationURL(ctx, OIDCAuthorizationParams{Issuer: s.issuer, ClientID: s.clientID, RedirectURL: redirect, State: base64.RawURLEncoding.EncodeToString(state[:]), Nonce: flow.Nonce, PKCEVerifier: flow.PKCEVerifier, Purpose: p.Purpose})
	if err != nil {
		return OIDCStartResult{}, ErrOIDCUnavailable
	}
	if err = s.repo.InsertFlow(ctx, flow); err != nil {
		return OIDCStartResult{}, errors.New("OIDC persistence failed")
	}
	return OIDCStartResult{auth, base64.RawURLEncoding.EncodeToString(browser[:])}, nil
}
func (s *OIDCService) Callback(ctx context.Context, params url.Values, browserToken, currentSessionToken string) (OIDCCallbackResult, error) {
	invalid := OIDCCallbackResult{Destination: "/login#oidcError=invalid_flow", Code: OIDCInvalidFlow}
	for k, vs := range params {
		max := 4096
		if k == "state" || k == "iss" {
			max = 2048
		}
		if k == "error" {
			max = 128
		}
		if len(vs) != 1 || len(vs[0]) > max {
			return invalid, nil
		}
	}
	state, code, providerErr := params.Get("state"), params.Get("code"), params.Get("error")
	if len(state) != 43 || len(browserToken) != 43 {
		return invalid, nil
	}
	stateRaw, e := base64.RawURLEncoding.DecodeString(state)
	if e != nil || len(stateRaw) != 32 || base64.RawURLEncoding.EncodeToString(stateRaw) != state {
		return invalid, nil
	}
	browserRaw, e := base64.RawURLEncoding.DecodeString(browserToken)
	if e != nil || len(browserRaw) != 32 || base64.RawURLEncoding.EncodeToString(browserRaw) != browserToken {
		return invalid, nil
	}
	flow, e := s.repo.ConsumeFlow(ctx, digest(stateRaw), digest(browserRaw), s.clock().UTC(), s.issuer, s.clientID)
	if e != nil {
		return invalid, nil
	}
	if providerErr != "" {
		return failure(flow.Purpose, OIDCCancelled), nil
	}
	if code == "" {
		return failure(flow.Purpose, OIDCInvalidFlow), nil
	}
	if flow.Purpose == OIDCPurposeLink {
		active, err := s.repo.FindSession(ctx, digest([]byte(currentSessionToken)), s.clock().UTC())
		if errors.Is(err, ErrNotFound) {
			return failure(flow.Purpose, OIDCSessionChanged), nil
		}
		if err != nil {
			return failure(flow.Purpose, OIDCUnavailable), nil
		}
		if currentSessionToken == "" || active.Principal.UserID != flow.UserID || active.ID != flow.SessionID {
			return failure(flow.Purpose, OIDCSessionChanged), nil
		}
	}
	redirect := strings.TrimRight(s.publicURL, "/") + "/api/v1/auth/oidc/callback"
	verified, err := s.provider.Exchange(ctx, OIDCExchangeParams{Code: code, RedirectURL: redirect, PKCEVerifier: flow.PKCEVerifier, Nonce: flow.Nonce, CallbackIssuer: params.Get("iss")})
	if err != nil {
		return failure(flow.Purpose, OIDCAuthenticationFailed), nil
	}
	userID, err := s.repo.FindIdentity(ctx, verified.Issuer, verified.Subject)
	if flow.Purpose == OIDCPurposeLogin {
		if errors.Is(err, ErrNotFound) {
			return failure(flow.Purpose, OIDCIdentityNotLinked), nil
		}
		if err != nil {
			return failure(flow.Purpose, OIDCUnavailable), nil
		}
	} else {
		userID = flow.UserID
	}
	var raw [32]byte
	if err = s.random(raw[:]); err != nil {
		return failure(flow.Purpose, OIDCUnavailable), nil
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.clock().UTC().Truncate(time.Second)
	ns := NewSession{UserID: userID, TokenHash: digest([]byte(token)), CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}
	if flow.Purpose == OIDCPurposeLink {
		_, err = s.repo.LinkAndCreateSession(ctx, flow, verified, ns, digest([]byte(currentSessionToken)))
	} else {
		_, err = s.repo.CreateOIDCLoginSession(ctx, ns)
	}
	if errors.Is(err, ErrOIDCConflict) {
		return failure(flow.Purpose, OIDCIdentityConflict), nil
	}
	if errors.Is(err, ErrOIDCSessionChanged) {
		return failure(flow.Purpose, OIDCSessionChanged), nil
	}
	if err != nil {
		return failure(flow.Purpose, OIDCUnavailable), nil
	}
	dest := "/"
	if flow.Purpose == OIDCPurposeLink {
		dest = "/account#oidc=connected"
	}
	return OIDCCallbackResult{Destination: dest, SessionToken: token}, nil
}
func failure(p OIDCPurpose, c OIDCCode) OIDCCallbackResult {
	dest := "/login"
	if p == OIDCPurposeLink {
		dest = "/account"
	}
	return OIDCCallbackResult{Destination: dest + "#oidcError=" + string(c), Code: c}
}
func digest(v []byte) []byte { h := sha256.Sum256(v); return h[:] }
