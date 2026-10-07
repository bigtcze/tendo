package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var completeAfterInsertHook func() error

func dbDate(d *schedule.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return date(d)
}
func decodeDate(d pgtype.Date) (*schedule.Date, error) {
	if !d.Valid {
		return nil, nil
	}
	if d.InfinityModifier != pgtype.Finite {
		return nil, errPersistence
	}
	v, err := schedule.NewDate(d.Time.Year(), d.Time.Month(), d.Time.Day())
	if err != nil {
		return nil, errPersistence
	}
	return &v, nil
}
func decodeReceipt(id, household, itemID, userID string, completed, cycle, next pgtype.Date, iv pgtype.Int4, unit, mode pgtype.Text, state, key string, fingerprint []byte, before int64, created pgtype.Timestamptz) (item.Completion, error) {
	if len(fingerprint) != sha256Size || before < 1 || !item.ValidateIdempotencyKey(key) || len(id) != 36 || len(household) != 36 || len(itemID) != 36 || len(userID) != 36 || created.InfinityModifier != pgtype.Finite {
		return item.Completion{}, errPersistence
	}
	completedOn, err := decodeDate(completed)
	if err != nil || completedOn == nil {
		return item.Completion{}, errPersistence
	}
	cycleDate, err := decodeDate(cycle)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	nextDate, err := decodeDate(next)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	var recurrence *schedule.Policy
	parts := 0
	if iv.Valid {
		parts++
	}
	if unit.Valid {
		parts++
	}
	if mode.Valid {
		parts++
	}
	if parts != 0 && parts != 3 {
		return item.Completion{}, errPersistence
	}
	if parts == 3 {
		recurrence = &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: int(iv.Int32), Unit: schedule.Unit(unit.String)}, Mode: schedule.Mode(mode.String)}
		if recurrence.Validate() != nil {
			return item.Completion{}, errPersistence
		}
	}
	workflow, ok := item.ParseWorkflowState(state)
	if !ok {
		return item.Completion{}, errPersistence
	}
	var fp [sha256Size]byte
	copy(fp[:], fingerprint)
	if iv.Valid && (iv.Int32 < 1 || iv.Int32 > 999) {
		return item.Completion{}, errPersistence
	}
	if cycleDate == nil && nextDate != nil && recurrence == nil {
		return item.Completion{}, errPersistence
	}
	return item.Completion{ID: id, HouseholdID: household, ItemID: itemID, CompletedByUserID: userID, CompletedOn: *completedOn, CycleAttentionOn: cycleDate, Recurrence: recurrence, PriorWorkflowState: workflow, NextAttentionOn: nextDate, ItemVersionBefore: before, IdempotencyKey: key, Fingerprint: fp, CreatedAt: created.Time.UTC()}, nil
}

const sha256Size = 32

func completionFromRow(r dbgen.InsertCompletionRow) (item.Completion, error) {
	return decodeReceipt(r.ID, r.HouseholdID, r.ItemID, r.CompletedByUserID, r.CompletedOn, r.CycleAttentionOn, r.NextAttentionOn, r.RecurrenceIntervalValue, r.RecurrenceIntervalUnit, r.RecurrenceMode, r.PriorWorkflowState, r.IdempotencyKey, r.RequestFingerprint, r.ItemVersionBefore, r.CreatedAt)
}
func completionFromExisting(r dbgen.GetCompletionByKeyRow) (item.Completion, error) {
	return decodeReceipt(r.ID, r.HouseholdID, r.ItemID, r.CompletedByUserID, r.CompletedOn, r.CycleAttentionOn, r.NextAttentionOn, r.RecurrenceIntervalValue, r.RecurrenceIntervalUnit, r.RecurrenceMode, r.PriorWorkflowState, r.IdempotencyKey, r.RequestFingerprint, r.ItemVersionBefore, r.CreatedAt)
}
func completionFromList(r dbgen.ListCompletionsRow) (item.Completion, error) {
	return decodeReceipt(r.ID, r.HouseholdID, r.ItemID, r.CompletedByUserID, r.CompletedOn, r.CycleAttentionOn, r.NextAttentionOn, r.RecurrenceIntervalValue, r.RecurrenceIntervalUnit, r.RecurrenceMode, r.PriorWorkflowState, r.IdempotencyKey, r.RequestFingerprint, r.ItemVersionBefore, r.CreatedAt)
}
func completionCurrent(r dbgen.LockItemForCompletionRow) (item.Item, error) {
	return toItem(row{ID: r.ID, HouseholdID: r.HouseholdID, SubjectID: r.SubjectID, Title: r.Title, Notes: r.Notes, AttentionOn: r.AttentionOn, RecurrenceIntervalValue: r.RecurrenceIntervalValue, RecurrenceIntervalUnit: r.RecurrenceIntervalUnit, RecurrenceMode: r.RecurrenceMode, WorkflowState: r.WorkflowState, Archived: r.Archived, Done: r.Done, LastCompletedOn: r.LastCompletedOn, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version})
}
func (r *Repository) Complete(ctx context.Context, hid, iid, key string, fingerprint [32]byte, decide item.CompletionDecider) (item.Completion, bool, error) {
	h, ok := parseUUID(hid)
	if !ok {
		return item.Completion{}, false, item.ErrNotFound
	}
	id, ok := parseUUID(iid)
	if !ok {
		return item.Completion{}, false, item.ErrNotFound
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return item.Completion{}, false, errPersistence
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	locked, err := q.LockItemForCompletion(ctx, dbgen.LockItemForCompletionParams{HouseholdID: h, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Completion{}, false, item.ErrNotFound
	}
	if err != nil {
		return item.Completion{}, false, errPersistence
	}
	current, err := completionCurrent(locked)
	if err != nil {
		return item.Completion{}, false, errPersistence
	}
	existingRow, err := q.GetCompletionByKey(ctx, dbgen.GetCompletionByKeyParams{HouseholdID: h, ItemID: id, IdempotencyKey: key})
	var existing *item.Completion
	if err == nil {
		decoded, e := completionFromExisting(existingRow)
		if e != nil {
			return item.Completion{}, false, errPersistence
		}
		existing = &decoded
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return item.Completion{}, false, errPersistence
	}
	plan, err := decide(current, existing)
	if err != nil {
		return item.Completion{}, false, err
	}
	if existing != nil {
		if err = tx.Commit(ctx); err != nil {
			return item.Completion{}, false, errPersistence
		}
		return *existing, true, nil
	}
	receipt := plan.Receipt
	params := dbgen.InsertCompletionParams{HouseholdID: h, ItemID: id, CompletedOn: date(&receipt.CompletedOn), CompletedByUserID: uuid(receipt.CompletedByUserID), CycleAttentionOn: dbDate(receipt.CycleAttentionOn), PriorWorkflowState: string(receipt.PriorWorkflowState), NextAttentionOn: dbDate(receipt.NextAttentionOn), ItemVersionBefore: receipt.ItemVersionBefore, IdempotencyKey: key, RequestFingerprint: fingerprint[:]}
	if receipt.Recurrence != nil {
		params.RecurrenceIntervalValue = pgtype.Int4{Int32: int32(receipt.Recurrence.Interval.Value), Valid: true}
		params.RecurrenceIntervalUnit = pgtype.Text{String: string(receipt.Recurrence.Interval.Unit), Valid: true}
		params.RecurrenceMode = pgtype.Text{String: string(receipt.Recurrence.Mode), Valid: true}
	}
	stored, err := q.InsertCompletion(ctx, params)
	if err != nil {
		return item.Completion{}, false, errPersistence
	}
	if completeAfterInsertHook != nil {
		if err = completeAfterInsertHook(); err != nil {
			if errors.Is(err, item.ErrUnavailable) {
				return item.Completion{}, false, item.ErrUnavailable
			}
			return item.Completion{}, false, err
		}
	}
	if err = q.UpdateItemForCompletion(ctx, dbgen.UpdateItemForCompletionParams{HouseholdID: h, ID: id, AttentionOn: dbDate(plan.Item.AttentionOn), WorkflowState: string(plan.Item.WorkflowState), Done: plan.Item.Done}); err != nil {
		return item.Completion{}, false, errPersistence
	}
	result, err := completionFromRow(stored)
	if err != nil {
		return item.Completion{}, false, errPersistence
	}
	if err = tx.Commit(ctx); err != nil {
		return item.Completion{}, false, errPersistence
	}
	return result, false, nil
}
func uuid(s string) pgtype.UUID { u, _ := parseUUID(s); return u }
func (r *Repository) ListCompletions(ctx context.Context, hid, iid, after string, limit int) ([]item.Completion, error) {
	h, ok1 := parseUUID(hid)
	id, ok2 := parseUUID(iid)
	afterID, ok3 := parseUUID(after)
	if !ok1 || !ok2 || !ok3 {
		return nil, item.ErrNotFound
	}
	if _, err := r.queries.GetItem(ctx, dbgen.GetItemParams{HouseholdID: h, ID: id}); errors.Is(err, pgx.ErrNoRows) {
		return nil, item.ErrNotFound
	} else if err != nil {
		return nil, errPersistence
	}
	rows, err := r.queries.ListCompletions(ctx, dbgen.ListCompletionsParams{HouseholdID: h, ItemID: id, ID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, errPersistence
	}
	out := make([]item.Completion, 0, len(rows))
	for _, row := range rows {
		c, e := completionFromList(row)
		if e != nil {
			return nil, errPersistence
		}
		out = append(out, c)
	}
	return out, nil
}
