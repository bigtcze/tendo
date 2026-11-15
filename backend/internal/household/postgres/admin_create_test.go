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
		if err == nil || errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown admin err=%v, want a generic non-404 failure", err)
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

	t.Run("concurrent same key creates exactly one household", func(t *testing.T) {
		f.prepare()
		actor, firstAdmin := newAdmin()
		ids2, versions := make([]string, 2), make([]int64, 2)
		results := race(
			func() error {
				house, version, err := f.center.CreateHousehold(ctx, actor, "race-key", "Race", "UTC", firstAdmin)
				ids2[0], versions[0] = house.ID, version
				return err
			},
			func() error {
				house, version, err := f.center.CreateHousehold(ctx, actor, "race-key", "Race", "UTC", firstAdmin)
				ids2[1], versions[1] = house.ID, version
				return err
			},
		)
		for i, err := range results {
			if err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
			if versions[i] != 1 {
				t.Fatalf("round %d version=%d want 1", i, versions[i])
			}
		}
		if ids2[0] == "" || ids2[0] != ids2[1] {
			t.Fatalf("ids=%v want the same household", ids2)
		}
		if got := f.householdTotal(); got != 1 {
			t.Fatalf("households=%d want 1", got)
		}
		if got := f.creationTotal(); got != 1 {
			t.Fatalf("creation rows=%d want 1", got)
		}
		if got := f.assignmentCount(ids2[0]); got != 1 {
			t.Fatalf("assignment rows=%d want 1", got)
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
