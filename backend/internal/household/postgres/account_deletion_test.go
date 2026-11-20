package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	itempostgres "github.com/bigtcze/tendo/backend/internal/item/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
)

// accountDeletionFixture wires household.AccountDeletionService exactly as
// cmd/tendo does (ADR 0022): the household-owned deletion repository reusing
// the ADR 0019 removal effects, plus the identity-owned account eraser built on
// the same transaction. Every effect is the real PostgreSQL adapter.
type accountDeletionFixture struct {
	*adminCenterFixture
	clock      func() time.Time
	repository *AccountDeletionRepository
	deletions  *household.AccountDeletionService
}

func newAccountDeletionFixture(t *testing.T) (context.Context, *accountDeletionFixture) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL account deletion tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	base := newAdminCenterFixture(t, ctx, admin, app)
	base.prepare()
	t.Cleanup(base.reset)
	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	repository := NewAccountDeletionRepository(app, MemberRemovalEffectFactories{
		Invitations: func(tx householddb.DBTX) household.InvitationRevoker {
			return identitypostgres.NewRemovalEffects(tx, clock)
		},
		Items: func(tx householddb.DBTX) household.ResponsibilityReleaser {
			return itempostgres.NewResponsibilityReleaser(tx)
		},
		Subjects: func(tx householddb.DBTX) household.MemberPersonUnlinker {
			return subjectpostgres.NewMemberPersonUnlinker(tx)
		},
		Resets: func(tx householddb.DBTX) household.PasswordResetRevoker {
			return identitypostgres.NewRemovalEffects(tx, clock)
		},
		Accounts: func(tx householddb.DBTX) household.DefaultHouseholdReassigner {
			return identitypostgres.NewRemovalEffects(tx, clock)
		},
	}, func(tx householddb.DBTX) household.AccountEraser {
		return identitypostgres.NewAccountEraser(tx)
	})
	return ctx, &accountDeletionFixture{
		adminCenterFixture: base,
		clock:              clock,
		repository:         repository,
		deletions:          household.NewAccountDeletionService(repository, clock),
	}
}

// setEmail gives the account a complete verified email fact set, which the
// tombstone must clear as a group.
func (f *accountDeletionFixture) setEmail(userID, email string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `UPDATE user_accounts
		SET email=$2, email_verified_at=now(), email_source='account_confirmed', email_issuer='https://local.test'
		WHERE id=$1::uuid`, userID, email); err != nil {
		f.t.Fatalf("set email: %v", err)
	}
}

func (f *accountDeletionFixture) addIdentity(userID, issuer, subject string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO oidc_identities(issuer, subject, user_id, created_at)
		VALUES($1, $2, $3::uuid, now())`, issuer, subject, userID); err != nil {
		f.t.Fatalf("add identity: %v", err)
	}
}

// addLinkFlow stores a link flow bound to a fresh session of the account,
// exactly as the OIDC link start does.
func (f *accountDeletionFixture) addLinkFlow(userID string) {
	f.t.Helper()
	var sessionID string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at, sign_in_method)
		VALUES ($1::uuid, decode(md5(random()::text)||md5(random()::text), 'hex'), now(), now()+interval '1 day', 'oidc')
		RETURNING id::text`, userID).Scan(&sessionID); err != nil {
		f.t.Fatalf("add link session: %v", err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO oidc_flows(state_hash, browser_token_hash, issuer, client_id, nonce,
		pkce_verifier, purpose, user_id, session_id, created_at, expires_at)
		VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'),
		        decode(md5(random()::text)||md5(random()::text), 'hex'),
		        'https://issuer.test', 'delete-client', 'delete-nonce', repeat('a', 43), 'link',
		        $1::uuid, $2::uuid, now(), now()+interval '5 minutes')`, userID, sessionID); err != nil {
		f.t.Fatalf("add link flow: %v", err)
	}
}

// addMCPGrant stores a live MCP grant of the account with one access token, one
// live and one consumed refresh token, plus an outstanding and a used
// authorization code, exactly the secret-bearing rows ADR 0023 says a deletion
// must erase. Rows are created an hour before the fixture clock; grants
// created after it are covered by addMCPGrantAt. It returns the grant id.
func (f *accountDeletionFixture) addMCPGrant(userID, clientID string) string {
	f.t.Helper()
	return f.addMCPGrantAt(userID, clientID, "2026-05-04 09:00:00+00")
}

// addMCPGrantAt is addMCPGrant with the grant's creation instant as a parameter.
// A grant created after the fixture clock (10:00) exercises a deletion whose
// clock reads earlier than created_at; the revocation must clamp, not violate the CHECK.
func (f *accountDeletionFixture) addMCPGrantAt(userID, clientID, createdAt string) string {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO mcp_clients(client_id, client_name, redirect_uris, created_at)
		VALUES ($1, 'Deletion test client', ARRAY['https://client.test/callback'], timestamptz '2026-05-04 09:00:00+00')
		ON CONFLICT (client_id) DO NOTHING`, clientID); err != nil {
		f.t.Fatalf("add MCP client: %v", err)
	}
	var grantID string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO mcp_grants(user_id, client_id, scopes, resource, created_at, expires_at)
		VALUES ($1::uuid, $2, ARRAY['tendo:read'], 'https://tendo.test/mcp', $3::timestamptz, $3::timestamptz+interval '90 days')
		RETURNING id::text`, userID, clientID, createdAt).Scan(&grantID); err != nil {
		f.t.Fatalf("add MCP grant: %v", err)
	}
	owner := []any{grantID, userID, clientID}
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO mcp_grant_events(grant_id, user_id, client_id, client_name, event, occurred_at)
		  VALUES ($1::uuid, $2::uuid, $3, 'Deletion test client', 'created', $4::timestamptz)`, append(owner, createdAt)},
		{`INSERT INTO mcp_access_tokens(token_hash, grant_id, created_at, expires_at)
		  VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'), $1::uuid, $2::timestamptz, $2::timestamptz+interval '10 minutes')`, []any{grantID, createdAt}},
		{`INSERT INTO mcp_refresh_tokens(token_hash, grant_id, created_at, expires_at)
		  VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'), $1::uuid, $2::timestamptz, $2::timestamptz+interval '30 days')`, []any{grantID, createdAt}},
		{`INSERT INTO mcp_refresh_tokens(token_hash, grant_id, created_at, expires_at, consumed_at)
		  VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'), $1::uuid, $2::timestamptz, $2::timestamptz+interval '30 days', $2::timestamptz)`, []any{grantID, createdAt}},
		{`INSERT INTO mcp_authorization_codes(code_hash, user_id, client_id, redirect_uri, code_challenge, scopes, resource, created_at, expires_at, used_at, issued_grant_id)
		  VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'), $2::uuid, $3, 'https://client.test/callback',
		          repeat('c', 43), ARRAY['tendo:read'], 'https://tendo.test/mcp', $4::timestamptz, $4::timestamptz+interval '1 minute', $4::timestamptz, $1::uuid)`, append(owner, createdAt)},
		{`INSERT INTO mcp_authorization_codes(code_hash, user_id, client_id, redirect_uri, code_challenge, scopes, resource, created_at, expires_at)
		  VALUES (decode(md5(random()::text)||md5(random()::text), 'hex'), $1::uuid, $2, 'https://client.test/callback',
		          repeat('d', 43), ARRAY['tendo:read'], 'https://tendo.test/mcp', $3::timestamptz, $3::timestamptz+interval '1 minute')`, []any{userID, clientID, createdAt}},
	} {
		if _, err := f.admin.Exec(f.ctx, step.sql, step.args...); err != nil {
			f.t.Fatalf("add MCP credential: %v\n%s", err, step.sql)
		}
	}
	return grantID
}

// mcpSecretRows counts every MCP code and token row of the account. A deleted
// account must have none: an unusable but surviving digest is not enough.
func (f *accountDeletionFixture) mcpSecretRows(userID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT
		(SELECT count(*) FROM mcp_authorization_codes c WHERE c.user_id = $1::uuid)
		+ (SELECT count(*) FROM mcp_access_tokens a JOIN mcp_grants g ON g.id = a.grant_id WHERE g.user_id = $1::uuid)
		+ (SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id = r.grant_id WHERE g.user_id = $1::uuid)`, userID)
}

func (f *accountDeletionFixture) addCompletion(householdID, itemID, userID, key string) string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO item_completions(id, household_id, item_id, completed_on,
		completed_by_user_id, prior_workflow_state, item_version_before, idempotency_key, request_fingerprint)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '2026-01-05', $3::uuid, 'open', 1, $4, decode(repeat('cd', 32), 'hex'))
		RETURNING id::text`, householdID, itemID, userID, key).Scan(&id); err != nil {
		f.t.Fatalf("add completion: %v", err)
	}
	return id
}

// deletionSnapshot renders every fact a refused deletion must leave untouched.
// Two identical snapshots mean the refusal wrote nothing about the account.
func (f *accountDeletionFixture) deletionSnapshot(userID string) string {
	f.t.Helper()
	var snapshot string
	if err := f.admin.QueryRow(f.ctx, `SELECT u.login
		|| '|' || coalesce(u.email, '-')
		|| '|' || coalesce(u.default_household_id::text, '-')
		|| '|' || (u.deleted_at IS NOT NULL)::text
		|| '|' || (SELECT count(*) FROM local_credentials c WHERE c.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM user_sessions s WHERE s.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM oidc_identities i WHERE i.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM oidc_flows w WHERE w.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM household_memberships m WHERE m.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM household_member_removals r WHERE r.target_user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM instance_admins g WHERE g.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM instance_admin_revocations v WHERE v.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM account_deletions d WHERE d.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM password_reset_tokens t WHERE t.target_user_id = u.id OR t.created_by_user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM mcp_grants g WHERE g.user_id = u.id AND g.revoked_at IS NULL)::text
		|| '|' || (SELECT count(*) FROM mcp_grant_events e WHERE e.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM mcp_authorization_codes c WHERE c.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM mcp_access_tokens a JOIN mcp_grants g ON g.id = a.grant_id WHERE g.user_id = u.id)::text
		|| '|' || (SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id = r.grant_id WHERE g.user_id = u.id)::text
		FROM user_accounts u WHERE u.id = $1::uuid`, userID).Scan(&snapshot); err != nil {
		f.t.Fatalf("deletion snapshot: %v", err)
	}
	return snapshot
}

// TestAccountDeletionAgainstPostgres covers the committed deletion and every
// refusal of ADR 0022 against the real adapters.
func TestAccountDeletionAgainstPostgres(t *testing.T) {
	ctx, f := newAccountDeletionFixture(t)

	t.Run("deletes the account in place and keeps every history row", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("delete_actor")
		f.grantInstanceAdmin(actor)
		keeper := f.newUserWithLogin("delete_keeper")
		f.grantInstanceAdmin(keeper)

		first := f.newHousehold("delete first house")
		second := f.newHousehold("delete second house")
		otherAdmin := f.newUserWithLogin("delete_first_house_admin")
		f.join(otherAdmin, first, household.RoleAdmin)

		const targetLogin = "delete_target"
		target := f.newUserWithLogin(targetLogin)
		f.join(target, first, household.RoleAdmin)
		f.join(target, second, household.RoleMember)
		f.setDefault(target, first)
		f.setEmail(target, "delete_target@example.test")
		f.password(target)
		f.addSession(target)
		f.addSession(target)
		f.addIdentity(target, "https://issuer.test", "delete-target-subject")
		f.addLinkFlow(target)
		invitation := f.addInvitation(first, target, "delete-happy-invite")
		targetReset := f.addReset(first, target, otherAdmin)
		resetSubject := f.newUserWithLogin("delete_reset_subject")
		f.join(resetSubject, first, household.RoleMember)
		issuedReset := f.addReset(first, resetSubject, target)
		person := f.addSubject(first, "delete target person", target)
		item := f.addItem(first, person, "delete target item", target, false, false)
		completion := f.addCompletion(first, item, target, "delete-completion")
		f.grantInstanceAdmin(target)
		firstGrant := f.addMCPGrant(target, "deletion-client-aaaaaaaa")
		secondGrant := f.addMCPGrant(target, "deletion-client-bbbbbbbb")
		bystander := f.addMCPGrant(keeper, "deletion-client-aaaaaaaa")
		if got := f.mcpSecretRows(target); got != 10 {
			t.Fatalf("fixture MCP secret rows=%d want 10", got)
		}
		var grantedAt time.Time
		if err := f.admin.QueryRow(ctx, `SELECT granted_at FROM instance_admins WHERE user_id=$1::uuid`, target).Scan(&grantedAt); err != nil {
			t.Fatal(err)
		}

		if err := f.deletions.DeleteAccount(ctx, actor, target, targetLogin); err != nil {
			t.Fatalf("DeleteAccount: %v", err)
		}

		wantTombstone := "~deleted-" + strings.ReplaceAll(target, "-", "")
		if got := f.loginOf(target); got != wantTombstone {
			t.Fatalf("login=%q want the tombstone %q", got, wantTombstone)
		}
		var deleted bool
		var email, defaultHousehold *string
		if err := f.admin.QueryRow(ctx, `SELECT deleted_at IS NOT NULL, email, default_household_id::text
			FROM user_accounts WHERE id=$1::uuid`, target).Scan(&deleted, &email, &defaultHousehold); err != nil {
			t.Fatal(err)
		}
		if !deleted {
			t.Fatal("deleted_at was not set")
		}
		if email != nil {
			t.Fatalf("email survived the deletion: %q", *email)
		}
		if defaultHousehold != nil {
			t.Fatalf("default household survived the deletion: %q", *defaultHousehold)
		}

		for name, query := range map[string]string{
			"credentials": `SELECT count(*) FROM local_credentials WHERE user_id=$1::uuid`,
			"sessions":    `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`,
			"identities":  `SELECT count(*) FROM oidc_identities WHERE user_id=$1::uuid`,
			"link flows":  `SELECT count(*) FROM oidc_flows WHERE user_id=$1::uuid`,
		} {
			if got := f.scalarInt(query, target); got != 0 {
				t.Fatalf("%s left behind: %d", name, got)
			}
		}

		// ADR 0022 "MCP tokens" / ADR 0023: no MCP code or token survives, each
		// grant is revoked (kept, secret-free) with an audited cause, and another
		// account's grant on the same client is untouched.
		if got := f.mcpSecretRows(target); got != 0 {
			t.Fatalf("MCP codes and tokens left behind: %d", got)
		}
		for _, grant := range []string{firstGrant, secondGrant} {
			if reason := f.scalarString(`SELECT coalesce(revoke_reason, 'live') FROM mcp_grants
				WHERE id=$1::uuid AND revoked_at IS NOT NULL`, grant); reason != "account_deletion" {
				t.Fatalf("grant %s revoke_reason=%q want account_deletion", grant, reason)
			}
			if got := f.scalarInt(`SELECT count(*) FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked'
				AND reason='account_deletion' AND actor_user_id=$2::uuid AND client_name='Deletion test client'`, grant, actor); got != 1 {
				t.Fatalf("grant %s revocation events=%d want 1", grant, got)
			}
		}
		if got := f.scalarInt(`SELECT count(*) FROM mcp_grants WHERE id=$1::uuid AND revoked_at IS NULL`, bystander); got != 1 {
			t.Fatal("another account's MCP grant was revoked")
		}
		if got := f.mcpSecretRows(keeper); got != 5 {
			t.Fatalf("another account's MCP secret rows=%d want 5", got)
		}

		if !f.invitationRevoked(invitation) {
			t.Fatal("the invitation the account created is still pending")
		}
		if !f.resetRevoked(targetReset) {
			t.Fatal("the reset link targeting the account is still outstanding")
		}
		if !f.resetRevoked(issuedReset) {
			t.Fatal("the reset link the account issued is still outstanding")
		}

		for householdID, wantRole := range map[string]string{first: household.RoleAdmin, second: household.RoleMember} {
			if _, _, ok := f.membership(householdID, target); ok {
				t.Fatalf("membership in %s survived the deletion", householdID)
			}
			var previousRole, authority, removalActor string
			if err := f.admin.QueryRow(ctx, `SELECT previous_role, actor_authority, actor_user_id::text
				FROM household_member_removals WHERE household_id=$1::uuid AND target_user_id=$2::uuid`,
				householdID, target).Scan(&previousRole, &authority, &removalActor); err != nil {
				t.Fatalf("removal audit for %s: %v", householdID, err)
			}
			if previousRole != wantRole || authority != household.AuthorityInstanceAdmin || removalActor != actor {
				t.Fatalf("removal audit for %s previous_role=%q authority=%q actor=%s want %q %q %s",
					householdID, previousRole, authority, removalActor, wantRole, household.AuthorityInstanceAdmin, actor)
			}
		}

		if linked, _, archived := f.subject(person); linked != "" || archived {
			t.Fatalf("member person linked_user_id=%q archived=%t want them unlinked and live", linked, archived)
		}
		if name := f.scalarString(`SELECT name FROM subjects WHERE id=$1::uuid`, person); name != "delete target person" {
			t.Fatalf("member person name=%q want the subject kept intact", name)
		}
		if responsible, _, _, _ := f.item(first, item); responsible != "" {
			t.Fatalf("item still responsible for the deleted account: %s", responsible)
		}
		if got := f.scalarString(`SELECT completed_by_user_id::text FROM item_completions WHERE id=$1::uuid`, completion); got != target {
			t.Fatalf("completion actor=%s want the deleted account id %s", got, target)
		}

		if got := f.scalarInt(`SELECT count(*) FROM instance_admins WHERE user_id=$1::uuid`, target); got != 0 {
			t.Fatalf("instance-administrator grant survived: %d", got)
		}
		var source, reason, revocationActor string
		var revocationGrantedAt time.Time
		if err := f.admin.QueryRow(ctx, `SELECT grant_source, reason, actor_user_id::text, granted_at
			FROM instance_admin_revocations WHERE user_id=$1::uuid`, target).
			Scan(&source, &reason, &revocationActor, &revocationGrantedAt); err != nil {
			t.Fatalf("revocation audit: %v", err)
		}
		if source != "cli" || reason != "account_deletion" || revocationActor != actor || !revocationGrantedAt.UTC().Equal(grantedAt.UTC()) {
			t.Fatalf("revocation audit source=%q reason=%q actor=%s granted_at=%s want cli account_deletion %s %s",
				source, reason, revocationActor, revocationGrantedAt.UTC(), actor, grantedAt.UTC())
		}

		var deletionActor string
		var deletedAt time.Time
		if err := f.admin.QueryRow(ctx, `SELECT actor_user_id::text, deleted_at FROM account_deletions WHERE user_id=$1::uuid`, target).
			Scan(&deletionActor, &deletedAt); err != nil {
			t.Fatalf("account deletion record: %v", err)
		}
		if deletionActor != actor || !deletedAt.UTC().Equal(f.clock().UTC()) {
			t.Fatalf("deletion record actor=%s deleted_at=%s want %s %s", deletionActor, deletedAt.UTC(), actor, f.clock().UTC())
		}

		if _, err := identitypostgres.New(f.app, nil).FindLogin(ctx, targetLogin); !errors.Is(err, identityapp.ErrNotFound) {
			t.Fatalf("FindLogin of the deleted login err=%v want ErrNotFound", err)
		}
		var replacement string
		if err := f.admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, targetLogin).Scan(&replacement); err != nil {
			t.Fatalf("the freed login is not reusable: %v", err)
		}
		if replacement == target {
			t.Fatal("the reused login resolved to the tombstoned account")
		}

		if got := f.scalarInt(`SELECT count(*) FROM instance_admins g
			JOIN user_accounts u ON u.id = g.user_id WHERE u.deleted_at IS NULL`); got < 1 {
			t.Fatalf("no live instance administrator remains: %d", got)
		}
		if got := f.scalarInt(`SELECT count(*) FROM instance_admins WHERE user_id=$1::uuid`, keeper); got != 1 {
			t.Fatalf("the second instance administrator lost its grant: %d", got)
		}
	})

	t.Run("a grant created after the deletion clock is revoked without violating the CHECK", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("late_grant_actor")
		f.grantInstanceAdmin(actor)
		keeper := f.newUserWithLogin("late_grant_keeper")
		f.grantInstanceAdmin(keeper)
		house := f.newHousehold("late grant house")
		f.join(actor, house, household.RoleAdmin)

		const targetLogin = "late_grant_target"
		target := f.newUserWithLogin(targetLogin)
		f.join(target, house, household.RoleMember)
		f.password(target)
		f.addSession(target)
		// The fixture clock is 10:00; this grant was created at 10:00:00.4, after it.
		grant := f.addMCPGrantAt(target, "late-grant-client-aaaa", "2026-05-04 10:00:00.4+00")
		if got := f.mcpSecretRows(target); got != 5 {
			t.Fatalf("fixture MCP secret rows=%d want 5", got)
		}

		if err := f.deletions.DeleteAccount(ctx, actor, target, targetLogin); err != nil {
			t.Fatalf("DeleteAccount with a clock earlier than created_at: %v", err)
		}
		if got := f.mcpSecretRows(target); got != 0 {
			t.Fatalf("surviving MCP secret rows after deletion: %d", got)
		}
		var revoked, ordered bool
		var reason string
		if err := f.admin.QueryRow(ctx, `SELECT revoked_at IS NOT NULL, revoke_reason, revoked_at >= created_at
			FROM mcp_grants WHERE id=$1::uuid`, grant).Scan(&revoked, &reason, &ordered); err != nil {
			t.Fatal(err)
		}
		if !revoked || reason != "account_deletion" || !ordered {
			t.Fatalf("grant revoked=%t reason=%q revoked_at>=created_at=%t, want revoked account_deletion ordered", revoked, reason, ordered)
		}
	})

	t.Run("every refusal changes nothing", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("refusal_actor")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("refusal house")
		otherAdmin := f.newUserWithLogin("refusal_other_admin")
		f.join(otherAdmin, house, household.RoleAdmin)

		const targetLogin = "refusal_target"
		target := f.newUserWithLogin(targetLogin)
		f.join(target, house, household.RoleMember)
		f.setDefault(target, house)
		f.setEmail(target, "refusal_target@example.test")
		f.password(target)
		f.addSession(target)
		f.addIdentity(target, "https://issuer.test", "refusal-subject")
		f.addMCPGrant(target, "refusal-client-aaaaaaaa")

		lastAdmin := f.newUserWithLogin("refusal_last_admin")
		lastAdminHouse := f.newHousehold("refusal last admin house")
		f.join(lastAdmin, lastAdminHouse, household.RoleAdmin)

		assertUnchanged := func(label, targetID string, call func() error, wantErr error) {
			t.Helper()
			before := f.deletionSnapshot(targetID)
			if err := call(); !errors.Is(err, wantErr) {
				t.Fatalf("%s err=%v want %v", label, err, wantErr)
			}
			if after := f.deletionSnapshot(targetID); after != before {
				t.Fatalf("%s changed the account: before=%q after=%q", label, before, after)
			}
		}

		assertUnchanged("last household admin", lastAdmin,
			func() error { return f.deletions.DeleteAccount(ctx, actor, lastAdmin, "refusal_last_admin") },
			household.ErrLastAdmin)
		assertUnchanged("self deletion", actor,
			func() error { return f.deletions.DeleteAccount(ctx, actor, actor, "refusal_actor") },
			household.ErrCannotDeleteSelf)

		// The confirmation mismatch is a validation error, never a not-found.
		before := f.deletionSnapshot(target)
		for _, confirm := range []string{strings.ToUpper(targetLogin), "refusal_target ", ""} {
			err := f.deletions.DeleteAccount(ctx, actor, target, confirm)
			var validation *household.ValidationError
			if !errors.As(err, &validation) || validation.Field != "confirmLogin" || validation.Code != "mismatch" {
				t.Fatalf("confirmation %q err=%v want ValidationError{confirmLogin, mismatch}", confirm, err)
			}
		}
		if after := f.deletionSnapshot(target); after != before {
			t.Fatalf("confirmation refusals changed the account: before=%q after=%q", before, after)
		}

		outsider := f.newUserWithLogin("refusal_outsider")
		assertUnchanged("non-admin actor", target,
			func() error { return f.deletions.DeleteAccount(ctx, outsider, target, targetLogin) },
			household.ErrInstanceAdminRequired)

		// An unknown identifier never reaches a row.
		unknown := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff"
		if err := f.deletions.DeleteAccount(ctx, actor, unknown, "whoever"); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown target err=%v want ErrNotFound", err)
		}

		// An account deleted by someone else's committed deletion is not found
		// on a retry, so a stale confirmation cannot delete anything twice.
		victim := f.newUserWithLogin("refusal_victim")
		tombstoneLogin := tombstoneAccountFixture(t, ctx, f.admin, victim, false)
		assertUnchanged("already deleted target", victim,
			func() error { return f.deletions.DeleteAccount(ctx, actor, victim, tombstoneLogin) },
			household.ErrNotFound)

		if got := f.scalarInt(`SELECT count(*) FROM account_deletions`); got != 0 {
			t.Fatalf("refusals wrote %d account deletion records", got)
		}
		if got := f.scalarInt(`SELECT count(*) FROM instance_admin_revocations`); got != 0 {
			t.Fatalf("refusals wrote %d revocation records", got)
		}
	})

	t.Run("deleting one of only two instance administrators succeeds", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("only_two_actor")
		f.grantInstanceAdmin(actor)
		const targetLogin = "only_two_target"
		target := f.newUserWithLogin(targetLogin)
		f.grantInstanceAdmin(target)

		if err := f.deletions.DeleteAccount(ctx, actor, target, targetLogin); err != nil {
			t.Fatalf("DeleteAccount: %v", err)
		}
		if got := f.scalarInt(`SELECT count(*) FROM instance_admins g
			JOIN user_accounts u ON u.id = g.user_id WHERE u.deleted_at IS NULL`); got != 1 {
			t.Fatalf("live instance administrators=%d want only the actor", got)
		}
		var remaining string
		if err := f.admin.QueryRow(ctx, `SELECT user_id::text FROM instance_admins`).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != actor {
			t.Fatalf("remaining instance administrator=%s want %s", remaining, actor)
		}
		if got := f.scalarInt(`SELECT count(*) FROM instance_admin_revocations WHERE user_id=$1::uuid`, target); got != 1 {
			t.Fatalf("revocation audit rows=%d want 1", got)
		}
	})

	// ErrLastInstanceAdmin is unreachable through the service: the actor is a
	// live instance administrator, the target is a different account, and
	// another administrator's own live grant always remains in the count. The
	// guard is defense in depth for a future caller that relaxes the actor
	// check, so it is asserted directly on the transaction statement.
	t.Run("last instance administrator guard is defense in depth", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("guard_actor")
		f.grantInstanceAdmin(actor)
		target := f.newUserWithLogin("guard_target")
		f.grantInstanceAdmin(target)

		var excluding, everyone int
		if err := f.repository.WithAccountDeletionTransaction(ctx, func(tx household.AccountDeletionTransaction, effects household.AccountDeletionEffects) error {
			var err error
			if excluding, err = tx.CountLiveInstanceAdminsExcluding(ctx, target); err != nil {
				return err
			}
			everyone, err = tx.CountLiveInstanceAdminsExcluding(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if excluding != 1 || everyone != 2 {
			t.Fatalf("live instance administrators excluding target=%d for an unknown id=%d want 1 and 2", excluding, everyone)
		}
	})
}

// TestAccountDeletionRacesAgainstPostgres covers the serialization guarantees
// of ADR 0022 and the ADR 0019 global lock order. A 40P01 deadlock is never
// accepted.
func TestAccountDeletionRacesAgainstPostgres(t *testing.T) {
	ctx, f := newAccountDeletionFixture(t)

	t.Run("two administrators deleting each other leave one live administrator", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 5; round++ {
			a := f.newUserWithLogin(fmt.Sprintf("mutual_delete_a_%d", round))
			b := f.newUserWithLogin(fmt.Sprintf("mutual_delete_b_%d", round))
			f.grantInstanceAdmin(a)
			f.grantInstanceAdmin(b)

			results := race(
				func() error { return f.deletions.DeleteAccount(ctx, a, b, fmt.Sprintf("mutual_delete_b_%d", round)) },
				func() error { return f.deletions.DeleteAccount(ctx, b, a, fmt.Sprintf("mutual_delete_a_%d", round)) },
			)
			successes, refused := 0, 0
			for i, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrInstanceAdminRequired):
					refused++
				default:
					expectRaceError(t, round, fmt.Sprintf("deletion %d", i), err, household.ErrInstanceAdminRequired)
				}
			}
			if successes != 1 || refused != 1 {
				t.Fatalf("round %d successes=%d instance-admin refusals=%d results=%v", round, successes, refused, results)
			}
			if live := f.scalarInt(`SELECT count(*) FROM instance_admins g
				JOIN user_accounts u ON u.id = g.user_id WHERE u.deleted_at IS NULL`); live < 1 {
				t.Fatalf("round %d no live instance administrator remains", round)
			}
			if got := f.scalarInt(`SELECT count(*) FROM account_deletions WHERE user_id IN ($1::uuid, $2::uuid)`, a, b); got != 1 {
				t.Fatalf("round %d deletion records=%d want exactly one", round, got)
			}
		}
	})

	t.Run("a membership that appeared before the account lock is removed on retry", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("grew_actor")
		f.grantInstanceAdmin(actor)
		const targetLogin = "grew_target"
		target := f.newUserWithLogin(targetLogin)
		first := f.newHousehold("grew first house")
		f.join(target, first, household.RoleMember)
		late := f.newHousehold("grew late house")
		f.setDefault(target, first)

		// A raw transaction on another connection holds the target account row
		// and inserts a membership the deletion's unlocked household read cannot
		// see. Committing it while the deletion waits must force the retry.
		raw, err := f.app.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = raw.Rollback(context.Background()) }()
		if _, err = raw.Exec(ctx, `SELECT 1 FROM user_accounts WHERE id=$1::uuid FOR NO KEY UPDATE`, target); err != nil {
			t.Fatalf("lock the target account: %v", err)
		}
		if _, err = raw.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role)
			VALUES($1::uuid, $2::uuid, 'member')`, target, late); err != nil {
			t.Fatalf("insert the late membership: %v", err)
		}

		result := make(chan error, 1)
		go func() { result <- f.deletions.DeleteAccount(ctx, actor, target, targetLogin) }()
		waitForAppLockWait(t, ctx, f.app)
		if err := raw.Commit(ctx); err != nil {
			t.Fatalf("commit the late membership: %v", err)
		}
		if err := <-result; err != nil {
			t.Fatalf("the deletion did not retry past the grown membership set: %v", err)
		}

		if _, _, ok := f.membership(first, target); ok {
			t.Fatal("the known membership survived the deletion")
		}
		if _, _, ok := f.membership(late, target); ok {
			t.Fatal("the membership that appeared after the unlocked read survived the retry")
		}
		if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid AND target_user_id=$2::uuid`, late, target); got != 1 {
			t.Fatalf("removal audit rows for the late household=%d want 1 (proof of the retry)", got)
		}
		if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE target_user_id=$1::uuid`, target); got != 2 {
			t.Fatalf("removal audit rows=%d want one per removed membership", got)
		}
		if got := f.loginOf(target); got != "~deleted-"+strings.ReplaceAll(target, "-", "") {
			t.Fatalf("login=%q want the tombstone", got)
		}
	})

	t.Run("deletion races a household administrator removing the same member", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 5; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("delete_remove_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("delete remove house %d", round))
			houseAdmin := f.newUserWithLogin(fmt.Sprintf("delete_remove_admin_%d", round))
			f.join(houseAdmin, house, household.RoleAdmin)
			targetLogin := fmt.Sprintf("delete_remove_target_%d", round)
			target := f.newUserWithLogin(targetLogin)
			f.join(target, house, household.RoleMember)
			f.setDefault(target, house)

			results := race(
				func() error { return f.deletions.DeleteAccount(ctx, actor, target, targetLogin) },
				func() error { return f.service.RemoveMember(ctx, houseAdmin, house, target, 1) },
			)
			if results[0] != nil {
				expectRaceError(t, round, "deletion", results[0])
			}
			if results[1] != nil {
				expectRaceError(t, round, "household removal", results[1], household.ErrNotFound)
			}
			if _, _, ok := f.membership(house, target); ok {
				t.Fatalf("round %d membership survived both paths", round)
			}
			if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid AND target_user_id=$2::uuid`, house, target); got != 1 {
				t.Fatalf("round %d removal audit rows=%d want exactly one", round, got)
			}
			if admins := f.adminCount(house); admins < 1 {
				t.Fatalf("round %d household lost every admin", round)
			}
		}
	})

	// The household-admin half of last-admin protection under concurrency
	// (SECURITY "Authorization invariants"): a household whose only admins are
	// A and B must never end without an admin when an instance administrator
	// deletes A while B gives up the admin role. Deletion locks every household
	// of the target before it counts the remaining admins, the same row the
	// household paths lock, so exactly one side commits and the other is
	// refused with last_admin.
	for _, step := range []struct {
		name string
		// giveUp makes B stop being an admin of house through a household path.
		giveUp func(b, house string) error
		// bRole is B's role after giveUp committed; "" means B left.
		bRole string
	}{
		{"deleting a household admin races the other admin's self-demotion", func(b, house string) error {
			_, err := f.accounts.ChangeRole(ctx, b, house, b, household.RoleMember, 1)
			return err
		}, household.RoleMember},
		{"deleting a household admin races the other admin's self-leave", func(b, house string) error {
			return f.service.RemoveMember(ctx, b, house, b, 1)
		}, ""},
	} {
		t.Run(step.name, func(t *testing.T) {
			f.prepare()
			deletionWins := 0
			for round := 0; round < 10; round++ {
				actor := f.newUserWithLogin(fmt.Sprintf("admin_race_actor_%d", round))
				f.grantInstanceAdmin(actor)
				house := f.newHousehold(fmt.Sprintf("admin race house %d", round))
				aLogin := fmt.Sprintf("admin_race_a_%d", round)
				a := f.newUserWithLogin(aLogin)
				b := f.newUserWithLogin(fmt.Sprintf("admin_race_b_%d", round))
				f.join(a, house, household.RoleAdmin)
				f.join(b, house, household.RoleAdmin)
				// The deletion revokes this invitation before it refuses, so a
				// refused deletion proves its rollback by leaving it unrevoked.
				invitation := f.addInvitation(house, a, fmt.Sprintf("admin-race-invite-%d", round))
				beforeA := f.deletionSnapshot(a)

				deletion := func() error { return f.deletions.DeleteAccount(ctx, actor, a, aLogin) }
				giveUp := func() error { return step.giveUp(b, house) }
				// Even rounds race freely, which is what catches a missing
				// household lock; B practically always wins them, because the
				// deletion first takes installation_state. Odd rounds queue the
				// deletion on the household row before B, so the deletion-wins
				// branch below is exercised too.
				var results []error
				if round%2 == 0 {
					results = race(deletion, giveUp)
				} else {
					results = f.queueOnHousehold(t, house, deletion, giveUp)
				}
				successes, refused := 0, 0
				for i, err := range results {
					switch {
					case err == nil:
						successes++
					case errors.Is(err, household.ErrLastAdmin):
						refused++
					default:
						expectRaceError(t, round, fmt.Sprintf("call %d", i), err)
					}
				}
				if successes != 1 || refused != 1 {
					t.Fatalf("round %d successes=%d last-admin refusals=%d results=%v", round, successes, refused, results)
				}
				if admins := f.adminCount(house); admins != 1 {
					t.Fatalf("round %d household admins=%d want exactly one", round, admins)
				}

				if results[0] == nil {
					// The deletion won: A is gone everywhere, and B's refused
					// household path wrote nothing.
					deletionWins++
					deleted := f.scalarInt(`SELECT count(*) FROM account_deletions WHERE user_id=$1::uuid`, a)
					if deleted != 1 || f.loginOf(a) != "~deleted-"+strings.ReplaceAll(a, "-", "") {
						t.Fatalf("round %d deletion committed but deletion records=%d login=%q", round, deleted, f.loginOf(a))
					}
					if !f.invitationRevoked(invitation) {
						t.Fatalf("round %d committed deletion left A's invitation pending", round)
					}
					if role, version, ok := f.membership(house, b); !ok || role != household.RoleAdmin || version != 1 {
						t.Fatalf("round %d refused B role=%q version=%d member=%t want an untouched admin", round, role, version, ok)
					}
					if got := f.roleChangeCount(house); got != 0 {
						t.Fatalf("round %d refused demotion wrote %d role-change audit rows", round, got)
					}
					if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE target_user_id=$1::uuid`, b); got != 0 {
						t.Fatalf("round %d refused leave wrote %d removal audit rows", round, got)
					}
					continue
				}
				// B won: the refused deletion rolled back every effect on A, which
				// stays the household's admin, and B's change committed.
				if after := f.deletionSnapshot(a); after != beforeA {
					t.Fatalf("round %d refused deletion changed A: before=%q after=%q", round, beforeA, after)
				}
				if f.invitationRevoked(invitation) {
					t.Fatalf("round %d refused deletion revoked A's invitation", round)
				}
				if role, _, ok := f.membership(house, a); !ok || role != household.RoleAdmin {
					t.Fatalf("round %d A role=%q member=%t want the remaining admin", round, role, ok)
				}
				role, _, ok := f.membership(house, b)
				if step.bRole == "" && ok {
					t.Fatalf("round %d B left but is still a member with role %q", round, role)
				}
				if step.bRole != "" && (!ok || role != step.bRole) {
					t.Fatalf("round %d B role=%q member=%t want %q", round, role, ok, step.bRole)
				}
			}
			// Free rounds depend on scheduling, so only the queued rounds are
			// asserted to have run the deletion-wins branch.
			if deletionWins < 5 {
				t.Fatalf("deletion won %d of 10 rounds want at least the 5 queued ones", deletionWins)
			}
		})
	}

	// Two deletions are serialized by installation_state FOR UPDATE, not by the
	// household lock: what this race protects is the household last-admin
	// re-count each deletion makes under its locks. It also keeps protecting
	// the invariant if the installation lock is ever relaxed, because the
	// household lock then decides.
	t.Run("two administrators deleting both household admins leave one", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			x := f.newUserWithLogin(fmt.Sprintf("both_admins_x_%d", round))
			y := f.newUserWithLogin(fmt.Sprintf("both_admins_y_%d", round))
			f.grantInstanceAdmin(x)
			f.grantInstanceAdmin(y)
			house := f.newHousehold(fmt.Sprintf("both admins house %d", round))
			aLogin, bLogin := fmt.Sprintf("both_admins_a_%d", round), fmt.Sprintf("both_admins_b_%d", round)
			a, b := f.newUserWithLogin(aLogin), f.newUserWithLogin(bLogin)
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)

			results := race(
				func() error { return f.deletions.DeleteAccount(ctx, x, a, aLogin) },
				func() error { return f.deletions.DeleteAccount(ctx, y, b, bLogin) },
			)
			successes, refused := 0, 0
			for i, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrLastAdmin):
					refused++
				default:
					expectRaceError(t, round, fmt.Sprintf("deletion %d", i), err)
				}
			}
			if successes != 1 || refused != 1 {
				t.Fatalf("round %d successes=%d last-admin refusals=%d results=%v", round, successes, refused, results)
			}
			if admins := f.adminCount(house); admins != 1 {
				t.Fatalf("round %d household admins=%d want exactly one", round, admins)
			}
			if got := f.scalarInt(`SELECT count(*) FROM account_deletions WHERE user_id IN ($1::uuid, $2::uuid)`, a, b); got != 1 {
				t.Fatalf("round %d deletion records=%d want exactly one", round, got)
			}
			if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, house); got != 1 {
				t.Fatalf("round %d removal audit rows=%d want exactly one", round, got)
			}
		}
	})
}

// queueOnHousehold holds householdID's row in a raw transaction, starts first
// and waits until it blocks on that row, then starts second and waits until it
// blocks too, and finally releases the row. PostgreSQL grants a contended row
// lock to its waiters in arrival order, so first takes the household before
// second while both are in flight. It returns the results in call order.
func (f *accountDeletionFixture) queueOnHousehold(t *testing.T, householdID string, first, second func() error) []error {
	t.Helper()
	raw, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Rollback(context.Background()) }()
	if _, err = raw.Exec(f.ctx, `SELECT 1 FROM households WHERE id=$1::uuid FOR NO KEY UPDATE`, householdID); err != nil {
		t.Fatalf("hold the household row: %v", err)
	}
	results := make([]error, 2)
	done := make(chan struct{}, 2)
	start := func(i int, call func() error) {
		go func() {
			results[i] = call()
			done <- struct{}{}
		}()
	}
	start(0, first)
	waitForAppLockWaiters(t, f.ctx, f.app, 1)
	start(1, second)
	waitForAppLockWaiters(t, f.ctx, f.app, 2)
	if err := raw.Commit(f.ctx); err != nil {
		t.Fatalf("release the household row: %v", err)
	}
	<-done
	<-done
	return results
}
