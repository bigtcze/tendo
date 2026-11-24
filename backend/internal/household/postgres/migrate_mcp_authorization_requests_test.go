package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcpAuthorizationRequestIndexes are the two indexes migration 030 creates for
// the foreign key and the expiry sweep (ADR 0023).
var mcpAuthorizationRequestIndexes = []string{
	"mcp_authorization_requests_client_id_idx",
	"mcp_authorization_requests_expires_at_idx",
}

// mcpReasonConstraints are the two CHECK names migration 030 widens. They are the
// names PostgreSQL generated for migration 028, verified in a migrated database.
var mcpReasonConstraints = []struct{ table, name string }{
	{"mcp_grants", "mcp_grants_revoke_reason_check"},
	{"mcp_grant_events", "mcp_grant_events_reason_check"},
}

// insertMCPAuthorizationRequest writes one pending consent with a valid code
// challenge. lifetime is an interval literal; state nil means no client state.
func insertMCPAuthorizationRequest(ctx context.Context, pool *pgxpool.Pool, requestHash, bindingHash []byte, state any, lifetime string) error {
	_, err := pool.Exec(ctx, `INSERT INTO mcp_authorization_requests(request_hash,binding_hash,client_id,redirect_uri,code_challenge,scopes,resource,state,created_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,ARRAY['tendo:read'],$6,$7,now(),now()+$8::interval)`,
		requestHash, bindingHash, mcpTestClientID, mcpTestRedirect, strings.Repeat("A", 43), mcpTestResource, state, lifetime)
	return err
}

// TestMigrationThirtyUpAndPrivileges covers migration 030: the consent table,
// its indexes, the runtime privileges (no UPDATE, no PUBLIC access), and the
// widened reason CHECKs under their explicit names.
func TestMigrationThirtyUpAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if !migrationRelationExists(t, ctx, admin, "mcp_authorization_requests") {
		t.Fatal("mcp_authorization_requests missing after migrate to 31")
	}
	for _, name := range mcpAuthorizationRequestIndexes {
		if !migrationRelationExists(t, ctx, admin, name) {
			t.Fatalf("index %s missing after migrate to 31", name)
		}
	}
	var selectOK, insertOK, deleteOK, updateOK bool
	if err := admin.QueryRow(ctx, `SELECT has_table_privilege('tendo','mcp_authorization_requests','SELECT'),
		has_table_privilege('tendo','mcp_authorization_requests','INSERT'),
		has_table_privilege('tendo','mcp_authorization_requests','DELETE'),
		has_table_privilege('tendo','mcp_authorization_requests','UPDATE')`).Scan(&selectOK, &insertOK, &deleteOK, &updateOK); err != nil {
		t.Fatal(err)
	}
	if !selectOK || !insertOK || !deleteOK || updateOK {
		t.Fatalf("tendo privileges on mcp_authorization_requests: select=%v insert=%v delete=%v update=%v; want select, insert, delete and no update", selectOK, insertOK, deleteOK, updateOK)
	}
	var publicGrants int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a
		WHERE c.oid = 'mcp_authorization_requests'::regclass AND a.grantee = 0`).Scan(&publicGrants); err != nil {
		t.Fatal(err)
	}
	if publicGrants != 0 {
		t.Fatalf("PUBLIC holds %d privileges on mcp_authorization_requests", publicGrants)
	}
	for _, c := range mcpReasonConstraints {
		var def string
		if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, c.table, c.name).Scan(&def); err != nil {
			t.Fatalf("constraint %s on %s: %v", c.name, c.table, err)
		}
		if !strings.Contains(def, "'client'") {
			t.Fatalf("%s does not admit 'client': %s", c.name, def)
		}
	}
}

// TestMigrationThirtyCheckRefusals proves the consent table's CHECKs refuse what
// the service never stores: malformed digests, a challenge of the wrong shape,
// a state outside printable ASCII, and a lifetime over ten minutes.
func TestMigrationThirtyCheckRefusals(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	insertMCPClient(t, ctx, admin)
	good := mcpDigest(0x31)
	other := mcpDigest(0x32)
	for _, tc := range []struct {
		name        string
		requestHash []byte
		bindingHash []byte
		state       any
		lifetime    string
		accepted    bool
	}{
		{name: "valid", requestHash: good, bindingHash: other, state: "ok", lifetime: "5 minutes", accepted: true},
		{name: "short request digest", requestHash: good[:31], bindingHash: other, state: nil, lifetime: "5 minutes"},
		{name: "short binding digest", requestHash: mcpDigest(0x33), bindingHash: other[:31], state: nil, lifetime: "5 minutes"},
		{name: "control byte in state", requestHash: mcpDigest(0x34), bindingHash: other, state: "a\x01b", lifetime: "5 minutes"},
		{name: "non-ascii state", requestHash: mcpDigest(0x35), bindingHash: other, state: "caf\u00e9", lifetime: "5 minutes"},
		{name: "empty state", requestHash: mcpDigest(0x36), bindingHash: other, state: "", lifetime: "5 minutes"},
		{name: "lifetime over ten minutes", requestHash: mcpDigest(0x37), bindingHash: other, state: nil, lifetime: "11 minutes"},
		{name: "zero lifetime", requestHash: mcpDigest(0x38), bindingHash: other, state: nil, lifetime: "0 seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := insertMCPAuthorizationRequest(ctx, admin, tc.requestHash, tc.bindingHash, tc.state, tc.lifetime)
			if tc.accepted && err != nil {
				t.Fatalf("insert refused: %v", err)
			}
			if !tc.accepted && err == nil {
				t.Fatal("insert accepted; the CHECK must refuse it")
			}
		})
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_authorization_requests`); got != 1 {
		t.Fatalf("rows=%d want only the valid request", got)
	}
}

// TestMigrationThirtyDownMapsClientRevocationsAndReapplies reverses 030 with a
// client revocation and its audit event in place. Down maps 'client' to 'user',
// restores the 028 CHECK sets under the same names, drops the consent table, and
// the audit row survives. Up then re-applies.
func TestMigrationThirtyDownMapsClientRevocationsAndReapplies(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	user := insertMigrationAccount(t, ctx, admin, "mcp_client_revoke_user")
	insertMCPClient(t, ctx, admin)
	grant := insertMCPGrant(t, ctx, admin, user)
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,occurred_at)
		VALUES ($1::uuid,$2::uuid,$3,'Fixture client','created',now())`, grant, user, mcpTestClientID); err != nil {
		t.Fatalf("created event: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='client' WHERE id=$1::uuid`, grant); err != nil {
		t.Fatalf("client revocation: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,reason,occurred_at)
		VALUES ($1::uuid,$2::uuid,$3,'Fixture client','revoked','client',now())`, grant, user, mcpTestClientID); err != nil {
		t.Fatalf("client revocation event: %v", err)
	}
	if err := insertMCPAuthorizationRequest(ctx, admin, mcpDigest(0x41), mcpDigest(0x42), nil, "5 minutes"); err != nil {
		t.Fatalf("pending consent before Down: %v", err)
	}

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion30SQL); err != nil {
		t.Fatalf("reverse 030: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 29)
	if migrationRelationExists(t, ctx, admin, "mcp_authorization_requests") {
		t.Fatal("mcp_authorization_requests survived Down")
	}
	for _, name := range mcpAuthorizationRequestIndexes {
		if migrationRelationExists(t, ctx, admin, name) {
			t.Fatalf("index %s survived Down", name)
		}
	}
	if got := migrationString(t, ctx, admin, `SELECT revoke_reason FROM mcp_grants WHERE id=$1::uuid`, grant); got != "user" {
		t.Fatalf("grant revoke_reason after Down=%q want user", got)
	}
	if got := migrationString(t, ctx, admin, `SELECT reason FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked'`, grant); got != "user" {
		t.Fatalf("event reason after Down=%q want user", got)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_grant_events WHERE grant_id=$1::uuid`, grant); got != 2 {
		t.Fatalf("events after Down=%d want 2 (created and revoked)", got)
	}
	for _, c := range mcpReasonConstraints {
		var def string
		if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, c.table, c.name).Scan(&def); err != nil {
			t.Fatalf("constraint %s on %s after Down: %v", c.name, c.table, err)
		}
		if strings.Contains(def, "'client'") {
			t.Fatalf("%s still admits 'client' after Down: %s", c.name, def)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoke_reason='client' WHERE id=$1::uuid`, grant); err == nil {
		t.Fatal("reason 'client' accepted after Down")
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 030: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if !migrationRelationExists(t, ctx, admin, "mcp_authorization_requests") {
		t.Fatal("mcp_authorization_requests missing after re-apply")
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_authorization_requests`); got != 0 {
		t.Fatalf("consent rows after lossy Down/Up=%d want 0", got)
	}
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoke_reason='client' WHERE id=$1::uuid`, grant); err != nil {
		t.Fatalf("reason 'client' refused after re-apply: %v", err)
	}
}
