package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/subject"
	subjectpg "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationURLs(t *testing.T) (string, string) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL item tests")
	}
	return appURL, adminURL
}
func pool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func TestItemsAgainstPostgres(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	var role string
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil || role != "tendo" {
		t.Fatalf("runtime role=%q err=%v", role, err)
	}
	household := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ($1,'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	hA, hB := household("A"), household("B")
	subRepo := subjectpg.NewRepository(app)
	newSubject := func(hid, name string) subject.Subject {
		t.Helper()
		s, err := subRepo.Create(ctx, hid, name, subject.TypePerson)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	sA, sB := newSubject(hA, "Person A"), newSubject(hB, "Person B")
	repo := NewRepository(app)
	create := func(hid, sid, title string, notes *string, attention *schedule.Date) item.Item {
		t.Helper()
		got, err := repo.Create(ctx, hid, item.Draft{SubjectID: sid, Title: title, Notes: notes, AttentionOn: attention})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	count := func(hid string) int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid`, hid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Run("create persists Unicode fields", func(t *testing.T) {
		notes := "  notes\n家族 🚗  "
		day, _ := schedule.NewDate(2030, time.January, 2)
		got := create(hA, sA.ID, "Veselí 家族 🚗", &notes, &day)
		if got.Title != "Veselí 家族 🚗" || got.Notes == nil || *got.Notes != notes || got.AttentionOn == nil || *got.AttentionOn != day || got.Version != 1 || got.WorkflowState != item.StateOpen || got.HouseholdID != hA {
			t.Fatalf("item=%+v", got)
		}
		var title, storedNotes, state string
		var date time.Time
		if err := admin.QueryRow(ctx, `SELECT title,notes,attention_on,workflow_state FROM items WHERE id=$1::uuid`, got.ID).Scan(&title, &storedNotes, &date, &state); err != nil {
			t.Fatal(err)
		}
		if title != got.Title || storedNotes != notes || date.Format("2006-01-02") != "2030-01-02" || state != "open" {
			t.Fatalf("stored=%q %q %v %q", title, storedNotes, date, state)
		}
	})
	t.Run("recurrence round trips, clears, and respects database constraints", func(t *testing.T) {
		for _, tc := range []struct {
			value int
			unit  schedule.Unit
			mode  schedule.Mode
		}{
			{1, schedule.UnitDay, schedule.ModeFixed}, {2, schedule.UnitWeek, schedule.ModeAfterCompletion}, {3, schedule.UnitMonth, schedule.ModeFixed}, {4, schedule.UnitYear, schedule.ModeAfterCompletion},
		} {
			policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: tc.value, Unit: tc.unit}, Mode: tc.mode}
			i := create(hA, sA.ID, "recurrence", nil, nil)
			updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(*policy)})
			if err != nil || updated.Recurrence == nil || *updated.Recurrence != *policy {
				t.Fatalf("round trip=%+v err=%v", updated, err)
			}
			got, err := repo.Get(ctx, hA, i.ID)
			if err != nil || got.Recurrence == nil || *got.Recurrence != *policy {
				t.Fatalf("get=%+v err=%v", got, err)
			}
			cleared, err := repo.Update(ctx, hA, i.ID, 2, item.Change{Recurrence: item.Null[schedule.Policy]()})
			if err != nil || cleared.Recurrence != nil {
				t.Fatalf("clear=%+v err=%v", cleared, err)
			}
		}
		for _, stmt := range []string{
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value) VALUES ($1::uuid,$2::uuid,'partial',1)`,
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'zero',0,'day','fixed')`,
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'high',1000,'day','fixed')`,
		} {
			if _, err := admin.Exec(ctx, stmt, hA, sA.ID); err == nil {
				t.Fatalf("constraint accepted %s", stmt)
			}
		}
		i := create(hA, sA.ID, "runtime recurrence", nil, nil)
		_, err := app.Exec(ctx, `UPDATE items SET recurrence_interval_value=1,recurrence_interval_unit='year',recurrence_mode='fixed' WHERE id=$1::uuid`, i.ID)
		if err != nil {
			t.Fatalf("runtime recurrence update denied: %v", err)
		}
	})
	t.Run("pagination no duplicate and archive filter", func(t *testing.T) {
		h := household("Pages")
		s := newSubject(h, "subject")
		var ids []string
		for i := 0; i < 7; i++ {
			ids = append(ids, create(h, s.ID, "row "+string(rune('a'+i)), nil, nil).ID)
		}
		var got []string
		after := "00000000-0000-0000-0000-000000000000"
		for {
			rows, err := repo.List(ctx, h, false, after, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range rows {
				got = append(got, v.ID)
			}
			if len(rows) < 3 {
				break
			}
			after = rows[len(rows)-1].ID
		}
		if strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Fatalf("got=%v want=%v", got, ids)
		}
		yes := true
		if _, err := repo.Update(ctx, h, ids[1], 1, item.Change{Archived: &yes}); err != nil {
			t.Fatal(err)
		}
		active, err := repo.List(ctx, h, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(active) != 6 {
			t.Fatalf("active=%d err=%v", len(active), err)
		}
		archived, err := repo.List(ctx, h, true, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(archived) != 1 || archived[0].ID != ids[1] {
			t.Fatalf("archived=%+v err=%v", archived, err)
		}
	})
	t.Run("version mismatch leaves row unchanged", func(t *testing.T) {
		i := create(hA, sA.ID, "stable", nil, nil)
		title := "changed"
		if _, err := repo.Update(ctx, hA, i.ID, 9, item.Change{Title: &title}); !errors.Is(err, item.ErrVersionMismatch) {
			t.Fatalf("err=%v", err)
		}
		got, err := repo.Get(ctx, hA, i.ID)
		if err != nil || got != i {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title})
		if err != nil || updated.Version != 2 || updated.Title != title {
			t.Fatalf("updated=%+v err=%v", updated, err)
		}
	})
	t.Run("concurrent updates exactly one succeeds", func(t *testing.T) {
		i := create(hA, sA.ID, "race", nil, nil)
		const workers = 8
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, workers)
		for n := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				v := "writer" + string(rune('a'+n))
				_, errs[n] = repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &v})
			}(n)
		}
		close(start)
		wg.Wait()
		ok, stale := 0, 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if errors.Is(err, item.ErrVersionMismatch) {
				stale++
			} else {
				t.Fatalf("err=%v", err)
			}
		}
		if ok != 1 || stale != workers-1 {
			t.Fatalf("success=%d stale=%d", ok, stale)
		}
	})
	t.Run("household isolation and foreign subject rejected", func(t *testing.T) {
		i := create(hB, sB.ID, "private", nil, nil)
		if _, err := repo.Get(ctx, hA, i.ID); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("get=%v", err)
		}
		title := "stolen"
		if _, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("update=%v", err)
		}
		rows, err := repo.List(ctx, hA, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range rows {
			if v.ID == i.ID || v.HouseholdID != hA {
				t.Fatalf("foreign list entry=%+v", v)
			}
		}
		if _, err := repo.Create(ctx, hA, item.Draft{SubjectID: sB.ID, Title: "bad"}); !errors.Is(err, item.ErrInvalidReference) {
			t.Fatalf("foreign subject err=%v", err)
		}
		var foreignCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND subject_id=$2::uuid`, hA, sB.ID).Scan(&foreignCount); err != nil || foreignCount != 0 {
			t.Fatalf("foreign item persisted; count=%d err=%v", foreignCount, err)
		}
	})
	t.Run("runtime permissions and database constraints", func(t *testing.T) {
		i := create(hA, sA.ID, "protected", nil, nil)
		for _, stmt := range []string{`DELETE FROM items WHERE id=$1::uuid`, `TRUNCATE items`} {
			args := []any{i.ID}
			if stmt == `TRUNCATE items` {
				args = nil
			}
			if _, err := app.Exec(ctx, stmt, args...); err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("%s err=%v", stmt, err)
			}
		}
		for field, stmt := range map[string]string{"id": `UPDATE items SET id=uuidv7() WHERE id=$1::uuid`, "household_id": `UPDATE items SET household_id=$2::uuid WHERE id=$1::uuid`, "created_at": `UPDATE items SET created_at=now() WHERE id=$1::uuid`} {
			args := []any{i.ID}
			if field == "household_id" {
				args = append(args, hB)
			}
			_, err := app.Exec(ctx, stmt, args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("update %s err=%v", field, err)
			}
		}
		for label, stmt := range map[string]string{"blank title": `INSERT INTO items(household_id,subject_id,title) VALUES ($1::uuid,$2::uuid,'  ')`, "empty notes": `INSERT INTO items(household_id,subject_id,title,notes) VALUES ($1::uuid,$2::uuid,'ok','')`, "bad state": `INSERT INTO items(household_id,subject_id,title,workflow_state) VALUES ($1::uuid,$2::uuid,'ok','done')`} {
			if _, err := admin.Exec(ctx, stmt, hA, sA.ID); err == nil {
				t.Fatalf("%s accepted", label)
			}
		}
	})
	t.Run("household deletion cascades", func(t *testing.T) {
		h := household("cascade")
		s := newSubject(h, "child")
		create(h, s.ID, "cascade item", nil, nil)
		if _, err := admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, h); err != nil {
			t.Fatal(err)
		}
		if count(h) != 0 {
			t.Fatal("items survived household deletion")
		}
	})
}

func TestMigrationUpgradeFromVersionFivePreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v5_items_test"
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
	if _, err := upAdmin.Exec(ctx, `ALTER TABLE items DROP CONSTRAINT items_recurrence_all_or_none; ALTER TABLE items DROP COLUMN recurrence_interval_value, DROP COLUMN recurrence_interval_unit, DROP COLUMN recurrence_mode; DELETE FROM tendo_schema_migrations WHERE version=6`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, iid string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ('Legacy','Europe/Prague') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES ($1::uuid,'person','Legacy person') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,attention_on) VALUES ($1::uuid,$2::uuid,'Legacy item','2030-01-02') RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("v5 schema accepted by v6 application")
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatal(err)
	}
	got, err := NewRepository(upApp).Get(ctx, hid, iid)
	if err != nil || got.Title != "Legacy item" || got.Recurrence != nil || got.AttentionOn == nil || got.AttentionOn.String() != "2030-01-02" {
		t.Fatalf("legacy item=%+v err=%v", got, err)
	}
	var max int64
	if err := upAdmin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&max); err != nil || max != 6 {
		t.Fatalf("version=%d err=%v", max, err)
	}
}

func TestMigrationUpgradeFromVersionFourPreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v4_items_test"
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
	if _, err := upAdmin.Exec(ctx, `DROP TABLE items; ALTER TABLE subjects DROP CONSTRAINT subjects_household_id_id_key; DELETE FROM tendo_schema_migrations WHERE version IN (5,6)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ('Legacy','Europe/Prague') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES ($1::uuid,'person','Legacy person') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("v4 database accepted by v6 application")
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := upApp.QueryRow(ctx, `SELECT name FROM subjects WHERE id=$1::uuid`, sid).Scan(&name); err != nil || name != "Legacy person" {
		t.Fatalf("legacy data=%q err=%v", name, err)
	}
	created, err := NewRepository(upApp).Create(ctx, hid, item.Draft{SubjectID: sid, Title: "After upgrade"})
	if err != nil || created.Version != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var max int64
	var dirty bool
	if err := upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 6 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
