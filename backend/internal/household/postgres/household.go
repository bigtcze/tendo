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
	return household.Household{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time.UTC(), Version: row.Version}, nil
}
