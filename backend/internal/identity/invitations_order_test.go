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
	lockedInvitation                                          Invitation
	membershipRole, defaultHousehold                          string
	other                                                     bool
	writeErr                                                  map[string]error
	listRows                                                  []Invitation
	listLimit                                                 int
	now                                                       time.Time
	listHousehold, listCursor                                 string
	createCreated                                             *bool
	createInput                                               CreateInvitationInput
}

type invitationRecordingTx struct{ repo *invitationRecordingRepo }

func (r *invitationRecordingRepo) record(call string) { r.calls = append(r.calls, call) }
func (r *invitationRecordingRepo) FindInvitation(ctx context.Context, digest []byte) (Invitation, error) {
	r.record("preflight.find")
	return r.invitationFake.FindInvitation(ctx, digest)
}
func (r *invitationRecordingRepo) WithInvitationTransaction(_ context.Context, fn func(InvitationTransaction) error) error {
	r.record("tx.begin")
	r.txCalls++
	return fn(&invitationRecordingTx{r})
}
func (t *invitationRecordingTx) fail(name string) error {
	if err := t.repo.writeErr[name]; err != nil {
		return err
	}
	return t.repo.writeErr["*"]
}
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
	t.repo.createInput = in
	if err := t.fail("create"); err != nil {
		return Invitation{}, false, err
	}
	created := true
	if t.repo.createCreated != nil {
		created = *t.repo.createCreated
	}
	if !created {
		return Invitation{ID: testHouse, HouseholdID: in.HouseholdID}, false, nil
	}
	return Invitation{ID: testHouse, HouseholdID: in.HouseholdID, CreatedByUserID: in.ActorID, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt}, true, nil
}
func (t *invitationRecordingTx) ListInvitations(_ context.Context, householdID, cursor string, limit int) ([]Invitation, string, error) {
	t.repo.record("list")
	t.repo.listLimit = limit
	t.repo.listHousehold, t.repo.listCursor = householdID, cursor
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
	if t.repo.lockedInvitation.ID != "" {
		return t.repo.lockedInvitation, nil
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
	if result.UserID != testActor || result.Login != "valid_login" || result.HouseholdID != testHouse || result.Role != household.RoleMember {
		t.Fatalf("result=%+v", result)
	}
	if !repo.now.Equal(invitationInstant.UTC().Truncate(time.Second)) {
		t.Fatalf("now=%v", repo.now)
	}
}

func TestAcceptInvitationWriteFailuresStopAtFailingStep(t *testing.T) {
	for _, failedStep := range []struct {
		name  string
		calls []string
	}{
		{"user.create", []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create"}},
		{"credential.create", []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create", "credential.create"}},
		{"member.add", []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create", "credential.create", "member.add:" + testActor + ":" + testHouse}},
		{"default.set", []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create", "credential.create", "member.add:" + testActor + ":" + testHouse, "default.set:" + testActor + ":" + testHouse}},
		{"invitation.accept", []string{"preflight.find", "password.hash", "tx.begin", "locked.find", "login.find", "user.create", "credential.create", "member.add:" + testActor + ":" + testHouse, "default.set:" + testActor + ":" + testHouse, "invitation.accept:" + testHouse}},
	} {
		t.Run(failedStep.name, func(t *testing.T) {
			cause := errors.New("write failed")
			repo := &invitationRecordingRepo{
				invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
				writeErr:       map[string]error{failedStep.name: cause},
			}
			service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
			service.SetPasswordHasher(func(string) (string, error) { repo.record("password.hash"); return "hash", nil })
			_, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
			if !errors.Is(err, cause) {
				t.Fatalf("error=%v", err)
			}
			assertInvitationCalls(t, repo.calls, failedStep.calls)
		})
	}
}

func TestAcceptInvitationNewAccountPropagatesLookupFailures(t *testing.T) {
	cause := errors.New("login lookup unavailable")
	repo := &invitationRecordingRepo{invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}, loginErr: cause, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	service.SetPasswordHasher(func(string) (string, error) { return "hash", nil })
	_, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"preflight.find", "tx.begin", "locked.find", "login.find"})
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

func TestAlreadyMemberPrecedesDefaultAndOtherHouseholdConflict(t *testing.T) {
	repo := &invitationRecordingRepo{
		invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
		membershipRole: household.RoleOwner, defaultHousehold: "another-house", other: true, writeErr: map[string]error{},
	}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
	if !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "locked.find", "user.lock", "membership"})
}

func TestLockedInvitationMustRemainPending(t *testing.T) {
	repo := &invitationRecordingRepo{invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}, writeErr: map[string]error{}}
	repo.lockedInvitation = Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(-time.Second)}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	service.SetPasswordHasher(func(string) (string, error) { return "hash", nil })
	_, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
	if !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"preflight.find", "tx.begin", "locked.find"})
}

func TestInvitationListUsesBoundedLookaheadAndOwnerAuthorization(t *testing.T) {
	rows := []Invitation{{ID: testActor, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}
	repo := &invitationRecordingRepo{invitationFake: invitationFake{}, listRows: rows, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	items, next, err := service.ListInvitations(context.Background(), testActor, testHouse, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "owner", "list"})
	if repo.listLimit != 101 || repo.listHousehold != testHouse || repo.listCursor != "" || len(items) != 1 || next != "" || items[0].Status != "pending" {
		t.Fatalf("limit=%d items=%+v next=%q", repo.listLimit, items, next)
	}

	repo.calls = nil
	repo.listRows = rows
	items, next, err = service.ListInvitations(context.Background(), testActor, testHouse, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(items) != 1 {
		t.Fatalf("cursor emitted without an extra row: next=%q items=%+v", next, items)
	}
}

func TestRevokeRequiresOwnerBeforeWriteAndUsesSecondPrecision(t *testing.T) {
	cause := errors.New("owner check")
	repo := &invitationRecordingRepo{invitationFake: invitationFake{requireErr: cause}, writeErr: map[string]error{}}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	err := service.RevokeInvitation(context.Background(), testActor, testHouse, testActor)
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"tx.begin", "owner"})
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

func TestNewAccountMalformedTokenPrecedesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := &invitationRecordingRepo{}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	_, err := service.AcceptInvitationNewAccount(ctx, "bad", "valid_login", "long enough password")
	if !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, nil)
}

func TestExistingAccountLockUserErrorsStopAtLock(t *testing.T) {
	infra := errors.New("lock failed")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{{"not found", ErrNotFound, ErrNotFound}, {"infrastructure", infra, infra}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &invitationRecordingRepo{
				invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
				lockUserErr:    tc.err,
			}
			service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
			_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			assertInvitationCalls(t, repo.calls, []string{"tx.begin", "locked.find", "user.lock"})
		})
	}
}

func TestExistingAccountHouseholdConflicts(t *testing.T) {
	for _, tc := range []struct {
		name             string
		defaultHousehold string
		other            bool
	}{{"default household", "another-house", false}, {"other membership", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &invitationRecordingRepo{
				invitationFake:   invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
				defaultHousehold: tc.defaultHousehold, other: tc.other,
			}
			service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
			_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
			if !errors.Is(err, ErrHouseholdConflict) {
				t.Fatalf("error=%v", err)
			}
			assertInvitationCalls(t, repo.calls, []string{"tx.begin", "locked.find", "user.lock", "membership", "other.membership"})
		})
	}
}

func TestExistingAccountMembershipLookupErrorsPropagate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		repo  *invitationRecordingRepo
		calls []string
	}{
		{"membership", &invitationRecordingRepo{membershipErr: errors.New("membership failed")}, []string{"tx.begin", "locked.find", "user.lock", "membership"}},
		{"other membership", &invitationRecordingRepo{otherErr: errors.New("other membership failed")}, []string{"tx.begin", "locked.find", "user.lock", "membership", "other.membership"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.repo.invitationFake = invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}}
			service, _ := NewInvitationService(tc.repo, func() time.Time { return invitationInstant })
			_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
			if err == nil {
				t.Fatal("expected lookup error")
			}
			assertInvitationCalls(t, tc.repo.calls, tc.calls)
		})
	}
}

func TestExistingAccountWriteFailuresStopAtFailingStep(t *testing.T) {
	member := "member.add:" + testActor + ":" + testHouse
	defaultSet := "default.set:" + testActor + ":" + testHouse
	accepted := "invitation.accept:" + testHouse
	for _, tc := range []struct {
		step   string
		writes []string
	}{
		{"member.add", []string{member}},
		{"default.set", []string{member, defaultSet}},
		{"invitation.accept", []string{member, defaultSet, accepted}},
	} {
		t.Run(tc.step, func(t *testing.T) {
			cause := errors.New("write failed")
			repo := &invitationRecordingRepo{
				invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
				writeErr:       map[string]error{tc.step: cause},
			}
			service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
			_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
			if !errors.Is(err, cause) {
				t.Fatalf("error=%v", err)
			}
			calls := append([]string{"tx.begin", "locked.find", "user.lock", "membership", "other.membership"}, tc.writes...)
			assertInvitationCalls(t, repo.calls, calls)
			if tc.step == "invitation.accept" && !repo.now.Equal(invitationInstant.UTC().Truncate(time.Second)) {
				t.Fatalf("now=%v", repo.now)
			}
		})
	}
}

func TestExistingAccountLockedInvitationLookupErrors(t *testing.T) {
	infra := errors.New("lookup failed")
	for _, tc := range []struct{ cause, want error }{{ErrNotFound, ErrInvalidInvitation}, {infra, infra}} {
		repo := &invitationRecordingRepo{lockedErr: tc.cause}
		service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
		_, err := service.AcceptInvitationExistingAccount(context.Background(), testActor, invitationToken)
		if !errors.Is(err, tc.want) {
			t.Fatalf("error=%v want=%v", err, tc.want)
		}
		assertInvitationCalls(t, repo.calls, []string{"tx.begin", "locked.find"})
	}
}

func TestNewAccountLockedInvitationNotFound(t *testing.T) {
	repo := &invitationRecordingRepo{
		invitationFake: invitationFake{inv: Invitation{ID: testHouse, HouseholdID: testHouse, ExpiresAt: invitationInstant.Add(time.Hour)}},
		lockedErr:      ErrNotFound,
	}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	service.SetPasswordHasher(func(string) (string, error) { return "hash", nil })
	_, err := service.AcceptInvitationNewAccount(context.Background(), invitationToken, "valid_login", "long enough password")
	if !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, []string{"preflight.find", "tx.begin", "locked.find"})
}

func TestInvitationListCursorLookaheadAndValidation(t *testing.T) {
	rows := []Invitation{{ID: testActor}, {ID: testHouse}, {ID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b6f"}}
	repo := &invitationRecordingRepo{listRows: rows}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	items, next, err := service.ListInvitations(context.Background(), testActor, testHouse, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].ID != testHouse || next != encodeInvitationCursor(testHouse) {
		t.Fatalf("items=%+v next=%q", items, next)
	}
	cursor := encodeInvitationCursor(testActor)
	_, _, err = service.ListInvitations(context.Background(), testActor, testHouse, 2, cursor)
	if err != nil || repo.listCursor != testActor {
		t.Fatalf("cursor=%q err=%v", repo.listCursor, err)
	}
	_, _, err = service.ListInvitations(context.Background(), testActor, testHouse, 0, "")
	if err != nil || repo.listLimit != 51 {
		t.Fatalf("limit=%d err=%v", repo.listLimit, err)
	}
	repo.calls = nil
	_, _, err = service.ListInvitations(context.Background(), testActor, testHouse, 2, "invalid")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "cursor" || validation.Code != "invalid_format" {
		t.Fatalf("error=%v", err)
	}
	assertInvitationCalls(t, repo.calls, nil)
}

func TestRevokePassesUTCSecondPrecision(t *testing.T) {
	repo := &invitationRecordingRepo{}
	service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
	if err := service.RevokeInvitation(context.Background(), testActor, testHouse, testActor); err != nil {
		t.Fatal(err)
	}
	if !repo.now.Equal(invitationInstant.UTC().Truncate(time.Second)) {
		t.Fatalf("now=%v", repo.now)
	}
}

func TestCreateInvitationRetryAndTokenTiming(t *testing.T) {
	for _, created := range []bool{false, true} {
		repo := &invitationRecordingRepo{createCreated: &created}
		service, _ := NewInvitationService(repo, func() time.Time { return invitationInstant })
		result, err := service.CreateInvitation(context.Background(), testActor, testHouse, "key")
		if err != nil {
			t.Fatal(err)
		}
		if result.Created != created || (created && len(result.Token) != 43) || (!created && result.Token != "") {
			t.Fatalf("created=%v result=%+v", created, result)
		}
		createdAt := invitationInstant.UTC().Truncate(time.Second)
		if !repo.createInput.CreatedAt.Equal(createdAt) || !repo.createInput.ExpiresAt.Equal(createdAt.Add(InvitationLifetime)) {
			t.Fatalf("input=%+v", repo.createInput)
		}
		if created && !result.Invitation.ExpiresAt.Equal(createdAt.Add(InvitationLifetime)) {
			t.Fatalf("invitation=%+v", result.Invitation)
		}
	}
}
