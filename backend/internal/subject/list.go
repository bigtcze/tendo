package subject

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func (s *Service) List(ctx context.Context, userID, householdID string, q ListQuery) (Page, error) {
	householdID, err := s.check(ctx, userID, householdID)
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
	ids, err := CanonicalIDs(q.IDs)
	if err != nil {
		return Page{}, &InvalidQueryError{"id"}
	}
	after := nilUUID
	if q.Cursor != "" {
		var cursorErr error
		if q.IDs == nil {
			after, cursorErr = DecodeCursor(q.Cursor)
		} else {
			after, cursorErr = decodeFilteredCursor(q.Cursor, householdID, q.Archived, ids)
		}
		if cursorErr != nil {
			return Page{}, &InvalidQueryError{"cursor"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	var items []Subject
	if q.IDs == nil {
		items, err = s.repository.List(ctx, householdID, q.Archived, after, limit+1)
	} else {
		items, err = s.repository.ListByIDs(ctx, householdID, q.Archived, ids, after, limit+1)
	}
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next := EncodeCursor(items[limit-1].ID)
		if q.IDs != nil {
			next = encodeFilteredCursor(householdID, q.Archived, ids, items[limit-1].ID)
		}
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Subject{}
	}
	return page, nil
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

// CanonicalIDs validates, lowercases, sorts, and deduplicates list filter IDs.
func CanonicalIDs(input []string) ([]string, error) {
	if input == nil {
		return nil, nil
	}
	if len(input) > 100 {
		return nil, errors.New("too many ids")
	}
	seen := make(map[string]struct{}, len(input))
	ids := make([]string, 0, len(input))
	for _, id := range input {
		if !validUUID(id) {
			return nil, errors.New("invalid id")
		}
		id = strings.ToLower(id)
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func filteredFingerprint(householdID string, archived bool, ids []string) string {
	fields := []string{"tendo.subject.list.v2", strings.ToLower(householdID), fmt.Sprint(archived)}
	fields = append(fields, ids...)
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return fmt.Sprintf("%x", digest[:16])
}

func encodeFilteredCursor(householdID string, archived bool, ids []string, lastID string) string {
	raw := fmt.Sprintf("s2:%s:%s", filteredFingerprint(householdID, archived, ids), strings.ToLower(lastID))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func validFilteredCursorParts(parts []string, householdID string, archived bool, ids []string) bool {
	if len(parts) != 3 || parts[0] != "s2" {
		return false
	}
	return parts[1] == filteredFingerprint(householdID, archived, ids) &&
		validUUID(parts[2]) && parts[2] == strings.ToLower(parts[2])
}

func decodeFilteredCursor(cursor, householdID string, archived bool, ids []string) (string, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(raw), ":")
	if !validFilteredCursorParts(parts, householdID, archived, ids) {
		return "", errors.New("invalid filtered cursor")
	}
	return parts[2], nil
}
