package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func seededInvitationToken(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestOIDCCallbackDecisionRouting(t *testing.T) {
	t.Run("linked identity signs in without a pending row", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "subject-1")
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/" || res.PendingToken != "" || res.SessionToken == "" || len(f.repo.pending) != 0 {
			t.Fatalf("%+v pending=%d", res, len(f.repo.pending))
		}
	})
	t.Run("verified email owner without this issuer auto-attaches", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-2"] = "anna"
		f.repo.emails["user-2"] = "anna@example.test"
		f.repo.ownedEmails["anna@example.test"] = "user-2"
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/" || res.PendingToken != "" || f.repo.identities[identityKey(oidcIssuer, "subject-new")] != "user-2" || len(f.repo.pending) != 0 {
			t.Fatalf("%+v identities=%v pending=%d", res, f.repo.identities, len(f.repo.pending))
		}
	})
	t.Run("matched account already bound to another subject conflicts", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-2"] = "anna"
		f.repo.emails["user-2"] = "anna@example.test"
		f.repo.ownedEmails["anna@example.test"] = "user-2"
		addIdentity(f.repo, "user-2", "subject-old")
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/login#oidcError=identity_conflict" || len(f.repo.pending) != 0 || len(f.repo.sessions) != 0 {
			t.Fatalf("%+v pending=%d sessions=%d", res, len(f.repo.pending), len(f.repo.sessions))
		}
	})
	t.Run("unowned verified email always needs a pending decision", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/login#oidc=continue" || !validToken(res.PendingToken) || len(f.repo.identities) != 0 || len(f.repo.sessions) != 0 {
			t.Fatalf("%+v identities=%d sessions=%d", res, len(f.repo.identities), len(f.repo.sessions))
		}
	})
	t.Run("unverified email is treated as missing", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: false}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/login#oidc=continue" || !validToken(res.PendingToken) {
			t.Fatalf("%+v", res)
		}
	})
}

func TestOIDCCallbackInvitationCarryOver(t *testing.T) {
	t.Run("valid invitation accepted after first-login", func(t *testing.T) {
		f := newOIDCFixture(t)
		token := seededInvitationToken(7)
		f.repo.pendingInvitations[string(digestInvitationToken(token))] = true
		start, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, InvitationToken: token})
		if err != nil {
			t.Fatal(err)
		}
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/login#oidc=continue" || !validToken(res.PendingToken) {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("expired invitation degrades after sign-in", func(t *testing.T) {
		f := newOIDCFixture(t)
		token := seededInvitationToken(8)
		f.repo.pendingInvitations[string(digestInvitationToken(token))] = true
		addIdentity(f.repo, "user-1", "subject-1")
		start, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, InvitationToken: token})
		if err != nil {
			t.Fatal(err)
		}
		delete(f.repo.pendingInvitations, string(digestInvitationToken(token)))
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if res.Destination != "/#oidcNotice=invitation_invalid" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("unknown invitation token is rejected before any flow is stored", func(t *testing.T) {
		f := newOIDCFixture(t)
		_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, InvitationToken: seededInvitationToken(9)})
		if !errors.Is(err, ErrInvalidInvitation) || len(f.repo.flows) != 0 {
			t.Fatalf("err=%v flows=%d", err, len(f.repo.flows))
		}
	})
	t.Run("malformed invitation token is rejected", func(t *testing.T) {
		f := newOIDCFixture(t)
		if _, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, InvitationToken: "short"}); !errors.Is(err, ErrInvalidInvitation) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestOIDCPendingAccountCreation(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
	created, err := f.service.CreatePendingAccount(context.Background(), res.PendingToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Session.Principal.Login, generatedLoginPrefix) || len(created.Session.Principal.Login) != len(generatedLoginPrefix)+8 {
		t.Fatalf("generated login=%q", created.Session.Principal.Login)
	}
	if created.Session.Token == "" || len(f.repo.identities) != 1 || len(f.repo.sessions) != 1 {
		t.Fatalf("session=%+v identities=%d sessions=%d", created.Session, len(f.repo.identities), len(f.repo.sessions))
	}
	if created.Invitation != OIDCInvitationNone {
		t.Fatalf("invitation=%q", created.Invitation)
	}
	// The handle is single-use.
	if _, err := f.service.CreatePendingAccount(context.Background(), res.PendingToken); !errors.Is(err, ErrOIDCPendingNotFound) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestOIDCPendingAccountRetriesLoginCollisions(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
	f.repo.loginCollisions = 2
	if _, err := f.service.CreatePendingAccount(context.Background(), res.PendingToken); err != nil {
		t.Fatalf("expected third attempt to succeed: %v", err)
	}
	f2 := newOIDCFixture(t)
	start2 := f2.start(OIDCPurposeLogin, "", "", "", "")
	res2 := invoke(t, f2.service, start2, callbackParams(startState(t, start2), "code"), "")
	f2.repo.loginCollisions = 3
	if _, err := f2.service.CreatePendingAccount(context.Background(), res2.PendingToken); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestOIDCPendingLink(t *testing.T) {
	t.Run("success records confirmed email and a session", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		f.repo.emails["user-1"] = ""
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		linked, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", oidcTestPassword)
		if err != nil {
			t.Fatal(err)
		}
		if !linked.EmailSaved || linked.Session.Token == "" || linked.Session.Principal.UserID != "user-1" {
			t.Fatalf("%+v", linked)
		}
		if f.repo.emails["user-1"] != "anna@example.test" {
			t.Fatalf("email=%q", f.repo.emails["user-1"])
		}
	})
	t.Run("email owned by another account is skipped", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		f.provider.identity = OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		// Another account claims the address while the user decides.
		f.repo.ownedEmails["anna@example.test"] = "other"
		linked, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", oidcTestPassword)
		if err != nil {
			t.Fatal(err)
		}
		if linked.EmailSaved || linked.Session.Token == "" {
			t.Fatalf("%+v", linked)
		}
	})
	t.Run("wrong password exhausts attempts and deletes the continuation", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		for attempt := 0; attempt < 5; attempt++ {
			if _, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("attempt %d err=%v", attempt, err)
			}
		}
		if _, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", oidcTestPassword); !errors.Is(err, ErrOIDCPendingNotFound) {
			t.Fatalf("err=%v", err)
		}
		if len(f.repo.identities) != 0 {
			t.Fatal("identity bound after failures")
		}
	})
	t.Run("the fifth failure reports exhaustion while staying invalid_credentials", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		for attempt := 1; attempt <= 4; attempt++ {
			if _, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", "wrong password"); !errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrOIDCPendingExhausted) {
				t.Fatalf("attempt %d err=%v", attempt, err)
			}
		}
		_, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", "wrong password")
		if !errors.Is(err, ErrOIDCPendingExhausted) || !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("fifth attempt err=%v", err)
		}
	})
	t.Run("fabricated handle is rejected before any credential work", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		verified := 0
		f.service.verifyPassword = func(string, string) error {
			verified++
			return nil
		}
		fabricated := seededInvitationToken(200)
		for _, password := range []string{"wrong password", oidcTestPassword} {
			if _, err := f.service.LinkPendingAccount(context.Background(), fabricated, "anna", password); !errors.Is(err, ErrOIDCPendingNotFound) {
				t.Fatalf("password %q err=%v", password, err)
			}
		}
		if verified != 0 || f.repo.findLoginCalls != 0 || len(f.repo.pending) != 0 {
			t.Fatalf("credential work performed: verify=%d findLogin=%d pending=%d", verified, f.repo.findLoginCalls, len(f.repo.pending))
		}
	})
	t.Run("unknown login is indistinguishable from a bad password", func(t *testing.T) {
		f := newOIDCFixture(t)
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if _, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "nobody", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("expired or rebound pending cannot be linked", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["user-1"] = "anna"
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		// Advance the clock beyond the continuation lifetime.
		*f.clock = f.clock.Add(OIDCPendingLifetime + time.Second)
		if _, err := f.service.LinkPendingAccount(context.Background(), res.PendingToken, "anna", oidcTestPassword); !errors.Is(err, ErrOIDCPendingNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestOIDCPendingCancelAndRead(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
	status, err := f.service.Pending(context.Background(), res.PendingToken)
	if err != nil || status.ExpiresAt.IsZero() {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if err := f.service.CancelPending(context.Background(), res.PendingToken); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Pending(context.Background(), res.PendingToken); !errors.Is(err, ErrOIDCPendingNotFound) {
		t.Fatalf("err=%v", err)
	}
	// Cancelling an absent continuation is idempotent.
	if err := f.service.CancelPending(context.Background(), res.PendingToken); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Pending(context.Background(), "not-a-token"); !errors.Is(err, ErrOIDCPendingNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestOIDCDiscardAccount(t *testing.T) {
	t.Run("discardable account is deleted and replaced by a continuation", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["jit-user"] = "member-abcdefgh"
		f.repo.emails["jit-user"] = "anna@example.test"
		f.repo.ownedEmails["anna@example.test"] = "jit-user"
		addIdentity(f.repo, "jit-user", "subject-jit")
		handle, status, err := f.service.DiscardAccount(context.Background(), "jit-user")
		if err != nil {
			t.Fatal(err)
		}
		if !validToken(handle) || !status.EmailVerified || len(f.repo.pending) != 1 {
			t.Fatalf("handle=%q status=%+v pending=%d", handle, status, len(f.repo.pending))
		}
		if _, ok := f.repo.accounts["jit-user"]; ok || len(f.repo.identities) != 0 {
			t.Fatal("account or identity survived the discard")
		}
	})
	t.Run("account with a membership or credential is refused", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.repo.accounts["member-user"] = "anna"
		addIdentity(f.repo, "member-user", "subject-1")
		f.repo.memberships["member-user"] = true
		if _, _, err := f.service.DiscardAccount(context.Background(), "member-user"); !errors.Is(err, ErrOIDCAccountNotDiscardable) {
			t.Fatalf("err=%v", err)
		}
		if _, _, err := f.service.DiscardAccount(context.Background(), "user-1"); !errors.Is(err, ErrOIDCAccountNotDiscardable) {
			t.Fatalf("credential err=%v", err)
		}
	})
}
