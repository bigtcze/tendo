package item

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"strconv"
	"strings"
)

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

func (s *Service) List(ctx context.Context, userID, householdID string, q ListQuery) (Page, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Page{}, err
	}
	prepared, err := prepareListQuery(q, today)
	if err != nil {
		return Page{}, err
	}
	cursor, err := listCursorContext(q.Cursor, householdID, prepared, today)
	if err != nil {
		return Page{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	options := ListOptions{
		Filters: prepared.filters, Today: today, Sort: prepared.sort,
		AfterID: cursor.afterID, AfterAttentionOn: cursor.afterAttention,
		Limit: prepared.limit + 1,
	}
	rows, err := s.repository.List(ctx, householdID, options)
	if err != nil {
		return Page{}, ErrUnavailable
	}
	return buildListPage(rows, prepared, cursor.extended, householdID, today)
}

type preparedList struct {
	filters   ListFilters
	sort      ListSort
	guardText string
	extended  bool
	limit     int
}
type listCursor struct {
	afterID        string
	afterAttention *schedule.Date
	extended       bool
}

func prepareListQuery(q ListQuery, today schedule.Date) (preparedList, error) {
	prepared := preparedList{sort: q.Sort, limit: q.Limit}
	if prepared.sort == "" {
		prepared.sort = SortCreated
	}
	if prepared.limit == 0 {
		prepared.limit = DefaultLimit
	}
	if prepared.limit < 1 || prepared.limit > MaxLimit {
		return prepared, &InvalidQueryError{"limit"}
	}
	collection, err := validateCollectionFilters(
		q.SubjectID, q.ResponsibleUserID, q.WorkflowState, q.Attention, q.Recurring,
	)
	if err != nil {
		return prepared, err
	}
	search := ""
	if q.Q != "" {
		search, err = ValidateSearch(q.Q)
		if err != nil {
			return prepared, err
		}
	}
	prepared.filters = ListFilters{
		Archived: q.Archived, Done: q.Done, SubjectID: collection.SubjectID,
		ResponsibleUserID: collection.ResponsibleUserID, WorkflowState: collection.WorkflowState,
		Attention: collection.Attention, Recurring: collection.Recurring, Q: search,
	}
	if err := validateListSort(prepared.sort); err != nil {
		return prepared, err
	}
	guardText, err := listGuard(q.AttentionDate, today)
	if err != nil {
		return prepared, err
	}
	prepared.guardText = guardText
	prepared.extended = hasExtendedListQuery(q, search, prepared.sort, guardText)
	return prepared, nil
}

func validateListSort(sortBy ListSort) error {
	if sortBy != SortCreated && sortBy != SortAttention {
		return &InvalidQueryError{"sort"}
	}
	return nil
}

func hasExtendedListQuery(q ListQuery, search string, sortBy ListSort, guardText string) bool {
	return q.SubjectID != "" || q.ResponsibleUserID != "" || q.WorkflowState != "" ||
		q.Attention != "" || q.Recurring != nil || search != "" ||
		sortBy == SortAttention || guardText != ""
}

func listGuard(value string, today schedule.Date) (string, error) {
	if value == "" {
		return "", nil
	}
	d, err := ParseDateForField(value, "attentionDate")
	if err != nil || d.Compare(today) != 0 {
		return "", &InvalidQueryError{"attentionDate"}
	}
	return d.String(), nil
}

func listCursorContext(value, household string, prepared preparedList, today schedule.Date) (listCursor, error) {
	cursor := listCursor{afterID: nilUUID, extended: prepared.extended}
	if value == "" {
		return cursor, nil
	}
	var err error
	if prepared.extended {
		cursor.afterID, cursor.afterAttention, err = decodeBoundCursor(
			value, household, prepared.filters, today, prepared.sort, prepared.guardText,
		)
	} else {
		cursor.afterID, err = DecodeCursor(value)
	}
	if err != nil {
		return listCursor{}, &InvalidQueryError{"cursor"}
	}
	return cursor, nil
}

func buildListPage(
	rows []Item, prepared preparedList, extended bool, household string, today schedule.Date,
) (Page, error) {
	page := Page{Items: rows}
	if len(rows) > prepared.limit {
		page.Items = rows[:prepared.limit]
		last := rows[prepared.limit-1]
		var next string
		if extended {
			next = encodeBoundCursor(household, prepared.filters, today, prepared.sort, prepared.guardText, last)
		} else {
			next = EncodeCursor(strings.ToLower(last.ID))
		}
		page.NextCursor = &next
	}
	out := make([]Item, 0, len(page.Items))
	for _, row := range page.Items {
		derived, err := derive(row, today)
		if err != nil {
			return Page{}, err
		}
		out = append(out, derived)
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
	fields := []string{
		"tendo.items.list.v2", strconv.FormatBool(f.Archived), strconv.FormatBool(f.Done),
		f.SubjectID, responsible, workflow, attention, recurring, string(sortBy), guardDate,
	}
	if f.Q != "" {
		fields = append(fields, "q", f.Q)
	}
	b, _ := json.Marshal(fields)
	d := sha256.Sum256(b)
	return fmt.Sprintf("%x", d)
}
func encodeBoundCursor(
	h string, f ListFilters, today schedule.Date, sortBy ListSort, guardText string, last Item,
) string {
	date, pos := "-", "-"
	if f.Attention != "" || guardText != "" {
		date = today.String()
	}
	if sortBy == SortAttention && last.AttentionOn != nil {
		pos = last.AttentionOn.String()
	}
	fields := []string{"i2", h, listFingerprint(f, sortBy, guardText), date, pos, strings.ToLower(last.ID)}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, ":")))
}
func decodeBoundCursor(
	cursor, household string, f ListFilters, today schedule.Date, sortBy ListSort, guardText string,
) (string, *schedule.Date, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", nil, err
	}
	parts := strings.Split(string(raw), ":")
	invalid := len(parts) != 6 || parts[0] != "i2" || parts[1] != household ||
		parts[2] != listFingerprint(f, sortBy, guardText) || !validUUID(parts[5]) ||
		parts[5] != strings.ToLower(parts[5])
	if invalid {
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
