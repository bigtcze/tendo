package household

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	accountActor  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	accountHouse  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	accountTarget = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
)

type accountFake struct {
	actorRole string
	gateErr   error
	records   map[string]AccountRecord
	order     []string
	admins    int
	audits    []RoleChange
	updates   []string
}

func newAccountFake() *accountFake {
	return &accountFake{actorRole: RoleAdmin, records: map[string]AccountRecord{}, admins: 1}
}
func (f *accountFake) LockHouseholdForMembership(context.Context, string, string) error {
	return f.gateErr
}
func (f *accountFake) GetMembership(context.Context, string, string) (string, error) {
	if f.gateErr != nil {
		return "", f.gateErr
	}
	return f.actorRole, nil
}
func (f *accountFake) GetAccount(_ context.Context, _, userID string) (AccountRecord, error) {
	record, ok := f.records[userID]
	if !ok {
		return AccountRecord{}, ErrNotFound
	}
	return record, nil
}
func (f *accountFake) ListAccounts(context.Context, string, string, int) ([]AccountRecord, string, error) {
	out := make([]AccountRecord, 0, len(f.order))
	for _, userID := range f.order {
		out = append(out, f.records[userID])
	}
	return out, "", nil
}
func (f *accountFake) WithAccountTransaction(_ context.Context, work func(AccountTransaction) error) error {
	records := make(map[string]AccountRecord, len(f.records))
	for key, value := range f.records {
		records[key] = value
	}
	audits, updates := f.audits, f.updates
	err := work((*accountTxFake)(f))
	if err != nil {
		f.records, f.audits, f.updates = records, audits, updates
	}
	return err
}

type accountTxFake accountFake

func (f *accountTxFake) LockHouseholdForMembership(context.Context, string, string) error {
	return f.gateErr
}
func (f *accountTxFake) GetMembership(context.Context, string, string) (string, error) {
	if f.gateErr != nil {
		return "", f.gateErr
	}
	return f.actorRole, nil
}
func (f *accountTxFake) GetAccount(ctx context.Context, householdID, userID string) (AccountRecord, error) {
	return (*accountFake)(f).GetAccount(ctx, householdID, userID)
}
func (f *accountTxFake) ListAccounts(ctx context.Context, householdID, cursor string, limit int) ([]AccountRecord, string, error) {
	return (*accountFake)(f).ListAccounts(ctx, householdID, cursor, limit)
}
func (f *accountTxFake) UpdateAccountRole(_ context.Context, _ string, userID, role string) (int64, error) {
	record := f.records[userID]
	record.Role = role
	record.Version++
	f.records[userID] = record
	f.updates = append(f.updates, userID)
	return record.Version, nil
}
func (f *accountTxFake) CountAdmins(context.Context, string) (int, error) { return f.admins, nil }
func (f *accountTxFake) RecordRoleChange(_ context.Context, change RoleChange) error {
	f.audits = append(f.audits, change)
	return nil
}

type accountLoginFake struct {
	logins map[string]string
	err    error
}

func (f accountLoginFake) LoginsByUserIDs(context.Context, []string) (map[string]string, error) {
	return f.logins, f.err
}

type accountSignInFake struct {
	methods map[string]SignInMethods
	err     error
}

func (f accountSignInFake) SignInMethodsByUserIDs(context.Context, []string) (map[string]SignInMethods, error) {
	return f.methods, f.err
}

type accountResetFake struct {
	resets map[string]PasswordReset
	err    error
}

func (f accountResetFake) PasswordResetsByUserIDs(context.Context, string, []string) (map[string]PasswordReset, error) {
	return f.resets, f.err
}

func accountFixture() (*accountFake, *AccountService) {
	repository := newAccountFake()
	repository.records[accountActor] = AccountRecord{UserID: accountActor, Role: RoleAdmin, Version: 1}
	repository.records[accountTarget] = AccountRecord{UserID: accountTarget, Role: RoleMember, Version: 1}
	repository.order = []string{accountActor, accountTarget}
	logins := accountLoginFake{logins: map[string]string{accountActor: "alice", accountTarget: "bob"}}
	signIn := accountSignInFake{methods: map[string]SignInMethods{accountActor: {Password: true}, accountTarget: {OIDC: true}}}
	service := NewAccountService(repository, logins, signIn, func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, accountResetFake{})
	return repository, service
}

func TestAccountCursorFormat(t *testing.T) {
	id := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	encoded := EncodeAccountCursor(id)
	if encoded[:2] != "a1" {
		t.Fatalf("cursor prefix=%q", encoded)
	}
	decoded, err := DecodeAccountCursor(encoded)
	if err != nil || decoded != id {
		t.Fatalf("decoded=%q err=%v", decoded, err)
	}
	for _, invalid := range []string{"m1" + encoded[2:], "a1bad", "a1" + encoded[2:len(encoded)-1]} {
		if _, err := DecodeAccountCursor(invalid); err == nil {
			t.Fatalf("cursor %q accepted", invalid)
		}
	}
}

func TestListAccountsJoinsIdentityFactsAndFailsClosed(t *testing.T) {
	_, service := accountFixture()
	accounts, next, err := service.ListAccounts(context.Background(), accountActor, accountHouse, "", 50)
	if err != nil || next != "" || len(accounts) != 2 {
		t.Fatalf("accounts=%+v next=%q err=%v", accounts, next, err)
	}
	if accounts[0].Login != "alice" || !accounts[0].SignInMethods.Password || accounts[1].Login != "bob" || !accounts[1].SignInMethods.OIDC {
		t.Fatalf("accounts=%+v", accounts)
	}
	if accounts[0].PasswordReset != nil || accounts[1].PasswordReset != nil {
		t.Fatalf("unexpected reset facts: %+v", accounts)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	repository, _ := accountFixture()
	resets := accountResetFake{resets: map[string]PasswordReset{accountTarget: {CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}}}
	withReset := NewAccountService(repository, accountLoginFake{logins: map[string]string{accountActor: "alice", accountTarget: "bob"}}, accountSignInFake{methods: map[string]SignInMethods{accountActor: {Password: true}, accountTarget: {OIDC: true}}}, time.Now, resets)
	account, err := withReset.GetAccount(context.Background(), accountActor, accountHouse, accountTarget)
	if err != nil || account.PasswordReset == nil || !account.PasswordReset.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("account=%+v err=%v", account, err)
	}

	missing := NewAccountService(repository, accountLoginFake{logins: map[string]string{accountActor: "alice", accountTarget: "bob"}}, accountSignInFake{methods: map[string]SignInMethods{accountActor: {Password: true}}}, time.Now, accountResetFake{})
	if _, _, err := missing.ListAccounts(context.Background(), accountActor, accountHouse, "", 50); err == nil {
		t.Fatal("missing sign-in methods defaulted instead of failing")
	}
	missing = NewAccountService(repository, accountLoginFake{logins: map[string]string{accountActor: "alice"}}, accountSignInFake{methods: map[string]SignInMethods{accountActor: {}, accountTarget: {}}}, time.Now, accountResetFake{})
	if _, _, err := missing.ListAccounts(context.Background(), accountActor, accountHouse, "", 50); err == nil {
		t.Fatal("missing login defaulted instead of failing")
	}
	resetDown := NewAccountService(repository, accountLoginFake{logins: map[string]string{accountActor: "alice", accountTarget: "bob"}}, accountSignInFake{methods: map[string]SignInMethods{accountActor: {Password: true}, accountTarget: {}}}, time.Now, nil)
	if _, _, err := resetDown.ListAccounts(context.Background(), accountActor, accountHouse, "", 50); err == nil {
		t.Fatal("missing password-reset lookup defaulted instead of failing")
	}
}

func TestChangeRoleAuthorizationAndPreconditions(t *testing.T) {
	repository, service := accountFixture()
	repository.actorRole = RoleMember
	if _, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountTarget, RoleAdmin, 1); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("member err=%v", err)
	}
	repository.actorRole = RoleAdmin
	if _, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountTarget, "owner", 1); err == nil {
		t.Fatal("unknown role accepted")
	}
	if _, err := service.ChangeRole(context.Background(), accountActor, accountHouse, "missing", RoleAdmin, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target err=%v", err)
	}
	if _, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountTarget, RoleAdmin, 9); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale err=%v", err)
	}
	if len(repository.updates) != 0 || len(repository.audits) != 0 {
		t.Fatalf("failed changes wrote updates=%v audits=%v", repository.updates, repository.audits)
	}
}

func TestChangeRoleNoOpAndAudit(t *testing.T) {
	repository, service := accountFixture()
	unchanged, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountTarget, RoleMember, 1)
	if err != nil || unchanged.Role != RoleMember || unchanged.Version != 1 {
		t.Fatalf("unchanged=%+v err=%v", unchanged, err)
	}
	if len(repository.updates) != 0 || len(repository.audits) != 0 {
		t.Fatalf("no-op wrote updates=%v audits=%v", repository.updates, repository.audits)
	}
	changed, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountTarget, RoleAdmin, 1)
	if err != nil || changed.Role != RoleAdmin || changed.Version != 2 {
		t.Fatalf("changed=%+v err=%v", changed, err)
	}
	if len(repository.audits) != 1 {
		t.Fatalf("audits=%+v", repository.audits)
	}
	audit := repository.audits[0]
	if audit.ActorUserID != accountActor || audit.TargetUserID != accountTarget || audit.PreviousRole != RoleMember || audit.NewRole != RoleAdmin {
		t.Fatalf("audit=%+v", audit)
	}
	if !audit.ChangedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("changedAt=%v", audit.ChangedAt)
	}
}

func TestChangeRoleRejectsLastAdminAndRollsBack(t *testing.T) {
	repository, service := accountFixture()
	repository.admins = 0
	if _, err := service.ChangeRole(context.Background(), accountActor, accountHouse, accountActor, RoleMember, 1); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err=%v", err)
	}
	if repository.records[accountActor].Role != RoleAdmin || repository.records[accountActor].Version != 1 {
		t.Fatalf("rollback failed: %+v", repository.records[accountActor])
	}
	if len(repository.audits) != 0 {
		t.Fatalf("last-admin rejection wrote audit: %+v", repository.audits)
	}
}

func TestGetAccountAuthorizationAndMalformedTarget(t *testing.T) {
	repository, service := accountFixture()
	if _, err := service.GetAccount(context.Background(), accountActor, accountHouse, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed target err=%v", err)
	}
	repository.actorRole = RoleMember
	if _, err := service.GetAccount(context.Background(), accountActor, accountHouse, accountTarget); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("member err=%v", err)
	}
	repository.actorRole, repository.gateErr = RoleAdmin, ErrNotFound
	if _, err := service.GetAccount(context.Background(), accountActor, accountHouse, accountTarget); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider err=%v", err)
	}
}
