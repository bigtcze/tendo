package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
)

// invitationRecordingRepo records service/repository boundaries and transaction
// operations so acceptance ordering remains explicit during extraction.
type invitationRecordingRepo struct {
	invitationFake
	calls                                                     []string
	clock                                                     func() time.Time
	loginErr, lockedErr, lockUserErr, membershipErr, otherErr error
	membershipRole, defaultHousehold                          string
	other                                                     bool
	writeErr                                                  map[string]error
	listRows                                                  []Invitation
	listLimit                                                 int
	now                                                       time.Time
}

type invitationRecordingTx struct{ repo *invitationRecordingRepo }

func (r *invitationRecordingRepo) record(call string) { r.calls = append(r.calls, call) }
func (r *invitationRecordingRepo) FindInvitation(ctx context.Context, digest []byte) (Invitation, error) {
	r.record("preflight.find")
	return r.invitationFake.FindInvitation(ctx, digest)
}
func (r *invitationRecordingRepo) WithInvitationTransaction(ctx context.Context, fn func(InvitationTransaction) error) error {
	r.record("tx.begin")
	r.txCalls++
	return fn(&invitationRecordingTx{r})
}
func (t *invitationRecordingTx) fail(name string) error { return t.repo.writeErr[name] }
func (t *invitationRecordingTx) RequireOwner(context.Context, string, string) error {
	t.repo.record("owner")
	return t.repo.requireErr
}
func (t *invitationRecordingTx) GetMembership(context.Context, string, string) (string, error) {
	t.repo.record("membership")
	if t.repo.membershipErr != nil {
		return "", t.repo.membershipErr
	}
	if t.repo.membershipRole != "" {
		return t.repo.membershipRole, nil
	}
	return "", household.ErrNotFound
}
func (t *invitationRecordingTx) CreateInvitation(_ context.Context, in CreateInvitationInput) (Invitation, bool, error) {
	t.repo.record("create")
	if err := t.fail("create"); err != nil {
		return Invitation{}, false, err
	}
	if t.repo.invitationFake.inv.ID != "" {
		return t.repo.invitationFake.inv, false, nil
	}
	return Invitation{ID: testHouse, HouseholdID: in.HouseholdID, CreatedByUserID: in.ActorID, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt}, true, nil
}
func (t *invitationRecordingTx) ListInvitations(_ context.Context, _, _ string, limit int) ([]Invitation, string, error) {
	t.repo.record("list")
	t.repo.listLimit = limit
	return t.repo.listRows, "", nil
}
func (t *invitationRecordingTx) RevokeInvitation(_ context.Context, _, _ string, now time.Time) error {
	t.repo.record("revoke")
	t.repo.now = now
	return t.fail("revoke")
}
func (t *invitationRecordingTx) FindInvitation(_ context.Context, _ []byte, _ bool) (Invitation, error) {
	t.repo.record("locked.find")
	if t.repo.lockedErr != nil {
		return Invitation{}, t.repo.lockedErr
	}
	return t.repo.invitationFake.inv, nil
}
func (t *invitationRecordingTx) FindLogin(context.Context, string) error {
	t.repo.record("login.find")
	if t.repo.loginErr != nil {
		return t.repo.loginErr
	}
	return ErrNotFound
}
func (t *invitationRecordingTx) LockUser(context.Context, string) (string, error) {
	t.repo.record("user.lock")
	if t.repo.lockUserErr != nil {
		return "", t.repo.lockUserErr
	}
	return t.repo.defaultHousehold, nil
}
func (t *invitationRecordingTx) HasOtherMembership(context.Context, string, string) (bool, error) {
	t.repo.record("other.membership")
	return t.repo.other, t.repo.otherErr
}
func (t *invitationRecordingTx) CreateUser(context.Context, string) (string, error) {
	t.repo.record("user.create")
	if err := t.fail("user.create"); err != nil {
		return "", err
	}
	return testActor, nil
}
func (t *invitationRecordingTx) CreateCredential(context.Context, string, string) error {
	t.repo.record("credential.create")
	return t.fail("credential.create")
}
func (t *invitationRecordingTx) AddInvitedMember(_ context.Context, userID, householdID string) error {
	t.repo.record("member.add:" + userID + ":" + householdID)
	return t.fail("member.add")
}
func (t *invitationRecordingTx) SetDefaultHousehold(_ context.Context, userID, householdID string) error {
	t.repo.record("default.set:" + userID + ":" + householdID)
	return t.fail("default.set")
}
func (t *invitationRecordingTx) AcceptInvitation(_ context.Context, id string, now time.Time) error {
	t.repo.record("invitation.accept:" + id)
	t.repo.now = now
	return t.fail("invitation.accept")
}

var invitationToken = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
var invitationInstant = time.Date(2026, 1, 2, 3, 4, 5, 987654321, time.FixedZone("test", 3600))

func assertInvitationCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls=%v want %v", got, want)
	}
}

func TestAcceptInvitationNewAccountOrderedEffects(t *testing.T) {
	invitation := Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}
	repo := &invitationRecordingRepo{invitationFake: invitationFake{inv: invitation}, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	service.SetPasswordHasher(func(string) (string, error) { repo.record("password.hash"); return "hash", nil })
	result, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
	if err != nil {
		t.Fatal(err)
	}
	assertInvitationCalls(t, repo.calls, []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create", "credential.create", "member.add:" + testActor + ":" + testHouse, "default.set:" + testActor + ":" + testHouse, "invitation.accept:" + testHouse})
	if result.UserID != testActor || result.Login != "valid_login" || result.HouseholdID != testHouse || result.Role != household.RoleMember || !repo.now.Equal(invitationInstant.UTC().Truncate(time.Second)) {
		t.Fatalf("result=%+v now=%v", result, repo.now)
	}
}

func TestAcceptInvitationNewAccountStopsBeforeWritesOnLoginConflict(t *testing.T) {
	repo := &invitationRecordingRepo{invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}, loginErr: ErrLoginUnavailable, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	service.SetPasswordHasher(func(string) (string, error) { return "hash", nil })
	_, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
	if !errors.Is(err, ErrLoginUnavailable) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"preflight.find", "tx.begin", "locked.find", "login.find"})
}

func TestAcceptInvitationExistingAccountOrderedEffects(t *testing.T) {
	repo := &invitationRecordingRepo{invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	result, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
	if err != nil {
		t.Fatal(err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "locked.find", "user.lock", "membership", "other.membership", "member.add:" + testActor + ":" + testHouse, "default.set:" + testActor + ":" + testHouse, "invitation.accept:" + testHouse})
	if result.UserID != testActor || result.Login != "" || result.HouseholdID != testHouse || result.Role != household.RoleMember {
		t.Fatalf("result=%+v", result)
	}
}

func TestInvitationAdminOrderingAndPaginationBound(t *testing.T) {
	expires := invitationInstant.Add(time.Hour)
	rows := []Invitation{{ID: testActor, HouseholdID: testHouse, ExpiresAt: expires}, {ID: testHouse, HouseholdID: testHouse, ExpiresAt: expires}}
	repo := &invitationRecordingRepo{invitationFake: invitationFake{}, listRows: rows, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	items, next, err := service.ListInvitations(context.Background(), testActor, testHouse, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "owner", "list"})
	if repo.listLimit != 2 || len(items) != 1 || next != encodeInvitationCursor(testActor) || items[0].Status != "pending" {
		t.Fatalf("limit=%d items=%+v next=%q", repo.listLimit, items, next)
	}
}

func TestRevokeRequiresOwnerBeforeWriteAndUsesSecondPrecision(t *testing.T) {
	repo := &invitationRecordingRepo{invitationFake: invitationFake{}, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	if err := service.RevokeInvitation(context.Background(), testActor, testHouse, testActor); err != nil {
		t.Fatal(err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "owner", "revoke"})
	if !repo.now.Equal(invitationInstant.UTC().Truncate(time.Second)) {
		t.Fatalf("now=%v", repo.now)
	}
}

func TestCreateChecksOwnerBeforePersisting(t *testing.T) {
	cause := errors.New("owner check")
	repo := &invitationRecordingRepo{invitationFake: invitationFake{requireErr: cause}, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	_, err := service.CreateInvitation(context.Background(), testActor, testHouse, "key")
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "owner"})
}
