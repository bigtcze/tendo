package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/bigtcze/tendo/backend/internal/subject/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func rowFromCreated(r dbgen.CreateSubjectRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func rowFromFields(id, householdID, typ, name string, archived bool, createdAt, updatedAt pgtype.Timestamptz, version int64, linkedUserID pgtype.UUID) row {
	return row{
		ID: id, HouseholdID: householdID, Type: typ, Name: name,
		Archived: archived, CreatedAt: createdAt, UpdatedAt: updatedAt,
		Version: version, LinkedUserID: linkedUserID,
	}
}

func rowFromFound(r dbgen.FindSubjectCreationRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func rowFromGet(r dbgen.GetSubjectRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func rowFromFilteredItem(r dbgen.ListSubjectsByIDsRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func rowFromItem(r dbgen.ListSubjectsRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func rowFromUpdated(r dbgen.UpdateSubjectRow) row {
	return rowFromFields(
		r.ID, r.HouseholdID, r.Type, r.Name, r.Archived,
		r.CreatedAt, r.UpdatedAt, r.Version, r.LinkedUserID,
	)
}

func toSubject(r row) subject.Subject {
	var linked *string
	if r.LinkedUserID.Valid {
		value := uuidString(r.LinkedUserID)
		linked = &value
	}
	return subject.Subject{
		ID: r.ID, HouseholdID: r.HouseholdID, Type: subject.Type(r.Type),
		Name: r.Name, Archived: r.Archived,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
		Version: r.Version, LinkedUserID: linked,
	}
}

func updateParams(hid, sid pgtype.UUID, expectedVersion int64, p subject.Patch) (dbgen.UpdateSubjectParams, error) {
	params := dbgen.UpdateSubjectParams{HouseholdID: hid, ID: sid, ExpectedVersion: expectedVersion, SetLinked: p.LinkedUserIDSet}
	if p.Name != nil {
		params.Name = pgtype.Text{String: *p.Name, Valid: true}
	}
	if p.Type != nil {
		params.Type = pgtype.Text{String: string(*p.Type), Valid: true}
	}
	if p.Archived != nil {
		params.Archived = pgtype.Bool{Bool: *p.Archived, Valid: true}
	}
	if p.LinkedUserIDSet {
		linkedID, err := nullableUUID(p.LinkedUserID)
		if err != nil {
			return dbgen.UpdateSubjectParams{}, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
		}
		params.LinkedUserID = linkedID
	}
	return params, nil
}

func (r *Repository) Update(ctx context.Context, householdID, subjectID string, expectedVersion int64, p subject.Patch) (subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	sid, ok2 := parseUUID(subjectID)
	if !ok1 || !ok2 {
		return subject.Subject{}, subject.ErrNotFound
	}
	params, err := updateParams(hid, sid, expectedVersion, p)
	if err != nil {
		return subject.Subject{}, err
	}
	updated, err := r.queries.UpdateSubject(ctx, params)
	if err == nil {
		return toSubject(rowFromUpdated(updated)), nil
	}
	if mapped := updateConstraintError(err); mapped != nil {
		return subject.Subject{}, mapped
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, errPersistence
	}
	if _, err = r.queries.GetSubject(ctx, dbgen.GetSubjectParams{HouseholdID: hid, ID: sid}); errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, subject.ErrNotFound
	} else if err != nil {
		return subject.Subject{}, errPersistence
	}
	return subject.Subject{}, subject.ErrVersionMismatch
}
