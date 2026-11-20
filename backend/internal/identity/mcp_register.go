package identity

import (
	"context"
	"errors"
	"time"
)

// MCPRegisteredClient is what the registration endpoint returns (RFC 7591 §3.2).
// The response fields are server policy: the client is always a public client
// with the fixed grant and response types and scope.
type MCPRegisteredClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	IssuedAt     time.Time
}

// RegisterClient registers a public OAuth client (RFC 7591) after the request
// passes the redirect and metadata policy. A disabled service refuses before
// any work, and a full grantless-client cap refuses without writing.
func (s *MCPService) RegisterClient(ctx context.Context, reg MCPClientRegistration) (MCPRegisteredClient, error) {
	if !s.enabled {
		return MCPRegisteredClient{}, ErrMCPDisabled
	}
	valid, err := ValidateClientRegistration(reg, s.publicHost)
	if err != nil {
		return MCPRegisteredClient{}, err
	}
	if s.clientStore == nil {
		// Fail closed: a repository without the cap cannot register clients.
		return MCPRegisteredClient{}, errors.New("mcp client store unavailable")
	}
	id, err := newMCPClientID()
	if err != nil {
		return MCPRegisteredClient{}, err
	}
	now := s.now()
	inserted, err := s.clientStore.CreateClientWithinCap(ctx, MCPClient{
		ID: id, Name: valid.Name, RedirectURIs: valid.RedirectURIs,
	}, now, MCPRegistrationCap)
	if err != nil {
		return MCPRegisteredClient{}, err
	}
	if !inserted {
		return MCPRegisteredClient{}, ErrMCPRegistrationFull
	}
	return MCPRegisteredClient{
		ID: id, Name: valid.Name, RedirectURIs: valid.RedirectURIs, IssuedAt: now,
	}, nil
}
