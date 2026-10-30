package oidcprovider

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
)

// These cases pin Exchange's early-return order where two independent failures
// are present. The detailed single-fault classification is covered by
// TestProviderValidation, TestJWKSUnavailableIsProviderUnavailable and
// TestTokenEndpointErrorsAreTyped.
func TestExchangeValidationOrder(t *testing.T) {
	fake, err := testoidc.New()
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()

	t.Run("callback issuer before discovery", func(t *testing.T) {
		fake.Faults.DiscoveryUnavailable = true
		defer func() { fake.Faults.DiscoveryUnavailable = false }()
		p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: time.Second}, time.Now, true)
		_, err := p.Exchange(context.Background(), identity.OIDCExchangeParams{
			CallbackIssuer: "https://wrong-issuer.example",
			Code:           "unused",
		})
		if !errors.Is(err, identity.ErrOIDCAuthenticationFailed) {
			t.Fatalf("error=%v, want authentication failed before discovery", err)
		}
	})

	t.Run("discovery before token exchange", func(t *testing.T) {
		fake.Faults.DiscoveryUnavailable = true
		defer func() { fake.Faults.DiscoveryUnavailable = false }()
		p := New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: time.Second}, time.Now, true)
		_, err := p.Exchange(context.Background(), identity.OIDCExchangeParams{Code: "unused"})
		if !errors.Is(err, identity.ErrOIDCProviderUnavailable) {
			t.Fatalf("error=%v, want discovery provider unavailable", err)
		}
	})
}
