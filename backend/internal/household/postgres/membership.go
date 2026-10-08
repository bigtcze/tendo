package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type MembershipRepository struct{ queries *dbgen.Queries }

func NewMembershipRepository(db dbgen.DBTX) *MembershipRepository {
	return &MembershipRepository{dbgen.New(db)}
}
func MembershipServiceFor(db dbgen.DBTX) *household.MembershipService {
	return household.NewMembershipService(NewMembershipRepository(db))
}
func (r *MembershipRepository) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	var u, h pgtype.UUID
	if u.Scan(userID) != nil || h.Scan(householdID) != nil {
		return "", household.ErrNotFound
	}
	v, e := r.queries.GetMembership(ctx, dbgen.GetMembershipParams{UserID: u, HouseholdID: h})
	if errors.Is(e, pgx.ErrNoRows) {
		return "", household.ErrNotFound
	}
	return v, e
}
func (r *MembershipRepository) AddInvitedMember(ctx context.Context, userID, householdID string) error {
	var u, h pgtype.UUID
	if e := u.Scan(userID); e != nil {
		return e
	}
	if e := h.Scan(householdID); e != nil {
		return e
	}
	return r.queries.AddInvitedMember(ctx, dbgen.AddInvitedMemberParams{UserID: u, HouseholdID: h})
}
func (r *MembershipRepository) ListMembers(ctx context.Context, householdID, cursor string, limit int) ([]household.Member, string, error) {
	decodedCursor, err := household.DecodeMemberCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if decodedCursor == "" && cursor != "" {
		return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
	}
	var h, after pgtype.UUID
	if e := h.Scan(householdID); e != nil {
		return nil, "", household.ErrNotFound
	}
	if decodedCursor != "" {
		if e := after.Scan(decodedCursor); e != nil {
			return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, e := r.queries.ListMembers(ctx, dbgen.ListMembersParams{HouseholdID: h, Column2: after, Limit: int32(limit + 1)})
	if e != nil {
		return nil, "", e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = household.EncodeMemberCursor(rows[len(rows)-1].UserID)
	}
	out := make([]household.Member, 0, len(rows))
	for _, m := range rows {
		if m.Role != household.RoleOwner && m.Role != household.RoleMember {
			return nil, "", errors.New("membership persistence returned unknown role")
		}
		out = append(out, household.Member{UserID: m.UserID, Login: m.Login, Role: m.Role})
	}
	return out, next, nil
}
