package identity

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Slice B1b1 of #457 (ADR 0023 "Slice B1b1: account policy API"). An instance
// administrator reads and changes one account's MCP override; the account owner
// reads and narrows their own settings. Both run at READ COMMITTED with explicit
// row locks in the ADR 0023 order and append one mcp_policy_events row per
// change. A write that changes nothing appends nothing and keeps the version.

var (
	// ErrInstanceAdminRequired is a caller without the live instance-administrator
	// grant, checked again inside the transaction.
	ErrInstanceAdminRequired = errors.New("instance admin required")
	// ErrMCPPolicyPreconditionFailed is an If-Match version that is not the
	// current account policy version.
	ErrMCPPolicyPreconditionFailed = errors.New("mcp policy precondition failed")
)

// MCPPolicyRow is one account's stored policy row with its version. The
// administrator columns are nil while the account follows the installation
// default.
type MCPPolicyRow struct {
	AdminEnabled   *bool
	AdminMaxScopes []string
	UserEnabled    bool
	UserScopes     []string
	Version        int64
}

// MCPPolicyEventKind is the audited policy level, as mcp_policy_events.policy
// accepts it.
type MCPPolicyEventKind string

const (
	MCPPolicyEventAccount MCPPolicyEventKind = "account"
	MCPPolicyEventUser    MCPPolicyEventKind = "user"
)

// MCPPolicyEvent is one append-only audit row: the policy as it stood after the
// change and the human who made it.
type MCPPolicyEvent struct {
	Kind         MCPPolicyEventKind
	ActorUserID  string
	TargetUserID string
	Enabled      *bool
	Scopes       []string
	OccurredAt   time.Time
}

// MCPPolicyTransaction is the transaction-scoped surface of the policy writes.
type MCPPolicyTransaction interface {
	// RequireInstanceAdmin takes the installation row FOR SHARE and checks the
	// actor's live instance-administrator grant.
	RequireInstanceAdmin(ctx context.Context, actorID string) error
	// LockLiveAccount takes the live account FOR SHARE. A deleted, unknown or
	// malformed account is ErrNotFound.
	LockLiveAccount(ctx context.Context, userID string) error
	// LockAccountPolicy inserts the default row when it is missing, then locks
	// the row FOR UPDATE. inserted reports that the row was missing.
	LockAccountPolicy(ctx context.Context, userID string) (row MCPPolicyRow, inserted bool, err error)
	// UpdateAccountPolicy stores the row's administrator and user columns and
	// its version.
	UpdateAccountPolicy(ctx context.Context, userID string, row MCPPolicyRow, now time.Time) error
	AppendPolicyEvent(ctx context.Context, event MCPPolicyEvent) error
	// ReadLiveAccountPolicy returns the stored policy and the row version of a
	// live account without a lock. A deleted, unknown or malformed account is
	// ErrNotFound. A missing account row is AccountFound false with version 0.
	ReadLiveAccountPolicy(ctx context.Context, userID string) (MCPPolicyRecord, int64, error)
}

// MCPPolicyRepository opens policy transactions at READ COMMITTED.
type MCPPolicyRepository interface {
	WithMCPPolicyTransaction(ctx context.Context, work func(MCPPolicyTransaction) error) error
}

// MCPAccountPolicy is the administrator's view of one account. Effective is
// what an app may use now: empty while TENDO_MCP_ENABLED, the installation, the
// account or the user is off.
type MCPAccountPolicy struct {
	UserID         string
	AdminEnabled   *bool
	AdminMaxScopes []string
	UserEnabled    bool
	UserScopes     []string
	Effective      []string
	Version        int64
}

// MCPSettings is the account owner's view of their own settings. Maximum is what
// the operator, the installation and the administrator allow; Effective is the
// maximum narrowed by the owner's settings.
type MCPSettings struct {
	Enabled   bool
	Scopes    []string
	Maximum   []string
	Effective []string
	Version   int64
}

// MCPAccountPolicyChange is the administrator's full replacement of the
// override. Nil returns a term to the installation default.
type MCPAccountPolicyChange struct {
	Enabled   *bool
	MaxScopes []string
}

// MCPSettingsChange is the owner's full replacement of their own settings.
type MCPSettingsChange struct {
	Enabled bool
	Scopes  []string
}

// MCPPolicyService serves the account policy API. It is independent of the
// OAuth service: the routes work whether or not TENDO_MCP_ENABLED is set, and
// the flag only empties the reported effective scopes.
type MCPPolicyService struct {
	repository MCPPolicyRepository
	mcpEnabled bool
	clock      func() time.Time
}

// NewMCPPolicyService builds the service. mcpEnabled is TENDO_MCP_ENABLED.
func NewMCPPolicyService(repository MCPPolicyRepository, mcpEnabled bool, clock func() time.Time) (*MCPPolicyService, error) {
	if repository == nil {
		return nil, errors.New("mcp policy repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &MCPPolicyService{repository: repository, mcpEnabled: mcpEnabled, clock: clock}, nil
}

func (s *MCPPolicyService) now() time.Time { return s.clock().UTC().Truncate(time.Microsecond) }

// RequireInstanceAdmin is the transport pre-check that orders the 403 before
// validation and lookups. Every command re-checks the grant in its own
// transaction.
func (s *MCPPolicyService) RequireInstanceAdmin(ctx context.Context, actorID string) error {
	return s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		return tx.RequireInstanceAdmin(ctx, actorID)
	})
}

// AccountPolicy returns one live account's policy to an instance administrator.
func (s *MCPPolicyService) AccountPolicy(ctx context.Context, actorID, userID string) (MCPAccountPolicy, error) {
	var out MCPAccountPolicy
	err := s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := requireAdminFor(ctx, tx, actorID, userID); err != nil {
			return err
		}
		record, version, err := tx.ReadLiveAccountPolicy(ctx, userID)
		if err != nil {
			return err
		}
		out = s.accountView(userID, record, version)
		return nil
	})
	return out, err
}

// UpdateAccountPolicy replaces one live account's override for an instance
// administrator, appending one account event when anything changed.
func (s *MCPPolicyService) UpdateAccountPolicy(ctx context.Context, actorID, userID string, change MCPAccountPolicyChange, expected int64) (MCPAccountPolicy, error) {
	var maxScopes []string
	if change.MaxScopes != nil {
		scopes, err := canonicalMCPScopes(change.MaxScopes, "maxScopes")
		if err != nil {
			return MCPAccountPolicy{}, err
		}
		maxScopes = scopes
	}
	now := s.now()
	var out MCPAccountPolicy
	err := s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := requireAdminFor(ctx, tx, actorID, userID); err != nil {
			return err
		}
		if err := tx.LockLiveAccount(ctx, userID); err != nil {
			return err
		}
		row, inserted, err := tx.LockAccountPolicy(ctx, userID)
		if err != nil {
			return err
		}
		if row.Version != expected {
			return ErrMCPPolicyPreconditionFailed
		}
		if inserted || !sameBool(row.AdminEnabled, change.Enabled) || !sameOptionalScopes(row.AdminMaxScopes, maxScopes) {
			row.AdminEnabled, row.AdminMaxScopes = change.Enabled, maxScopes
			if row, err = writePolicy(ctx, tx, userID, row, MCPPolicyEvent{
				Kind: MCPPolicyEventAccount, ActorUserID: actorID, TargetUserID: userID,
				Enabled: change.Enabled, Scopes: maxScopes, OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		record, _, err := tx.ReadLiveAccountPolicy(ctx, userID)
		if err != nil {
			return err
		}
		out = s.accountView(userID, record, row.Version)
		return nil
	})
	return out, err
}

// Settings returns the signed-in account's own settings. A deleted account is
// ErrUnauthenticated.
func (s *MCPPolicyService) Settings(ctx context.Context, userID string) (MCPSettings, error) {
	var out MCPSettings
	err := s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		record, version, err := tx.ReadLiveAccountPolicy(ctx, userID)
		if errors.Is(err, ErrNotFound) {
			return ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		out = s.settingsView(record, version)
		return nil
	})
	return out, err
}

// UpdateSettings replaces the signed-in account's own settings, appending one
// user event when anything changed. A deleted account is ErrUnauthenticated.
func (s *MCPPolicyService) UpdateSettings(ctx context.Context, userID string, change MCPSettingsChange, expected int64) (MCPSettings, error) {
	scopes, err := canonicalMCPScopes(change.Scopes, "scopes")
	if err != nil {
		return MCPSettings{}, err
	}
	now := s.now()
	var out MCPSettings
	err = s.repository.WithMCPPolicyTransaction(ctx, func(tx MCPPolicyTransaction) error {
		if err := tx.LockLiveAccount(ctx, userID); errors.Is(err, ErrNotFound) {
			return ErrUnauthenticated
		} else if err != nil {
			return err
		}
		row, inserted, err := tx.LockAccountPolicy(ctx, userID)
		if err != nil {
			return err
		}
		if row.Version != expected {
			return ErrMCPPolicyPreconditionFailed
		}
		if inserted || row.UserEnabled != change.Enabled || !slices.Equal(sortedMCPScopes(row.UserScopes), scopes) {
			row.UserEnabled, row.UserScopes = change.Enabled, scopes
			enabled := change.Enabled
			if row, err = writePolicy(ctx, tx, userID, row, MCPPolicyEvent{
				Kind: MCPPolicyEventUser, ActorUserID: userID, TargetUserID: userID,
				Enabled: &enabled, Scopes: scopes, OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		record, _, err := tx.ReadLiveAccountPolicy(ctx, userID)
		if err != nil {
			return err
		}
		out = s.settingsView(record, row.Version)
		return nil
	})
	return out, err
}

// requireAdminFor checks the actor's grant first, so a caller without it
// learns nothing about the target. Only a canonical lowercase id can name an
// account; any other spelling is ErrNotFound without a lookup.
func requireAdminFor(ctx context.Context, tx MCPPolicyTransaction, actorID, userID string) error {
	if err := tx.RequireInstanceAdmin(ctx, actorID); err != nil {
		return err
	}
	if !isLowerCanonicalUUID(userID) {
		return ErrNotFound
	}
	return nil
}

// writePolicy stores a changed row under its lock with the next version and
// appends its audit event. It returns the row as stored.
func writePolicy(ctx context.Context, tx MCPPolicyTransaction, userID string, row MCPPolicyRow, event MCPPolicyEvent) (MCPPolicyRow, error) {
	row.Version++
	if err := tx.UpdateAccountPolicy(ctx, userID, row, event.OccurredAt); err != nil {
		return MCPPolicyRow{}, err
	}
	if err := tx.AppendPolicyEvent(ctx, event); err != nil {
		return MCPPolicyRow{}, err
	}
	return row, nil
}

// allowed is the policy's allowed set, empty while TENDO_MCP_ENABLED is off.
func (s *MCPPolicyService) allowed(record MCPPolicyRecord) []string {
	if !s.mcpEnabled {
		return []string{}
	}
	return nonNil(record.Allowed())
}

// accountView reports a missing account row with the values its repair would
// insert and version 1, so If-Match "1" repairs it. Its effective set is empty:
// a missing row is inactive.
func (s *MCPPolicyService) accountView(userID string, record MCPPolicyRecord, version int64) MCPAccountPolicy {
	if !record.AccountFound {
		return MCPAccountPolicy{
			UserID: userID, UserEnabled: true, UserScopes: allMCPScopes(),
			Effective: []string{}, Version: 1,
		}
	}
	var maxScopes []string
	if record.AdminMaxScopes != nil {
		maxScopes = sortedMCPScopes(record.AdminMaxScopes)
	}
	return MCPAccountPolicy{
		UserID: userID, AdminEnabled: record.AdminEnabled, AdminMaxScopes: maxScopes,
		UserEnabled: record.UserEnabled, UserScopes: sortedMCPScopes(record.UserScopes),
		Effective: s.allowed(record), Version: version,
	}
}

// settingsView is the owner's view. The maximum ignores the owner's own terms.
func (s *MCPPolicyService) settingsView(record MCPPolicyRecord, version int64) MCPSettings {
	if !record.AccountFound {
		return MCPSettings{Enabled: true, Scopes: allMCPScopes(), Maximum: []string{}, Effective: []string{}, Version: 1}
	}
	unrestricted := record
	unrestricted.UserEnabled, unrestricted.UserScopes = true, allMCPScopes()
	return MCPSettings{
		Enabled: record.UserEnabled, Scopes: sortedMCPScopes(record.UserScopes),
		Maximum: s.allowed(unrestricted), Effective: s.allowed(record), Version: version,
	}
}

// canonicalMCPScopes validates a submitted scope list: at least one, every value
// known, none repeated. It returns the list sorted.
func canonicalMCPScopes(scopes []string, field string) ([]string, error) {
	invalid := &ValidationError{Field: field, Code: "invalid_value"}
	if len(scopes) == 0 || len(scopes) > 2 {
		return nil, invalid
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope] || (scope != MCPScopeRead && scope != MCPScopeWrite) {
			return nil, invalid
		}
		seen[scope] = true
	}
	return sortedMCPScopes(scopes), nil
}

// sortedMCPScopes returns the known scopes in scopes, in canonical order.
func sortedMCPScopes(scopes []string) []string {
	return nonNil(intersectMCPScopes(scopes, allMCPScopes()))
}

func allMCPScopes() []string { return []string{MCPScopeRead, MCPScopeWrite} }

func nonNil(scopes []string) []string {
	if scopes == nil {
		return []string{}
	}
	return scopes
}

func sameBool(a, b *bool) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func sameOptionalScopes(stored, next []string) bool {
	if (stored == nil) != (next == nil) {
		return false
	}
	return stored == nil || slices.Equal(sortedMCPScopes(stored), next)
}
