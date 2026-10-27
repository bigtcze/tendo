package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/url"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
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
	ErrOIDCSessionChanged       = errors.New("oidc initiating session changed")
	ErrOIDCProviderUnavailable  = errors.New("OIDC provider unavailable")
	ErrOIDCAuthenticationFailed = errors.New("OIDC authentication failed")
)

type OIDCVerifiedIdentity struct{ Issuer, Subject string }
type OIDCFlow struct {
	StateHash, BrowserTokenHash           []byte
	PreviousBrowserTokenHash              []byte
	Issuer, ClientID, Nonce, PKCEVerifier string
	Purpose                               OIDCPurpose
	UserID, SessionID                     string
	CreatedAt, ExpiresAt                  time.Time
}
type OIDCStartParams struct {
	Purpose              OIDCPurpose
	Principal            Principal
	Session              SessionInfo
	CurrentPassword      string
	PreviousBrowserToken string
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
	dummyPasswordHash                        string
}
type OIDCStartResult struct{ AuthorizationURL, BrowserToken string }
type OIDCCallbackResult struct {
	Destination  string
	Code         OIDCCode
	SessionToken string
}

// A service is constructed only when OIDC is enabled; disabled mode is represented by a nil service.
func NewOIDCService(
	repo OIDCRepository,
	provider OIDCProvider,
	issuer, clientID, displayName, publicURL string,
	clock func() time.Time,
	random func([]byte) error,
	verifyPassword func(string, string) error,
) (*OIDCService, error) {
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
	dummyHash, err := security.HashPassword(dummyPassword)
	if err != nil {
		return nil, errors.New("OIDC password verification initialization failed")
	}
	return &OIDCService{
		repo: repo, provider: provider, issuer: issuer, clientID: clientID,
		displayName: displayName, publicURL: publicURL, clock: clock,
		random: random, verifyPassword: verifyPassword, dummyPasswordHash: dummyHash,
	}, nil
}
func (s *OIDCService) Status() (bool, string) { return true, s.displayName }
func (s *OIDCService) Linked(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	return s.repo.Linked(ctx, userID, s.issuer)
}

const OIDCOperationBudget = 5 * time.Second
const oidcOperationBudget = OIDCOperationBudget
const dummyPassword = DummyPassword

func (s *OIDCService) Start(ctx context.Context, p OIDCStartParams) (OIDCStartResult, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return OIDCStartResult{}, ErrOIDCUnavailable
	}
	flow, state, browser, err := s.prepareStartFlow(ctx, p)
	if err != nil {
		return OIDCStartResult{}, err
	}
	return s.authorizeAndPersist(ctx, p, flow, state, browser)
}
func (s *OIDCService) Callback(ctx context.Context, params url.Values, browserToken, currentSessionToken string) (OIDCCallbackResult, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcOperationBudget)
	defer cancel()
	parsed, ok := parseCallback(params, browserToken)
	if !ok {
		return invalidCallback(), nil
	}
	flow, err := s.repo.ConsumeFlow(ctx, digest(parsed.state), digest(parsed.browser), s.clock().UTC(), s.issuer, s.clientID)
	if errors.Is(err, ErrNotFound) {
		return invalidCallback(), nil
	}
	if err != nil {
		return failure(OIDCPurposeLogin, OIDCUnavailable), nil
	}
	if parsed.providerError != "" {
		return failure(flow.Purpose, OIDCCancelled), nil
	}
	if parsed.code == "" {
		return failure(flow.Purpose, OIDCInvalidFlow), nil
	}
	if result, rejected := s.checkCallbackSession(ctx, flow, currentSessionToken); rejected {
		return result, nil
	}
	verified, err := s.exchangeCallback(ctx, flow, parsed)
	if err != nil {
		return failure(flow.Purpose, callbackExchangeCode(err)), nil
	}
	userID, err := s.callbackUser(ctx, flow, verified)
	if err != nil {
		return failure(flow.Purpose, callbackIdentityCode(err)), nil
	}
	return s.issueCallbackSession(ctx, flow, verified, userID, currentSessionToken)
}
func failure(p OIDCPurpose, c OIDCCode) OIDCCallbackResult {
	dest := "/login"
	if p == OIDCPurposeLink {
		dest = "/account"
	}
	return OIDCCallbackResult{Destination: dest + "#oidcError=" + string(c), Code: c}
}
func digest(v []byte) []byte { h := sha256.Sum256(v); return h[:] }
