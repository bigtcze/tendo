package household

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

const instanceAdminCenterBudget = 5 * time.Second

var (
	// ErrInstanceAdminRequired means the caller does not currently hold the
	// installation-wide administrator capability. It is answered before any
	// household or account lookup, so it reveals nothing about either.
	ErrInstanceAdminRequired = errors.New("instance_admin_required")
	// ErrAlreadyMember means the account already belongs to the household with
	// a different role; role changes go through the role update instead.
	ErrAlreadyMember = errors.New("already_member")
	// ErrHouseholdRequiresAdmin means the assignment would create a household
	// membership while the household has no admin. The first assignment into an
	// admin-less household must be an admin.
	ErrHouseholdRequiresAdmin = errors.New("household_requires_admin")
)

// AdminHousehold is one household as seen by an instance administrator. It
// carries no household content (items, subjects) and no viewer role.
type AdminHousehold struct {
	ID        string
	Name      string
	Timezone  string
	CreatedAt time.Time
}

// AdminAccount is one household membership as seen by an instance
// administrator. It deliberately omits password-reset facts, which belong to
// the household-admin surface (ADR 0020).
type AdminAccount struct {
	UserID        string
	Login         string
	Role          string
	Version       int64
	SignInMethods SignInMethods
}

// DirectoryAccount is one installation account in the instance-admin account
// directory.
type DirectoryAccount struct {
	UserID string
	Login  string
}

// DirectoryPage asks the identity-owned directory for accounts ordered by
// (login, user ID) strictly after the given position. Search is already
// validated; the adapter matches it case-insensitively as a literal substring
// of the login.
type DirectoryPage struct {
	Search      string
	AfterLogin  string
	AfterUserID string
	Limit       int
}

// AccountDirectory is the identity-owned, read-only account directory.
type AccountDirectory interface {
	ListDirectoryAccounts(context.Context, DirectoryPage) ([]DirectoryAccount, error)
}

// MemberAssignment is one audited instance-admin assignment of an existing
// account into a household.
type MemberAssignment struct {
	HouseholdID, TargetUserID, ActorUserID, Role, ActorAuthority string
	AssignedAt                                                   time.Time
}

// AssignableAccountLocker locks the target account row FOR NO KEY UPDATE and
// returns its login (ErrNotFound when the account does not exist). It is owned
// by the identity module and uses the same lock as invitation acceptance, so
// an assignment and an acceptance for the same account serialize.
type AssignableAccountLocker interface {
	LockAssignableAccount(ctx context.Context, userID string) (string, error)
}

// AssignmentDefaultSetter points the account default at the household only
// when the account has no default yet. An instance admin's assignment never
// moves an existing navigation hint. Owned by the identity module.
type AssignmentDefaultSetter interface {
	SetDefaultHouseholdIfUnset(ctx context.Context, userID, householdID string) error
}

// AssignmentPersonProvisioner provisions the canonical member person of the
// assigned account. Owned by the subject module.
type AssignmentPersonProvisioner interface {
	EnsureMemberPerson(ctx context.Context, householdID, userID, name string) error
}

// AssignmentEffects groups the transaction-scoped effects of an assignment on
// tables other modules own. Like MemberRemovalEffects, every implementation
// writes through the same transaction as the membership insert.
type AssignmentEffects struct {
	Accounts AssignableAccountLocker
	Defaults AssignmentDefaultSetter
	Persons  AssignmentPersonProvisioner
}

// InstanceAdminEffects carries both effect groups an instance-admin
// transaction may need, plus the identity-owned account directory read on the
// same transaction, so the directory page is read under the grant check.
type InstanceAdminEffects struct {
	Removal    MemberRemovalEffects
	Assignment AssignmentEffects
	Directory  AccountDirectory
}

// InstanceAdminTransaction is the persistence surface of the instance-admin
// household center. The household-admin role change and removal cores run on
// it unchanged after the instance-admin gate and the household lock.
type InstanceAdminTransaction interface {
	RoleChangeTransaction
	MemberRemovalTransaction
	// RequireInstanceAdmin locks installation_state FOR SHARE and re-reads the
	// actor's instance_admins grant in the same transaction. A future revoke
	// takes installation_state FOR UPDATE, so a revoked administrator's
	// in-flight command either commits before the revoke or observes it.
	RequireInstanceAdmin(ctx context.Context, actorID string) error
	// LockHousehold locks the household row FOR NO KEY UPDATE by ID alone
	// (the instance-admin grant replaces the membership gate). ErrNotFound when
	// it does not exist.
	LockHousehold(ctx context.Context, householdID string) error
	// HouseholdExists is a non-locking existence read for list operations.
	HouseholdExists(ctx context.Context, householdID string) error
	ListHouseholds(ctx context.Context, afterID string, limit int) ([]AdminHousehold, error)
	ListAccounts(ctx context.Context, householdID, afterUserID string, limit int) ([]AccountRecord, string, error)
	// InsertMembership inserts the membership with the given role and returns
	// its version. A concurrent duplicate surfaces as ErrAlreadyMember.
	InsertMembership(ctx context.Context, householdID, userID, role string) (int64, error)
	RecordMemberAssignment(ctx context.Context, assignment MemberAssignment) error
	// InsertHousehold creates the household and returns it with its version.
	InsertHousehold(ctx context.Context, name, timezone string, createdAt time.Time) (AdminHousehold, int64, error)
	// InsertHouseholdCreation records the actor/key replay row. A concurrent
	// duplicate for the same actor and key surfaces as ErrCreationKeyRaced.
	InsertHouseholdCreation(ctx context.Context, creation HouseholdCreation) error
	// FindHouseholdCreation returns the household and stored fingerprint for an
	// actor/key pair, or ErrNotFound when the key was never used.
	FindHouseholdCreation(ctx context.Context, actorID, key string) (AdminHousehold, int64, []byte, error)
	// GetHousehold reads one household and its version by ID.
	GetHousehold(ctx context.Context, householdID string) (AdminHousehold, int64, error)
}

type InstanceAdminCenterRepository interface {
	WithInstanceAdminTransaction(ctx context.Context, work func(InstanceAdminTransaction, InstanceAdminEffects) error) error
}

// InstanceAdminCenter is the application service behind /api/v1/admin. Every
// operation first re-checks the caller's instance-admin grant inside its own
// transaction, then applies the uniform not-found policy to households and
// accounts. It never reads or returns household content.
type InstanceAdminCenter struct {
	repository InstanceAdminCenterRepository
	logins     LoginLookup
	signIn     SignInMethodsLookup
	clock      func() time.Time
	deletions  *AccountDeletionService
}

func NewInstanceAdminCenter(repository InstanceAdminCenterRepository, logins LoginLookup, signIn SignInMethodsLookup, clock func() time.Time) *InstanceAdminCenter {
	if clock == nil {
		clock = time.Now
	}
	return &InstanceAdminCenter{repository: repository, logins: logins, signIn: signIn, clock: clock}
}

// RequireInstanceAdmin is the transport pre-check: it lets the HTTP adapter
// answer 403 before validating parameters or bodies, so a caller without the
// grant learns nothing from validation errors. Every operation re-checks the
// grant inside its own transaction; this pre-check is never the only gate.
func (s *InstanceAdminCenter) RequireInstanceAdmin(ctx context.Context, actorID string) error {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	if !ids.IsCanonicalUUID(actorID) {
		return ErrInstanceAdminRequired
	}
	return s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		return tx.RequireInstanceAdmin(bounded, actorID)
	})
}

// ListHouseholds returns one page of every household of the installation in
// household ID order.
func (s *InstanceAdminCenter) ListHouseholds(ctx context.Context, actorID, cursor string, limit int) ([]AdminHousehold, string, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var households []AdminHousehold
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		afterID, err := DecodeHouseholdCursor(cursor)
		if err != nil {
			return err
		}
		households, err = tx.ListHouseholds(bounded, afterID, clampLimit(limit)+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if limit = clampLimit(limit); len(households) > limit {
		households = households[:limit]
		next = EncodeHouseholdCursor(households[limit-1].ID)
	}
	return households, next, nil
}

// ListDirectory returns one page of installation accounts, optionally filtered
// by a login substring. It returns identifiers and logins only.
func (s *InstanceAdminCenter) ListDirectory(ctx context.Context, actorID, search, cursor string, limit int) ([]DirectoryAccount, string, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var accounts []DirectoryAccount
	var page DirectoryPage
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, effects InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if effects.Directory == nil {
			return errors.New("account directory unavailable")
		}
		var err error
		if page, err = directoryPage(search, cursor, clampLimit(limit)); err != nil {
			return err
		}
		accounts, err = effects.Directory.ListDirectoryAccounts(bounded, page)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(accounts) > page.Limit-1 {
		accounts = accounts[:page.Limit-1]
		last := accounts[len(accounts)-1]
		next = EncodeDirectoryCursor(last.Login, last.UserID)
	}
	return accounts, next, nil
}

// ListAccounts returns one page of the household's member accounts.
func (s *InstanceAdminCenter) ListAccounts(ctx context.Context, actorID, householdID, cursor string, limit int) ([]AdminAccount, string, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var records []AccountRecord
	var next string
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if !ids.IsCanonicalUUID(householdID) {
			return ErrNotFound
		}
		afterUserID, err := DecodeAccountCursor(cursor)
		if err != nil {
			return err
		}
		if err := tx.HouseholdExists(bounded, householdID); err != nil {
			return err
		}
		records, next, err = tx.ListAccounts(bounded, householdID, afterUserID, clampLimit(limit))
		return err
	})
	if err != nil {
		return nil, "", err
	}
	accounts, err := s.decorate(bounded, records)
	if err != nil {
		return nil, "", err
	}
	return accounts, next, nil
}

// GetAccount returns one membership of the household.
func (s *InstanceAdminCenter) GetAccount(ctx context.Context, actorID, householdID, targetUserID string) (AdminAccount, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var record AccountRecord
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
			return ErrNotFound
		}
		var err error
		record, err = tx.GetAccount(bounded, householdID, targetUserID)
		return err
	})
	if err != nil {
		return AdminAccount{}, err
	}
	return s.decorateOne(bounded, record)
}

// AssignAccount puts an existing account into a household with the given role.
// created is true when a new membership was inserted. Repeating the same
// assignment returns the existing membership without a version bump or audit
// row; an existing membership with a different role is ErrAlreadyMember. An
// instance administrator may assign themselves; the audit row records it.
func (s *InstanceAdminCenter) AssignAccount(ctx context.Context, actorID, householdID, targetUserID, role string) (AdminAccount, bool, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var record AccountRecord
	created := false
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, effects InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if role != RoleAdmin && role != RoleMember {
			return &ValidationError{Field: "role", Code: "invalid_value"}
		}
		if !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
			return ErrNotFound
		}
		// Identity facts are keyed by the stored lowercase spelling.
		targetUserID = strings.ToLower(targetUserID)
		if err := tx.LockHousehold(bounded, householdID); err != nil {
			return err
		}
		var err error
		record, created, err = s.assignLocked(bounded, tx, effects.Assignment, householdID, targetUserID, actorID, role, s.clock().UTC())
		return err
	})
	if err != nil {
		return AdminAccount{}, false, err
	}
	account, err := s.decorateOne(bounded, record)
	return account, created, err
}

// assignLocked runs after the household row is locked. Order (ADR 0020):
//  1. lock the account row FOR NO KEY UPDATE (as invitation acceptance does),
//     so a concurrent acceptance or removal of the same account serializes;
//  2. read the existing membership: same role is an idempotent replay,
//     another role is ErrAlreadyMember;
//  3. refuse a plain-member assignment into a household without an admin;
//  4. insert the membership, provision the member person, set the account
//     default only when it has none, and write the audit row.
func (s *InstanceAdminCenter) assignLocked(ctx context.Context, tx InstanceAdminTransaction, effects AssignmentEffects, householdID, targetUserID, actorID, role string, now time.Time) (AccountRecord, bool, error) {
	if effects.Accounts == nil || effects.Defaults == nil || effects.Persons == nil {
		return AccountRecord{}, false, errors.New("assignment effects unavailable")
	}
	login, err := effects.Accounts.LockAssignableAccount(ctx, targetUserID)
	if err != nil {
		return AccountRecord{}, false, err
	}
	existing, err := tx.GetAccount(ctx, householdID, targetUserID)
	if err == nil {
		if existing.Role == role {
			return existing, false, nil
		}
		return AccountRecord{}, false, ErrAlreadyMember
	}
	if !errors.Is(err, ErrNotFound) {
		return AccountRecord{}, false, err
	}
	if role == RoleMember {
		admins, err := tx.CountAdmins(ctx, householdID)
		if err != nil {
			return AccountRecord{}, false, err
		}
		if admins == 0 {
			return AccountRecord{}, false, ErrHouseholdRequiresAdmin
		}
	}
	version, err := tx.InsertMembership(ctx, householdID, targetUserID, role)
	if err != nil {
		return AccountRecord{}, false, err
	}
	if err := effects.Persons.EnsureMemberPerson(ctx, householdID, targetUserID, login); err != nil {
		return AccountRecord{}, false, err
	}
	if err := effects.Defaults.SetDefaultHouseholdIfUnset(ctx, targetUserID, householdID); err != nil {
		return AccountRecord{}, false, err
	}
	if err := tx.RecordMemberAssignment(ctx, MemberAssignment{
		HouseholdID: householdID, TargetUserID: targetUserID, ActorUserID: actorID,
		Role: role, ActorAuthority: AuthorityInstanceAdmin, AssignedAt: now,
	}); err != nil {
		return AccountRecord{}, false, err
	}
	return AccountRecord{UserID: targetUserID, Role: role, Version: version}, true, nil
}

// ChangeRole changes a membership role under the instance-admin grant through
// the same core as the household-admin path, including last-admin protection
// and the no-op rule.
func (s *InstanceAdminCenter) ChangeRole(ctx context.Context, actorID, householdID, targetUserID, role string, expectedVersion int64) (AdminAccount, error) {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	var record AccountRecord
	err := s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, _ InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if role != RoleAdmin && role != RoleMember {
			return &ValidationError{Field: "role", Code: "invalid_value"}
		}
		if !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
			return ErrNotFound
		}
		if err := tx.LockHousehold(bounded, householdID); err != nil {
			return err
		}
		var err error
		record, err = changeRoleLocked(bounded, tx, householdID, targetUserID, actorID, AuthorityInstanceAdmin, role, expectedVersion, s.clock)
		return err
	})
	if err != nil {
		return AdminAccount{}, err
	}
	return s.decorateOne(bounded, record)
}

// RemoveAccount removes one membership under the instance-admin grant through
// the household removal core (ADR 0019 order), including last-admin protection.
// It never deletes the account itself.
func (s *InstanceAdminCenter) RemoveAccount(ctx context.Context, actorID, householdID, targetUserID string, expectedVersion int64) error {
	bounded, cancel := context.WithTimeout(ctx, instanceAdminCenterBudget)
	defer cancel()
	return s.repository.WithInstanceAdminTransaction(bounded, func(tx InstanceAdminTransaction, effects InstanceAdminEffects) error {
		if err := s.gate(bounded, tx, actorID); err != nil {
			return err
		}
		if !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(targetUserID) {
			return ErrNotFound
		}
		if expectedVersion < 1 {
			return ErrPreconditionFailed
		}
		if err := tx.LockHousehold(bounded, householdID); err != nil {
			return err
		}
		return removeMembershipLocked(bounded, tx, effects.Removal, householdID, targetUserID, actorID, AuthorityInstanceAdmin, expectedVersion, s.clock)
	})
}

// gate is the first step of every operation: a malformed actor is refused as
// lacking the grant, never as not found.
func (s *InstanceAdminCenter) gate(ctx context.Context, tx InstanceAdminTransaction, actorID string) error {
	if !ids.IsCanonicalUUID(actorID) {
		return ErrInstanceAdminRequired
	}
	return tx.RequireInstanceAdmin(ctx, actorID)
}

func (s *InstanceAdminCenter) decorateOne(ctx context.Context, record AccountRecord) (AdminAccount, error) {
	accounts, err := s.decorate(ctx, []AccountRecord{record})
	if err != nil {
		return AdminAccount{}, err
	}
	return accounts[0], nil
}

// decorate joins identity-owned login and sign-in facts and fails closed when
// either is missing, like AccountService.decorate.
func (s *InstanceAdminCenter) decorate(ctx context.Context, records []AccountRecord) ([]AdminAccount, error) {
	if s.logins == nil || s.signIn == nil {
		return nil, errors.New("account identity lookup unavailable")
	}
	userIDs := make([]string, 0, len(records))
	for _, record := range records {
		userIDs = append(userIDs, record.UserID)
	}
	logins, err := s.logins.LoginsByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	methods, err := s.signIn.SignInMethodsByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	accounts := make([]AdminAccount, 0, len(records))
	for _, record := range records {
		login, ok := logins[record.UserID]
		if !ok {
			return nil, errors.New("account login missing")
		}
		method, ok := methods[record.UserID]
		if !ok {
			return nil, errors.New("account sign-in methods missing")
		}
		accounts = append(accounts, AdminAccount{UserID: record.UserID, Login: login, Role: record.Role, Version: record.Version, SignInMethods: method})
	}
	return accounts, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
