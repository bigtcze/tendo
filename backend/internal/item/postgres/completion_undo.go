package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func loadCompletionUndo(ctx context.Context, q *dbgen.Queries, household, itemID, completionID pgtype.UUID) (item.Item, item.Completion, *int64, error) {
	locked, err := q.LockItemForCompletion(ctx, dbgen.LockItemForCompletionParams{HouseholdID: household, ID: itemID})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.Completion{}, nil, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, item.Completion{}, nil, errPersistence
	}
	current, err := completionCurrent(locked)
	if err != nil {
		return item.Item{}, item.Completion{}, nil, errPersistence
	}
	raw, err := q.GetCompletionForUndo(ctx, dbgen.GetCompletionForUndoParams{HouseholdID: household, ItemID: itemID, ID: completionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.Completion{}, nil, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, item.Completion{}, nil, errPersistence
	}
	receipt, err := completionFromUndo(raw)
	if err != nil {
		return item.Item{}, item.Completion{}, nil, errPersistence
	}
	version, err := q.LatestActiveCompletionVersion(ctx, dbgen.LatestActiveCompletionVersionParams{HouseholdID: household, ItemID: itemID})
	if errors.Is(err, pgx.ErrNoRows) {
		return current, receipt, nil, nil
	}
	if err != nil {
		return item.Item{}, item.Completion{}, nil, errPersistence
	}
	return current, receipt, &version, nil
}

func persistCompletionUndo(
	ctx context.Context,
	q *dbgen.Queries,
	household, itemID, completionID pgtype.UUID,
	plan item.CompletionUndoPlan,
) (item.Completion, error) {
	mark := dbgen.MarkCompletionUndoneParams{
		HouseholdID: household, ItemID: itemID, ID: completionID,
		UndoneAt: pgtype.Timestamptz{Time: plan.UndoneAt, Valid: true}, UndoneByUserID: uuid(plan.UndoneByUserID),
	}
	rows, err := q.MarkCompletionUndone(ctx, mark)
	if err != nil || rows != 1 {
		return item.Completion{}, errPersistence
	}
	update := dbgen.UpdateItemForUndoParams{
		HouseholdID: household, ID: itemID, AttentionOn: dbDate(plan.Item.AttentionOn),
		WorkflowState: string(plan.Item.WorkflowState),
	}
	if err = q.UpdateItemForUndo(ctx, update); err != nil {
		return item.Completion{}, errPersistence
	}
	stored, err := q.GetCompletionForUndo(ctx, dbgen.GetCompletionForUndoParams{HouseholdID: household, ItemID: itemID, ID: completionID})
	if err != nil {
		return item.Completion{}, errPersistence
	}
	receipt, err := completionFromUndo(stored)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	return receipt, nil
}
