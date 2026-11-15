package household

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	deletionActor  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	deletionTarget = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	deletionHouseA = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	deletionHouseB = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"
	deletionHouseC = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64"
)

var deletionNow = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

// deletionRecorder is the shared call log of one fixture. Transaction methods
// and effect methods append to it, so the whole ADR 0022 order (locks, reads,
// refusals, effects) is asserted as one contract instead of one mock at a time.
type deletionRecorder struct{ calls []string }

func (r *deletionRecorder) record(call string) { r.calls = append(r.calls, call) }

func (r *deletionRecorder) reset() { r.calls = nil }

func (r *deletionRecorder) has(prefix string) bool {
	return slices.ContainsFunc(r.calls, func(call string) bool { return strings.HasPrefix(call, prefix) })
}

// deletionTxFake implements AccountDeletionTransaction. Methods the deletion
// never reaches keep the interface satisfied; unused ones just record.
type deletionTxFake struct {
	rec *deletionRecorder

	requireErr     error
	households     []string
	listHouseErr   error
	lockHouseErr   error
	memberships    []AccountMembership
	lockMembersErr error
	admins         map[string]int
	countAdminsErr error
	grant          InstanceAdminGrantRecord
	granted        bool
	findGrantErr   error
	liveAdmins     int
	countLiveErr   error
	revokeErr      error

	lockedHouseholds []string
	revocations      []InstanceAdminRevocation
	removals         []MemberRemoval
	deleted          []string
	deletions        []AccountDeletion
}

func (f *deletionTxFake) clone() *deletionTxFake {
	clone := *f
	clone.households = slices.Clone(f.households)
	clone.memberships = slices.Clone(f.memberships)
	clone.admins = make(map[string]int, len(f.admins))
	for house, count := range f.admins {
		clone.admins[house] = count
	}
	return &clone
}

func (f *deletionTxFake) LockHouseholdForMembership(context.Context, string, string) error {
	f.rec.record("lock-household-for-membership")
	return nil
}

func (f *deletionTxFake) GetMembership(context.Context, string, string) (string, error) {
	f.rec.record("read-membership")
	return RoleAdmin, nil
}

func (f *deletionTxFake) LockMemberAccount(context.Context, string) error {
	f.rec.record("lock-member-account")
	return nil
}

func (f *deletionTxFake) LockMembership(context.Context, string, string) (MembershipRecord, int64, error) {
	f.rec.record("lock-membership")
	return MembershipRecord{}, 0, nil
}

func (f *deletionTxFake) RequireInstanceAdminExclusive(_ context.Context, actorID string) error {
	f.rec.record("installation-lock:" + actorID)
	return f.requireErr
}

func (f *deletionTxFake) LockHousehold(_ context.Context, householdID string) error {
	f.rec.record("lock-household:" + householdID)
	f.lockedHouseholds = append(f.lockedHouseholds, householdID)
	return f.lockHouseErr
}

func (f *deletionTxFake) ListUserHouseholds(_ context.Context, userID string) ([]string, error) {
	f.rec.record("list-households:" + userID)
	return f.households, f.listHouseErr
}

func (f *deletionTxFake) LockUserMemberships(_ context.Context, userID string) ([]AccountMembership, error) {
	f.rec.record("lock-memberships:" + userID)
	return f.memberships, f.lockMembersErr
}

func (f *deletionTxFake) CountAdminsExcluding(_ context.Context, householdID, _ string) (int, error) {
	f.rec.record("count-admins:" + householdID)
	return f.admins[householdID], f.countAdminsErr
}

func (f *deletionTxFake) FindInstanceAdminGrant(_ context.Context, userID string) (InstanceAdminGrantRecord, bool, error) {
	f.rec.record("find-grant:" + userID)
	return f.grant, f.granted, f.findGrantErr
}

func (f *deletionTxFake) CountLiveInstanceAdminsExcluding(_ context.Context, userID string) (int, error) {
	f.rec.record("count-instance-admins:" + userID)
	return f.liveAdmins, f.countLiveErr
}

func (f *deletionTxFake) RevokeInstanceAdmin(_ context.Context, revocation InstanceAdminRevocation) error {
	f.rec.record("revoke-grant:" + revocation.TargetUserID)
	f.revocations = append(f.revocations, revocation)
	return f.revokeErr
}

func (f *deletionTxFake) RecordAccountDeletion(_ context.Context, deletion AccountDeletion) error {
	f.rec.record("deletion-record:" + deletion.TargetUserID)
	f.deletions = append(f.deletions, deletion)
	return nil
}

func (f *deletionTxFake) RecordMemberRemoval(_ context.Context, removal MemberRemoval) error {
	f.rec.record("removal-audit:" + removal.HouseholdID)
	f.removals = append(f.removals, removal)
	return nil
}

func (f *deletionTxFake) DeleteMembership(_ context.Context, householdID, userID string) error {
	f.rec.record("delete-membership:" + householdID)
	f.deleted = append(f.deleted, householdID+"/"+userID)
	return nil
}

// accountEraserFake implements AccountEraser and records the spelling of every
// user ID it is handed, so lowercasing is asserted at the service boundary.
type accountEraserFake struct {
	rec *deletionRecorder

	login             string
	findErr           error
	lockIdentitiesErr error
	lockErr           error
	revokeResetsErr   error
	eraseErr          error
	anonymizeErr      error

	findUserIDs     []string
	lockIdentitiIDs []string
	lockUserIDs     []string
	anonymizedID    string
	anonymizedAt    time.Time
}

func (f *accountEraserFake) FindDeletableAccount(_ context.Context, userID string) (string, error) {
	f.rec.record("find-account:" + userID)
	f.findUserIDs = append(f.findUserIDs, userID)
	return f.login, f.findErr
}

func (f *accountEraserFake) LockAccountIdentities(_ context.Context, userID string) error {
	f.rec.record("lock-identities:" + userID)
	f.lockIdentitiIDs = append(f.lockIdentitiIDs, userID)
	return f.lockIdentitiesErr
}

func (f *accountEraserFake) LockDeletableAccount(_ context.Context, userID string) (string, error) {
	f.rec.record("lock-account:" + userID)
	f.lockUserIDs = append(f.lockUserIDs, userID)
	return f.login, f.lockErr
}

func (f *accountEraserFake) RevokeAccountPasswordResets(_ context.Context, userID string, _ time.Time) error {
	f.rec.record("account-resets:" + userID)
	return f.revokeResetsErr
}

func (f *accountEraserFake) EraseAccountAccess(_ context.Context, userID string) error {
	f.rec.record("erase-access:" + userID)
	return f.eraseErr
}

func (f *accountEraserFake) AnonymizeAccount(_ context.Context, userID string, now time.Time) error {
	f.rec.record("anonymize:" + userID)
	f.anonymizedID, f.anonymizedAt = userID, now
	return f.anonymizeErr
}

// deletionEffectFake serves the four per-household removal effect interfaces.
type deletionEffectFake struct {
	rec      *deletionRecorder
	name     string
	result   error
	lastPair string
}

func (e *deletionEffectFake) record(householdID string) error {
	e.lastPair = householdID
	e.rec.record(fmt.Sprintf("%s:%s", e.name, householdID))
	return e.result
}

func (e *deletionEffectFake) RevokePendingInvitationsCreatedBy(_ context.Context, householdID, _ string) error {
	return e.record(householdID)
}

func (e *deletionEffectFake) ReleaseMemberResponsibility(_ context.Context, householdID, _ string) error {
	return e.record(householdID)
}

func (e *deletionEffectFake) UnlinkMemberPerson(_ context.Context, householdID, _ string) error {
	return e.record(householdID)
}

func (e *deletionEffectFake) RevokeMemberPasswordResets(_ context.Context, householdID, _ string) error {
	return e.record(householdID)
}

type deletionRepoFake struct {
	rec      *deletionRecorder
	effects  AccountDeletionEffects
	base     *deletionTxFake
	newTx    func(attempt int) *deletionTxFake
	attempts int
	txs      []*deletionTxFake
	err      error
}

func (r *deletionRepoFake) WithAccountDeletionTransaction(_ context.Context, work func(AccountDeletionTransaction, AccountDeletionEffects) error) error {
	r.attempts++
	tx := r.base
	if r.newTx != nil {
		tx = r.newTx(r.attempts)
	}
	r.txs = append(r.txs, tx)
	if r.err != nil {
		return r.err
	}
	return work(tx, r.effects)
}

type deletionFixture struct {
	service *AccountDeletionService
	repo    *deletionRepoFake
	tx      *deletionTxFake
	effect  map[string]*deletionEffectFake
	account *accountEraserFake
	rec     *deletionRecorder
}

func newDeletionFixture() *deletionFixture {
	rec := &deletionRecorder{}
	account := &accountEraserFake{rec: rec, login: "alice"}
	effects := AccountDeletionEffects{
		Removal: MemberRemovalEffects{
			Invitations: &deletionEffectFake{rec: rec, name: "revoke-invitations"},
			Items:       &deletionEffectFake{rec: rec, name: "items"},
			Subjects:    &deletionEffectFake{rec: rec, name: "subjects"},
			Resets:      &deletionEffectFake{rec: rec, name: "resets"},
		},
		Account: account,
	}
	tx := &deletionTxFake{
		rec:         rec,
		households:  []string{deletionHouseA, deletionHouseB},
		memberships: []AccountMembership{{HouseholdID: deletionHouseA, Role: RoleAdmin}, {HouseholdID: deletionHouseB, Role: RoleMember}},
		admins:      map[string]int{deletionHouseA: 1},
		grant:       InstanceAdminGrantRecord{Source: "bootstrap", GrantedAt: deletionNow.Add(-time.Hour)},
		granted:     true,
		liveAdmins:  1,
	}
	repo := &deletionRepoFake{rec: rec, effects: effects, base: tx}
	return &deletionFixture{
		service: NewAccountDeletionService(repo, func() time.Time { return deletionNow }),
		repo:    repo,
		tx:      tx,
		effect: map[string]*deletionEffectFake{
			"revoke-invitations": effects.Removal.Invitations.(*deletionEffectFake),
			"items":              effects.Removal.Items.(*deletionEffectFake),
			"subjects":           effects.Removal.Subjects.(*deletionEffectFake),
			"resets":             effects.Removal.Resets.(*deletionEffectFake),
		},
		account: account,
		rec:     rec,
	}
}

func requireDeletionCalls(t *testing.T, rec *deletionRecorder, want ...string) {
	t.Helper()
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("calls=%v\nwant=%v", rec.calls, want)
	}
}

func requireDeletionPrefix(t *testing.T, rec *deletionRecorder, want ...string) {
	t.Helper()
	if len(rec.calls) < len(want) || !slices.Equal(rec.calls[:len(want)], want) {
		t.Fatalf("calls=%v\nwant prefix=%v", rec.calls, want)
	}
}

// deletionMutations are the effects that must never be reached on a refusal.
// The invitation revocation runs before the account lock (ADR 0022 order) and
// is rolled back with the transaction, so it is not in this set.
var deletionMutations = []string{
	"revoke-grant", "delete-membership", "account-resets", "erase-access",
	"deletion-record", "anonymize",
}

func requireNoDeletionMutations(t *testing.T, rec *deletionRecorder) {
	t.Helper()
	for _, mutation := range deletionMutations {
		if rec.has(mutation) {
			t.Fatalf("refusal performed %s: %v", mutation, rec.calls)
		}
	}
}

func requireValidation(t *testing.T, err error, field, code string) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err=%v want ValidationError{%s,%s}", err, field, code)
	}
	if validation.Field != field || validation.Code != code {
		t.Fatalf("validation=%+v want field=%q code=%q", validation, field, code)
	}
}

// TestAccountDeletionEffectOrder pins the full ADR 0022 order, including the
// per-household ADR 0019 effects.
func TestAccountDeletionEffectOrder(t *testing.T) {
	f := newDeletionFixture()
	if err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	requireDeletionCalls(t, f.rec,
		"installation-lock:"+deletionActor,
		"find-account:"+deletionTarget,
		"list-households:"+deletionTarget,
		"lock-household:"+deletionHouseA,
		"lock-household:"+deletionHouseB,
		"lock-identities:"+deletionTarget,
		"revoke-invitations:"+deletionHouseA,
		"revoke-invitations:"+deletionHouseB,
		"lock-account:"+deletionTarget,
		"lock-memberships:"+deletionTarget,
		"count-admins:"+deletionHouseA,
		"find-grant:"+deletionTarget,
		"count-instance-admins:"+deletionTarget,
		"revoke-grant:"+deletionTarget,
		"items:"+deletionHouseA,
		"subjects:"+deletionHouseA,
		"resets:"+deletionHouseA,
		"removal-audit:"+deletionHouseA,
		"delete-membership:"+deletionHouseA,
		"items:"+deletionHouseB,
		"subjects:"+deletionHouseB,
		"resets:"+deletionHouseB,
		"removal-audit:"+deletionHouseB,
		"delete-membership:"+deletionHouseB,
		"account-resets:"+deletionTarget,
		"erase-access:"+deletionTarget,
		"deletion-record:"+deletionTarget,
		"anonymize:"+deletionTarget,
	)
	if !slices.Equal(f.tx.lockedHouseholds, []string{deletionHouseA, deletionHouseB}) {
		t.Fatalf("locked households=%v", f.tx.lockedHouseholds)
	}
	if !slices.Equal(f.tx.deleted, []string{
		deletionHouseA + "/" + deletionTarget,
		deletionHouseB + "/" + deletionTarget,
	}) {
		t.Fatalf("deleted=%v", f.tx.deleted)
	}
	wantRemovals := []MemberRemoval{
		{HouseholdID: deletionHouseA, TargetUserID: deletionTarget, ActorUserID: deletionActor, PreviousRole: RoleAdmin, ActorAuthority: AuthorityInstanceAdmin, RemovedAt: deletionNow},
		{HouseholdID: deletionHouseB, TargetUserID: deletionTarget, ActorUserID: deletionActor, PreviousRole: RoleMember, ActorAuthority: AuthorityInstanceAdmin, RemovedAt: deletionNow},
	}
	if !slices.Equal(f.tx.removals, wantRemovals) {
		t.Fatalf("removals=%+v\nwant=%+v", f.tx.removals, wantRemovals)
	}
	wantRevocation := InstanceAdminRevocation{
		TargetUserID: deletionTarget, ActorUserID: deletionActor, GrantSource: "bootstrap",
		Reason: "account_deletion", GrantedAt: deletionNow.Add(-time.Hour), RevokedAt: deletionNow,
	}
	if !slices.Equal(f.tx.revocations, []InstanceAdminRevocation{wantRevocation}) {
		t.Fatalf("revocations=%+v", f.tx.revocations)
	}
	if !slices.Equal(f.tx.deletions, []AccountDeletion{{TargetUserID: deletionTarget, ActorUserID: deletionActor, DeletedAt: deletionNow}}) {
		t.Fatalf("deletions=%+v", f.tx.deletions)
	}
	if f.account.anonymizedID != deletionTarget || !f.account.anonymizedAt.Equal(deletionNow) {
		t.Fatalf("anonymized=%q at=%s", f.account.anonymizedID, f.account.anonymizedAt)
	}
	if f.repo.attempts != 1 {
		t.Fatalf("transactions=%d", f.repo.attempts)
	}
}

// TestAccountDeletionEvaluationOrder pins that every refusal happens exactly at
// its documented place and that nothing is written afterwards.
func TestAccountDeletionEvaluationOrder(t *testing.T) {
	cases := []struct {
		name      string
		actor     string
		target    string
		confirm   string
		prepare   func(*deletionFixture)
		want      error
		field     string
		code      string
		wantCalls []string
	}{
		{
			name: "malformed actor", actor: "instance-admin", target: deletionTarget, confirm: "alice",
			want: ErrInstanceAdminRequired,
		},
		{
			name: "malformed target", actor: deletionActor, target: "not-a-uuid", confirm: "alice",
			want: &ValidationError{Field: "userId", Code: "invalid_reference"}, field: "userId", code: "invalid_reference",
			wantCalls: []string{"installation-lock:" + deletionActor},
		},
		{
			name: "self in different case", actor: deletionActor, target: strings.ToUpper(deletionActor), confirm: "alice",
			want:      ErrCannotDeleteSelf,
			wantCalls: []string{"installation-lock:" + deletionActor},
		},
		{
			name: "unknown or already deleted account", actor: deletionActor, target: deletionTarget, confirm: "alice",
			prepare:   func(f *deletionFixture) { f.account.findErr = ErrNotFound },
			want:      ErrNotFound,
			wantCalls: []string{"installation-lock:" + deletionActor, "find-account:" + deletionTarget},
		},
		{
			name: "confirmation mismatch", actor: deletionActor, target: deletionTarget, confirm: "Alice",
			want: &ValidationError{Field: "confirmLogin", Code: "mismatch"}, field: "confirmLogin", code: "mismatch",
			wantCalls: []string{
				"installation-lock:" + deletionActor,
				"find-account:" + deletionTarget,
				"list-households:" + deletionTarget,
				"lock-household:" + deletionHouseA,
				"lock-household:" + deletionHouseB,
				"lock-identities:" + deletionTarget,
				"revoke-invitations:" + deletionHouseA,
				"revoke-invitations:" + deletionHouseB,
				"lock-account:" + deletionTarget,
			},
		},
		{
			name: "last household admin", actor: deletionActor, target: deletionTarget, confirm: "alice",
			prepare: func(f *deletionFixture) { f.tx.admins[deletionHouseA] = 0 },
			want:    ErrLastAdmin,
			wantCalls: []string{
				"installation-lock:" + deletionActor,
				"find-account:" + deletionTarget,
				"list-households:" + deletionTarget,
				"lock-household:" + deletionHouseA,
				"lock-household:" + deletionHouseB,
				"lock-identities:" + deletionTarget,
				"revoke-invitations:" + deletionHouseA,
				"revoke-invitations:" + deletionHouseB,
				"lock-account:" + deletionTarget,
				"lock-memberships:" + deletionTarget,
				"count-admins:" + deletionHouseA,
			},
		},
		{
			name: "last instance administrator", actor: deletionActor, target: deletionTarget, confirm: "alice",
			prepare: func(f *deletionFixture) { f.tx.liveAdmins = 0 },
			want:    ErrLastInstanceAdmin,
			wantCalls: []string{
				"installation-lock:" + deletionActor,
				"find-account:" + deletionTarget,
				"list-households:" + deletionTarget,
				"lock-household:" + deletionHouseA,
				"lock-household:" + deletionHouseB,
				"lock-identities:" + deletionTarget,
				"revoke-invitations:" + deletionHouseA,
				"revoke-invitations:" + deletionHouseB,
				"lock-account:" + deletionTarget,
				"lock-memberships:" + deletionTarget,
				"count-admins:" + deletionHouseA,
				"find-grant:" + deletionTarget,
				"count-instance-admins:" + deletionTarget,
			},
		},
		{
			name: "grant check fails", actor: deletionActor, target: deletionTarget, confirm: "alice",
			prepare:   func(f *deletionFixture) { f.tx.requireErr = ErrInstanceAdminRequired },
			want:      ErrInstanceAdminRequired,
			wantCalls: []string{"installation-lock:" + deletionActor},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeletionFixture()
			if tc.prepare != nil {
				tc.prepare(f)
			}
			err := f.service.DeleteAccount(context.Background(), tc.actor, tc.target, tc.confirm)
			if tc.field != "" {
				requireValidation(t, err, tc.field, tc.code)
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			requireDeletionCalls(t, f.rec, tc.wantCalls...)
			requireNoDeletionMutations(t, f.rec)
			if f.repo.attempts != 1 {
				t.Fatalf("transactions=%d", f.repo.attempts)
			}
		})
	}
}

// TestAccountDeletionLowercasesTarget pins that a canonical but upper-case
// identifier addresses the same account while every stored spelling is lower.
func TestAccountDeletionLowercasesTarget(t *testing.T) {
	f := newDeletionFixture()
	actor := strings.ToUpper(deletionActor)
	target := strings.ToUpper(deletionTarget)
	if err := f.service.DeleteAccount(context.Background(), actor, target, "alice"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if !slices.Equal(f.account.findUserIDs, []string{deletionTarget}) || !slices.Equal(f.account.lockUserIDs, []string{deletionTarget}) {
		t.Fatalf("find=%v lock=%v", f.account.findUserIDs, f.account.lockUserIDs)
	}
	if !slices.Equal(f.account.lockIdentitiIDs, []string{deletionTarget}) {
		t.Fatalf("identity locks=%v", f.account.lockIdentitiIDs)
	}
	if !slices.Equal(f.tx.deleted, []string{deletionHouseA + "/" + deletionTarget, deletionHouseB + "/" + deletionTarget}) {
		t.Fatalf("deleted=%v", f.tx.deleted)
	}
	if len(f.tx.removals) != 2 || f.tx.removals[0].ActorUserID != deletionActor || f.tx.removals[0].TargetUserID != deletionTarget {
		t.Fatalf("removals=%+v", f.tx.removals)
	}
	if len(f.tx.revocations) != 1 || f.tx.revocations[0].ActorUserID != deletionActor || f.tx.revocations[0].TargetUserID != deletionTarget {
		t.Fatalf("revocations=%+v", f.tx.revocations)
	}
	if len(f.tx.deletions) != 1 || f.tx.deletions[0].ActorUserID != deletionActor || f.tx.deletions[0].TargetUserID != deletionTarget {
		t.Fatalf("deletions=%+v", f.tx.deletions)
	}
	if f.account.anonymizedID != deletionTarget {
		t.Fatalf("anonymized=%q", f.account.anonymizedID)
	}
}

// TestAccountDeletionConfirmationIsExact pins that confirmLogin is compared
// byte for byte: no case folding, no trimming.
func TestAccountDeletionConfirmationIsExact(t *testing.T) {
	for _, tc := range []struct {
		name    string
		login   string
		confirm string
		ok      bool
	}{
		{name: "exact", login: "alice", confirm: "alice", ok: true},
		{name: "case differs", login: "alice", confirm: "Alice"},
		{name: "trailing space", login: "alice", confirm: "alice "},
		{name: "leading space", login: "alice", confirm: " alice"},
		{name: "inner space", login: "alice smith", confirm: "alice smith", ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeletionFixture()
			f.account.login = tc.login
			err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, tc.confirm)
			if tc.ok {
				if err != nil {
					t.Fatalf("DeleteAccount: %v", err)
				}
				return
			}
			requireValidation(t, err, "confirmLogin", "mismatch")
			requireNoDeletionMutations(t, f.rec)
		})
	}
}

// TestAccountDeletionWithoutGrantSkipsRevoke pins that an account without an
// instance-administrator grant is deleted without a revocation audit row and
// without the live-administrator count.
func TestAccountDeletionWithoutGrantSkipsRevoke(t *testing.T) {
	f := newDeletionFixture()
	f.tx.granted = false
	if err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if f.rec.has("revoke-grant") || f.rec.has("count-instance-admins") {
		t.Fatalf("grant path ran: %v", f.rec.calls)
	}
	if len(f.tx.revocations) != 0 {
		t.Fatalf("revocations=%+v", f.tx.revocations)
	}
	if !f.rec.has("deletion-record") || !f.rec.has("anonymize") {
		t.Fatalf("deletion did not finish: %v", f.rec.calls)
	}
}

// TestAccountDeletionRetriesOnMembershipGrowth pins that a membership set that
// grew between the unlocked household read and the account lock abandons the
// attempt and retries in a fresh transaction.
func TestAccountDeletionRetriesOnMembershipGrowth(t *testing.T) {
	f := newDeletionFixture()
	f.repo.newTx = func(attempt int) *deletionTxFake {
		tx := f.tx.clone()
		if attempt == 1 {
			tx.memberships = append(tx.memberships, AccountMembership{HouseholdID: deletionHouseC, Role: RoleMember})
		}
		return tx
	}
	if err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if f.repo.attempts != 2 || len(f.repo.txs) != 2 {
		t.Fatalf("transactions=%d", f.repo.attempts)
	}
	if f.repo.txs[0] == f.repo.txs[1] {
		t.Fatal("retry reused the abandoned transaction")
	}
	if len(f.repo.txs[0].revocations) != 0 || len(f.repo.txs[0].deletions) != 0 || len(f.repo.txs[0].deleted) != 0 {
		t.Fatalf("abandoned attempt wrote: %+v", f.repo.txs[0])
	}
	if len(f.repo.txs[1].deletions) != 1 || f.repo.txs[1].deletions[0].TargetUserID != deletionTarget {
		t.Fatalf("second attempt did not delete: %+v", f.repo.txs[1])
	}
}

// TestAccountDeletionContendedAfterThreeAttempts pins the retry bound.
func TestAccountDeletionContendedAfterThreeAttempts(t *testing.T) {
	f := newDeletionFixture()
	f.repo.newTx = func(int) *deletionTxFake {
		tx := f.tx.clone()
		tx.memberships = append(tx.memberships, AccountMembership{HouseholdID: deletionHouseC, Role: RoleMember})
		return tx
	}
	err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice")
	if !errors.Is(err, ErrAccountDeletionContended) {
		t.Fatalf("err=%v want=%v", err, ErrAccountDeletionContended)
	}
	if f.repo.attempts != accountDeletionAttempts || len(f.repo.txs) != accountDeletionAttempts {
		t.Fatalf("transactions=%d want=%d", f.repo.attempts, accountDeletionAttempts)
	}
	for i, tx := range f.repo.txs {
		if len(tx.revocations) != 0 || len(tx.deletions) != 0 || len(tx.deleted) != 0 {
			t.Fatalf("attempt %d wrote: %+v", i+1, tx)
		}
	}
	requireNoDeletionMutations(t, f.rec)
}

// TestAccountDeletionRequiresEffects pins that a misconfigured repository is
// refused before any lock or lookup runs.
func TestAccountDeletionRequiresEffects(t *testing.T) {
	for _, tc := range []struct {
		name        string
		breakEffect func(*deletionFixture)
	}{
		{name: "account eraser", breakEffect: func(f *deletionFixture) { f.repo.effects.Account = nil }},
		{name: "invitations", breakEffect: func(f *deletionFixture) { f.repo.effects.Removal.Invitations = nil }},
		{name: "items", breakEffect: func(f *deletionFixture) { f.repo.effects.Removal.Items = nil }},
		{name: "subjects", breakEffect: func(f *deletionFixture) { f.repo.effects.Removal.Subjects = nil }},
		{name: "resets", breakEffect: func(f *deletionFixture) { f.repo.effects.Removal.Resets = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeletionFixture()
			tc.breakEffect(f)
			err := f.service.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice")
			if err == nil {
				t.Fatal("expected an unavailable-effects error")
			}
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrCannotDeleteSelf) {
				t.Fatalf("err=%v", err)
			}
			if len(f.rec.calls) != 0 {
				t.Fatalf("effects check ran after work: %v", f.rec.calls)
			}
			if f.repo.attempts != 1 {
				t.Fatalf("transactions=%d", f.repo.attempts)
			}
		})
	}
}

// TestInstanceAdminCenterDeleteAccount pins the transport-facing delegation and
// its unconfigured failure.
func TestInstanceAdminCenterDeleteAccount(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		var center InstanceAdminCenter
		if err := center.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice"); err == nil {
			t.Fatal("expected an unavailable error")
		}
	})
	t.Run("delegates verbatim", func(t *testing.T) {
		f := newDeletionFixture()
		var center InstanceAdminCenter
		center.WithAccountDeletion(f.service)
		if err := center.DeleteAccount(context.Background(), deletionActor, deletionTarget, "alice"); err != nil {
			t.Fatalf("DeleteAccount: %v", err)
		}
		if f.account.login != "alice" {
			t.Fatalf("login=%q", f.account.login)
		}
		if !f.rec.has("deletion-record:" + deletionTarget) {
			t.Fatalf("delegated call did not delete: %v", f.rec.calls)
		}
	})
}
