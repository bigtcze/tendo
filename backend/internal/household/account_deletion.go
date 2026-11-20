package household

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// accountDeletionAttempts bounds the retries of one deletion. A retry happens
// only when a membership appeared between the unlocked household read and the
// account lock; anything else is answered on the first attempt (ADR 0022).
const accountDeletionAttempts = 3

var (
	// ErrCannotDeleteSelf refuses an instance administrator deleting their own
	// account. Another administrator must do it.
	ErrCannotDeleteSelf = errors.New("cannot_delete_self")
	// ErrLastInstanceAdmin refuses deleting the installation's last live
	// instance administrator.
	ErrLastInstanceAdmin = errors.New("last_instance_admin")
	// ErrAccountDeletionContended means the target kept joining households
	// faster than the deletion could lock them. It is answered as unavailable.
	ErrAccountDeletionContended = errors.New("account deletion contended")
	// errMembershipsGrew is the internal retry signal of one attempt.
	errMembershipsGrew = errors.New("account memberships grew")
)

// AccountDeletion is the append-only record of one deletion. It names who
// deleted which account and when, never the login or any household content.
type AccountDeletion struct {
	TargetUserID, ActorUserID string
	DeletedAt                 time.Time
}

// InstanceAdminRevocation is the append-only audit of one revoked
// instance-administrator grant (ADR 0016 duty). It moves the grant's source
// and time out of instance_admins before the grant row is deleted.
type InstanceAdminRevocation struct {
	TargetUserID, ActorUserID, GrantSource, Reason string
	GrantedAt, RevokedAt                           time.Time
}

// InstanceAdminGrantRecord is one live instance-administrator grant.
type InstanceAdminGrantRecord struct {
	Source    string
	GrantedAt time.Time
}

// AccountMembership is one membership of the deleted account, locked FOR
// UPDATE in ascending household order.
type AccountMembership struct {
	HouseholdID string
	Role        string
}

// AccountEraser is the identity-owned side of a deletion. Every method writes
// through the deletion transaction.
type AccountEraser interface {
	// FindDeletableAccount is the unlocked pre-read of a live account's login.
	// ErrNotFound when the account does not exist or is already deleted.
	FindDeletableAccount(ctx context.Context, userID string) (string, error)
	// LockAccountIdentities takes the OIDC advisory lock of every identity
	// currently linked to the account, in ascending key order, so a concurrent
	// OIDC sign-in of that identity either finishes first or sees it unlinked.
	LockAccountIdentities(ctx context.Context, userID string) error
	// LockDeletableAccount locks the live account row FOR NO KEY UPDATE and
	// returns its login. ErrNotFound when it is missing or deleted.
	LockDeletableAccount(ctx context.Context, userID string) (string, error)
	// RevokeAccountPasswordResets revokes every outstanding reset link the
	// account is party to, as target or issuer, in any household.
	RevokeAccountPasswordResets(ctx context.Context, userID string, now time.Time) error
	// EraseAccountAccess deletes the local credential, revokes every MCP grant
	// (audited with the actor) and deletes its codes and tokens, then deletes
	// every session, OIDC identity and link flow.
	EraseAccountAccess(ctx context.Context, userID, actorID string, now time.Time) error
	// AnonymizeAccount replaces the login with the tombstone, clears the email
	// facts and the default household, and sets deleted_at. It deletes sessions
	// a final time immediately before the update.
	AnonymizeAccount(ctx context.Context, userID string, now time.Time) error
}

// AccountDeletionEffects groups the effects of a deletion on tables other
// modules own: the per-household removal effects of ADR 0019 and the
// identity-owned account eraser.
type AccountDeletionEffects struct {
	Removal MemberRemovalEffects
	Account AccountEraser
}

// AccountDeletionTransaction is the household-owned persistence surface of a
// deletion. It reuses the removal statements for each membership.
type AccountDeletionTransaction interface {
	MemberRemovalTransaction
	// RequireInstanceAdminExclusive locks installation_state FOR UPDATE, then
	// re-reads the actor's live grant. Deletions change the installation-wide
	// administrator set, so they serialize with each other and with grants.
	RequireInstanceAdminExclusive(ctx context.Context, actorID string) error
	// LockHousehold locks one household row FOR NO KEY UPDATE by ID.
	LockHousehold(ctx context.Context, householdID string) error
	// ListUserHouseholds is the unlocked read of the account's household IDs
	// in ascending order.
	ListUserHouseholds(ctx context.Context, userID string) ([]string, error)
	// LockUserMemberships locks every membership of the account FOR UPDATE in
	// ascending household order and returns them.
	LockUserMemberships(ctx context.Context, userID string) ([]AccountMembership, error)
	// FindInstanceAdminGrant reads the account's grant; ok is false when none.
	FindInstanceAdminGrant(ctx context.Context, userID string) (InstanceAdminGrantRecord, bool, error)
	// CountLiveInstanceAdminsExcluding counts grants of live accounts other
	// than userID.
	CountLiveInstanceAdminsExcluding(ctx context.Context, userID string) (int, error)
	// RevokeInstanceAdmin writes the revocation audit row, then deletes the
	// grant.
	RevokeInstanceAdmin(ctx context.Context, revocation InstanceAdminRevocation) error
	RecordAccountDeletion(ctx context.Context, deletion AccountDeletion) error
}

type AccountDeletionRepository interface {
	WithAccountDeletionTransaction(ctx context.Context, work func(AccountDeletionTransaction, AccountDeletionEffects) error) error
}

// AccountDeletionService deletes an installation account under the
// instance-administrator grant (ADR 0022). The account row is anonymized in
// place, never removed, so every history row keeps its stable identifier.
type AccountDeletionService struct {
	repository AccountDeletionRepository
	clock      func() time.Time
}

func NewAccountDeletionService(repository AccountDeletionRepository, clock func() time.Time) *AccountDeletionService {
	if clock == nil {
		clock = time.Now
	}
	return &AccountDeletionService{repository: repository, clock: clock}
}

// WithAccountDeletion attaches the global account deletion to the
// instance-admin center so the admin transport serves it with the other
// instance-administrator routes.
func (s *InstanceAdminCenter) WithAccountDeletion(deletions *AccountDeletionService) *InstanceAdminCenter {
	s.deletions = deletions
	return s
}

// DeleteAccount deletes an installation account (ADR 0022).
func (s *InstanceAdminCenter) DeleteAccount(ctx context.Context, actorID, targetUserID, confirmLogin string) error {
	if s.deletions == nil {
		return errors.New("account deletion unavailable")
	}
	return s.deletions.DeleteAccount(ctx, actorID, targetUserID, confirmLogin)
}

// DeleteAccount deletes targetUserID after confirmLogin matched its login.
// Evaluation order: grant (ErrInstanceAdminRequired), userId shape (422
// invalid_reference), self (ErrCannotDeleteSelf), unknown or already deleted
// (ErrNotFound), confirmation (422 mismatch on confirmLogin), last household
// admin (ErrLastAdmin), last instance administrator (ErrLastInstanceAdmin).
func (s *AccountDeletionService) DeleteAccount(ctx context.Context, actorID, targetUserID, confirmLogin string) error {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	for attempt := 0; attempt < accountDeletionAttempts; attempt++ {
		err := s.repository.WithAccountDeletionTransaction(bounded, func(tx AccountDeletionTransaction, effects AccountDeletionEffects) error {
			return s.deleteOnce(bounded, tx, effects, actorID, targetUserID, confirmLogin)
		})
		if !errors.Is(err, errMembershipsGrew) {
			return err
		}
	}
	return ErrAccountDeletionContended
}

// deleteOnce is one attempt in the ADR 0019 global lock order:
//
//  1. installation_state FOR UPDATE and the actor's live grant;
//  2. validation, self refusal and the unlocked target read;
//  3. the target's households FOR NO KEY UPDATE, ascending;
//  4. the advisory locks of the target's OIDC identities, ascending;
//  5. the target's pending invitations, per household;
//  6. the account row FOR NO KEY UPDATE and the confirmation;
//  7. the memberships FOR UPDATE; a household outside the locked set means
//     the set grew, so the attempt is abandoned and retried;
//  8. last-admin and last-instance-admin refusals;
//  9. grant revocation (audit first), then per household: item
//     responsibility, member person, reset links, removal audit, membership;
//  10. account-wide reset links, credential, sessions, OIDC links (and, with
//     #456, MCP tokens), the deletion record and the tombstone.
func (s *AccountDeletionService) deleteOnce(ctx context.Context, tx AccountDeletionTransaction, effects AccountDeletionEffects, actorID, targetUserID, confirmLogin string) error {
	if effects.Account == nil || effects.Removal.Invitations == nil || effects.Removal.Items == nil ||
		effects.Removal.Subjects == nil || effects.Removal.Resets == nil {
		return errors.New("account deletion effects unavailable")
	}
	actor, target, err := authorizeDeletion(ctx, tx, effects.Account, actorID, targetUserID)
	if err != nil {
		return err
	}
	memberships, err := lockDeletionTarget(ctx, tx, effects, target, confirmLogin)
	if err != nil {
		return err
	}
	grant, granted, err := s.checkDeletable(ctx, tx, target, memberships)
	if err != nil {
		return err
	}
	now := s.clock().UTC()
	if granted {
		if err := tx.RevokeInstanceAdmin(ctx, InstanceAdminRevocation{
			TargetUserID: target, ActorUserID: actor, GrantSource: grant.Source,
			Reason: "account_deletion", GrantedAt: grant.GrantedAt, RevokedAt: now,
		}); err != nil {
			return err
		}
	}
	for _, membership := range memberships {
		if err := removeDeletedMembership(ctx, tx, effects.Removal, membership, target, actor, now); err != nil {
			return err
		}
	}
	return eraseAccount(ctx, tx, effects.Account, target, actor, now)
}

// authorizeDeletion performs steps 1 and 2 of deleteOnce: the exclusive grant
// gate, the userId shape, the self refusal and the unlocked target read. It
// returns the lowercased actor and target.
func authorizeDeletion(ctx context.Context, tx AccountDeletionTransaction, account AccountEraser, actorID, targetUserID string) (string, string, error) {
	if !ids.IsCanonicalUUID(actorID) {
		return "", "", ErrInstanceAdminRequired
	}
	if err := tx.RequireInstanceAdminExclusive(ctx, actorID); err != nil {
		return "", "", err
	}
	if !ids.IsCanonicalUUID(targetUserID) {
		return "", "", &ValidationError{Field: "userId", Code: "invalid_reference"}
	}
	actor, target := strings.ToLower(actorID), strings.ToLower(targetUserID)
	if actor == target {
		return "", "", ErrCannotDeleteSelf
	}
	if _, err := account.FindDeletableAccount(ctx, target); err != nil {
		return "", "", err
	}
	return actor, target, nil
}

// lockDeletionTarget performs steps 3 to 7 of deleteOnce and returns the
// memberships locked FOR UPDATE. errMembershipsGrew abandons the attempt.
func lockDeletionTarget(ctx context.Context, tx AccountDeletionTransaction, effects AccountDeletionEffects, target, confirmLogin string) ([]AccountMembership, error) {
	households, err := tx.ListUserHouseholds(ctx, target)
	if err != nil {
		return nil, err
	}
	for _, householdID := range households {
		if err := tx.LockHousehold(ctx, householdID); err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	if err := effects.Account.LockAccountIdentities(ctx, target); err != nil {
		return nil, err
	}
	for _, householdID := range households {
		if err := effects.Removal.Invitations.RevokePendingInvitationsCreatedBy(ctx, householdID, target); err != nil {
			return nil, err
		}
	}
	login, err := effects.Account.LockDeletableAccount(ctx, target)
	if err != nil {
		return nil, err
	}
	if confirmLogin != login {
		return nil, &ValidationError{Field: "confirmLogin", Code: "mismatch"}
	}
	memberships, err := tx.LockUserMemberships(ctx, target)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		if !slices.Contains(households, membership.HouseholdID) {
			return nil, errMembershipsGrew
		}
	}
	return memberships, nil
}

// checkDeletable refuses leaving any household without an admin, then leaving
// the installation without a live instance administrator. It returns the
// target's grant when it has one.
func (s *AccountDeletionService) checkDeletable(ctx context.Context, tx AccountDeletionTransaction, target string, memberships []AccountMembership) (InstanceAdminGrantRecord, bool, error) {
	for _, membership := range memberships {
		if membership.Role != RoleAdmin {
			continue
		}
		remaining, err := tx.CountAdminsExcluding(ctx, membership.HouseholdID, target)
		if err != nil {
			return InstanceAdminGrantRecord{}, false, err
		}
		if remaining == 0 {
			return InstanceAdminGrantRecord{}, false, ErrLastAdmin
		}
	}
	grant, granted, err := tx.FindInstanceAdminGrant(ctx, target)
	if err != nil || !granted {
		return InstanceAdminGrantRecord{}, false, err
	}
	remaining, err := tx.CountLiveInstanceAdminsExcluding(ctx, target)
	if err != nil {
		return InstanceAdminGrantRecord{}, false, err
	}
	if remaining == 0 {
		return InstanceAdminGrantRecord{}, false, ErrLastInstanceAdmin
	}
	return grant, true, nil
}

// removeDeletedMembership applies the ADR 0019 removal effects to one locked
// membership. The account default is not moved: the tombstone clears it.
func removeDeletedMembership(ctx context.Context, tx AccountDeletionTransaction, effects MemberRemovalEffects, membership AccountMembership, target, actor string, now time.Time) error {
	if err := releaseMemberEffects(ctx, effects, membership.HouseholdID, target); err != nil {
		return err
	}
	if err := tx.RecordMemberRemoval(ctx, MemberRemoval{
		HouseholdID: membership.HouseholdID, TargetUserID: target, ActorUserID: actor,
		PreviousRole: membership.Role, ActorAuthority: AuthorityInstanceAdmin, RemovedAt: now,
	}); err != nil {
		return err
	}
	return tx.DeleteMembership(ctx, membership.HouseholdID, target)
}

// eraseAccount performs step 10 of deleteOnce.
func eraseAccount(ctx context.Context, tx AccountDeletionTransaction, account AccountEraser, target, actor string, now time.Time) error {
	if err := account.RevokeAccountPasswordResets(ctx, target, now); err != nil {
		return err
	}
	if err := account.EraseAccountAccess(ctx, target, actor, now); err != nil {
		return err
	}
	if err := tx.RecordAccountDeletion(ctx, AccountDeletion{TargetUserID: target, ActorUserID: actor, DeletedAt: now}); err != nil {
		return err
	}
	return account.AnonymizeAccount(ctx, target, now)
}
