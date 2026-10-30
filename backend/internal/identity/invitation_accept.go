package identity

import (
	"context"
	"errors"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"

	"github.com/bigtcze/tendo/backend/internal/household"
)

func (s *InvitationService) AcceptInvitationNewAccount(ctx context.Context, token, login, password string) (InvitationUser, error) {
	bounded, cancel := context.WithTimeout(ctx, invitationRepositoryBudget)
	defer cancel()
	ctx = bounded
	if !validInvitationToken(token) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	digest := digestInvitationToken(token)
	now := invitationNow(s.clock)
	inv, e := s.repository.FindInvitation(ctx, digest)
	if errors.Is(e, ErrNotFound) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	if e != nil {
		return InvitationUser{}, e
	}
	if invitationStatus(inv, now) != "pending" {
		return InvitationUser{}, ErrInvalidInvitation
	}
	if err := validateCredentials(login, password); err != nil {
		return InvitationUser{}, err
	}
	hash, e := s.hashPassword(password)
	if errors.Is(e, ErrPasswordWorkLimit) {
		return InvitationUser{}, ErrPasswordWorkLimit
	}
	if e != nil {
		return InvitationUser{}, e
	}
	if err := ctx.Err(); err != nil {
		return InvitationUser{}, err
	}
	var result InvitationUser
	e = s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		locked, lockedAt, lockErr := lockPendingInvitation(ctx, tx, digest, s.clock)
		now = lockedAt
		if lockErr != nil {
			return lockErr
		}
		if x := tx.FindLogin(ctx, login); x == nil {
			return ErrLoginUnavailable
		} else if !errors.Is(x, ErrNotFound) {
			return x
		}
		userID, createErr := tx.CreateUser(ctx, login)
		if createErr != nil {
			return createErr
		}
		if createErr = tx.CreateCredential(ctx, userID, hash); createErr != nil {
			return createErr
		}
		if createErr = joinInvitedHousehold(ctx, tx, userID, locked.HouseholdID, locked.ID, now); createErr != nil {
			return createErr
		}
		result = InvitationUser{userID, login, locked.HouseholdID, household.RoleMember}
		return nil
	})
	return result, e
}

func (s *InvitationService) AcceptInvitationExistingAccount(ctx context.Context, userID, token string) (InvitationUser, error) {
	bounded, cancel := context.WithTimeout(ctx, invitationRepositoryBudget)
	defer cancel()
	ctx = bounded
	if err := ctx.Err(); err != nil {
		return InvitationUser{}, err
	}
	if !ids.IsCanonicalUUID(userID) || !validInvitationToken(token) {
		return InvitationUser{}, ErrInvalidInvitation
	}
	digest := digestInvitationToken(token)
	now := invitationNow(s.clock)
	var result InvitationUser
	e := s.repository.WithInvitationTransaction(ctx, func(tx InvitationTransaction) error {
		invitation, lockedAt, lockErr := lockPendingInvitation(ctx, tx, digest, s.clock)
		now = lockedAt
		if lockErr != nil {
			return lockErr
		}
		defaultHouseholdID, lockErr := tx.LockUser(ctx, userID)
		if errors.Is(lockErr, ErrNotFound) {
			return ErrNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if _, membershipErr := tx.GetMembership(ctx, userID, invitation.HouseholdID); membershipErr == nil {
			return ErrAlreadyMember
		} else if !errors.Is(membershipErr, household.ErrNotFound) {
			return membershipErr
		}
		otherMembership, membershipErr := tx.HasOtherMembership(ctx, userID, invitation.HouseholdID)
		if membershipErr != nil {
			return membershipErr
		}
		if defaultHouseholdID != "" || otherMembership {
			return ErrHouseholdConflict
		}
		if err := joinInvitedHousehold(ctx, tx, userID, invitation.HouseholdID, invitation.ID, now); err != nil {
			return err
		}
		result = InvitationUser{UserID: userID, HouseholdID: invitation.HouseholdID, Role: household.RoleMember}
		return nil
	})
	return result, e
}
