package oidcprovider

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
)

func TestProviderValidation(t *testing.T) {
	cases := []struct {
		name           string
		fault          testoidc.Faults
		callbackIssuer string
		success        bool
	}{{name: "success", success: true}, {name: "bad signature", fault: testoidc.Faults{BadSignature: true}}, {name: "wrong issuer", fault: testoidc.Faults{WrongIssuer: true}}, {name: "wrong audience", fault: testoidc.Faults{WrongAudience: true}}, {name: "wrong azp", fault: testoidc.Faults{WrongAZP: true}}, {name: "multi audience missing azp", fault: testoidc.Faults{MultiAudienceNoAZP: true}}, {name: "expired", fault: testoidc.Faults{Expired: true}}, {name: "future iat", fault: testoidc.Faults{FutureIssuedAt: true}}, {name: "nonce mismatch", fault: testoidc.Faults{NonceMismatch: true}}, {name: "missing id token", fault: testoidc.Faults{MissingIDToken: true}}, {name: "token 500", fault: testoidc.Faults{TokenServerError: true}}, {name: "callback iss", callbackIssuer: "https://wrong"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := testoidc.New()
			if err != nil {
				t.Fatal(err)
			}
			defer fake.Close()
			fake.Faults = tc.fault
			p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: 2 * time.Second}, time.Now, true)
			redirect := fake.Issuer() + "/callback"
			v := "verifier-value"
			auth, err := p.AuthorizationURL(context.Background(), identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID, RedirectURL: redirect, State: "state-value", Nonce: "nonce-value", PKCEVerifier: v, Purpose: identity.OIDCPurposeLogin})
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(auth)
			client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Get(u.String())
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			loc, err := url.Parse(resp.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			code := loc.Query().Get("code")
			if code == "" {
				t.Fatalf("authorize status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
			}
			result, err := p.Exchange(context.Background(), identity.OIDCExchangeParams{Code: code, RedirectURL: redirect, PKCEVerifier: v, Nonce: "nonce-value", CallbackIssuer: tc.callbackIssuer})
			if tc.success {
				if err != nil || result.Subject != "subject-1" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("validation failure accepted")
			}
		})
	}
}
func TestDiscoveryFailureNotCachedAndHTTPPolicy(t *testing.T) {
	fake, err := testoidc.New()
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	dead := New("http://127.0.0.1:1", fake.ClientID, fake.ClientSecret, nil, nil, true)
	if _, e := dead.AuthorizationURL(context.Background(), identity.OIDCAuthorizationParams{Issuer: "http://127.0.0.1:1", ClientID: fake.ClientID}); e == nil {
		t.Fatal("expected discovery failure")
	}
	recovering := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, nil, nil, true)
	if _, e := recovering.AuthorizationURL(context.Background(), identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID}); e != nil {
		t.Fatalf("discovery recovery: %v", e)
	}
	secure := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, nil, nil, false)
	if _, e := secure.AuthorizationURL(context.Background(), identity.OIDCAuthorizationParams{Issuer: fake.Issuer(), ClientID: fake.ClientID}); e == nil {
		t.Fatal("http endpoints accepted in https mode")
	}
	_ = strings.TrimSpace
}
