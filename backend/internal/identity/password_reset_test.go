package identity

import (
	"context"
	"errors"
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
	updateRows      int64
	sessionsDeleted bool
	notPermitted    bool
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
func (s *passwordResetTxStub) LockPasswordResetTarget(context.Context, string) (string, error) {
	return "target_login", nil
}
func (s *passwordResetTxStub) PasswordResetPermitted(context.Context, string, string) (bool, error) {
	return !s.notPermitted, nil
}
func (s *passwordResetTxStub) HasLocalCredential(context.Context, string) (bool, error) {
	return true, nil
}
func (s *passwordResetTxStub) RevokeOutstandingPasswordResetsForTarget(context.Context, string, time.Time) error {
	return nil
}
func (s *passwordResetTxStub) RevokeOutstandingPasswordResetsInHousehold(context.Context, string, string, time.Time) error {
	return nil
}
func (s *passwordResetTxStub) CreatePasswordReset(context.Context, CreatePasswordResetInput) (PasswordResetRecord, error) {
	return s.record, nil
}
func (s *passwordResetTxStub) FindPasswordReset(context.Context, []byte, bool) (PasswordResetRecord, error) {
	return s.record, nil
}
func (s *passwordResetTxStub) CompletePasswordReset(context.Context, string, time.Time) (int64, error) {
	return 1, nil
}
func (s *passwordResetTxStub) LockLocalCredential(context.Context, string) (int64, error) {
	return 1, nil
}
func (s *passwordResetTxStub) UpdateLocalCredential(context.Context, string, string) (int64, error) {
	return s.updateRows, nil
}
func (s *passwordResetTxStub) DeleteUserSessions(context.Context, string) error {
	s.sessionsDeleted = true
	return nil
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

// A reset whose credential update affects no row must fail as an invalid link
// and must not delete sessions, so the whole transaction rolls back unconsumed.
func TestPasswordResetZeroRowCredentialUpdateRollsBack(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := PasswordResetRecord{
		ID: "reset-id", HouseholdID: "house-id", TargetUserID: "target-id", CreatedByUserID: "issuer-id",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	tx := &passwordResetTxStub{record: record}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{record: record, tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(string) (string, error) { return "hash:new", nil })
	if _, err = service.RedeemPasswordReset(context.Background(), repeat("A", 43), "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
		t.Fatalf("zero-row credential update error=%v", err)
	}
	if tx.sessionsDeleted {
		t.Fatal("sessions were deleted without a credential row")
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
	tx := &passwordResetTxStub{record: record, updateRows: 1, notPermitted: true}
	service, err := NewPasswordResetService(&passwordResetTxRepoStub{record: record, tx: tx}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(string) (string, error) { return "hash:new", nil })
	if _, err = service.RedeemPasswordReset(context.Background(), repeat("A", 43), "a long enough new password"); !errors.Is(err, ErrInvalidPasswordReset) {
		t.Fatalf("redeem without account authority error=%v", err)
	}
	if tx.sessionsDeleted {
		t.Fatal("sessions were deleted without reset authority")
	}
	if _, err = service.IssuePasswordReset(context.Background(), "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"); !errors.Is(err, ErrPasswordResetNotPermitted) {
		t.Fatalf("issue without account authority error=%v", err)
	}
}
