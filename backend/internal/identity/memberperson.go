package identity

import "context"

// MemberPersonProvisioner creates the canonical person subject belonging to a
// household membership. It is a narrow consumer interface so identity does not
// import the subject module; the transaction-bound implementation is wired in
// the process composition root.
type MemberPersonProvisioner interface {
	EnsureMemberPerson(ctx context.Context, householdID, userID, name string) error
}
