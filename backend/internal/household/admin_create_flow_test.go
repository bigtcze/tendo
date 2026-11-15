package household

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	adminCreateActor = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	adminCreateAdmin = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	adminCreateHouse = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
)

// adminCreateTxFake is one scripted instance-admin transaction. The service
// calls only the create/replay steps of InstanceAdminTransaction; the remaining
// methods satisfy the shared household-admin interfaces and are never reached,
// so a call to them fails the test through the repository's unexpected-call
// guard rather than silently succeeding.
type adminCreateTxFake struct {
	t *testing.T

	gateErr   error
	gateCalls int

	findCalls       int
	findHouse       AdminHousehold
	findVersion     int64
	findFingerprint []byte
	findErr         error

	insertHouseCalls   int
	insertHouseErr     error
	insertCreationHits int
	insertCreationErr  error

	getAccountErr       error
	insertMembershipErr error
	assignmentCalls     int
}

func newAdminCreateTx(t *testing.T) *adminCreateTxFake {
	t.Helper()
	return &adminCreateTxFake{
		t:           t,
		findErr:     ErrNotFound,
		findHouse:   AdminHousehold{ID: adminCreateHouse, Name: "Home", Timezone: "UTC"},
		findVersion: 1,
	}
}

func (f *adminCreateTxFake) RequireInstanceAdmin(context.Context, string) error {
	f.gateCalls++
	return f.gateErr
}

func (f *adminCreateTxFake) FindHouseholdCreation(context.Context, string, string) (AdminHousehold, int64, []byte, error) {
	f.findCalls++
	if f.findErr != nil {
		return AdminHousehold{}, 0, nil, f.findErr
	}
	return f.findHouse, f.findVersion, f.findFingerprint, nil
}

func (f *adminCreateTxFake) InsertHousehold(context.Context, string, string, time.Time) (AdminHousehold, int64, error) {
	f.insertHouseCalls++
	if f.insertHouseErr != nil {
		return AdminHousehold{}, 0, f.insertHouseErr
	}
	return AdminHousehold{ID: adminCreateHouse, Name: "Home", Timezone: "UTC"}, 1, nil
}

func (f *adminCreateTxFake) InsertHouseholdCreation(context.Context, HouseholdCreation) error {
	f.insertCreationHits++
	return f.insertCreationErr
}

func (f *adminCreateTxFake) GetAccount(context.Context, string, string) (AccountRecord, error) {
	return AccountRecord{}, f.getAccountErr
}

func (f *adminCreateTxFake) InsertMembership(context.Context, string, string, string) (int64, error) {
	return 1, f.insertMembershipErr
}

func (f *adminCreateTxFake) RecordMemberAssignment(context.Context, MemberAssignment) error {
	f.assignmentCalls++
	return nil
}

func (f *adminCreateTxFake) LockHousehold(context.Context, string) error {
	f.t.Fatalf("LockHousehold called by the create path")
	return nil
}

func (f *adminCreateTxFake) HouseholdExists(context.Context, string) error {
	f.t.Fatalf("HouseholdExists called by the create path")
	return nil
}

func (f *adminCreateTxFake) ListHouseholds(context.Context, string, int) ([]AdminHousehold, error) {
	f.t.Fatalf("ListHouseholds called by the create path")
	return nil, nil
}

func (f *adminCreateTxFake) ListAccounts(context.Context, string, string, int) ([]AccountRecord, string, error) {
	f.t.Fatalf("ListAccounts called by the create path")
	return nil, "", nil
}

func (f *adminCreateTxFake) GetHousehold(context.Context, string) (AdminHousehold, int64, error) {
	f.t.Fatalf("GetHousehold called by the create path")
	return AdminHousehold{}, 0, nil
}

func (f *adminCreateTxFake) LockHouseholdForMembership(context.Context, string, string) error {
	f.t.Fatalf("LockHouseholdForMembership called by the create path")
	return nil
}

func (f *adminCreateTxFake) GetMembership(context.Context, string, string) (string, error) {
	f.t.Fatalf("GetMembership called by the create path")
	return "", nil
}

func (f *adminCreateTxFake) LockMemberAccount(context.Context, string) error {
	f.t.Fatalf("LockMemberAccount called by the create path")
	return nil
}

func (f *adminCreateTxFake) LockMembership(context.Context, string, string) (MembershipRecord, int64, error) {
	f.t.Fatalf("LockMembership called by the create path")
	return MembershipRecord{}, 0, nil
}

func (f *adminCreateTxFake) CountAdminsExcluding(context.Context, string, string) (int, error) {
	f.t.Fatalf("CountAdminsExcluding called by the create path")
	return 0, nil
}

func (f *adminCreateTxFake) DeleteMembership(context.Context, string, string) error {
	f.t.Fatalf("DeleteMembership called by the create path")
	return nil
}

func (f *adminCreateTxFake) RecordMemberRemoval(context.Context, MemberRemoval) error {
	f.t.Fatalf("RecordMemberRemoval called by the create path")
	return nil
}

func (f *adminCreateTxFake) UpdateAccountRole(context.Context, string, string, string) (int64, error) {
	f.t.Fatalf("UpdateAccountRole called by the create path")
	return 0, nil
}

func (f *adminCreateTxFake) CountAdmins(context.Context, string) (int, error) {
	return 0, nil
}

func (f *adminCreateTxFake) RecordRoleChange(context.Context, RoleChange) error {
	f.t.Fatalf("RecordRoleChange called by the create path")
	return nil
}

type adminCreateRepoFake struct {
	txs     []*adminCreateTxFake
	effects InstanceAdminEffects
	calls   int
	extra   bool
}

func (r *adminCreateRepoFake) WithInstanceAdminTransaction(_ context.Context, work func(InstanceAdminTransaction, InstanceAdminEffects) error) error {
	r.calls++
	if r.calls > len(r.txs) {
		r.extra = true
		return errors.New("unexpected extra instance-admin transaction")
	}
	return work(r.txs[r.calls-1], r.effects)
}

type assignmentLockerFake struct {
	err error
}

func (f *assignmentLockerFake) LockAssignableAccount(context.Context, string) (string, error) {
	return "admin", f.err
}

type assignmentDefaultsFake struct{}

func (assignmentDefaultsFake) SetDefaultHouseholdIfUnset(context.Context, string, string) error {
	return nil
}

type assignmentPersonsFake struct{}

func (assignmentPersonsFake) EnsureMemberPerson(context.Context, string, string, string) error {
	return nil
}

func newAdminCreateService(t *testing.T, locker *assignmentLockerFake, txs ...*adminCreateTxFake) (*InstanceAdminCenter, *adminCreateRepoFake) {
	t.Helper()
	repo := &adminCreateRepoFake{
		txs: txs,
		effects: InstanceAdminEffects{Assignment: AssignmentEffects{
			Accounts: locker,
			Defaults: assignmentDefaultsFake{},
			Persons:  assignmentPersonsFake{},
		}},
	}
	return NewInstanceAdminCenter(repo, nil, nil, func() time.Time {
		return time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	}), repo
}

func TestCreateHouseholdGateFailsBeforeReplayLookup(t *testing.T) {
	tx := newAdminCreateTx(t)
	tx.gateErr = ErrInstanceAdminRequired
	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, tx)

	if _, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin); !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("create err=%v want ErrInstanceAdminRequired", err)
	}
	if repo.calls != 1 {
		t.Fatalf("transactions=%d want 1", repo.calls)
	}
	if tx.gateCalls != 1 || tx.findCalls != 0 {
		t.Fatalf("gate calls=%d find calls=%d; the replay lookup must not run without the grant", tx.gateCalls, tx.findCalls)
	}
	if tx.insertHouseCalls != 0 || tx.insertCreationHits != 0 {
		t.Fatalf("writes ran without the grant: household=%d creation=%d", tx.insertHouseCalls, tx.insertCreationHits)
	}
}

func TestCreateHouseholdRacedKeyReplaysTheWinnerOnce(t *testing.T) {
	winner := AdminHousehold{ID: adminCreateHouse, Name: "Home", Timezone: "UTC"}
	first := newAdminCreateTx(t)
	first.insertCreationErr = ErrCreationKeyRaced

	second := newAdminCreateTx(t)
	second.findErr = nil
	second.findHouse = winner
	second.findVersion = 1
	second.findFingerprint = householdCreationFingerprint(adminCreateActor, "Home", "UTC", adminCreateAdmin)

	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, first, second)
	house, version, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin)
	if err != nil {
		t.Fatalf("create after the lost race: %v", err)
	}
	if house.ID != winner.ID || version != 1 {
		t.Fatalf("household=%+v version=%d want the winning household", house, version)
	}
	if repo.calls != 2 || repo.extra {
		t.Fatalf("transactions=%d extra=%t want exactly one replay transaction", repo.calls, repo.extra)
	}
	if first.gateCalls != 1 || first.findCalls != 1 || first.insertHouseCalls != 1 || first.insertCreationHits != 1 {
		t.Fatalf("first transaction gate=%d find=%d household=%d creation=%d", first.gateCalls, first.findCalls, first.insertHouseCalls, first.insertCreationHits)
	}
	if second.gateCalls != 1 || second.findCalls != 1 {
		t.Fatalf("replay gate=%d find=%d; the replay must re-check the grant and read the key", second.gateCalls, second.findCalls)
	}
	if second.insertHouseCalls != 0 || second.insertCreationHits != 0 || second.assignmentCalls != 0 {
		t.Fatalf("replay must never insert: household=%d creation=%d assignment=%d", second.insertHouseCalls, second.insertCreationHits, second.assignmentCalls)
	}
}

func TestCreateHouseholdReplayRequiresTheGrant(t *testing.T) {
	first := newAdminCreateTx(t)
	first.insertCreationErr = ErrCreationKeyRaced
	second := newAdminCreateTx(t)
	second.gateErr = ErrInstanceAdminRequired

	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, first, second)
	if _, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin); !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("create err=%v want ErrInstanceAdminRequired", err)
	}
	if repo.calls != 2 || second.gateCalls != 1 || second.findCalls != 0 {
		t.Fatalf("transactions=%d replay gate=%d find=%d; the revoked grant must stop the replay before its lookup", repo.calls, second.gateCalls, second.findCalls)
	}
}

func TestCreateHouseholdReplayFindsNothingIsUnavailableNotNotFound(t *testing.T) {
	first := newAdminCreateTx(t)
	first.insertCreationErr = ErrCreationKeyRaced
	second := newAdminCreateTx(t)
	second.findErr = ErrNotFound

	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, first, second)
	_, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin)
	if !errors.Is(err, errAdminAccountUnavailable) {
		t.Fatalf("create err=%v want the unavailable error", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("create err=%v must not leak ErrNotFound", err)
	}
	if repo.calls != 2 {
		t.Fatalf("transactions=%d want 2", repo.calls)
	}
}

func TestCreateHouseholdReplayDifferentIntentIsReused(t *testing.T) {
	first := newAdminCreateTx(t)
	first.insertCreationErr = ErrCreationKeyRaced
	second := newAdminCreateTx(t)
	second.findErr = nil
	second.findHouse = AdminHousehold{ID: adminCreateHouse, Name: "Other", Timezone: "UTC"}
	second.findFingerprint = householdCreationFingerprint(adminCreateActor, "Other", "UTC", adminCreateAdmin)

	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, first, second)
	if _, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("create err=%v want ErrIdempotencyKeyReused", err)
	}
	if repo.calls != 2 {
		t.Fatalf("transactions=%d want 2", repo.calls)
	}
}

func TestCreateHouseholdSecondRaceIsNeverRetried(t *testing.T) {
	first := newAdminCreateTx(t)
	first.insertCreationErr = ErrCreationKeyRaced
	second := newAdminCreateTx(t)
	second.findErr = ErrCreationKeyRaced

	service, repo := newAdminCreateService(t, &assignmentLockerFake{}, first, second)
	if _, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin); !errors.Is(err, ErrCreationKeyRaced) {
		t.Fatalf("create err=%v want ErrCreationKeyRaced", err)
	}
	if repo.calls != 2 || repo.extra {
		t.Fatalf("transactions=%d extra=%t; a lost race gets exactly one replay, never a third transaction", repo.calls, repo.extra)
	}
	if second.findCalls != 1 {
		t.Fatalf("replay find calls=%d want 1", second.findCalls)
	}
}

func TestCreateHouseholdMissingAdminAccountIsUnavailableNotNotFound(t *testing.T) {
	tx := newAdminCreateTx(t)
	locker := &assignmentLockerFake{err: ErrNotFound}

	service, repo := newAdminCreateService(t, locker, tx)
	_, _, err := service.CreateHousehold(context.Background(), adminCreateActor, "key", "Home", "UTC", adminCreateAdmin)
	if !errors.Is(err, errAdminAccountUnavailable) {
		t.Fatalf("create err=%v want the unavailable error", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("create err=%v must not leak ErrNotFound", err)
	}
	if repo.calls != 1 || tx.insertHouseCalls != 1 || tx.insertCreationHits != 1 {
		t.Fatalf("transactions=%d household=%d creation=%d", repo.calls, tx.insertHouseCalls, tx.insertCreationHits)
	}
	if tx.assignmentCalls != 0 {
		t.Fatalf("assignment rows=%d; a missing account must not be assigned", tx.assignmentCalls)
	}
}
