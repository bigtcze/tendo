package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func loadCompletionForWrite(ctx context.Context, q *dbgen.Queries, household, id pgtype.UUID, key string) (item.Item, *item.Completion, error) {
	locked, err := q.LockItemForCompletion(ctx, dbgen.LockItemForCompletionParams{HouseholdID: household, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, nil, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	current, err := completionCurrent(locked)
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	row, err := q.GetCompletionByKey(ctx, dbgen.GetCompletionByKeyParams{HouseholdID: household, ItemID: id, IdempotencyKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return current, nil, nil
	}
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	existing, err := completionFromExisting(row)
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	return current, &existing, nil
}

func completionInsertParams(household, id pgtype.UUID, key string, fingerprint [32]byte, receipt item.Completion) dbgen.InsertCompletionParams {
	params := dbgen.InsertCompletionParams{
		HouseholdID: household, ItemID: id, CompletedOn: date(&receipt.CompletedOn),
		CompletedByUserID: uuid(receipt.CompletedByUserID), CycleAttentionOn: dbDate(receipt.CycleAttentionOn),
		PriorWorkflowState: string(receipt.PriorWorkflowState), NextAttentionOn: dbDate(receipt.NextAttentionOn),
		ItemVersionBefore: receipt.ItemVersionBefore, IdempotencyKey: key, RequestFingerprint: fingerprint[:],
	}
	if receipt.Recurrence != nil {
		params.RecurrenceIntervalValue = pgtype.Int4{Int32: int32(receipt.Recurrence.Interval.Value), Valid: true}
		params.RecurrenceIntervalUnit = pgtype.Text{String: string(receipt.Recurrence.Interval.Unit), Valid: true}
		params.RecurrenceMode = pgtype.Text{String: string(receipt.Recurrence.Mode), Valid: true}
	}
	return params
}

func insertCompletionAndUpdateItem(
	ctx context.Context,
	q *dbgen.Queries,
	household, id pgtype.UUID,
	key string,
	fingerprint [32]byte,
	plan item.CompletionPlan,
) (item.Completion, error) {
	stored, err := q.InsertCompletion(ctx, completionInsertParams(household, id, key, fingerprint, plan.Receipt))
	if err != nil {
		return item.Completion{}, errPersistence
	}
	if completeAfterInsertHook != nil {
		if err = completeAfterInsertHook(); err != nil {
			if errors.Is(err, item.ErrUnavailable) {
				return item.Completion{}, item.ErrUnavailable
			}
			return item.Completion{}, err
		}
	}
	params := dbgen.UpdateItemForCompletionParams{
		HouseholdID: household, ID: id, AttentionOn: dbDate(plan.Item.AttentionOn),
		WorkflowState: string(plan.Item.WorkflowState), Done: plan.Item.Done,
	}
	if err = q.UpdateItemForCompletion(ctx, params); err != nil {
		return item.Completion{}, errPersistence
	}
	result, err := completionFromRow(stored)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	return result, nil
}
