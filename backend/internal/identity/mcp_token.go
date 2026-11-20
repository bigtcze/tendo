package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"
)

// MCPTokens is a freshly issued access and refresh token pair. The plaintext
// exists only here; the store keeps digests.
type MCPTokens struct {
	AccessToken, RefreshToken string
	AccessExpiresAt           time.Time
	Scopes                    []string
}

// MCPCodeExchange is an authorization_code token request from a public client.
type MCPCodeExchange struct {
	Code, CodeVerifier, ClientID, RedirectURI, Resource string
}

// MCPRefresh is a refresh_token token request from a public client.
type MCPRefresh struct {
	RefreshToken, ClientID, Resource string
}

// ExchangeCode redeems a single-use code. The grant is created here, bound to
// the code's user, client, scopes and resource. A code presented a second time
// revokes the grant it issued (code_reuse); that revocation commits even
// though the request fails.
func (s *MCPService) ExchangeCode(ctx context.Context, in MCPCodeExchange) (MCPTokens, error) {
	if !s.enabled {
		return MCPTokens{}, ErrMCPDisabled
	}
	if !mcpSecretPattern.MatchString(in.Code) || !mcpCodeVerifierPattern.MatchString(in.CodeVerifier) || in.Resource != s.resource {
		return MCPTokens{}, ErrMCPInvalidGrant
	}
	now := s.now()
	codeHash := tokenDigest(in.Code)
	var tokens MCPTokens
	var outcome error
	err := s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		var e error
		tokens, outcome, e = s.exchangeLocked(ctx, tx, in, codeHash, now)
		return e
	})
	if err != nil {
		return MCPTokens{}, err
	}
	if outcome != nil {
		return MCPTokens{}, outcome
	}
	return tokens, nil
}

// exchangeLocked returns (tokens, nil, nil) on success, (_, outcome, nil) for a
// refusal whose writes must still commit, and (_, _, err) to roll back.
func (s *MCPService) exchangeLocked(ctx context.Context, tx MCPTransaction, in MCPCodeExchange, codeHash []byte, now time.Time) (MCPTokens, error, error) {
	owner, err := tx.FindCodeOwner(ctx, codeHash)
	if errors.Is(err, ErrNotFound) {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	if err != nil {
		return MCPTokens{}, nil, err
	}
	if err = tx.LockLiveAccount(ctx, owner); errors.Is(err, ErrNotFound) {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	} else if err != nil {
		return MCPTokens{}, nil, err
	}
	code, err := tx.LockAuthorizationCode(ctx, codeHash)
	if errors.Is(err, ErrNotFound) {
		// Revoked or erased between the pre-read and the lock.
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	if err != nil {
		return MCPTokens{}, nil, err
	}
	if code.Used {
		if _, err = tx.LockGrant(ctx, code.IssuedGrantID); err != nil {
			return MCPTokens{}, nil, err
		}
		return MCPTokens{}, ErrMCPInvalidGrant, tx.RevokeGrant(ctx, code.IssuedGrantID, now, MCPRevocation{Reason: MCPRevokeCodeReuse})
	}
	if !code.redeemableBy(in, now) {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	grantID, err := tx.CreateGrant(ctx, MCPGrantRecord{
		UserID: code.UserID, ClientID: code.ClientID, Resource: code.Resource, Scopes: code.Scopes,
		CreatedAt: now, ExpiresAt: now.Add(MCPGrantLifetime),
	})
	if err != nil {
		return MCPTokens{}, nil, err
	}
	if err = tx.MarkAuthorizationCodeUsed(ctx, codeHash, grantID, now); err != nil {
		return MCPTokens{}, nil, err
	}
	tokens, err := issueMCPTokens(ctx, tx, grantID, code.Scopes, now, now.Add(MCPGrantLifetime))
	return tokens, nil, err
}

// redeemableBy reports whether an unused code is unexpired and the exchange
// presents exactly its client, redirect URI, resource and PKCE verifier.
func (c MCPCodeRecord) redeemableBy(in MCPCodeExchange, now time.Time) bool {
	return c.ExpiresAt.After(now) && c.ClientID == in.ClientID && c.RedirectURI == in.RedirectURI &&
		c.Resource == in.Resource && pkceS256Matches(in.CodeVerifier, c.CodeChallenge)
}

// Refresh rotates a refresh token. The presented token is consumed and a new
// pair is issued for the same grant and scopes. Presenting a consumed token is
// replay: the whole grant is revoked (replay) and that revocation commits.
func (s *MCPService) Refresh(ctx context.Context, in MCPRefresh) (MCPTokens, error) {
	if !s.enabled {
		return MCPTokens{}, ErrMCPDisabled
	}
	if !mcpSecretPattern.MatchString(in.RefreshToken) || in.Resource != s.resource {
		return MCPTokens{}, ErrMCPInvalidGrant
	}
	now := s.now()
	refreshHash := tokenDigest(in.RefreshToken)
	var tokens MCPTokens
	var outcome error
	err := s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		var e error
		tokens, outcome, e = s.refreshLocked(ctx, tx, in, refreshHash, now)
		return e
	})
	if err != nil {
		return MCPTokens{}, err
	}
	if outcome != nil {
		return MCPTokens{}, outcome
	}
	return tokens, nil
}

func (s *MCPService) refreshLocked(ctx context.Context, tx MCPTransaction, in MCPRefresh, refreshHash []byte, now time.Time) (MCPTokens, error, error) {
	owner, grantID, err := tx.FindRefreshOwner(ctx, refreshHash)
	if errors.Is(err, ErrNotFound) {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	if err != nil {
		return MCPTokens{}, nil, err
	}
	if err = tx.LockLiveAccount(ctx, owner); errors.Is(err, ErrNotFound) {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	} else if err != nil {
		return MCPTokens{}, nil, err
	}
	grant, err := tx.LockGrant(ctx, grantID)
	if err != nil {
		return MCPTokens{}, nil, err
	}
	refresh, err := tx.LockRefreshToken(ctx, refreshHash)
	if errors.Is(err, ErrNotFound) || grant.Revoked {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	if err != nil {
		return MCPTokens{}, nil, err
	}
	if refresh.Consumed {
		return MCPTokens{}, ErrMCPInvalidGrant, tx.RevokeGrant(ctx, grantID, now, MCPRevocation{Reason: MCPRevokeReplay})
	}
	if !refresh.ExpiresAt.After(now) || !grant.ExpiresAt.After(now) ||
		grant.ClientID != in.ClientID || grant.Resource != in.Resource {
		return MCPTokens{}, ErrMCPInvalidGrant, nil
	}
	if err = tx.ConsumeRefreshToken(ctx, refreshHash, now); err != nil {
		return MCPTokens{}, nil, err
	}
	tokens, err := issueMCPTokens(ctx, tx, grantID, grant.Scopes, now, grant.ExpiresAt)
	return tokens, nil, err
}

// issueMCPTokens mints and stores a new pair. Neither token outlives the
// grant's absolute expiry.
func issueMCPTokens(ctx context.Context, tx MCPTransaction, grantID string, scopes []string, now, grantExpiresAt time.Time) (MCPTokens, error) {
	access, accessHash, err := newMCPSecret()
	if err != nil {
		return MCPTokens{}, err
	}
	refresh, refreshHash, err := newMCPSecret()
	if err != nil {
		return MCPTokens{}, err
	}
	issue := MCPTokenIssue{
		GrantID: grantID, AccessHash: accessHash, RefreshHash: refreshHash, CreatedAt: now,
		AccessExpiresAt:  earliest(now.Add(MCPAccessTokenLifetime), grantExpiresAt),
		RefreshExpiresAt: earliest(now.Add(MCPRefreshIdleLifetime), grantExpiresAt),
	}
	if err = tx.CreateTokens(ctx, issue); err != nil {
		return MCPTokens{}, err
	}
	return MCPTokens{AccessToken: access, RefreshToken: refresh, AccessExpiresAt: issue.AccessExpiresAt, Scopes: scopes}, nil
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// pkceS256Matches compares BASE64URL(SHA256(verifier)) with the stored
// challenge in constant time (RFC 7636 section 4.6).
func pkceS256Matches(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}
