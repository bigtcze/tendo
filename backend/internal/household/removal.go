package household

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

const memberRemovalBudget = 5 * time.Second

// MemberRemoval is one audited membership removal. It records who removed whom
// and the removed membership's role, never household content.
type MemberRemoval struct {
	HouseholdID, TargetUserID, ActorUserID, PreviousRole string
	RemovedAt                                            time.Time
}

// InvitationRevoker revokes the still-pending invitations of a household that
// were created by one member. It is owned by the identity module because
// invitation rows and their token lifecycle belong there.
type InvitationRevoker interface {
	RevokePendingInvitationsCreatedBy(ctx context.Context, householdID, userID string) error
}

// ResponsibilityReleaser clears the removed member from every item of the
// household. It is owned by the item module.
type ResponsibilityReleaser interface {
	ReleaseMemberResponsibility(ctx context.Context, householdID, userID string) error
}

// MemberPersonUnlinker unlinks the removed member's canonical person subject
// without archiving it. It is owned by the subject module.
type MemberPersonUnlinker interface {
	UnlinkMemberPerson(ctx context.Context, householdID, userID string) error
}

// PasswordResetRevoker revokes the outstanding password-reset links a removed
// member is party to inside the household, as target or as issuer. It is owned
// by the identity module.
type PasswordResetRevoker interface {
	RevokeMemberPasswordResets(ctx context.Context, householdID, userID string) error
}

// DefaultHouseholdReassigner moves an account default off a household it just
// left. It is owned by the identity module, which owns user_accounts defaults.
type DefaultHouseholdReassigner interface {
	ReassignDefaultHousehold(ctx context.Context, userID, removedHouseholdID string) error
}

// MemberRemovalEffects groups the transaction-scoped effects a removal has on
// tables another module owns. Every implementation must write through the same
// transaction as the membership removal, so a refused removal leaves no effect
// behind.
type MemberRemovalEffects struct {
	Invitations InvitationRevoker
	Items       ResponsibilityReleaser
	Subjects    MemberPersonUnlinker
	Resets      PasswordResetRevoker
	Accounts    DefaultHouseholdReassigner
}

// MemberRemovalTransaction is the household-owned persistence surface of a
// removal. The caller acquires the household row through the shared admin lock
// protocol first; these statements then run in the documented order.
type MemberRemovalTransaction interface {
	AdminGate
	// LockMemberAccount locks user_accounts FOR NO KEY UPDATE so concurrent
	// account-level writes serialize without conflicting with foreign-key key
	// shares (ADR 0018). ErrNotFound means the account is gone.
	LockMemberAccount(ctx context.Context, userID string) error
	// LockMembership locks the membership FOR UPDATE and returns its role and
	// version. FOR UPDATE (never FOR NO KEY UPDATE) makes concurrent foreign-key
	// inserts that reference the membership block until the removal commits.
	LockMembership(ctx context.Context, householdID, userID string) (MembershipRecord, int64, error)
	// CountAdminsExcluding counts the household's admins other than the target.
	CountAdminsExcluding(ctx context.Context, householdID, userID string) (int, error)
	DeleteMembership(ctx context.Context, householdID, userID string) error
	RecordMemberRemoval(ctx context.Context, removal MemberRemoval) error
}

// MemberRemovalRepository runs one removal in one transaction and hands the
// same transaction to every effect implementation.
type MemberRemovalRepository interface {
	WithMemberRemovalTransaction(ctx context.Context, work func(MemberRemovalTransaction, MemberRemovalEffects) error) error
}

// MemberRemovalService removes one member from one household. It also serves
// the "leave household" path: a plain member may remove themselves.
type MemberRemovalService struct {
	repository MemberRemovalRepository
	clock      func() time.Time
}

func NewMemberRemovalService(repository MemberRemovalRepository, clock func() time.Time) *MemberRemovalService {
	if clock == nil {
		clock = time.Now
	}
	return &MemberRemovalService{repository: repository, clock: clock}
}

// AccountCenter is the admin account surface of one household: account reads
// and role changes plus membership removal. Both services share the same
// repository lock protocol.
type AccountCenter struct {
	*AccountService
	*MemberRemovalService
}

func NewAccountCenter(accounts *AccountService, removals *MemberRemovalService) *AccountCenter {
	return &AccountCenter{AccountService: accounts, MemberRemovalService: removals}
}

// RemoveMember removes targetUserID from householdID when the actor is a
// household admin (any target, including self) or when the actor removes
// themselves as a plain member. Every refusal is indistinguishable from a
// missing household for a non-member, and a target outside the household is
// also not found. The expectedVersion is the target membership version from
// If-Match.
func (s *MemberRemovalService) RemoveMember(ctx context.Context, actorID, householdID, targetUserID string, expectedVersion int64) error {
	bounded, cancel := context.WithTimeout(ctx, memberRemovalBudget)
	defer cancel()
	// A malformed actor, household or target is the uniform not-found before
	// any authorization decision, so a malformed target never reveals the
	// actor's role.
	if !ids.IsCanonicalUUID(actorID) || !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
		return ErrNotFound
	}
	if expectedVersion < 1 {
		return ErrPreconditionFailed
	}
	return s.repository.WithMemberRemovalTransaction(bounded, func(tx MemberRemovalTransaction, effects MemberRemovalEffects) error {
		if err := authorizeMemberRemoval(bounded, tx, actorID, householdID, targetUserID); err != nil {
			return err
		}
		return removeMembershipLocked(bounded, tx, effects, householdID, targetUserID, actorID, expectedVersion, s.clock)
	})
}

// authorizeMemberRemoval runs the household lock of the shared admin protocol
// and re-reads the actor's role under it. An admin may remove anyone; a plain
// member only themselves. The caller has already validated the identifiers.
func authorizeMemberRemoval(ctx context.Context, tx MemberRemovalTransaction, actorID, householdID, targetUserID string) error {
	if err := tx.LockHouseholdForMembership(ctx, actorID, householdID); err != nil {
		return err
	}
	role, err := tx.GetMembership(ctx, actorID, householdID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	switch role {
	case RoleAdmin:
		return nil
	case RoleMember:
		if strings.EqualFold(actorID, targetUserID) {
			return nil
		}
		return ErrAdminRequired
	default:
		return ErrNotFound
	}
}

// removeMembershipLocked performs one removal after the household row is
// locked and the actor is authorized. The order is a cross-module contract
// (ADR 0019) and must not be reordered:
//
//  1. revoke the target's pending invitations of this household, before the
//     account row, because invitation acceptance locks the invitation and then
//     the account;
//  2. lock user_accounts FOR NO KEY UPDATE;
//  3. lock the membership FOR UPDATE and check the expected version;
//  4. refuse leaving the household without an admin;
//  5. release item responsibility;
//  6. unlink the member person;
//  7. revoke reset links the target is party to inside this household;
//  8. delete the membership;
//  9. move or clear the account default;
//  10. write the audit row.
//
// The function is self-contained so the future instance-admin path can reuse it
// after its own authorization and household (or installation) lock.
func removeMembershipLocked(ctx context.Context, tx MemberRemovalTransaction, effects MemberRemovalEffects, householdID, targetUserID, actorID string, expectedVersion int64, clock func() time.Time) error {
	if err := effects.Invitations.RevokePendingInvitationsCreatedBy(ctx, householdID, targetUserID); err != nil {
		return err
	}
	if err := tx.LockMemberAccount(ctx, targetUserID); err != nil {
		return err
	}
	membership, version, err := tx.LockMembership(ctx, householdID, targetUserID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return ErrPreconditionFailed
	}
	if membership.Role != RoleAdmin && membership.Role != RoleMember {
		return errUnavailable
	}
	if membership.Role == RoleAdmin {
		remaining, err := tx.CountAdminsExcluding(ctx, householdID, targetUserID)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return ErrLastAdmin
		}
	}
	if err := effects.Items.ReleaseMemberResponsibility(ctx, householdID, targetUserID); err != nil {
		return err
	}
	if err := effects.Subjects.UnlinkMemberPerson(ctx, householdID, targetUserID); err != nil {
		return err
	}
	if err := effects.Resets.RevokeMemberPasswordResets(ctx, householdID, targetUserID); err != nil {
		return err
	}
	if err := tx.DeleteMembership(ctx, householdID, targetUserID); err != nil {
		return err
	}
	if err := effects.Accounts.ReassignDefaultHousehold(ctx, targetUserID, householdID); err != nil {
		return err
	}
	return tx.RecordMemberRemoval(ctx, MemberRemoval{
		HouseholdID: householdID, TargetUserID: targetUserID, ActorUserID: actorID,
		PreviousRole: membership.Role, RemovedAt: clock().UTC(),
	})
}
