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

func classifyConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch {
	case pgErr.Code == "23503" && pgErr.ConstraintName == "subjects_linked_membership_fk":
		return &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
	case pgErr.Code == "23514" && pgErr.ConstraintName == "subjects_linked_user_person_only":
		return &subject.ValidationError{Field: "linkedUserId", Code: "requires_person"}
	case pgErr.Code == "23503":
		return subject.ErrNotFound
	default:
		return nil
	}
}

func updateConstraintError(err error) error {
	mapped := classifyConstraintError(err)
	if errors.Is(mapped, subject.ErrNotFound) {
		return nil
	}
	return mapped
}

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

func (r *Repository) CreateIdempotent(
	ctx context.Context,
	householdID, actorID, key, name string,
	t subject.Type,
	linkedUserID *string,
	fingerprint []byte,
) (subject.Subject, bool, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return subject.Subject{}, false, subject.ErrNotFound
	}
	linkedID, err := nullableUUID(linkedUserID)
	if err != nil {
		return subject.Subject{}, false, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}
	}
	params := dbgen.CreateSubjectParams{
		HouseholdID: hid, Type: string(t), Name: name, LinkedUserID: linkedID,
		CreatedByUserID: actor, CreationKey: pgtype.Text{String: key, Valid: true},
		CreationRequestFingerprint: fingerprint,
	}
	created, err := r.queries.CreateSubject(ctx, params)
	if mapped := classifyConstraintError(err); mapped != nil {
		return subject.Subject{}, false, mapped
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, false, nil
	}
	if err != nil {
		return subject.Subject{}, false, errPersistence
	}
	return toSubject(rowFromCreated(created)), true, nil
}

func (r *Repository) FindCreation(ctx context.Context, householdID, actorID, key string) (subject.Subject, []byte, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	params := dbgen.FindSubjectCreationParams{
		HouseholdID: hid, CreatedByUserID: actor,
		CreationKey: pgtype.Text{String: key, Valid: true},
	}
	found, err := r.queries.FindSubjectCreation(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, nil, subject.ErrNotFound
	}
	if err != nil {
		return subject.Subject{}, nil, errPersistence
	}
	return toSubject(rowFromFound(found)), found.CreationRequestFingerprint, nil
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
	return toSubject(rowFromGet(found)), nil
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
		items = append(items, toSubject(rowFromItem(item)))
	}
	return items, nil
}

func (r *Repository) ListByIDs(ctx context.Context, householdID string, archived bool, ids []string, afterID string, limit int) ([]subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	after, ok2 := parseUUID(afterID)
	if !ok1 || !ok2 {
		return nil, subject.ErrNotFound
	}
	uids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		u, ok := parseUUID(id)
		if !ok {
			return nil, subject.ErrNotFound
		}
		uids = append(uids, u)
	}
	params := dbgen.ListSubjectsByIDsParams{
		HouseholdID: hid, Archived: archived, Ids: uids,
		AfterID: after, RowLimit: int32(limit),
	}
	rows, err := r.queries.ListSubjectsByIDs(ctx, params)
	if err != nil {
		return nil, errPersistence
	}
	items := make([]subject.Subject, 0, len(rows))
	for _, item := range rows {
		items = append(items, toSubject(rowFromFilteredItem(item)))
	}
	return items, nil
}

// Update applies the patch atomically when the stored version equals
// expectedVersion. When no row matches, a household-scoped lookup tells a
// missing subject from a stale version.
