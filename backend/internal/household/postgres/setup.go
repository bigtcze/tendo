package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

type BootstrapRepository struct {
	queries *dbgen.Queries
}

func NewBootstrapRepository(queries *dbgen.Queries) *BootstrapRepository {
	return &BootstrapRepository{queries: queries}
}

func (r *BootstrapRepository) CreateBootstrapHousehold(ctx context.Context, input household.Bootstrap) (string, error) {
	id, err := r.queries.CreateHousehold(ctx, dbgen.CreateHouseholdParams{Name: input.Name, Timezone: input.Timezone})
	if err != nil {
		return "", errors.New("household persistence failed")
	}
	return id, nil
}

func (r *BootstrapRepository) AddOwner(ctx context.Context, userID, householdID string) error {
	var userUUID, householdUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return err
	}
	if err := householdUUID.Scan(householdID); err != nil {
		return err
	}
	return r.queries.AddOwner(ctx, dbgen.AddOwnerParams{UserID: userUUID, HouseholdID: householdUUID})
}
