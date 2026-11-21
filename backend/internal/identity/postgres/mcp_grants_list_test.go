package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Connected apps (ADR 0023, slice A4b) against the real schema: the signed-in
// account's live grant list and its own revocation. Grant ids are fixed so the
// keyset order can be checked; they sort like uuidv7 creation order.

func mcpSeedGrantID(n int) string {
	return fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012x", n)
}

// seedGrant inserts a grant row directly so a test can place its created,
// expires and revoked times exactly. A nil revokedAt means not revoked.
func (e *mcpEnv) seedGrant(t *testing.T, id, user string, createdAt, expiresAt time.Time, revokedAt *time.Time) {
	t.Helper()
	var reason *string
	if revokedAt != nil {
		r := string(identity.MCPRevokeUser)
		reason = &r
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO mcp_grants(id, user_id, client_id, scopes, resource, created_at, expires_at, revoked_at, revoke_reason)
		VALUES($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		id, user, mcpTestClientID, []string{identity.MCPScopeRead}, mcpTestResource, createdAt, expiresAt, revokedAt, reason); err != nil {
		t.Fatal(err)
	}
}

// liveWindow returns a created and expires pair that a live grant holds at now.
// The grant lifetime is capped at 2160 hours by the table, so expiry is kept an
// hour inside the lifetime.
func liveWindow(now time.Time) (time.Time, time.Time) {
	return now.Add(-time.Minute), now.Add(identity.MCPGrantLifetime - time.Hour)
}

// TestConnectedAppsListExcludesRevokedExpiredAndForeign: only the account's own
// live grant is listed. A revoked grant, an expired grant and another account's
// live grant are all absent.
func TestConnectedAppsListExcludesRevokedExpiredAndForeign(t *testing.T) {
	env := newMCPEnv(t)
	owner, _ := env.newAccount(t, "apps_list_owner")
	other, _ := env.newAccount(t, "apps_list_other")
	now := env.clock.now()
	created, expires := liveWindow(now)
	revoked := now.Add(-time.Second)
	env.seedGrant(t, mcpSeedGrantID(1), owner, created, expires, nil)
	env.seedGrant(t, mcpSeedGrantID(2), owner, now.Add(-2*time.Minute), expires, &revoked)
	env.seedGrant(t, mcpSeedGrantID(3), owner, now.Add(-3*time.Hour), now.Add(-time.Minute), nil)
	env.seedGrant(t, mcpSeedGrantID(4), other, created, expires, nil)

	page, err := env.svc.ListGrants(env.ctx, owner, 0, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != mcpSeedGrantID(1) || page.NextCursor != "" {
		t.Fatalf("page = %+v, want only the live grant", page)
	}
	item := page.Items[0]
	if item.ClientName != "Test MCP client" || !slices.Equal(item.Scopes, []string{identity.MCPScopeRead}) ||
		!item.CreatedAt.Equal(created) || !item.ExpiresAt.Equal(expires) {
		t.Fatalf("item = %+v", item)
	}
}

// TestConnectedAppsListPagesStableOrder: 53 live grants page newest first with
// no gap and no repeat, at the default page size and at a small one. The
// page counts are fixed: ceil(53/50) = 2 and ceil(53/7) = 8.
func TestConnectedAppsListPagesStableOrder(t *testing.T) {
	env := newMCPEnv(t)
	user, _ := env.newAccount(t, "apps_list_pages")
	created, expires := liveWindow(env.clock.now())
	const total = 53
	var want []string
	for n := 1; n <= total; n++ {
		env.seedGrant(t, mcpSeedGrantID(n), user, created, expires, nil)
	}
	for n := total; n >= 1; n-- {
		want = append(want, mcpSeedGrantID(n))
	}
	for _, tc := range []struct{ limit, pages int }{{0, 2}, {7, 8}} {
		var got []string
		cursor, pages := "", 0
		for {
			page, err := env.svc.ListGrants(env.ctx, user, tc.limit, cursor)
			if err != nil {
				t.Fatalf("limit %d page %d: %v", tc.limit, pages, err)
			}
			pages++
			for _, item := range page.Items {
				got = append(got, item.ID)
			}
			if page.NextCursor == "" {
				break
			}
			if pages > total {
				t.Fatalf("limit %d: cursor never ended", tc.limit)
			}
			cursor = page.NextCursor
		}
		if pages != tc.pages {
			t.Fatalf("limit %d: pages = %d, want %d", tc.limit, pages, tc.pages)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("limit %d: order differs from newest-first; got %d ids", tc.limit, len(got))
		}
	}
}

// TestConnectedAppsRevokeOwnGrantEndToEnd: the account revokes its own live
// grant. The one revoked event names the user as reason and actor, every secret
// row is gone, the access token and the refresh token both fail, and the grant
// leaves the list. A second revoke is the same not-found refusal.
func TestConnectedAppsRevokeOwnGrantEndToEnd(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "apps_revoke_own")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	grantID := env.grantOf(t, user).id
	if err = env.svc.RevokeGrantForUser(env.ctx, user, grantID); err != nil {
		t.Fatalf("revoke own grant: %v", err)
	}
	env.requireGrantRevoked(t, user, string(identity.MCPRevokeUser), user)
	env.requireNoMCPSecrets(t, user)
	if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("revoked access token err=%v, want ErrMCPInvalidToken", err)
	}
	if _, err = env.refresh(tokens.RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("refresh after revoke err=%v, want ErrMCPInvalidGrant", err)
	}
	page, err := env.svc.ListGrants(env.ctx, user, 0, "")
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("list after revoke = %+v, err=%v; want empty", page, err)
	}
	if err = env.svc.RevokeGrantForUser(env.ctx, user, grantID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("second revoke err=%v, want ErrNotFound", err)
	}
}

// TestConnectedAppsRevokeRefusesNonLiveGrants: a revoked grant, an expired grant,
// an unknown grant and an uppercase spelling of a real grant are all ErrNotFound,
// and none of them changes a row.
func TestConnectedAppsRevokeRefusesNonLiveGrants(t *testing.T) {
	env := newMCPEnv(t)
	user, _ := env.newAccount(t, "apps_refuse")
	now := env.clock.now()
	_, expires := liveWindow(now)
	revoked := now.Add(-time.Second)
	env.seedGrant(t, mcpSeedGrantID(10), user, now.Add(-2*time.Minute), expires, &revoked)
	env.seedGrant(t, mcpSeedGrantID(11), user, now.Add(-3*time.Hour), now.Add(-time.Minute), nil)
	refused := []string{
		mcpSeedGrantID(10),
		mcpSeedGrantID(11),
		mcpSeedGrantID(12),
		"0198A2F0-7C1E-7A53-9B0E-" + fmt.Sprintf("%012x", 11),
	}
	for _, id := range refused {
		if err := env.svc.RevokeGrantForUser(env.ctx, user, id); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("revoke %s err=%v, want ErrNotFound", id, err)
		}
	}
	var expiredRevokedAt *time.Time
	if err := env.admin.QueryRow(env.ctx, `SELECT revoked_at FROM mcp_grants WHERE id=$1::uuid`, mcpSeedGrantID(11)).Scan(&expiredRevokedAt); err != nil || expiredRevokedAt != nil {
		t.Fatalf("expired grant was touched: revoked_at=%v err=%v", expiredRevokedAt, err)
	}
}

// TestConnectedAppsRevokeForeignGrantPromptNoLock: another account's grant is
// held FOR UPDATE by an open transaction. The revoke must still answer
// ErrNotFound at once: the owner is read without a lock, so the refusal never
// waits for the row. A lock wait would surface as the deadline error instead.
func TestConnectedAppsRevokeForeignGrantPromptNoLock(t *testing.T) {
	env := newMCPEnv(t)
	owner, _ := env.newAccount(t, "apps_foreign_owner")
	attacker, _ := env.newAccount(t, "apps_foreign_attacker")
	now := env.clock.now()
	created, expires := liveWindow(now)
	grantID := mcpSeedGrantID(70)
	env.seedGrant(t, grantID, owner, created, expires, nil)

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(env.ctx, `SELECT id FROM mcp_grants WHERE id=$1::uuid FOR UPDATE`, grantID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(env.ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	err = env.svc.RevokeGrantForUser(ctx, attacker, grantID)
	if elapsed := time.Since(start); !errors.Is(err, identity.ErrNotFound) || elapsed > 2*time.Second {
		t.Fatalf("foreign revoke err=%v after %s, want ErrNotFound at once", err, elapsed)
	}
	var revokedAt *time.Time
	if err = env.admin.QueryRow(env.ctx, `SELECT revoked_at FROM mcp_grants WHERE id=$1::uuid`, grantID).Scan(&revokedAt); err != nil || revokedAt != nil {
		t.Fatalf("foreign grant revoked by another account: revoked_at=%v err=%v", revokedAt, err)
	}
}

// TestConnectedAppsDeletionFirstIsUnauthenticated: the account is deleted and the
// deletion commits before the revoke starts. The revoke is ErrUnauthenticated,
// the grant ends with the deletion's reason, and no user revocation is written.
func TestConnectedAppsDeletionFirstIsUnauthenticated(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "apps_deleted_first")
	actor := env.newActor(t)
	if _, err := env.exchange(env.issue(t, user, session)); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	grantID := env.grantOf(t, user).id
	run := env.startDeletion(user, actor)
	<-run.finished
	if run.err != nil {
		t.Fatalf("deletion: %v", run.err)
	}
	if err := env.svc.RevokeGrantForUser(env.ctx, user, grantID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoke after deletion err=%v, want ErrUnauthenticated", err)
	}
	env.requireDeletedGrant(t, user, actor)
	if n := env.revokedEvents(t, grantID, string(identity.MCPRevokeUser)); n != 0 {
		t.Fatalf("user revocation events = %d after deletion, want none", n)
	}
}
