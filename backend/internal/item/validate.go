package item

import (
	"context"
	"errors"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

// InvalidQueryError names the offending list query parameter.
type InvalidQueryError struct{ Parameter string }

func (e *InvalidQueryError) Error() string { return "invalid query parameter " + e.Parameter }

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

// validationAfterExistenceCheck preserves PATCH evaluation order without adding
// reads to valid updates. The scoped probe maps infrastructure failures normally.
func (s *Service) validationAfterExistenceCheck(ctx context.Context, householdID, itemID string, validationErr error) error {
	var validation *ValidationError
	if !errors.As(validationErr, &validation) {
		return validationErr
	}
	probeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := s.repository.Get(probeCtx, householdID, itemID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	return validationErr
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
