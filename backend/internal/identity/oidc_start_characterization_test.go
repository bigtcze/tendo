package identity

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestOIDCStartCredentialLookupAndDummyVerification(t *testing.T) {
	f := newOIDCFixture(t)
	calls := 0
	f.service.verifyPassword = func(hash, _ string) error {
		calls++
		if hash != f.service.dummyPasswordHash {
			t.Fatalf("not dummy hash")
		}
		return nil
	}
	delete(f.repo.credentials, "user-1")
	_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink, Principal: Principal{UserID: "user-1"}, Session: SessionInfo{ID: "s", Principal: Principal{UserID: "user-1"}}, CurrentPassword: oidcTestPassword})
	if !errors.Is(err, ErrInvalidCredentials) || calls != 1 {
		t.Fatalf("err=%v dummy calls=%d", err, calls)
	}
}

func TestOIDCStartRandomProviderAndInsertFailures(t *testing.T) {
	t.Run("random", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.service.random = func([]byte) error { return errors.New("random") }
		_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin})
		if !errors.Is(err, ErrOIDCUnavailable) || f.repo.inserted != 0 {
			t.Fatalf("err=%v inserted=%d", err, f.repo.inserted)
		}
	})
	t.Run("insert", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.insertErr = errors.New("db")
		_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin})
		if err == nil || errors.Is(err, ErrOIDCUnavailable) || f.repo.inserted != 0 {
			t.Fatalf("err=%v inserted=%d", err, f.repo.inserted)
		}
	})
}

func TestOIDCStartAuthorizationErrorsUnavailableWithoutInsertingFlow(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plain", errors.New("provider down")},
		{"wrapped provider unavailable", fmt.Errorf("authorization: %w", ErrOIDCProviderUnavailable)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.provider.authErr = tc.err
			result, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin})
			if !errors.Is(err, ErrOIDCUnavailable) || result != (OIDCStartResult{}) || f.repo.inserted != 0 || len(f.repo.flows) != 0 {
				t.Fatalf("result=%+v err=%v inserted=%d flows=%d", result, err, f.repo.inserted, len(f.repo.flows))
			}
		})
	}
}

func TestOIDCStartStoredFlowMetadataAndPreviousToken(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "invalid"}[valid], func(t *testing.T) {
			f := newOIDCFixture(t)
			prev := "not-a-token"
			if valid {
				prev = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			}
			_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, PreviousBrowserToken: prev})
			if err != nil {
				t.Fatal(err)
			}
			flow := onlyFlow(t, f.repo)
			if !flow.CreatedAt.Equal(flow.CreatedAt.UTC().Truncate(time.Second)) || !flow.ExpiresAt.Equal(flow.CreatedAt.Add(10*time.Minute)) {
				t.Fatalf("timestamps=%v / %v", flow.CreatedAt, flow.ExpiresAt)
			}
			if (len(flow.PreviousBrowserTokenHash) == 32) != valid {
				t.Fatalf("previous hash length=%d", len(flow.PreviousBrowserTokenHash))
			}
		})
	}
}
