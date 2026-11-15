package household

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	removalActor   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	removalHouse   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	removalTarget  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	removalUnknown = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"
)

// removalTxFake records the order of every transaction step so ordering can be
// asserted as a contract, not as an implementation detail of one statement.
type removalTxFake struct {
	events     []string
	gateErr    error
	actorRole  string
	target     MembershipRecord
	version    int64
	membership error
	accountErr error
	admins     int
	deleted    []string
	audits     []MemberRemoval
}

func (f *removalTxFake) record(name string) { f.events = append(f.events, name) }

func (f *removalTxFake) LockHouseholdForMembership(context.Context, string, string) error {
	f.record("lock-household")
	return f.gateErr
}
func (f *removalTxFake) GetMembership(context.Context, string, string) (string, error) {
	f.record("read-actor-role")
	if f.gateErr != nil {
		return "", f.gateErr
	}
	return f.actorRole, nil
}
func (f *removalTxFake) LockMemberAccount(context.Context, string) error {
	f.record("lock-account")
	return f.accountErr
}
func (f *removalTxFake) LockMembership(context.Context, string, string) (MembershipRecord, int64, error) {
	f.record("lock-membership")
	if f.membership != nil {
		return MembershipRecord{}, 0, f.membership
	}
	return f.target, f.version, nil
}
func (f *removalTxFake) CountAdminsExcluding(context.Context, string, string) (int, error) {
	f.record("count-admins")
	return f.admins, nil
}
func (f *removalTxFake) DeleteMembership(_ context.Context, _, userID string) error {
	f.record("delete-membership")
	f.deleted = append(f.deleted, userID)
	return nil
}
func (f *removalTxFake) RecordMemberRemoval(_ context.Context, removal MemberRemoval) error {
	f.record("audit")
	f.audits = append(f.audits, removal)
	return nil
}

type effectFake struct {
	name  string
	tx    *removalTxFake
	calls []string
}

func (e *effectFake) record(householdID, userID string) error {
	e.tx.events = append(e.tx.events, e.name)
	e.calls = append(e.calls, householdID+"/"+userID)
	return nil
}
func (e *effectFake) RevokePendingInvitationsCreatedBy(_ context.Context, householdID, userID string) error {
	return e.record(householdID, userID)
}
func (e *effectFake) ReleaseMemberResponsibility(_ context.Context, householdID, userID string) error {
	return e.record(householdID, userID)
}
func (e *effectFake) UnlinkMemberPerson(_ context.Context, householdID, userID string) error {
	return e.record(householdID, userID)
}
func (e *effectFake) RevokeMemberPasswordResets(_ context.Context, householdID, userID string) error {
	return e.record(householdID, userID)
}
func (e *effectFake) ReassignDefaultHousehold(_ context.Context, userID, householdID string) error {
	return e.record(householdID, userID)
}

type removalRepoFake struct {
	tx      *removalTxFake
	effects MemberRemovalEffects
	calls   int
	err     error
}

func (r *removalRepoFake) WithMemberRemovalTransaction(_ context.Context, work func(MemberRemovalTransaction, MemberRemovalEffects) error) error {
	r.calls++
	if r.err != nil {
		return r.err
	}
	return work(r.tx, r.effects)
}

func newRemovalFixture() (*MemberRemovalService, *removalRepoFake, *removalTxFake) {
	tx := &removalTxFake{actorRole: RoleAdmin, target: MembershipRecord{UserID: removalTarget, Role: RoleMember}, version: 1, admins: 1}
	effects := MemberRemovalEffects{
		Invitations: &effectFake{name: "revoke-invitations", tx: tx},
		Items:       &effectFake{name: "release-items", tx: tx},
		Subjects:    &effectFake{name: "unlink-person", tx: tx},
		Resets:      &effectFake{name: "revoke-resets", tx: tx},
		Accounts:    &effectFake{name: "reassign-default", tx: tx},
	}
	repo := &removalRepoFake{tx: tx, effects: effects}
	clock := func() time.Time { return time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC) }
	return NewMemberRemovalService(repo, clock), repo, tx
}

func (r *removalRepoFake) effect(name string) *effectFake {
	switch name {
	case "revoke-invitations":
		return r.effects.Invitations.(*effectFake)
	case "release-items":
		return r.effects.Items.(*effectFake)
	case "unlink-person":
		return r.effects.Subjects.(*effectFake)
	case "revoke-resets":
		return r.effects.Resets.(*effectFake)
	default:
		return r.effects.Accounts.(*effectFake)
	}
}

func TestRemoveMemberAuthorizationMatrix(t *testing.T) {
	cases := []struct {
		name       string
		actor      string
		target     string
		actorRole  string
		gateErr    error
		record     MembershipRecord
		version    int64
		admins     int
		membership error
		want       error
	}{
		{name: "admin removes member", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, record: MembershipRecord{Role: RoleMember}, version: 1, admins: 0},
		{name: "admin removes admin", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, record: MembershipRecord{Role: RoleAdmin}, version: 1, admins: 1},
		{name: "admin removes self", actor: removalActor, target: removalActor, actorRole: RoleAdmin, record: MembershipRecord{Role: RoleAdmin}, version: 1, admins: 1},
		{name: "last admin removes self", actor: removalActor, target: removalActor, actorRole: RoleAdmin, record: MembershipRecord{Role: RoleAdmin}, version: 1, admins: 0, want: ErrLastAdmin},
		{name: "member leaves", actor: removalActor, target: removalActor, actorRole: RoleMember, record: MembershipRecord{Role: RoleMember}, version: 1, admins: 1},
		{name: "member leaves in different case", actor: removalActor, target: "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B60", actorRole: RoleMember, record: MembershipRecord{Role: RoleMember}, version: 1, admins: 1},
		{name: "member removes another member", actor: removalActor, target: removalTarget, actorRole: RoleMember, record: MembershipRecord{Role: RoleMember}, version: 1, admins: 1, want: ErrAdminRequired},
		{name: "member removes an admin", actor: removalActor, target: removalTarget, actorRole: RoleMember, record: MembershipRecord{Role: RoleAdmin}, version: 1, admins: 1, want: ErrAdminRequired},
		{name: "outsider", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, gateErr: ErrNotFound, want: ErrNotFound},
		{name: "unknown household", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, gateErr: ErrNotFound, want: ErrNotFound},
		{name: "target outside household", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, membership: ErrNotFound, want: ErrNotFound},
		{name: "stale version", actor: removalActor, target: removalTarget, actorRole: RoleAdmin, record: MembershipRecord{Role: RoleMember}, version: 4, want: ErrPreconditionFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, tx := newRemovalFixture()
			tx.actorRole, tx.gateErr, tx.target, tx.version, tx.admins = tc.actorRole, tc.gateErr, tc.record, tc.version, tc.admins
			tx.membership = tc.membership
			err := service.RemoveMember(context.Background(), tc.actor, removalHouse, tc.target, 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if tc.want != nil {
				if len(tx.deleted) != 0 || len(tx.audits) != 0 {
					t.Fatalf("refused removal changed state: deleted=%v audits=%v", tx.deleted, tx.audits)
				}
				return
			}
			if len(tx.deleted) != 1 || tx.deleted[0] != tc.target {
				t.Fatalf("deleted=%v", tx.deleted)
			}
			if len(tx.audits) != 1 || tx.audits[0].ActorUserID != tc.actor ||
				tx.audits[0].TargetUserID != tc.target || tx.audits[0].PreviousRole != tc.record.Role ||
				!tx.audits[0].RemovedAt.Equal(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)) {
				t.Fatalf("audits=%+v", tx.audits)
			}
			if got := repo.effect("revoke-invitations").calls; len(got) != 1 || got[0] != removalHouse+"/"+tc.target {
				t.Fatalf("invitation revocations=%v", got)
			}
		})
	}
}

func TestRemoveMemberRejectsIdentifiersBeforeAnyDatabaseWork(t *testing.T) {
	for _, tc := range []struct{ name, actor, house, target string }{
		{"malformed target", removalActor, removalHouse, "not-a-uuid"},
		{"malformed household", removalActor, "nope", removalTarget},
		{"malformed actor", "nope", removalHouse, removalTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _ := newRemovalFixture()
			if err := service.RemoveMember(context.Background(), tc.actor, tc.house, tc.target, 1); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err=%v", err)
			}
			if repo.calls != 0 {
				t.Fatalf("repository called %d times", repo.calls)
			}
		})
	}
	service, repo, _ := newRemovalFixture()
	if err := service.RemoveMember(context.Background(), removalActor, removalHouse, removalTarget, 0); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("zero version err=%v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("repository called %d times", repo.calls)
	}
}

func TestRemoveMemberEffectOrder(t *testing.T) {
	service, repo, tx := newRemovalFixture()
	if err := service.RemoveMember(context.Background(), removalActor, removalHouse, removalTarget, 1); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"lock-household", "read-actor-role",
		"revoke-invitations",
		"lock-account", "lock-membership",
		"release-items", "unlink-person", "revoke-resets",
		"delete-membership", "reassign-default", "audit",
	}
	if len(tx.events) != len(want) {
		t.Fatalf("events=%v", tx.events)
	}
	for i := range want {
		if tx.events[i] != want[i] {
			t.Fatalf("events=%v want=%v", tx.events, want)
		}
	}
	// Invitations must be revoked before the account lock: acceptance takes the
	// invitation row and then the account row (ADR 0019).
	invitationIndex, accountIndex := indexOf(tx.events, "revoke-invitations"), indexOf(tx.events, "lock-account")
	if invitationIndex > accountIndex {
		t.Fatalf("invitations revoked after account lock: %v", tx.events)
	}
	for _, name := range []string{"release-items", "unlink-person", "revoke-resets", "reassign-default"} {
		calls := repo.effect(name).calls
		if len(calls) != 1 {
			t.Fatalf("%s calls=%v", name, calls)
		}
	}
}

func TestRemoveMemberStopsAfterVersionMismatch(t *testing.T) {
	service, repo, tx := newRemovalFixture()
	tx.version = 9
	if err := service.RemoveMember(context.Background(), removalActor, removalHouse, removalTarget, 2); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("err=%v", err)
	}
	for _, name := range []string{"release-items", "unlink-person", "revoke-resets"} {
		if calls := repo.effect(name).calls; len(calls) != 0 {
			t.Fatalf("%s ran after version mismatch: %v", name, calls)
		}
	}
	if len(tx.deleted) != 0 || len(tx.audits) != 0 {
		t.Fatal("stale version changed state")
	}
}

func TestRemoveMemberStopsOnLastAdminBeforeEffects(t *testing.T) {
	service, repo, tx := newRemovalFixture()
	tx.target = MembershipRecord{Role: RoleAdmin}
	tx.admins = 0
	if err := service.RemoveMember(context.Background(), removalActor, removalHouse, removalTarget, 1); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err=%v", err)
	}
	for _, name := range []string{"release-items", "unlink-person", "revoke-resets", "reassign-default"} {
		if calls := repo.effect(name).calls; len(calls) != 0 {
			t.Fatalf("%s ran before the last-admin refusal: %v", name, calls)
		}
	}
}

func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return -1
}
