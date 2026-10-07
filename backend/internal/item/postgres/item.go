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

// Repository stores items through the runtime-role connection. Every query
// filters by household and item id, so cross-household access is impossible at
// the SQL level regardless of authorization. A composite foreign key ties each
// item to a subject of the same household.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
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
	Notes                             pgtype.Text
	AttentionOn                       pgtype.Date
	WorkflowState                     string
	Archived                          bool
	CreatedAt, UpdatedAt              pgtype.Timestamptz
	Version                           int64
}

func toItem(r row) (item.Item, error) {
	out := item.Item{ID: r.ID, HouseholdID: r.HouseholdID, SubjectID: r.SubjectID, Title: r.Title, WorkflowState: item.WorkflowState(r.WorkflowState), Archived: r.Archived, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(), Version: r.Version}
	if r.Notes.Valid {
		notes := r.Notes.String
		out.Notes = &notes
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

func isForeignKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (r *Repository) Create(ctx context.Context, householdID string, d item.Draft) (item.Item, error) {
	hid, ok1 := parseUUID(householdID)
	sid, ok2 := parseUUID(d.SubjectID)
	if !ok1 {
		return item.Item{}, item.ErrNotFound
	}
	if !ok2 {
		return item.Item{}, item.ErrInvalidReference
	}
	created, err := r.queries.CreateItem(ctx, dbgen.CreateItemParams{HouseholdID: hid, SubjectID: sid, Title: d.Title, Notes: text(d.Notes), AttentionOn: date(d.AttentionOn)})
	if isForeignKey(err) {
		// The subject is not in this household, or a household/subject was
		// deleted concurrently.
		return item.Item{}, item.ErrInvalidReference
	}
	if err != nil {
		return item.Item{}, errPersistence
	}
	return toItem(row(created))
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
	return toItem(row(found))
}

func (r *Repository) List(ctx context.Context, householdID string, archived bool, afterID string, limit int) ([]item.Item, error) {
	hid, ok1 := parseUUID(householdID)
	after, ok2 := parseUUID(afterID)
	if !ok1 || !ok2 {
		return nil, item.ErrNotFound
	}
	rows, err := r.queries.ListItems(ctx, dbgen.ListItemsParams{HouseholdID: hid, Archived: archived, AfterID: after, RowLimit: int32(limit)})
	if err != nil {
		return nil, errPersistence
	}
	items := make([]item.Item, 0, len(rows))
	for _, found := range rows {
		converted, err := toItem(row(found))
		if err != nil {
			return nil, err
		}
		items = append(items, converted)
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
	if c.Notes.Set {
		params.SetNotes = true
		params.Notes = text(c.Notes.Value)
	}
	if c.AttentionOn.Set {
		params.SetAttentionOn = true
		params.AttentionOn = date(c.AttentionOn.Value)
	}
	if c.WorkflowState != nil {
		params.WorkflowState = pgtype.Text{String: string(*c.WorkflowState), Valid: true}
	}
	if c.Archived != nil {
		params.Archived = pgtype.Bool{Bool: *c.Archived, Valid: true}
	}
	updated, err := r.queries.UpdateItem(ctx, params)
	if err == nil {
		return toItem(row(updated))
	}
	if isForeignKey(err) {
		return item.Item{}, item.ErrInvalidReference
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, errPersistence
	}
	if _, err = r.queries.GetItem(ctx, dbgen.GetItemParams{HouseholdID: hid, ID: iid}); errors.Is(err, pgx.ErrNoRows) {
		return item.Item{}, item.ErrNotFound
	} else if err != nil {
		return item.Item{}, errPersistence
	}
	return item.Item{}, item.ErrVersionMismatch
}
