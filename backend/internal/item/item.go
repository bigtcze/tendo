// Package item holds the household-scoped backlog item domain and application
// service for one-off items. It does not import other modules or adapters;
// household membership/timezone and subject lookups are injected through
// Households and Subjects.
package item

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	// ErrInvalidResponsibleReference means the responsible-member reference was
	// rejected by storage (for example, membership ended after validation).
	ErrInvalidResponsibleReference = errors.New("invalid responsible member reference")
)

const (
	DefaultLimit = 50
	MaxLimit     = 100

	MaxTitleRunes  = 200
	MaxNotesRunes  = 4000
	MaxSearchRunes = 200

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
	ID                string
	HouseholdID       string
	SubjectID         string
	ResponsibleUserID *string
	Title             string
	Notes             *string
	AttentionOn       *schedule.Date
	Recurrence        *schedule.Policy
	WorkflowState     WorkflowState
	Attention         schedule.Attention
	Archived          bool
	Done              bool
	LastCompletedOn   *schedule.Date
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Version           int64
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
	SubjectID             string
	ResponsibleUserID     *string
	Title                 string
	Notes                 *string
	AttentionOn           *string
	HistoricalCompletedOn *string
	Recurrence            *schedule.Policy
}

// Patch is the unvalidated partial update input. Unset fields are unchanged.
// Notes, AttentionOn, and ResponsibleUserID distinguish omitted from null to
// support explicit clearing; a non-null ResponsibleUserID assigns a member.
type Patch struct {
	Title             *string
	SubjectID         *string
	ResponsibleUserID Nullable[string]
	Notes             Nullable[string]
	AttentionOn       Nullable[string]
	Recurrence        Nullable[schedule.Policy]
	WorkflowState     *string
	Archived          *bool
}

func (p Patch) empty() bool {
	return p.Title == nil && p.SubjectID == nil && !p.ResponsibleUserID.Set && !p.Notes.Set && !p.AttentionOn.Set && !p.Recurrence.Set && p.WorkflowState == nil && p.Archived == nil
}

// Draft is a validated new item handed to the repository.
type Draft struct {
	ActorID               string
	CreationKey           string
	CreationFingerprint   []byte
	SubjectID             string
	ResponsibleUserID     *string
	Title                 string
	Notes                 *string
	AttentionOn           *schedule.Date
	Recurrence            *schedule.Policy
	InitializationReceipt *Completion
}

// Change is a validated patch handed to the repository.
type Change struct {
	Title             *string
	SubjectID         *string
	ResponsibleUserID Nullable[string]
	Notes             Nullable[string]
	AttentionOn       Nullable[schedule.Date]
	Recurrence        Nullable[schedule.Policy]
	WorkflowState     *WorkflowState
	Archived          *bool
}

type ListSort string

const (
	SortCreated   ListSort = "created"
	SortAttention ListSort = "attention_on"
)

type ListQuery struct {
	Sort              ListSort
	AttentionDate     string
	Archived          bool
	Done              bool
	SubjectID         string
	ResponsibleUserID string
	WorkflowState     string
	Attention         string
	Recurring         *bool
	Q                 string
	// Limit 0 selects DefaultLimit.
	Limit  int
	Cursor string
}

type CollectionFilters struct {
	SubjectID         string
	ResponsibleUserID Nullable[string]
	WorkflowState     WorkflowState
	Attention         schedule.Attention
	Recurring         *bool
	Q                 string
}

type ListFilters struct {
	Archived, Done    bool
	SubjectID         string
	ResponsibleUserID Nullable[string]
	WorkflowState     WorkflowState
	Attention         schedule.Attention
	Recurring         *bool
	Q                 string
}

type SummaryQuery struct {
	SubjectID, ResponsibleUserID, WorkflowState, Attention string
	Recurring                                              *bool
	AttentionDate                                          string
	Q                                                      string
}

type SummaryOptions struct {
	Filters CollectionFilters
	Today   schedule.Date
}

type SummaryCounts struct {
	Total, NeedsAttention, InProgress, Waiting, Upcoming, Paused, Done, Archived int64
}

type Summary struct {
	EvaluatedOn schedule.Date
	SummaryCounts
}

type ListOptions struct {
	Filters          ListFilters
	Today            schedule.Date
	Sort             ListSort
	AfterID          string
	AfterAttentionOn *schedule.Date
	Limit            int
}

type Page struct {
	Items      []Item
	NextCursor *string
}

// Repository persists items. Every method is scoped by household; Get and
// Update return ErrNotFound for rows outside that household. Update returns
// ErrVersionMismatch when the item exists with another version. Create and
// Update return ErrInvalidReference when the subject reference is rejected by
// storage, or ErrInvalidResponsibleReference when the responsible-member
// reference is rejected. List returns at most limit items after the keyset
// boundary in the selected sort: SortCreated orders by ascending id after
// AfterID; SortAttention orders null attentionOn first, then ascending
// attentionOn and id, after (AfterAttentionOn, AfterID) where a nil
// AfterAttentionOn denotes a null-date boundary. Repositories never populate
// Item.Attention.
// Any other error is an infrastructure failure.
type Repository interface {
	CreateIdempotent(ctx context.Context, householdID string, d Draft) (Item, bool, error)
	FindCreation(ctx context.Context, householdID, actorID, key string) (Item, []byte, error)
	Get(ctx context.Context, householdID, itemID string) (Item, error)
	List(ctx context.Context, householdID string, opts ListOptions) ([]Item, error)
	Summary(ctx context.Context, householdID string, opts SummaryOptions) (SummaryCounts, error)
	Complete(ctx context.Context, householdID, itemID, key string, fingerprint [32]byte, decide CompletionDecider) (Completion, bool, error)
	UndoCompletion(ctx context.Context, householdID, itemID, completionID string, decide CompletionUndoDecider) (Completion, error)
	ListCompletions(ctx context.Context, householdID, itemID, boundary string, limit int, newest bool) ([]Completion, error)
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

// Members validates that candidate userID belongs to householdID. A nil error
// means the candidate is a member, ErrNotFound means they are not a member, and
// any other error is treated as an infrastructure failure.
type Members func(ctx context.Context, userID, householdID string) error

type Service struct {
	repository Repository
	households Households
	subjects   Subjects
	members    Members
	now        func() time.Time
}

func NewService(repository Repository, households Households, subjects Subjects, members Members, now func() time.Time) *Service {
	return &Service{repository: repository, households: households, subjects: subjects, members: members, now: now}
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
	case errors.Is(err, ErrInvalidResponsibleReference):
		return &ValidationError{"responsibleUserId", "invalid_reference"}
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

// checkResponsibleMember requires candidate to be a member of the household and
// returns its canonical lowercase UUID.
func (s *Service) checkResponsibleMember(ctx context.Context, householdID string, candidate *string) (*string, error) {
	if candidate == nil {
		return nil, nil
	}
	invalid := &ValidationError{"responsibleUserId", "invalid_reference"}
	if !validUUID(*candidate) {
		return nil, invalid
	}
	id := strings.ToLower(*candidate)
	err := s.members(ctx, id, householdID)
	if errors.Is(err, ErrNotFound) {
		return nil, invalid
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	return &id, nil
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

func (s *Service) Create(ctx context.Context, userID, householdID, key string, n NewItem) (Item, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	actorID := strings.ToLower(userID)
	fingerprint := creationFingerprint(actorID, n)
	if existing, stored, findErr := s.repository.FindCreation(ctx, householdID, actorID, key); findErr == nil {
		return replayCreation(existing, stored, fingerprint, today)
	} else if !errors.Is(findErr, ErrNotFound) {
		return Item{}, mapRepoErr(findErr)
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
	if err := validateRecurrence(n.Recurrence); err != nil {
		return Item{}, err
	}
	n.Recurrence = canonicalRecurrence(n.Recurrence)
	var attentionOn *schedule.Date
	if n.AttentionOn != nil {
		d, err := ParseDate(*n.AttentionOn)
		if err != nil {
			return Item{}, err
		}
		attentionOn = &d
	}
	var historicalDate *schedule.Date
	if n.HistoricalCompletedOn != nil {
		d, err := ParseDateForField(*n.HistoricalCompletedOn, "historicalCompletedOn")
		if err != nil {
			return Item{}, err
		}
		historicalDate = &d
	}
	if n.HistoricalCompletedOn != nil {
		d := *historicalDate
		if n.Recurrence == nil || !n.Recurrence.Enabled {
			return Item{}, &ValidationError{"historicalCompletedOn", "requires_recurrence"}
		}
		if attentionOn != nil {
			return Item{}, &ValidationError{"historicalCompletedOn", "conflicting_fields"}
		}
		if d.Compare(today) > 0 {
			return Item{}, &ValidationError{"historicalCompletedOn", "future_date"}
		}
	}
	var receipt *Completion
	if historicalDate != nil {
		next, nextErr := schedule.NextCycle(nil, *historicalDate, *n.Recurrence)
		if errors.Is(nextErr, schedule.ErrDateOverflow) {
			return Item{}, &ValidationError{"recurrence", "date_overflow"}
		}
		if nextErr != nil {
			return Item{}, ErrUnavailable
		}
		attentionOn = next
		fingerprint := CompletionFingerprint(userID, n.HistoricalCompletedOn)
		receipt = &Completion{CompletedOn: *historicalDate, CompletedByUserID: userID, Recurrence: n.Recurrence, PriorWorkflowState: StateOpen, NextAttentionOn: next, ItemVersionBefore: 1, Fingerprint: fingerprint}
	}
	subjectID, err := s.checkSubject(ctx, userID, householdID, n.SubjectID)
	if err != nil {
		return Item{}, err
	}
	responsibleID, err := s.checkResponsibleMember(ctx, householdID, n.ResponsibleUserID)
	if err != nil {
		return Item{}, err
	}
	created, inserted, err := s.repository.CreateIdempotent(ctx, householdID, Draft{ActorID: actorID, CreationKey: key, CreationFingerprint: fingerprint, SubjectID: subjectID, ResponsibleUserID: responsibleID, Title: title, Notes: n.Notes, AttentionOn: attentionOn, Recurrence: n.Recurrence, InitializationReceipt: receipt})
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	if inserted {
		return derive(created, today)
	}
	existing, stored, err := s.repository.FindCreation(ctx, householdID, actorID, key)
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return replayCreation(existing, stored, fingerprint, today)
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
	sortBy := q.Sort
	if sortBy == "" {
		sortBy = SortCreated
	}
	var guard *schedule.Date
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return Page{}, &InvalidQueryError{"limit"}
	}
	collectionFilters, err := validateCollectionFilters(q.SubjectID, q.ResponsibleUserID, q.WorkflowState, q.Attention, q.Recurring)
	if err != nil {
		return Page{}, err
	}
	search := ""
	if q.Q != "" {
		search, err = ValidateSearch(q.Q)
		if err != nil {
			return Page{}, err
		}
	}
	filters := ListFilters{Archived: q.Archived, Done: q.Done, SubjectID: collectionFilters.SubjectID, ResponsibleUserID: collectionFilters.ResponsibleUserID, WorkflowState: collectionFilters.WorkflowState, Attention: collectionFilters.Attention, Recurring: collectionFilters.Recurring, Q: search}
	if sortBy != SortCreated && sortBy != SortAttention {
		return Page{}, &InvalidQueryError{"sort"}
	}
	if q.AttentionDate != "" {
		d, pe := ParseDateForField(q.AttentionDate, "attentionDate")
		if pe != nil {
			return Page{}, &InvalidQueryError{"attentionDate"}
		}
		guard = &d
	}
	if guard != nil && guard.Compare(today) != 0 {
		return Page{}, &InvalidQueryError{"attentionDate"}
	}
	guardText := ""
	if guard != nil {
		guardText = guard.String()
	}
	extended := q.SubjectID != "" || q.ResponsibleUserID != "" || q.WorkflowState != "" || q.Attention != "" || q.Recurring != nil || search != "" || sortBy == SortAttention || guard != nil
	after := nilUUID
	var afterAttention *schedule.Date
	if q.Cursor != "" {
		if extended {
			after, afterAttention, err = decodeBoundCursor(q.Cursor, householdID, filters, today, sortBy, guardText)
		} else {
			after, err = DecodeCursor(q.Cursor)
		}
		if err != nil {
			return Page{}, &InvalidQueryError{"cursor"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	rows, err := s.repository.List(ctx, householdID, ListOptions{Filters: filters, Today: today, Sort: sortBy, AfterID: after, AfterAttentionOn: afterAttention, Limit: limit + 1})
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := strings.ToLower(rows[limit-1].ID)
		var next string
		if extended {
			next = encodeBoundCursor(householdID, filters, today, sortBy, guardText, rows[limit-1])
		} else {
			next = EncodeCursor(last)
		}
		page.NextCursor = &next
	}
	out := make([]Item, 0, len(page.Items))
	for _, row := range page.Items {
		d, e := derive(row, today)
		if e != nil {
			return Page{}, e
		}
		out = append(out, d)
	}
	page.Items = out
	return page, nil
}

func listFingerprint(f ListFilters, sortBy ListSort, guardDate string) string {
	responsible := ""
	if f.ResponsibleUserID.Set {
		if f.ResponsibleUserID.Value == nil {
			responsible = "unassigned"
		} else {
			responsible = *f.ResponsibleUserID.Value
		}
	}
	workflow, attention, recurring := "", "", ""
	if f.WorkflowState != "" {
		workflow = string(f.WorkflowState)
	}
	if f.Attention != "" {
		attention = string(f.Attention)
	}
	if f.Recurring != nil {
		recurring = strconv.FormatBool(*f.Recurring)
	}
	fields := []string{"tendo.items.list.v2", strconv.FormatBool(f.Archived), strconv.FormatBool(f.Done), f.SubjectID, responsible, workflow, attention, recurring, string(sortBy), guardDate}
	if f.Q != "" {
		fields = append(fields, "q", f.Q)
	}
	b, _ := json.Marshal(fields)
	d := sha256.Sum256(b)
	return fmt.Sprintf("%x", d)
}
func encodeBoundCursor(h string, f ListFilters, today schedule.Date, sortBy ListSort, guardText string, last Item) string {
	date, pos := "-", "-"
	if f.Attention != "" || guardText != "" {
		date = today.String()
	}
	if sortBy == SortAttention && last.AttentionOn != nil {
		pos = last.AttentionOn.String()
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{"i2", h, listFingerprint(f, sortBy, guardText), date, pos, strings.ToLower(last.ID)}, ":")))
}
func decodeBoundCursor(cursor, household string, f ListFilters, today schedule.Date, sortBy ListSort, guardText string) (string, *schedule.Date, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", nil, err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 6 || parts[0] != "i2" || parts[1] != household || parts[2] != listFingerprint(f, sortBy, guardText) || !validUUID(parts[5]) || parts[5] != strings.ToLower(parts[5]) {
		return "", nil, errors.New("invalid bound cursor")
	}
	var afterDate *schedule.Date
	if sortBy == SortCreated {
		if parts[4] != "-" {
			return "", nil, errors.New("created cursor has date")
		}
	} else if parts[4] != "-" {
		d, pe := ParseDate(parts[4])
		if pe != nil {
			return "", nil, pe
		}
		afterDate = &d
	}
	wantDate := "-"
	if f.Attention != "" || guardText != "" {
		wantDate = today.String()
	}
	if parts[3] != wantDate {
		return "", nil, errors.New("stale bound cursor")
	}
	return strings.ToLower(parts[5]), afterDate, nil
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
	if p.Recurrence.Set {
		if err := validateRecurrence(p.Recurrence.Value); err != nil {
			return Item{}, err
		}
		p.Recurrence.Value = canonicalRecurrence(p.Recurrence.Value)
		c.Recurrence = p.Recurrence
	}
	if p.WorkflowState != nil {
		state, ok := ParseWorkflowState(*p.WorkflowState)
		if !ok {
			return Item{}, &ValidationError{"workflowState", "invalid_workflow_state"}
		}
		c.WorkflowState = &state
	}
	c.Archived = p.Archived
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if p.SubjectID != nil {
		subjectID, err := s.checkSubject(ctx, userID, householdID, *p.SubjectID)
		if err != nil {
			return Item{}, err
		}
		c.SubjectID = &subjectID
	}
	if p.ResponsibleUserID.Set {
		c.ResponsibleUserID.Set = true
		if p.ResponsibleUserID.Value != nil {
			responsibleID, err := s.checkResponsibleMember(ctx, householdID, p.ResponsibleUserID.Value)
			if err != nil {
				return Item{}, err
			}
			c.ResponsibleUserID.Value = responsibleID
		}
	}
	updated, err := s.repository.Update(ctx, householdID, itemID, expectedVersion, c)
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return derive(updated, today)
}

func replayCreation(existing Item, stored, submitted []byte, today schedule.Date) (Item, error) {
	if !bytes.Equal(stored, submitted) {
		return Item{}, ErrIdempotencyKeyReused
	}
	return derive(existing, today)
}

func creationFingerprint(actor string, n NewItem) []byte {
	var encoded []byte
	field := func(value *string) {
		if value == nil {
			encoded = append(encoded, 0)
			return
		}
		encoded = append(encoded, 1)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(*value)))
		encoded = append(encoded, size[:]...)
		encoded = append(encoded, (*value)...)
	}
	for _, value := range []string{"tendo.item.create.v1", strings.ToLower(actor), strings.TrimSpace(n.Title), strings.ToLower(n.SubjectID)} {
		v := value
		field(&v)
	}
	if n.ResponsibleUserID == nil {
		field(nil)
	} else {
		normalized := strings.ToLower(*n.ResponsibleUserID)
		field(&normalized)
	}
	field(n.AttentionOn)
	field(n.Notes)
	recurrence := canonicalRecurrence(n.Recurrence)
	if recurrence == nil {
		field(nil)
	} else {
		fieldString := strings.Join([]string{strconv.Itoa(recurrence.Interval.Value), string(recurrence.Interval.Unit), string(recurrence.Mode)}, "\x00")
		field(&fieldString)
	}
	field(n.HistoricalCompletedOn)
	digest := sha256.Sum256(encoded)
	return digest[:]
}

func canonicalRecurrence(policy *schedule.Policy) *schedule.Policy {
	if policy == nil || !policy.Enabled {
		return nil
	}
	return policy
}

func validateRecurrence(policy *schedule.Policy) error {
	if policy == nil {
		return nil
	}
	if policy.Interval.Value < 1 || policy.Interval.Value > 999 {
		return &ValidationError{"recurrence", "invalid_interval"}
	}
	switch policy.Interval.Unit {
	case schedule.UnitDay, schedule.UnitWeek, schedule.UnitMonth, schedule.UnitYear:
	default:
		return &ValidationError{"recurrence", "invalid_interval_unit"}
	}
	switch policy.Mode {
	case schedule.ModeFixed, schedule.ModeAfterCompletion:
	default:
		return &ValidationError{"recurrence", "invalid_mode"}
	}
	return nil
}

func forbiddenRune(r rune) bool {
	// Control, format (zero-width, bidi), and line or paragraph separator
	// characters can hide or reorder text.
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// ValidateSearch returns the trimmed search term or an invalid-query error.
func ValidateSearch(q string) (string, error) {
	if !utf8.ValidString(q) {
		return "", &InvalidQueryError{"q"}
	}
	q = strings.TrimSpace(q)
	if q == "" || utf8.RuneCountInString(q) > MaxSearchRunes {
		return "", &InvalidQueryError{"q"}
	}
	for _, r := range q {
		if forbiddenRune(r) {
			return "", &InvalidQueryError{"q"}
		}
	}
	return q, nil
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
func ParseDate(s string) (schedule.Date, error) { return ParseDateForField(s, "attentionOn") }

func ParseDateForField(s, field string) (schedule.Date, error) {
	invalid := &ValidationError{field, "invalid_date"}
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
