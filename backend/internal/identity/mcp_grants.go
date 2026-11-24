package identity

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// Slice A4b (ADR 0023, connected apps). A signed-in person lists and revokes
// their own MCP grants. Both operations work whether or not TENDO_MCP_ENABLED
// is set: while MCP is off the grants are paused, not removed, so a person can
// still see and disconnect them. Neither operation consults Available(), the
// installation switch or TENDO_MCP_ENABLED.

const (
	// MCPGrantListDefaultLimit is the page size when the caller names none.
	MCPGrantListDefaultLimit = 50
	// MCPGrantListMaxLimit bounds one page.
	MCPGrantListMaxLimit = 100
	// mcpGrantCursorPrefix versions the opaque cursor.
	mcpGrantCursorPrefix = "g1"
	// mcpGrantCursorLength is the exact length of a cursor: the prefix plus the
	// unpadded base64url encoding of 16 UUID bytes.
	mcpGrantCursorLength = len(mcpGrantCursorPrefix) + 22
)

var (
	// ErrMCPInvalidCursor is a connected-apps cursor that is not the exact
	// opaque form this service issued. It never reaches persistence.
	ErrMCPInvalidCursor = errors.New("mcp invalid cursor")
	// errMCPGrantStoreMissing fails the list closed when the repository does not
	// provide the connected-apps read.
	errMCPGrantStoreMissing = errors.New("mcp grant store is not configured")
)

// MCPGrantItem is one live connected app of the signed-in account. It carries
// no token, no secret and no resource: the client name is the app's own
// registration and is not verified.
type MCPGrantItem struct {
	ID, ClientName       string
	Scopes               []string
	CreatedAt, ExpiresAt time.Time
}

// MCPGrantPage is one page of connected apps. NextCursor is empty on the last
// page.
type MCPGrantPage struct {
	Items      []MCPGrantItem
	NextCursor string
}

// MCPGrantStore is the connected-apps read. It is a plain pool read, outside any
// issuance transaction. The service finds it by type assertion on the
// repository, so the other repository fakes need not implement it.
type MCPGrantStore interface {
	// ListLiveGrants returns up to limit live grants of the account (not
	// revoked, not expired) with id strictly below afterID when afterID is
	// non-empty, newest id first.
	ListLiveGrants(ctx context.Context, userID string, now time.Time, afterID string, limit int) ([]MCPGrantItem, error)
}

// ListGrants returns one page of the signed-in account's live connected apps.
// A limit of zero means the default, and anything above the maximum is clamped.
// A cursor that is not the exact opaque form is ErrMCPInvalidCursor.
func (s *MCPService) ListGrants(ctx context.Context, userID string, limit int, cursor string) (MCPGrantPage, error) {
	afterID, err := decodeMCPGrantCursor(cursor)
	if err != nil {
		return MCPGrantPage{}, err
	}
	if s.grantStore == nil {
		return MCPGrantPage{}, errMCPGrantStoreMissing
	}
	switch {
	case limit <= 0:
		limit = MCPGrantListDefaultLimit
	case limit > MCPGrantListMaxLimit:
		limit = MCPGrantListMaxLimit
	}
	// One extra row tells whether another page exists without a count.
	items, err := s.grantStore.ListLiveGrants(ctx, userID, s.now(), afterID, limit+1)
	if err != nil {
		return MCPGrantPage{}, err
	}
	page := MCPGrantPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next, err := encodeMCPGrantCursor(page.Items[limit-1].ID)
		if err != nil {
			return MCPGrantPage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}

// RevokeGrantForUser revokes one of the signed-in account's own live grants
// with the audited reason user and the account as actor. Every refusal is
// ErrNotFound: an unknown, malformed, foreign, revoked or expired grant reads
// the same, so the response never reveals another account's grants. A deleted
// account is ErrUnauthenticated.
//
// The owner is read without a lock, so a foreign or unknown grant takes no
// lock and never queues behind another account's transaction. The account is
// then locked before the grant, in the ADR 0023 order.
func (s *MCPService) RevokeGrantForUser(ctx context.Context, userID, grantID string) error {
	// A grant id that is not canonical lowercase text cannot be one of ours:
	// refuse without a database call.
	if !isLowerCanonicalUUID(grantID) {
		return ErrNotFound
	}
	now := s.now()
	return s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		owner, err := tx.FindGrantOwner(ctx, grantID)
		if err != nil {
			return err
		}
		if owner != userID {
			return ErrNotFound
		}
		if err = tx.LockLiveAccount(ctx, userID); errors.Is(err, ErrNotFound) {
			return ErrUnauthenticated
		} else if err != nil {
			return err
		}
		grant, err := tx.LockGrant(ctx, grantID)
		if err != nil {
			return err
		}
		// Re-checked under the locks: the grant may have been revoked, expired
		// or moved while the account lock was waited for.
		if grant.UserID != userID || grant.Revoked || !grant.ExpiresAt.After(now) {
			return ErrNotFound
		}
		return tx.RevokeGrant(ctx, grantID, now, MCPRevocation{Reason: MCPRevokeUser, ActorUserID: userID})
	})
}

// isLowerCanonicalUUID reports whether s is a canonical UUID in lowercase, the
// only spelling the database and the cursor use.
func isLowerCanonicalUUID(s string) bool {
	return ids.IsCanonicalUUID(s) && s == strings.ToLower(s)
}

// encodeMCPGrantCursor wraps the last id of a page. It stores no plaintext UUID
// text, only the 16 bytes behind it.
func encodeMCPGrantCursor(id string) (string, error) {
	if !isLowerCanonicalUUID(id) {
		return "", errors.New("mcp grant cursor id is not a canonical uuid")
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(raw) != 16 {
		return "", errors.New("mcp grant cursor id is not a uuid")
	}
	return mcpGrantCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// decodeMCPGrantCursor returns the id a cursor names, or "" for the first page.
// The cursor must be exactly the form encodeMCPGrantCursor writes: the right
// length and prefix, strict base64url, 16 bytes, and re-encoding it gives the
// same text, so no alternative spelling of one position is accepted.
func decodeMCPGrantCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) != mcpGrantCursorLength || !strings.HasPrefix(cursor, mcpGrantCursorPrefix) {
		return "", ErrMCPInvalidCursor
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor[len(mcpGrantCursorPrefix):])
	if err != nil || len(raw) != 16 {
		return "", ErrMCPInvalidCursor
	}
	if mcpGrantCursorPrefix+base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return "", ErrMCPInvalidCursor
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
