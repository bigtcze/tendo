// Package item holds the household-scoped backlog item domain and application
// service for one-off items. It does not import other modules or adapters;
// household membership/timezone and subject lookups are injected through
// Households and Subjects.
package item

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

var (
	// ErrNotFound covers malformed identifiers, nonexistent items or
	// households, other households' items, and non-members. The cases are
	// deliberately indistinguishable.
	ErrNotFound = errors.New("item not found")
	// ErrVersionMismatch means the item exists but the expected version is stale.
	ErrVersionMismatch = errors.New("item version mismatch")
	// ErrUnavailable is an infrastructure failure without internal detail.
	ErrUnavailable = errors.New("item unavailable")
	// ErrEmptyPatch means an update named no field. It is a malformed request,
	// not a field validation failure.
	ErrEmptyPatch = errors.New("empty patch")
	// ErrInvalidReference is returned by repositories when the subject reference
	// is rejected by the storage layer (for example a concurrent race with a
	// subject or household change). The service reports it as a subjectId
	// validation failure.
	ErrInvalidReference = errors.New("invalid subject reference")
)

const (
	DefaultLimit = 50
	MaxLimit     = 100

	MaxTitleRunes = 200
	MaxNotesRunes = 4000

	operationTimeout = 5 * time.Second
	cursorPrefix     = "i1:"
	// nilUUID sorts before every generated identifier; it starts the first page.
	nilUUID = "00000000-0000-0000-0000-000000000000"
)

// WorkflowState is the manually selected lifecycle state of an item.
type WorkflowState string

const (
	StateOpen       WorkflowState = "open"
	StateInProgress WorkflowState = "in_progress"
	StateWaiting    WorkflowState = "waiting"
	StatePaused     WorkflowState = "paused"
)

// ParseWorkflowState accepts only the exact lowercase wire values.
func ParseWorkflowState(s string) (WorkflowState, bool) {
	switch v := WorkflowState(s); v {
	case StateOpen, StateInProgress, StateWaiting, StatePaused:
		return v, true
	}
	return "", false
}

// Item is a household-scoped backlog item. HouseholdID and Version are
// populated by repositories but are not part of the public representation.
// Attention is derived by the Service and is never stored.
type Item struct {
	ID            string
	HouseholdID   string
	SubjectID     string
	Title         string
	Notes         *string
	AttentionOn   *schedule.Date
	WorkflowState WorkflowState
	Attention     schedule.Attention
	Archived      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int64
}

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

// InvalidQueryError names the offending list query parameter.
type InvalidQueryError struct{ Parameter string }

func (e *InvalidQueryError) Error() string { return "invalid query parameter " + e.Parameter }

// Nullable is an optional value that distinguishes "not provided" (Set false)
// from an explicit null (Set true, Value nil).
type Nullable[T any] struct {
	Set   bool
	Value *T
}

// Some returns a provided non-null value.
func Some[T any](v T) Nullable[T] { return Nullable[T]{Set: true, Value: &v} }

// Null returns a provided explicit null.
func Null[T any]() Nullable[T] { return Nullable[T]{Set: true} }

// NewItem is the unvalidated create input.
type NewItem struct {
	SubjectID   string
	Title       string
	Notes       *string
	AttentionOn *string
}

// Patch is the unvalidated partial update input. Unset fields are unchanged;
// Notes and AttentionOn may be set to null to clear them.
type Patch struct {
	Title         *string
	SubjectID     *string
	Notes         Nullable[string]
	AttentionOn   Nullable[string]
	WorkflowState *string
	Archived      *bool
}

func (p Patch) empty() bool {
	return p.Title == nil && p.SubjectID == nil && !p.Notes.Set && !p.AttentionOn.Set && p.WorkflowState == nil && p.Archived == nil
}

// Draft is a validated new item handed to the repository.
type Draft struct {
	SubjectID   string
	Title       string
	Notes       *string
	AttentionOn *schedule.Date
}

// Change is a validated patch handed to the repository.
type Change struct {
	Title         *string
	SubjectID     *string
	Notes         Nullable[string]
	AttentionOn   Nullable[schedule.Date]
	WorkflowState *WorkflowState
	Archived      *bool
}

type ListQuery struct {
	Archived bool
	// Limit 0 selects DefaultLimit.
	Limit  int
	Cursor string
}

type Page struct {
	Items      []Item
	NextCursor *string
}

// Repository persists items. Every method is scoped by household; Get and
// Update return ErrNotFound for rows outside that household. Update returns
// ErrVersionMismatch when the item exists with another version. Create and
// Update return ErrInvalidReference when the subject reference is rejected by
// storage. List returns items with id greater than afterID in ascending id
// order, at most limit. Repositories never populate Item.Attention. Any other
// error is an infrastructure failure.
type Repository interface {
	Create(ctx context.Context, householdID string, d Draft) (Item, error)
	Get(ctx context.Context, householdID, itemID string) (Item, error)
	List(ctx context.Context, householdID string, archived bool, afterID string, limit int) ([]Item, error)
	Update(ctx context.Context, householdID, itemID string, expectedVersion int64, c Change) (Item, error)
}

// Households authorizes userID in householdID and returns the household IANA
// timezone. It returns ErrNotFound for a non-member or nonexistent household;
// other errors are infrastructure failures.
type Households func(ctx context.Context, userID, householdID string) (timezone string, err error)

// Subjects reports whether a subject of the household is archived. It returns
// ErrNotFound when the subject is absent in that household; other errors are
// infrastructure failures.
type Subjects func(ctx context.Context, userID, householdID, subjectID string) (archived bool, err error)

type Service struct {
	repository Repository
	households Households
	subjects   Subjects
	now        func() time.Time
}

func NewService(repository Repository, households Households, subjects Subjects, now func() time.Time) *Service {
	return &Service{repository: repository, households: households, subjects: subjects, now: now}
}

// check validates the household identifier, canonicalizes it to lowercase, and
// authorizes the caller. It returns the canonical identifier and today's date in
// the household timezone.
func (s *Service) check(ctx context.Context, userID, householdID string) (string, schedule.Date, error) {
	if !validUUID(householdID) {
		return "", schedule.Date{}, ErrNotFound
	}
	householdID = strings.ToLower(householdID)
	zone, err := s.households(ctx, userID, householdID)
	if errors.Is(err, ErrNotFound) {
		return "", schedule.Date{}, ErrNotFound
	}
	if err != nil {
		return "", schedule.Date{}, ErrUnavailable
	}
	today, err := schedule.BusinessDate(s.now(), zone)
	if err != nil {
		return "", schedule.Date{}, ErrUnavailable
	}
	return householdID, today, nil
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrVersionMismatch):
		return err
	case errors.Is(err, ErrInvalidReference):
		return &ValidationError{"subjectId", "invalid_reference"}
	default:
		return ErrUnavailable
	}
}

// derive fills the derived attention state.
func derive(i Item, today schedule.Date) (Item, error) {
	a, err := schedule.DeriveAttention(i.AttentionOn, today)
	if err != nil {
		return Item{}, ErrUnavailable
	}
	i.Attention = a
	return i, nil
}

// checkSubject requires subjectID to name an active subject of the household.
func (s *Service) checkSubject(ctx context.Context, userID, householdID, subjectID string) (string, error) {
	invalid := &ValidationError{"subjectId", "invalid_reference"}
	if !validUUID(subjectID) {
		return "", invalid
	}
	subjectID = strings.ToLower(subjectID)
	archived, err := s.subjects(ctx, userID, householdID, subjectID)
	if errors.Is(err, ErrNotFound) {
		return "", invalid
	}
	if err != nil {
		return "", ErrUnavailable
	}
	if archived {
		return "", invalid
	}
	return subjectID, nil
}

func (s *Service) Create(ctx context.Context, userID, householdID string, n NewItem) (Item, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	title, err := ValidateTitle(n.Title)
	if err != nil {
		return Item{}, err
	}
	if n.Notes != nil {
		if err = ValidateNotes(*n.Notes); err != nil {
			return Item{}, err
		}
	}
	var attentionOn *schedule.Date
	if n.AttentionOn != nil {
		d, err := ParseDate(*n.AttentionOn)
		if err != nil {
			return Item{}, err
		}
		attentionOn = &d
	}
	subjectID, err := s.checkSubject(ctx, userID, householdID, n.SubjectID)
	if err != nil {
		return Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	created, err := s.repository.Create(ctx, householdID, Draft{SubjectID: subjectID, Title: title, Notes: n.Notes, AttentionOn: attentionOn})
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return derive(created, today)
}

func (s *Service) Get(ctx context.Context, userID, householdID, itemID string) (Item, error) {
	if !validUUID(itemID) {
		return Item{}, ErrNotFound
	}
	itemID = strings.ToLower(itemID)
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	found, err := s.repository.Get(ctx, householdID, itemID)
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return derive(found, today)
}

func (s *Service) List(ctx context.Context, userID, householdID string, q ListQuery) (Page, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Page{}, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return Page{}, &InvalidQueryError{"limit"}
	}
	after := nilUUID
	if q.Cursor != "" {
		id, err := DecodeCursor(q.Cursor)
		if err != nil {
			return Page{}, &InvalidQueryError{"cursor"}
		}
		after = id
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	rows, err := s.repository.List(ctx, householdID, q.Archived, after, limit+1)
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		next := EncodeCursor(rows[limit-1].ID)
		page.NextCursor = &next
	}
	out := make([]Item, 0, len(page.Items))
	for _, row := range page.Items {
		d, err := derive(row, today)
		if err != nil {
			return Page{}, err
		}
		out = append(out, d)
	}
	page.Items = out
	return page, nil
}

func (s *Service) Update(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, p Patch) (Item, error) {
	if !validUUID(itemID) {
		return Item{}, ErrNotFound
	}
	itemID = strings.ToLower(itemID)
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	if p.empty() {
		return Item{}, ErrEmptyPatch
	}
	var c Change
	if p.Title != nil {
		title, err := ValidateTitle(*p.Title)
		if err != nil {
			return Item{}, err
		}
		c.Title = &title
	}
	if p.Notes.Set {
		if p.Notes.Value != nil {
			if err := ValidateNotes(*p.Notes.Value); err != nil {
				return Item{}, err
			}
		}
		c.Notes = p.Notes
	}
	if p.AttentionOn.Set {
		c.AttentionOn.Set = true
		if p.AttentionOn.Value != nil {
			d, err := ParseDate(*p.AttentionOn.Value)
			if err != nil {
				return Item{}, err
			}
			c.AttentionOn.Value = &d
		}
	}
	if p.WorkflowState != nil {
		state, ok := ParseWorkflowState(*p.WorkflowState)
		if !ok {
			return Item{}, &ValidationError{"workflowState", "invalid_workflow_state"}
		}
		c.WorkflowState = &state
	}
	c.Archived = p.Archived
	if p.SubjectID != nil {
		subjectID, err := s.checkSubject(ctx, userID, householdID, *p.SubjectID)
		if err != nil {
			return Item{}, err
		}
		c.SubjectID = &subjectID
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	updated, err := s.repository.Update(ctx, householdID, itemID, expectedVersion, c)
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return derive(updated, today)
}

func forbiddenRune(r rune) bool {
	// Control, format (zero-width, bidi), and line or paragraph separator
	// characters can hide or reorder text.
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// ValidateTitle returns the trimmed title or a ValidationError.
func ValidateTitle(title string) (string, error) {
	if !utf8.ValidString(title) {
		return "", &ValidationError{"title", "invalid_characters"}
	}
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > MaxTitleRunes {
		return "", &ValidationError{"title", "invalid_length"}
	}
	for _, r := range title {
		if forbiddenRune(r) {
			return "", &ValidationError{"title", "invalid_characters"}
		}
	}
	return title, nil
}

// ValidateNotes checks notes without altering them. Newline, carriage return,
// and tab are allowed; other control, format, and separator characters are not.
// The empty string is invalid; clients send null to clear notes.
func ValidateNotes(notes string) error {
	if !utf8.ValidString(notes) {
		return &ValidationError{"notes", "invalid_characters"}
	}
	if n := utf8.RuneCountInString(notes); n < 1 || n > MaxNotesRunes {
		return &ValidationError{"notes", "invalid_length"}
	}
	for _, r := range notes {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if forbiddenRune(r) {
			return &ValidationError{"notes", "invalid_characters"}
		}
	}
	return nil
}

// ParseDate accepts only a real calendar date written exactly YYYY-MM-DD.
func ParseDate(s string) (schedule.Date, error) {
	invalid := &ValidationError{"attentionOn", "invalid_date"}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return schedule.Date{}, invalid
	}
	num := func(part string) (int, bool) {
		n := 0
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return 0, false
			}
			n = n*10 + int(part[i]-'0')
		}
		return n, true
	}
	y, ok1 := num(s[0:4])
	m, ok2 := num(s[5:7])
	d, ok3 := num(s[8:10])
	if !ok1 || !ok2 || !ok3 {
		return schedule.Date{}, invalid
	}
	date, err := schedule.NewDate(y, time.Month(m), d)
	if err != nil {
		return schedule.Date{}, invalid
	}
	return date, nil
}

// EncodeCursor returns the opaque, versioned cursor for the last returned id.
func EncodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strings.ToLower(id)))
}

// DecodeCursor returns the canonical lowercase id inside a cursor.
func DecodeCursor(cursor string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", errors.New("malformed cursor")
	}
	id, ok := strings.CutPrefix(string(raw), cursorPrefix)
	if !ok || !validUUID(id) {
		return "", errors.New("malformed cursor")
	}
	return strings.ToLower(id), nil
}

// validUUID accepts only the canonical 8-4-4-4-12 hexadecimal form.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
