package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
)

func (s *InvitationService) CreateInvitation(ctx context.Context, actor, householdID, key string) (InvitationResult, error) {
	bounded, cancel := context.WithTimeout(ctx, invitationRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return InvitationResult{}, err
	}
	if !validUUID(actor) || !validUUID(householdID) {
		return InvitationResult{}, household.ErrNotFound
	}
	if key == "" {
		return InvitationResult{}, ErrIdempotencyKeyRequired
	}
	if !ValidCreationKey(key) {
		return InvitationResult{}, ErrInvalidIdempotencyKey
	}
	now := invitationNow(s.clock)
	var result InvitationResult
	err := s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if e := tx.RequireOwner(ctx, actor, householdID); e != nil {
			return e
		}
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			return errors.New("invitation token generation failed")
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		input := CreateInvitationInput{
			HouseholdID: householdID, ActorID: actor, CreationKey: key,
			TokenHash: digestInvitationToken(token), CreatedAt: now,
			ExpiresAt: now.Add(InvitationLifetime),
		}
		inv, created, e := tx.CreateInvitation(ctx, input)
		if e != nil {
			return e
		}
		inv.Status = invitationStatus(inv, now)
		result = InvitationResult{Invitation: inv, Created: created}
		if created {
			result.Token = token
		}
		return nil
	})
	return result, err
}
func encodeInvitationCursor(id string) string {
	return "n1" + base64.RawURLEncoding.EncodeToString([]byte(id))
}
func decodeInvitationCursor(c string) (string, error) {
	if c == "" {
		return "", nil
	}
	if len(c) < 3 || c[:2] != "n1" {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	b, e := base64.RawURLEncoding.DecodeString(c[2:])
	if e != nil || len(b) != 36 || !validUUID(string(b)) {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	return string(b), nil
}
func (s *InvitationService) ListInvitations(ctx context.Context, actor, householdID string, limit int, cursor string) ([]Invitation, string, error) {
	bounded, cancel := context.WithTimeout(ctx, invitationRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if !validUUID(actor) || !validUUID(householdID) {
		return nil, "", household.ErrNotFound
	}
	id, e := decodeInvitationCursor(cursor)
	if e != nil {
		return nil, "", e
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var out []Invitation
	e = s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if x := tx.RequireOwner(ctx, actor, householdID); x != nil {
			return x
		}
		var x error
		out, _, x = tx.ListInvitations(ctx, householdID, id, limit+1)
		return x
	})
	if e != nil {
		return nil, "", e
	}
	now := invitationNow(s.clock)
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = encodeInvitationCursor(out[len(out)-1].ID)
	}
	for i := range out {
		out[i].Status = invitationStatus(out[i], now)
	}
	return out, next, nil
}
func (s *InvitationService) RevokeInvitation(ctx context.Context, actor, householdID, id string) error {
	bounded, cancel := context.WithTimeout(ctx, invitationRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validUUID(actor) || !validUUID(householdID) || !validUUID(id) {
		return household.ErrNotFound
	}
	return s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		if e := tx.RequireOwner(ctx, actor, householdID); e != nil {
			return e
		}
		return tx.RevokeInvitation(ctx, householdID, id, invitationNow(s.clock))
	})
}
