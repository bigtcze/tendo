package identity

import (
	"context"
	"errors"
	"time"
)

func lockPendingInvitation(ctx context.Context, tx InvitationTransaction, digest []byte, clock func() time.Time) (Invitation, time.Time, error) {
	invitation, err := tx.FindInvitation(ctx, digest, true)
	return checkPendingInvitation(invitation, err, clock)
}

func checkPendingInvitation(invitation Invitation, err error, clock func() time.Time) (Invitation, time.Time, error) {
	now := invitationNow(clock)
	if errors.Is(err, ErrNotFound) {
		return Invitation{}, now, ErrInvalidInvitation
	}
	if err != nil {
		return Invitation{}, now, err
	}
	if invitationStatus(invitation, now) != "pending" {
		return Invitation{}, now, ErrInvalidInvitation
	}
	return invitation, now, nil
}

func joinInvitedHousehold(ctx context.Context, tx InvitationTransaction, userID, householdID, invitationID string, now time.Time) error {
	if err := tx.AddInvitedMember(ctx, userID, householdID); err != nil {
		return err
	}
	if err := tx.SetDefaultHousehold(ctx, userID, householdID); err != nil {
		return err
	}
	return tx.AcceptInvitation(ctx, invitationID, now)
}
