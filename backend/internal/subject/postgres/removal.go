package postgres

import (
	"context"

	"github.com/bigtcze/tendo/backend/internal/subject/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

// MemberPersonUnlinker unlinks a removed member's canonical person subject. It
// is constructed with the household removal transaction so the unlink commits
// or rolls back with the membership removal (ADR 0019).
type MemberPersonUnlinker struct{ q *dbgen.Queries }

func NewMemberPersonUnlinker(db dbgen.DBTX) *MemberPersonUnlinker {
	return &MemberPersonUnlinker{q: dbgen.New(db)}
}

// UnlinkMemberPerson clears linked_user_id on the member's person subject. The
// person stays active; it is never archived or merged, so item and history
// associations survive and a rejoin provisions a fresh person.
func (u *MemberPersonUnlinker) UnlinkMemberPerson(ctx context.Context, householdID, userID string) error {
	var house, user pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return errPersistence
	}
	if err := user.Scan(userID); err != nil {
		return errPersistence
	}
	return u.q.UnlinkMemberPersonSubject(ctx, dbgen.UnlinkMemberPersonSubjectParams{HouseholdID: house, LinkedUserID: user})
}
