package household

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

const accountRepositoryBudget = 5 * time.Second

var (
	// ErrPreconditionFailed means the supplied membership version is stale.
	ErrPreconditionFailed = errors.New("precondition failed")
	// ErrLastAdmin means the change would leave the household without an admin.
	ErrLastAdmin = errors.New("last admin")
)

// SignInMethods describes how an account can sign in. It never contains
// credentials.
type SignInMethods struct {
	Password bool
	OIDC     bool
}

// SignInMethodsLookup is an identity-owned batched lookup. A user ID missing
// from the result is an error, never a default of "no methods".
type SignInMethodsLookup interface {
	SignInMethodsByUserIDs(context.Context, []string) (map[string]SignInMethods, error)
}

// AccountRecord is the household-owned part of a member account.
type AccountRecord struct {
	UserID  string
	Role    string
	Version int64
}

// Account adds identity-owned login and sign-in facts to an AccountRecord.
type Account struct {
	UserID        string
	Login         string
	Role          string
	Version       int64
	SignInMethods SignInMethods
}

// RoleChange is one audited membership role transition.
type RoleChange struct {
	HouseholdID, TargetUserID, ActorUserID, PreviousRole, NewRole string
	ChangedAt                                                     time.Time
}

// AccountTransaction is the transaction-scoped surface for admin account
// reads and writes. It exposes the shared AdminGate so every admin operation
// uses the same lock protocol and keeps its reads inside the same transaction.
type AccountTransaction interface {
	AdminGate
	GetAccount(context.Context, string, string) (AccountRecord, error)
	ListAccounts(context.Context, string, string, int) ([]AccountRecord, string, error)
	UpdateAccountRole(context.Context, string, string, string) (int64, error)
	CountAdmins(context.Context, string) (int, error)
	RecordRoleChange(context.Context, RoleChange) error
}

type AccountRepository interface {
	WithAccountTransaction(context.Context, func(AccountTransaction) error) error
}

// AccountService is the admin-facing application service for household member
// accounts. Sign-in facts stay identity-owned and are injected as lookups.
type AccountService struct {
	repository AccountRepository
	logins     LoginLookup
	signIn     SignInMethodsLookup
	clock      func() time.Time
}

func NewAccountService(repository AccountRepository, logins LoginLookup, signIn SignInMethodsLookup, clock func() time.Time) *AccountService {
	if clock == nil {
		clock = time.Now
	}
	return &AccountService{repository: repository, logins: logins, signIn: signIn, clock: clock}
}

func EncodeAccountCursor(id string) string {
	return "a1" + base64.RawURLEncoding.EncodeToString([]byte(id))
}
func DecodeAccountCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) < 3 || cursor[:2] != "a1" {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(cursor[2:])
	if e != nil || len(b) != 36 || !ids.IsCanonicalUUID(string(b)) {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	return string(b), nil
}

// ListAccounts returns every member account of the household for an admin.
func (s *AccountService) ListAccounts(ctx context.Context, actorID, householdID, cursor string, limit int) ([]Account, string, error) {
	bounded, cancel := context.WithTimeout(ctx, accountRepositoryBudget)
	defer cancel()
	decodedCursor, e := DecodeAccountCursor(cursor)
	if e != nil {
		return nil, "", e
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var records []AccountRecord
	var next string
	err := s.repository.WithAccountTransaction(bounded, func(tx AccountTransaction) error {
		if e := RequireAdmin(bounded, tx, actorID, householdID); e != nil {
			return e
		}
		var e error
		records, next, e = tx.ListAccounts(bounded, householdID, decodedCursor, limit)
		return e
	})
	if err != nil {
		return nil, "", err
	}
	accounts, e := s.decorate(bounded, records)
	if e != nil {
		return nil, "", e
	}
	return accounts, next, nil
}

// GetAccount returns one member account for an admin.
func (s *AccountService) GetAccount(ctx context.Context, actorID, householdID, targetUserID string) (Account, error) {
	bounded, cancel := context.WithTimeout(ctx, accountRepositoryBudget)
	defer cancel()
	if !ids.IsCanonicalUUID(targetUserID) {
		return Account{}, ErrNotFound
	}
	var record AccountRecord
	err := s.repository.WithAccountTransaction(bounded, func(tx AccountTransaction) error {
		if e := RequireAdmin(bounded, tx, actorID, householdID); e != nil {
			return e
		}
		var e error
		record, e = tx.GetAccount(bounded, householdID, targetUserID)
		return e
	})
	if err != nil {
		return Account{}, err
	}
	accounts, e := s.decorate(bounded, []AccountRecord{record})
	if e != nil {
		return Account{}, e
	}
	return accounts[0], nil
}

// ChangeRole changes a member role with optimistic concurrency and an audit
// row. A no-op change returns the current representation without a version bump
// or audit row. Removing the last admin is rejected and rolled back.
func (s *AccountService) ChangeRole(ctx context.Context, actorID, householdID, targetUserID, role string, expectedVersion int64) (Account, error) {
	bounded, cancel := context.WithTimeout(ctx, accountRepositoryBudget)
	defer cancel()
	if role != RoleAdmin && role != RoleMember {
		return Account{}, &ValidationError{Field: "role", Code: "invalid_value"}
	}
	if !ids.IsCanonicalUUID(targetUserID) {
		return Account{}, ErrNotFound
	}
	var record AccountRecord
	err := s.repository.WithAccountTransaction(bounded, func(tx AccountTransaction) error {
		if e := RequireAdmin(bounded, tx, actorID, householdID); e != nil {
			return e
		}
		current, e := tx.GetAccount(bounded, householdID, targetUserID)
		if e != nil {
			return e
		}
		if current.Version != expectedVersion {
			return ErrPreconditionFailed
		}
		if current.Role == role {
			record = current
			return nil
		}
		version, e := tx.UpdateAccountRole(bounded, householdID, targetUserID, role)
		if e != nil {
			return e
		}
		admins, e := tx.CountAdmins(bounded, householdID)
		if e != nil {
			return e
		}
		if admins == 0 {
			return ErrLastAdmin
		}
		if e = tx.RecordRoleChange(bounded, RoleChange{
			HouseholdID: householdID, TargetUserID: targetUserID, ActorUserID: actorID,
			PreviousRole: current.Role, NewRole: role, ChangedAt: s.clock().UTC(),
		}); e != nil {
			return e
		}
		record = AccountRecord{UserID: targetUserID, Role: role, Version: version}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	accounts, err := s.decorate(bounded, []AccountRecord{record})
	if err != nil {
		return Account{}, err
	}
	return accounts[0], nil
}

// decorate joins identity-owned login and sign-in facts. Missing facts fail
// closed instead of being reported as false.
func (s *AccountService) decorate(ctx context.Context, records []AccountRecord) ([]Account, error) {
	if s.logins == nil || s.signIn == nil {
		return nil, errors.New("account identity lookup unavailable")
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.UserID)
	}
	logins, err := s.logins.LoginsByUserIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	methods, err := s.signIn.SignInMethodsByUserIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(records))
	for _, record := range records {
		login, ok := logins[record.UserID]
		if !ok {
			return nil, errors.New("account login missing")
		}
		method, ok := methods[record.UserID]
		if !ok {
			return nil, errors.New("account sign-in methods missing")
		}
		accounts = append(accounts, Account{UserID: record.UserID, Login: login, Role: record.Role, Version: record.Version, SignInMethods: method})
	}
	return accounts, nil
}
