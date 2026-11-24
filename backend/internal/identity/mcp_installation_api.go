package identity

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Slice B1b2 of #457 (ADR 0023 "Slice B1b2: installation switch, policy API and
// audit list"). The installation switch gates the MCP surface exactly as
// TENDO_MCP_ENABLED does. An instance administrator reads and changes the
// installation terms, and reads the policy audit list, newest first.

var (
	// ErrMCPInstallationMissing is a missing mcp_installation_policy row. It is
	// never defaulted: the gate fails closed and the API answers unavailable.
	ErrMCPInstallationMissing = errors.New("mcp installation policy is missing")
	// errMCPInstallationUnreadable is an enabled gate with no way to read the
	// installation switch. It fails closed.
	errMCPInstallationUnreadable = errors.New("mcp installation switch is not readable")
)

const (
	// MCPPolicyEventListDefaultLimit is the audit page size when none is named.
	MCPPolicyEventListDefaultLimit = 50
	// MCPPolicyEventListMaxLimit bounds one audit page.
	MCPPolicyEventListMaxLimit = 100
	// mcpPolicyEventCursorPrefix versions the opaque audit cursor.
	mcpPolicyEventCursorPrefix = "p1"
	// mcpPolicyEventCursorLength is the exact cursor length: the prefix plus the
	// unpadded base64url encoding of 16 UUID bytes.
	mcpPolicyEventCursorLength = len(mcpPolicyEventCursorPrefix) + 22
)

// MCPInstallationStore reads the installation switch without a lock. The gate
// uses it on every MCP route. Repositories that do not provide it make an
// enabled gate fail closed.
type MCPInstallationStore interface {
	// ReadInstallationSwitch returns the installation's enabled flag. A missing
	// row is ErrMCPInstallationMissing.
	ReadInstallationSwitch(ctx context.Context) (bool, error)
}

// MCPInstallationRow is the stored installation policy with its version.
type MCPInstallationRow struct {
	Enabled               bool
	DefaultAccountEnabled bool
	DefaultMaxScopes      []string
	Version               int64
}

// MCPInstallationChange is the administrator's full replacement of the
// installation terms.
type MCPInstallationChange struct {
	Enabled               bool
	DefaultAccountEnabled bool
	DefaultMaxScopes      []string
}

// MCPInstallationView is the administrator's view. OperatorEnabled is
// TENDO_MCP_ENABLED and is read-only here.
type MCPInstallationView struct {
	Enabled               bool
	OperatorEnabled       bool
	DefaultAccountEnabled bool
	DefaultMaxScopes      []string
	Version               int64
}

// MCPPolicyEventList is one newest-first page of the audit list. NextCursor is
// empty on the last page.
type MCPPolicyEventList struct {
	Items      []MCPPolicyEvent
	NextCursor string
}

// Available reports whether the MCP surface answers now: TENDO_MCP_ENABLED and
// the installation switch. With the flag off nothing is read. A failed read or
// a missing row is an error, and the caller fails closed in its own shape.
func (s *MCPService) Available(ctx context.Context) (bool, error) {
	if !s.enabled {
		return false, nil
	}
	if s.installation == nil {
		return false, errMCPInstallationUnreadable
	}
	if s.dbTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.dbTimeout)
		defer cancel()
	}
	return s.installation.ReadInstallationSwitch(ctx)
}

// InstallationPolicy returns the installation terms to an instance
// administrator. A missing row is ErrMCPInstallationMissing.
func (s *MCPPolicyService) InstallationPolicy(ctx context.Context, actorID string) (MCPInstallationView, error) {
	var out MCPInstallationView
	err := s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := tx.RequireInstanceAdmin(ctx, actorID); err != nil {
			return err
		}
		row, err := tx.ReadInstallationPolicy(ctx)
		if err != nil {
			return err
		}
		out = s.installationView(row)
		return nil
	})
	return out, err
}

// UpdateInstallationPolicy replaces the installation terms for an instance
// administrator. It appends one installation event when anything changed. An
// unchanged write keeps the version and appends nothing.
func (s *MCPPolicyService) UpdateInstallationPolicy(ctx context.Context, actorID string, change MCPInstallationChange, expected int64) (MCPInstallationView, error) {
	scopes, err := canonicalMCPScopes(change.DefaultMaxScopes, "defaultMaxScopes")
	if err != nil {
		return MCPInstallationView{}, err
	}
	now := s.now()
	var out MCPInstallationView
	err = s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := tx.RequireInstanceAdmin(ctx, actorID); err != nil {
			return err
		}
		row, err := tx.LockInstallationPolicy(ctx)
		if err != nil {
			return err
		}
		if row.Version != expected {
			return ErrMCPPolicyPreconditionFailed
		}
		if row.Enabled != change.Enabled || row.DefaultAccountEnabled != change.DefaultAccountEnabled ||
			!slices.Equal(sortedMCPScopes(row.DefaultMaxScopes), scopes) {
			row.Enabled, row.DefaultAccountEnabled, row.DefaultMaxScopes = change.Enabled, change.DefaultAccountEnabled, scopes
			row.Version++
			if err := tx.UpdateInstallationPolicy(ctx, row, now); err != nil {
				return err
			}
			enabled, defaultOn := change.Enabled, change.DefaultAccountEnabled
			if err := tx.AppendPolicyEvent(ctx, MCPPolicyEvent{
				Kind: MCPPolicyEventInstallation, ActorUserID: actorID,
				Enabled: &enabled, DefaultAccountEnabled: &defaultOn, Scopes: scopes, OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		out = s.installationView(row)
		return nil
	})
	return out, err
}

// PolicyEvents returns one page of the policy audit list, newest first, for an
// instance administrator. The cursor is decoded before any transaction, so an
// invalid cursor never reaches the database.
func (s *MCPPolicyService) PolicyEvents(ctx context.Context, actorID string, limit int, cursor string) (MCPPolicyEventList, error) {
	after, err := decodeMCPPolicyEventCursor(cursor)
	if err != nil {
		return MCPPolicyEventList{}, err
	}
	switch {
	case limit <= 0:
		limit = MCPPolicyEventListDefaultLimit
	case limit > MCPPolicyEventListMaxLimit:
		limit = MCPPolicyEventListMaxLimit
	}
	var out MCPPolicyEventList
	err = s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := tx.RequireInstanceAdmin(ctx, actorID); err != nil {
			return err
		}
		// One extra row tells whether another page exists without a count.
		events, err := tx.ListPolicyEvents(ctx, after, limit+1)
		if err != nil {
			return err
		}
		if events == nil {
			events = []MCPPolicyEvent{}
		}
		if len(events) > limit {
			events = events[:limit]
			next, err := encodeMCPPolicyEventCursor(events[limit-1].ID)
			if err != nil {
				return err
			}
			out.NextCursor = next
		}
		out.Items = events
		return nil
	})
	return out, err
}

func (s *MCPPolicyService) installationView(row MCPInstallationRow) MCPInstallationView {
	return MCPInstallationView{
		Enabled: row.Enabled, OperatorEnabled: s.mcpEnabled, DefaultAccountEnabled: row.DefaultAccountEnabled,
		DefaultMaxScopes: sortedMCPScopes(row.DefaultMaxScopes), Version: row.Version,
	}
}

// encodeMCPPolicyEventCursor writes the cursor for the audit row id. It mirrors
// encodeMCPGrantCursor with its own prefix, so a grant cursor never pages the
// audit list.
func encodeMCPPolicyEventCursor(id string) (string, error) {
	if !isLowerCanonicalUUID(id) {
		return "", errors.New("mcp policy event cursor id is not a canonical uuid")
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(raw) != 16 {
		return "", errors.New("mcp policy event cursor id is not a uuid")
	}
	return mcpPolicyEventCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// decodeMCPPolicyEventCursor returns the id a cursor names, or "" for the first
// page. Only the exact form encodeMCPPolicyEventCursor writes is accepted.
func decodeMCPPolicyEventCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) != mcpPolicyEventCursorLength || !strings.HasPrefix(cursor, mcpPolicyEventCursorPrefix) {
		return "", ErrMCPInvalidCursor
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor[len(mcpPolicyEventCursorPrefix):])
	if err != nil || len(raw) != 16 {
		return "", ErrMCPInvalidCursor
	}
	if mcpPolicyEventCursorPrefix+base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return "", ErrMCPInvalidCursor
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
