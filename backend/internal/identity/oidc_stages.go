package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"
)

func (s *OIDCService) prepareStartFlow(
	ctx context.Context,
	p OIDCStartParams,
) (OIDCFlow, []byte, []byte, error) {
	if p.Purpose != OIDCPurposeLogin && p.Purpose != OIDCPurposeLink {
		return OIDCFlow{}, nil, nil, ErrOIDCInvalidPurpose
	}
	if p.Purpose == OIDCPurposeLogin && p.Session.ID != "" {
		return OIDCFlow{}, nil, nil, ErrOIDCAlreadySignedIn
	}
	flow := OIDCFlow{
		Issuer: s.issuer, ClientID: s.clientID, Purpose: p.Purpose,
		CreatedAt: s.clock().UTC().Truncate(time.Second),
	}
	if p.Purpose == OIDCPurposeLink {
		if err := s.verifyLinkPassword(ctx, p); err != nil {
			return OIDCFlow{}, nil, nil, err
		}
		flow.UserID, flow.SessionID = p.Principal.UserID, p.Session.ID
	}
	var state, browser, nonce, verifier [32]byte
	for _, b := range [][]byte{state[:], browser[:], nonce[:], verifier[:]} {
		if err := s.random(b); err != nil {
			return OIDCFlow{}, nil, nil, ErrOIDCUnavailable
		}
	}
	flow.StateHash = digest(state[:])
	flow.BrowserTokenHash = digest(browser[:])
	if validToken(p.PreviousBrowserToken) {
		previous, _ := base64.RawURLEncoding.DecodeString(p.PreviousBrowserToken)
		flow.PreviousBrowserTokenHash = digest(previous)
	}
	flow.Nonce = base64.RawURLEncoding.EncodeToString(nonce[:])
	flow.PKCEVerifier = base64.RawURLEncoding.EncodeToString(verifier[:])
	flow.ExpiresAt = flow.CreatedAt.Add(10 * time.Minute)
	return flow, state[:], browser[:], nil
}
func (s *OIDCService) verifyLinkPassword(ctx context.Context, p OIDCStartParams) error {
	if p.Principal.UserID == "" || p.Session.ID == "" || p.Session.Principal.UserID != p.Principal.UserID {
		return ErrUnauthenticated
	}
	if len(p.CurrentPassword) == 0 || len(p.CurrentPassword) > maxPasswordBytes {
		return ErrInvalidCredentials
	}
	hash, err := s.repo.FindCredential(ctx, p.Principal.UserID)
	if err != nil {
		if dummyErr := s.verifyPassword(s.dummyPasswordHash, p.CurrentPassword); errors.Is(dummyErr, ErrPasswordWorkLimit) {
			return ErrPasswordWorkLimit
		}
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidCredentials
		}
		return ErrOIDCUnavailable
	}
	return s.verifyPassword(hash, p.CurrentPassword)
}
func (s *OIDCService) authorizeAndPersist(
	ctx context.Context,
	p OIDCStartParams,
	flow OIDCFlow,
	state, browser []byte,
) (OIDCStartResult, error) {
	authParams := OIDCAuthorizationParams{
		Issuer: s.issuer, ClientID: s.clientID, RedirectURL: s.callbackURL(),
		State: base64.RawURLEncoding.EncodeToString(state), Nonce: flow.Nonce,
		PKCEVerifier: flow.PKCEVerifier, Purpose: p.Purpose,
	}
	auth, err := s.provider.AuthorizationURL(ctx, authParams)
	if err != nil {
		return OIDCStartResult{}, ErrOIDCUnavailable
	}
	if err = s.repo.InsertFlow(ctx, flow); err != nil {
		return OIDCStartResult{}, errors.New("OIDC persistence failed")
	}
	return OIDCStartResult{auth, base64.RawURLEncoding.EncodeToString(browser)}, nil
}
func invalidCallback() OIDCCallbackResult {
	return OIDCCallbackResult{Destination: "/login#oidcError=invalid_flow", Code: OIDCInvalidFlow}
}

type parsedCallback struct {
	state, browser              []byte
	code, providerError, issuer string
}

func parseCallback(params url.Values, browserToken string) (parsedCallback, bool) {
	for k, vs := range params {
		max := 4096
		if k == "state" || k == "iss" {
			max = 2048
		}
		if k == "error" {
			max = 128
		}
		if len(vs) != 1 || len(vs[0]) > max {
			return parsedCallback{}, false
		}
	}
	state := params.Get("state")
	if len(state) != 43 || len(browserToken) != 43 {
		return parsedCallback{}, false
	}
	stateRaw, stateOK := decodeToken(state)
	browserRaw, browserOK := decodeToken(browserToken)
	if !stateOK || !browserOK {
		return parsedCallback{}, false
	}
	return parsedCallback{
		state: stateRaw, browser: browserRaw, code: params.Get("code"),
		providerError: params.Get("error"), issuer: params.Get("iss"),
	}, true
}
func (s *OIDCService) checkCallbackSession(
	ctx context.Context,
	flow OIDCFlow,
	token string,
) (OIDCCallbackResult, bool) {
	if flow.Purpose == OIDCPurposeLogin && token != "" {
		_, err := s.repo.FindSession(ctx, digest([]byte(token)), s.clock().UTC())
		if err == nil {
			return failure(flow.Purpose, OIDCSessionChanged), true
		}
		if !errors.Is(err, ErrNotFound) {
			return failure(flow.Purpose, OIDCUnavailable), true
		}
	}
	if flow.Purpose == OIDCPurposeLink {
		active, err := s.repo.FindSession(ctx, digest([]byte(token)), s.clock().UTC())
		if errors.Is(err, ErrNotFound) {
			return failure(flow.Purpose, OIDCSessionChanged), true
		}
		if err != nil {
			return failure(flow.Purpose, OIDCUnavailable), true
		}
		if token == "" || active.Principal.UserID != flow.UserID || active.ID != flow.SessionID {
			return failure(flow.Purpose, OIDCSessionChanged), true
		}
	}
	return OIDCCallbackResult{}, false
}
func (s *OIDCService) exchangeCallback(
	ctx context.Context,
	flow OIDCFlow,
	cb parsedCallback,
) (OIDCVerifiedIdentity, error) {
	params := OIDCExchangeParams{
		Code: cb.code, RedirectURL: s.callbackURL(), PKCEVerifier: flow.PKCEVerifier,
		Nonce: flow.Nonce, CallbackIssuer: cb.issuer,
	}
	return s.provider.Exchange(ctx, params)
}
func (s *OIDCService) callbackUser(ctx context.Context, flow OIDCFlow, v OIDCVerifiedIdentity) (string, error) {
	user, err := s.repo.FindIdentity(ctx, v.Issuer, v.Subject)
	if flow.Purpose == OIDCPurposeLink {
		return flow.UserID, nil
	}
	return user, err
}
func exchangeFailureCode(err error) OIDCCode {
	if errors.Is(err, ErrOIDCProviderUnavailable) {
		return OIDCUnavailable
	}
	return OIDCAuthenticationFailed
}
func identityFailureCode(err error) OIDCCode {
	if errors.Is(err, ErrNotFound) {
		return OIDCIdentityNotLinked
	}
	return OIDCUnavailable
}
func (s *OIDCService) issueCallbackSession(ctx context.Context, flow OIDCFlow, v OIDCVerifiedIdentity, userID, token string) (OIDCCallbackResult, error) {
	var raw [32]byte
	if err := s.random(raw[:]); err != nil {
		return failure(flow.Purpose, OIDCUnavailable), nil
	}
	sessionToken := base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.clock().UTC().Truncate(time.Second)
	ns := NewSession{UserID: userID, TokenHash: digest([]byte(sessionToken)), CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}
	var err error
	if flow.Purpose == OIDCPurposeLink {
		_, err = s.repo.LinkAndCreateSession(ctx, flow, v, ns, digest([]byte(token)))
	} else {
		_, err = s.repo.CreateOIDCLoginSession(ctx, ns)
	}
	if err != nil {
		code := OIDCUnavailable
		if errors.Is(err, ErrOIDCConflict) {
			code = OIDCIdentityConflict
		} else if errors.Is(err, ErrOIDCSessionChanged) {
			code = OIDCSessionChanged
		}
		return failure(flow.Purpose, code), nil
	}
	dest := "/"
	if flow.Purpose == OIDCPurposeLink {
		dest = "/account#oidc=connected"
	}
	return OIDCCallbackResult{Destination: dest, SessionToken: sessionToken}, nil
}
func (s *OIDCService) callbackURL() string {
	return strings.TrimRight(s.publicURL, "/") + "/api/v1/auth/oidc/callback"
}
