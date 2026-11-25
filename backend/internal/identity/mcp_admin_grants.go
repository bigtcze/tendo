package identity

import "context"

// Slice B1b3 of #457 (ADR 0023 "Slice B1b3: administrator grant list and
// revocation"). An instance administrator lists one account's live MCP grants,
// revokes one of them or revokes all of them. Each revocation is audited with
// the reason admin and the administrator as actor. Like the other policy routes
// these work whether or not TENDO_MCP_ENABLED or the installation switch is on:
// a paused grant can still be listed and revoked.
//
// Every command runs in one policy transaction in the ADR 0023 order (amendment
// 8): installation_state FOR SHARE and the live instance-administrator check,
// then the target account FOR SHARE with its local credential FOR SHARE, then
// the grant's codes, the grant and its token rows.

// AccountGrants returns one page of a live account's live grants to an
// instance administrator. The page and the cursor are those of the connected
// apps list. An invalid cursor is ErrMCPInvalidCursor; a malformed, unknown or
// deleted account is ErrNotFound.
func (s *MCPPolicyService) AccountGrants(ctx context.Context, actorID, userID string, limit int, cursor string) (MCPGrantPage, error) {
	afterID, err := decodeMCPGrantCursor(cursor)
	if err != nil {
		return MCPGrantPage{}, err
	}
	limit = clampMCPGrantLimit(limit)
	now := s.now()
	var out MCPGrantPage
	err = s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := requireAdminFor(ctx, tx, actorID, userID); err != nil {
			return err
		}
		// The account must be live: a deleted account answers 404 like the
		// account policy does, never an empty list.
		if _, _, err := tx.ReadLiveAccountPolicy(ctx, userID); err != nil {
			return err
		}
		// One extra row tells whether another page exists without a count.
		items, err := tx.ListLiveGrants(ctx, userID, now, afterID, limit+1)
		if err != nil {
			return err
		}
		out, err = mcpGrantPage(items, limit)
		return err
	})
	return out, err
}

// RevokeAccountGrant revokes one live grant of the account for an instance
// administrator, with the reason admin and the administrator as actor. Every
// refusal after the administrator check is ErrNotFound: a malformed or unknown
// account or grant, a grant of another account, and a revoked or expired grant
// read the same.
//
// The grant owner is read without a lock, so a grant of another account takes
// no account or grant lock and never queues behind that account.
func (s *MCPPolicyService) RevokeAccountGrant(ctx context.Context, actorID, userID, grantID string) error {
	now := s.now()
	return s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := requireAdminFor(ctx, tx, actorID, userID); err != nil {
			return err
		}
		if !isLowerCanonicalUUID(grantID) {
			return ErrNotFound
		}
		owner, err := tx.FindGrantOwner(ctx, grantID)
		if err != nil {
			return err
		}
		if owner != userID {
			return ErrNotFound
		}
		if err = tx.LockAccountForGrants(ctx, userID); err != nil {
			return err
		}
		grant, err := tx.LockGrant(ctx, grantID)
		if err != nil {
			return err
		}
		// Re-checked under the locks: the grant may have been revoked since the
		// unlocked owner read.
		if grant.UserID != userID || grant.Revoked || !grant.ExpiresAt.After(now) {
			return ErrNotFound
		}
		return tx.RevokeGrant(ctx, grantID, now, MCPRevocation{Reason: MCPRevokeAdmin, ActorUserID: actorID})
	})
}

// RevokeAccountGrants revokes every unrevoked grant of a live account for an
// instance administrator through the same account-wide statement a password
// replacement uses: one admin event per revoked grant, and every code and token
// row of the account deleted. An account with nothing to revoke writes no event.
// A malformed, unknown or deleted account is ErrNotFound.
func (s *MCPPolicyService) RevokeAccountGrants(ctx context.Context, actorID, userID string) error {
	now := s.now()
	return s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := requireAdminFor(ctx, tx, actorID, userID); err != nil {
			return err
		}
		if err := tx.LockAccountForGrants(ctx, userID); err != nil {
			return err
		}
		return tx.RevokeAccountGrants(ctx, userID, now, MCPRevocation{Reason: MCPRevokeAdmin, ActorUserID: actorID})
	})
}

// clampMCPGrantLimit maps zero to the default and caps the page size.
func clampMCPGrantLimit(limit int) int {
	switch {
	case limit <= 0:
		return MCPGrantListDefaultLimit
	case limit > MCPGrantListMaxLimit:
		return MCPGrantListMaxLimit
	}
	return limit
}

// mcpGrantPage cuts a read of limit+1 rows into one page and its cursor.
func mcpGrantPage(items []MCPGrantItem, limit int) (MCPGrantPage, error) {
	if items == nil {
		items = []MCPGrantItem{}
	}
	page := MCPGrantPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next, err := encodeMCPGrantCursor(page.Items[limit-1].ID)
		if err != nil {
			return MCPGrantPage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}
