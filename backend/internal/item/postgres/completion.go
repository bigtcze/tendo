package postgres

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

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
func decodeReceipt(
	id, household, itemID, userID string,
	completed, cycle, next pgtype.Date,
	iv pgtype.Int4, unit, mode pgtype.Text,
	state, key string,
	fingerprint []byte,
	before int64,
	created pgtype.Timestamptz,
) (item.Completion, error) {
	if !validReceiptMetadata(id, household, itemID, userID, key, fingerprint, before, created) {
		return item.Completion{}, errPersistence
	}
	completedOn, cycleDate, nextDate, err := decodeReceiptDates(completed, cycle, next)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	recurrence, err := decodeRecurrence(iv, unit, mode)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	workflow, ok := item.ParseWorkflowState(state)
	if !ok || (cycleDate == nil && nextDate != nil && recurrence == nil) || (recurrence != nil && nextDate == nil) {
		return item.Completion{}, errPersistence
	}
	var fp [sha256Size]byte
	copy(fp[:], fingerprint)
	return item.Completion{
		ID: id, HouseholdID: household, ItemID: itemID, CompletedByUserID: userID,
		CompletedOn: *completedOn, CycleAttentionOn: cycleDate, Recurrence: recurrence,
		PriorWorkflowState: workflow, NextAttentionOn: nextDate, ItemVersionBefore: before,
		IdempotencyKey: key, Fingerprint: fp, CreatedAt: created.Time.UTC(),
	}, nil
}

func validReceiptMetadata(id, household, itemID, userID, key string, fingerprint []byte, before int64, created pgtype.Timestamptz) bool {
	return len(fingerprint) == sha256Size && before >= 1 && item.ValidateIdempotencyKey(key) &&
		len(id) == 36 && len(household) == 36 && len(itemID) == 36 && len(userID) == 36 && created.InfinityModifier == pgtype.Finite
}

func decodeReceiptDates(completed, cycle, next pgtype.Date) (*schedule.Date, *schedule.Date, *schedule.Date, error) {
	completedOn, err := decodeDate(completed)
	if err != nil || completedOn == nil {
		return nil, nil, nil, errPersistence
	}
	cycleDate, err := decodeDate(cycle)
	if err != nil {
		return nil, nil, nil, errPersistence
	}
	nextDate, err := decodeDate(next)
	if err != nil {
		return nil, nil, nil, errPersistence
	}
	return completedOn, cycleDate, nextDate, nil
}

func decodeRecurrence(iv pgtype.Int4, unit, mode pgtype.Text) (*schedule.Policy, error) {
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
	if parts == 0 {
		return nil, nil
	}
	if parts != 3 || iv.Int32 < 1 || iv.Int32 > 999 {
		return nil, errPersistence
	}
	policy := &schedule.Policy{
		Enabled:  true,
		Interval: schedule.Interval{Value: int(iv.Int32), Unit: schedule.Unit(unit.String)},
		Mode:     schedule.Mode(mode.String),
	}
	if policy.Validate() != nil {
		return nil, errPersistence
	}
	return policy, nil
}

const sha256Size = 32

func withUndo(c item.Completion, at pgtype.Timestamptz, userID pgtype.UUID, err error) (item.Completion, error) {
	if err != nil {
		return item.Completion{}, err
	}
	if at.Valid != userID.Valid {
		return item.Completion{}, errPersistence
	}
	if !at.Valid {
		return c, nil
	}
	if at.InfinityModifier != pgtype.Finite {
		return item.Completion{}, errPersistence
	}
	t := at.Time.UTC()
	c.UndoneAt = &t
	u := uuidString(userID)
	c.UndoneByUserID = &u
	return c, nil
}

func completionCurrent(r dbgen.LockItemForCompletionRow) (item.Item, error) {
	return toItem(row{
		ID: r.ID, HouseholdID: r.HouseholdID, SubjectID: r.SubjectID, ResponsibleUserID: r.ResponsibleUserID,
		Title: r.Title, Notes: r.Notes, AttentionOn: r.AttentionOn,
		RecurrenceIntervalValue: r.RecurrenceIntervalValue, RecurrenceIntervalUnit: r.RecurrenceIntervalUnit,
		RecurrenceMode: r.RecurrenceMode, WorkflowState: r.WorkflowState, Archived: r.Archived,
		Done: r.Done, LastCompletedOn: r.LastCompletedOn, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
	})
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
	current, existing, err := loadCompletionForWrite(ctx, q, h, id, key)
	if err != nil {
		return item.Completion{}, false, err
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
	result, err := insertCompletionAndUpdateItem(ctx, q, h, id, key, fingerprint, plan)
	if err != nil {
		return item.Completion{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return item.Completion{}, false, errPersistence
	}
	return result, false, nil
}
func (r *Repository) UndoCompletion(ctx context.Context, hid, iid, cid string, decide item.CompletionUndoDecider) (item.Completion, error) {
	h, ok1 := parseUUID(hid)
	i, ok2 := parseUUID(iid)
	c, ok3 := parseUUID(cid)
	if !ok1 || !ok2 || !ok3 {
		return item.Completion{}, item.ErrNotFound
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return item.Completion{}, errPersistence
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	current, receipt, latest, err := loadCompletionUndo(ctx, q, h, i, c)
	if err != nil {
		return item.Completion{}, err
	}
	plan, err := decide(current, receipt, latest)
	if err != nil {
		return item.Completion{}, err
	}
	if plan.Apply {
		plan.Receipt, err = persistCompletionUndo(ctx, q, h, i, c, plan)
		if err != nil {
			return item.Completion{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return item.Completion{}, errPersistence
	}
	return plan.Receipt, nil
}

func uuid(s string) pgtype.UUID { u, _ := parseUUID(s); return u }
func uuidString(u pgtype.UUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(u.Bytes[0:4]), binary.BigEndian.Uint16(u.Bytes[4:6]),
		binary.BigEndian.Uint16(u.Bytes[6:8]), binary.BigEndian.Uint16(u.Bytes[8:10]), u.Bytes[10:16])
}
func (r *Repository) ListCompletions(ctx context.Context, hid, iid, after string, limit int, newest bool) ([]item.Completion, error) {
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
	if newest {
		rows, err := r.queries.ListCompletionsNewest(ctx, dbgen.ListCompletionsNewestParams{HouseholdID: h, ItemID: id, ID: afterID, Limit: int32(limit)})
		if err != nil {
			return nil, errPersistence
		}
		return completionRowsFromNewest(rows)
	}
	rows, err := r.queries.ListCompletions(ctx, dbgen.ListCompletionsParams{HouseholdID: h, ItemID: id, ID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, errPersistence
	}
	return completionRowsFromList(rows)
}

func completionRowsFromList(rows []dbgen.ListCompletionsRow) ([]item.Completion, error) {
	out := make([]item.Completion, 0, len(rows))
	for _, row := range rows {
		c, err := completionFromList(row)
		if err != nil {
			return nil, errPersistence
		}
		out = append(out, c)
	}
	return out, nil
}

func completionRowsFromNewest(rows []dbgen.ListCompletionsNewestRow) ([]item.Completion, error) {
	converted := make([]dbgen.ListCompletionsRow, len(rows))
	for i, row := range rows {
		converted[i] = dbgen.ListCompletionsRow(row)
	}
	return completionRowsFromList(converted)
}
