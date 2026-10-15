package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/subject"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
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

type findBarrierRepository struct {
	subject.Repository
	target  int
	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func newFindBarrierRepository(repo subject.Repository, target int) *findBarrierRepository {
	return &findBarrierRepository{Repository: repo, target: target, release: make(chan struct{})}
}

func (r *findBarrierRepository) FindCreation(ctx context.Context, householdID, actorID, key string) (subject.Subject, []byte, error) {
	found, fingerprint, err := r.Repository.FindCreation(ctx, householdID, actorID, key)
	if errors.Is(err, subject.ErrNotFound) {
		r.mu.Lock()
		r.arrived++
		if r.arrived > r.target {
			r.mu.Unlock()
			return found, fingerprint, err
		}
		if r.arrived == r.target {
			close(r.release)
		}
		r.mu.Unlock()
		select {
		case <-r.release:
		case <-ctx.Done():
			return subject.Subject{}, nil, ctx.Err()
		}
	}
	return found, fingerprint, err
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

const goldenSubjectFingerprint = "f2b6334101aa25a1bb974aea738a9dc17081c3408e12f857b24a3fe41cb220c1"

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
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil || currentUser != testpostgres.MustRuntimeRole(t) {
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
	var actorID, secondActor string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(id,login) VALUES ('0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60','subject_test_actor') RETURNING id::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('subject_test_actor_two') RETURNING id::text`).Scan(&secondActor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES ($1::uuid,$2::uuid,'owner'),($3::uuid,$2::uuid,'member')`, actorID, homeA, secondActor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES ($1::uuid,$2::uuid,'owner')`, actorID, homeB); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)

	create := func(hid, name string, typ subject.Type) subject.Subject {
		t.Helper()
		s, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, actorID, hid, "seed-"+strings.ReplaceAll(name, " ", "-"), name, typ)
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
		s, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, actorID, homeA, "pg-key", "  Veselí 家族 🚗  ", subject.TypeVehicle)
		if err != nil {
			t.Fatalf("create subject: %v", err)
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
		var pageActor string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('subject_pages_actor') RETURNING id::text`).Scan(&pageActor); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, pageActor, hid); err != nil {
			t.Fatal(err)
		}
		var want []string
		for i := 0; i < 7; i++ {
			name := "item " + string(rune('a'+i))
			created, err := subject.NewService(repo, func(_ context.Context, userID, householdID string) error {
				if userID != pageActor || householdID != hid {
					return subject.ErrNotFound
				}
				return nil
			}).Create(ctx, pageActor, hid, "page-"+strings.ReplaceAll(name, " ", "-"), name, subject.TypeCustom)
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, created.ID)
		}
		svc := subject.NewService(repo, func(_ context.Context, userID, householdID string) error {
			if userID != pageActor || householdID != hid {
				return subject.ErrNotFound
			}
			return nil
		})
		var got []string
		cursor, pages := "", 0
		for {
			page, err := svc.List(ctx, pageActor, hid, subject.ListQuery{Limit: 3, Cursor: cursor})
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
		page, err := svc.List(ctx, pageActor, hid, subject.ListQuery{Limit: 7})
		if err != nil || len(page.Items) != 7 || page.NextCursor != nil {
			t.Fatalf("exact page=%d next=%v err=%v", len(page.Items), page.NextCursor, err)
		}
		// Subjects created after a cursor was issued appear after it, never before.
		late := create(hid, "late", subject.TypeCustom)
		page, err = svc.List(ctx, pageActor, hid, subject.ListQuery{Limit: 7, Cursor: subject.EncodeCursor(want[6])})
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

	t.Run("idempotent creation replay and concurrency", func(t *testing.T) {
		isMember := func(_ context.Context, userID, householdID string) error {
			if userID != actorID || householdID != homeA {
				return subject.ErrNotFound
			}
			return nil
		}
		key := "pg-replay-key"
		baselineRows := rowCount(homeA)
		first, err := subject.NewService(repo, isMember).Create(ctx, actorID, homeA, key, "Replay subject", subject.TypeHome)
		if err != nil {
			t.Fatalf("first create=%+v err=%v", first, err)
		}
		var storedActor, storedKey string
		var storedFingerprint []byte
		if err := admin.QueryRow(ctx, `SELECT created_by_user_id::text,creation_key,creation_request_fingerprint FROM subjects WHERE id=$1::uuid`, first.ID).Scan(&storedActor, &storedKey, &storedFingerprint); err != nil || storedActor != actorID || storedKey != key || len(storedFingerprint) != 32 {
			t.Fatalf("metadata creator=%s key=%s fp=%x err=%v", storedActor, storedKey, storedFingerprint, err)
		}
		wantFingerprint, err := hex.DecodeString(goldenSubjectFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if string(storedFingerprint) != string(wantFingerprint) {
			t.Fatalf("stored fingerprint %x want %x", storedFingerprint, wantFingerprint)
		}
		original := first
		updateName, updateType, archive := "Renamed replay", subject.TypeVehicle, true
		current, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Update(ctx, actorID, homeA, first.ID, 1, subject.Patch{Name: &updateName, Type: &updateType, Archived: &archive})
		if err != nil {
			t.Fatal(err)
		}
		metadataBefore := append([]byte(nil), storedFingerprint...)
		beforeCount := rowCount(homeA)
		replayed, err := subject.NewService(repo, isMember).Create(ctx, actorID, homeA, key, "Replay subject", subject.TypeHome)
		if err != nil || replayed != current {
			t.Fatalf("replay=%+v current=%+v err=%v", replayed, current, err)
		}
		persistedAfterReplay, err := repo.Get(ctx, homeA, original.ID)
		if err != nil || persistedAfterReplay != current {
			t.Fatalf("persisted after replay=%+v current=%+v err=%v", persistedAfterReplay, current, err)
		}
		if rowCount(homeA) != beforeCount || beforeCount != baselineRows+1 {
			t.Fatalf("replay changed count %d -> %d baseline=%d", beforeCount, rowCount(homeA), baselineRows)
		}
		var afterActor, afterKey string
		var afterFingerprint []byte
		if err = admin.QueryRow(ctx, `SELECT created_by_user_id::text,creation_key,creation_request_fingerprint FROM subjects WHERE id=$1::uuid`, original.ID).Scan(&afterActor, &afterKey, &afterFingerprint); err != nil || afterActor != storedActor || afterKey != storedKey || string(afterFingerprint) != string(metadataBefore) {
			t.Fatalf("replay metadata changed actor=%s key=%s fp=%x err=%v", afterActor, afterKey, afterFingerprint, err)
		}

		const workers = 8
		concurrentKey := "pg-concurrent-same"
		before := rowCount(homeA)
		barrierRepo := newFindBarrierRepository(repo, workers)
		start := make(chan struct{})
		results := make(chan subject.Subject, workers)
		errs := make(chan error, workers)
		var concurrentWG sync.WaitGroup
		for i := 0; i < workers; i++ {
			concurrentWG.Add(1)
			go func() {
				defer concurrentWG.Done()
				<-start
				got, e := subject.NewService(barrierRepo, isMember).Create(ctx, actorID, homeA, concurrentKey, "Concurrent same", subject.TypeHome)
				if e != nil {
					errs <- e
				} else {
					results <- got
				}
			}()
		}
		close(start)
		concurrentWG.Wait()
		close(results)
		close(errs)
		allResults := make([]subject.Subject, 0, workers)
		for got := range results {
			allResults = append(allResults, got)
		}
		for e := range errs {
			t.Fatalf("identical concurrent create failed: %v", e)
		}
		if len(allResults) != workers {
			t.Fatalf("got %d results, want %d", len(allResults), workers)
		}
		sameBodyFirst := allResults[0]
		for _, got := range allResults {
			if got.ID != sameBodyFirst.ID {
				t.Fatalf("concurrent IDs %s != %s", got.ID, sameBodyFirst.ID)
			}
		}
		if rowCount(homeA) != before+1 || before != baselineRows+1 {
			t.Fatalf("same-body concurrency row count=%d, before=%d baseline=%d", rowCount(homeA), before, baselineRows)
		}

		differentKey := "pg-racing-bodies"
		before = rowCount(homeA)
		barrierRepo = newFindBarrierRepository(repo, 2)
		start = make(chan struct{})
		concurrentResults := make(chan error, 2)
		var differentWG sync.WaitGroup
		for _, name := range []string{"Intent A", "Intent B"} {
			name := name
			differentWG.Add(1)
			go func() {
				defer differentWG.Done()
				<-start
				_, e := subject.NewService(barrierRepo, isMember).Create(ctx, actorID, homeA, differentKey, name, subject.TypeHome)
				concurrentResults <- e
			}()
		}
		close(start)
		differentWG.Wait()
		close(concurrentResults)
		outcomes := []error{<-concurrentResults, <-concurrentResults}
		createdCount, reusedCount := 0, 0
		for _, e := range outcomes {
			if e == nil {
				createdCount++
			} else if errors.Is(e, subject.ErrIdempotencyKeyReused) {
				reusedCount++
			} else {
				t.Fatalf("concurrent mismatch error=%v", e)
			}
		}
		if createdCount != 1 || reusedCount != 1 || rowCount(homeA) != before+1 {
			t.Fatalf("concurrent differing bodies created=%d reused=%d rows=%d before=%d", createdCount, reusedCount, rowCount(homeA), before)
		}
		otherActor := secondActor
		independent, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, otherActor, homeA, key, "Replay subject", subject.TypeHome)
		if err != nil || independent.ID == first.ID {
			t.Fatalf("other actor=%+v err=%v", independent, err)
		}
		independent, err = subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, actorID, homeB, key, "Replay subject", subject.TypeHome)
		if err != nil || independent.ID == first.ID {
			t.Fatalf("other household=%+v err=%v", independent, err)
		}
	})

	t.Run("cross-household isolation", func(t *testing.T) {
		b := create(homeB, "Bob's boat", subject.TypeVehicle)
		listedB, err := repo.List(ctx, homeB, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(listedB) != 2 || listedB[1].ID != b.ID {
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
			"tab-only name":              `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', E'\t')`,
			"bad type":                   `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'robot', 'x')`,
			"empty name":                 `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', '')`,
			"blank name":                 `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', '   ')`,
			"long name":                  `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', repeat('a',101))`,
			"partial creation metadata":  `INSERT INTO subjects(household_id, type, name, created_by_user_id) VALUES ($1::uuid, 'home', 'x', $2::uuid)`,
			"invalid creation key":       `INSERT INTO subjects(household_id, type, name, created_by_user_id, creation_key, creation_request_fingerprint) VALUES ($1::uuid, 'home', 'x', $2::uuid, E'bad key', decode(repeat('aa',32),'hex'))`,
			"invalid fingerprint length": `INSERT INTO subjects(household_id, type, name, created_by_user_id, creation_key, creation_request_fingerprint) VALUES ($1::uuid, 'home', 'x', $2::uuid, 'valid-key', decode(repeat('aa',31),'hex'))`,
			"duplicate creation key":     `INSERT INTO subjects(household_id,type,name,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,'home','x',$2::uuid,'pg-replay-key',decode(repeat('aa',32),'hex'))`,
			"zero version":               `INSERT INTO subjects(household_id, type, name, version) VALUES ($1::uuid, 'home', 'x', 0)`,
			"null name":                  `INSERT INTO subjects(household_id, type, name) VALUES ($1::uuid, 'home', NULL)`,
			"orphan househ":              `INSERT INTO subjects(household_id, type, name) VALUES (uuidv7(), 'home', 'x')`,
		} {
			args := []any{}
			if !strings.Contains(name, "orphan") {
				args = append(args, homeA)
			}
			if strings.Contains(name, "creation") || strings.Contains(name, "fingerprint") {
				args = append(args, actorID)
			}
			if _, err := admin.Exec(ctx, stmt, args...); err == nil {
				t.Fatalf("%s accepted by database", name)
			} else {
				if name != "duplicate creation key" && (strings.Contains(name, "creation") || strings.Contains(name, "fingerprint")) {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
						t.Fatalf("%s SQLSTATE=%v, want 23514", name, err)
					}
					expected := map[string]string{"partial creation metadata": "subjects_creation_metadata_all_or_none", "invalid creation key": "subjects_creation_key_valid", "invalid fingerprint length": "subjects_creation_fingerprint_length"}
					if got := pgErr.ConstraintName; got != expected[name] {
						t.Fatalf("%s constraint=%s, want %s", name, got, expected[name])
					}
				}
			}
			if _, err := app.Exec(ctx, stmt, args...); err == nil {
				t.Fatalf("%s accepted for runtime role", name)
			}
		}
		for name, stmt := range map[string]string{
			"household_id":                 `UPDATE subjects SET household_id=$2::uuid WHERE id=$1::uuid`,
			"created_at":                   `UPDATE subjects SET created_at=now() WHERE id=$1::uuid`,
			"creation_key":                 `UPDATE subjects SET creation_key='changed' WHERE id=$1::uuid`,
			"created_by_user_id":           `UPDATE subjects SET created_by_user_id=$2::uuid WHERE id=$1::uuid`,
			"creation_request_fingerprint": `UPDATE subjects SET creation_request_fingerprint=decode(repeat('bb',32),'hex') WHERE id=$1::uuid`,
			"id":                           `UPDATE subjects SET id=uuidv7() WHERE id=$1::uuid`,
		} {
			args := []any{s.ID}
			if name == "household_id" {
				args = append(args, homeB)
			}
			if name == "created_by_user_id" {
				args = append(args, actorID)
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
		if _, _, err := repo.CreateIdempotent(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99", actorID, "orphan-key", "orphan", subject.TypeHome, make([]byte, 32)); !errors.Is(err, subject.ErrNotFound) {
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

func TestMigrationUpgradeFromVersionElevenPreservesSubjectRows(t *testing.T) {
	appURL, adminURL := urls(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
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
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var hid, legacyID string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('Upgrade 11','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','Legacy') RETURNING id::text`, hid).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `ALTER TABLE subjects DROP CONSTRAINT subjects_household_creator_creation_key_unique, DROP CONSTRAINT subjects_creation_fingerprint_length, DROP CONSTRAINT subjects_creation_key_valid, DROP CONSTRAINT subjects_creation_metadata_all_or_none; ALTER TABLE subjects DROP COLUMN creation_request_fingerprint, DROP COLUMN creation_key, DROP COLUMN created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (12,13)`); err != nil {
		t.Fatal(err)
	}
	if err = database.ValidateSchema(ctx, upApp); err == nil {
		t.Fatal("version 11 schema accepted by version 13 application")
	}
	var legacyName string
	if err = upAdmin.QueryRow(ctx, `SELECT name FROM subjects WHERE id=$1::uuid`, legacyID).Scan(&legacyName); err != nil || legacyName != "Legacy" {
		t.Fatalf("legacy row name=%q err=%v", legacyName, err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var metadata int
	if err = upAdmin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE id=$1::uuid AND name='Legacy' AND created_by_user_id IS NULL AND creation_key IS NULL AND creation_request_fingerprint IS NULL`, legacyID).Scan(&metadata); err != nil || metadata != 1 {
		t.Fatalf("legacy metadata rows=%d err=%v", metadata, err)
	}
}

func TestMigrationUpgradeFromVersionThreePreservesData(t *testing.T) {
	appURL, adminURL := urls(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
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
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	// Rewind to the exact version 3 schema, then add pre-existing data.
	if _, err := upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; DROP TABLE items; DROP TABLE subjects; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (4,5,6,7,8,9,10,11,12,13)`); err != nil {
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
		t.Fatal("version 3 database accepted by version 13 application")
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
	var upgradeActor string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('upgrade_subject_actor') RETURNING id::text`).Scan(&upgradeActor); err != nil {
		t.Fatal(err)
	}
	created, err := subject.NewService(repo, func(context.Context, string, string) error { return nil }).Create(ctx, upgradeActor, hid, "upgrade-create-key", "After upgrade", subject.TypePerson)
	if err != nil || created.Version != 1 {
		t.Fatalf("create after upgrade: %+v err=%v", created, err)
	}
	if _, err := upApp.Exec(ctx, `DELETE FROM subjects`); err == nil {
		t.Fatal("runtime role can delete subjects after upgrade")
	}
	var max int64
	var dirty bool
	if err := upAdmin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 13 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
