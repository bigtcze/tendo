package household

import (
	"context"
	"errors"
	"time"
)

const instanceAdminOperationBudget = 5 * time.Second

var (
	// ErrSetupIncomplete refuses a grant before first-run setup completed. Setup
	// itself grants the installation's first administrator in its own
	// transaction.
	ErrSetupIncomplete = errors.New("setup is not complete")
	// ErrAccountNotFound reports that no account has the requested exact login.
	ErrAccountNotFound = errors.New("account not found")
)

// InstanceAdminGrant is the outcome of a grant command.
type InstanceAdminGrant struct {
	UserID string
	// Granted is false when the account already held the capability.
	Granted bool
}

// InstanceAdminRepository is the persistence port for the installation-wide
// administrator capability. The capability is installation-scoped and
// independent of household roles, so it is not part of the household
// membership ports.
type InstanceAdminRepository interface {
	// GrantInstanceAdmin grants the capability to the account with the exact
	// login. It locks the installation row, refuses while setup is required and
	// is idempotent for an account that already holds the grant.
	GrantInstanceAdmin(context.Context, string) (InstanceAdminGrant, error)
	// SetupCompleteWithoutInstanceAdmin reports whether the installation is set
	// up yet has no instance-administrator grant at all.
	SetupCompleteWithoutInstanceAdmin(context.Context) (bool, error)
}

// InstanceAdmins is the application service for the installation-wide
// administrator capability. The recovery CLI and any later admin surface call
// it; it owns no SQL.
type InstanceAdmins struct {
	repository InstanceAdminRepository
}

func NewInstanceAdmins(repository InstanceAdminRepository) *InstanceAdmins {
	return &InstanceAdmins{repository: repository}
}

// Grant records the capability for the account with the exact login.
func (s *InstanceAdmins) Grant(ctx context.Context, login string) (InstanceAdminGrant, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminOperationBudget)
	defer cancel()
	return s.repository.GrantInstanceAdmin(bounded, login)
}

// SetupCompleteWithoutInstanceAdmin decides whether startup must warn that no
// administrator is configured.
func (s *InstanceAdmins) SetupCompleteWithoutInstanceAdmin(ctx context.Context) (bool, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminOperationBudget)
	defer cancel()
	return s.repository.SetupCompleteWithoutInstanceAdmin(bounded)
}
