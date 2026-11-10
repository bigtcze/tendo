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
	for _, token := range []string{"", "short", repeat("A", 42), repeat("A", 44), "!" + repeat("A", 42)} {
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
