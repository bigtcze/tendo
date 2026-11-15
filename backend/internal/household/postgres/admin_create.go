package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// householdCreationActorKeyConstraint is the only unique violation the create
// path treats as a race. Any other conflict is an unexpected persistence error.
const householdCreationActorKeyConstraint = "household_creations_actor_key_unique"

func (t *instanceAdminTx) InsertHousehold(ctx context.Context, name, timezone string, createdAt time.Time) (household.AdminHousehold, int64, error) {
	row, err := t.q.InsertHousehold(ctx, dbgen.InsertHouseholdParams{
		Name: name, Timezone: timezone,
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	})
	if err != nil {
		return household.AdminHousehold{}, 0, err
	}
	return household.AdminHousehold{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time}, row.Version, nil
}

func (t *instanceAdminTx) InsertHouseholdCreation(ctx context.Context, creation household.HouseholdCreation) error {
	house, err := scanUUID(creation.HouseholdID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(creation.ActorUserID)
	if err != nil {
		return household.ErrInstanceAdminRequired
	}
	err = t.q.InsertHouseholdCreation(ctx, dbgen.InsertHouseholdCreationParams{
		HouseholdID: house, ActorUserID: actor, CreationKey: creation.CreationKey,
		RequestFingerprint: creation.RequestFingerprint,
		CreatedAt:          pgtype.Timestamptz{Time: creation.CreatedAt, Valid: true},
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == householdCreationActorKeyConstraint {
		return household.ErrCreationKeyRaced
	}
	return err
}

func (t *instanceAdminTx) FindHouseholdCreation(ctx context.Context, actorID, key string) (household.AdminHousehold, int64, []byte, error) {
	actor, err := scanUUID(actorID)
	if err != nil {
		return household.AdminHousehold{}, 0, nil, household.ErrNotFound
	}
	row, err := t.q.FindHouseholdCreation(ctx, dbgen.FindHouseholdCreationParams{ActorUserID: actor, CreationKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return household.AdminHousehold{}, 0, nil, household.ErrNotFound
	}
	if err != nil {
		return household.AdminHousehold{}, 0, nil, err
	}
	return household.AdminHousehold{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time}, row.Version, row.RequestFingerprint, nil
}

func (t *instanceAdminTx) GetHousehold(ctx context.Context, householdID string) (household.AdminHousehold, int64, error) {
	house, err := scanUUID(householdID)
	if err != nil {
		return household.AdminHousehold{}, 0, household.ErrNotFound
	}
	row, err := t.q.GetHousehold(ctx, house)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.AdminHousehold{}, 0, household.ErrNotFound
	}
	if err != nil {
		return household.AdminHousehold{}, 0, err
	}
	return household.AdminHousehold{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time}, row.Version, nil
}
