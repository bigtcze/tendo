package postgres

import (
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

type receiptRow struct {
	id, householdID, itemID, completedBy           string
	completedOn, cycleAttentionOn, nextAttentionOn pgtype.Date
	recurrenceIntervalValue                        pgtype.Int4
	recurrenceIntervalUnit, recurrenceMode         pgtype.Text
	priorWorkflowState, idempotencyKey             string
	fingerprint                                    []byte
	versionBefore                                  int64
	createdAt, undoneAt                            pgtype.Timestamptz
	undoneBy                                       pgtype.UUID
}

func (r receiptRow) completion() (item.Completion, error) {
	c, err := decodeReceipt(
		r.id, r.householdID, r.itemID, r.completedBy,
		r.completedOn, r.cycleAttentionOn, r.nextAttentionOn,
		r.recurrenceIntervalValue, r.recurrenceIntervalUnit, r.recurrenceMode,
		r.priorWorkflowState, r.idempotencyKey, r.fingerprint, r.versionBefore, r.createdAt,
	)
	return withUndo(c, r.undoneAt, r.undoneBy, err)
}

func completionFromRowValues(
	id, householdID, itemID, completedBy string,
	completedOn, cycleAttentionOn, nextAttentionOn pgtype.Date,
	recurrenceIntervalValue pgtype.Int4,
	recurrenceIntervalUnit, recurrenceMode pgtype.Text,
	priorWorkflowState, idempotencyKey string,
	fingerprint []byte,
	versionBefore int64,
	createdAt, undoneAt pgtype.Timestamptz,
	undoneBy pgtype.UUID,
) (item.Completion, error) {
	return (receiptRow{
		id: id, householdID: householdID, itemID: itemID, completedBy: completedBy,
		completedOn: completedOn, cycleAttentionOn: cycleAttentionOn, nextAttentionOn: nextAttentionOn,
		recurrenceIntervalValue: recurrenceIntervalValue, recurrenceIntervalUnit: recurrenceIntervalUnit,
		recurrenceMode: recurrenceMode, priorWorkflowState: priorWorkflowState, idempotencyKey: idempotencyKey,
		fingerprint: fingerprint, versionBefore: versionBefore, createdAt: createdAt, undoneAt: undoneAt, undoneBy: undoneBy,
	}).completion()
}

func completionFromRow(r dbgen.InsertCompletionRow) (item.Completion, error) {
	return completionFromRowValues(
		r.ID, r.HouseholdID, r.ItemID, r.CompletedByUserID,
		r.CompletedOn, r.CycleAttentionOn, r.NextAttentionOn,
		r.RecurrenceIntervalValue, r.RecurrenceIntervalUnit, r.RecurrenceMode,
		r.PriorWorkflowState, r.IdempotencyKey, r.RequestFingerprint,
		r.ItemVersionBefore, r.CreatedAt, r.UndoneAt, r.UndoneByUserID,
	)
}

func completionFromInitializationRow(r dbgen.InsertInitializationCompletionRow) (item.Completion, error) {
	return completionFromRowValues(
		r.ID, r.HouseholdID, r.ItemID, r.CompletedByUserID,
		r.CompletedOn, r.CycleAttentionOn, r.NextAttentionOn,
		r.RecurrenceIntervalValue, r.RecurrenceIntervalUnit, r.RecurrenceMode,
		r.PriorWorkflowState, r.IdempotencyKey, r.RequestFingerprint,
		r.ItemVersionBefore, r.CreatedAt, r.UndoneAt, r.UndoneByUserID,
	)
}

func completionFromExisting(r dbgen.GetCompletionByKeyRow) (item.Completion, error) {
	return (receiptRow{
		id: r.ID, householdID: r.HouseholdID, itemID: r.ItemID, completedBy: r.CompletedByUserID,
		completedOn: r.CompletedOn, cycleAttentionOn: r.CycleAttentionOn, nextAttentionOn: r.NextAttentionOn,
		recurrenceIntervalValue: r.RecurrenceIntervalValue, recurrenceIntervalUnit: r.RecurrenceIntervalUnit,
		recurrenceMode: r.RecurrenceMode, priorWorkflowState: r.PriorWorkflowState, idempotencyKey: r.IdempotencyKey,
		fingerprint: r.RequestFingerprint, versionBefore: r.ItemVersionBefore, createdAt: r.CreatedAt,
		undoneAt: r.UndoneAt, undoneBy: r.UndoneByUserID,
	}).completion()
}

func completionFromList(r dbgen.ListCompletionsRow) (item.Completion, error) {
	return (receiptRow{
		id: r.ID, householdID: r.HouseholdID, itemID: r.ItemID, completedBy: r.CompletedByUserID,
		completedOn: r.CompletedOn, cycleAttentionOn: r.CycleAttentionOn, nextAttentionOn: r.NextAttentionOn,
		recurrenceIntervalValue: r.RecurrenceIntervalValue, recurrenceIntervalUnit: r.RecurrenceIntervalUnit,
		recurrenceMode: r.RecurrenceMode, priorWorkflowState: r.PriorWorkflowState, idempotencyKey: r.IdempotencyKey,
		fingerprint: r.RequestFingerprint, versionBefore: r.ItemVersionBefore, createdAt: r.CreatedAt,
		undoneAt: r.UndoneAt, undoneBy: r.UndoneByUserID,
	}).completion()
}

func completionFromUndo(r dbgen.GetCompletionForUndoRow) (item.Completion, error) {
	return (receiptRow{
		id: r.ID, householdID: r.HouseholdID, itemID: r.ItemID, completedBy: r.CompletedByUserID,
		completedOn: r.CompletedOn, cycleAttentionOn: r.CycleAttentionOn, nextAttentionOn: r.NextAttentionOn,
		recurrenceIntervalValue: r.RecurrenceIntervalValue, recurrenceIntervalUnit: r.RecurrenceIntervalUnit,
		recurrenceMode: r.RecurrenceMode, priorWorkflowState: r.PriorWorkflowState, idempotencyKey: r.IdempotencyKey,
		fingerprint: r.RequestFingerprint, versionBefore: r.ItemVersionBefore, createdAt: r.CreatedAt,
		undoneAt: r.UndoneAt, undoneBy: r.UndoneByUserID,
	}).completion()
}
