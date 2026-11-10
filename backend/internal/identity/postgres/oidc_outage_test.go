package postgres

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/identity/oidcprovider"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

// TestOIDCProviderOutageDoesNotAffectLocalLogin proves the local credential path
// is independent of the provider: discovery fails, createSession still works.
func TestOIDCProviderOutageDoesNotAffectLocalLogin(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := security.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	var user, house string
	if err = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('local_outage') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1,$2)`, user, passwordHash); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('outage','UTC') RETURNING id::text`).Scan(&house); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1,$2,'admin')`, user, house); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, user, house); err != nil {
		t.Fatal(err)
	}

	repo := jitRepo(app)
	sessions, err := identity.NewSessionService(repo, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	gate := security.NewPasswordGate(2)
	sessions.SetPasswordVerifier(gate.VerifyPassword)
	// A closed port: discovery and token requests fail fast.
	unreachable := "http://127.0.0.1:1"
	provider := oidcprovider.New(unreachable, "client", "secret", &http.Client{Timeout: 500 * time.Millisecond}, time.Now, true)
	oidcService, err := identity.NewOIDCService(repo, provider, unreachable, "client", "Down IdP", "http://tendo.test", time.Now, nil, sessions.VerifyCurrentPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oidcService.Start(ctx, identity.OIDCStartParams{Purpose: identity.OIDCPurposeLogin}); !errors.Is(err, identity.ErrOIDCUnavailable) {
		t.Fatalf("provider outage not reported: %v", err)
	}
	session, err := sessions.Login(ctx, "local_outage", "correct horse battery")
	if err != nil {
		t.Fatalf("local login failed during provider outage: %v", err)
	}
	if session.Principal.UserID != user || session.Principal.DefaultHouseholdID != house || session.Token == "" {
		t.Fatalf("session=%+v", session)
	}
	var sessionsN int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1`, user).Scan(&sessionsN); err != nil || sessionsN != 1 {
		t.Fatalf("sessions=%d err=%v", sessionsN, err)
	}
}
