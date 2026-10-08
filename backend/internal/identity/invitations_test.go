package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
)

const testActor = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
const testHouse = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"

type invitationFake struct {
	inv                                               Invitation
	findErr, txFindErr, requireErr, loginErr, userErr error
	findCalls, txCalls                                int
	status                                            string
	created, accepted, revoked                        bool
}
type invitationFakeTx struct{ f *invitationFake }

func (f *invitationFake) FindInvitation(context.Context, []byte) (Invitation, error) {
	f.findCalls++
	if f.findErr != nil {
		return Invitation{}, f.findErr
	}
	if f.inv.ID == "" {
		return Invitation{}, ErrNotFound
	}
	return f.inv, nil
}
func (f *invitationFake) WithInvitationTransaction(_ context.Context, fn func(InvitationTransaction) error) error {
	f.txCalls++
	return fn(&invitationFakeTx{f})
}
func (t *invitationFakeTx) RequireOwner(context.Context, string, string) error { return t.f.requireErr }
func (t *invitationFakeTx) GetMembership(context.Context, string, string) (string, error) {
	if t.f.status == "member" || t.f.status == "owner" {
		return t.f.status, nil
	}
	return "", household.ErrNotFound
}
func (t *invitationFakeTx) CreateInvitation(_ context.Context, in CreateInvitationInput) (Invitation, bool, error) {
	if t.f.inv.ID != "" {
		return t.f.inv, false, nil
	}
	t.f.inv = Invitation{ID: testHouse, HouseholdID: in.HouseholdID, CreatedByUserID: in.ActorID, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt}
	t.f.created = true
	return t.f.inv, true, nil
}
func (t *invitationFakeTx) ListInvitations(context.Context, string, string, int) ([]Invitation, string, error) {
	return nil, "", nil
}
func (t *invitationFakeTx) RevokeInvitation(context.Context, string, string, time.Time) error {
	return nil
}
func (t *invitationFakeTx) FindInvitation(context.Context, []byte, bool) (Invitation, error) {
	if t.f.txFindErr != nil {
		return Invitation{}, t.f.txFindErr
	}
	if t.f.inv.ID == "" {
		return Invitation{}, ErrNotFound
	}
	return t.f.inv, nil
}
func (t *invitationFakeTx) FindLogin(context.Context, string) error          { return t.f.loginErr }
func (t *invitationFakeTx) LockUser(context.Context, string) (string, error) { return "", nil }
func (t *invitationFakeTx) HasOtherMembership(context.Context, string, string) (bool, error) {
	return false, nil
}
func (t *invitationFakeTx) CreateUser(context.Context, string) (string, error) {
	if t.f.userErr != nil {
		return "", t.f.userErr
	}
	return testActor, nil
}
func (*invitationFakeTx) CreateCredential(context.Context, string, string) error    { return nil }
func (*invitationFakeTx) AddInvitedMember(context.Context, string, string) error    { return nil }
func (*invitationFakeTx) SetDefaultHousehold(context.Context, string, string) error { return nil }
func (t *invitationFakeTx) AcceptInvitation(_ context.Context, _ string, now time.Time) error {
	t.f.accepted = true
	t.f.inv.AcceptedAt = &now
	return nil
}
func TestInvitationTokenCanonicalFormat(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if !validInvitationToken(token) {
		t.Fatal("canonical rejected")
	}
	for _, bad := range []string{token + "=", token[:42] + "!", token[:42]} {
		if validInvitationToken(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}
func TestInvitationLifetimeAndStatusBoundaries(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expires := now.Add(InvitationLifetime)
	if InvitationLifetime != 7*24*time.Hour {
		t.Fatal(InvitationLifetime)
	}
	i := Invitation{ExpiresAt: expires}
	if invitationStatus(i, expires.Add(-time.Second)) != "pending" || invitationStatus(i, expires) != "expired" {
		t.Fatal("expiry boundary")
	}
	i.AcceptedAt = &now
	i.RevokedAt = &now
	if invitationStatus(i, now) != "accepted" {
		t.Fatal("accepted precedence")
	}
	i.AcceptedAt = nil
	if invitationStatus(i, now) != "revoked" {
		t.Fatal("revoked precedence")
	}
}
func TestCreateTokenOnlyForNewInvitationAndOwnerBeforeRetry(t *testing.T) {
	f := &invitationFake{}
	s, _ := NewInvitationService(f, nil)
	r, e := s.CreateInvitation(context.Background(), testActor, testHouse, "key")
	if e != nil || !r.Created || len(r.Token) != 43 {
		t.Fatalf("new=%+v err=%v", r, e)
	}
	retry, e := s.CreateInvitation(context.Background(), testActor, testHouse, "key")
	if e != nil || retry.Created || retry.Token != "" || retry.Invitation.ID != r.Invitation.ID {
		t.Fatalf("retry=%+v err=%v", retry, e)
	}
	f.requireErr = household.ErrForbidden
	if _, e = s.CreateInvitation(context.Background(), testActor, testHouse, "key"); !errors.Is(e, household.ErrForbidden) {
		t.Fatal(e)
	}
}
func TestCreateInvitationValidationErrors(t *testing.T) {
	s, _ := NewInvitationService(&invitationFake{}, nil)
	if _, e := s.CreateInvitation(context.Background(), "bad", testHouse, "k"); !errors.Is(e, household.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.CreateInvitation(context.Background(), testActor, testHouse, ""); !errors.Is(e, ErrIdempotencyKeyRequired) {
		t.Fatal(e)
	}
	if _, e := s.CreateInvitation(context.Background(), testActor, testHouse, "with space"); !errors.Is(e, ErrInvalidIdempotencyKey) {
		t.Fatal(e)
	}
}
func TestInvitationPreflightInfraPropagates(t *testing.T) {
	cause := errors.New("database unavailable")
	f := &invitationFake{findErr: cause}
	s, _ := NewInvitationService(f, nil)
	tok := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, e := s.AcceptInvitationNewAccount(context.Background(), tok, "valid_login", "a sufficiently long password")
	if !errors.Is(e, cause) || errors.Is(e, ErrInvalidInvitation) {
		t.Fatalf("error=%v", e)
	}
}
func TestMalformedTokenDoesNotLookupOrHash(t *testing.T) {
	f := &invitationFake{}
	s, _ := NewInvitationService(f, nil)
	_, e := s.AcceptInvitationNewAccount(context.Background(), "bad", "x", "short")
	if !errors.Is(e, ErrInvalidInvitation) || f.findCalls != 0 || f.txCalls != 0 {
		t.Fatalf("error=%v find=%d tx=%d", e, f.findCalls, f.txCalls)
	}
}
func TestAcceptCredentialValidationMatchesSetup(t *testing.T) {
	s, _ := NewInvitationService(&invitationFake{}, nil)
	tok := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, e := s.AcceptInvitationNewAccount(context.Background(), tok, "INVALID", "a sufficiently long password")
	var v *ValidationError
	if !errors.As(e, &v) || v.Field != "login" || v.Code != "invalid_format" {
		t.Fatalf("login=%v", e)
	}
	_, e = s.AcceptInvitationNewAccount(context.Background(), tok, "valid_login", "short")
	if !errors.As(e, &v) || v.Field != "password" || v.Code != "invalid_length" {
		t.Fatalf("password=%v", e)
	}
}
func TestCreationKeyVisibleASCII(t *testing.T) {
	for _, tc := range []struct {
		k  string
		ok bool
	}{{"key", true}, {"", false}, {"has space", false}, {"é", false}} {
		if ValidCreationKey(tc.k) != tc.ok {
			t.Errorf("%q", tc.k)
		}
	}
}
