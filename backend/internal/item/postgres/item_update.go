package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Update applies the change atomically when the stored version equals
// expectedVersion. When no row matches, a household-scoped lookup tells a
// missing item from a stale version.
func (r *Repository) Update(ctx context.Context, householdID, itemID string, expectedVersion int64, c item.Change) (item.Item, error) {
	params, err := updateParams(householdID, itemID, expectedVersion, c)
	if err != nil {
		return item.Item{}, err
	}
	updated, err := r.queries.UpdateItem(ctx, params)
	if err == nil {
		return toItem(updateRow(updated))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, classifyForeignKey(err)
	}
	return r.updateMissingResult(ctx, params)
}

func updateParams(householdID, itemID string, expectedVersion int64, c item.Change) (dbgen.UpdateItemParams, error) {
	hid, ok1 := parseUUID(householdID)
	iid, ok2 := parseUUID(itemID)
	if !ok1 || !ok2 {
		return dbgen.UpdateItemParams{}, item.ErrNotFound
	}
	params := dbgen.UpdateItemParams{HouseholdID: hid, ID: iid, ExpectedVersion: expectedVersion, Title: text(c.Title)}
	if err := mapUpdateReferences(&params, c); err != nil {
		return dbgen.UpdateItemParams{}, err
	}
	mapUpdateFields(&params, c)
	return params, nil
}

func mapUpdateReferences(params *dbgen.UpdateItemParams, c item.Change) error {
	if c.SubjectID != nil {
		sid, ok := parseUUID(*c.SubjectID)
		if !ok {
			return item.ErrInvalidReference
		}
		params.SubjectID = sid
	}
	if c.ResponsibleUserID.Set {
		responsibleID, err := nullableUUID(c.ResponsibleUserID.Value)
		if err != nil {
			return err
		}
		params.SetResponsibleUserID = true
		params.ResponsibleUserID = responsibleID
	}
	return nil
}

func mapUpdateFields(params *dbgen.UpdateItemParams, c item.Change) {
	if c.Notes.Set {
		params.SetNotes = true
		params.Notes = text(c.Notes.Value)
	}
	if c.AttentionOn.Set {
		params.SetAttentionOn = true
		params.AttentionOn = date(c.AttentionOn.Value)
	}
	if c.Recurrence.Set {
		params.SetRecurrence = true
		if c.Recurrence.Value != nil {
			params.RecurrenceIntervalValue = pgtype.Int4{Int32: int32(c.Recurrence.Value.Interval.Value), Valid: true}
			params.RecurrenceIntervalUnit = pgtype.Text{String: string(c.Recurrence.Value.Interval.Unit), Valid: true}
			params.RecurrenceMode = pgtype.Text{String: string(c.Recurrence.Value.Mode), Valid: true}
		}
	}
	if c.WorkflowState != nil {
		params.WorkflowState = pgtype.Text{String: string(*c.WorkflowState), Valid: true}
	}
	if c.Archived != nil {
		params.Archived = pgtype.Bool{Bool: *c.Archived, Valid: true}
	}
}

func (r *Repository) updateMissingResult(ctx context.Context, params dbgen.UpdateItemParams) (item.Item, error) {
	_, err := r.queries.GetItem(ctx, dbgen.GetItemParams{HouseholdID: params.HouseholdID, ID: params.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, errPersistence
	}
	return item.Item{}, item.ErrVersionMismatch
}

func updateRow(found dbgen.UpdateItemRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}
