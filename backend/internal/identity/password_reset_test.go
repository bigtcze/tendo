package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
)

type passwordResetRepoStub struct {
	record    PasswordResetRecord
	findErr   error
	findCalls int
	txCalls   int
}

func (s *passwordResetRepoStub) WithPasswordResetTransaction(_ context.Context, work func(PasswordResetTransaction) error) error {
	s.txCalls++
	return work(nil)
}
func (s *passwordResetRepoStub) FindPasswordReset(context.Context, []byte) (PasswordResetRecord, error) {
	s.findCalls++
	return s.record, s.findErr
}

func TestPasswordResetRejectsMalformedTokenBeforePersistence(t *testing.T) {
	repository := &passwordResetRepoStub{}
	service, err := NewPasswordResetService(repository, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	hashes := 0
	service.SetPasswordHasher(func(string) (string, error) { hashes++; return "hash", nil })
	// The last token is 43 characters that decodes to 32 bytes but is not the
	// canonical encoding, because its final symbol sets unused trailing bits.
	for _, token := range []string{"", "short", repeat("A", 42), repeat("A", 44), "!" + repeat("A", 42), repeat("A", 42) + "B"} {
		if _, err := service.RedeemPasswordReset(context.Background(), token, "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
			t.Fatalf("token %q error=%v", token, err)
		}
	}
	if repository.findCalls != 0 || hashes != 0 {
		t.Fatalf("malformed token reached persistence: find=%d hashes=%d", repository.findCalls, hashes)
	}
}

func TestPasswordResetUnusablePrefetchSkipsHashing(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	usedAt := now.Add(-time.Minute)
	revokedAt := now.Add(-time.Minute)
	for _, record := range []PasswordResetRecord{
		{CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), UsedAt: &usedAt},
		{CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt},
		{CreatedAt: now.Add(-time.Hour), ExpiresAt: now},
	} {
		repository := &passwordResetRepoStub{record: record}
		service, err := NewPasswordResetService(repository, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		hashes := 0
		service.SetPasswordHasher(func(string) (string, error) { hashes++; return "hash", nil })
		if _, err := service.RedeemPasswordReset(context.Background(), repeat("A", 43), "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
			t.Fatalf("record=%+v error=%v", record, err)
		}
		if repository.findCalls != 1 || repository.txCalls != 0 || hashes != 0 {
			t.Fatalf("record=%+v find=%d tx=%d hashes=%d", record, repository.findCalls, repository.txCalls, hashes)
		}
	}
}

func TestPasswordResetWeakPasswordKeepsTransactionUntouched(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repository := &passwordResetRepoStub{record: PasswordResetRecord{CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	service, err := NewPasswordResetService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	hashes := 0
	service.SetPasswordHasher(func(string) (string, error) { hashes++; return "hash", nil })
	_, err = service.RedeemPasswordReset(context.Background(), repeat("A", 43), "short")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "password" || validation.Code != "invalid_length" {
		t.Fatalf("error=%v", err)
	}
	if hashes != 0 || repository.txCalls != 0 {
		t.Fatalf("weak password hashed or wrote: hashes=%d tx=%d", hashes, repository.txCalls)
	}
}

func TestPasswordResetIssueRejectsMalformedIdentifiers(t *testing.T) {
	repository := &passwordResetRepoStub{}
	service, err := NewPasswordResetService(repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][3]string{
		{"missing", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"},
		{"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "missing", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"},
		{"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "missing"},
	} {
		if _, err := service.IssuePasswordReset(context.Background(), ids[0], ids[1], ids[2]); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("ids=%v error=%v", ids, err)
		}
		if err := service.RevokePasswordReset(context.Background(), ids[0], ids[1], ids[2]); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("revoke ids=%v error=%v", ids, err)
		}
	}
	if repository.txCalls != 0 {
		t.Fatalf("malformed identifiers reached persistence: %d", repository.txCalls)
	}
}

func repeat(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}

// passwordResetTxStub is a full PasswordResetTransaction double for the failure
// paths that only the real repository normally reaches.
type passwordResetTxStub struct {
	record          PasswordResetRecord
	replaceErr      error
	sessionsDeleted bool
	notPermitted    bool
	completed       bool
}

func (s *passwordResetTxStub) RequireAdmin(context.Context, string, string) error { return nil }
func (s *passwordResetTxStub) GetMembership(_ context.Context, userID, _ string) (string, error) {
	if userID == s.record.CreatedByUserID {
		return household.RoleAdmin, nil
	}
	return household.RoleMember, nil
}
func (s *passwordResetTxStub) FindUserLogin(context.Context, string) (string, error) {
	return "target_login", nil
}
func (s *passwordResetTxStub) LockPasswordResetTarget(_ context.Context, userID string) (string, error) {
	return strings.ToLower(userID), nil
}
func (s *passwordResetTxStub) PasswordResetPermitted(context.Context, string, string) (bool, error) {
	return !s.notPermitted, nil
}
func (s *passwordResetTxStub) HasLocalCredential(context.Context, string) (bool, error) {
	return true, nil
}
func (s *passwordResetTxStub) RevokeOutstandingPasswordResets(context.Context, string, time.Time) error {
	return nil
}
func (s *passwordResetTxStub) CreatePasswordReset(context.Context, CreatePasswordResetInput) (PasswordResetRecord, error) {
	return s.record, nil
}
func (s *passwordResetTxStub) FindPasswordReset(context.Context, []byte, bool) (PasswordResetRecord, error) {
	return s.record, nil
}
func (s *passwordResetTxStub) CompletePasswordReset(context.Context, string, time.Time) (int64, error) {
	s.completed = true
	return 1, nil
}
func (s *passwordResetTxStub) LockLocalCredential(context.Context, string) (LocalCredential, error) {
	return LocalCredential{PasswordHash: "old-hash", Version: 1}, nil
}
func (s *passwordResetTxStub) ReplaceLocalPassword(context.Context, string, string, time.Time, MCPRevocation) (Revocation, error) {
	if s.replaceErr != nil {
		return Revocation{}, s.replaceErr
	}
	s.sessionsDeleted = true
	return NewRevocation([][]byte{[]byte("live-session")}), nil
}

type passwordResetTxRepoStub struct {
	record PasswordResetRecord
	tx     PasswordResetTransaction
}

func (s *passwordResetTxRepoStub) WithPasswordResetTransaction(_ context.Context, work func(PasswordResetTransaction) error) error {
	return work(s.tx)
}
func (s *passwordResetTxRepoStub) FindPasswordReset(context.Context, []byte) (PasswordResetRecord, error) {
	return s.record, nil
}

// A reset whose credential row is gone must fail as an invalid link and must
// not report a session revocation, so the whole transaction rolls back
// unconsumed.
func TestPasswordResetVanishedCredentialRollsBack(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := PasswordResetRecord{
		ID: "reset-id", HouseholdID: "house-id", TargetUserID: "target-id", CreatedByUserID: "issuer-id",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	tx := &passwordResetTxStub{record: record, replaceErr: ErrNotFound}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{record: record, tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(string) (string, error) { return "hash:new", nil })
	if _, err = service.RedeemPasswordReset(context.Background(), repeat("A", 43), "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
		t.Fatalf("vanished credential error=%v", err)
	}
	if tx.sessionsDeleted {
		t.Fatal("sessions were reported as revoked without a credential row")
	}
}

// Losing reset authority over the whole account after issue voids the link
// with the uniform invalid answer, before any credential or session write.
func TestPasswordResetRedeemWithoutAccountAuthorityIsInvalid(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := PasswordResetRecord{
		ID: "reset-id", HouseholdID: "house-id", TargetUserID: "target-id", CreatedByUserID: "issuer-id",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	tx := &passwordResetTxStub{record: record, notPermitted: true}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{record: record, tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(string) (string, error) { return "hash:new", nil })
	if _, err = service.RedeemPasswordReset(context.Background(), repeat("A", 43), "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
		t.Fatalf("redeem without account authority error=%v", err)
	}
	if tx.sessionsDeleted || tx.completed {
		t.Fatalf("link consumed without reset authority: sessionsDeleted=%v completed=%v", tx.sessionsDeleted, tx.completed)
	}
	if _, err = service.IssuePasswordReset(context.Background(), "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"); !errors.Is(err, ErrPasswordResetNotPermitted) {
		t.Fatalf("issue without account authority error=%v", err)
	}
}

// The per-target issuance budget is asked only after every authorization check
// has passed, with the canonical account ID, so a refused caller never spends
// a target's budget and a case-variant ID cannot open a second bucket.
func TestPasswordResetIssueAdmitsOnlyAuthorizedIssuance(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	actor, house, target := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B62"
	tx := &passwordResetTxStub{record: PasswordResetRecord{CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, notPermitted: true}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	admit := func(id string) bool { asked = append(asked, id); return false }
	if _, err = service.IssuePasswordResetAdmitted(context.Background(), actor, house, target, admit); !errors.Is(err, ErrPasswordResetNotPermitted) {
		t.Fatalf("refused issue error=%v", err)
	}
	if _, err = service.IssuePasswordResetAdmitted(context.Background(), actor, house, actor, admit); !errors.Is(err, ErrCannotResetSelf) {
		t.Fatalf("self issue error=%v", err)
	}
	if len(asked) != 0 {
		t.Fatalf("refused attempts spent the budget: %v", asked)
	}
	tx.notPermitted = false
	if _, err = service.IssuePasswordResetAdmitted(context.Background(), actor, house, target, admit); !errors.Is(err, ErrPasswordResetThrottled) {
		t.Fatalf("throttled issue error=%v", err)
	}
	if len(asked) != 1 || asked[0] != strings.ToLower(target) {
		t.Fatalf("budget keys=%v, want the canonical target ID once", asked)
	}
	if _, err = service.IssuePasswordResetAdmitted(context.Background(), actor, house, target, func(string) bool { return true }); err != nil {
		t.Fatalf("admitted issue error=%v", err)
	}
}

// The actor's own ID spelled in upper case is still the actor: issue and
// cancel both refuse it as self-targeting before any budget or link write.
func TestPasswordResetSelfCheckIgnoresIDCase(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	actor, house := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	tx := &passwordResetTxStub{record: PasswordResetRecord{CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	admitted := 0
	admit := func(string) bool { admitted++; return true }
	if _, err = service.IssuePasswordResetAdmitted(context.Background(), actor, house, strings.ToUpper(actor), admit); !errors.Is(err, ErrCannotResetSelf) {
		t.Fatalf("upper-case self issue error=%v", err)
	}
	if admitted != 0 {
		t.Fatalf("self issue spent the budget %d time(s)", admitted)
	}
	if err = service.RevokePasswordReset(context.Background(), actor, house, strings.ToUpper(actor)); !errors.Is(err, ErrCannotResetSelf) {
		t.Fatalf("upper-case self cancel error=%v", err)
	}
}
