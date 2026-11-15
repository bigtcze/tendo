package postgres

import (
	"context"

	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

// ResponsibilityReleaser clears a removed member from the household's items. It
// is constructed with the household removal transaction so the release commits
// or rolls back with the membership removal (ADR 0019).
type ResponsibilityReleaser struct{ q *dbgen.Queries }

func NewResponsibilityReleaser(db dbgen.DBTX) *ResponsibilityReleaser {
	return &ResponsibilityReleaser{q: dbgen.New(db)}
}

// ReleaseMemberResponsibility clears responsible_user_id on every item of the
// household that pointed at the member, archived and done items included. An
// item with no such responsibility is not rewritten, so its version is stable.
func (r *ResponsibilityReleaser) ReleaseMemberResponsibility(ctx context.Context, householdID, userID string) error {
	var house, user pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return errPersistence
	}
	if err := user.Scan(userID); err != nil {
		return errPersistence
	}
	return r.q.ReleaseItemsResponsibleByMember(ctx, dbgen.ReleaseItemsResponsibleByMemberParams{HouseholdID: house, ResponsibleUserID: user})
}
