package oidcprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Provider struct {
	issuer, clientID, clientSecret string
	client                         *http.Client
	clock                          func() time.Time
	allowHTTP                      bool
	mu                             sync.Mutex
	discovered                     *oidc.Provider
}

func New(issuer, clientID, secret string, client *http.Client, clock func() time.Time, allowHTTP bool) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if client.Timeout == 0 {
		client.Timeout = 10 * time.Second
	}
	if clock == nil {
		clock = time.Now
	}
	return &Provider{issuer: issuer, clientID: clientID, clientSecret: secret, client: client, clock: clock, allowHTTP: allowHTTP}
}
func (p *Provider) discovery(ctx context.Context) (*oidc.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered != nil {
		return p.discovered, nil
	}
	ctx = oidc.ClientContext(ctx, p.client)
	provider, err := oidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return nil, errors.New("OIDC provider unavailable")
	}
	for _, endpoint := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, provider.Endpoint().DeviceAuthURL} {
		if endpoint != "" {
			if err = validateEndpoint(endpoint, p.allowHTTP); err != nil {
				return nil, errors.New("OIDC provider unavailable")
			}
		}
	}
	p.discovered = provider
	return provider, nil
}
func validateEndpoint(raw string, allowHTTP bool) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || u.Scheme == "http" && !allowHTTP {
		return errors.New("invalid OIDC endpoint")
	}
	return nil
}
func (p *Provider) AuthorizationURL(ctx context.Context, a identity.OIDCAuthorizationParams) (string, error) {
	provider, err := p.discovery(ctx)
	if err != nil {
		return "", err
	}
	if a.Issuer != p.issuer || a.ClientID != p.clientID {
		return "", errors.New("OIDC configuration mismatch")
	}
	sum := sha256.Sum256([]byte(a.PKCEVerifier))
	cfg := oauth2.Config{ClientID: p.clientID, ClientSecret: p.clientSecret, Endpoint: provider.Endpoint(), RedirectURL: a.RedirectURL, Scopes: []string{oidc.ScopeOpenID}}
	options := []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("nonce", a.Nonce), oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:])), oauth2.SetAuthURLParam("code_challenge_method", "S256"), oauth2.SetAuthURLParam("response_mode", "query")}
	if a.Purpose == identity.OIDCPurposeLink {
		options = append(options, oauth2.SetAuthURLParam("prompt", "login"))
	}
	return cfg.AuthCodeURL(a.State, options...), nil
}
func (p *Provider) Exchange(ctx context.Context, x identity.OIDCExchangeParams) (identity.OIDCVerifiedIdentity, error) {
	if len(x.CallbackIssuer) > 0 && x.CallbackIssuer != p.issuer {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	provider, err := p.discovery(ctx)
	if err != nil {
		return identity.OIDCVerifiedIdentity{}, err
	}
	ctx = oidc.ClientContext(ctx, p.client)
	cfg := oauth2.Config{ClientID: p.clientID, ClientSecret: p.clientSecret, Endpoint: provider.Endpoint(), RedirectURL: x.RedirectURL, Scopes: []string{oidc.ScopeOpenID}}
	tok, err := cfg.Exchange(ctx, x.Code, oauth2.SetAuthURLParam("code_verifier", x.PKCEVerifier))
	if err != nil {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	id, err := provider.Verifier(&oidc.Config{ClientID: p.clientID, Now: p.clock}).Verify(ctx, raw)
	if err != nil {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	if id.Nonce != x.Nonce || id.Issuer != p.issuer || id.Subject == "" || len(id.Subject) > 255 || id.IssuedAt.After(p.clock().Add(time.Minute)) {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	// The aud claim may be a string or an array; go-oidc normalizes it into id.Audience.
	var claims struct {
		Azp string `json:"azp"`
	}
	if id.Claims(&claims) != nil || len(id.Audience) > 1 && claims.Azp != p.clientID || claims.Azp != "" && claims.Azp != p.clientID {
		return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
	}
	for _, r := range id.Subject {
		if r < 0x21 || r > 0x7e {
			return identity.OIDCVerifiedIdentity{}, errors.New("OIDC authentication failed")
		}
	}
	return identity.OIDCVerifiedIdentity{Issuer: id.Issuer, Subject: id.Subject}, nil
}
