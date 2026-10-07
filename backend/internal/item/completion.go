package item

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

var (
	ErrArchived             = errors.New("item archived")
	ErrDone                 = errors.New("item done")
	ErrIdempotencyKeyReused = errors.New("idempotency key reused")
)

type Completion struct {
	ID, HouseholdID, ItemID, CompletedByUserID string
	CompletedOn                                schedule.Date
	CycleAttentionOn                           *schedule.Date
	Recurrence                                 *schedule.Policy
	PriorWorkflowState                         WorkflowState
	NextAttentionOn                            *schedule.Date
	ItemVersionBefore                          int64
	IdempotencyKey                             string
	Fingerprint                                [32]byte
	CreatedAt                                  time.Time
}

type CompletionPlan struct {
	Receipt Completion
	Item    Item
}
type CompletionDecider func(Item, *Completion) (CompletionPlan, error)

type CompletionRequest struct {
	CompletedOn *string
}

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
	if !validUUID(itemID) {
		return Completion{}, false, ErrNotFound
	}
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Completion{}, false, err
	}
	itemID = strings.ToLower(itemID)
	var requested *schedule.Date
	repo := s.repository
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	fingerprint := CompletionFingerprint(userID, request.CompletedOn)
	decide := func(current Item, existing *Completion) (CompletionPlan, error) {
		if existing != nil {
			if existing.Fingerprint != fingerprint {
				return CompletionPlan{}, ErrIdempotencyKeyReused
			}
			return CompletionPlan{Receipt: *existing, Item: current}, nil
		}
		if request.CompletedOn != nil {
			d, e := ParseDate(*request.CompletedOn)
			if e != nil {
				return CompletionPlan{}, &ValidationError{"completedOn", "invalid_date"}
			}
			requested = &d
		}
		completed := today
		if requested != nil {
			completed = *requested
		}
		if completed.Compare(today) > 0 {
			return CompletionPlan{}, &ValidationError{"completedOn", "future_date"}
		}
		if current.Version != expectedVersion {
			return CompletionPlan{}, ErrVersionMismatch
		}
		if current.Archived {
			return CompletionPlan{}, ErrArchived
		}
		if current.Done {
			return CompletionPlan{}, ErrDone
		}
		next, err := schedule.NextCycle(current.AttentionOn, completed, policyOrDisabled(current.Recurrence))
		if errors.Is(err, schedule.ErrDateOverflow) {
			return CompletionPlan{}, &ValidationError{"recurrence", "date_overflow"}
		}
		if err != nil {
			return CompletionPlan{}, ErrUnavailable
		}
		receipt := Completion{HouseholdID: householdID, ItemID: itemID, CompletedByUserID: userID, CompletedOn: completed, CycleAttentionOn: current.AttentionOn, Recurrence: current.Recurrence, PriorWorkflowState: current.WorkflowState, NextAttentionOn: next, ItemVersionBefore: current.Version, IdempotencyKey: key, Fingerprint: fingerprint}
		updated := current
		if current.Recurrence != nil {
			updated.AttentionOn, updated.WorkflowState, updated.Done = next, StateOpen, false
		} else {
			updated.Done = true
		}
		updated.LastCompletedOn = &completed
		updated.Version++
		return CompletionPlan{Receipt: receipt, Item: updated}, nil
	}
	result, replayed, err := repo.Complete(ctx, householdID, itemID, key, fingerprint, decide)
	if errors.Is(err, ErrIdempotencyKeyReused) || errors.Is(err, ErrVersionMismatch) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrArchived) || errors.Is(err, ErrDone) {
		return Completion{}, false, err
	}
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			return Completion{}, false, err
		}
		return Completion{}, false, ErrUnavailable
	}
	return result, replayed, nil
}

func policyOrDisabled(policy *schedule.Policy) schedule.Policy {
	if policy == nil {
		return schedule.Policy{}
	}
	return *policy
}

const completionCursorPrefix = "c1:"

func EncodeCompletionCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(completionCursorPrefix + strings.ToLower(id)))
}
func DecodeCompletionCursor(cursor string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", err
	}
	id, ok := strings.CutPrefix(string(raw), completionCursorPrefix)
	if !ok || !validUUID(id) {
		return "", errors.New("invalid completion cursor")
	}
	return strings.ToLower(id), nil
}

type CompletionPage struct {
	Items      []Completion
	NextCursor *string
}

func (s *Service) ListCompletions(ctx context.Context, userID, householdID, itemID string, limit int, cursor string) (CompletionPage, error) {
	householdID, _, err := s.check(ctx, userID, householdID)
	if err != nil {
		return CompletionPage{}, err
	}
	if !validUUID(itemID) {
		return CompletionPage{}, ErrNotFound
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return CompletionPage{}, &InvalidQueryError{"limit"}
	}
	after := nilUUID
	if cursor != "" {
		after, err = DecodeCompletionCursor(cursor)
		if err != nil {
			return CompletionPage{}, &InvalidQueryError{"cursor"}
		}
	}
	repo := s.repository
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	rows, err := repo.ListCompletions(ctx, householdID, strings.ToLower(itemID), after, limit+1)
	if errors.Is(err, ErrNotFound) {
		return CompletionPage{}, err
	}
	if err != nil {
		return CompletionPage{}, ErrUnavailable
	}
	page := CompletionPage{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		next := EncodeCompletionCursor(rows[limit-1].ID)
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Completion{}
	}
	return page, nil
}
