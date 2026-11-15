package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (f *adminCenterFixture) householdTotal() int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM households`)
}

func (f *adminCenterFixture) creationTotal() int {
	f.t.Helper()
	return f.scalarInt(`SELECT count(*) FROM household_creations`)
}

func (f *adminCenterFixture) creationFingerprint(householdID string) []byte {
	f.t.Helper()
	var fingerprint []byte
	if err := f.admin.QueryRow(f.ctx,
		`SELECT request_fingerprint FROM household_creations WHERE household_id=$1::uuid`, householdID).Scan(&fingerprint); err != nil {
		f.t.Fatal(err)
	}
	return fingerprint
}

func TestInstanceAdminCreateHouseholdAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL admin-create tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newAdminCenterFixture(t, ctx, admin, app)
	f.reset()
	defer f.reset()

	newAdmin := func() (string, string) {
		f.t.Helper()
		actor, member := f.newUser(), f.newUser()
		f.grantInstanceAdmin(actor)
		return actor, member
	}

	t.Run("create assigns the first admin and replays identically", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		house, version, err := f.center.CreateHousehold(ctx, actor, "create-key", "  Veselí  ", "Europe/Prague", strings.ToUpper(firstAdmin))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if version != 1 {
			t.Fatalf("version=%d want 1", version)
		}
		if !ids.IsCanonicalUUID(house.ID) || house.Name != "Veselí" || house.Timezone != "Europe/Prague" || house.CreatedAt.IsZero() {
			t.Fatalf("household=%+v", house)
		}
		if got := f.scalarString(`SELECT name FROM households WHERE id=$1::uuid`, house.ID); got != "Veselí" {
			t.Fatalf("stored name=%q", got)
		}
		if got := f.userMembershipCount(house.ID, firstAdmin); got != 1 {
			t.Fatalf("admin memberships=%d want 1", got)
		}
		if got := f.scalarString(`SELECT role FROM household_memberships WHERE household_id=$1::uuid AND user_id=$2::uuid`, house.ID, firstAdmin); got != household.RoleAdmin {
			t.Fatalf("role=%q want admin", got)
		}
		if rows := f.assignmentRows(house.ID); len(rows) != 1 || rows[0] != (memberAssignmentRow{Target: firstAdmin, Actor: actor, Role: household.RoleAdmin, Authority: household.AuthorityInstanceAdmin}) {
			t.Fatalf("assignment rows=%+v", rows)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows=%d want 1", got)
		}
		if fingerprint := f.creationFingerprint(house.ID); len(fingerprint) != 32 {
			t.Fatalf("stored fingerprint length=%d", len(fingerprint))
		}

		replay, replayVersion, err := f.center.CreateHousehold(ctx, actor, "create-key", "Veselí", "Europe/Prague", firstAdmin)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if replay.ID != house.ID || replayVersion != version {
			t.Fatalf("replay household=%+v version=%d want %s %d", replay, replayVersion, house.ID, version)
		}
		if got := f.householdTotal(); got != 1 {
			t.Fatalf("households after replay=%d want 1", got)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows after replay=%d want 1", got)
		}
		if got := f.assignmentCount(house.ID); got != 1 {
			t.Fatalf("assignment rows after replay=%d want 1", got)
		}

		got, gotVersion, err := f.center.GetHousehold(ctx, actor, strings.ToUpper(house.ID))
		if err != nil || got.ID != house.ID || gotVersion != version {
			t.Fatalf("get household=%+v version=%d err=%v", got, gotVersion, err)
		}
	})

	t.Run("same key with another intent is rejected", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		if _, _, err := f.center.CreateHousehold(ctx, actor, "reuse-key", "Home", "UTC", firstAdmin); err != nil {
			t.Fatalf("create: %v", err)
		}
		for name, call := range map[string]func() error{
			"name": func() error {
				_, _, err := f.center.CreateHousehold(ctx, actor, "reuse-key", "Other", "UTC", firstAdmin)
				return err
			},
			"timezone": func() error {
				_, _, err := f.center.CreateHousehold(ctx, actor, "reuse-key", "Home", "Europe/Berlin", firstAdmin)
				return err
			},
			"admin": func() error {
				_, _, err := f.center.CreateHousehold(ctx, actor, "reuse-key", "Home", "UTC", f.newUser())
				return err
			},
		} {
			if err := call(); !errors.Is(err, household.ErrIdempotencyKeyReused) {
				t.Fatalf("%s reuse err=%v want idempotency_key_reused", name, err)
			}
		}
		if got := f.householdTotal(); got != 1 {
			t.Fatalf("households=%d want 1", got)
		}
	})

	t.Run("validation order and unknown admin", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		for _, tc := range []struct {
			name, householdName, timezone, adminUser, field, code string
		}{
			{"name before timezone", "", "Nowhere/Nothing", firstAdmin, "name", "invalid_length"},
			{"timezone", "Home", "Nowhere/Nothing", firstAdmin, "timezone", "invalid_iana_timezone"},
			{"admin user", "Home", "UTC", "not-a-uuid", "adminUserId", "invalid_reference"},
		} {
			_, _, err := f.center.CreateHousehold(ctx, actor, "validation-"+tc.name, tc.householdName, tc.timezone, tc.adminUser)
			var validation *household.ValidationError
			if !errors.As(err, &validation) || validation.Field != tc.field || validation.Code != tc.code {
				t.Fatalf("%s: err=%v want %s/%s", tc.name, err, tc.field, tc.code)
			}
		}
		if got := f.householdTotal(); got != 0 {
			t.Fatalf("households after validation=%d want 0", got)
		}
		_, _, err := f.center.CreateHousehold(ctx, actor, "missing-admin", "Home", "UTC", "00000000-0000-7000-8000-000000000000")
		if err == nil {
			t.Fatal("unknown admin accepted")
		}
		for _, forbidden := range []error{
			household.ErrNotFound,
			household.ErrAlreadyMember,
			household.ErrHouseholdRequiresAdmin,
			household.ErrInstanceAdminRequired,
			household.ErrIdempotencyKeyReused,
			household.ErrPreconditionFailed,
		} {
			if errors.Is(err, forbidden) {
				t.Fatalf("unknown admin err=%v must not match %v", err, forbidden)
			}
		}
		var validation *household.ValidationError
		if errors.As(err, &validation) {
			t.Fatalf("unknown admin err=%v must not be a validation error", err)
		}
		if got := f.householdTotal(); got != 0 {
			t.Fatalf("households after unknown admin=%d want 0", got)
		}
		if got := f.creationTotal(); got != 0 {
			t.Fatalf("creation rows after unknown admin=%d want 0", got)
		}
	})

	t.Run("non-admin actor is refused before any lookup", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := f.newUser(), f.newUser()
		if _, _, err := f.center.CreateHousehold(ctx, actor, "not-admin", "Home", "UTC", firstAdmin); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("create err=%v", err)
		}
		if _, _, err := f.center.GetHousehold(ctx, actor, "00000000-0000-7000-8000-000000000000"); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("get err=%v", err)
		}
		if got := f.householdTotal(); got != 0 {
			t.Fatalf("households=%d want 0", got)
		}
	})

	t.Run("concurrent same key replays the committed winner", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		fingerprint := f.committedCreationFingerprint(actor, "probe-same", "Race", "UTC", firstAdmin)
		if got := f.householdTotal(); got != 0 {
			t.Fatalf("probe households=%d want 0", got)
		}

		winnerTx, winner := f.holdCreationWinner(actor, "race-key", "Race", "UTC", fingerprint, firstAdmin)
		defer rollbackAdminTx(t, winnerTx)

		type createResult struct {
			house   household.AdminHousehold
			version int64
			err     error
		}
		done := make(chan createResult, 1)
		go func() {
			house, version, err := f.center.CreateHousehold(ctx, actor, "race-key", "Race", "UTC", firstAdmin)
			done <- createResult{house, version, err}
		}()
		f.awaitCreationInsertLock()
		select {
		case result := <-done:
			t.Fatalf("create returned before the winner committed: %+v err=%v", result.house, result.err)
		default:
		}
		if err := winnerTx.Commit(ctx); err != nil {
			t.Fatalf("commit winning create: %v", err)
		}
		result := <-done
		if result.err != nil {
			t.Fatalf("losing create: %v", result.err)
		}
		if result.house.ID != winner || result.version != 1 {
			t.Fatalf("replayed household=%s version=%d want winner %s version 1", result.house.ID, result.version, winner)
		}
		if got := f.householdTotal(); got != 1 {
			t.Fatalf("households=%d want 1", got)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows=%d want 1", got)
		}
		if got := f.scalarInt(`SELECT count(*) FROM household_creations WHERE creation_key=$1`, "race-key"); got != 1 {
			t.Fatalf("creation rows for the key=%d want 1", got)
		}
		if got := f.membershipCount(winner); got != 1 {
			t.Fatalf("memberships=%d want the winner's single admin", got)
		}
		if got := f.userMembershipCount(winner, firstAdmin); got != 1 {
			t.Fatalf("winner admin memberships=%d want 1", got)
		}
		if got := f.assignmentCount(winner); got != 1 {
			t.Fatalf("assignment rows=%d want 1", got)
		}
		if got := f.totalAuditRows(); got != 1 {
			t.Fatalf("audit rows=%d want only the winner's assignment", got)
		}
	})

	t.Run("concurrent same key with another intent is rejected", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		fingerprint := f.committedCreationFingerprint(actor, "probe-other", "Other", "UTC", firstAdmin)

		winnerTx, winner := f.holdCreationWinner(actor, "race-key", "Other", "UTC", fingerprint, "")
		defer rollbackAdminTx(t, winnerTx)

		done := make(chan error, 1)
		go func() {
			_, _, err := f.center.CreateHousehold(ctx, actor, "race-key", "Race", "UTC", firstAdmin)
			done <- err
		}()
		f.awaitCreationInsertLock()
		select {
		case err := <-done:
			t.Fatalf("create returned before the winner committed: %v", err)
		default:
		}
		if err := winnerTx.Commit(ctx); err != nil {
			t.Fatalf("commit winning create: %v", err)
		}
		if err := <-done; !errors.Is(err, household.ErrIdempotencyKeyReused) {
			t.Fatalf("different intent err=%v want ErrIdempotencyKeyReused", err)
		}
		if got := f.householdTotal(); got != 1 {
			t.Fatalf("households=%d want only the winner", got)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows=%d want only the winner", got)
		}
		if got := f.membershipCount(winner); got != 0 {
			t.Fatalf("memberships=%d; the rejected intent must create none", got)
		}
		if got := f.assignmentCount(winner); got != 0 {
			t.Fatalf("assignment rows=%d; the rejected intent must create none", got)
		}
	})

	t.Run("concurrent different keys create distinct households", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		houses := make([]string, 2)
		results := race(
			func() error {
				house, _, err := f.center.CreateHousehold(ctx, actor, "race-a", "A", "UTC", firstAdmin)
				houses[0] = house.ID
				return err
			},
			func() error {
				house, _, err := f.center.CreateHousehold(ctx, actor, "race-b", "B", "UTC", firstAdmin)
				houses[1] = house.ID
				return err
			},
		)
		for i, err := range results {
			if err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
		}
		if houses[0] == "" || houses[1] == "" || houses[0] == houses[1] {
			t.Fatalf("houses=%v want two distinct households", houses)
		}
		if got := f.householdTotal(); got != 2 {
			t.Fatalf("households=%d want 2", got)
		}
		if got := f.creationTotal(); got != 2 {
			t.Fatalf("creation rows=%d want 2", got)
		}
	})

	t.Run("another unique violation is not a race", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		house, _, err := f.center.CreateHousehold(ctx, actor, "adapter-key", "Home", "UTC", firstAdmin)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// A second creation row for the same household violates the primary key,
		// not the actor/key unique constraint, so it must not be mapped to the
		// replayable race signal.
		var insertErr error
		err = f.repository.WithInstanceAdminTransaction(ctx, func(tx household.InstanceAdminTransaction, _ household.InstanceAdminEffects) error {
			insertErr = tx.InsertHouseholdCreation(ctx, household.HouseholdCreation{
				HouseholdID:        house.ID,
				ActorUserID:        actor,
				CreationKey:        "adapter-fresh-key",
				RequestFingerprint: make([]byte, 32),
				CreatedAt:          time.Now().UTC(),
			})
			return insertErr
		})
		if err != nil && !errors.Is(err, insertErr) {
			t.Fatalf("adapter transaction: %v", err)
		}
		if insertErr == nil {
			t.Fatal("duplicate household creation accepted")
		}
		if errors.Is(insertErr, household.ErrCreationKeyRaced) {
			t.Fatalf("primary-key violation mapped to ErrCreationKeyRaced: %v", insertErr)
		}
		var pgErr *pgconn.PgError
		if !errors.As(insertErr, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "household_creations_pkey" {
			t.Fatalf("insert err=%v want 23505 household_creations_pkey", insertErr)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows=%d want 1", got)
		}
	})

	t.Run("get household not found", func(t *testing.T) {
		f.prepare()
		actor, _ := newAdmin()
		for _, id := range []string{"not-a-uuid", "00000000-0000-7000-8000-000000000000"} {
			if _, _, err := f.center.GetHousehold(ctx, actor, id); !errors.Is(err, household.ErrNotFound) {
				t.Fatalf("get %q err=%v", id, err)
			}
		}
	})
}

// committedCreationFingerprint asks the real service to create a household with
// the given intent, reads the production fingerprint it stored, then removes the
// probe household. The create fingerprint does not include the key, so the
// returned digest is exactly what a later create with the same actor, name,
// timezone and admin would compute.
func (f *adminCenterFixture) committedCreationFingerprint(actor, key, name, timezone, adminUserID string) []byte {
	f.t.Helper()
	house, _, err := f.center.CreateHousehold(f.ctx, actor, key, name, timezone, adminUserID)
	if err != nil {
		f.t.Fatalf("probe create: %v", err)
	}
	fingerprint := f.creationFingerprint(house.ID)
	if _, err := f.admin.Exec(f.ctx, `UPDATE user_accounts SET default_household_id=NULL WHERE default_household_id=$1::uuid`, house.ID); err != nil {
		f.t.Fatalf("clear probe default household: %v", err)
	}
	if _, err := f.admin.Exec(f.ctx, `DELETE FROM households WHERE id=$1::uuid`, house.ID); err != nil {
		f.t.Fatalf("remove probe household: %v", err)
	}
	if got := f.creationTotal(); got != 0 {
		f.t.Fatalf("probe creation rows=%d want 0", got)
	}
	return fingerprint
}

// holdCreationWinner opens an uncommitted transaction that has already committed
// the same actor/key (and optionally a realistic first-admin membership and
// assignment) so a concurrent create blocks on the actor/key unique index until
// the caller commits. It returns the transaction and the winning household ID.
func (f *adminCenterFixture) holdCreationWinner(actor, key, name, timezone string, fingerprint []byte, adminUserID string) (pgx.Tx, string) {
	f.t.Helper()
	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		f.t.Fatalf("begin winning create: %v", err)
	}
	var householdID string
	if err := tx.QueryRow(f.ctx,
		`INSERT INTO households(name, timezone, created_at) VALUES ($1, $2, now()) RETURNING id::text`,
		name, timezone).Scan(&householdID); err != nil {
		f.t.Fatalf("insert winning household: %v", err)
	}
	if _, err := tx.Exec(f.ctx,
		`INSERT INTO household_creations(household_id, actor_user_id, creation_key, request_fingerprint, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, now())`, householdID, actor, key, fingerprint); err != nil {
		f.t.Fatalf("insert winning creation row: %v", err)
	}
	if adminUserID != "" {
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, 'admin')`,
			adminUserID, householdID); err != nil {
			f.t.Fatalf("insert winning membership: %v", err)
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO household_member_assignments(household_id, target_user_id, actor_user_id, role, actor_authority, assigned_at)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, 'admin', $4, now())`,
			householdID, adminUserID, actor, household.AuthorityInstanceAdmin); err != nil {
			f.t.Fatalf("insert winning assignment: %v", err)
		}
	}
	return tx, householdID
}

func rollbackAdminTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(rollbackCtx)
}

// awaitCreationInsertLock waits until the concurrent create is blocked inserting
// its creation row. Polling from the runtime pool sees the runtime session; the
// admin pool cannot observe another role's sessions.
func (f *adminCenterFixture) awaitCreationInsertLock() {
	f.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var waiting int
		if err := f.app.QueryRow(f.ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
			 AND wait_event_type='Lock' AND pid<>pg_backend_pid()
			 AND query LIKE '%INSERT INTO household_creations%'`).Scan(&waiting); err != nil {
			f.t.Fatalf("poll creation lock waiters: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatal("concurrent create never blocked on the creation key")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
