package identity

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Slice A3a (ADR 0023, "A3a: authorization, consent decision, token and
// revocation"). This file is the shared contract between the service core and
// its HTTP adapter. The browser-facing authorization request is stored as a
// pending consent: only the SHA-256 digests of its handle and of its browser
// binding are kept, it is single use, and it lives MCPAuthorizationRequestLifetime.

const (
	// MCPAuthorizationRequestLifetime bounds a pending consent. The database
	// CHECK allows at most 10 minutes.
	MCPAuthorizationRequestLifetime = 5 * time.Minute
	// MCPMaxStateBytes bounds the client's opaque state (RFC 6749 VSCHAR).
	MCPMaxStateBytes = 1024
)

var (
	// ErrMCPInvalidScope is an authorization request whose known scopes are
	// empty after unknown scopes are dropped, or a refresh request that names a
	// scope set different from the grant's (narrowing is not supported).
	ErrMCPInvalidScope = errors.New("mcp invalid scope")
	// ErrMCPInvalidTarget is a resource indicator (RFC 8707) that is not the
	// single configured resource, or a repeated resource parameter.
	ErrMCPInvalidTarget = errors.New("mcp invalid target")
	// ErrMCPAuthorizationRequestNotFound is the single outcome for an unknown,
	// expired, consumed or differently bound pending consent. It never says which.
	ErrMCPAuthorizationRequestNotFound = errors.New("mcp authorization request not found")
)

// MCPAuthorizationParams is a syntactically parsed /oauth/authorize request.
// The adapter has already refused duplicate recognized parameters and a
// response_type other than "code". Empty strings mean absent.
type MCPAuthorizationParams struct {
	ClientID, RedirectURI, CodeChallenge, CodeChallengeMethod string
	// Scopes is the space-split scope parameter. Nil or empty means absent:
	// both Tendo scopes are requested. Unknown scopes are dropped; an empty
	// intersection is ErrMCPInvalidScope.
	Scopes []string
	// Resource is empty when absent (the configured resource applies);
	// otherwise it must equal the configured resource exactly.
	Resource string
	// State is the client's opaque state, empty when absent.
	State string
}

// MCPPendingAuthorization is a stored pending consent. Handle and
// BrowserBinding are the only plaintext copies: the handle goes to the SPA in
// the URL fragment, the binding to the browser in an HttpOnly cookie.
type MCPPendingAuthorization struct {
	Handle, BrowserBinding string
	ExpiresAt              time.Time
}

// MCPAuthorizationRequestRecord is a stored pending consent.
type MCPAuthorizationRequestRecord struct {
	RequestHash, BindingHash                                          []byte
	ClientID, ClientName, RedirectURI, CodeChallenge, Resource, State string
	Scopes                                                            []string
	CreatedAt, ExpiresAt                                              time.Time
}

// MCPAuthorizationRequestView is what the consent screen shows.
type MCPAuthorizationRequestView struct {
	ClientName, RedirectURI string
	Scopes                  []string
	ExpiresAt               time.Time
}

// MCPDecision is the signed-in human's answer to a pending consent.
type MCPDecision struct {
	Principal              Principal
	SessionToken           string
	Handle, BrowserBinding string
	Approve                bool
}

// MCPDecisionResult is what the adapter turns into the redirect back to the
// client. Code is empty for a denial. State is empty when the client sent none.
type MCPDecisionResult struct {
	RedirectURI, Code, State string
}

// MCPTokenRevocation is an RFC 7009 revocation request from a public client.
type MCPTokenRevocation struct {
	Token, ClientID string
}

// MCPAuthorizationTransaction is the A3a part of MCPTransaction. Lock order
// (ADR 0023): user_accounts -> local_credentials -> user_sessions ->
// mcp_clients (FOR KEY SHARE) -> mcp_authorization_requests ->
// mcp_authorization_codes -> mcp_grants -> token rows.
type MCPAuthorizationTransaction interface {
	// CreateAuthorizationRequest inserts a pending consent. The caller already
	// holds the client FOR KEY SHARE. A foreign-key failure is ErrMCPInvalidClient.
	CreateAuthorizationRequest(ctx context.Context, request MCPAuthorizationRequestRecord) error
	// FindAuthorizationRequest is an unlocked read of an unexpired pending
	// consent with this handle digest and binding digest, joined with the client
	// name. Anything else is ErrNotFound.
	FindAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (MCPAuthorizationRequestRecord, error)
	// ConsumeAuthorizationRequest deletes and returns an unexpired pending
	// consent with this handle digest and binding digest (DELETE ... RETURNING).
	// A wrong binding deletes nothing. Anything else is ErrNotFound.
	ConsumeAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (MCPAuthorizationRequestRecord, error)
	// FindTokenGrantOwner is the unlocked pre-read for revocation: the account
	// and grant of an access token or of a refresh token, consumed or not.
	FindTokenGrantOwner(ctx context.Context, tokenHash []byte) (userID, grantID string, err error)
}

// BeginAuthorization validates an authorization request and stores it as a
// pending consent. It never redirects anywhere; every refusal is shown by Tendo
// itself. Refusals: ErrMCPInvalidRequest, ErrMCPInvalidScope,
// ErrMCPInvalidTarget, ErrMCPInvalidClient, ErrMCPInvalidRedirectURI.
func (s *MCPService) BeginAuthorization(ctx context.Context, in MCPAuthorizationParams) (MCPPendingAuthorization, error) {
	if !s.enabled {
		return MCPPendingAuthorization{}, ErrMCPDisabled
	}
	scopes, err := s.validateAuthorizationParams(in)
	if err != nil {
		return MCPPendingAuthorization{}, err
	}
	handle, requestHash, err := newMCPSecret()
	if err != nil {
		return MCPPendingAuthorization{}, err
	}
	binding, bindingHash, err := newMCPSecret()
	if err != nil {
		return MCPPendingAuthorization{}, err
	}
	now := s.now()
	record := MCPAuthorizationRequestRecord{
		RequestHash: requestHash, BindingHash: bindingHash, ClientID: in.ClientID,
		RedirectURI: in.RedirectURI, CodeChallenge: in.CodeChallenge, Scopes: scopes,
		Resource: s.resource, State: in.State,
		CreatedAt: now, ExpiresAt: now.Add(MCPAuthorizationRequestLifetime),
	}
	err = s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		return storeAuthorizationRequest(ctx, tx, in, record)
	})
	if err != nil {
		return MCPPendingAuthorization{}, err
	}
	return MCPPendingAuthorization{Handle: handle, BrowserBinding: binding, ExpiresAt: record.ExpiresAt}, nil
}

// validateAuthorizationParams applies every check that needs no database read
// and returns the normalized scopes. The endpoint is unauthenticated, so every
// field is checked here (ADR 0023).
func (s *MCPService) validateAuthorizationParams(in MCPAuthorizationParams) ([]string, error) {
	if in.ClientID == "" || in.RedirectURI == "" || in.CodeChallengeMethod != "S256" ||
		!mcpSecretPattern.MatchString(in.CodeChallenge) || !validAuthorizationState(in.State) {
		return nil, ErrMCPInvalidRequest
	}
	if in.Resource != "" && in.Resource != s.resource {
		return nil, ErrMCPInvalidTarget
	}
	scopes, err := requestedAuthorizationScopes(in.Scopes)
	if err != nil {
		return nil, err
	}
	if ValidateRedirectURI(in.RedirectURI, s.publicHost) != nil {
		return nil, ErrMCPInvalidRedirectURI
	}
	return scopes, nil
}

// storeAuthorizationRequest checks the registered redirect and inserts the
// pending consent. No account lock: the request is unauthenticated. The client
// row is locked FOR KEY SHARE so a concurrent deletion cannot leave a request
// behind it.
func storeAuthorizationRequest(ctx context.Context, tx MCPTransaction, in MCPAuthorizationParams, record MCPAuthorizationRequestRecord) error {
	client, err := tx.FindClient(ctx, in.ClientID)
	if errors.Is(err, ErrNotFound) {
		return ErrMCPInvalidClient
	} else if err != nil {
		return err
	}
	if !MCPRedirectURIMatches(client.RedirectURIs, in.RedirectURI) {
		return ErrMCPInvalidRedirectURI
	}
	return tx.CreateAuthorizationRequest(ctx, record)
}

// validAuthorizationState admits an absent state or 1..MCPMaxStateBytes bytes
// of printable ASCII (RFC 6749 VSCHAR). Control and non-ASCII bytes would reach
// the consent page and the redirect unchanged.
func validAuthorizationState(state string) bool {
	if state == "" {
		return true
	}
	if len(state) > MCPMaxStateBytes {
		return false
	}
	for i := 0; i < len(state); i++ {
		if state[i] < 0x20 || state[i] > 0x7E {
			return false
		}
	}
	return true
}

// requestedAuthorizationScopes applies the authorize scope rule: absent means
// both Tendo scopes; otherwise unknown scopes are dropped, the rest are
// deduplicated and sorted, and an empty result is ErrMCPInvalidScope.
func requestedAuthorizationScopes(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return []string{MCPScopeRead, MCPScopeWrite}, nil
	}
	scopes := make([]string, 0, 2)
	for _, scope := range requested {
		if (scope == MCPScopeRead || scope == MCPScopeWrite) && !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, ErrMCPInvalidScope
	}
	slices.Sort(scopes)
	return scopes, nil
}

// GetAuthorizationRequest returns the consent screen view of a pending consent
// bound to this browser. Refusal: ErrMCPAuthorizationRequestNotFound.
func (s *MCPService) GetAuthorizationRequest(ctx context.Context, handle, browserBinding string) (MCPAuthorizationRequestView, error) {
	if !s.enabled {
		return MCPAuthorizationRequestView{}, ErrMCPDisabled
	}
	if !mcpSecretPattern.MatchString(handle) || !mcpSecretPattern.MatchString(browserBinding) {
		return MCPAuthorizationRequestView{}, ErrMCPAuthorizationRequestNotFound
	}
	var view MCPAuthorizationRequestView
	err := s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		rec, e := tx.FindAuthorizationRequest(ctx, tokenDigest(handle), tokenDigest(browserBinding), s.now())
		if errors.Is(e, ErrNotFound) {
			return ErrMCPAuthorizationRequestNotFound
		} else if e != nil {
			return e
		}
		view = MCPAuthorizationRequestView{
			ClientName: rec.ClientName, RedirectURI: rec.RedirectURI,
			Scopes: slices.Clone(rec.Scopes), ExpiresAt: rec.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return MCPAuthorizationRequestView{}, err
	}
	return view, nil
}

// DecideAuthorization consumes a pending consent bound to this browser and, on
// approval, issues a single-use code for the signed-in account. Approval and
// denial both consume it. Refusals: ErrUnauthenticated,
// ErrMCPAuthorizationRequestNotFound.
func (s *MCPService) DecideAuthorization(ctx context.Context, in MCPDecision) (MCPDecisionResult, error) {
	if !s.enabled {
		return MCPDecisionResult{}, ErrMCPDisabled
	}
	if in.Principal.UserID == "" || in.SessionToken == "" {
		return MCPDecisionResult{}, ErrUnauthenticated
	}
	if !mcpSecretPattern.MatchString(in.Handle) || !mcpSecretPattern.MatchString(in.BrowserBinding) {
		return MCPDecisionResult{}, ErrMCPAuthorizationRequestNotFound
	}
	now := s.now()
	var result MCPDecisionResult
	err := s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		var e error
		result, e = decideLocked(ctx, tx, in, now)
		return e
	})
	if err != nil {
		return MCPDecisionResult{}, err
	}
	return result, nil
}

// decideLocked runs the decision under the transaction. Lock order (ADR 0023):
// account, then the session that approves, then the client, then the pending
// consent. A refusal returns an error, so nothing commits unless the decision
// itself succeeds.
func decideLocked(ctx context.Context, tx MCPTransaction, in MCPDecision, now time.Time) (MCPDecisionResult, error) {
	if err := lockApprovingSession(ctx, tx, in, now); err != nil {
		return MCPDecisionResult{}, err
	}
	requestHash, bindingHash := tokenDigest(in.Handle), tokenDigest(in.BrowserBinding)
	// The unlocked pre-read only names the client to lock; the consume below
	// is the authoritative single-use step.
	pending, err := tx.FindAuthorizationRequest(ctx, requestHash, bindingHash, now)
	if errors.Is(err, ErrNotFound) {
		return MCPDecisionResult{}, ErrMCPAuthorizationRequestNotFound
	} else if err != nil {
		return MCPDecisionResult{}, err
	}
	client, err := tx.FindClient(ctx, pending.ClientID)
	if errors.Is(err, ErrNotFound) {
		return MCPDecisionResult{}, ErrMCPAuthorizationRequestNotFound
	} else if err != nil {
		return MCPDecisionResult{}, err
	}
	rec, err := tx.ConsumeAuthorizationRequest(ctx, requestHash, bindingHash, now)
	if errors.Is(err, ErrNotFound) {
		// Lost a race to a concurrent decision.
		return MCPDecisionResult{}, ErrMCPAuthorizationRequestNotFound
	} else if err != nil {
		return MCPDecisionResult{}, err
	}
	if !in.Approve {
		// Denial commits the consumption and issues no code.
		return MCPDecisionResult{RedirectURI: rec.RedirectURI, State: rec.State}, nil
	}
	return approveLocked(ctx, tx, in.Principal.UserID, client, rec, now)
}

// lockApprovingSession locks the signed-in account and proves the approving
// session still exists under it. Either failure is ErrUnauthenticated.
func lockApprovingSession(ctx context.Context, tx MCPTransaction, in MCPDecision, now time.Time) error {
	if err := tx.LockLiveAccount(ctx, in.Principal.UserID); errors.Is(err, ErrNotFound) {
		return ErrUnauthenticated
	} else if err != nil {
		return err
	}
	active, err := tx.ActiveSessionExists(ctx, in.Principal.UserID, tokenDigest(in.SessionToken), now)
	if err != nil {
		return err
	}
	if !active {
		return ErrUnauthenticated
	}
	return nil
}

// approveLocked issues the code for a consumed consent. The client may have
// dropped the registration after the consent began; the match is repeated
// here, and a mismatch rolls back the consumption.
func approveLocked(ctx context.Context, tx MCPTransaction, userID string, client MCPClient, rec MCPAuthorizationRequestRecord, now time.Time) (MCPDecisionResult, error) {
	if !MCPRedirectURIMatches(client.RedirectURIs, rec.RedirectURI) {
		return MCPDecisionResult{}, ErrMCPAuthorizationRequestNotFound
	}
	code, record, err := newAuthorizationCode(userID, rec.ClientID, rec.RedirectURI,
		rec.CodeChallenge, rec.Resource, rec.Scopes, now)
	if err != nil {
		return MCPDecisionResult{}, err
	}
	if err = tx.CreateAuthorizationCode(ctx, record); err != nil {
		return MCPDecisionResult{}, err
	}
	return MCPDecisionResult{RedirectURI: rec.RedirectURI, Code: code, State: rec.State}, nil
}

// RevokeToken revokes the whole grant of an access or refresh token presented
// by the client that holds it (RFC 7009, reason "client"). An unknown,
// malformed, foreign or already revoked token is a silent success.
func (s *MCPService) RevokeToken(ctx context.Context, in MCPTokenRevocation) error {
	if !s.enabled {
		return ErrMCPDisabled
	}
	// A malformed token or missing client cannot name a grant: silent success,
	// with no repository call (RFC 7009 section 2.2).
	if !mcpSecretPattern.MatchString(in.Token) || in.ClientID == "" {
		return nil
	}
	return s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		owner, grantID, e := tx.FindTokenGrantOwner(ctx, tokenDigest(in.Token))
		if errors.Is(e, ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		if e = tx.LockLiveAccount(ctx, owner); errors.Is(e, ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		grant, e := tx.LockGrant(ctx, grantID)
		if errors.Is(e, ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		// Another client's token, or a grant that already ended, is a silent
		// success: the caller learns nothing about other clients' grants.
		if grant.Revoked || grant.ClientID != in.ClientID {
			return nil
		}
		// The token holder is a client, not a signed-in human: no actor.
		return tx.RevokeGrant(ctx, grantID, s.now(), MCPRevocation{Reason: MCPRevokeClient})
	})
}

// Resource is the configured protected resource URI (<public URL>/mcp).
func (s *MCPService) Resource() string { return s.resource }
