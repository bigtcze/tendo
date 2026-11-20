package postgres

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// sweepSeed builds the retention fixture around the clock instant. Rows named
// "...deleted" must be removed by one sweep; rows named "...kept" must survive.
// It returns the live and the ended grant IDs.
func sweepSeed(t *testing.T, env *mcpEnv) (liveGrant, endedGrant string) {
	t.Helper()
	now := env.clock.now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ahead := func(d time.Duration) time.Time { return now.Add(d) }
	user, _ := env.newAccount(t, "mcp_sweep_user")

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := env.admin.Exec(env.ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustGrant := func(created, expires time.Time) string {
		t.Helper()
		var id string
		if err := env.admin.QueryRow(env.ctx, `INSERT INTO mcp_grants(user_id, client_id, scopes, resource, created_at, expires_at)
			VALUES($1::uuid, $2, ARRAY['tendo:read'], $3, $4, $5) RETURNING id::text`,
			user, mcpTestClientID, mcpTestResource, created, expires).Scan(&id); err != nil {
			t.Fatalf("seed grant: %v", err)
		}
		return id
	}
	access := func(label, grant string, created, expires time.Time) {
		mustExec(`INSERT INTO mcp_access_tokens(token_hash, grant_id, created_at, expires_at) VALUES($1, $2::uuid, $3, $4)`,
			sha256Sum(label), grant, created, expires)
	}
	refresh := func(label, grant string, created, expires time.Time, consumed *time.Time) {
		mustExec(`INSERT INTO mcp_refresh_tokens(token_hash, grant_id, created_at, expires_at, consumed_at) VALUES($1, $2::uuid, $3, $4, $5)`,
			sha256Sum(label), grant, created, expires, consumed)
	}
	code := func(label, client string, created, expires time.Time, used *time.Time, grant string) {
		var issued *string
		if grant != "" {
			issued = &grant
		}
		mustExec(`INSERT INTO mcp_authorization_codes(code_hash, user_id, client_id, redirect_uri, code_challenge, scopes, resource, created_at, expires_at, used_at, issued_grant_id)
			VALUES($1, $2::uuid, $3, 'https://client.test/cb', $4, ARRAY['tendo:read'], $5, $6, $7, $8, $9::uuid)`,
			sha256Sum(label), user, client, mcpTestChallenge(), mcpTestResource, created, expires, used, issued)
	}
	at := func(v time.Time) *time.Time { return &v }

	// Clients: the test client is old but holds grants, so it stays. The old
	// client is grantless and codeless past 24 h, so it goes. The young client is
	// grantless but inside 24 h, and the code client holds a code, so both stay.
	mustExec(`UPDATE mcp_clients SET created_at=$1 WHERE client_id=$2`, ago(48*time.Hour), mcpTestClientID)
	mustExec(`INSERT INTO mcp_clients(client_id, client_name, redirect_uris, created_at) VALUES
		('client_old_0000000001', 'Old', ARRAY['https://client.test/old'], $1),
		('client_young_000000001', 'Young', ARRAY['https://client.test/young'], $2),
		('client_code_0000000001', 'Code', ARRAY['https://client.test/code'], $3)`,
		ago(25*time.Hour), ago(23*time.Hour), ago(48*time.Hour))

	liveGrant = mustGrant(ago(1000*time.Hour), ahead(1000*time.Hour))
	endedGrant = mustGrant(ago(2200*time.Hour), ago(40*time.Hour))

	access("a_expired_deleted", liveGrant, ago(2*time.Hour), ago(time.Hour))
	access("a_live_kept", liveGrant, ago(30*time.Minute), ahead(30*time.Minute))
	access("a_ended_deleted", endedGrant, ago(2200*time.Hour), ago(2199*time.Hour))

	refresh("r_expired_deleted", liveGrant, ago(1000*time.Hour), ago(time.Hour), nil)
	refresh("r_consumed_live_kept", liveGrant, ago(1000*time.Hour), ago(time.Hour), at(ago(500*time.Hour)))
	refresh("r_live_kept", liveGrant, ago(30*time.Minute), ahead(1000*time.Hour), nil)
	refresh("r_ended_deleted", endedGrant, ago(2200*time.Hour), ago(40*time.Hour), at(ago(1000*time.Hour)))

	code("c_unused_expired_deleted", mcpTestClientID, ago(20*time.Minute), ago(10*time.Minute), nil, "")
	code("c_unused_live_kept", mcpTestClientID, ago(time.Minute), ahead(9*time.Minute), nil, "")
	code("c_used_live_kept", mcpTestClientID, ago(20*time.Minute), ago(10*time.Minute), at(ago(15*time.Minute)), liveGrant)
	code("c_used_ended_deleted", mcpTestClientID, ago(2200*time.Hour), ago(2200*time.Hour-10*time.Minute), at(ago(2199*time.Hour)), endedGrant)
	code("c_code_client_kept", "client_code_0000000001", ago(time.Minute), ahead(9*time.Minute), nil, "")
	return liveGrant, endedGrant
}

// sweepExpectation is one seeded row and whether it must survive the sweep.
type sweepExpectation struct {
	name string
	sql  string
	arg  any
	kept bool
}

func sweepExpectations(liveGrant, endedGrant string) []sweepExpectation {
	return []sweepExpectation{
		{"access expired deleted", `SELECT count(*) FROM mcp_access_tokens WHERE token_hash=$1`, sha256Sum("a_expired_deleted"), false},
		{"access live kept", `SELECT count(*) FROM mcp_access_tokens WHERE token_hash=$1`, sha256Sum("a_live_kept"), true},
		{"access of ended grant deleted", `SELECT count(*) FROM mcp_access_tokens WHERE token_hash=$1`, sha256Sum("a_ended_deleted"), false},
		{"refresh unconsumed expired deleted", `SELECT count(*) FROM mcp_refresh_tokens WHERE token_hash=$1`, sha256Sum("r_expired_deleted"), false},
		{"refresh consumed of live grant kept", `SELECT count(*) FROM mcp_refresh_tokens WHERE token_hash=$1`, sha256Sum("r_consumed_live_kept"), true},
		{"refresh live kept", `SELECT count(*) FROM mcp_refresh_tokens WHERE token_hash=$1`, sha256Sum("r_live_kept"), true},
		{"refresh of ended grant deleted", `SELECT count(*) FROM mcp_refresh_tokens WHERE token_hash=$1`, sha256Sum("r_ended_deleted"), false},
		{"unused expired code deleted", `SELECT count(*) FROM mcp_authorization_codes WHERE code_hash=$1`, sha256Sum("c_unused_expired_deleted"), false},
		{"unused live code kept", `SELECT count(*) FROM mcp_authorization_codes WHERE code_hash=$1`, sha256Sum("c_unused_live_kept"), true},
		{"used code of live grant kept", `SELECT count(*) FROM mcp_authorization_codes WHERE code_hash=$1`, sha256Sum("c_used_live_kept"), true},
		{"used code of ended grant deleted", `SELECT count(*) FROM mcp_authorization_codes WHERE code_hash=$1`, sha256Sum("c_used_ended_deleted"), false},
		{"old grantless codeless client deleted", `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, "client_old_0000000001", false},
		{"young grantless client kept", `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, "client_young_000000001", true},
		{"client with a code kept", `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, "client_code_0000000001", true},
		{"client with a grant kept", `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, mcpTestClientID, true},
		{"live grant row kept", `SELECT count(*) FROM mcp_grants WHERE id=$1::uuid`, liveGrant, true},
		{"ended grant row kept", `SELECT count(*) FROM mcp_grants WHERE id=$1::uuid`, endedGrant, true},
	}
}

// TestSweepDeletesExactlyTheRetentionSet runs the sweep on a disabled service:
// the sweep runs regardless of the enabled flag.
func TestSweepDeletesExactlyTheRetentionSet(t *testing.T) {
	env := newMCPEnv(t)
	liveGrant, endedGrant := sweepSeed(t, env)
	svc, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: false}, env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.Sweep(env.ctx, env.clock.now())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(report.Locked) != 0 {
		t.Fatalf("uncontended sweep yielded steps %v", report.Locked)
	}
	for _, row := range sweepExpectations(liveGrant, endedGrant) {
		var count int
		if err := env.admin.QueryRow(env.ctx, row.sql, row.arg).Scan(&count); err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if got := count == 1; got != row.kept {
			t.Errorf("%s: present=%v, want %v", row.name, got, row.kept)
		}
	}
}

// TestSweepLockTimeoutYieldsAndRetries holds an expired access token row FOR
// UPDATE, as a revocation does. The access step must yield on lock_timeout
// without failing the run, the other steps must still run, and the next run
// must delete the row once the revocation commits.
func TestSweepLockTimeoutYieldsAndRetries(t *testing.T) {
	env := newMCPEnv(t)
	sweepSeed(t, env)
	svc, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: false}, env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(env.ctx) }()
	if _, err := holder.Exec(env.ctx, `SELECT 1 FROM mcp_access_tokens WHERE token_hash=$1 FOR UPDATE`, sha256Sum("a_expired_deleted")); err != nil {
		t.Fatal(err)
	}

	report, err := svc.Sweep(env.ctx, env.clock.now())
	if err != nil {
		t.Fatalf("a lock timeout must not fail the run: %v", err)
	}
	if !slices.Contains(report.Locked, identity.MCPSweepAccessTokensExpired) {
		t.Fatalf("access step did not yield: locked=%v", report.Locked)
	}
	if report.Deleted[identity.MCPSweepRefreshTokensExpired] != 1 || report.Deleted[identity.MCPSweepGrantlessClients] != 1 {
		t.Fatalf("other steps did not run: deleted=%v", report.Deleted)
	}
	if err := holder.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}

	report, err = svc.Sweep(env.ctx, env.clock.now())
	if err != nil {
		t.Fatalf("retry Sweep: %v", err)
	}
	if len(report.Locked) != 0 || report.Deleted[identity.MCPSweepAccessTokensExpired] != 1 {
		t.Fatalf("retry did not delete the released row: locked=%v deleted=%v", report.Locked, report.Deleted)
	}
}

// TestSweepSkipsClientHeldByIssuance is the race between code issuance and the
// sweep. The issuance reads the client with FindClient (FOR KEY SHARE) and holds
// it while the sweep runs. The sweep must skip the client, not delete it, and the
// issuer must still insert its code and commit without error.
func TestSweepSkipsClientHeldByIssuance(t *testing.T) {
	env := newMCPEnv(t)
	if _, err := env.admin.Exec(env.ctx, `UPDATE mcp_clients SET created_at=$1 WHERE client_id=$2`, env.clock.now().Add(-48*time.Hour), mcpTestClientID); err != nil {
		t.Fatal(err)
	}
	user, _ := env.newAccount(t, "mcp_keyshare_race_user")
	repo := NewMCPRepository(env.app)
	svc, err := identity.NewMCPService(repo, identity.MCPOptions{Enabled: false}, env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	found := make(chan struct{})
	proceed := make(chan struct{})
	issueDone := make(chan error, 1)
	go func() {
		issueDone <- repo.WithMCPTransaction(env.ctx, func(tx identity.MCPTransaction) error {
			if _, err := tx.FindClient(env.ctx, mcpTestClientID); err != nil {
				return err
			}
			close(found)
			<-proceed
			return tx.CreateAuthorizationCode(env.ctx, identity.MCPCodeRecord{
				CodeHash: sha256Sum("keyshare-race-code"), UserID: user, ClientID: mcpTestClientID,
				RedirectURI: mcpTestRedirectURI, CodeChallenge: mcpTestChallenge(),
				Scopes: []string{identity.MCPScopeRead}, Resource: mcpTestResource,
				CreatedAt: env.clock.now(), ExpiresAt: env.clock.now().Add(time.Minute),
			})
		})
	}()
	<-found

	report, sweepErr := svc.Sweep(env.ctx, env.clock.now())
	close(proceed)
	if sweepErr != nil {
		t.Fatalf("Sweep: %v", sweepErr)
	}
	if report.Deleted[identity.MCPSweepGrantlessClients] != 0 {
		t.Fatalf("sweep deleted a client held by issuance: deleted=%v", report.Deleted)
	}
	if err := <-issueDone; err != nil {
		t.Fatalf("issuer saw an error after the sweep ran: %v", err)
	}
	var survivors int
	if err := env.admin.QueryRow(env.ctx, `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, mcpTestClientID).Scan(&survivors); err != nil || survivors != 1 {
		t.Fatalf("client survivors=%d err=%v; want the client kept", survivors, err)
	}
}

// TestSweepCoversEveryClientReferencingTable pins the grantless-client NOT EXISTS
// set to the foreign keys that reference mcp_clients in the catalog. A new
// referencing table must join the sweep, or this test fails.
func TestSweepCoversEveryClientReferencingTable(t *testing.T) {
	env := newMCPEnv(t)
	rows, err := env.admin.Query(env.ctx, `SELECT conrelid::regclass::text FROM pg_constraint
		WHERE contype = 'f' AND confrelid = 'mcp_clients'::regclass`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), sweepClientReferencingTables...)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("foreign keys to mcp_clients = %v; sweep covers %v", got, want)
	}
}
