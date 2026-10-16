package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/bigtcze/tendo/backend/internal/subject/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Repository stores subjects through the runtime-role connection. Every query
// filters by household and subject id, so cross-household access is impossible
// at the SQL level regardless of authorization.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

var errPersistence = errors.New("subject persistence failed")

func nullableUUID(s *string) (pgtype.UUID, error) {
	if s == nil {
		return pgtype.UUID{}, nil
	}
	var u pgtype.UUID
	if err := u.Scan(*s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func uuidString(u pgtype.UUID) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, false
	}
	return u, true
}

type row struct {
	ID, HouseholdID, Type, Name string
	LinkedUserID                pgtype.UUID
	Archived                    bool
	CreatedAt, UpdatedAt        pgtype.Timestamptz
	Version                     int64
}

func toSubject(r row) subject.Subject {
	var linked *string
	if r.LinkedUserID.Valid {
		value := uuidString(r.LinkedUserID)
		linked = &value
	}
	return subject.Subject{ID: r.ID, HouseholdID: r.HouseholdID, Type: subject.Type(r.Type), Name: r.Name, Archived: r.Archived, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(), Version: r.Version, LinkedUserID: linked}
}

func (r *Repository) CreateIdempotent(ctx context.Context, householdID, actorID, key, name string, t subject.Type, linkedUserID *string, fingerprint []byte) (subject.Subject, bool, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return subject.Subject{}, false, subject.ErrNotFound
	}
	linkedID, err := nullableUUID(linkedUserID)
	if err != nil {
		return subject.Subject{}, false, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
	}
	created, err := r.queries.CreateSubject(ctx, dbgen.CreateSubjectParams{HouseholdID: hid, Type: string(t), Name: name, LinkedUserID: linkedID, CreatedByUserID: actor, CreationKey: pgtype.Text{String: key, Valid: true}, CreationRequestFingerprint: fingerprint})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" && pgErr.ConstraintName == "subjects_linked_membership_fk" {
			return subject.Subject{}, false, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
		}
		if pgErr.Code == "23514" && pgErr.ConstraintName == "subjects_linked_user_person_only" {
			return subject.Subject{}, false, &subject.ValidationError{Field: "linkedUserId", Code: "requires_person"}
		}
		if pgErr.Code == "23503" {
			return subject.Subject{}, false, subject.ErrNotFound
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, false, nil
	}
	if err != nil {
		return subject.Subject{}, false, errPersistence
	}
	return toSubject(row{ID: created.ID, HouseholdID: created.HouseholdID, Type: created.Type, Name: created.Name, Archived: created.Archived, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt, Version: created.Version, LinkedUserID: created.LinkedUserID}), true, nil
}

func (r *Repository) FindCreation(ctx context.Context, householdID, actorID, key string) (subject.Subject, []byte, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	found, err := r.queries.FindSubjectCreation(ctx, dbgen.FindSubjectCreationParams{HouseholdID: hid, CreatedByUserID: actor, CreationKey: pgtype.Text{String: key, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	if err != nil {
		return subject.Subject{}, nil, errPersistence
	}
	return toSubject(row{ID: found.ID, HouseholdID: found.HouseholdID, Type: found.Type, Name: found.Name, Archived: found.Archived, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version, LinkedUserID: found.LinkedUserID}), found.CreationRequestFingerprint, nil
}

func (r *Repository) Get(ctx context.Context, householdID, subjectID string) (subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	sid, ok2 := parseUUID(subjectID)
	if !ok1 || !ok2 {
		return subject.Subject{}, subject.ErrNotFound
	}
	found, err := r.queries.GetSubject(ctx, dbgen.GetSubjectParams{HouseholdID: hid, ID: sid})
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, subject.ErrNotFound
	}
	if err != nil {
		return subject.Subject{}, errPersistence
	}
	return toSubject(row{ID: found.ID, HouseholdID: found.HouseholdID, Type: found.Type, Name: found.Name, Archived: found.Archived, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version, LinkedUserID: found.LinkedUserID}), nil
}

func (r *Repository) List(ctx context.Context, householdID string, archived bool, afterID string, limit int) ([]subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	after, ok2 := parseUUID(afterID)
	if !ok1 || !ok2 {
		return nil, subject.ErrNotFound
	}
	rows, err := r.queries.ListSubjects(ctx, dbgen.ListSubjectsParams{HouseholdID: hid, Archived: archived, AfterID: after, RowLimit: int32(limit)})
	if err != nil {
		return nil, errPersistence
	}
	items := make([]subject.Subject, 0, len(rows))
	for _, item := range rows {
		items = append(items, toSubject(row{ID: item.ID, HouseholdID: item.HouseholdID, Type: item.Type, Name: item.Name, Archived: item.Archived, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Version: item.Version, LinkedUserID: item.LinkedUserID}))
	}
	return items, nil
}

// Update applies the patch atomically when the stored version equals
// expectedVersion. When no row matches, a household-scoped lookup tells a
// missing subject from a stale version.
func (r *Repository) Update(ctx context.Context, householdID, subjectID string, expectedVersion int64, p subject.Patch) (subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	sid, ok2 := parseUUID(subjectID)
	if !ok1 || !ok2 {
		return subject.Subject{}, subject.ErrNotFound
	}
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
		linkedID, parseErr := nullableUUID(p.LinkedUserID)
		if parseErr != nil {
			return subject.Subject{}, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
		}
		params.LinkedUserID = linkedID
	}
	updated, err := r.queries.UpdateSubject(ctx, params)
	if err == nil {
		return toSubject(row{ID: updated.ID, HouseholdID: updated.HouseholdID, Type: updated.Type, Name: updated.Name, Archived: updated.Archived, CreatedAt: updated.CreatedAt, UpdatedAt: updated.UpdatedAt, Version: updated.Version, LinkedUserID: updated.LinkedUserID}), nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" && pgErr.ConstraintName == "subjects_linked_membership_fk" {
			return subject.Subject{}, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
		}
		if pgErr.Code == "23514" && pgErr.ConstraintName == "subjects_linked_user_person_only" {
			return subject.Subject{}, &subject.ValidationError{Field: "linkedUserId", Code: "requires_person"}
		}
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
