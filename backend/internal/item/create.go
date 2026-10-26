package item

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

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

func (s *Service) Create(ctx context.Context, userID, householdID, key string, n NewItem) (Item, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	actorID := strings.ToLower(userID)
	fingerprint := creationFingerprint(actorID, n)
	existing, found, err := s.findCreationReplay(ctx, householdID, actorID, key, fingerprint, today)
	if found || err != nil {
		return existing, err
	}
	draft, err := s.validateDraft(ctx, userID, householdID, today, actorID, key, fingerprint, n)
	if err != nil {
		return Item{}, err
	}
	return s.persistCreation(ctx, householdID, actorID, key, fingerprint, today, draft)
}

func (s *Service) findCreationReplay(
	ctx context.Context, householdID, actorID, key string, fingerprint []byte, today schedule.Date,
) (Item, bool, error) {
	existing, stored, err := s.repository.FindCreation(ctx, householdID, actorID, key)
	if errors.Is(err, ErrNotFound) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, true, mapRepoErr(err)
	}
	item, err := replayCreation(existing, stored, fingerprint, today)
	return item, true, err
}

func (s *Service) persistCreation(
	ctx context.Context, householdID, actorID, key string, fingerprint []byte, today schedule.Date, draft Draft,
) (Item, error) {
	created, inserted, err := s.repository.CreateIdempotent(ctx, householdID, draft)
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
func (s *Service) validateDraft(
	ctx context.Context, userID, householdID string, today schedule.Date,
	actorID, key string, fingerprint []byte, n NewItem,
) (Draft, error) {
	title, err := ValidateTitle(n.Title)
	if err != nil {
		return Draft{}, err
	}
	if n.Notes != nil {
		if err := ValidateNotes(*n.Notes); err != nil {
			return Draft{}, err
		}
	}
	if err := validateRecurrence(n.Recurrence); err != nil {
		return Draft{}, err
	}
	n.Recurrence = canonicalRecurrence(n.Recurrence)
	attentionOn, historicalDate, err := parseCreationDates(n)
	if err != nil {
		return Draft{}, err
	}
	attentionOn, receipt, err := initializeHistorical(today, n, attentionOn, historicalDate, userID)
	if err != nil {
		return Draft{}, err
	}
	subjectID, err := s.checkSubject(ctx, userID, householdID, n.SubjectID)
	if err != nil {
		return Draft{}, err
	}
	responsibleID, err := s.checkResponsibleMember(ctx, householdID, n.ResponsibleUserID)
	if err != nil {
		return Draft{}, err
	}
	return Draft{
		ActorID: actorID, CreationKey: key, CreationFingerprint: fingerprint,
		SubjectID: subjectID, ResponsibleUserID: responsibleID, Title: title,
		Notes: n.Notes, AttentionOn: attentionOn, Recurrence: n.Recurrence,
		InitializationReceipt: receipt,
	}, nil
}

func parseCreationDates(n NewItem) (*schedule.Date, *schedule.Date, error) {
	var attention, historical *schedule.Date
	if n.AttentionOn != nil {
		d, err := ParseDate(*n.AttentionOn)
		if err != nil {
			return nil, nil, err
		}
		attention = &d
	}
	if n.HistoricalCompletedOn != nil {
		d, err := ParseDateForField(*n.HistoricalCompletedOn, "historicalCompletedOn")
		if err != nil {
			return nil, nil, err
		}
		historical = &d
	}
	return attention, historical, nil
}

func initializeHistorical(
	today schedule.Date, n NewItem, attentionOn, historicalDate *schedule.Date, userID string,
) (*schedule.Date, *Completion, error) {
	if historicalDate == nil {
		return attentionOn, nil, nil
	}
	if n.Recurrence == nil || !n.Recurrence.Enabled {
		return nil, nil, &ValidationError{"historicalCompletedOn", "requires_recurrence"}
	}
	if attentionOn != nil {
		return nil, nil, &ValidationError{"historicalCompletedOn", "conflicting_fields"}
	}
	if historicalDate.Compare(today) > 0 {
		return nil, nil, &ValidationError{"historicalCompletedOn", "future_date"}
	}
	next, err := schedule.NextCycle(nil, *historicalDate, *n.Recurrence)
	if errors.Is(err, schedule.ErrDateOverflow) {
		return nil, nil, &ValidationError{"recurrence", "date_overflow"}
	}
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	fingerprint := CompletionFingerprint(userID, n.HistoricalCompletedOn)
	receipt := &Completion{
		CompletedOn: *historicalDate, CompletedByUserID: userID,
		Recurrence: n.Recurrence, PriorWorkflowState: StateOpen,
		NextAttentionOn: next, ItemVersionBefore: 1, Fingerprint: fingerprint,
	}
	return next, receipt, nil
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
	values := []string{
		"tendo.item.create.v1", strings.ToLower(actor),
		strings.TrimSpace(n.Title), strings.ToLower(n.SubjectID),
	}
	for _, value := range values {
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
		fieldString := strings.Join([]string{
			strconv.Itoa(recurrence.Interval.Value), string(recurrence.Interval.Unit), string(recurrence.Mode),
		}, "\x00")
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
