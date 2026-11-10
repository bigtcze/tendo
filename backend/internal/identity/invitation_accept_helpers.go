package identity

import (
	"context"
	"errors"
	"time"
)

// InvitationAcceptTx is the subset of InvitationTransaction that accepting an
// invitation needs. It lets the OIDC first-login transaction reuse the same
// acceptance rule through its own adapter instead of duplicating it.
type InvitationAcceptTx interface {
	FindInvitation(context.Context, []byte, bool) (Invitation, error)
	GetMembership(context.Context, string, string) (string, error)
	HasOtherMembership(context.Context, string, string) (bool, error)
	LockUser(context.Context, string) (string, string, error)
	EnsureMemberPerson(context.Context, string, string, string) error
	AddInvitedMember(context.Context, string, string) error
	SetDefaultHousehold(context.Context, string, string) error
	AcceptInvitation(context.Context, string, time.Time) error
}

func lockPendingInvitation(ctx context.Context, tx InvitationAcceptTx, digest []byte, clock func() time.Time) (Invitation, time.Time, error) {
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

func joinInvitedHousehold(ctx context.Context, tx InvitationAcceptTx, userID, login, householdID, invitationID string, now time.Time) error {
	if err := tx.AddInvitedMember(ctx, userID, householdID); err != nil {
		return err
	}
	if err := tx.EnsureMemberPerson(ctx, householdID, userID, login); err != nil {
		return err
	}
	if err := tx.SetDefaultHousehold(ctx, userID, householdID); err != nil {
		return err
	}
	return tx.AcceptInvitation(ctx, invitationID, now)
}

// LockPendingInvitation and JoinInvitedHousehold expose the shared acceptance
// rule to adapters outside this package (the OIDC first-login transaction).
func LockPendingInvitation(ctx context.Context, tx InvitationAcceptTx, digest []byte, clock func() time.Time) (Invitation, time.Time, error) {
	return lockPendingInvitation(ctx, tx, digest, clock)
}

func JoinInvitedHousehold(ctx context.Context, tx InvitationAcceptTx, userID, login, householdID, invitationID string, now time.Time) error {
	return joinInvitedHousehold(ctx, tx, userID, login, householdID, invitationID, now)
}
