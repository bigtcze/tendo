package oidcprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strings"
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
	} else {
		copy := *client
		client = &copy
		if client.Timeout == 0 {
			client.Timeout = 10 * time.Second
		}
	}
	priorCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("OIDC redirect limit exceeded")
		}
		if len(via) > 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && strings.EqualFold(req.URL.Scheme, "http") {
			return errors.New("OIDC HTTPS downgrade refused")
		}
		if priorCheck != nil {
			return priorCheck(req, via)
		}
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	return &Provider{issuer: issuer, clientID: clientID, clientSecret: secret, client: client, clock: clock, allowHTTP: allowHTTP}
}

type discoveryClaims struct {
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	ResponseTypesSupported []string `json:"response_types_supported"`
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
		return nil, identity.ErrOIDCProviderUnavailable
	}
	var claims discoveryClaims
	if err = provider.Claims(&claims); err != nil {
		return nil, identity.ErrOIDCProviderUnavailable
	}
	for _, endpoint := range []string{claims.AuthorizationEndpoint, claims.TokenEndpoint, claims.JWKSURI} {
		if endpoint == "" || validateEndpoint(endpoint, p.allowHTTP) != nil {
			return nil, identity.ErrOIDCProviderUnavailable
		}
	}
	if len(claims.ResponseTypesSupported) > 0 {
		found := false
		for _, responseType := range claims.ResponseTypesSupported {
			if responseType == "code" {
				found = true
				break
			}
		}
		if !found {
			return nil, identity.ErrOIDCProviderUnavailable
		}
	}
	p.discovered = provider
	return provider, nil
}

func validateEndpoint(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Scheme == "http" && !allowHTTP) {
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
		return "", identity.ErrOIDCAuthenticationFailed
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
	if x.CallbackIssuer != "" && x.CallbackIssuer != p.issuer {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	provider, err := p.discovery(ctx)
	if err != nil {
		return identity.OIDCVerifiedIdentity{}, err
	}
	ctx = oidc.ClientContext(ctx, p.client)
	cfg := oauth2.Config{ClientID: p.clientID, ClientSecret: p.clientSecret, Endpoint: provider.Endpoint(), RedirectURL: x.RedirectURL, Scopes: []string{oidc.ScopeOpenID}}
	tok, err := cfg.Exchange(ctx, x.Code, oauth2.SetAuthURLParam("code_verifier", x.PKCEVerifier))
	if err != nil {
		var retrieve *oauth2.RetrieveError
		if errors.As(err, &retrieve) && retrieve.Response != nil && retrieve.Response.StatusCode >= 400 && retrieve.Response.StatusCode < 500 {
			return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
		}
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCProviderUnavailable
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	id, err := provider.Verifier(&oidc.Config{ClientID: p.clientID, Now: p.clock}).Verify(ctx, raw)
	if err != nil {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	if id.Nonce != x.Nonce || id.Issuer != p.issuer || id.Subject == "" || len(id.Subject) > 255 || id.IssuedAt.After(p.clock().Add(time.Minute)) {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	var claims struct {
		Azp      string       `json:"azp"`
		IssuedAt *json.Number `json:"iat"`
	}
	if id.Claims(&claims) != nil || claims.IssuedAt == nil {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	issuedAt, parseErr := claims.IssuedAt.Float64()
	if parseErr != nil {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	if math.IsNaN(issuedAt) || math.IsInf(issuedAt, 0) || issuedAt < 0 || issuedAt > float64(p.clock().Add(time.Minute).Unix()) {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	if len(id.Audience) > 1 && claims.Azp != p.clientID || claims.Azp != "" && claims.Azp != p.clientID {
		return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
	}
	for _, r := range id.Subject {
		if r < 0x21 || r > 0x7e {
			return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCAuthenticationFailed
		}
	}
	return identity.OIDCVerifiedIdentity{Issuer: id.Issuer, Subject: id.Subject}, nil
}

// RedirectPolicy is exposed for focused verification of transport downgrade protection.
func (p *Provider) RedirectPolicy() func(*http.Request, []*http.Request) error {
	return p.client.CheckRedirect
}
