package oidcprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
)

func TestProviderValidation(t *testing.T) {
	cases := []struct {
		name  string
		fault testoidc.Faults
	}{
		{name: "bad signature", fault: testoidc.Faults{BadSignature: true}},
		{name: "wrong issuer", fault: testoidc.Faults{WrongIssuer: true}},
		{name: "wrong audience", fault: testoidc.Faults{WrongAudience: true}},
		{name: "wrong azp", fault: testoidc.Faults{WrongAZP: true}},
		{name: "multi audience missing azp", fault: testoidc.Faults{MultiAudienceNoAZP: true}},
		{name: "expired", fault: testoidc.Faults{Expired: true}},
		{name: "future iat", fault: testoidc.Faults{FutureIssuedAt: true}},
		{name: "missing iat", fault: testoidc.Faults{MissingIssuedAt: true}},
		{name: "string iat", fault: testoidc.Faults{StringIssuedAt: true}},
		{name: "nonce mismatch", fault: testoidc.Faults{NonceMismatch: true}},
		{name: "missing id token", fault: testoidc.Faults{MissingIDToken: true}},
		{name: "empty subject", fault: testoidc.Faults{SubjectEmpty: true}},
		{name: "non-ascii subject", fault: testoidc.Faults{SubjectNonASCII: true}},
		{name: "long subject", fault: testoidc.Faults{SubjectTooLong: true}},
		{name: "disallowed algorithm", fault: testoidc.Faults{DisallowedAlgorithm: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := testoidc.New()
			if err != nil {
				t.Fatal(err)
			}
			defer fake.Close()
			fake.Faults = tc.fault
			p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: 2 * time.Second}, time.Now, true)
			code, authURL := authorize(t, p, fake, identity.OIDCPurposeLogin)
			_ = authURL
			_, err = p.Exchange(context.Background(), identity.OIDCExchangeParams{Code: code, RedirectURL: fake.Issuer() + "/callback", PKCEVerifier: "verifier-value", Nonce: "nonce-value"})
			if !errors.Is(err, identity.ErrOIDCAuthenticationFailed) {
				t.Fatalf("error=%v, want authentication failure", err)
			}
		})
	}
}

func TestProviderSuccessAndAuthorizationParameters(t *testing.T) {
	fake, err := testoidc.New()
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, nil, nil, true)
	verifier := "verifier-value"
	for _, purpose := range []identity.OIDCPurpose{identity.OIDCPurposeLogin, identity.OIDCPurposeLink} {
		a := identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID, RedirectURL: fake.Issuer() + "/callback", State: "independent-state", Nonce: "fresh-nonce", PKCEVerifier: verifier, Purpose: purpose}
		auth, err := p.AuthorizationURL(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(auth)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		sum := sha256.Sum256([]byte(verifier))
		if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid" || q.Get("response_type") != "code" || q.Get("nonce") != a.Nonce || q.Get("state") != a.State || q.Get("redirect_uri") != a.RedirectURL {
			t.Fatalf("authorization params=%v", q)
		}
		if purpose == identity.OIDCPurposeLink && q.Get("prompt") != "login" {
			t.Fatalf("link prompt=%q", q.Get("prompt"))
		}
		if purpose == identity.OIDCPurposeLogin && q.Has("prompt") {
			t.Fatalf("login prompt unexpectedly present: %v", q)
		}
	}
	code, _ := authorize(t, p, fake, identity.OIDCPurposeLogin)
	verified, err := p.Exchange(context.Background(), identity.OIDCExchangeParams{Code: code, RedirectURL: fake.Issuer() + "/callback", PKCEVerifier: "verifier-value", Nonce: "nonce-value"})
	if err != nil || verified.Issuer != fake.Issuer() || verified.Subject != "subject-1" {
		t.Fatalf("identity=%+v err=%v", verified, err)
	}
}

func TestDiscoveryRequirements(t *testing.T) {
	cases := []struct {
		name      string
		fault     testoidc.Faults
		allowHTTP bool
	}{
		{name: "HTTP JWKS rejected in HTTPS mode", fault: testoidc.Faults{SecureAuthEndpoints: true, WrongJWKSHTTP: true}},
		{name: "missing authorization endpoint", fault: testoidc.Faults{MissingAuthEndpoint: true}, allowHTTP: true},
		{name: "missing token endpoint", fault: testoidc.Faults{MissingTokenEndpoint: true}, allowHTTP: true},
		{name: "missing JWKS URI", fault: testoidc.Faults{MissingJWKSURI: true}, allowHTTP: true},
		{name: "response types omit code", fault: testoidc.Faults{NoCodeResponseType: true}, allowHTTP: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := testoidc.New()
			if err != nil {
				t.Fatal(err)
			}
			defer fake.Close()
			fake.Faults = tc.fault
			p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, nil, nil, tc.allowHTTP)
			_, err = p.AuthorizationURL(context.Background(), identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID})
			if !errors.Is(err, identity.ErrOIDCProviderUnavailable) {
				t.Fatalf("error=%v, want provider unavailable", err)
			}
		})
	}
}

func TestDiscoveryFailureNotCachedOnSameProvider(t *testing.T) {
	fake, err := testoidc.New()
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	fake.Faults.DiscoveryUnavailable = true
	p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, nil, nil, true)
	params := identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID}
	if _, err = p.AuthorizationURL(context.Background(), params); !errors.Is(err, identity.ErrOIDCProviderUnavailable) {
		t.Fatalf("first discovery error=%v", err)
	}
	fake.Faults.DiscoveryUnavailable = false
	if _, err = p.AuthorizationURL(context.Background(), params); err != nil {
		t.Fatalf("same instance did not recover: %v", err)
	}
}

func TestRedirectPolicyRejectsDowngradeAndCapsRedirects(t *testing.T) {
	p := New("https://issuer.test", "client", "secret", nil, nil, false)
	prev, _ := http.NewRequest(http.MethodGet, "https://issuer.test/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "http://issuer.test/next", nil)
	if err := p.RedirectPolicy()(next, []*http.Request{prev}); err == nil {
		t.Fatal("HTTPS downgrade accepted")
	}
	if err := p.RedirectPolicy()(prev, []*http.Request{prev, prev, prev}); err == nil {
		t.Fatal("redirect limit not enforced")
	}
}

func TestTokenEndpointErrorsAreTyped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fault   testoidc.Faults
		timeout time.Duration
		want    error
	}{{name: "server error", fault: testoidc.Faults{TokenServerError: true}, timeout: time.Second, want: identity.ErrOIDCProviderUnavailable}, {name: "timeout", fault: testoidc.Faults{Delay: 150 * time.Millisecond}, timeout: 20 * time.Millisecond, want: identity.ErrOIDCProviderUnavailable}, {name: "invalid grant", fault: testoidc.Faults{InvalidGrant: true}, timeout: time.Second, want: identity.ErrOIDCAuthenticationFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := testoidc.New()
			if err != nil {
				t.Fatal(err)
			}
			defer fake.Close()
			fake.Faults = tc.fault
			p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: tc.timeout}, time.Now, true)
			code, _ := authorize(t, p, fake, identity.OIDCPurposeLogin)
			_, err = p.Exchange(context.Background(), identity.OIDCExchangeParams{Code: code, RedirectURL: fake.Issuer() + "/callback", PKCEVerifier: "verifier-value", Nonce: "nonce-value"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
		})
	}
}

func authorize(t *testing.T, p *Provider, fake *testoidc.Provider, purpose identity.OIDCPurpose) (string, string) {
	t.Helper()
	verifier := "verifier-value"
	a := identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID, RedirectURL: fake.Issuer() + "/callback", State: "state-value", Nonce: "nonce-value", PKCEVerifier: verifier, Purpose: purpose}
	auth, err := p.AuthorizationURL(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(auth)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("provider returned no code: status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	return code, auth
}
