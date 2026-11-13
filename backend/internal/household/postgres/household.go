package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Repository reads households through the runtime-role connection.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

// FindForMember returns the household only when userID has a membership in it.
func (r *Repository) FindForMember(ctx context.Context, userID, householdID string) (household.Household, error) {
	var householdUUID, userUUID pgtype.UUID
	if err := householdUUID.Scan(householdID); err != nil {
		return household.Household{}, household.ErrNotFound
	}
	if err := userUUID.Scan(userID); err != nil {
		return household.Household{}, household.ErrNotFound
	}
	row, err := r.queries.FindMemberHousehold(ctx, dbgen.FindMemberHouseholdParams{ID: householdUUID, UserID: userUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		return household.Household{}, household.ErrNotFound
	}
	if err != nil {
		return household.Household{}, errors.New("household persistence failed")
	}
	return household.Household{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time.UTC(), Version: row.Version, ViewerRole: row.ViewerRole}, nil
}

// ListForMember returns one page of the caller's member households in ID order.
func (r *Repository) ListForMember(ctx context.Context, userID, afterID string, limit int) ([]household.ListedHousehold, string, error) {
	var userUUID, afterUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return nil, "", household.ErrNotFound
	}
	if afterID != "" {
		if err := afterUUID.Scan(afterID); err != nil {
			return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, err := r.queries.ListMemberHouseholds(ctx, dbgen.ListMemberHouseholdsParams{UserID: userUUID, Column2: afterUUID, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = household.EncodeHouseholdCursor(rows[len(rows)-1].ID)
	}
	out := make([]household.ListedHousehold, 0, len(rows))
	for _, row := range rows {
		out = append(out, household.ListedHousehold{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time.UTC(), ViewerRole: row.ViewerRole})
	}
	return out, next, nil
}
