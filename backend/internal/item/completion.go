package item

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

var (
	ErrArchived             = errors.New("item archived")
	ErrDone                 = errors.New("item done")
	ErrIdempotencyKeyReused = errors.New("idempotency key reused")
	ErrCompletionNotLatest  = errors.New("completion not latest")
	// ErrCompletionSuperseded means the item's schedule state has moved away from
	// what the completion receipt produced, so undoing it would silently discard
	// a later edit.
	ErrCompletionSuperseded = errors.New("completion superseded")
)

type Completion struct {
	ID, HouseholdID, ItemID, CompletedByUserID string
	CompletedOn                                schedule.Date
	CycleAttentionOn                           *schedule.Date
	// CycleAnchorDay snapshots the item anchor before the completion and
	// NextAnchorDay the anchor of the resulting cycle. Both are nil on receipts
	// written before anchors existed (legacy) and when there was no cycle.
	CycleAnchorDay     *int
	NextAnchorDay      *int
	Recurrence         *schedule.Policy
	PriorWorkflowState WorkflowState
	NextAttentionOn    *schedule.Date
	ItemVersionBefore  int64
	IdempotencyKey     string
	Fingerprint        [32]byte
	CreatedAt          time.Time
	UndoneAt           *time.Time
	UndoneByUserID     *string
}

type CompletionPlan struct {
	Receipt Completion
	Item    Item
}
type CompletionDecider func(Item, *Completion) (CompletionPlan, error)

type CompletionRequest struct {
	CompletedOn *string
}

type CompletionUndoPlan struct {
	Receipt        Completion
	Item           Item
	Apply          bool
	UndoneAt       time.Time
	UndoneByUserID string
}
type CompletionUndoDecider func(Item, Completion, *int64) (CompletionUndoPlan, error)

func ValidateIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func CompletionFingerprint(userID string, submitted *string) [32]byte {
	value := "-"
	if submitted != nil {
		value = "d:" + *submitted
	}
	return sha256.Sum256([]byte("v1\x00" + userID + "\x00" + value))
}

func (s *Service) Complete(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, key string, request CompletionRequest) (Completion, bool, error) {
	if !ValidateIdempotencyKey(key) {
		return Completion{}, false, &ValidationError{"", "invalid_idempotency_key"}
	}
	if !ids.IsCanonicalUUID(itemID) {
		return Completion{}, false, ErrNotFound
	}
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Completion{}, false, err
	}
	itemID = strings.ToLower(itemID)
	repo := s.repository
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	fingerprint := CompletionFingerprint(userID, request.CompletedOn)
	decide := completionDecider(userID, householdID, itemID, expectedVersion, key, fingerprint, request, today)
	result, replayed, err := repo.Complete(ctx, householdID, itemID, key, fingerprint, decide)
	if err = completionServiceError(err); err != nil {
		return Completion{}, false, err
	}
	return result, replayed, nil
}

func (s *Service) UndoCompletion(ctx context.Context, userID, householdID, itemID, completionID string, expectedVersion int64) (Completion, error) {
	if !ids.IsCanonicalUUID(itemID) || !ids.IsCanonicalUUID(completionID) {
		return Completion{}, ErrNotFound
	}
	householdID, _, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Completion{}, err
	}
	itemID, completionID = strings.ToLower(itemID), strings.ToLower(completionID)
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	decide := func(current Item, receipt Completion, latest *int64) (CompletionUndoPlan, error) {
		return planUndo(current, receipt, latest, expectedVersion, userID, s.now)
	}
	result, err := s.repository.UndoCompletion(ctx, householdID, itemID, completionID, decide)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrVersionMismatch) || errors.Is(err, ErrArchived) ||
		errors.Is(err, ErrCompletionNotLatest) || errors.Is(err, ErrCompletionSuperseded) {
		return Completion{}, err
	}
	if err != nil {
		return Completion{}, ErrUnavailable
	}
	return result, nil
}

// planUndo validates the undo and builds the item transition. An already-undone
// receipt replays unchanged; otherwise version, archived state, latest-active
// receipt and superseded schedule state are checked in that order.
func planUndo(current Item, receipt Completion, latest *int64, expectedVersion int64, userID string, now func() time.Time) (CompletionUndoPlan, error) {
	if receipt.UndoneAt != nil {
		return CompletionUndoPlan{Receipt: receipt}, nil
	}
	if current.Version != expectedVersion {
		return CompletionUndoPlan{}, ErrVersionMismatch
	}
	if current.Archived {
		return CompletionUndoPlan{}, ErrArchived
	}
	if latest == nil || *latest != receipt.ItemVersionBefore {
		return CompletionUndoPlan{}, ErrCompletionNotLatest
	}
	if !undoable(current, receipt) {
		return CompletionUndoPlan{}, ErrCompletionSuperseded
	}
	updated := current
	updated.AttentionOn = receipt.CycleAttentionOn
	updated.AttentionAnchorDay = undoAnchor(receipt, current)
	updated.WorkflowState = receipt.PriorWorkflowState
	updated.Done = false
	updated.Version++
	return CompletionUndoPlan{Receipt: receipt, Item: updated, Apply: true, UndoneAt: now().UTC(), UndoneByUserID: userID}, nil
}

// undoable reports whether the current item still shows the schedule state the
// receipt produced. It compares schedule state only (never the workflow state)
// so chained undo keeps working, and it classifies by the receipt snapshot, not
// the current policy.
func undoable(current Item, receipt Completion) bool {
	if receipt.Recurrence != nil {
		if current.Done || !sameDate(current.AttentionOn, receipt.NextAttentionOn) {
			return false
		}
		if receipt.NextAnchorDay != nil {
			return current.AttentionAnchorDay != nil && *current.AttentionAnchorDay == *receipt.NextAnchorDay
		}
		return true
	}
	return current.Done && sameDate(current.AttentionOn, receipt.CycleAttentionOn)
}

// undoAnchor restores the anchor that was active before the completion. Legacy
// receipts carry no anchor, so the current anchor is kept when it is still a
// valid clamp-consistent anchor for the restored cycle date.
func undoAnchor(receipt Completion, current Item) *int {
	if receipt.CycleAttentionOn == nil {
		return nil
	}
	if receipt.CycleAnchorDay != nil {
		anchor := *receipt.CycleAnchorDay
		return &anchor
	}
	if current.AttentionAnchorDay != nil {
		if _, err := schedule.NewCycle(*receipt.CycleAttentionOn, *current.AttentionAnchorDay); err == nil {
			anchor := *current.AttentionAnchorDay
			return &anchor
		}
	}
	anchor := receipt.CycleAttentionOn.Day()
	return &anchor
}

func sameDate(a, b *schedule.Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Compare(*b) == 0
}

func policyOrDisabled(policy *schedule.Policy) schedule.Policy {
	if policy == nil {
		return schedule.Policy{}
	}
	return *policy
}

const completionCursorPrefix = "c1:"
const completionNewestCursorPrefix = "c2:"

const (
	CompletionOrderOldest = "oldest"
	CompletionOrderNewest = "newest"
)

const newestCompletionBoundary = "ffffffff-ffff-ffff-ffff-ffffffffffff"

func EncodeCompletionCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(completionCursorPrefix + strings.ToLower(id)))
}
func EncodeCompletionCursorForOrder(id, order string) string {
	prefix := completionCursorPrefix
	if order == CompletionOrderNewest {
		prefix = completionNewestCursorPrefix
	}
	return base64.RawURLEncoding.EncodeToString([]byte(prefix + strings.ToLower(id)))
}

func DecodeCompletionCursorForOrder(cursor, order string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", err
	}
	prefix := completionCursorPrefix
	if order == CompletionOrderNewest {
		prefix = completionNewestCursorPrefix
	}
	id, ok := strings.CutPrefix(string(raw), prefix)
	if !ok || !ids.IsCanonicalUUID(id) {
		return "", errors.New("invalid completion cursor")
	}
	return strings.ToLower(id), nil
}

func DecodeCompletionCursor(cursor string) (string, error) {
	return DecodeCompletionCursorForOrder(cursor, CompletionOrderOldest)
}

type CompletionPage struct {
	Items      []Completion
	NextCursor *string
}

func (s *Service) ListCompletions(ctx context.Context, userID, householdID, itemID string, limit int, cursor, order string) (CompletionPage, error) {
	householdID, _, err := s.check(ctx, userID, householdID)
	if err != nil {
		return CompletionPage{}, err
	}
	if !ids.IsCanonicalUUID(itemID) {
		return CompletionPage{}, ErrNotFound
	}
	query, err := parseCompletionListQuery(limit, cursor, order)
	if err != nil {
		return CompletionPage{}, err
	}
	repo := s.repository
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	rows, err := repo.ListCompletions(ctx, householdID, strings.ToLower(itemID), query.after, query.limit+1, query.newest)
	if errors.Is(err, ErrNotFound) {
		return CompletionPage{}, err
	}
	if err != nil {
		return CompletionPage{}, ErrUnavailable
	}
	page := CompletionPage{Items: rows}
	if len(rows) > query.limit {
		page.Items = rows[:query.limit]
		next := EncodeCompletionCursorForOrder(rows[query.limit-1].ID, query.order)
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Completion{}
	}
	return page, nil
}
