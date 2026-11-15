package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// adminCenterFixture wires household.InstanceAdminCenter exactly as
// cmd/tendo/newInstanceAdminCenter does (ADR 0020), on top of the removal
// fixture so the household-admin path stays comparable in the same database.
type adminCenterFixture struct {
	*removalFixture
	center     *household.InstanceAdminCenter
	accounts   *household.AccountService
	repository *InstanceAdminCenterRepository
}

func newAdminCenterFixture(t *testing.T, ctx context.Context, admin, app *pgxpool.Pool) *adminCenterFixture {
	t.Helper()
	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	repository := NewInstanceAdminCenterRepository(app, MemberRemovalEffectFactories{
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
	}, AssignmentEffectFactories{
		Accounts: func(tx householddb.DBTX) household.AssignableAccountLocker {
			return identitypostgres.NewAssignmentEffects(tx)
		},
		Defaults: func(tx householddb.DBTX) household.AssignmentDefaultSetter {
			return identitypostgres.NewAssignmentEffects(tx)
		},
		Persons: func(tx householddb.DBTX) household.AssignmentPersonProvisioner {
			return subjectpostgres.NewMemberPersonProvisioner(tx)
		},
	}, func(tx householddb.DBTX) household.AccountDirectory {
		return identitypostgres.NewAccountDirectory(tx)
	})
	identity := identitypostgres.New(app, nil)
	return &adminCenterFixture{
		removalFixture: newRemovalFixture(t, ctx, admin, app),
		center: household.NewInstanceAdminCenter(
			repository, identity, identity, clock),
		accounts:   AccountServiceFor(app, identity, identity, emptyResetLookup{}, clock),
		repository: repository,
	}
}

// prepare resets the installation and completes setup, so a granted account is
// a real instance administrator rather than a pre-setup installation.
func (f *adminCenterFixture) prepare() {
	f.t.Helper()
	f.reset()
	f.setSetupRequired(false)
}

func (f *adminCenterFixture) setSetupRequired(required bool) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `UPDATE installation_state SET setup_required=$1`, required); err != nil {
		f.t.Fatal(err)
	}
}

func (f *adminCenterFixture) newUserWithLogin(login string) string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *adminCenterFixture) grantInstanceAdmin(userID string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx,
		`INSERT INTO instance_admins(user_id, granted_at, grant_source) VALUES ($1::uuid, now(), 'cli')`, userID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *adminCenterFixture) scalarInt(query string, args ...any) int {
	f.t.Helper()
	var value int
	if err := f.admin.QueryRow(f.ctx, query, args...).Scan(&value); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return value
}

func (f *adminCenterFixture) scalarString(query string, args ...any) string {
	f.t.Helper()
	var value string
	if err := f.admin.QueryRow(f.ctx, query, args...).Scan(&value); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return value
}

func (f *adminCenterFixture) accountExists(userID string) bool {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM user_accounts WHERE id=$1::uuid`, userID) == 1
}

func (f *adminCenterFixture) membershipCount(householdID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid`, householdID)
}

func (f *adminCenterFixture) userMembershipCount(householdID, userID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid AND user_id=$2::uuid`,
		householdID, userID)
}

func (f *adminCenterFixture) personLinks(householdID, userID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM subjects WHERE household_id=$1::uuid AND type='person' AND linked_user_id=$2::uuid`,
		householdID, userID)
}

func (f *adminCenterFixture) assignmentCount(householdID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM household_member_assignments WHERE household_id=$1::uuid`, householdID)
}

type memberAssignmentRow struct {
	Target, Actor, Role, Authority string
}

func (f *adminCenterFixture) assignmentRows(householdID string) []memberAssignmentRow {
	f.t.Helper()
	rows, err := f.admin.Query(f.ctx,
		`SELECT target_user_id::text, actor_user_id::text, role, actor_authority
		 FROM household_member_assignments WHERE household_id=$1::uuid ORDER BY assigned_at, id`, householdID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []memberAssignmentRow
	for rows.Next() {
		var row memberAssignmentRow
		if err := rows.Scan(&row.Target, &row.Actor, &row.Role, &row.Authority); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *adminCenterFixture) roleChangeCount(householdID string) int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM household_role_changes WHERE household_id=$1::uuid`, householdID)
}

// roleChangeAuthorities returns the recorded authority of every role change of
// the household in insertion order.
func (f *adminCenterFixture) roleChangeAuthorities(householdID string) []string {
	f.t.Helper()
	rows, err := f.admin.Query(f.ctx,
		`SELECT actor_authority FROM household_role_changes WHERE household_id=$1::uuid ORDER BY changed_at, id`, householdID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var authority string
		if err := rows.Scan(&authority); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, authority)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// removalAuthorities returns the recorded authority of every removal of the
// household in insertion order.
func (f *adminCenterFixture) removalAuthorities(householdID string) []string {
	f.t.Helper()
	rows, err := f.admin.Query(f.ctx,
		`SELECT actor_authority FROM household_member_removals WHERE household_id=$1::uuid ORDER BY removed_at, id`, householdID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var authority string
		if err := rows.Scan(&authority); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, authority)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *adminCenterFixture) totalAuditRows() int {
	f.t.Helper()
	return f.scalarInt(`SELECT (SELECT count(*) FROM household_role_changes)
		+ (SELECT count(*) FROM household_member_removals)
		+ (SELECT count(*) FROM household_member_assignments)`)
}

// requireMembership asserts the stored membership role and version.
func (f *adminCenterFixture) requireMembership(householdID, userID, role string, version int64) {
	f.t.Helper()
	gotRole, gotVersion, ok := f.membership(householdID, userID)
	if !ok || gotRole != role || gotVersion != version {
		f.t.Fatalf("membership %s/%s role=%q version=%d ok=%t, want %q version %d",
			householdID, userID, gotRole, gotVersion, ok, role, version)
	}
}

func TestInstanceAdminCenterAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL instance-admin center tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newAdminCenterFixture(t, ctx, admin, app)
	f.prepare()
	defer f.reset()

	t.Run("without the grant every method refuses and changes nothing", func(t *testing.T) {
		f.prepare()
		house := f.newHousehold("gate house")
		actor := f.newUserWithLogin("gate_household_admin")
		member := f.newUserWithLogin("gate_member")
		outsider := f.newUserWithLogin("gate_outsider")
		f.join(actor, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)

		for name, err := range map[string]error{
			"RequireInstanceAdmin": f.center.RequireInstanceAdmin(ctx, actor),
			"ListDirectory":        thirdError(f.center.ListDirectory(ctx, actor, "", "", 10)),
			"AssignAccount":        thirdError(f.center.AssignAccount(ctx, actor, house, outsider, household.RoleMember)),
			"ChangeRole":           secondError(f.center.ChangeRole(ctx, actor, house, member, household.RoleAdmin, 1)),
			"RemoveAccount":        f.center.RemoveAccount(ctx, actor, house, member, 1),
		} {
			if !errors.Is(err, household.ErrInstanceAdminRequired) {
				t.Fatalf("%s err=%v want instance_admin_required", name, err)
			}
		}
		if _, _, err := f.center.ListHouseholds(ctx, actor, "", 10); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("ListHouseholds err=%v", err)
		}
		if _, _, err := f.center.ListAccounts(ctx, actor, house, "", 10); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("ListAccounts err=%v", err)
		}
		if _, err := f.center.GetAccount(ctx, actor, house, member); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("GetAccount err=%v", err)
		}
		if _, _, err := f.center.ListHouseholds(ctx, "not-a-uuid", "", 10); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("malformed actor err=%v want instance_admin_required", err)
		}

		f.requireMembership(house, actor, household.RoleAdmin, 1)
		f.requireMembership(house, member, household.RoleMember, 1)
		if _, _, ok := f.membership(house, outsider); ok {
			t.Fatal("refused assignment created a membership")
		}
		if got := f.totalAuditRows(); got != 0 {
			t.Fatalf("refused calls wrote %d audit rows", got)
		}
		if got := f.defaultHousehold(outsider); got != nil {
			t.Fatalf("refused assignment set default household %q", *got)
		}
	})

	t.Run("setup still required is treated as no grant", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("setup_gate_admin")
		house := f.newHousehold("setup gate house")
		target := f.newUserWithLogin("setup_gate_target")
		f.grantInstanceAdmin(actor)
		f.setSetupRequired(true)
		if err := f.center.RequireInstanceAdmin(ctx, actor); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("pre-check err=%v want instance_admin_required", err)
		}
		if _, _, err := f.center.ListHouseholds(ctx, actor, "", 10); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("ListHouseholds err=%v", err)
		}
		if _, _, err := f.center.AssignAccount(ctx, actor, house, target, household.RoleAdmin); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("AssignAccount err=%v", err)
		}
		if _, _, ok := f.membership(house, target); ok {
			t.Fatal("pre-setup assignment wrote a membership")
		}
		f.setSetupRequired(false)
		if err := f.center.RequireInstanceAdmin(ctx, actor); err != nil {
			t.Fatalf("post-setup pre-check err=%v", err)
		}
	})

	t.Run("households list every installation household in id order with a cursor", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("household_list_admin")
		f.grantInstanceAdmin(actor)
		own := f.newHousehold("household list own")
		other := f.newHousehold("household list other")
		third := f.newHousehold("household list third")
		f.join(actor, own, household.RoleAdmin)

		first, next, err := f.center.ListHouseholds(ctx, actor, "", 2)
		if err != nil {
			t.Fatalf("first page: %v", err)
		}
		if len(first) != 2 || next == "" {
			t.Fatalf("first page len=%d next=%q", len(first), next)
		}
		second, next2, err := f.center.ListHouseholds(ctx, actor, next, 2)
		if err != nil {
			t.Fatalf("second page: %v", err)
		}
		if len(second) != 1 || next2 != "" {
			t.Fatalf("second page len=%d next=%q", len(second), next2)
		}
		got := []household.AdminHousehold{first[0], first[1], second[0]}
		want := []string{own, other, third}
		sort.Strings(want)
		for i, item := range got {
			if item.ID != want[i] {
				t.Fatalf("household %d id=%s want %s", i, item.ID, want[i])
			}
			if item.Timezone != "UTC" || item.CreatedAt.IsZero() || item.Name == "" {
				t.Fatalf("household %d representation=%+v", i, item)
			}
		}
		if _, _, err := f.center.ListHouseholds(ctx, actor, "forged", 2); !isValidationError(err) {
			t.Fatalf("forged cursor err=%v want validation", err)
		}
	})

	t.Run("directory matches logins literally and case-insensitively with keyset paging", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("directory_admin")
		f.grantInstanceAdmin(actor)
		logins := []string{
			"dir_alpha", "DIR_BRAVO", "dir_charlie",
			"dir_lit_a%b", "dir_lit_a_b", "DIR_LIT_AXB",
			"dir_tie", "DIR_TIE",
		}
		byLogin := map[string]string{}
		for _, login := range logins {
			byLogin[login] = f.newUserWithLogin(login)
		}
		// The instance administrator's own account is part of the directory.
		byLogin["directory_admin"] = actor

		for search, want := range map[string][]string{
			"DIR_ALPHA": {"dir_alpha"},
			"a%b":       {"dir_lit_a%b"},
			"a_b":       {"dir_lit_a_b"},
			"%":         {"dir_lit_a%b"},
			"lit_a_b":   {"dir_lit_a_b"},
			"LIT_A%B":   {"dir_lit_a%b"},
			"bravo":     {"DIR_BRAVO"},
		} {
			got, _, err := f.center.ListDirectory(ctx, actor, search, "", 10)
			if err != nil {
				t.Fatalf("search %q: %v", search, err)
			}
			gotLogins := make([]string, 0, len(got))
			for _, account := range got {
				gotLogins = append(gotLogins, account.Login)
			}
			sort.Strings(gotLogins)
			sortedWant := append([]string(nil), want...)
			sort.Strings(sortedWant)
			if strings.Join(gotLogins, ",") != strings.Join(sortedWant, ",") {
				t.Fatalf("search %q logins=%v want %v", search, gotLogins, sortedWant)
			}
		}

		var seen []household.DirectoryAccount
		cursor := ""
		for page := 0; page < 20; page++ {
			accounts, next, err := f.center.ListDirectory(ctx, actor, "", cursor, 2)
			if err != nil {
				t.Fatalf("page %d: %v", page, err)
			}
			seen = append(seen, accounts...)
			if next == "" {
				break
			}
			cursor = next
		}
		if len(seen) != len(byLogin) {
			t.Fatalf("paged accounts=%d want %d", len(seen), len(byLogin))
		}
		// Contract ordering is (lower(login), id); the database collation, not
		// Go byte order, decides the literal order, so the expectation is read
		// with exactly the contract's ORDER BY.
		expectedRows, err := admin.Query(ctx, `SELECT id::text, login FROM user_accounts ORDER BY lower(login), id`)
		if err != nil {
			t.Fatal(err)
		}
		var want []household.DirectoryAccount
		for expectedRows.Next() {
			var account household.DirectoryAccount
			if err := expectedRows.Scan(&account.UserID, &account.Login); err != nil {
				expectedRows.Close()
				t.Fatal(err)
			}
			want = append(want, account)
		}
		expectedRows.Close()
		if err := expectedRows.Err(); err != nil {
			t.Fatal(err)
		}
		for i := range want {
			if seen[i] != want[i] {
				t.Fatalf("paged account %d=%+v want %+v", i, seen[i], want[i])
			}
		}
		if _, _, err := f.center.ListDirectory(ctx, actor, "", "forged", 2); !isValidationError(err) {
			t.Fatalf("forged cursor err=%v want validation", err)
		}
		if _, _, err := f.center.ListDirectory(ctx, actor, strings.Repeat("a", household.MaxDirectorySearchRunes+1), "", 2); !isValidationError(err) {
			t.Fatalf("oversized search err=%v want validation", err)
		}
	})

	t.Run("household accounts are readable without being a member", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("account_read_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("account read house")
		houseAdmin := f.newUserWithLogin("account_read_house_admin")
		member := f.newUserWithLogin("account_read_member")
		f.join(houseAdmin, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)

		accounts, next, err := f.center.ListAccounts(ctx, actor, house, "", 10)
		if err != nil {
			t.Fatalf("ListAccounts: %v", err)
		}
		if len(accounts) != 2 || next != "" {
			t.Fatalf("accounts=%d next=%q", len(accounts), next)
		}
		seen := map[string]household.AdminAccount{}
		for _, account := range accounts {
			seen[account.UserID] = account
		}
		if got := seen[houseAdmin]; got.Login != "account_read_house_admin" || got.Role != household.RoleAdmin || got.Version != 1 {
			t.Fatalf("household admin account=%+v", got)
		}
		if got := seen[member]; got.Login != "account_read_member" || got.Role != household.RoleMember || got.Version != 1 {
			t.Fatalf("member account=%+v", got)
		}

		first, next, err := f.center.ListAccounts(ctx, actor, house, "", 1)
		if err != nil || len(first) != 1 || next == "" {
			t.Fatalf("paged ListAccounts len=%d next=%q err=%v", len(first), next, err)
		}
		rest, next2, err := f.center.ListAccounts(ctx, actor, house, next, 1)
		if err != nil || len(rest) != 1 || next2 != "" {
			t.Fatalf("second ListAccounts page len=%d next=%q err=%v", len(rest), next2, err)
		}
		if first[0].UserID == rest[0].UserID {
			t.Fatal("account cursor repeated a row")
		}

		account, err := f.center.GetAccount(ctx, actor, house, member)
		if err != nil || account.UserID != member || account.Role != household.RoleMember || account.Version != 1 {
			t.Fatalf("GetAccount=%+v err=%v", account, err)
		}

		missing := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff"
		if _, err := f.center.GetAccount(ctx, actor, house, missing); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown account err=%v", err)
		}
		if _, err := f.center.GetAccount(ctx, actor, missing, member); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown household err=%v", err)
		}
		if _, _, err := f.center.ListAccounts(ctx, actor, missing, "", 10); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("ListAccounts unknown household err=%v", err)
		}
		if _, err := f.center.GetAccount(ctx, actor, "malformed", member); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("malformed household err=%v", err)
		}
	})

	t.Run("assignment creates one audited membership with its defaults and person", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("assign_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("assign house")
		other := f.newHousehold("assign other")
		f.join(f.newUserWithLogin("assign_house_admin"), house, household.RoleAdmin)
		otherAdmin := f.newUserWithLogin("assign_other_admin")
		f.join(otherAdmin, other, household.RoleAdmin)

		fresh := f.newUserWithLogin("assign_fresh")
		account, created, err := f.center.AssignAccount(ctx, actor, house, fresh, household.RoleMember)
		if err != nil || !created {
			t.Fatalf("assign fresh created=%t err=%v", created, err)
		}
		if account.UserID != fresh || account.Role != household.RoleMember || account.Version != 1 || account.Login != "assign_fresh" {
			t.Fatalf("assigned account=%+v", account)
		}
		f.requireMembership(house, fresh, household.RoleMember, 1)
		if got := f.personLinks(house, fresh); got != 1 {
			t.Fatalf("member person links=%d want 1", got)
		}
		if got := f.defaultHousehold(fresh); got == nil || *got != house {
			t.Fatalf("fresh default=%v want %s", got, house)
		}
		rows := f.assignmentRows(house)
		if len(rows) != 1 || rows[0].Target != fresh || rows[0].Actor != actor ||
			rows[0].Role != household.RoleMember || rows[0].Authority != household.AuthorityInstanceAdmin {
			t.Fatalf("assignment audit rows=%+v", rows)
		}

		replay, created, err := f.center.AssignAccount(ctx, actor, house, fresh, household.RoleMember)
		if err != nil || created {
			t.Fatalf("replay created=%t err=%v", created, err)
		}
		if replay.Version != 1 || replay.Role != household.RoleMember {
			t.Fatalf("replay account=%+v", replay)
		}
		f.requireMembership(house, fresh, household.RoleMember, 1)
		if got := f.assignmentCount(house); got != 1 {
			t.Fatalf("replay wrote audit rows, total=%d", got)
		}
		if got := f.personLinks(house, fresh); got != 1 {
			t.Fatalf("replay provisioned another person: %d", got)
		}

		if _, _, err := f.center.AssignAccount(ctx, actor, house, fresh, household.RoleAdmin); !errors.Is(err, household.ErrAlreadyMember) {
			t.Fatalf("different role err=%v want already_member", err)
		}
		f.requireMembership(house, fresh, household.RoleMember, 1)
		if got := f.assignmentCount(house); got != 1 {
			t.Fatalf("already-member refusal wrote audit rows, total=%d", got)
		}

		// An account that already has a navigation hint keeps it.
		settled := f.newUserWithLogin("assign_settled")
		f.join(settled, other, household.RoleMember)
		f.setDefault(settled, other)
		if _, created, err := f.center.AssignAccount(ctx, actor, house, settled, household.RoleMember); err != nil || !created {
			t.Fatalf("assign settled created=%t err=%v", created, err)
		}
		if got := f.defaultHousehold(settled); got == nil || *got != other {
			t.Fatalf("settled default=%v want %s (assignment must not move it)", got, other)
		}
		if got := f.personLinks(house, settled); got != 1 {
			t.Fatalf("settled person links=%d want 1", got)
		}

		// Self-assignment is permitted and records the actor as the target.
		repair := f.newHousehold("assign repair house")
		if _, created, err := f.center.AssignAccount(ctx, actor, repair, actor, household.RoleAdmin); err != nil || !created {
			t.Fatalf("self-assignment created=%t err=%v", created, err)
		}
		f.requireMembership(repair, actor, household.RoleAdmin, 1)
		selfRows := f.assignmentRows(repair)
		if len(selfRows) != 1 || selfRows[0].Target != actor || selfRows[0].Actor != actor ||
			selfRows[0].Role != household.RoleAdmin || selfRows[0].Authority != household.AuthorityInstanceAdmin {
			t.Fatalf("self-assignment audit rows=%+v", selfRows)
		}
	})

	t.Run("assignment refuses a member into an admin-less household", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("assign_guard_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("assign admin-less house")
		member := f.newUserWithLogin("assign_guard_member")
		admin := f.newUserWithLogin("assign_guard_target_admin")

		if _, _, err := f.center.AssignAccount(ctx, actor, house, member, household.RoleMember); !errors.Is(err, household.ErrHouseholdRequiresAdmin) {
			t.Fatalf("member into admin-less household err=%v want household_requires_admin", err)
		}
		if got := f.membershipCount(house); got != 0 {
			t.Fatalf("refused assignment wrote %d memberships", got)
		}
		if got := f.assignmentCount(house); got != 0 {
			t.Fatalf("refused assignment wrote %d audit rows", got)
		}
		if got := f.personLinks(house, member); got != 0 {
			t.Fatalf("refused assignment provisioned %d persons", got)
		}
		if got := f.defaultHousehold(member); got != nil {
			t.Fatalf("refused assignment set default=%q", *got)
		}

		if _, created, err := f.center.AssignAccount(ctx, actor, house, admin, household.RoleAdmin); err != nil || !created {
			t.Fatalf("admin into admin-less household created=%t err=%v", created, err)
		}
		if _, created, err := f.center.AssignAccount(ctx, actor, house, member, household.RoleMember); err != nil || !created {
			t.Fatalf("member after repair created=%t err=%v", created, err)
		}
		f.requireMembership(house, admin, household.RoleAdmin, 1)
		f.requireMembership(house, member, household.RoleMember, 1)
	})

	t.Run("uppercase target identifiers commit once and answer with the stored spelling", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("upper_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("upper house")
		houseAdmin := f.newUserWithLogin("upper_house_admin")
		f.join(houseAdmin, house, household.RoleAdmin)
		target := f.newUserWithLogin("upper_target")

		assigned, created, err := f.center.AssignAccount(ctx, actor, house, strings.ToUpper(target), household.RoleMember)
		if err != nil || !created {
			t.Fatalf("uppercase assign created=%t err=%v", created, err)
		}
		if assigned.UserID != target || assigned.Login != "upper_target" || assigned.Version != 1 {
			t.Fatalf("uppercase assign account=%+v", assigned)
		}
		if got := f.assignmentCount(house); got != 1 {
			t.Fatalf("uppercase assign audit rows=%d want 1", got)
		}

		promoted, err := f.center.ChangeRole(ctx, actor, house, strings.ToUpper(target), household.RoleAdmin, 1)
		if err != nil || promoted.UserID != target || promoted.Login != "upper_target" || promoted.Version != 2 {
			t.Fatalf("uppercase instance-admin promote=%+v err=%v", promoted, err)
		}
		demoted, err := f.accounts.ChangeRole(ctx, houseAdmin, house, strings.ToUpper(target), household.RoleMember, 2)
		if err != nil || demoted.UserID != target || demoted.Login != "upper_target" || demoted.Version != 3 {
			t.Fatalf("uppercase household-admin demote=%+v err=%v", demoted, err)
		}
		if got := f.roleChangeCount(house); got != 2 {
			t.Fatalf("uppercase role change audit rows=%d want 2", got)
		}
	})

	t.Run("assignment validation and unknown targets are uniform", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("assign_validation_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("assign validation house")
		f.join(f.newUserWithLogin("assign_validation_house_admin"), house, household.RoleAdmin)
		target := f.newUserWithLogin("assign_validation_target")
		missing := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff"

		if _, _, err := f.center.AssignAccount(ctx, actor, house, missing, household.RoleMember); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown account err=%v want not_found", err)
		}
		if _, _, err := f.center.AssignAccount(ctx, actor, missing, target, household.RoleMember); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown household err=%v want not_found", err)
		}
		if _, _, err := f.center.AssignAccount(ctx, actor, house, target, "owner"); !isValidationError(err) {
			t.Fatalf("invalid role err=%v want validation", err)
		}
		if _, _, ok := f.membership(house, target); ok {
			t.Fatal("failed assignment wrote a membership")
		}
		if got := f.totalAuditRows(); got != 0 {
			t.Fatalf("failed assignment wrote %d audit rows", got)
		}
	})

	t.Run("role changes use the shared core and record the instance-admin authority", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("role_change_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("role change house")
		houseAdmin := f.newUserWithLogin("role_change_house_admin")
		secondAdmin := f.newUserWithLogin("role_change_second_admin")
		member := f.newUserWithLogin("role_change_member")
		f.join(houseAdmin, house, household.RoleAdmin)
		f.join(secondAdmin, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)

		promoted, err := f.center.ChangeRole(ctx, actor, house, member, household.RoleAdmin, 1)
		if err != nil || promoted.Role != household.RoleAdmin || promoted.Version != 2 {
			t.Fatalf("promote=%+v err=%v", promoted, err)
		}
		f.requireMembership(house, member, household.RoleAdmin, 2)
		if got := f.roleChangeAuthorities(house); len(got) != 1 || got[0] != household.AuthorityInstanceAdmin {
			t.Fatalf("role change authorities=%v", got)
		}
		change := f.scalarString(`SELECT actor_user_id::text FROM household_role_changes WHERE household_id=$1::uuid`, house)
		if change != actor {
			t.Fatalf("role change actor=%s want %s", change, actor)
		}

		unchanged, err := f.center.ChangeRole(ctx, actor, house, member, household.RoleAdmin, 2)
		if err != nil || unchanged.Version != 2 || unchanged.Role != household.RoleAdmin {
			t.Fatalf("no-op change=%+v err=%v", unchanged, err)
		}
		f.requireMembership(house, member, household.RoleAdmin, 2)
		if got := f.roleChangeCount(house); got != 1 {
			t.Fatalf("no-op change wrote audit rows, total=%d", got)
		}

		if _, err := f.center.ChangeRole(ctx, actor, house, secondAdmin, household.RoleMember, 9); !errors.Is(err, household.ErrPreconditionFailed) {
			t.Fatalf("stale change err=%v want precondition_failed", err)
		}
		f.requireMembership(house, secondAdmin, household.RoleAdmin, 1)
		if got := f.roleChangeCount(house); got != 1 {
			t.Fatalf("stale change wrote audit rows, total=%d", got)
		}

		demoted, err := f.center.ChangeRole(ctx, actor, house, secondAdmin, household.RoleMember, 1)
		if err != nil || demoted.Role != household.RoleMember || demoted.Version != 2 {
			t.Fatalf("demote=%+v err=%v", demoted, err)
		}
		if got := f.roleChangeAuthorities(house); len(got) != 2 || got[1] != household.AuthorityInstanceAdmin {
			t.Fatalf("role change authorities=%v", got)
		}

		last := f.newHousehold("role change last admin house")
		lastAdmin := f.newUserWithLogin("role_change_last_admin")
		f.join(lastAdmin, last, household.RoleAdmin)
		if _, err := f.center.ChangeRole(ctx, actor, last, lastAdmin, household.RoleMember, 1); !errors.Is(err, household.ErrLastAdmin) {
			t.Fatalf("demote last admin err=%v want last_admin", err)
		}
		f.requireMembership(last, lastAdmin, household.RoleAdmin, 1)
		if got := f.roleChangeCount(last); got != 0 {
			t.Fatalf("last-admin refusal wrote %d audit rows", got)
		}
	})

	t.Run("removal uses the shared core without deleting the account or its sessions", func(t *testing.T) {
		f.prepare()
		actor := f.newUserWithLogin("remove_admin")
		f.grantInstanceAdmin(actor)
		house := f.newHousehold("remove house")
		other := f.newHousehold("remove other")
		houseAdmin := f.newUserWithLogin("remove_house_admin")
		member := f.newUserWithLogin("remove_member")
		f.join(houseAdmin, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)
		f.join(member, other, household.RoleMember)
		f.setDefault(member, house)
		f.addSession(member)
		person := f.addSubject(house, "remove member person", member)

		if err := f.center.RemoveAccount(ctx, actor, house, member, 1); err != nil {
			t.Fatalf("RemoveAccount: %v", err)
		}
		if _, _, ok := f.membership(house, member); ok {
			t.Fatal("membership survived the instance-admin removal")
		}
		if !f.accountExists(member) {
			t.Fatal("the account row was deleted")
		}
		if got := f.sessionCount(member); got != 1 {
			t.Fatalf("sessions=%d want 1 (removal never revokes sessions)", got)
		}
		if got := f.defaultHousehold(member); got == nil || *got != other {
			t.Fatalf("default=%v want %s", got, other)
		}
		if linked, _, _ := f.subject(person); linked != "" {
			t.Fatalf("person still linked: %q", linked)
		}
		if got := f.removalAuthorities(house); len(got) != 1 || got[0] != household.AuthorityInstanceAdmin {
			t.Fatalf("removal authorities=%v", got)
		}
		if got := f.scalarString(`SELECT actor_user_id::text FROM household_member_removals WHERE household_id=$1::uuid`, house); got != actor {
			t.Fatalf("removal actor=%s want %s", got, actor)
		}

		if err := f.center.RemoveAccount(ctx, actor, house, houseAdmin, 1); !errors.Is(err, household.ErrLastAdmin) {
			t.Fatalf("remove last admin err=%v want last_admin", err)
		}
		f.requireMembership(house, houseAdmin, household.RoleAdmin, 1)
		if got := f.removalAuthorities(house); len(got) != 1 {
			t.Fatalf("last-admin refusal wrote audit rows: %v", got)
		}
		if err := f.center.RemoveAccount(ctx, actor, house, houseAdmin, 9); !errors.Is(err, household.ErrPreconditionFailed) {
			t.Fatalf("stale removal err=%v want precondition_failed", err)
		}
		if err := f.center.RemoveAccount(ctx, actor, house, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff", 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown target err=%v want not_found", err)
		}
	})

	t.Run("household-admin and self-leave paths keep their own authority", func(t *testing.T) {
		f.prepare()
		house := f.newHousehold("authority house")
		houseAdmin := f.newUserWithLogin("authority_admin")
		promoted := f.newUserWithLogin("authority_promoted")
		leaver := f.newUserWithLogin("authority_leaver")
		f.join(houseAdmin, house, household.RoleAdmin)
		f.join(promoted, house, household.RoleMember)
		f.join(leaver, house, household.RoleMember)

		if _, err := f.accounts.ChangeRole(ctx, houseAdmin, house, promoted, household.RoleAdmin, 1); err != nil {
			t.Fatalf("household-admin role change: %v", err)
		}
		if err := f.service.RemoveMember(ctx, leaver, house, leaver, 1); err != nil {
			t.Fatalf("self-leave: %v", err)
		}
		if err := f.service.RemoveMember(ctx, houseAdmin, house, promoted, 2); err != nil {
			t.Fatalf("household-admin removal: %v", err)
		}
		if got := f.roleChangeAuthorities(house); len(got) != 1 || got[0] != household.AuthorityHouseholdAdmin {
			t.Fatalf("role change authorities=%v", got)
		}
		if got := f.removalAuthorities(house); len(got) != 2 ||
			got[0] != household.AuthorityHouseholdMember || got[1] != household.AuthorityHouseholdAdmin {
			t.Fatalf("removal authorities=%v", got)
		}
	})
}

// TestInstanceAdminCenterRacesAgainstPostgres exercises the serialization
// guarantees of ADR 0020: the installation grant check, the household lock that
// replaces the membership gate, the assignment account lock and the shared role
// change and removal cores. Every round asserts the invariant it protects and a
// 40P01 deadlock is never accepted.
func TestInstanceAdminCenterRacesAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL instance-admin center tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newAdminCenterFixture(t, ctx, admin, app)
	f.prepare()
	defer f.reset()

	t.Run("instance-admin and household-admin demotions keep one admin", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("demote_race_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("demote race house %d", round))
			a := f.newUserWithLogin(fmt.Sprintf("demote_race_a_%d", round))
			b := f.newUserWithLogin(fmt.Sprintf("demote_race_b_%d", round))
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)

			results := race(
				func() error {
					_, err := f.center.ChangeRole(ctx, actor, house, a, household.RoleMember, 1)
					return err
				},
				func() error {
					_, err := f.accounts.ChangeRole(ctx, b, house, b, household.RoleMember, 1)
					return err
				},
			)
			successes, refused := 0, 0
			for i, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrLastAdmin):
					refused++
				default:
					expectRaceError(t, round, fmt.Sprintf("demotion %d", i), err)
				}
			}
			if successes != 1 || refused != 1 {
				t.Fatalf("round %d successes=%d last-admin refusals=%d results=%v", round, successes, refused, results)
			}
			if admins := f.adminCount(house); admins < 1 {
				t.Fatalf("round %d household lost every admin", round)
			}
			if audits := f.roleChangeCount(house); audits != 1 {
				t.Fatalf("round %d role-change audit rows=%d want only the committed change", round, audits)
			}
		}
	})

	t.Run("instance-admin removal races a household admin self-leave", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("remove_leave_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("remove leave house %d", round))
			a := f.newUserWithLogin(fmt.Sprintf("remove_leave_a_%d", round))
			b := f.newUserWithLogin(fmt.Sprintf("remove_leave_b_%d", round))
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)

			results := race(
				func() error { return f.center.RemoveAccount(ctx, actor, house, a, 1) },
				func() error { return f.service.RemoveMember(ctx, a, house, a, 1) },
			)
			successes, notFound := 0, 0
			for i, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrNotFound):
					notFound++
				default:
					expectRaceError(t, round, fmt.Sprintf("removal %d", i), err)
				}
			}
			if successes != 1 || notFound != 1 {
				t.Fatalf("round %d successes=%d not-found=%d results=%v", round, successes, notFound, results)
			}
			if admins := f.adminCount(house); admins < 1 {
				t.Fatalf("round %d household lost every admin", round)
			}
			if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, house); got != 1 {
				t.Fatalf("round %d removal audit rows=%d want 1", round, got)
			}
		}
	})

	t.Run("instance-admin removal races a household admin removing the other admin", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("mutual_remove_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("mutual remove house %d", round))
			a := f.newUserWithLogin(fmt.Sprintf("mutual_remove_a_%d", round))
			b := f.newUserWithLogin(fmt.Sprintf("mutual_remove_b_%d", round))
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)

			results := race(
				func() error { return f.center.RemoveAccount(ctx, actor, house, a, 1) },
				func() error { return f.service.RemoveMember(ctx, a, house, b, 1) },
			)
			successes := 0
			for i, err := range results {
				if err == nil {
					successes++
					continue
				}
				expectRaceError(t, round, fmt.Sprintf("removal %d", i), err, household.ErrNotFound, household.ErrLastAdmin)
			}
			if successes != 1 {
				t.Fatalf("round %d successes=%d want exactly one results=%v", round, successes, results)
			}
			if admins := f.adminCount(house); admins < 1 {
				t.Fatalf("round %d household lost every admin", round)
			}
			if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, house); got != 1 {
				t.Fatalf("round %d removal audit rows=%d want 1", round, got)
			}
		}
	})

	t.Run("assignment races invitation acceptance of the same account", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("assign_accept_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("assign accept house %d", round))
			creator := f.newUserWithLogin(fmt.Sprintf("assign_accept_creator_%d", round))
			f.join(creator, house, household.RoleAdmin)
			target := f.newUserWithLogin(fmt.Sprintf("assign_accept_target_%d", round))
			invitation, err := f.invitations.CreateInvitation(ctx, creator, house, fmt.Sprintf("assign-accept-%d", round))
			if err != nil {
				t.Fatalf("round %d create invitation: %v", round, err)
			}

			var created bool
			var accepted bool
			results := race(
				func() error {
					var err error
					_, created, err = f.center.AssignAccount(ctx, actor, house, target, household.RoleMember)
					return err
				},
				func() error {
					_, err := f.invitations.AcceptInvitationExistingAccount(ctx, target, invitation.Token)
					accepted = err == nil
					return err
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "assignment", results[0], household.ErrAlreadyMember)
			}
			if results[1] != nil {
				expectRaceError(t, round, "invitation acceptance", results[1], identityapp.ErrAlreadyMember)
			}
			if got := f.userMembershipCount(house, target); got != 1 {
				t.Fatalf("round %d memberships=%d want exactly one", round, got)
			}
			f.requireMembership(house, target, household.RoleMember, 1)
			createdPaths := 0
			if created {
				createdPaths++
			}
			if accepted {
				createdPaths++
			}
			if createdPaths != 1 {
				t.Fatalf("round %d creating paths=%d want 1 (assignment err=%v accepted=%t)",
					round, createdPaths, results[0], accepted)
			}
			wantAudits := 0
			if created {
				wantAudits = 1
			}
			if got := f.assignmentCount(house); got != wantAudits {
				t.Fatalf("round %d assignment audit rows=%d want %d", round, got, wantAudits)
			}
			if got := f.defaultHousehold(target); got == nil || *got != house {
				t.Fatalf("round %d default household=%v want %s", round, got, house)
			}
		}
	})

	t.Run("assignment races removal of the same account in the same household", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("assign_remove_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("assign remove house %d", round))
			houseAdmin := f.newUserWithLogin(fmt.Sprintf("assign_remove_admin_%d", round))
			f.join(houseAdmin, house, household.RoleAdmin)
			target := f.newUserWithLogin(fmt.Sprintf("assign_remove_target_%d", round))
			f.join(target, house, household.RoleMember)
			f.setDefault(target, house)

			var created bool
			results := race(
				func() error {
					var err error
					_, created, err = f.center.AssignAccount(ctx, actor, house, target, household.RoleAdmin)
					return err
				},
				func() error { return f.service.RemoveMember(ctx, target, house, target, 1) },
			)
			if results[0] != nil {
				expectRaceError(t, round, "assignment", results[0], household.ErrAlreadyMember)
			}
			if results[1] != nil {
				expectRaceError(t, round, "self-leave", results[1], household.ErrNotFound)
			}
			if got := f.userMembershipCount(house, target); got > 1 {
				t.Fatalf("round %d duplicate memberships=%d", round, got)
			}
			role, version, present := f.membership(house, target)
			assignmentRows := f.assignmentCount(house)
			defaultHousehold := f.defaultHousehold(target)
			if present {
				if role != household.RoleAdmin || version < 1 {
					t.Fatalf("round %d surviving membership role=%q version=%d", round, role, version)
				}
				if assignmentRows != 1 {
					t.Fatalf("round %d membership survived but assignment audit rows=%d", round, assignmentRows)
				}
				if !created {
					t.Fatalf("round %d membership survived without a committed assignment (created=%t)", round, created)
				}
				if defaultHousehold == nil || *defaultHousehold != house {
					t.Fatalf("round %d default household=%v want %s", round, defaultHousehold, house)
				}
				continue
			}
			if assignmentRows != 0 {
				t.Fatalf("round %d removed member kept %d assignment audit rows", round, assignmentRows)
			}
			if defaultHousehold != nil {
				t.Fatalf("round %d default household=%q want it cleared", round, *defaultHousehold)
			}
		}
	})

	t.Run("identical concurrent assignments create one membership and one audit row", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("assign_duplicate_actor_%d", round))
			f.grantInstanceAdmin(actor)
			house := f.newHousehold(fmt.Sprintf("assign duplicate house %d", round))
			f.join(f.newUserWithLogin(fmt.Sprintf("assign_duplicate_admin_%d", round)), house, household.RoleAdmin)
			target := f.newUserWithLogin(fmt.Sprintf("assign_duplicate_target_%d", round))

			var createdA, createdB bool
			results := race(
				func() error {
					var err error
					_, createdA, err = f.center.AssignAccount(ctx, actor, house, target, household.RoleMember)
					return err
				},
				func() error {
					var err error
					_, createdB, err = f.center.AssignAccount(ctx, actor, house, target, household.RoleMember)
					return err
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "first assignment", results[0], household.ErrAlreadyMember)
			}
			if results[1] != nil {
				expectRaceError(t, round, "second assignment", results[1], household.ErrAlreadyMember)
			}
			createdCount := 0
			if createdA {
				createdCount++
			}
			if createdB {
				createdCount++
			}
			if createdCount != 1 {
				t.Fatalf("round %d created=%d want exactly one (a=%t b=%t)", round, createdCount, createdA, createdB)
			}
			if got := f.userMembershipCount(house, target); got != 1 {
				t.Fatalf("round %d memberships=%d want 1", round, got)
			}
			f.requireMembership(house, target, household.RoleMember, 1)
			if got := f.assignmentCount(house); got != 1 {
				t.Fatalf("round %d assignment audit rows=%d want 1", round, got)
			}
			if got := f.personLinks(house, target); got != 1 {
				t.Fatalf("round %d person links=%d want 1", round, got)
			}
		}
	})

	t.Run("assignment in one household races removal in another", func(t *testing.T) {
		f.prepare()
		for round := 0; round < 10; round++ {
			actor := f.newUserWithLogin(fmt.Sprintf("assign_cross_actor_%d", round))
			f.grantInstanceAdmin(actor)
			first := f.newHousehold(fmt.Sprintf("assign cross first %d", round))
			second := f.newHousehold(fmt.Sprintf("assign cross second %d", round))
			f.join(f.newUserWithLogin(fmt.Sprintf("assign_cross_admin_%d", round)), first, household.RoleAdmin)
			secondAdmin := f.newUserWithLogin(fmt.Sprintf("assign_cross_second_admin_%d", round))
			f.join(secondAdmin, second, household.RoleAdmin)
			target := f.newUserWithLogin(fmt.Sprintf("assign_cross_target_%d", round))
			f.join(target, second, household.RoleMember)
			f.setDefault(target, second)

			results := race(
				func() error {
					_, _, err := f.center.AssignAccount(ctx, actor, first, target, household.RoleMember)
					return err
				},
				func() error { return f.service.RemoveMember(ctx, secondAdmin, second, target, 1) },
			)
			for i, err := range results {
				if err != nil {
					expectRaceError(t, round, fmt.Sprintf("operation %d", i), err)
				}
			}
			f.requireMembership(first, target, household.RoleMember, 1)
			if _, _, ok := f.membership(second, target); ok {
				t.Fatalf("round %d membership in the removed household survived", round)
			}
			got := f.defaultHousehold(target)
			if got == nil || *got != first {
				t.Fatalf("round %d default household=%v want %s", round, got, first)
			}
			if got := f.assignmentCount(first); got != 1 {
				t.Fatalf("round %d assignment audit rows=%d want 1", round, got)
			}
			if got := f.scalarInt(`SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, second); got != 1 {
				t.Fatalf("round %d removal audit rows=%d want 1", round, got)
			}
		}
	})
}

// isValidationError reports whether err is the household package's validation
// error, the shape every malformed cursor and search term must produce.
func isValidationError(err error) bool {
	var validation *household.ValidationError
	return errors.As(err, &validation)
}

// secondError and thirdError discard the leading result values of the multi-value
// service methods so the gate table stays readable.
func secondError[T any](_ T, err error) error { return err }

func thirdError[T any, U any](_ T, _ U, err error) error { return err }
