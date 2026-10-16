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
	out := item.Item{ID: r.ID, HouseholdID: r.HouseholdID, SubjectID: r.SubjectID, ResponsibleUserID: uuidPtr(r.ResponsibleUserID), Title: r.Title, WorkflowState: item.WorkflowState(r.WorkflowState), Archived: r.Archived, Done: r.Done, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(), Version: r.Version}
	if r.Notes.Valid {
		notes := r.Notes.String
		out.Notes = &notes
	}
	recurrenceParts := 0
	if r.RecurrenceIntervalValue.Valid {
		recurrenceParts++
	}
	if r.RecurrenceIntervalUnit.Valid {
		recurrenceParts++
	}
	if r.RecurrenceMode.Valid {
		recurrenceParts++
	}
	if recurrenceParts != 0 && recurrenceParts != 3 {
		return item.Item{}, errPersistence
	}
	if recurrenceParts == 3 {
		if r.RecurrenceIntervalValue.Int32 < 1 || r.RecurrenceIntervalValue.Int32 > 999 {
			return item.Item{}, errPersistence
		}
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: int(r.RecurrenceIntervalValue.Int32), Unit: schedule.Unit(r.RecurrenceIntervalUnit.String)}, Mode: schedule.Mode(r.RecurrenceMode.String)}
		if policy.Validate() != nil {
			return item.Item{}, errPersistence
		}
		out.Recurrence = policy
	}
	if r.LastCompletedOn.Valid {
		d, err := schedule.NewDate(r.LastCompletedOn.Time.Year(), r.LastCompletedOn.Time.Month(), r.LastCompletedOn.Time.Day())
		if err != nil {
			return item.Item{}, errPersistence
		}
		out.LastCompletedOn = &d
	}
	if r.AttentionOn.Valid {
		if r.AttentionOn.InfinityModifier != pgtype.Finite {
			return item.Item{}, errPersistence
		}
		d, err := schedule.NewDate(r.AttentionOn.Time.Year(), r.AttentionOn.Time.Month(), r.AttentionOn.Time.Day())
		if err != nil {
			return item.Item{}, errPersistence
		}
		out.AttentionOn = &d
	}
	return out, nil
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
	params := dbgen.CreateItemParams{HouseholdID: hid, SubjectID: sid, ResponsibleUserID: responsibleID, Title: d.Title, Notes: text(d.Notes), AttentionOn: date(d.AttentionOn), CreatedByUserID: actor, CreationKey: pgtype.Text{String: d.CreationKey, Valid: true}, CreationRequestFingerprint: d.CreationFingerprint}
	if d.Recurrence != nil {
		params.RecurrenceIntervalValue = pgtype.Int4{Int32: int32(d.Recurrence.Interval.Value), Valid: true}
		params.RecurrenceIntervalUnit = pgtype.Text{String: string(d.Recurrence.Interval.Unit), Valid: true}
		params.RecurrenceMode = pgtype.Text{String: string(d.Recurrence.Mode), Valid: true}
	}
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
	converted, convErr := toItem(row{ID: created.ID, HouseholdID: created.HouseholdID, SubjectID: created.SubjectID, ResponsibleUserID: created.ResponsibleUserID, Title: created.Title, Notes: created.Notes, AttentionOn: created.AttentionOn, RecurrenceIntervalValue: created.RecurrenceIntervalValue, RecurrenceIntervalUnit: created.RecurrenceIntervalUnit, RecurrenceMode: created.RecurrenceMode, WorkflowState: created.WorkflowState, Archived: created.Archived, Done: created.Done, LastCompletedOn: created.LastCompletedOn, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt, Version: created.Version})
	return converted, true, convErr
}

func (r *Repository) createInitialized(ctx context.Context, hid pgtype.UUID, d item.Draft, params dbgen.CreateItemParams) (item.Item, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return item.Item{}, false, errPersistence
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	created, err := q.InsertInitializedItem(ctx, dbgen.InsertInitializedItemParams{HouseholdID: params.HouseholdID, SubjectID: params.SubjectID, ResponsibleUserID: params.ResponsibleUserID, Title: params.Title, Notes: params.Notes, RecurrenceIntervalValue: params.RecurrenceIntervalValue, RecurrenceIntervalUnit: params.RecurrenceIntervalUnit, RecurrenceMode: params.RecurrenceMode, CreatedByUserID: params.CreatedByUserID, CreationKey: params.CreationKey, CreationRequestFingerprint: params.CreationRequestFingerprint})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, false, nil
	}
	if err != nil {
		return item.Item{}, false, classifyForeignKey(err)
	}
	receipt := *d.InitializationReceipt
	stored, err := q.InsertInitializationCompletion(ctx, dbgen.InsertInitializationCompletionParams{HouseholdID: hid, ItemID: uuid(created.ID), CompletedOn: date(&receipt.CompletedOn), CompletedByUserID: uuid(receipt.CompletedByUserID), RecurrenceIntervalValue: pgtype.Int4{Int32: int32(receipt.Recurrence.Interval.Value), Valid: true}, RecurrenceIntervalUnit: pgtype.Text{String: string(receipt.Recurrence.Interval.Unit), Valid: true}, RecurrenceMode: pgtype.Text{String: string(receipt.Recurrence.Mode), Valid: true}, NextAttentionOn: date(receipt.NextAttentionOn), RequestFingerprint: receipt.Fingerprint[:]})
	if err != nil {
		return item.Item{}, false, errPersistence
	}
	if completeAfterInsertHook != nil {
		if err = completeAfterInsertHook(); err != nil {
			return item.Item{}, false, errPersistence
		}
	}
	if err = q.UpdateItemForCompletion(ctx, dbgen.UpdateItemForCompletionParams{HouseholdID: hid, ID: uuid(created.ID), AttentionOn: date(receipt.NextAttentionOn), WorkflowState: string(item.StateOpen), Done: false}); err != nil {
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
	converted, convErr := toItem(row{ID: final.ID, HouseholdID: final.HouseholdID, SubjectID: final.SubjectID, ResponsibleUserID: final.ResponsibleUserID, Title: final.Title, Notes: final.Notes, AttentionOn: final.AttentionOn, RecurrenceIntervalValue: final.RecurrenceIntervalValue, RecurrenceIntervalUnit: final.RecurrenceIntervalUnit, RecurrenceMode: final.RecurrenceMode, WorkflowState: final.WorkflowState, Archived: final.Archived, Done: final.Done, LastCompletedOn: final.LastCompletedOn, CreatedAt: final.CreatedAt, UpdatedAt: final.UpdatedAt, Version: final.Version})
	return converted, true, convErr
}

func (r *Repository) FindCreation(ctx context.Context, householdID, actorID, key string) (item.Item, []byte, error) {
	hid, ok1 := parseUUID(householdID)
	actor, ok2 := parseUUID(actorID)
	if !ok1 || !ok2 {
		return item.Item{}, nil, item.ErrNotFound
	}
	found, err := r.queries.FindItemCreation(ctx, dbgen.FindItemCreationParams{HouseholdID: hid, CreatedByUserID: actor, CreationKey: pgtype.Text{String: key, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, nil, item.ErrNotFound
	}
	if err != nil {
		return item.Item{}, nil, errPersistence
	}
	converted, err := toItem(row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID, Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue, RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState, Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version})
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
	return toItem(row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID, Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue, RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState, Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version})
}

func (r *Repository) List(ctx context.Context, householdID string, opts item.ListOptions) ([]item.Item, error) {
	if opts.Sort == "" {
		opts.Sort = item.SortCreated
	}
	hid, ok1 := parseUUID(householdID)
	after, ok2 := parseUUID(opts.AfterID)
	if !ok1 || !ok2 {
		return nil, item.ErrNotFound
	}
	f := opts.Filters
	var subjectID pgtype.UUID
	if f.SubjectID != "" {
		var ok bool
		subjectID, ok = parseUUID(f.SubjectID)
		if !ok {
			return nil, item.ErrNotFound
		}
	}
	var responsible pgtype.UUID
	if f.ResponsibleUserID.Value != nil {
		var ok bool
		responsible, ok = parseUUID(*f.ResponsibleUserID.Value)
		if !ok {
			return nil, item.ErrNotFound
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
	search := pgtype.Text{}
	if f.Q != "" {
		search = pgtype.Text{String: f.Q, Valid: true}
	}
	var recurring pgtype.Bool
	if f.Recurring != nil {
		recurring = pgtype.Bool{Bool: *f.Recurring, Valid: true}
	}
	params := dbgen.ListItemsParams{HouseholdID: hid, Archived: f.Archived, Done: f.Done, AfterID: after, SubjectID: subjectID, FilterResponsible: f.ResponsibleUserID.Set, ResponsibleUserID: responsible, WorkflowState: workflow, Attention: attention, Today: today, Recurring: recurring, Q: search, RowLimit: int32(opts.Limit)}
	items := make([]item.Item, 0, opts.Limit)
	switch opts.Sort {
	case item.SortCreated:
		rows, err := r.queries.ListItems(ctx, params)
		if err != nil {
			return nil, errPersistence
		}
		for _, found := range rows {
			converted, convErr := toItem(row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID, Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue, RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState, Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version})
			if convErr != nil {
				return nil, convErr
			}
			items = append(items, converted)
		}
	case item.SortAttention:
		rows, err := r.queries.ListItemsByAttention(ctx, dbgen.ListItemsByAttentionParams{HouseholdID: params.HouseholdID, Archived: params.Archived, Done: params.Done, AfterID: params.AfterID, AfterAttentionOn: date(opts.AfterAttentionOn), SubjectID: params.SubjectID, FilterResponsible: params.FilterResponsible, ResponsibleUserID: params.ResponsibleUserID, WorkflowState: params.WorkflowState, Attention: params.Attention, Today: params.Today, Recurring: params.Recurring, Q: params.Q, RowLimit: params.RowLimit})
		if err != nil {
			return nil, errPersistence
		}
		for _, found := range rows {
			converted, convErr := toItem(row{ID: found.ID, HouseholdID: found.HouseholdID, SubjectID: found.SubjectID, ResponsibleUserID: found.ResponsibleUserID, Title: found.Title, Notes: found.Notes, AttentionOn: found.AttentionOn, RecurrenceIntervalValue: found.RecurrenceIntervalValue, RecurrenceIntervalUnit: found.RecurrenceIntervalUnit, RecurrenceMode: found.RecurrenceMode, WorkflowState: found.WorkflowState, Archived: found.Archived, Done: found.Done, LastCompletedOn: found.LastCompletedOn, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt, Version: found.Version})
			if convErr != nil {
				return nil, convErr
			}
			items = append(items, converted)
		}
	default:
		return nil, item.ErrUnavailable
	}
	return items, nil
}

// Update applies the change atomically when the stored version equals
// expectedVersion. When no row matches, a household-scoped lookup tells a
// missing item from a stale version.
func (r *Repository) Update(ctx context.Context, householdID, itemID string, expectedVersion int64, c item.Change) (item.Item, error) {
	hid, ok1 := parseUUID(householdID)
	iid, ok2 := parseUUID(itemID)
	if !ok1 || !ok2 {
		return item.Item{}, item.ErrNotFound
	}
	params := dbgen.UpdateItemParams{HouseholdID: hid, ID: iid, ExpectedVersion: expectedVersion, Title: text(c.Title)}
	if c.SubjectID != nil {
		sid, ok := parseUUID(*c.SubjectID)
		if !ok {
			return item.Item{}, item.ErrInvalidReference
		}
		params.SubjectID = sid
	}
	if c.ResponsibleUserID.Set {
		responsibleID, err := nullableUUID(c.ResponsibleUserID.Value)
		if err != nil {
			return item.Item{}, err
		}
		params.SetResponsibleUserID = true
		params.ResponsibleUserID = responsibleID
	}
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
	updated, err := r.queries.UpdateItem(ctx, params)
	if err == nil {
		return toItem(row{ID: updated.ID, HouseholdID: updated.HouseholdID, SubjectID: updated.SubjectID, ResponsibleUserID: updated.ResponsibleUserID, Title: updated.Title, Notes: updated.Notes, AttentionOn: updated.AttentionOn, RecurrenceIntervalValue: updated.RecurrenceIntervalValue, RecurrenceIntervalUnit: updated.RecurrenceIntervalUnit, RecurrenceMode: updated.RecurrenceMode, WorkflowState: updated.WorkflowState, Archived: updated.Archived, Done: updated.Done, LastCompletedOn: updated.LastCompletedOn, CreatedAt: updated.CreatedAt, UpdatedAt: updated.UpdatedAt, Version: updated.Version})
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, classifyForeignKey(err)
	}
	if _, err = r.queries.GetItem(ctx, dbgen.GetItemParams{HouseholdID: hid, ID: iid}); errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.ErrNotFound
	} else if err != nil {
		return item.Item{}, errPersistence
	}
	return item.Item{}, item.ErrVersionMismatch
}
