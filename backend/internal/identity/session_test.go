package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

type fakeSessionRepo struct {
	logins     map[string]LoginRecord
	sessions   map[string]SessionInfo // keyed by string(digest)
	created    []NewSession
	pruned     []string
	prunedAt   []time.Time
	findCalls  int
	deleteCall int
	findErr    error
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{logins: map[string]LoginRecord{}, sessions: map[string]SessionInfo{}}
}

func (f *fakeSessionRepo) FindLogin(_ context.Context, login string) (LoginRecord, error) {
	r, ok := f.logins[login]
	if !ok {
		return LoginRecord{}, ErrNotFound
	}
	return r, nil
}
func (f *fakeSessionRepo) PruneExpired(_ context.Context, userID string, now time.Time) error {
	f.pruned = append(f.pruned, userID)
	f.prunedAt = append(f.prunedAt, now)
	return nil
}
func (f *fakeSessionRepo) CreateSession(_ context.Context, s NewSession) (string, error) {
	f.created = append(f.created, s)
	id := "session-id"
	f.sessions[string(s.TokenHash)] = SessionInfo{ID: id, ExpiresAt: s.ExpiresAt, Principal: Principal{UserID: s.UserID, Login: "owner_1", DefaultHouseholdID: "house-1"}}
	return id, nil
}
func (f *fakeSessionRepo) FindActive(_ context.Context, hash []byte, now time.Time) (SessionInfo, error) {
	f.findCalls++
	if f.findErr != nil {
		return SessionInfo{}, f.findErr
	}
	s, ok := f.sessions[string(hash)]
	if !ok || !s.ExpiresAt.After(now) {
		return SessionInfo{}, ErrNotFound
	}
	return s, nil
}
func (f *fakeSessionRepo) DeleteByTokenHash(_ context.Context, hash []byte) error {
	f.deleteCall++
	delete(f.sessions, string(hash))
	return nil
}

const testPassword = "correct horse battery"

func newSessionFixture(t *testing.T) (*SessionService, *fakeSessionRepo, *time.Time) {
	t.Helper()
	hash, err := security.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeSessionRepo()
	repo.logins["owner_1"] = LoginRecord{Principal: Principal{UserID: "user-1", Login: "owner_1", DefaultHouseholdID: "house-1"}, PasswordHash: hash}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	service, err := NewSessionService(repo, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return service, repo, &now
}

func TestLoginStoresOnlyDigestAndExpiresInThirtyDays(t *testing.T) {
	service, repo, now := newSessionFixture(t)
	session, err := service.Login(context.Background(), "owner_1", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Token) != 43 {
		t.Fatalf("token length %d", len(session.Token))
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d sessions", len(repo.created))
	}
	stored := repo.created[0]
	sum := sha256.Sum256([]byte(session.Token))
	if !bytes.Equal(stored.TokenHash, sum[:]) || bytes.Equal(stored.TokenHash, []byte(session.Token)) {
		t.Fatal("stored value is not the SHA-256 digest of the token")
	}
	if !stored.ExpiresAt.Equal(now.Add(30*24*time.Hour)) || !session.ExpiresAt.Equal(stored.ExpiresAt) || !stored.CreatedAt.Equal(*now) {
		t.Fatalf("created=%v expires=%v", stored.CreatedAt, stored.ExpiresAt)
	}
	if len(repo.pruned) != 1 || repo.pruned[0] != "user-1" || !repo.prunedAt[0].Equal(*now) {
		t.Fatalf("prune calls=%v", repo.pruned)
	}
	if session.Principal.DefaultHouseholdID != "house-1" || session.ID != "session-id" {
		t.Fatalf("session=%+v", session)
	}
	other, err := service.Login(context.Background(), "owner_1", testPassword)
	if err != nil || other.Token == session.Token {
		t.Fatalf("tokens must be unique: err=%v", err)
	}
}

func TestLoginFailuresAreIndistinguishableAndCreateNothing(t *testing.T) {
	service, repo, _ := newSessionFixture(t)
	for name, c := range map[string][2]string{
		"unknown login":  {"nobody_1", testPassword},
		"wrong password": {"owner_1", "another wrong password"},
		"bad login":      {"BAD LOGIN", testPassword},
		"short password": {"owner_1", "short"},
		"huge password":  {"owner_1", strings.Repeat("a", 513)},
		"invalid utf8":   {"owner_1", "\xff\xfe invalid utf8 pass"},
	} {
		_, err := service.Login(context.Background(), c[0], c[1])
		if err != ErrInvalidCredentials {
			t.Errorf("%s: err=%v, want ErrInvalidCredentials", name, err)
		}
	}
	if len(repo.created) != 0 || len(repo.pruned) != 0 {
		t.Fatalf("failed logins touched sessions: %d created %d pruned", len(repo.created), len(repo.pruned))
	}
}

func TestLoginPropagatesRepositoryFailureWithoutCredentialError(t *testing.T) {
	service, repo, _ := newSessionFixture(t)
	delete(repo.logins, "owner_1")
	failing := &failingLoginRepo{fakeSessionRepo: repo}
	service.repository = failing
	_, err := service.Login(context.Background(), "owner_1", testPassword)
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err=%v, want infrastructure error", err)
	}
}

type failingLoginRepo struct{ *fakeSessionRepo }

func (f *failingLoginRepo) FindLogin(context.Context, string) (LoginRecord, error) {
	return LoginRecord{}, errors.New("db down")
}

func TestAuthenticateAndLogout(t *testing.T) {
	service, repo, now := newSessionFixture(t)
	session, err := service.Login(context.Background(), "owner_1", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.Authenticate(context.Background(), session.Token)
	if err != nil || principal.UserID != "user-1" || principal.DefaultHouseholdID != "house-1" || principal.Login != "owner_1" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
	repo.findCalls = 0
	for _, bad := range []string{"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44), strings.Repeat("!", 43), strings.Repeat("a", 42) + "=", strings.Repeat("+", 43), strings.Repeat("a", 43)} {
		if _, err := service.Authenticate(context.Background(), bad); err != ErrUnauthenticated {
			t.Errorf("token %q: err=%v", bad, err)
		}
	}
	// 43 'a' characters decode to 32 bytes only with non-zero trailing bits rejected by strict decoding.
	if repo.findCalls != 0 {
		t.Fatalf("malformed tokens reached the repository %d times", repo.findCalls)
	}
	unknown := strings.Repeat("A", 42) + "Q"
	if _, err := service.Authenticate(context.Background(), unknown); err != ErrUnauthenticated {
		t.Fatalf("unknown token err=%v", err)
	}
	if repo.findCalls != 1 {
		t.Fatalf("well-formed unknown token should query once, got %d", repo.findCalls)
	}
	*now = now.Add(30 * 24 * time.Hour)
	if _, err := service.Authenticate(context.Background(), session.Token); err != ErrUnauthenticated {
		t.Fatalf("session at exact expiry err=%v", err)
	}
	*now = now.Add(-time.Second)
	if _, err := service.Authenticate(context.Background(), session.Token); err != nil {
		t.Fatalf("session one second before expiry err=%v", err)
	}
	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), session.Token); err != ErrUnauthenticated {
		t.Fatalf("revoked session err=%v", err)
	}
	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("second logout: %v", err)
	}
	deletes := repo.deleteCall
	if err := service.Logout(context.Background(), "malformed"); err != nil || repo.deleteCall != deletes {
		t.Fatalf("malformed logout err=%v deletes=%d->%d", err, deletes, repo.deleteCall)
	}
}

func TestAuthenticateInfrastructureErrorIsNotUnauthenticated(t *testing.T) {
	service, repo, _ := newSessionFixture(t)
	repo.findErr = errors.New("db down")
	_, err := service.Authenticate(context.Background(), strings.Repeat("A", 42)+"Q")
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err=%v", err)
	}
}

func TestSubsecondClockIsTruncatedForPersistenceAndLookup(t *testing.T) {
	service, repo, now := newSessionFixture(t)
	*now = time.Date(2026, 1, 2, 3, 4, 5, 987654321, time.FixedZone("x", 3600))
	session, err := service.Login(context.Background(), "owner_1", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 2, 2, 4, 5, 0, time.UTC).Add(SessionLifetime)
	stored := repo.created[0]
	if !session.ExpiresAt.Equal(want) || !stored.ExpiresAt.Equal(want) || stored.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("session=%v stored=%v", session.ExpiresAt, stored)
	}
	info, err := service.Lookup(context.Background(), session.Token)
	if err != nil || !info.ExpiresAt.Equal(session.ExpiresAt) || info.ID != session.ID {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestLoginAllowsPrincipalWithoutDefaultHousehold(t *testing.T) {
	service, repo, _ := newSessionFixture(t)
	record := repo.logins["owner_1"]
	record.Principal.DefaultHouseholdID = ""
	repo.logins["owner_1"] = record
	session, err := service.Login(context.Background(), "owner_1", testPassword)
	if err != nil || session.Principal.DefaultHouseholdID != "" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
}
