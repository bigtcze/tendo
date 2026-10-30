package postgres

import (
	"context"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) List(ctx context.Context, householdID string, opts item.ListOptions) ([]item.Item, error) {
	if opts.Sort == "" {
		opts.Sort = item.SortCreated
	}
	params, err := listParams(householdID, opts)
	if err != nil {
		return nil, err
	}
	return r.executeList(ctx, params, opts)
}

func listParams(householdID string, opts item.ListOptions) (dbgen.ListItemsParams, error) {
	hid, ok1 := parseUUID(householdID)
	after, ok2 := parseUUID(opts.AfterID)
	if !ok1 || !ok2 {
		return dbgen.ListItemsParams{}, item.ErrNotFound
	}
	f := opts.Filters
	var subjectID pgtype.UUID
	if f.SubjectID != "" {
		var ok bool
		subjectID, ok = parseUUID(f.SubjectID)
		if !ok {
			return dbgen.ListItemsParams{}, item.ErrNotFound
		}
	}
	var responsible pgtype.UUID
	if f.ResponsibleUserID.Value != nil {
		var ok bool
		responsible, ok = parseUUID(*f.ResponsibleUserID.Value)
		if !ok {
			return dbgen.ListItemsParams{}, item.ErrNotFound
		}
	}
	workflow, attention := pgtype.Text{}, pgtype.Text{}
	if f.WorkflowState != "" {
		workflow = pgtype.Text{String: string(f.WorkflowState), Valid: true}
	}
	if f.Attention != "" {
		attention = pgtype.Text{String: string(f.Attention), Valid: true}
	}
	search := pgtype.Text{}
	if f.Q != "" {
		search = pgtype.Text{String: f.Q, Valid: true}
	}
	var recurring pgtype.Bool
	if f.Recurring != nil {
		recurring = pgtype.Bool{Bool: *f.Recurring, Valid: true}
	}
	return dbgen.ListItemsParams{HouseholdID: hid, Archived: f.Archived, Done: f.Done, AfterID: after,
		SubjectID: subjectID, FilterResponsible: f.ResponsibleUserID.Set, ResponsibleUserID: responsible,
		WorkflowState: workflow, Attention: attention, Today: date(&opts.Today), Recurring: recurring,
		Q: search, RowLimit: int32(opts.Limit)}, nil
}

func (r *Repository) executeList(ctx context.Context, params dbgen.ListItemsParams, opts item.ListOptions) ([]item.Item, error) {
	var rows []row
	switch opts.Sort {
	case item.SortCreated:
		found, err := r.queries.ListItems(ctx, params)
		if err != nil {
			return nil, errPersistence
		}
		rows = make([]row, 0, len(found))
		for _, value := range found {
			rows = append(rows, listRow(value))
		}
	case item.SortAttention:
		found, err := r.queries.ListItemsByAttention(ctx, dbgen.ListItemsByAttentionParams{
			HouseholdID: params.HouseholdID, Archived: params.Archived, Done: params.Done, AfterID: params.AfterID,
			AfterAttentionOn: date(opts.AfterAttentionOn), SubjectID: params.SubjectID,
			FilterResponsible: params.FilterResponsible, ResponsibleUserID: params.ResponsibleUserID,
			WorkflowState: params.WorkflowState, Attention: params.Attention, Today: params.Today,
			Recurring: params.Recurring, Q: params.Q, RowLimit: params.RowLimit,
		})
		if err != nil {
			return nil, errPersistence
		}
		rows = make([]row, 0, len(found))
		for _, value := range found {
			rows = append(rows, attentionRow(value))
		}
	default:
		return nil, item.ErrUnavailable
	}
	return convertRows(rows)
}

func convertRows(rows []row) ([]item.Item, error) {
	items := make([]item.Item, 0, len(rows))
	for _, value := range rows {
		converted, err := toItem(value)
		if err != nil {
			return nil, err
		}
		items = append(items, converted)
	}
	return items, nil
}

func listRow(found dbgen.ListItemsRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}

func attentionRow(found dbgen.ListItemsByAttentionRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}
