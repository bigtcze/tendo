package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

// passwordChangeTxStub is the full self-service transaction double. events
// records the shared protocol order so a test can prove reset links are revoked
// before the credential row is locked.
type passwordChangeTxStub struct {
	credential       LocalCredential
	lockErr          error
	replaceErr       error
	revocation       Revocation
	selectActive     bool
	createSessionErr error
	created          []NewSession
	events           *[]string
}

func (s *passwordChangeTxStub) record(event string) {
	if s.events != nil {
		*s.events = append(*s.events, event)
	}
}

func (s *passwordChangeTxStub) RevokeOutstandingPasswordResets(context.Context, string, time.Time) error {
	s.record("revoke-resets")
	return nil
}

func (s *passwordChangeTxStub) LockLocalCredential(context.Context, string) (LocalCredential, error) {
	s.record("lock")
	if s.lockErr != nil {
		return LocalCredential{}, s.lockErr
	}
	return s.credential, nil
}

func (s *passwordChangeTxStub) ReplaceLocalPassword(context.Context, string, string, time.Time) (Revocation, error) {
	s.record("replace")
	if s.replaceErr != nil {
		return Revocation{}, s.replaceErr
	}
	return s.revocation, nil
}

func (s *passwordChangeTxStub) ActiveSessionExists(context.Context, string, []byte, time.Time) (bool, error) {
	return s.selectActive, nil
}

func (s *passwordChangeTxStub) CreateSession(_ context.Context, session NewSession) (string, error) {
	if s.createSessionErr != nil {
		return "", s.createSessionErr
	}
	s.created = append(s.created, session)
	return "session-id", nil
}

type passwordChangeRepoStub struct {
	credential LocalCredential
	readErr    error
	reads      int
	txCalls    int
	tx         *passwordChangeTxStub
}

func (s *passwordChangeRepoStub) ReadLocalCredential(context.Context, string) (LocalCredential, error) {
	s.reads++
	if s.readErr != nil {
		return LocalCredential{}, s.readErr
	}
	return s.credential, nil
}

func (s *passwordChangeRepoStub) WithPasswordChangeTransaction(_ context.Context, work func(PasswordChangeTransaction) error) error {
	s.txCalls++
	return work(s.tx)
}

const (
	testOldPassword = "old local password value"
	testNewPassword = "a brand new local password"
	testSessionA    = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func testPrincipal() Principal {
	return Principal{UserID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", Login: "owner", DefaultHouseholdID: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", InstanceAdmin: true}
}

// newTestPasswordChange builds the service with deterministic hash and verify
// doubles over the fixture's "hash:<password>" values.
func newTestPasswordChange(t *testing.T, repo PasswordChangeRepository, verifier func(string, string) (bool, error)) *PasswordChangeService {
	t.Helper()
	service, err := NewPasswordChangeService(repo, func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(password string) (string, error) { return "hash:" + password, nil })
	if verifier == nil {
		verifier = fakePasswordVerifier
	}
	service.SetPasswordVerifier(verifier)
	return service
}

func fakePasswordVerifier(hash, password string) (bool, error) { return hash == "hash:"+password, nil }

func newChangeInput() ChangePasswordInput {
	return ChangePasswordInput{
		Principal: testPrincipal(), SignInMethod: SignInPassword, SessionToken: testSessionA,
		CurrentPassword: testOldPassword, NewPassword: testNewPassword,
	}
}

func TestChangePasswordRefusesNonLocalSessionsBeforePersistence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input func() ChangePasswordInput
	}{
		{name: "oidc session", input: func() ChangePasswordInput { in := newChangeInput(); in.SignInMethod = SignInOIDC; return in }},
		{name: "legacy session", input: func() ChangePasswordInput { in := newChangeInput(); in.SignInMethod = SignInLegacy; return in }},
		{name: "zero session kind", input: func() ChangePasswordInput { in := newChangeInput(); in.SignInMethod = ""; return in }},
		{name: "malformed user id", input: func() ChangePasswordInput { in := newChangeInput(); in.Principal.UserID = "not-a-uuid"; return in }},
		{name: "malformed session token", input: func() ChangePasswordInput { in := newChangeInput(); in.SessionToken = "short"; return in }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &passwordChangeRepoStub{credential: LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 1}}
			service := newTestPasswordChange(t, repo, nil)
			if _, err := service.ChangePassword(context.Background(), tc.input()); !errors.Is(err, ErrLocalSessionRequired) {
				t.Fatalf("error=%v, want ErrLocalSessionRequired", err)
			}
			if repo.reads != 0 || repo.txCalls != 0 {
				t.Fatalf("refused session reached persistence: reads=%d tx=%d", repo.reads, repo.txCalls)
			}
		})
	}
}

func TestChangePasswordRefusesOIDCSessionEvenWhenAccountHasLocalPassword(t *testing.T) {
	repo := &passwordChangeRepoStub{credential: LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 1}}
	service := newTestPasswordChange(t, repo, nil)
	input := newChangeInput()
	input.SignInMethod = SignInOIDC
	if _, err := service.ChangePassword(context.Background(), input); !errors.Is(err, ErrLocalSessionRequired) {
		t.Fatalf("error=%v", err)
	}
	if repo.txCalls != 0 {
		t.Fatalf("oidc session reached the replacement transaction: %d", repo.txCalls)
	}
}

func TestChangePasswordValidatesNewPasswordBeforeReadingTheCredential(t *testing.T) {
	repo := &passwordChangeRepoStub{credential: LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 1}}
	service := newTestPasswordChange(t, repo, nil)
	input := newChangeInput()
	input.NewPassword = "short"
	_, err := service.ChangePassword(context.Background(), input)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "newPassword" || validation.Code != "invalid_length" {
		t.Fatalf("error=%v", err)
	}
	if repo.reads != 0 || repo.txCalls != 0 {
		t.Fatalf("weak new password reached persistence: reads=%d tx=%d", repo.reads, repo.txCalls)
	}
}

func TestChangePasswordWrongCurrentPasswordKeepsTheSession(t *testing.T) {
	repo := &passwordChangeRepoStub{credential: LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 1}}
	service := newTestPasswordChange(t, repo, nil)
	input := newChangeInput()
	input.CurrentPassword = "the wrong current password"
	if _, err := service.ChangePassword(context.Background(), input); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("error=%v", err)
	}
	if repo.txCalls != 0 {
		t.Fatalf("wrong current password reached the transaction: %d", repo.txCalls)
	}
}

// A corrupt stored hash stays a plain field rejection but is still
// recognizable so handlers can record the integrity fault.
func TestChangePasswordCorruptStoredHashKeepsIntegrityFault(t *testing.T) {
	repo := &passwordChangeRepoStub{credential: LocalCredential{PasswordHash: "not-a-phc-hash", Version: 1}}
	service := newTestPasswordChange(t, repo, func(string, string) (bool, error) {
		return false, security.ErrInvalidPasswordHash
	})
	_, err := service.ChangePassword(context.Background(), newChangeInput())
	if !errors.Is(err, ErrCurrentPasswordIncorrect) || !errors.Is(err, security.ErrInvalidPasswordHash) {
		t.Fatalf("error=%v", err)
	}
}

func TestChangePasswordAccountWithoutLocalCredentialIsForbidden(t *testing.T) {
	repo := &passwordChangeRepoStub{readErr: ErrNotFound}
	service := newTestPasswordChange(t, repo, nil)
	if _, err := service.ChangePassword(context.Background(), newChangeInput()); !errors.Is(err, ErrLocalSessionRequired) {
		t.Fatalf("error=%v", err)
	}
}

// The happy path must revoke outstanding reset links before locking the
// credential, replace the hash, and hand the caller exactly one fresh local
// session while the old one is reported as deleted.
func TestChangePasswordRotatesTheCallersSession(t *testing.T) {
	credential := LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 4}
	var events []string
	repo := &passwordChangeRepoStub{
		credential: credential,
		tx: &passwordChangeTxStub{
			credential: credential,
			revocation: NewRevocation([][]byte{tokenDigest(testSessionA)}),
			events:     &events,
		},
	}
	service := newTestPasswordChange(t, repo, nil)
	session, err := service.ChangePassword(context.Background(), newChangeInput())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"revoke-resets", "lock", "replace"}
	if len(events) != len(want) || events[0] != want[0] || events[1] != want[1] || events[2] != want[2] {
		t.Fatalf("protocol order=%v, want %v", events, want)
	}
	if session.Token == "" || session.Token == testSessionA || !validToken(session.Token) {
		t.Fatalf("rotated token=%q", session.Token)
	}
	if session.SignInMethod != SignInPassword || session.Principal != testPrincipal() || session.ID != "session-id" {
		t.Fatalf("session=%+v", session)
	}
	if len(repo.tx.created) != 1 {
		t.Fatalf("created sessions=%d", len(repo.tx.created))
	}
	created := repo.tx.created[0]
	if created.Method != SignInPassword || created.UserID != testPrincipal().UserID || string(created.TokenHash) != string(tokenDigest(session.Token)) {
		t.Fatalf("created session=%+v", created)
	}
	if created.ExpiresAt != session.ExpiresAt || created.ExpiresAt.Sub(created.CreatedAt) != SessionLifetime {
		t.Fatalf("created lifetime=%v expires=%v session expires=%v", created.ExpiresAt.Sub(created.CreatedAt), created.ExpiresAt, session.ExpiresAt)
	}
}

// A credential that moved between verification and the locked write means the
// caller's current password is stale. The answer depends on whether the caller
// still has a session: one that is gone is signed out, one that remains is told
// the field was wrong.
func TestChangePasswordStaleCredentialAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		active   bool
		wantErr  error
		wantCode string
	}{
		{name: "session survives", active: true, wantErr: ErrCurrentPasswordIncorrect},
		{name: "session is gone", active: false, wantErr: ErrUnauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verified := LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 4}
			repo := &passwordChangeRepoStub{
				credential: verified,
				tx: &passwordChangeTxStub{
					credential:   LocalCredential{PasswordHash: "hash:someone-else", Version: 5},
					selectActive: tc.active,
					revocation:   NewRevocation([][]byte{tokenDigest(testSessionA)}),
				},
			}
			service := newTestPasswordChange(t, repo, nil)
			if _, err := service.ChangePassword(context.Background(), newChangeInput()); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v, want %v", err, tc.wantErr)
			}
			if len(repo.tx.created) != 0 {
				t.Fatalf("stale credential created a session: %d", len(repo.tx.created))
			}
		})
	}
}

// The caller's session disappearing exactly when the replacement runs leaves
// nothing to rotate into.
func TestChangePasswordVanishedOwnSessionIsUnauthenticated(t *testing.T) {
	credential := LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 4}
	repo := &passwordChangeRepoStub{
		credential: credential,
		tx:         &passwordChangeTxStub{credential: credential, revocation: NewRevocation(nil)},
	}
	service := newTestPasswordChange(t, repo, nil)
	if _, err := service.ChangePassword(context.Background(), newChangeInput()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error=%v", err)
	}
	if len(repo.tx.created) != 0 {
		t.Fatal("vanished session was replaced anyway")
	}
}

type adminPasswordRepoStub struct {
	userID    string
	findErr   error
	findCalls int
	txCalls   int
	tx        *passwordChangeTxStub
}

func (s *adminPasswordRepoStub) FindUserIDByLogin(context.Context, string) (string, error) {
	s.findCalls++
	return s.userID, s.findErr
}

func (s *adminPasswordRepoStub) WithCredentialReplacement(_ context.Context, work func(CredentialReplacementTransaction) error) error {
	s.txCalls++
	return work(s.tx)
}

func newAdminPasswordService(t *testing.T, repo AdminPasswordRepository) *AdminPasswordService {
	t.Helper()
	service, err := NewAdminPasswordService(repo, func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(password string) (string, error) { return "hash:" + password, nil })
	return service
}

func TestAdminPasswordResetReplacesCredentialAndReportsSessions(t *testing.T) {
	var events []string
	repo := &adminPasswordRepoStub{
		userID: testPrincipal().UserID,
		tx: &passwordChangeTxStub{
			credential: LocalCredential{PasswordHash: "hash:" + testOldPassword, Version: 2},
			revocation: NewRevocation([][]byte{[]byte("session-1"), []byte("session-2")}),
			events:     &events,
		},
	}
	service := newAdminPasswordService(t, repo)
	reset, err := service.SetPassword(context.Background(), "owner", testNewPassword)
	if err != nil {
		t.Fatal(err)
	}
	if reset.UserID != testPrincipal().UserID || reset.Sessions != 2 {
		t.Fatalf("reset=%+v", reset)
	}
	want := []string{"revoke-resets", "lock", "replace"}
	if len(events) != len(want) || events[0] != want[0] || events[1] != want[1] || events[2] != want[2] {
		t.Fatalf("protocol order=%v, want %v", events, want)
	}
}

func TestAdminPasswordResetErrors(t *testing.T) {
	t.Run("unknown login", func(t *testing.T) {
		repo := &adminPasswordRepoStub{findErr: ErrNotFound}
		service := newAdminPasswordService(t, repo)
		if _, err := service.SetPassword(context.Background(), "nobody", testNewPassword); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error=%v", err)
		}
		if repo.txCalls != 0 {
			t.Fatalf("unknown login reached the transaction: %d", repo.txCalls)
		}
	})
	t.Run("malformed login", func(t *testing.T) {
		repo := &adminPasswordRepoStub{userID: testPrincipal().UserID}
		service := newAdminPasswordService(t, repo)
		if _, err := service.SetPassword(context.Background(), "Not A Login", testNewPassword); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error=%v", err)
		}
		if repo.findCalls != 0 {
			t.Fatalf("malformed login reached persistence: %d", repo.findCalls)
		}
	})
	t.Run("oidc-only account", func(t *testing.T) {
		repo := &adminPasswordRepoStub{
			userID: testPrincipal().UserID,
			tx:     &passwordChangeTxStub{lockErr: ErrNotFound},
		}
		service := newAdminPasswordService(t, repo)
		if _, err := service.SetPassword(context.Background(), "owner", testNewPassword); !errors.Is(err, ErrNoLocalPassword) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("weak password before lookup", func(t *testing.T) {
		repo := &adminPasswordRepoStub{userID: testPrincipal().UserID}
		service := newAdminPasswordService(t, repo)
		_, err := service.SetPassword(context.Background(), "owner", "short")
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != "password" || validation.Code != "invalid_length" {
			t.Fatalf("error=%v", err)
		}
		if repo.findCalls != 0 || repo.txCalls != 0 {
			t.Fatalf("weak password reached persistence: find=%d tx=%d", repo.findCalls, repo.txCalls)
		}
	})
}

func TestValidateLocalPasswordExposesTheSharedPolicy(t *testing.T) {
	if err := ValidateLocalPassword(testNewPassword); err != nil {
		t.Fatalf("valid password: %v", err)
	}
	for _, weak := range []string{"", "short", repeat("a", 14), repeat("a", 129)} {
		if err := ValidateLocalPassword(weak); err == nil {
			t.Fatalf("weak password %q accepted", weak)
		}
	}
}
