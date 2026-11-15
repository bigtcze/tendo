package postgres

import (
	"context"
	"time"

	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

// RemovalEffects implements the identity-owned side of a household membership
// removal. It is constructed with the household removal transaction, so
// invitation and reset revocations and the default-household move commit or
// roll back with the removal (ADR 0019).
type RemovalEffects struct {
	q     *identitydb.Queries
	clock func() time.Time
}

func NewRemovalEffects(db identitydb.DBTX, clock ...func() time.Time) *RemovalEffects {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &RemovalEffects{q: identitydb.New(db), clock: now}
}

// RevokePendingInvitationsCreatedBy revokes every still-pending invitation of
// the household the removed member created. It runs before the account row is
// locked, because invitation acceptance locks the invitation row and then the
// account row (ADR 0019).
func (e *RemovalEffects) RevokePendingInvitationsCreatedBy(ctx context.Context, householdID, userID string) error {
	var house, user pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return nil
	}
	if err := user.Scan(userID); err != nil {
		return nil
	}
	return e.q.RevokePendingInvitationsCreatedBy(ctx, identitydb.RevokePendingInvitationsCreatedByParams{
		HouseholdID: house, CreatedByUserID: user, RevokedAt: ts(e.clock().UTC()),
	})
}

// RevokeMemberPasswordResets revokes the outstanding reset links of this
// household in which the removed member is the target or the issuer. Links the
// same account has in other households are left alone.
func (e *RemovalEffects) RevokeMemberPasswordResets(ctx context.Context, householdID, userID string) error {
	var house, user pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return nil
	}
	if err := user.Scan(userID); err != nil {
		return nil
	}
	return e.q.RevokeMemberResetsInHousehold(ctx, identitydb.RevokeMemberResetsInHouseholdParams{
		HouseholdID: house, TargetUserID: user, RevokedAt: ts(e.clock().UTC()),
	})
}

// ReassignDefaultHousehold moves the account default to its lowest-id remaining
// household, or clears it, when it still points at the removed household. The
// statement is a no-op for any other default, so a concurrent last-write-wins
// selection is not overwritten.
func (e *RemovalEffects) ReassignDefaultHousehold(ctx context.Context, userID, removedHouseholdID string) error {
	var user, removed pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return nil
	}
	if err := removed.Scan(removedHouseholdID); err != nil {
		return nil
	}
	return e.q.ReassignDefaultHousehold(ctx, identitydb.ReassignDefaultHouseholdParams{
		UserID: user, RemovedHouseholdID: removed,
	})
}
