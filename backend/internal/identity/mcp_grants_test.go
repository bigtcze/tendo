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

const mcpTestOtherUser = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"

// mcpGrantID returns a canonical lowercase UUIDv7-shaped id. Ids of one fixture
// sort in creation order, as the database's uuidv7() does.
func mcpGrantID(n int) string {
	return fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b%02x", n)
}

// seedMCPGrant inserts a grant owned by user with the given expiry.
func seedMCPGrant(f *mcpFake, id, user string, expires time.Time) {
	f.state.grants[id] = MCPGrantRecord{
		ID: id, UserID: user, ClientID: mcpTestClient, Scopes: []string{MCPScopeRead},
		Resource: mcpTestResource, CreatedAt: mcpTestStart, ExpiresAt: expires,
	}
}

// ListLiveGrants mirrors the production filter and keyset order: live means
// unrevoked and unexpired, newest id first, strictly below afterID.
func (f *mcpFake) ListLiveGrants(ctx context.Context, userID string, now time.Time, afterID string, limit int) ([]MCPGrantItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "ListLiveGrants")
	var ids []string
	for id, g := range f.state.grants {
		if g.UserID == userID && !g.Revoked && g.ExpiresAt.After(now) && (afterID == "" || id < afterID) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	var items []MCPGrantItem
	for _, id := range ids {
		if len(items) == limit {
			break
		}
		g := f.state.grants[id]
		items = append(items, MCPGrantItem{
			ID: id, ClientName: f.clients[g.ClientID].Name, Scopes: slices.Clone(g.Scopes),
			CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt,
		})
	}
	return items, nil
}

func disabledMCPFixture(t *testing.T) (*mcpFake, *MCPService) {
	t.Helper()
	f := newMCPFake(mcpTestStart)
	svc, err := NewMCPService(f, MCPOptions{Enabled: false}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	return f, svc
}

func TestConnectedAppsWorkWhileMCPIsDisabled(t *testing.T) {
	f, svc := disabledMCPFixture(t)
	seedMCPGrant(f, mcpGrantID(1), mcpTestUser, mcpTestStart.Add(MCPGrantLifetime))
	page, err := svc.ListGrants(context.Background(), mcpTestUser, 0, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListGrants while disabled = %+v, %v; want one item", page, err)
	}
	if err := svc.RevokeGrantForUser(context.Background(), mcpTestUser, mcpGrantID(1)); err != nil {
		t.Fatalf("RevokeGrantForUser while disabled: %v", err)
	}
	if !f.state.grants[mcpGrantID(1)].Revoked {
		t.Fatal("grant not revoked while MCP is disabled")
	}
	// Every other MCP operation stays off.
	if _, err := svc.VerifyAccessToken(context.Background(), randomMCPSecretForIdentity()); !errors.Is(err, ErrMCPDisabled) {
		t.Fatalf("VerifyAccessToken while disabled err = %v, want ErrMCPDisabled", err)
	}
}

func randomMCPSecretForIdentity() string { return strings.Repeat("A", 43) }

func TestRevokeGrantRefusesMalformedIDWithoutRepositoryCall(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", strings.ToUpper(mcpGrantID(1)), mcpGrantID(1) + "0", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b01 "} {
		f, svc := disabledMCPFixture(t)
		seedMCPGrant(f, mcpGrantID(1), mcpTestUser, mcpTestStart.Add(time.Hour))
		err := svc.RevokeGrantForUser(context.Background(), mcpTestUser, id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q: err = %v, want ErrNotFound", id, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("id %q reached the repository: %v", id, f.calls)
		}
	}
}

// A foreign grant is refused before any lock is taken: the unlocked owner read
// names a different account, so LockLiveAccount and LockGrant never run.
func TestRevokeForeignGrantRefusedWithoutLocks(t *testing.T) {
	fx := newMCPFixture(t)
	seedMCPGrant(fx.f, mcpGrantID(2), mcpTestOtherUser, mcpTestStart.Add(time.Hour))
	fx.f.accounts[mcpTestOtherUser] = true
	err := fx.svc.RevokeGrantForUser(context.Background(), mcpTestUser, mcpGrantID(2))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign revoke err = %v, want ErrNotFound", err)
	}
	want := []string{"WithMCPTransaction", "FindGrantOwner"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v (no account or grant lock)", fx.f.calls, want)
	}
	if fx.f.state.grants[mcpGrantID(2)].Revoked {
		t.Fatal("foreign grant was revoked")
	}
}

func TestRevokeUnknownGrantIsNotFound(t *testing.T) {
	fx := newMCPFixture(t)
	err := fx.svc.RevokeGrantForUser(context.Background(), mcpTestUser, mcpGrantID(9))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown revoke err = %v, want ErrNotFound", err)
	}
}

func TestRevokeOwnLiveGrantAuditsUserActor(t *testing.T) {
	fx := newMCPFixture(t)
	seedMCPGrant(fx.f, mcpGrantID(3), mcpTestUser, mcpTestStart.Add(time.Hour))
	if err := fx.svc.RevokeGrantForUser(context.Background(), mcpTestUser, mcpGrantID(3)); err != nil {
		t.Fatalf("RevokeGrantForUser: %v", err)
	}
	want := MCPRevocation{Reason: MCPRevokeUser, ActorUserID: mcpTestUser}
	if got := fx.f.state.revocations[mcpGrantID(3)]; got != want {
		t.Fatalf("revocation = %+v, want %+v", got, want)
	}
	wantOrder := []string{"WithMCPTransaction", "FindGrantOwner", "LockLiveAccount", "LockGrant", "RevokeGrant"}
	if !slices.Equal(fx.f.calls, wantOrder) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, wantOrder)
	}
}

func TestRevokeRefusesRevokedAndExpiredGrants(t *testing.T) {
	fx := newMCPFixture(t)
	seedMCPGrant(fx.f, mcpGrantID(4), mcpTestUser, mcpTestStart.Add(time.Hour))
	g := fx.f.state.grants[mcpGrantID(4)]
	g.Revoked = true
	fx.f.state.grants[mcpGrantID(4)] = g
	seedMCPGrant(fx.f, mcpGrantID(5), mcpTestUser, mcpTestStart)
	for _, id := range []string{mcpGrantID(4), mcpGrantID(5)} {
		if err := fx.svc.RevokeGrantForUser(context.Background(), mcpTestUser, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("grant %s err = %v, want ErrNotFound", id, err)
		}
	}
	if len(fx.f.state.revocations) != 0 {
		t.Fatalf("refused revoke wrote revocations: %v", fx.f.state.revocations)
	}
}

func TestRevokeWhenAccountGoneIsUnauthenticated(t *testing.T) {
	fx := newMCPFixture(t)
	seedMCPGrant(fx.f, mcpGrantID(6), mcpTestUser, mcpTestStart.Add(time.Hour))
	fx.f.accounts[mcpTestUser] = false
	if err := fx.svc.RevokeGrantForUser(context.Background(), mcpTestUser, mcpGrantID(6)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("deleted account err = %v, want ErrUnauthenticated", err)
	}
}

func TestListGrantsPagesNewestFirstAndClampsLimit(t *testing.T) {
	fx := newMCPFixture(t)
	for n := 0; n < 120; n++ {
		seedMCPGrant(fx.f, mcpGrantID(n), mcpTestUser, mcpTestStart.Add(time.Hour))
	}
	seedMCPGrant(fx.f, mcpGrantID(200), mcpTestOtherUser, mcpTestStart.Add(time.Hour))
	ctx := context.Background()

	first, err := fx.svc.ListGrants(ctx, mcpTestUser, 0, "")
	if err != nil || len(first.Items) != MCPGrantListDefaultLimit || first.NextCursor == "" {
		t.Fatalf("default page = %d items cursor=%q err=%v", len(first.Items), first.NextCursor, err)
	}
	if first.Items[0].ID != mcpGrantID(119) {
		t.Fatalf("first item = %s, want newest %s", first.Items[0].ID, mcpGrantID(119))
	}
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page, err := fx.svc.ListGrants(ctx, mcpTestUser, 0, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate id %s across pages", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 120 || pages != 3 {
		t.Fatalf("paged %d grants in %d pages, want 120 in 3", len(seen), pages)
	}
	if seen[mcpGrantID(200)] {
		t.Fatal("another account's grant was listed")
	}

	clamped, _ := fx.svc.ListGrants(ctx, mcpTestUser, 500, "")
	if len(clamped.Items) != MCPGrantListMaxLimit {
		t.Fatalf("limit 500 page = %d items, want %d", len(clamped.Items), MCPGrantListMaxLimit)
	}
	exact, _ := fx.svc.ListGrants(ctx, mcpTestUser, 100, "")
	if len(exact.Items) != 100 || exact.NextCursor == "" {
		t.Fatalf("limit 100 page = %d items cursor=%q", len(exact.Items), exact.NextCursor)
	}
}

func TestListGrantsRefusesInvalidCursorBeforeRepository(t *testing.T) {
	fx := newMCPFixture(t)
	seedMCPGrant(fx.f, mcpGrantID(1), mcpTestUser, mcpTestStart.Add(time.Hour))
	valid, err := encodeMCPGrantCursor(mcpGrantID(1))
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"x" + valid[1:],                      // wrong prefix
		valid[:len(valid)-1],                 // short
		valid + "A",                          // long
		valid[:len(valid)-1] + "B",           // non-zero padding bits: not canonical
		"g1" + strings.Repeat("!", 22),       // not base64url
		"g1" + strings.Repeat("A", 21) + "/", // standard base64 alphabet
		"g1" + strings.Repeat("A", 23),       // wrong byte length for the alphabet
	}
	for _, cursor := range bad {
		fx.f.calls = nil
		if _, err := fx.svc.ListGrants(context.Background(), mcpTestUser, 0, cursor); !errors.Is(err, ErrMCPInvalidCursor) {
			t.Errorf("cursor %q err = %v, want ErrMCPInvalidCursor", cursor, err)
		}
		if len(fx.f.calls) != 0 {
			t.Errorf("cursor %q reached the repository: %v", cursor, fx.f.calls)
		}
	}
}

func TestConnectedAppsCursorRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 0x7f, 0xff} {
		id := mcpGrantID(n)
		cursor, err := encodeMCPGrantCursor(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(cursor) != mcpGrantCursorLength || !strings.HasPrefix(cursor, mcpGrantCursorPrefix) {
			t.Fatalf("cursor %q has the wrong shape", cursor)
		}
		got, err := decodeMCPGrantCursor(cursor)
		if err != nil || got != id {
			t.Fatalf("round trip of %s = %q, %v", id, got, err)
		}
	}
	if _, err := encodeMCPGrantCursor(strings.ToUpper(mcpGrantID(1))); err == nil {
		t.Fatal("encoded a non-lowercase id")
	}
	if got, err := decodeMCPGrantCursor(""); err != nil || got != "" {
		t.Fatalf("empty cursor = %q, %v; want first page", got, err)
	}
}
