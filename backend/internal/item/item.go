package item

import (
	"context"
	"errors"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"strings"
	"time"
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
type Nullable[T any] struct {
	Set   bool
	Value *T
}

func Some[T any](v T) Nullable[T] { return Nullable[T]{Set: true, Value: &v} }
func Null[T any]() Nullable[T]    { return Nullable[T]{Set: true} }

type NewItem struct {
	SubjectID             string
	ResponsibleUserID     *string
	Title                 string
	Notes                 *string
	AttentionOn           *string
	HistoricalCompletedOn *string
	Recurrence            *schedule.Policy
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
func derive(i Item, today schedule.Date) (Item, error) {
	a, err := schedule.DeriveAttention(i.AttentionOn, today)
	if err != nil {
		return Item{}, ErrUnavailable
	}
	i.Attention = a
	return i, nil
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
