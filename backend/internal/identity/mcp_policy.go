package identity

import (
	"errors"
	"slices"
)

// Slice B1a of #457 (ADR 0023 "Slice B1a: access policy"). The effective MCP
// rights of a request are
//
//	TENDO_MCP_ENABLED AND installation enabled AND account enabled AND user
//	enabled AND (account maximum ∩ user scopes ∩ grant scopes)
//
// read from the database on every verification, exchange, refresh and consent
// decision, never cached. Household access is the live membership the canonical
// services check on every tool call; the policy adds no household term.

// ErrMCPAccessDenied is a direct code issuance the policy does not allow: the
// account or user is switched off, or no requested scope is allowed. The
// consent decision answers the same case with an access_denied redirect.
var ErrMCPAccessDenied = errors.New("mcp access denied")

// MCPActiveAccess is a live access token's grant together with the policy read
// in the same statement.
type MCPActiveAccess struct {
	Grant  MCPGrantRecord
	Policy MCPPolicyRecord
}

// MCPPolicyRecord is the stored policy of one account as read, before any
// default is applied. A missing installation row or a missing account row is
// inactive, never a default.
type MCPPolicyRecord struct {
	InstallationFound     bool
	InstallationEnabled   bool
	DefaultAccountEnabled bool
	DefaultMaxScopes      []string

	AccountFound bool
	// AdminEnabled is nil while the account follows DefaultAccountEnabled.
	AdminEnabled *bool
	// AdminMaxScopes is nil while the account follows DefaultMaxScopes.
	AdminMaxScopes []string
	UserEnabled    bool
	UserScopes     []string
}

// InstallationActive reports whether MCP is switched on for the whole
// installation. While it is off every MCP route answers as if MCP were
// disabled by the operator.
func (p MCPPolicyRecord) InstallationActive() bool {
	return p.InstallationFound && p.InstallationEnabled
}

// Allowed returns the scopes the policy allows for the account, sorted, or nil
// when the installation, the account or the user is switched off. It is the
// account maximum intersected with the user's own scopes; the user's settings
// can only narrow it.
func (p MCPPolicyRecord) Allowed() []string {
	if !p.InstallationActive() || !p.AccountFound || !p.UserEnabled {
		return nil
	}
	enabled := p.DefaultAccountEnabled
	if p.AdminEnabled != nil {
		enabled = *p.AdminEnabled
	}
	if !enabled {
		return nil
	}
	maximum := p.DefaultMaxScopes
	if p.AdminMaxScopes != nil {
		maximum = p.AdminMaxScopes
	}
	return intersectMCPScopes(maximum, p.UserScopes)
}

// Effective returns the scopes a grant may use now: the allowed scopes
// intersected with the scopes the grant was consented for. Empty means none.
func (p MCPPolicyRecord) Effective(grantScopes []string) []string {
	return intersectMCPScopes(p.Allowed(), grantScopes)
}

// intersectMCPScopes returns the known Tendo scopes present in both sets,
// sorted and without duplicates. Unknown values never survive, so a corrupted
// row can only narrow access.
func intersectMCPScopes(a, b []string) []string {
	var out []string
	for _, scope := range []string{MCPScopeRead, MCPScopeWrite} {
		if slices.Contains(a, scope) && slices.Contains(b, scope) {
			out = append(out, scope)
		}
	}
	return out
}
