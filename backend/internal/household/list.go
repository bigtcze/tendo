package household

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// ListedHousehold is one row of the caller's membership-scoped household list.
// It deliberately omits the resource version: list responses carry no per-item
// ETag, and a listed household is not a concurrency token.
type ListedHousehold struct {
	ID         string
	Name       string
	Timezone   string
	CreatedAt  time.Time
	ViewerRole string
}

// EncodeHouseholdCursor produces the opaque "h1" cursor for a household ID.
func EncodeHouseholdCursor(id string) string {
	return "h1" + base64.RawURLEncoding.EncodeToString([]byte(id))
}

// DecodeHouseholdCursor validates and unwraps a household-list cursor. It
// mirrors the members cursor: only the exact opaque form is accepted, so a
// forged or truncated cursor cannot reach persistence.
func DecodeHouseholdCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) < 3 || cursor[:2] != "h1" {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(cursor[2:])
	if e != nil || len(b) != 36 || !ids.IsCanonicalUUID(string(b)) {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	return string(b), nil
}

// ListForMember returns one page of households the caller belongs to, in
// household ID order. Membership is the only authorization: the caller ID comes
// from the authenticated principal and the query filters on it. An unknown
// stored role fails closed as an unavailable response.
func (s *Service) ListForMember(ctx context.Context, userID, cursor string, limit int) ([]ListedHousehold, string, error) {
	if !ids.IsCanonicalUUID(userID) {
		return nil, "", ErrNotFound
	}
	afterID, err := DecodeHouseholdCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	rows, next, err := s.repository.ListForMember(ctx, userID, afterID, limit)
	if errors.Is(err, ErrNotFound) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", errUnavailable
	}
	for _, row := range rows {
		if row.ViewerRole != RoleAdmin && row.ViewerRole != RoleMember {
			return nil, "", errUnavailable
		}
	}
	return rows, next, nil
}
