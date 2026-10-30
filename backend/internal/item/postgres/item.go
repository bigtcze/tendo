package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type TxDB interface {
	dbgen.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// Repository stores items through the runtime-role connection. Every query
// filters by household and item id, so cross-household access is impossible at
// the SQL level regardless of authorization.
type Repository struct {
	queries *dbgen.Queries
	db      TxDB
}

func NewRepository(db TxDB) *Repository {
	return &Repository{queries: dbgen.New(db), db: db}
}

var errPersistence = errors.New("item persistence failed")

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, false
	}
	return u, true
}

func nullableUUID(s *string) (pgtype.UUID, error) {
	if s == nil {
		return pgtype.UUID{}, nil
	}
	u, ok := parseUUID(*s)
	if !ok {
		return pgtype.UUID{}, item.ErrInvalidResponsibleReference
	}
	return u, nil
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	v := uuidString(u)
	return &v
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func date(d *schedule.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: timeOf(*d), Valid: true}
}

func classifyForeignKey(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return errPersistence
	}
	switch pgErr.ConstraintName {
	case "items_responsible_membership_fk":
		return item.ErrInvalidResponsibleReference
	case "items_household_id_subject_id_fkey":
		return item.ErrInvalidReference
	case "items_household_id_fkey":
		return item.ErrNotFound
	default:
		return errPersistence
	}
}

func (r *Repository) CreateIdempotent(ctx context.Context, householdID string, d item.Draft) (item.Item, bool, error) {
	hid, ok1 := parseUUID(householdID)
	sid, ok2 := parseUUID(d.SubjectID)
	if !ok1 {
		return item.Item{}, false, item.ErrNotFound
	}
	if !ok2 {
		return item.Item{}, false, item.ErrInvalidReference
	}
	responsibleID, err := nullableUUID(d.ResponsibleUserID)
	if err != nil {
		return item.Item{}, false, err
	}
	actor, ok := parseUUID(d.ActorID)
	if !ok {
		return item.Item{}, false, item.ErrNotFound
	}
	params := createParams(hid, sid, responsibleID, actor, d)
	if d.InitializationReceipt != nil {
		return r.createInitialized(ctx, hid, d, params)
	}
	created, err := r.queries.CreateItem(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return item.Item{}, false, nil
		}
		return item.Item{}, false, classifyForeignKey(err)
	}
	converted, convErr := toItem(createdRow(created))
	return converted, true, convErr
}

func (r *Repository) createInitialized(ctx context.Context, hid pgtype.UUID, d item.Draft, params dbgen.CreateItemParams) (item.Item, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return item.Item{}, false, errPersistence
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	created, err := q.InsertInitializedItem(ctx, initializedItemParams(params))
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, false, nil
	}
	if err != nil {
		return item.Item{}, false, classifyForeignKey(err)
	}
	receipt := *d.InitializationReceipt
	stored, err := q.InsertInitializationCompletion(ctx, initializationCompletionParams(hid, created.ID, receipt))
	if err != nil {
		return item.Item{}, false, errPersistence
	}
	if err = q.UpdateItemForCompletion(ctx, dbgen.UpdateItemForCompletionParams{
		HouseholdID: hid, ID: uuid(created.ID), AttentionOn: date(receipt.NextAttentionOn),
		WorkflowState: string(item.StateOpen), Done: false,
	}); err != nil {
		return item.Item{}, false, errPersistence
	}
	if _, err = completionFromInitializationRow(stored); err != nil {
		return item.Item{}, false, errPersistence
	}
	final, err := q.GetItem(ctx, dbgen.GetItemParams{HouseholdID: hid, ID: uuid(created.ID)})
	if err != nil {
		return item.Item{}, false, errPersistence
	}
	if err = tx.Commit(ctx); err != nil {
		return item.Item{}, false, errPersistence
	}
	converted, convErr := toItem(getRow(final))
	return converted, true, convErr
}

func (r *Repository) FindCreation(ctx context.Context, householdID, actorID, key string) (item.Item, []byte, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return item.Item{}, nil, item.ErrNotFound
	}
	found, err := r.queries.FindItemCreation(ctx, dbgen.FindItemCreationParams{
		HouseholdID: hid, CreatedByUserID: actor, CreationKey: pgtype.Text{String: key, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, nil, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	converted, err := toItem(findCreationRow(found))
	return converted, found.CreationRequestFingerprint, err
}

func (r *Repository) Get(ctx context.Context, householdID, itemID string) (item.Item, error) {
	hid, ok1 := parseUUID(householdID)
	iid, ok2 := parseUUID(itemID)
	if !ok1 || !ok2 {
		return item.Item{}, item.ErrNotFound
	}
	found, err := r.queries.GetItem(ctx, dbgen.GetItemParams{HouseholdID: hid, ID: iid})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, errPersistence
	}
	return toItem(getRow(found))
}

func createParams(householdID, subjectID, responsibleID, actor pgtype.UUID, d item.Draft) dbgen.CreateItemParams {
	params := dbgen.CreateItemParams{HouseholdID: householdID, SubjectID: subjectID, ResponsibleUserID: responsibleID,
		Title: d.Title, Notes: text(d.Notes), AttentionOn: date(d.AttentionOn), CreatedByUserID: actor,
		CreationKey: pgtype.Text{String: d.CreationKey, Valid: true}, CreationRequestFingerprint: d.CreationFingerprint}
	if d.Recurrence != nil {
		params.RecurrenceIntervalValue = pgtype.Int4{Int32: int32(d.Recurrence.Interval.Value), Valid: true}
		params.RecurrenceIntervalUnit = pgtype.Text{String: string(d.Recurrence.Interval.Unit), Valid: true}
		params.RecurrenceMode = pgtype.Text{String: string(d.Recurrence.Mode), Valid: true}
	}
	return params
}

func initializedItemParams(params dbgen.CreateItemParams) dbgen.InsertInitializedItemParams {
	return dbgen.InsertInitializedItemParams{
		HouseholdID: params.HouseholdID, SubjectID: params.SubjectID, ResponsibleUserID: params.ResponsibleUserID,
		Title: params.Title, Notes: params.Notes, RecurrenceIntervalValue: params.RecurrenceIntervalValue,
		RecurrenceIntervalUnit: params.RecurrenceIntervalUnit, RecurrenceMode: params.RecurrenceMode,
		CreatedByUserID: params.CreatedByUserID, CreationKey: params.CreationKey,
		CreationRequestFingerprint: params.CreationRequestFingerprint,
	}
}

func initializationCompletionParams(hid pgtype.UUID, id string, receipt item.Completion) dbgen.InsertInitializationCompletionParams {
	return dbgen.InsertInitializationCompletionParams{
		HouseholdID: hid, ItemID: uuid(id), CompletedOn: date(&receipt.CompletedOn),
		CompletedByUserID:       uuid(receipt.CompletedByUserID),
		RecurrenceIntervalValue: pgtype.Int4{Int32: int32(receipt.Recurrence.Interval.Value), Valid: true},
		RecurrenceIntervalUnit:  pgtype.Text{String: string(receipt.Recurrence.Interval.Unit), Valid: true},
		RecurrenceMode:          pgtype.Text{String: string(receipt.Recurrence.Mode), Valid: true},
		NextAttentionOn:         date(receipt.NextAttentionOn), RequestFingerprint: receipt.Fingerprint[:],
	}
}
