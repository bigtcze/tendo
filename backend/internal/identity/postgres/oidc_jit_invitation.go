package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
)

// acceptInvitationFor and oidcInvitationTx adapt the OIDC transaction to the
// invitation acceptance seam so both paths share one acceptance rule.

// acceptInvitationFor applies the existing-account invitation semantics inside
// the caller's transaction. An unusable invitation degrades to "invalid" with
// the sign-in still completed.
func acceptInvitationFor(ctx context.Context, tx pgx.Tx, r *Repository, userID string, inviteHash []byte, now time.Time) (identity.OIDCInvitationOutcome, error) {
	if len(inviteHash) != 32 {
		return identity.OIDCInvitationNone, nil
	}
	if r.invitationHousehold == nil || r.memberPersonFactory == nil {
		return "", errors.New("OIDC invitation acceptance is not configured")
	}
	adapter := &oidcInvitationTx{
		tx: tx, household: r.invitationHousehold(tx), memberPersons: r.memberPersonFactory(tx), now: now,
	}
	invitation, _, err := identity.LockPendingInvitation(ctx, adapter, inviteHash, func() time.Time { return now })
	if errors.Is(err, identity.ErrInvalidInvitation) {
		return identity.OIDCInvitationInvalid, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := identity.JoinAsExistingMember(ctx, adapter, userID, invitation.HouseholdID, invitation.ID, now); err != nil {
		if errors.Is(err, identity.ErrAlreadyMember) || errors.Is(err, identity.ErrNotFound) {
			return identity.OIDCInvitationInvalid, nil
		}
		return "", err
	}
	return identity.OIDCInvitationAccepted, nil
}

// oidcInvitationTx adapts the OIDC transaction to the invitation acceptance
// seam so both paths share one acceptance rule.
type oidcInvitationTx struct {
	tx            pgx.Tx
	household     InvitationHouseholdService
	memberPersons identity.MemberPersonProvisioner
	now           time.Time
}

func (o *oidcInvitationTx) FindInvitation(ctx context.Context, digest []byte, lock bool) (identity.Invitation, error) {
	return findInvitation(ctx, identitydb.New(o.tx), digest, lock)
}

func (o *oidcInvitationTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	return o.household.GetMembership(ctx, userID, householdID)
}

func (o *oidcInvitationTx) LockUser(ctx context.Context, userValue string) (string, string, error) {
	id, err := uuidOrNull(userValue)
	if err != nil {
		return "", "", identity.ErrNotFound
	}
	var login, defaultHousehold string
	err = o.tx.QueryRow(ctx, `SELECT login, COALESCE(default_household_id::text,'') FROM user_accounts WHERE id=$1 FOR UPDATE`, id).Scan(&login, &defaultHousehold)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", identity.ErrNotFound
	}
	return defaultHousehold, login, err
}

func (o *oidcInvitationTx) EnsureMemberPerson(ctx context.Context, householdID, userID, name string) error {
	return o.memberPersons.EnsureMemberPerson(ctx, householdID, userID, name)
}

func (o *oidcInvitationTx) AddInvitedMember(ctx context.Context, userID, householdID string) error {
	return o.household.AddInvitedMember(ctx, userID, householdID)
}

func (o *oidcInvitationTx) SetDefaultHousehold(ctx context.Context, userValue, householdValue string) error {
	id, err := uuidOrNull(userValue)
	if err != nil {
		return err
	}
	householdID, err := uuidOrNull(householdValue)
	if err != nil {
		return err
	}
	_, err = o.tx.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, id, householdID)
	return err
}

func (o *oidcInvitationTx) AcceptInvitation(ctx context.Context, invitationValue string, now time.Time) error {
	id, err := uuidOrNull(invitationValue)
	if err != nil {
		return identity.ErrInvalidInvitation
	}
	tag, err := o.tx.Exec(ctx, `UPDATE household_invitations SET accepted_at=$2
		WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>$2`, id, timestamptz(now))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrInvalidInvitation
	}
	return nil
}
