package postgres

import (
	"context"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) Summary(ctx context.Context, householdID string, opts item.SummaryOptions) (item.SummaryCounts, error) {
	hid, ok := parseUUID(householdID)
	if !ok {
		return item.SummaryCounts{}, item.ErrNotFound
	}
	f := opts.Filters
	var subject, responsible pgtype.UUID
	if f.SubjectID != "" {
		var valid bool
		subject, valid = parseUUID(f.SubjectID)
		if !valid {
			return item.SummaryCounts{}, item.ErrNotFound
		}
	}
	if f.ResponsibleUserID.Value != nil {
		var valid bool
		responsible, valid = parseUUID(*f.ResponsibleUserID.Value)
		if !valid {
			return item.SummaryCounts{}, item.ErrNotFound
		}
	}
	workflow, attention := pgtype.Text{}, pgtype.Text{}
	if f.WorkflowState != "" {
		workflow = pgtype.Text{String: string(f.WorkflowState), Valid: true}
	}
	if f.Attention != "" {
		attention = pgtype.Text{String: string(f.Attention), Valid: true}
	}
	today := date(&opts.Today)
	var recurring pgtype.Bool
	if f.Recurring != nil {
		recurring = pgtype.Bool{Bool: *f.Recurring, Valid: true}
	}
	counts, err := r.queries.SummarizeItems(ctx, dbgen.SummarizeItemsParams{HouseholdID: hid, SubjectID: subject, FilterResponsible: f.ResponsibleUserID.Set, ResponsibleUserID: responsible, WorkflowState: workflow, Attention: attention, Today: today, Recurring: recurring})
	if err != nil {
		return item.SummaryCounts{}, errPersistence
	}
	return item.SummaryCounts{Total: counts.Total, NeedsAttention: counts.NeedsAttention, InProgress: counts.InProgress, Waiting: counts.Waiting, Upcoming: counts.Upcoming, Paused: counts.Paused, Done: counts.Done, Archived: counts.Archived}, nil
}
