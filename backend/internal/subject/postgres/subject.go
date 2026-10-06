package postgres

import (
	"context"
	"errors"

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

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, false
	}
	return u, true
}

type row struct {
	ID, HouseholdID, Type, Name string
	Archived                    bool
	CreatedAt, UpdatedAt        pgtype.Timestamptz
	Version                     int64
}

func toSubject(r row) subject.Subject {
	return subject.Subject{ID: r.ID, HouseholdID: r.HouseholdID, Type: subject.Type(r.Type), Name: r.Name, Archived: r.Archived, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(), Version: r.Version}
}

func (r *Repository) Create(ctx context.Context, householdID, name string, t subject.Type) (subject.Subject, error) {
	hid, ok := parseUUID(householdID)
	if !ok {
		return subject.Subject{}, subject.ErrNotFound
	}
	created, err := r.queries.CreateSubject(ctx, dbgen.CreateSubjectParams{HouseholdID: hid, Type: string(t), Name: name})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		// The household was deleted concurrently.
		return subject.Subject{}, subject.ErrNotFound
	}
	if err != nil {
		return subject.Subject{}, errPersistence
	}
	return toSubject(row(created)), nil
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
	return toSubject(row(found)), nil
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
		items = append(items, toSubject(row(item)))
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
	params := dbgen.UpdateSubjectParams{HouseholdID: hid, ID: sid, ExpectedVersion: expectedVersion}
	if p.Name != nil {
		params.Name = pgtype.Text{String: *p.Name, Valid: true}
	}
	if p.Type != nil {
		params.Type = pgtype.Text{String: string(*p.Type), Valid: true}
	}
	if p.Archived != nil {
		params.Archived = pgtype.Bool{Bool: *p.Archived, Valid: true}
	}
	updated, err := r.queries.UpdateSubject(ctx, params)
	if err == nil {
		return toSubject(row(updated)), nil
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
