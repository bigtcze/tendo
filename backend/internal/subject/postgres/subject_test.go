package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func urls(t *testing.T) (string, string) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL subject tests")
	}
	return appURL, adminURL
}

func connect(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSubjectsAgainstPostgres(t *testing.T) {
	appURL, adminURL := urls(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()

	var currentUser string
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil || currentUser != "tendo" {
		t.Fatalf("must use the restricted runtime role, got %q err=%v", currentUser, err)
	}
	newHousehold := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	homeA, homeB := newHousehold("A"), newHousehold("B")
	repo := NewRepository(app)

	create := func(hid, name string, typ subject.Type) subject.Subject {
		t.Helper()
		s, err := repo.Create(ctx, hid, name, typ)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		return s
	}
	rowCount := func(hid string) int {
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE household_id=$1::uuid`, hid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("create persists Unicode name type and version 1", func(t *testing.T) {
		s, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", homeA, "  Veselí 家族 🚗  ", subject.TypeVehicle)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "Veselí 家族 🚗" || s.Type != subject.TypeVehicle || s.Version != 1 || s.Archived || s.HouseholdID != homeA || s.ID == "" {
			t.Fatalf("s=%+v", s)
		}
		if s.CreatedAt.Location() != time.UTC || time.Since(s.CreatedAt) > time.Minute || !s.CreatedAt.Equal(s.UpdatedAt) {
			t.Fatalf("timestamps created=%v updated=%v", s.CreatedAt, s.UpdatedAt)
		}
		var name, typ string
		var version int64
		var archived bool
		if err := admin.QueryRow(ctx, `SELECT name, type, version, archived FROM subjects WHERE id=$1::uuid`, s.ID).Scan(&name, &typ, &version, &archived); err != nil {
			t.Fatal(err)
		}
		if name != "Veselí 家族 🚗" || typ != "vehicle" || version != 1 || archived {
			t.Fatalf("stored name=%q type=%q version=%d archived=%v", name, typ, version, archived)
		}
		got, err := repo.Get(ctx, homeA, s.ID)
		if err != nil || got != s {
			t.Fatalf("get=%+v err=%v want=%+v", got, err, s)
		}
	})

	t.Run("get unknown or malformed ids", func(t *testing.T) {
		if _, err := repo.Get(ctx, homeA, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
		if _, err := repo.Get(ctx, homeA, "nope"); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("pagination over three pages without gaps or duplicates", func(t *testing.T) {
		hid := newHousehold("Pages")
		var want []string
		for i := 0; i < 7; i++ {
			want = append(want, create(hid, "item "+string(rune('a'+i)), subject.TypeCustom).ID)
		}
		svc := subject.NewService(repo, func(context.Context, string, string) error { return nil })
		var got []string
		cursor, pages := "", 0
		for {
			page, err := svc.List(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", hid, subject.ListQuery{Limit: 3, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			pages++
			for _, item := range page.Items {
				got = append(got, item.ID)
			}
			if page.NextCursor == nil {
				if len(page.Items) != 1 {
					t.Fatalf("last page size=%d", len(page.Items))
				}
				break
			}
			if len(page.Items) != 3 {
				t.Fatalf("page size=%d", len(page.Items))
			}
			cursor = *page.NextCursor
		}
		if pages != 3 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("pages=%d got=%v want=%v", pages, got, want)
		}
		// An exact multiple of the limit must end with a null cursor, not an empty page.
		page, err := svc.List(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", hid, subject.ListQuery{Limit: 7})
		if err != nil || len(page.Items) != 7 || page.NextCursor != nil {
			t.Fatalf("exact page=%d next=%v err=%v", len(page.Items), page.NextCursor, err)
		}
		// Subjects created after a cursor was issued appear after it, never before.
		late := create(hid, "late", subject.TypeCustom)
		page, err = svc.List(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", hid, subject.ListQuery{Limit: 7, Cursor: subject.EncodeCursor(want[6])})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != late.ID {
			t.Fatalf("late page=%+v err=%v", page, err)
		}
	})

	t.Run("archived filter separation", func(t *testing.T) {
		hid := newHousehold("Archive")
		active := create(hid, "active", subject.TypeHome)
		gone := create(hid, "gone", subject.TypePet)
		yes := true
		archivedSubject, err := repo.Update(ctx, hid, gone.ID, 1, subject.Patch{Archived: &yes})
		if err != nil || !archivedSubject.Archived || archivedSubject.Version != 2 {
			t.Fatalf("archive=%+v err=%v", archivedSubject, err)
		}
		list := func(archived bool) []subject.Subject {
			items, err := repo.List(ctx, hid, archived, "00000000-0000-0000-0000-000000000000", 10)
			if err != nil {
				t.Fatal(err)
			}
			return items
		}
		if items := list(false); len(items) != 1 || items[0].ID != active.ID {
			t.Fatalf("active=%+v", items)
		}
		if items := list(true); len(items) != 1 || items[0].ID != gone.ID {
			t.Fatalf("archived=%+v", items)
		}
		if got, err := repo.Get(ctx, hid, gone.ID); err != nil || !got.Archived {
			t.Fatalf("archived subject must stay readable: %+v err=%v", got, err)
		}
		no := false
		if back, err := repo.Update(ctx, hid, gone.ID, 2, subject.Patch{Archived: &no}); err != nil || back.Archived || back.Version != 3 {
			t.Fatalf("unarchive=%+v err=%v", back, err)
		}
		if len(list(true)) != 0 || len(list(false)) != 2 {
			t.Fatal("unarchive did not move the subject back")
		}
	})

	t.Run("update bumps version and changes fields", func(t *testing.T) {
		s := create(homeA, "Before", subject.TypeCustom)
		time.Sleep(5 * time.Millisecond)
		name, typ := "After", subject.TypePerson
		updated, err := repo.Update(ctx, homeA, s.ID, 1, subject.Patch{Name: &name, Type: &typ})
		if err != nil {
			t.Fatal(err)
		}
		if updated.Name != "After" || updated.Type != subject.TypePerson || updated.Version != 2 || updated.Archived || !updated.CreatedAt.Equal(s.CreatedAt) || !updated.UpdatedAt.After(s.UpdatedAt) {
			t.Fatalf("updated=%+v original=%+v", updated, s)
		}
		onlyName := "Third"
		third, err := repo.Update(ctx, homeA, s.ID, 2, subject.Patch{Name: &onlyName})
		if err != nil || third.Name != "Third" || third.Type != subject.TypePerson || third.Version != 3 {
			t.Fatalf("partial update must keep other fields: %+v err=%v", third, err)
		}
		stored, _ := repo.Get(ctx, homeA, s.ID)
		if stored != third {
			t.Fatalf("stored=%+v want=%+v", stored, third)
		}
	})

	t.Run("stale version is a mismatch and leaves the row unchanged", func(t *testing.T) {
		s := create(homeA, "Stable", subject.TypeHome)
		name := "Changed"
		if _, err := repo.Update(ctx, homeA, s.ID, 5, subject.Patch{Name: &name}); !errors.Is(err, subject.ErrVersionMismatch) {
			t.Fatalf("err=%v", err)
		}
		if got, _ := repo.Get(ctx, homeA, s.ID); got != s {
			t.Fatalf("row changed: %+v want %+v", got, s)
		}
		if _, err := repo.Update(ctx, homeA, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99", 1, subject.Patch{Name: &name}); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("missing err=%v", err)
		}
	})

	t.Run("concurrent updates with the same expected version yield exactly one success", func(t *testing.T) {
		s := create(homeA, "Race", subject.TypeCustom)
		const workers = 12
		var wg sync.WaitGroup
		results := make([]error, workers)
		start := make(chan struct{})
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				name := "writer-" + string(rune('a'+i))
				_, results[i] = repo.Update(ctx, homeA, s.ID, 1, subject.Patch{Name: &name})
			}(i)
		}
		close(start)
		wg.Wait()
		ok, mismatch := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, subject.ErrVersionMismatch):
				mismatch++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if ok != 1 || mismatch != workers-1 {
			t.Fatalf("ok=%d mismatch=%d", ok, mismatch)
		}
		if got, _ := repo.Get(ctx, homeA, s.ID); got.Version != 2 || !strings.HasPrefix(got.Name, "writer-") {
			t.Fatalf("final=%+v", got)
		}
	})

	t.Run("cross-household isolation", func(t *testing.T) {
		b := create(homeB, "Bob's boat", subject.TypeVehicle)
		listedB, err := repo.List(ctx, homeB, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(listedB) != 1 || listedB[0].ID != b.ID {
			t.Fatalf("household B list=%+v err=%v", listedB, err)
		}
		if _, err := repo.Get(ctx, homeA, b.ID); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("get via A: %v", err)
		}
		name := "Stolen"
		if _, err := repo.Update(ctx, homeA, b.ID, 1, subject.Patch{Name: &name}); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("update via A: %v", err)
		}
		listedA, err := repo.List(ctx, homeA, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listedA {
			if item.ID == b.ID || item.HouseholdID != homeA {
				t.Fatalf("household A list contains foreign subject: %+v", item)
			}
		}
		if got, _ := repo.Get(ctx, homeB, b.ID); got != b {
			t.Fatalf("B's subject changed: %+v", got)
		}
	})

	t.Run("runtime role cannot delete and constraints hold at database level", func(t *testing.T) {
		s := create(homeA, "Keep", subject.TypeHome)
		if _, err := app.Exec(ctx, `DELETE FROM subjects WHERE id=$1::uuid`, s.ID); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("runtime delete err=%v", err)
		}
		if _, err := app.Exec(ctx, `TRUNCATE subjects`); err == nil {
			t.Fatal("runtime truncate accepted")
		}
		if _, err := repo.Get(ctx, homeA, s.ID); err != nil {
			t.Fatalf("subject lost: %v", err)
		}
		before := rowCount(homeA)
		for name, stmt := range map[string]string{
			"tab-only name": `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', E'\t')`,
			"bad type":      `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'robot', 'x')`,
			"empty name":    `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', '')`,
			"blank name":    `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', '   ')`,
			"long name":     `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', repeat('a',101))`,
			"zero version":  `INSERT INTO subjects(household_id, type, name, version) VALUES ($1::uuid, 'home', 'x', 0)`,
			"null name":     `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', NULL)`,
			"orphan househ": `INSERT INTO subjects(household_id, type, name) VALUES (uuidv7(), 'home', 'x')`,
		} {
			args := []any{}
			if !strings.Contains(name, "orphan") {
				args = append(args, homeA)
			}
			if _, err := admin.Exec(ctx, stmt, args...); err == nil {
				t.Fatalf("%s accepted by database", name)
			}
			if _, err := app.Exec(ctx, stmt, args...); err == nil {
				t.Fatalf("%s accepted for runtime role", name)
			}
		}
		for name, stmt := range map[string]string{
			"household_id": `UPDATE subjects SET household_id=$2::uuid WHERE id=$1::uuid`,
			"created_at":   `UPDATE subjects SET created_at=now() WHERE id=$1::uuid`,
			"id":           `UPDATE subjects SET id=uuidv7() WHERE id=$1::uuid`,
		} {
			args := []any{s.ID}
			if name == "household_id" {
				args = append(args, homeB)
			}
			_, err := app.Exec(ctx, stmt, args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("runtime update of %s must fail with 42501, got %v", name, err)
			}
		}
		if got, err := repo.Get(ctx, homeA, s.ID); err != nil || got != s {
			t.Fatalf("denied updates changed the row: %+v err=%v", got, err)
		}
		if _, err := admin.Exec(ctx, `UPDATE subjects SET type='robot' WHERE id=$1::uuid`, s.ID); err == nil {
			t.Fatal("bad type accepted on update")
		}
		if rowCount(homeA) != before {
			t.Fatal("rejected inserts changed row count")
		}
	})

	t.Run("create in a household that no longer exists is not found", func(t *testing.T) {
		if _, err := repo.Create(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99", "orphan", subject.TypeHome); !errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("deleting a household cascades to its subjects", func(t *testing.T) {
		hid := newHousehold("Doomed")
		create(hid, "child", subject.TypePet)
		if _, err := admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, hid); err != nil {
			t.Fatal(err)
		}
		if rowCount(hid) != 0 {
			t.Fatal("subjects survived household deletion")
		}
	})

	t.Run("persistence failure is not reported as not found", func(t *testing.T) {
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if _, err := repo.Get(cancelled, homeA, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"); err == nil || errors.Is(err, subject.ErrNotFound) {
			t.Fatalf("get err=%v", err)
		}
		name := "x"
		if _, err := repo.Update(cancelled, homeA, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99", 1, subject.Patch{Name: &name}); err == nil || errors.Is(err, subject.ErrNotFound) || errors.Is(err, subject.ErrVersionMismatch) {
			t.Fatalf("update err=%v", err)
		}
	})
}

func TestMigrationUpgradeFromVersionThreePreservesData(t *testing.T) {
	appURL, adminURL := urls(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v3_subjects_test"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 20*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	}()
	adminCfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	// Rewind to the exact version 3 schema, then add pre-existing data.
	if _, err := upAdmin.Exec(ctx, `DROP TABLE items; DROP TABLE subjects; DELETE FROM tendo_schema_migrations WHERE version IN (4,5)`); err != nil {
		t.Fatal(err)
	}
	var hid string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ('Legacy Home', 'Europe/Prague') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if _, err := upAdmin.Exec(ctx, `UPDATE households SET version=3 WHERE id=$1::uuid`, hid); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("version 3 database accepted by version 5 application")
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatalf("validate after upgrade: %v", err)
	}
	var name, tz string
	var version int64
	if err := upApp.QueryRow(ctx, `SELECT name, timezone, version FROM households WHERE id=$1::uuid`, hid).Scan(&name, &tz, &version); err != nil || name != "Legacy Home" || tz != "Europe/Prague" || version != 3 {
		t.Fatalf("household data changed: %q %q %d err=%v", name, tz, version, err)
	}
	repo := NewRepository(upApp)
	created, err := repo.Create(ctx, hid, "After upgrade", subject.TypePerson)
	if err != nil || created.Version != 1 {
		t.Fatalf("create after upgrade: %+v err=%v", created, err)
	}
	if _, err := upApp.Exec(ctx, `DELETE FROM subjects`); err == nil {
		t.Fatal("runtime role can delete subjects after upgrade")
	}
	var max int64
	var dirty bool
	if err := upAdmin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 5 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
