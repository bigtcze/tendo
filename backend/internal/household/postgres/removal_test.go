package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	itempostgres "github.com/bigtcze/tendo/backend/internal/item/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// removalFixture wires the real membership removal service against the database
// exactly as cmd/tendo does, and offers the narrow fixture/assertion helpers the
// removal tests need.
type removalFixture struct {
	t           *testing.T
	ctx         context.Context
	admin, app  *pgxpool.Pool
	service     *household.MemberRemovalService
	invitations *identityapp.InvitationService
	resets      *identityapp.PasswordResetService
	sessions    *identityapp.SessionService
	changes     *identityapp.PasswordChangeService
	subjects    *subjectapp.Service
	memberships *household.MembershipService
	seq         int
}

func newRemovalFixture(t *testing.T, ctx context.Context, admin, app *pgxpool.Pool) *removalFixture {
	t.Helper()
	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	f := &removalFixture{t: t, ctx: ctx, admin: admin, app: app}
	f.service = household.NewMemberRemovalService(NewMemberRemovalRepository(app, MemberRemovalEffectFactories{
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
	}), clock)
	invitations, err := identityapp.NewInvitationService(identitypostgres.NewInvitationRepository(app, func(tx householddb.DBTX) identitypostgres.InvitationHouseholdService {
		return MembershipServiceFor(tx)
	}, testMemberPeople), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.invitations = invitations
	resets, err := identityapp.NewPasswordResetService(identitypostgres.NewPasswordResetRepository(app, func(tx householddb.DBTX) identitypostgres.PasswordResetHouseholdService {
		return MembershipServiceFor(tx)
	}), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.resets = resets
	sessions, err := identityapp.NewSessionService(identitypostgres.New(app, nil), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.sessions = sessions
	changes, err := identityapp.NewPasswordChangeService(identitypostgres.NewPasswordChangeRepository(app), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.changes = changes
	f.memberships = MembershipServiceFor(app)
	f.subjects = subjectapp.NewService(subjectpostgres.NewRepository(app), f.subjectAuthorizer, f.subjectAuthorizer)
	return f
}

// subjectAuthorizer adapts the real membership lookup to the subject module's
// authorization port, exactly as cmd/tendo wires it.
func (f *removalFixture) subjectAuthorizer(ctx context.Context, userID, householdID string) error {
	_, err := f.memberships.GetMembership(ctx, userID, householdID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, household.ErrNotFound):
		return subjectapp.ErrNotFound
	default:
		return subjectapp.ErrUnavailable
	}
}

func (f *removalFixture) reset() {
	f.t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := f.admin.Exec(c, `TRUNCATE user_accounts, households, household_member_removals CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
		f.t.Fatalf("reset fixtures: %v", err)
	}
}

func (f *removalFixture) login() string {
	f.seq++
	return fmt.Sprintf("removal_user_%d", f.seq)
}

func (f *removalFixture) newUser() string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, f.login()).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) newHousehold(name string) string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) join(userID, householdID, role string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, $3)`, userID, householdID, role); err != nil {
		f.t.Fatal(err)
	}
}

func (f *removalFixture) password(userID string) {
	f.t.Helper()
	hash, err := security.HashPassword("correct horse battery")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO local_credentials(user_id, password_hash) VALUES ($1::uuid, $2)`, userID, hash); err != nil {
		f.t.Fatal(err)
	}
}

func (f *removalFixture) setDefault(userID, householdID string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, userID, householdID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *removalFixture) addInvitation(householdID, createdBy, creationKey string) string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO household_invitations(household_id, created_by_user_id, creation_key, token_hash, created_at, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, decode(md5(random()::text)||md5(random()::text), 'hex'), '2026-01-01T00:00:00Z', '2026-01-08T00:00:00Z')
		RETURNING id::text`, householdID, createdBy, creationKey).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) acceptInvitation(invitationID string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `UPDATE household_invitations SET accepted_at=now() WHERE id=$1::uuid`, invitationID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *removalFixture) addReset(householdID, targetUserID, createdBy string) string {
	f.t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO password_reset_tokens(household_id, target_user_id, created_by_user_id, token_hash, created_at, expires_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, decode(md5(random()::text)||md5(random()::text), 'hex'), '2026-01-01T00:00:00Z', '2026-01-01T01:00:00Z')
		RETURNING id::text`, householdID, targetUserID, createdBy).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) addSubject(householdID, name string, linkedUserID string) string {
	f.t.Helper()
	var linked *string
	if linkedUserID != "" {
		linked = &linkedUserID
	}
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO subjects(household_id, type, name, linked_user_id) VALUES ($1::uuid, 'person', $2, $3::uuid) RETURNING id::text`, householdID, name, linked).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) addItem(householdID, subjectID, title, responsibleUserID string, archived, done bool) string {
	f.t.Helper()
	var responsible *string
	if responsibleUserID != "" {
		responsible = &responsibleUserID
	}
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO items(household_id, subject_id, title, responsible_user_id, archived, done)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6) RETURNING id::text`, householdID, subjectID, title, responsible, archived, done).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *removalFixture) addSession(userID string) {
	f.t.Helper()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at, sign_in_method)
		VALUES ($1::uuid, decode(md5(random()::text)||md5(random()::text), 'hex'), now(), now()+interval '1 day', 'password')`, userID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *removalFixture) membership(householdID, userID string) (string, int64, bool) {
	f.t.Helper()
	var role string
	var version int64
	err := f.admin.QueryRow(f.ctx, `SELECT role, version FROM household_memberships WHERE household_id=$1::uuid AND user_id=$2::uuid`, householdID, userID).Scan(&role, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return role, version, true
}

func (f *removalFixture) defaultHousehold(userID string) *string {
	f.t.Helper()
	var householdID *string
	if err := f.admin.QueryRow(f.ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, userID).Scan(&householdID); err != nil {
		f.t.Fatal(err)
	}
	return householdID
}

func (f *removalFixture) item(householdID, itemID string) (string, int64, bool, bool) {
	f.t.Helper()
	var responsible *string
	var version int64
	var archived, done bool
	if err := f.admin.QueryRow(f.ctx, `SELECT responsible_user_id::text, version, archived, done FROM items WHERE household_id=$1::uuid AND id=$2::uuid`, householdID, itemID).Scan(&responsible, &version, &archived, &done); err != nil {
		f.t.Fatal(err)
	}
	if responsible == nil {
		return "", version, archived, done
	}
	return *responsible, version, archived, done
}

func (f *removalFixture) subject(subjectID string) (string, int64, bool) {
	f.t.Helper()
	var linked *string
	var version int64
	var archived bool
	if err := f.admin.QueryRow(f.ctx, `SELECT linked_user_id::text, version, archived FROM subjects WHERE id=$1::uuid`, subjectID).Scan(&linked, &version, &archived); err != nil {
		f.t.Fatal(err)
	}
	if linked == nil {
		return "", version, archived
	}
	return *linked, version, archived
}

func (f *removalFixture) invitationRevoked(invitationID string) bool {
	f.t.Helper()
	var revoked bool
	if err := f.admin.QueryRow(f.ctx, `SELECT revoked_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, invitationID).Scan(&revoked); err != nil {
		f.t.Fatal(err)
	}
	return revoked
}

func (f *removalFixture) resetRevoked(resetID string) bool {
	f.t.Helper()
	var revoked bool
	if err := f.admin.QueryRow(f.ctx, `SELECT revoked_at IS NOT NULL FROM password_reset_tokens WHERE id=$1::uuid`, resetID).Scan(&revoked); err != nil {
		f.t.Fatal(err)
	}
	return revoked
}

func (f *removalFixture) auditCount(householdID string) int {
	f.t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, householdID).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func (f *removalFixture) sessionCount(userID string) int {
	f.t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, userID).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func (f *removalFixture) adminCount(householdID string) int {
	f.t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid AND role='admin'`, householdID).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func (f *removalFixture) loginOf(userID string) string {
	f.t.Helper()
	var login string
	if err := f.admin.QueryRow(f.ctx, `SELECT login FROM user_accounts WHERE id=$1::uuid`, userID).Scan(&login); err != nil {
		f.t.Fatal(err)
	}
	return login
}

// resetLinkState reports how many reset links of one target exist in one
// household and whether all of them are revoked.
func (f *removalFixture) resetLinkState(householdID, targetUserID string) (int, bool) {
	f.t.Helper()
	var count int
	var revoked bool
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*), coalesce(bool_and(revoked_at IS NOT NULL), false)
		FROM password_reset_tokens WHERE household_id=$1::uuid AND target_user_id=$2::uuid`, householdID, targetUserID).Scan(&count, &revoked); err != nil {
		f.t.Fatal(err)
	}
	return count, revoked
}

// danglingSubjectLinks counts subjects of the household whose linked account is
// no longer a member, exactly what the removal's unlink must prevent.
func (f *removalFixture) danglingSubjectLinks(householdID string) int {
	f.t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM subjects s
		WHERE s.household_id=$1::uuid AND s.linked_user_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM household_memberships m
		    WHERE m.household_id=s.household_id AND m.user_id=s.linked_user_id)`, householdID).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

// expectRaceError fails with the PostgreSQL SQLSTATE when a race produced an
// error the subtest does not allow, so a 40P01 deadlock is never swallowed.
func expectRaceError(t *testing.T, round int, label string, err error, allowed ...error) {
	t.Helper()
	for _, candidate := range allowed {
		if errors.Is(err, candidate) {
			return
		}
	}
	t.Fatalf("round %d %s: unexpected error (SQLSTATE %q): %v", round, label, pgCode(err), err)
}

// race starts every call at once and returns their results in input order.
func race(calls ...func() error) []error {
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	results := make([]error, len(calls))
	ready.Add(len(calls))
	done.Add(len(calls))
	for i := range calls {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			results[i] = calls[i]()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestMemberRemovalAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL removal tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newRemovalFixture(t, ctx, admin, app)
	f.reset()
	defer f.reset()

	t.Run("admin removal persists every membership effect", func(t *testing.T) {
		f.reset()
		house, other := f.newHousehold("removal effects house"), f.newHousehold("removal effects other")
		admin1, admin2, member := f.newUser(), f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(admin2, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)
		f.join(member, other, household.RoleMember)
		f.setDefault(member, house)
		f.addSession(member)

		person := f.addSubject(house, "member person", member)
		openItem := f.addItem(house, person, "open item", member, false, false)
		archivedItem := f.addItem(house, person, "archived item", member, true, false)
		doneItem := f.addItem(house, person, "done item", member, false, true)
		otherItem := f.addItem(house, person, "other responsibility", admin1, false, false)
		unassignedItem := f.addItem(house, person, "unassigned", "", false, false)

		// The target created one pending invitation that must be revoked, one
		// already accepted invitation that must be left alone, and one pending
		// invitation created by another member that must also stay usable.
		pendingByM := f.addInvitation(house, member, "member-pending")
		acceptedByM := f.addInvitation(house, member, "member-accepted")
		f.acceptInvitation(acceptedByM)
		pendingByAdmin := f.addInvitation(house, admin1, "admin-pending")
		// A reset link whose target is the removed member, issued in this house.
		resetForM := f.addReset(house, member, admin1)

		if err := f.service.RemoveMember(ctx, admin1, house, member, 1); err != nil {
			t.Fatalf("remove member: %v", err)
		}

		if _, _, ok := f.membership(house, member); ok {
			t.Fatal("membership survived removal")
		}
		if role, version, ok := f.membership(other, member); !ok || role != household.RoleMember || version != 1 {
			t.Fatalf("unrelated membership changed: role=%s version=%d ok=%t", role, version, ok)
		}
		for name, itemID := range map[string]string{"open": openItem, "archived": archivedItem, "done": doneItem} {
			responsible, version, archived, done := f.item(house, itemID)
			if responsible != "" || version != 2 {
				t.Fatalf("%s item responsible=%q version=%d", name, responsible, version)
			}
			if name == "archived" && !archived {
				t.Fatalf("archived flag cleared on %s item", name)
			}
			if name == "done" && !done {
				t.Fatalf("done flag cleared on %s item", name)
			}
		}
		if responsible, version, _, _ := f.item(house, otherItem); responsible != admin1 || version != 1 {
			t.Fatalf("unrelated item responsibility=%q version=%d", responsible, version)
		}
		if responsible, version, _, _ := f.item(house, unassignedItem); responsible != "" || version != 1 {
			t.Fatalf("unassigned item responsibility=%q version=%d", responsible, version)
		}
		if linked, version, archived := f.subject(person); linked != "" || version != 2 || archived {
			t.Fatalf("person subject linked=%q version=%d archived=%t", linked, version, archived)
		}
		if !f.invitationRevoked(pendingByM) {
			t.Fatal("target's pending invitation was not revoked")
		}
		if f.invitationRevoked(acceptedByM) {
			t.Fatal("accepted invitation was revoked")
		}
		if f.invitationRevoked(pendingByAdmin) {
			t.Fatal("another member's pending invitation was revoked")
		}
		if !f.resetRevoked(resetForM) {
			t.Fatal("target reset link in the household was not revoked")
		}
		if got := f.defaultHousehold(member); got == nil || *got != other {
			t.Fatalf("default household=%v want %s", got, other)
		}
		if got := f.sessionCount(member); got != 1 {
			t.Fatalf("sessions=%d want 1 (removal never revokes sessions)", got)
		}
		if got := f.auditCount(house); got != 1 {
			t.Fatalf("audit rows=%d want 1", got)
		}
		var targetID, actorID, previousRole string
		if err := admin.QueryRow(ctx, `SELECT target_user_id::text, actor_user_id::text, previous_role FROM household_member_removals WHERE household_id=$1::uuid`, house).Scan(&targetID, &actorID, &previousRole); err != nil {
			t.Fatal(err)
		}
		if targetID != member || actorID != admin1 || previousRole != household.RoleMember {
			t.Fatalf("audit target=%s actor=%s role=%s", targetID, actorID, previousRole)
		}
		if got := f.adminCount(house); got != 2 {
			t.Fatalf("admin count=%d want 2", got)
		}
	})

	t.Run("removing an admin revokes issuer-side resets inside this household only", func(t *testing.T) {
		f.reset()
		house, other := f.newHousehold("removal admin house"), f.newHousehold("removal admin other")
		admin1, admin2, target := f.newUser(), f.newUser(), f.newUser()
		resetTarget := f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(admin2, house, household.RoleAdmin)
		f.join(target, house, household.RoleAdmin)
		f.join(target, other, household.RoleAdmin)
		f.join(resetTarget, other, household.RoleMember)
		person := f.addSubject(house, "target admin person", target)

		// Target-side link in this house, issuer-side link in this house, and an
		// issuer-side link the same account holds through another household.
		resetAsTarget := f.addReset(house, target, admin1)
		resetAsIssuer := f.addReset(house, admin2, target)
		resetOtherHouse := f.addReset(other, resetTarget, target)

		if err := f.service.RemoveMember(ctx, admin1, house, target, 1); err != nil {
			t.Fatalf("remove admin: %v", err)
		}
		if !f.resetRevoked(resetAsTarget) {
			t.Fatal("target-side reset in the household was not revoked")
		}
		if !f.resetRevoked(resetAsIssuer) {
			t.Fatal("issuer-side reset in the household was not revoked")
		}
		if f.resetRevoked(resetOtherHouse) {
			t.Fatal("reset the member issued through another household was revoked")
		}
		if role, _, ok := f.membership(other, target); !ok || role != household.RoleAdmin {
			t.Fatalf("target's other-household membership changed: role=%s ok=%t", role, ok)
		}
		if linked, _, archived := f.subject(person); linked != "" || archived {
			t.Fatalf("admin person linked=%q archived=%t", linked, archived)
		}
		if got := f.auditCount(house); got != 1 {
			t.Fatalf("audit rows=%d want 1", got)
		}
	})

	t.Run("last admin cannot leave", func(t *testing.T) {
		f.reset()
		house := f.newHousehold("last admin removal house")
		admin1 := f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		person := f.addSubject(house, "last admin person", admin1)
		if err := f.service.RemoveMember(ctx, admin1, house, admin1, 1); !errors.Is(err, household.ErrLastAdmin) {
			t.Fatalf("err=%v want last_admin", err)
		}
		if role, version, ok := f.membership(house, admin1); !ok || role != household.RoleAdmin || version != 1 {
			t.Fatalf("last admin membership changed: role=%s version=%d ok=%t", role, version, ok)
		}
		if linked, _, _ := f.subject(person); linked != admin1 {
			t.Fatalf("last admin person unlinked: %q", linked)
		}
		if got := f.auditCount(house); got != 0 {
			t.Fatalf("last-admin refusal wrote %d audit rows", got)
		}
	})

	t.Run("plain member may leave and the default moves", func(t *testing.T) {
		f.reset()
		house, other := f.newHousehold("leave house"), f.newHousehold("leave other")
		admin1, leaver := f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(leaver, house, household.RoleMember)
		f.join(leaver, other, household.RoleMember)
		f.setDefault(leaver, house)
		person := f.addSubject(house, "leaver person", leaver)
		item := f.addItem(house, person, "leaver item", leaver, false, false)

		if err := f.service.RemoveMember(ctx, leaver, house, leaver, 1); err != nil {
			t.Fatalf("leave: %v", err)
		}
		if _, _, ok := f.membership(house, leaver); ok {
			t.Fatal("leaver membership survived")
		}
		if got := f.defaultHousehold(leaver); got == nil || *got != other {
			t.Fatalf("default household=%v want %s", got, other)
		}
		if responsible, _, _, _ := f.item(house, item); responsible != "" {
			t.Fatalf("leaver item still responsible=%q", responsible)
		}
		if linked, _, _ := f.subject(person); linked != "" {
			t.Fatalf("leaver person still linked: %q", linked)
		}
	})

	t.Run("plain member cannot remove another member", func(t *testing.T) {
		f.reset()
		house := f.newHousehold("member forbid house")
		admin1, actor, target := f.newUser(), f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(actor, house, household.RoleMember)
		f.join(target, house, household.RoleMember)
		if err := f.service.RemoveMember(ctx, actor, house, target, 1); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("err=%v want admin_required", err)
		}
		if _, _, ok := f.membership(house, target); !ok {
			t.Fatal("forbidden removal removed the target")
		}
		if got := f.auditCount(house); got != 0 {
			t.Fatalf("forbidden removal wrote %d audit rows", got)
		}
	})

	t.Run("non-member, unknown household and non-member target are not found", func(t *testing.T) {
		f.reset()
		house := f.newHousehold("not found house")
		admin1, member, outsider := f.newUser(), f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)
		if err := f.service.RemoveMember(ctx, outsider, house, member, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("non-member actor err=%v", err)
		}
		if err := f.service.RemoveMember(ctx, admin1, house, outsider, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("non-member target err=%v", err)
		}
		if err := f.service.RemoveMember(ctx, admin1, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff", member, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown household err=%v", err)
		}
		if _, _, ok := f.membership(house, member); !ok {
			t.Fatal("not-found path removed the member")
		}
	})

	t.Run("stale version changes nothing", func(t *testing.T) {
		f.reset()
		house := f.newHousehold("stale removal house")
		admin1, member := f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(member, house, household.RoleMember)
		person := f.addSubject(house, "stale person", member)
		item := f.addItem(house, person, "stale item", member, false, false)
		if err := f.service.RemoveMember(ctx, admin1, house, member, 9); !errors.Is(err, household.ErrPreconditionFailed) {
			t.Fatalf("err=%v want precondition_failed", err)
		}
		if role, version, ok := f.membership(house, member); !ok || role != household.RoleMember || version != 1 {
			t.Fatalf("stale removal changed membership: role=%s version=%d ok=%t", role, version, ok)
		}
		if linked, _, _ := f.subject(person); linked != member {
			t.Fatalf("stale removal unlinked person: %q", linked)
		}
		if responsible, version, _, _ := f.item(house, item); responsible != member || version != 1 {
			t.Fatalf("stale removal changed item: responsible=%q version=%d", responsible, version)
		}
		if got := f.auditCount(house); got != 0 {
			t.Fatalf("stale removal wrote %d audit rows", got)
		}
	})

	t.Run("rejoin provisions a fresh person and old links stay unusable", func(t *testing.T) {
		f.reset()
		house := f.newHousehold("rejoin house")
		admin1, rejoinUser := f.newUser(), f.newUser()
		f.join(admin1, house, household.RoleAdmin)
		f.join(rejoinUser, house, household.RoleMember)
		f.password(rejoinUser)
		oldPerson := f.addSubject(house, "rejoin old person", rejoinUser)

		issued, err := f.resets.IssuePasswordReset(ctx, admin1, house, rejoinUser)
		if err != nil {
			t.Fatalf("issue reset: %v", err)
		}
		if err := f.service.RemoveMember(ctx, admin1, house, rejoinUser, 1); err != nil {
			t.Fatalf("remove for rejoin: %v", err)
		}
		var revokedResets int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE household_id=$1::uuid AND target_user_id=$2::uuid AND revoked_at IS NOT NULL`, house, rejoinUser).Scan(&revokedResets); err != nil {
			t.Fatal(err)
		}
		if revokedResets != 1 {
			t.Fatalf("revoked reset links=%d want 1", revokedResets)
		}

		invitation, err := f.invitations.CreateInvitation(ctx, admin1, house, "rejoin")
		if err != nil {
			t.Fatalf("create rejoin invitation: %v", err)
		}
		accepted, err := f.invitations.AcceptInvitationExistingAccount(ctx, rejoinUser, invitation.Token)
		if err != nil {
			t.Fatalf("accept rejoin invitation: %v", err)
		}
		if accepted.HouseholdID != house || accepted.Role != household.RoleMember {
			t.Fatalf("rejoin accepted=%+v", accepted)
		}
		if role, version, ok := f.membership(house, rejoinUser); !ok || role != household.RoleMember || version != 1 {
			t.Fatalf("rejoined membership role=%s version=%d ok=%t", role, version, ok)
		}
		if got := f.defaultHousehold(rejoinUser); got == nil || *got != house {
			t.Fatalf("rejoined default=%v want %s", got, house)
		}
		var people []struct {
			id     string
			linked *string
			arch   bool
		}
		rows, err := admin.Query(ctx, `SELECT id::text, linked_user_id::text, archived FROM subjects WHERE household_id=$1::uuid AND type='person' ORDER BY created_at, id`, house)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var p struct {
				id     string
				linked *string
				arch   bool
			}
			if err := rows.Scan(&p.id, &p.linked, &p.arch); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			people = append(people, p)
		}
		rows.Close()
		if len(people) != 2 {
			t.Fatalf("person subjects=%d want 2 (old unlinked + fresh)", len(people))
		}
		var old, fresh int
		for _, p := range people {
			switch {
			case p.id == oldPerson:
				if p.linked != nil || p.arch {
					t.Fatalf("old person linked=%v archived=%t", p.linked, p.arch)
				}
				old++
			case p.linked != nil && *p.linked == rejoinUser:
				fresh++
			}
		}
		if old != 1 || fresh != 1 {
			t.Fatalf("old=%d fresh=%d people=%+v", old, fresh, people)
		}
		if _, err := f.resets.RedeemPasswordReset(ctx, issued.Token, "correct horse battery staple"); !errors.Is(err, identityapp.ErrInvalidPasswordReset) {
			t.Fatalf("revoked reset link redeemed after rejoin: %v", err)
		}
	})
}

// TestMemberRemovalRacesAgainstPostgres exercises the serialization guarantees
// the removal transaction relies on: the shared household lock, the membership
// FOR UPDATE lock and the deferred default-household duty.
func TestMemberRemovalRacesAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL removal tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newRemovalFixture(t, ctx, admin, app)
	f.reset()
	defer f.reset()

	t.Run("two last admins leaving leave exactly one admin", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("last admin race %d", round))
			a, b := f.newUser(), f.newUser()
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)
			results := race(
				func() error { return f.service.RemoveMember(ctx, a, house, a, 1) },
				func() error { return f.service.RemoveMember(ctx, b, house, b, 1) },
			)
			successes, refused := 0, 0
			for _, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrLastAdmin):
					refused++
				default:
					t.Fatalf("round %d unexpected error: %v", round, err)
				}
			}
			if successes != 1 || refused != 1 {
				t.Fatalf("round %d successes=%d last-admin refusals=%d", round, successes, refused)
			}
			if admins := f.adminCount(house); admins != 1 {
				t.Fatalf("round %d admins=%d", round, admins)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d, want only the committed removal", round, audits)
			}
		}
	})

	t.Run("admins removing each other leave exactly one not-found", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("mutual removal race %d", round))
			a, b := f.newUser(), f.newUser()
			f.join(a, house, household.RoleAdmin)
			f.join(b, house, household.RoleAdmin)
			results := race(
				func() error { return f.service.RemoveMember(ctx, a, house, b, 1) },
				func() error { return f.service.RemoveMember(ctx, b, house, a, 1) },
			)
			successes, notFound := 0, 0
			for _, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, household.ErrNotFound):
					notFound++
				default:
					t.Fatalf("round %d unexpected error: %v", round, err)
				}
			}
			if successes != 1 || notFound != 1 {
				t.Fatalf("round %d successes=%d not-found=%d", round, successes, notFound)
			}
			var members int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid`, house).Scan(&members); err != nil {
				t.Fatal(err)
			}
			if members != 1 {
				t.Fatalf("round %d members=%d want 1", round, members)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d want 1", round, audits)
			}
		}
	})

	t.Run("concurrent item assignment never leaves responsibility on a removed member", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("item race %d", round))
			actor, worker := f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(worker, house, household.RoleMember)
			subject := f.addSubject(house, "race item subject", "")
			var insertErr error
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, worker, 1) },
				func() error {
					_, insertErr = app.Exec(ctx, `INSERT INTO items(household_id, subject_id, title, responsible_user_id) VALUES ($1::uuid, $2::uuid, 'race item', $3::uuid)`, house, subject, worker)
					return insertErr
				},
			)
			if results[0] != nil {
				t.Fatalf("round %d removal err=%v", round, results[0])
			}
			if insertErr != nil && pgCode(insertErr) != "23503" {
				t.Fatalf("round %d item insert err=%v", round, insertErr)
			}
			if _, _, ok := f.membership(house, worker); ok {
				t.Fatalf("round %d removed membership survived", round)
			}
			var dangling int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND responsible_user_id=$2::uuid`, house, worker).Scan(&dangling); err != nil {
				t.Fatal(err)
			}
			if dangling != 0 {
				t.Fatalf("round %d items still reference the removed member: %d", round, dangling)
			}
		}
	})

	t.Run("concurrent default selection keeps a valid default", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("default race house %d", round))
			other := f.newHousehold(fmt.Sprintf("default race other %d", round))
			actor, worker := f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(worker, house, household.RoleMember)
			f.join(worker, other, household.RoleMember)
			f.setDefault(worker, house)
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, worker, 1) },
				func() error {
					_, err := app.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, worker, other)
					return err
				},
			)
			if results[0] != nil {
				t.Fatalf("round %d removal err=%v", round, results[0])
			}
			if results[1] != nil {
				t.Fatalf("round %d default update err=%v", round, results[1])
			}
			got := f.defaultHousehold(worker)
			// The concurrent selection always keeps `other` (the only remaining
			// membership), so a cleared default is a regression: removal must not
			// overwrite it with NULL.
			if got == nil {
				t.Fatalf("round %d default household cleared although a membership remains", round)
			}
			if *got != other {
				t.Fatalf("round %d default=%s want %s", round, *got, other)
			}
		}
	})

	// The same person can be removed from two households at once by each
	// household's admin. Both removals must commit: each takes its own household
	// lock and then the shared account row FOR NO KEY UPDATE, which serializes
	// them without a deadlock. After both, no membership remains, the account
	// default is cleared, and each household holds exactly one audit row.
	t.Run("same target removed from two households concurrently", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			first := f.newHousehold(fmt.Sprintf("shared target house %d", round))
			second := f.newHousehold(fmt.Sprintf("shared target other %d", round))
			adminA, adminB, target := f.newUser(), f.newUser(), f.newUser()
			f.join(adminA, first, household.RoleAdmin)
			f.join(adminB, second, household.RoleAdmin)
			f.join(target, first, household.RoleMember)
			f.join(target, second, household.RoleMember)
			f.setDefault(target, first)

			results := race(
				func() error { return f.service.RemoveMember(ctx, adminA, first, target, 1) },
				func() error { return f.service.RemoveMember(ctx, adminB, second, target, 1) },
			)
			for i, err := range results {
				if err != nil {
					if code := pgCode(err); code == "40P01" {
						t.Fatalf("round %d removal %d deadlocked: %v", round, i, err)
					}
					t.Fatalf("round %d removal %d err=%v", round, i, err)
				}
			}
			if _, _, ok := f.membership(first, target); ok {
				t.Fatalf("round %d target still a member of the first household", round)
			}
			if _, _, ok := f.membership(second, target); ok {
				t.Fatalf("round %d target still a member of the second household", round)
			}
			if got := f.defaultHousehold(target); got != nil {
				t.Fatalf("round %d default household=%s want it cleared", round, *got)
			}
			if audits := f.auditCount(first); audits != 1 {
				t.Fatalf("round %d first household audit rows=%d want 1", round, audits)
			}
			if audits := f.auditCount(second); audits != 1 {
				t.Fatalf("round %d second household audit rows=%d want 1", round, audits)
			}
			var total int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_member_removals WHERE target_user_id=$1::uuid`, target).Scan(&total); err != nil {
				t.Fatal(err)
			}
			if total != 2 {
				t.Fatalf("round %d audit rows for the target=%d want 2", round, total)
			}
		}
	})

	// Acceptance locks the invitation row and then the account row, so the
	// removal revokes the target's invitations of this household first. The
	// acceptance here joins another household, which the removal must not touch:
	// both sides succeed, the target keeps only the new membership and the
	// account default still points at a real membership.
	t.Run("removal races invitation acceptance into another household", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("invitation race house %d", round))
			other := f.newHousehold(fmt.Sprintf("invitation race other %d", round))
			actor, target, host := f.newUser(), f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(target, house, household.RoleMember)
			f.join(host, other, household.RoleAdmin)
			f.setDefault(target, house)

			invitation, err := f.invitations.CreateInvitation(ctx, host, other, fmt.Sprintf("removal-race-%d", round))
			if err != nil {
				t.Fatalf("round %d create invitation: %v", round, err)
			}
			var acceptance error
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, target, 1) },
				func() error {
					_, acceptance = f.invitations.AcceptInvitationExistingAccount(ctx, target, invitation.Token)
					return acceptance
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "removal", results[0])
			}
			if results[1] != nil {
				expectRaceError(t, round, "invitation acceptance", results[1])
			}
			if _, _, ok := f.membership(house, target); ok {
				t.Fatalf("round %d removal left the target in the household", round)
			}
			role, _, joined := f.membership(other, target)
			if acceptance == nil && (!joined || role != household.RoleMember) {
				t.Fatalf("round %d acceptance succeeded but membership role=%q joined=%t", round, role, joined)
			}
			if acceptance != nil && joined {
				t.Fatalf("round %d acceptance failed (%v) but the target joined anyway", round, acceptance)
			}
			got := f.defaultHousehold(target)
			if got == nil {
				t.Fatalf("round %d default household cleared although a membership remains", round)
			}
			if _, _, ok := f.membership(*got, target); !ok {
				t.Fatalf("round %d default household %s is not an existing membership", round, *got)
			}
			if *got != other {
				t.Fatalf("round %d default household=%s want %s", round, *got, other)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d want exactly the committed removal", round, audits)
			}
		}
	})

	// Transfer locks the membership row for the member before its subjects, the
	// same row the removal locks FOR UPDATE. Whichever wins, no subject may stay
	// linked to an account that is no longer a member, and a transfer that lost
	// the race must refuse instead of writing a dangling link.
	t.Run("removal races a member person transfer", func(t *testing.T) {
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("transfer race house %d", round))
			actor, worker := f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(worker, house, household.RoleMember)
			current := f.addSubject(house, "race member person", worker)
			candidate := f.addSubject(house, "race transfer candidate", "")

			var transferErr error
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, worker, 1) },
				func() error {
					_, transferErr = f.subjects.Transfer(ctx, worker, house, worker, subjectapp.TransferRequest{
						SubjectID: candidate, SubjectVersion: 1,
						CurrentSubjectID: current, CurrentSubjectVersion: 1,
					})
					return transferErr
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "removal", results[0])
			}
			if transferErr != nil {
				expectRaceError(t, round, "transfer", transferErr, subjectapp.ErrNotFound)
			}
			if _, _, ok := f.membership(house, worker); ok {
				t.Fatalf("round %d removal left the target in the household", round)
			}
			if dangling := f.danglingSubjectLinks(house); dangling != 0 {
				t.Fatalf("round %d subjects still linked to a non-member: %d", round, dangling)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d want 1", round, audits)
			}
		}
	})

	// Reset issue takes the account row, then the reset rows; the removal takes
	// the account row before revoking this household's reset links. The two
	// therefore serialize per target: a link issued here before the removal wins
	// is revoked, one issued afterwards fails authorization. A link an admin of
	// the other household issues after the removal must stay redeemable.
	t.Run("removal races password reset issuance through both households", func(t *testing.T) {
		const newPassword = "a brand new reset password"
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("reset race house %d", round))
			other := f.newHousehold(fmt.Sprintf("reset race other %d", round))
			actor, otherAdmin, target := f.newUser(), f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(actor, other, household.RoleAdmin)
			f.join(otherAdmin, other, household.RoleAdmin)
			f.join(target, house, household.RoleMember)
			f.join(target, other, household.RoleMember)
			f.password(target)

			var hereIssued, otherIssued identityapp.PasswordResetIssued
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, target, 1) },
				func() error {
					var err error
					hereIssued, err = f.resets.IssuePasswordReset(ctx, actor, house, target)
					return err
				},
				func() error {
					var err error
					otherIssued, err = f.resets.IssuePasswordReset(ctx, otherAdmin, other, target)
					return err
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "removal", results[0])
			}
			if results[1] != nil {
				expectRaceError(t, round, "reset issuance in the removal household", results[1], household.ErrNotFound)
			}
			if results[2] != nil {
				expectRaceError(t, round, "reset issuance in the other household", results[2], identityapp.ErrPasswordResetNotPermitted)
			}
			if _, _, ok := f.membership(house, target); ok {
				t.Fatalf("round %d removal left the target in the household", round)
			}
			if _, _, ok := f.membership(other, target); !ok {
				t.Fatalf("round %d removal changed the other household membership", round)
			}

			hereCount, hereRevoked := f.resetLinkState(house, target)
			switch {
			case results[1] == nil:
				if hereCount != 1 || !hereRevoked {
					t.Fatalf("round %d issued link here count=%d revoked=%t", round, hereCount, hereRevoked)
				}
				if _, err := f.resets.RedeemPasswordReset(ctx, hereIssued.Token, newPassword); !errors.Is(err, identityapp.ErrInvalidPasswordReset) {
					t.Fatalf("round %d removed household's link redeemed: %v", round, err)
				}
			case hereCount != 0:
				t.Fatalf("round %d refused issuance still wrote %d links here", round, hereCount)
			}

			otherCount, otherRevoked := f.resetLinkState(other, target)
			switch {
			case results[2] == nil:
				if otherCount != 1 || otherRevoked {
					t.Fatalf("round %d issued link other count=%d revoked=%t", round, otherCount, otherRevoked)
				}
				login, err := f.resets.RedeemPasswordReset(ctx, otherIssued.Token, newPassword)
				if err != nil {
					t.Fatalf("round %d other household's link not redeemable: %v", round, err)
				}
				if login != f.loginOf(target) {
					t.Fatalf("round %d redeemed login=%q", round, login)
				}
			case otherCount != 0:
				t.Fatalf("round %d refused issuance still wrote %d links in the other household", round, otherCount)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d want 1", round, audits)
			}
		}
	})

	// The self-service change revokes reset links, takes the credential row and
	// deletes sessions; the removal takes the account row and never a credential
	// or session lock. Both must commit, because the account survives the
	// membership removal (ADR 0018, ADR 0019).
	t.Run("removal races the target changing their own password", func(t *testing.T) {
		const (
			oldPassword = "correct horse battery"
			newPassword = "correct horse battery staple"
		)
		for round := 0; round < 10; round++ {
			house := f.newHousehold(fmt.Sprintf("password race house %d", round))
			actor, worker := f.newUser(), f.newUser()
			f.join(actor, house, household.RoleAdmin)
			f.join(worker, house, household.RoleMember)
			f.password(worker)
			login := f.loginOf(worker)

			signedIn, err := f.sessions.Login(ctx, login, oldPassword)
			if err != nil {
				t.Fatalf("round %d sign in: %v", round, err)
			}
			var changed identityapp.Session
			results := race(
				func() error { return f.service.RemoveMember(ctx, actor, house, worker, 1) },
				func() error {
					var err error
					changed, err = f.changes.ChangePassword(ctx, identityapp.ChangePasswordInput{
						Principal:       signedIn.Principal,
						SignInMethod:    signedIn.SignInMethod,
						SessionToken:    signedIn.Token,
						CurrentPassword: oldPassword,
						NewPassword:     newPassword,
					})
					return err
				},
			)
			if results[0] != nil {
				expectRaceError(t, round, "removal", results[0])
			}
			if results[1] != nil {
				expectRaceError(t, round, "password change", results[1])
			}
			if changed.Token == "" || changed.Token == signedIn.Token {
				t.Fatalf("round %d password change did not rotate the caller session", round)
			}
			if _, _, ok := f.membership(house, worker); ok {
				t.Fatalf("round %d removal left the target in the household", round)
			}
			if _, err := f.sessions.Login(ctx, login, newPassword); err != nil {
				t.Fatalf("round %d new password does not sign in: %v", round, err)
			}
			if _, err := f.sessions.Login(ctx, login, oldPassword); !errors.Is(err, identityapp.ErrInvalidCredentials) {
				t.Fatalf("round %d old password still signs in: %v", round, err)
			}
			if audits := f.auditCount(house); audits != 1 {
				t.Fatalf("round %d audit rows=%d want 1", round, audits)
			}
		}
	})
}
