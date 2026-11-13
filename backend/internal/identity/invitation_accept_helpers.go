package identity

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
)

// InvitationAcceptTx is the subset of InvitationTransaction that accepting an
// invitation needs. It lets the OIDC first-login transaction reuse the same
// acceptance rule through its own adapter instead of duplicating it.
type InvitationAcceptTx interface {
	FindInvitation(context.Context, []byte, bool) (Invitation, error)
	GetMembership(context.Context, string, string) (string, error)
	LockUser(context.Context, string) (string, error)
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

// LockPendingInvitation exposes the shared acceptance rule to adapters outside
// this package (the OIDC first-login transaction).
func LockPendingInvitation(ctx context.Context, tx InvitationAcceptTx, digest []byte, clock func() time.Time) (Invitation, time.Time, error) {
	return lockPendingInvitation(ctx, tx, digest, clock)
}

// JoinAsExistingMember is the one existing-account acceptance rule shared by the
// local and OIDC sign-in paths. The caller must already have locked and validated
// the invitation row in tx; this helper then locks the user row (invitation ->
// user lock order), rejects an account that already belongs to the household, and
// joins it as a member. Joining always makes the invited household the account's
// default, which is a navigation hint rather than an authorization grant.
//
// A vanished user returns ErrNotFound, an existing membership returns
// ErrAlreadyMember, and every other error is an infrastructure failure.
func JoinAsExistingMember(ctx context.Context, tx InvitationAcceptTx, userID, householdID, invitationID string, now time.Time) (InvitationUser, error) {
	login, err := tx.LockUser(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return InvitationUser{}, ErrNotFound
	}
	if err != nil {
		return InvitationUser{}, err
	}
	if _, err := tx.GetMembership(ctx, userID, householdID); err == nil {
		return InvitationUser{}, ErrAlreadyMember
	} else if !errors.Is(err, household.ErrNotFound) {
		return InvitationUser{}, err
	}
	if err := joinInvitedHousehold(ctx, tx, userID, login, householdID, invitationID, now); err != nil {
		return InvitationUser{}, err
	}
	return InvitationUser{UserID: userID, HouseholdID: householdID, Role: household.RoleMember}, nil
}
