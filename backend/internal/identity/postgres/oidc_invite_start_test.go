package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

const inviteStartIssuer = "https://idp.test"
const inviteStartClient = "client"

type staticOIDCProvider struct{}

func (staticOIDCProvider) AuthorizationURL(_ context.Context, a identity.OIDCAuthorizationParams) (string, error) {
	return "https://idp.test/authorize?state=" + url.QueryEscape(a.State), nil
}

func (staticOIDCProvider) Exchange(context.Context, identity.OIDCExchangeParams) (identity.OIDCVerifiedIdentity, error) {
	return identity.OIDCVerifiedIdentity{}, identity.ErrOIDCProviderUnavailable
}

// TestOIDCInviteStartPersistsHashWithRuntimeRole is the regression test for the
// missing runtime grant: Start must persist the invitation digest through the
// generated INSERT as the restricted `tendo` role, and ConsumeFlow must read it
// back.
func TestOIDCInviteStartPersistsHashWithRuntimeRole(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, house := invitationFixture(t, ctx, admin)
	now := time.Now().UTC().Truncate(time.Second)
	rawInvite := make([]byte, 32)
	for i := range rawInvite {
		rawInvite[i] = 77
	}
	inviteToken := base64.RawURLEncoding.EncodeToString(rawInvite)
	inviteHash := sha256.Sum256([]byte(inviteToken))
	if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		SELECT $1,id,'invite-start',$2,$3,$4 FROM user_accounts LIMIT 1`, house, inviteHash[:], now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	repo := NewWithInvitations(app, nil, memberPeopleFactory, invitationFactory)
	sessions, err := identity.NewSessionService(repo, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPasswordVerifier(security.NewPasswordGate(2).VerifyPassword)
	service, err := identity.NewOIDCService(repo, staticOIDCProvider{}, inviteStartIssuer, inviteStartClient, "Test", "http://tendo.test", time.Now, nil, sessions.VerifyCurrentPassword)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Start(ctx, identity.OIDCStartParams{Purpose: identity.OIDCPurposeLogin, InvitationToken: inviteToken})
	if err != nil {
		t.Fatalf("invite-carrying start failed: %v", err)
	}
	var stored []byte
	if err := admin.QueryRow(ctx, `SELECT invite_token_hash FROM oidc_flows`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(inviteHash[:]) {
		t.Fatalf("stored hash=%x want %x", stored, inviteHash)
	}
	authURL, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	stateRaw, err := base64.RawURLEncoding.DecodeString(authURL.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	browserRaw, err := base64.RawURLEncoding.DecodeString(result.BrowserToken)
	if err != nil {
		t.Fatal(err)
	}
	stateHash, browserHash := sha256.Sum256(stateRaw), sha256.Sum256(browserRaw)
	flow, err := repo.ConsumeFlow(ctx, stateHash[:], browserHash[:], now, inviteStartIssuer, inviteStartClient)
	if err != nil {
		t.Fatalf("consume flow: %v", err)
	}
	if string(flow.InviteTokenHash) != string(inviteHash[:]) {
		t.Fatalf("consumed invite hash=%x want %x", flow.InviteTokenHash, inviteHash)
	}
}
