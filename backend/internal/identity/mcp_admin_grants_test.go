package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Slice B1b3 of #457: the administrator grant list and revocations, against
// the in-memory policy repository, which logs every lock in order.

// adminGrantID is a canonical lowercase uuidv7-shaped grant id that sorts in
// creation order.
func adminGrantID(n int) string {
	return fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-6d3f2c1a%04x", n)
}

// seedPolicyGrant adds a grant of user to the repository, live until expires.
func (r *policyRepo) seedPolicyGrant(id, user string, expires time.Time) {
	r.grants[id] = MCPGrantRecord{
		ID: id, UserID: user, ClientID: "client", Scopes: []string{MCPScopeRead},
		CreatedAt: policyNow.Add(-time.Hour), ExpiresAt: expires,
	}
}

func (r *policyRepo) ListLiveGrants(_ context.Context, userID string, now time.Time, afterID string, limit int) ([]MCPGrantItem, error) {
	if err := r.step("listGrants"); err != nil {
		return nil, err
	}
	var ids []string
	for id, g := range r.grants {
		if g.UserID == userID && !g.Revoked && g.ExpiresAt.After(now) && (afterID == "" || id < afterID) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	var out []MCPGrantItem
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		g := r.grants[id]
		out = append(out, MCPGrantItem{ID: id, ClientName: "App", Scopes: g.Scopes, CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt})
	}
	return out, nil
}

func (r *policyRepo) FindGrantOwner(_ context.Context, grantID string) (string, error) {
	if err := r.step("findOwner"); err != nil {
		return "", err
	}
	g, ok := r.grants[grantID]
	if !ok {
		return "", ErrNotFound
	}
	return g.UserID, nil
}

func (r *policyRepo) LockAccountForGrants(_ context.Context, userID string) error {
	if err := r.step("lockAccountForGrants"); err != nil {
		return err
	}
	if !r.accounts[userID] {
		return ErrNotFound
	}
	return nil
}

func (r *policyRepo) LockGrant(_ context.Context, grantID string) (MCPGrantRecord, error) {
	if err := r.step("lockGrant"); err != nil {
		return MCPGrantRecord{}, err
	}
	g, ok := r.grants[grantID]
	if !ok {
		return MCPGrantRecord{}, ErrNotFound
	}
	return g, nil
}

func (r *policyRepo) RevokeGrant(_ context.Context, grantID string, _ time.Time, revocation MCPRevocation) error {
	if err := r.step("revokeGrant"); err != nil {
		return err
	}
	g := r.grants[grantID]
	g.Revoked = true
	r.grants[grantID] = g
	r.revocations[grantID] = revocation
	return nil
}

func (r *policyRepo) RevokeAccountGrants(_ context.Context, userID string, _ time.Time, revocation MCPRevocation) error {
	if err := r.step("revokeAccount"); err != nil {
		return err
	}
	r.accountRevocations[userID] = revocation
	for id, g := range r.grants {
		if g.UserID == userID && !g.Revoked {
			g.Revoked = true
			r.grants[id] = g
			r.revocations[id] = revocation
		}
	}
	return nil
}

func TestMCPAdminGrantsRefuseNonAdminFirst(t *testing.T) {
	ctx := context.Background()
	for name, run := range map[string]func(*MCPPolicyService) error{
		"list": func(s *MCPPolicyService) error {
			_, err := s.AccountGrants(ctx, policyPeerID, policyTargetID, 0, "")
			return err
		},
		"revoke": func(s *MCPPolicyService) error {
			return s.RevokeAccountGrant(ctx, policyPeerID, policyTargetID, adminGrantID(1))
		},
		"revoke all": func(s *MCPPolicyService) error {
			return s.RevokeAccountGrants(ctx, policyPeerID, policyTargetID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := newPolicyRepo()
			repo.seedPolicyGrant(adminGrantID(1), policyTargetID, policyNow.Add(time.Hour))
			if err := run(newPolicyService(t, repo, true)); !errors.Is(err, ErrInstanceAdminRequired) {
				t.Fatalf("err=%v want ErrInstanceAdminRequired", err)
			}
			policyAssertLog(t, repo.log, "tx", "admin")
			if len(repo.revocations) != 0 {
				t.Fatal("a non-administrator revoked a grant")
			}
		})
	}
}

// TestMCPAdminGrantsMalformedIDsAreNotFoundWithoutLookup: after the
// administrator check, a malformed account or grant id is ErrNotFound without
// any read or lock.
func TestMCPAdminGrantsMalformedIDsAreNotFoundWithoutLookup(t *testing.T) {
	ctx := context.Background()
	bad := []string{"", "not-a-uuid", strings.ToUpper(policyTargetID), strings.ReplaceAll(policyTargetID, "-", "")}
	for _, id := range bad {
		repo := newPolicyRepo()
		svc := newPolicyService(t, repo, true)
		if _, err := svc.AccountGrants(ctx, policyAdminID, id, 0, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("list %q err=%v", id, err)
		}
		if err := svc.RevokeAccountGrant(ctx, policyAdminID, id, adminGrantID(1)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoke account %q err=%v", id, err)
		}
		if err := svc.RevokeAccountGrants(ctx, policyAdminID, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoke all %q err=%v", id, err)
		}
		if err := svc.RevokeAccountGrant(ctx, policyAdminID, policyTargetID, strings.ToUpper(adminGrantID(1))); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoke uppercase grant err=%v", err)
		}
		policyAssertLog(t, repo.log, "tx", "admin", "tx", "admin", "tx", "admin", "tx", "admin")
	}
}

func TestMCPAdminGrantsListPagesLiveGrantsOfTheAccount(t *testing.T) {
	repo := newPolicyRepo()
	for n := 0; n < 60; n++ {
		repo.seedPolicyGrant(adminGrantID(n), policyTargetID, policyNow.Add(time.Hour))
	}
	repo.seedPolicyGrant(adminGrantID(100), policyTargetID, policyNow) // expired
	repo.seedPolicyGrant(adminGrantID(101), policyPeerID, policyNow.Add(time.Hour))
	revoked := adminGrantID(102)
	repo.seedPolicyGrant(revoked, policyTargetID, policyNow.Add(time.Hour))
	g := repo.grants[revoked]
	g.Revoked = true
	repo.grants[revoked] = g
	svc := newPolicyService(t, repo, false) // TENDO_MCP_ENABLED off changes nothing
	ctx := context.Background()

	first, err := svc.AccountGrants(ctx, policyAdminID, policyTargetID, 0, "")
	if err != nil || len(first.Items) != MCPGrantListDefaultLimit || first.NextCursor == "" || first.Items[0].ID != adminGrantID(59) {
		t.Fatalf("first page %d items cursor=%q err=%v", len(first.Items), first.NextCursor, err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "read", "listGrants")
	second, err := svc.AccountGrants(ctx, policyAdminID, policyTargetID, 0, first.NextCursor)
	if err != nil || len(second.Items) != 10 || second.NextCursor != "" {
		t.Fatalf("second page %d items cursor=%q err=%v", len(second.Items), second.NextCursor, err)
	}
	for _, item := range append(first.Items, second.Items...) {
		if item.ID == adminGrantID(100) || item.ID == adminGrantID(101) || item.ID == revoked {
			t.Fatalf("listed a grant that is expired, foreign or revoked: %s", item.ID)
		}
	}
	clamped, err := svc.AccountGrants(ctx, policyAdminID, policyTargetID, 500, "")
	if err != nil || len(clamped.Items) != 60 {
		t.Fatalf("clamped page %d err=%v", len(clamped.Items), err)
	}
}

func TestMCPAdminGrantsListEmptyIsNonNil(t *testing.T) {
	repo := newPolicyRepo()
	page, err := newPolicyService(t, repo, true).AccountGrants(context.Background(), policyAdminID, policyTargetID, 0, "")
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

func TestMCPAdminGrantsListInvalidCursorBeforeRepository(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).AccountGrants(context.Background(), policyAdminID, policyTargetID, 0, "g1-not-a-cursor")
	if !errors.Is(err, ErrMCPInvalidCursor) {
		t.Fatalf("err=%v want ErrMCPInvalidCursor", err)
	}
	if len(repo.log) != 0 {
		t.Fatalf("an invalid cursor reached the repository: %v", repo.log)
	}
}

func TestMCPAdminGrantsListDeletedAccountIsNotFound(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).AccountGrants(context.Background(), policyAdminID, policyGoneID, 0, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "read")
}

// TestMCPAdminRevokeLockOrderAndAudit: the administrator check, the unlocked
// owner read, the account lock, the grant lock, then one admin revocation with
// the administrator as actor.
func TestMCPAdminRevokeLockOrderAndAudit(t *testing.T) {
	repo := newPolicyRepo()
	repo.seedPolicyGrant(adminGrantID(1), policyTargetID, policyNow.Add(time.Hour))
	if err := newPolicyService(t, repo, false).RevokeAccountGrant(context.Background(), policyAdminID, policyTargetID, adminGrantID(1)); err != nil {
		t.Fatal(err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "findOwner", "lockAccountForGrants", "lockGrant", "revokeGrant")
	want := MCPRevocation{Reason: MCPRevokeAdmin, ActorUserID: policyAdminID}
	if got := repo.revocations[adminGrantID(1)]; got != want {
		t.Fatalf("revocation=%+v want %+v", got, want)
	}
}

// TestMCPAdminRevokeForeignGrantTakesNoLock: a grant of another account named
// under {userId} is ErrNotFound after the unlocked owner read, before any
// account or grant lock.
func TestMCPAdminRevokeForeignGrantTakesNoLock(t *testing.T) {
	repo := newPolicyRepo()
	repo.accounts[policyPeerID] = true
	repo.seedPolicyGrant(adminGrantID(2), policyPeerID, policyNow.Add(time.Hour))
	err := newPolicyService(t, repo, true).RevokeAccountGrant(context.Background(), policyAdminID, policyTargetID, adminGrantID(2))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "findOwner")
	if repo.grants[adminGrantID(2)].Revoked {
		t.Fatal("a grant of another account was revoked")
	}
}

func TestMCPAdminRevokeUnknownRevokedExpiredAreNotFound(t *testing.T) {
	repo := newPolicyRepo()
	repo.seedPolicyGrant(adminGrantID(3), policyTargetID, policyNow.Add(time.Hour))
	g := repo.grants[adminGrantID(3)]
	g.Revoked = true
	repo.grants[adminGrantID(3)] = g
	repo.seedPolicyGrant(adminGrantID(4), policyTargetID, policyNow)
	svc := newPolicyService(t, repo, true)
	for _, id := range []string{adminGrantID(3), adminGrantID(4), adminGrantID(5)} {
		if err := svc.RevokeAccountGrant(context.Background(), policyAdminID, policyTargetID, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("grant %s err=%v want ErrNotFound", id, err)
		}
	}
	if len(repo.revocations) != 0 {
		t.Fatalf("a refused revoke wrote %v", repo.revocations)
	}
}

// TestMCPAdminRevokeDeletedAccountIsNotFound: the account lock finds the account
// deleted, which is the administrator's 404, never the caller's 401.
func TestMCPAdminRevokeDeletedAccountIsNotFound(t *testing.T) {
	repo := newPolicyRepo()
	repo.seedPolicyGrant(adminGrantID(6), policyGoneID, policyNow.Add(time.Hour))
	err := newPolicyService(t, repo, true).RevokeAccountGrant(context.Background(), policyAdminID, policyGoneID, adminGrantID(6))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
	if len(repo.revocations) != 0 {
		t.Fatal("revoked a grant of a deleted account")
	}
}

func TestMCPAdminRevokeAllLockOrderAndAudit(t *testing.T) {
	repo := newPolicyRepo()
	repo.seedPolicyGrant(adminGrantID(7), policyTargetID, policyNow.Add(time.Hour))
	repo.seedPolicyGrant(adminGrantID(8), policyTargetID, policyNow.Add(time.Hour))
	repo.seedPolicyGrant(adminGrantID(9), policyPeerID, policyNow.Add(time.Hour))
	if err := newPolicyService(t, repo, false).RevokeAccountGrants(context.Background(), policyAdminID, policyTargetID); err != nil {
		t.Fatal(err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockAccountForGrants", "revokeAccount")
	want := MCPRevocation{Reason: MCPRevokeAdmin, ActorUserID: policyAdminID}
	if got := repo.accountRevocations[policyTargetID]; got != want {
		t.Fatalf("account revocation=%+v want %+v", got, want)
	}
	if repo.grants[adminGrantID(9)].Revoked {
		t.Fatal("revoke-all reached another account")
	}
}

func TestMCPAdminRevokeAllDeletedAccountIsNotFound(t *testing.T) {
	repo := newPolicyRepo()
	err := newPolicyService(t, repo, true).RevokeAccountGrants(context.Background(), policyAdminID, policyGoneID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockAccountForGrants")
}

func TestMCPAdminRevokeRepositoryErrorsPropagate(t *testing.T) {
	for _, step := range []string{"findOwner", "lockAccountForGrants", "lockGrant", "revokeGrant"} {
		repo := newPolicyRepo()
		repo.fail = step
		repo.seedPolicyGrant(adminGrantID(1), policyTargetID, policyNow.Add(time.Hour))
		if err := newPolicyService(t, repo, true).RevokeAccountGrant(context.Background(), policyAdminID, policyTargetID, adminGrantID(1)); !errors.Is(err, errPolicyInjected) {
			t.Fatalf("%s: err=%v want the injected error", step, err)
		}
	}
	repo := newPolicyRepo()
	repo.fail = "listGrants"
	if _, err := newPolicyService(t, repo, true).AccountGrants(context.Background(), policyAdminID, policyTargetID, 0, ""); !errors.Is(err, errPolicyInjected) {
		t.Fatalf("list err=%v", err)
	}
}
