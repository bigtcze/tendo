package postgres

import (
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5/pgtype"
)

type row struct {
	ID, HouseholdID, SubjectID, Title string
	ResponsibleUserID                 pgtype.UUID
	Notes                             pgtype.Text
	AttentionOn                       pgtype.Date
	RecurrenceIntervalValue           pgtype.Int4
	RecurrenceIntervalUnit            pgtype.Text
	RecurrenceMode                    pgtype.Text
	WorkflowState                     string
	Archived                          bool
	Done                              bool
	LastCompletedOn                   pgtype.Date
	CreatedAt, UpdatedAt              pgtype.Timestamptz
	Version                           int64
}

func toItem(r row) (item.Item, error) {
	out := item.Item{ID: r.ID, HouseholdID: r.HouseholdID, SubjectID: r.SubjectID,
		ResponsibleUserID: uuidPtr(r.ResponsibleUserID), Title: r.Title,
		WorkflowState: item.WorkflowState(r.WorkflowState), Archived: r.Archived, Done: r.Done,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(), Version: r.Version}
	out.Notes = nullableText(r.Notes)
	recurrence, err := recurrenceFromRow(r)
	if err != nil {
		return item.Item{}, err
	}
	out.Recurrence = recurrence
	lastCompleted, err := dateFromRow(r.LastCompletedOn, false)
	if err != nil {
		return item.Item{}, err
	}
	out.LastCompletedOn = lastCompleted
	attention, err := dateFromRow(r.AttentionOn, true)
	if err != nil {
		return item.Item{}, err
	}
	out.AttentionOn = attention
	return out, nil
}

func nullableText(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func recurrenceFromRow(r row) (*schedule.Policy, error) {
	valid := 0
	for _, present := range []bool{r.RecurrenceIntervalValue.Valid, r.RecurrenceIntervalUnit.Valid, r.RecurrenceMode.Valid} {
		if present {
			valid++
		}
	}
	if valid == 0 {
		return nil, nil
	}
	if valid != 3 || r.RecurrenceIntervalValue.Int32 < 1 || r.RecurrenceIntervalValue.Int32 > 999 {
		return nil, errPersistence
	}
	policy := &schedule.Policy{
		Enabled: true,
		Interval: schedule.Interval{
			Value: int(r.RecurrenceIntervalValue.Int32),
			Unit:  schedule.Unit(r.RecurrenceIntervalUnit.String),
		},
		Mode: schedule.Mode(r.RecurrenceMode.String),
	}
	if policy.Validate() != nil {
		return nil, errPersistence
	}
	return policy, nil
}

func dateFromRow(value pgtype.Date, rejectInfinity bool) (*schedule.Date, error) {
	if !value.Valid {
		return nil, nil
	}
	if rejectInfinity && value.InfinityModifier != pgtype.Finite {
		return nil, errPersistence
	}
	date, err := schedule.NewDate(value.Time.Year(), value.Time.Month(), value.Time.Day())
	if err != nil {
		return nil, errPersistence
	}
	return &date, nil
}

func createdRow(found dbgen.CreateItemRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}

func getRow(found dbgen.GetItemRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}

func findCreationRow(found dbgen.FindItemCreationRow) row {
	return row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID,
		Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState,
		Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt,
		UpdatedAt: found.UpdatedAt, Version: found.Version}
}
