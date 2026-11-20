package identity

// MCPRevokeReason is the audited cause of an MCP grant revocation. The values
// are exactly the ones mcp_grants.revoke_reason and mcp_grant_events.reason
// accept (migration 028, ADR 0023).
type MCPRevokeReason string

const (
	MCPRevokeUser            MCPRevokeReason = "user"
	MCPRevokePasswordChange  MCPRevokeReason = "password_change"
	MCPRevokeAdminReset      MCPRevokeReason = "admin_reset"
	MCPRevokeCLIReset        MCPRevokeReason = "cli_reset"
	MCPRevokeAccountDeletion MCPRevokeReason = "account_deletion"
	MCPRevokeReplay          MCPRevokeReason = "replay"
	MCPRevokeCodeReuse       MCPRevokeReason = "code_reuse"
)

// Valid reports whether r is one of the values the database accepts.
func (r MCPRevokeReason) Valid() bool {
	switch r {
	case MCPRevokeUser, MCPRevokePasswordChange, MCPRevokeAdminReset, MCPRevokeCLIReset,
		MCPRevokeAccountDeletion, MCPRevokeReplay, MCPRevokeCodeReuse:
		return true
	}
	return false
}

// MCPRevocation names why every MCP grant of an account is being revoked and
// which human caused it. ActorUserID is empty when no signed-in human did: the
// operator CLI reset and the automatic replay and code-reuse revocations.
type MCPRevocation struct {
	Reason      MCPRevokeReason
	ActorUserID string
}
