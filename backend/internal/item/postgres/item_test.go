package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/subject"
	subjectpg "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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
func TestMigrationUpgradeFromV9ToV10PreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
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
	var hid, owner, member, sid, iid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('v9 upgrade','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ login, role string }{{"v9_owner", "owner"}, {"v9_member", "member"}} {
		var uid string
		if err = upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, tc.login).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if _, err = upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,$3)`, uid, hid, tc.role); err != nil {
			t.Fatal(err)
		}
		if tc.role == "owner" {
			owner = uid
		} else {
			member = uid
		}
	}
	if _, err = upAdmin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, owner, hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','v9 subject') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,version,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES($1::uuid,$2::uuid,'v9 item',7,1,'year','fixed') RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-10-07',$3::uuid,'waiting',6,'v9-receipt',decode(repeat('ab',32),'hex'))`, hid, iid, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; REVOKE UPDATE (responsible_user_id) ON items FROM tendo; ALTER TABLE items DROP CONSTRAINT items_responsible_membership_fk; DROP INDEX items_responsible_membership_idx; ALTER TABLE items DROP COLUMN responsible_user_id; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (10,11,12,13)`); err != nil {
		t.Fatal(err)
	}
	if err = database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("unmigrated v9 schema accepted")
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var title string
	var version int64
	var responsible pgtype.UUID
	if err = upAdmin.QueryRow(ctx, `SELECT title,version,responsible_user_id FROM items WHERE id=$1::uuid`, iid).Scan(&title, &version, &responsible); err != nil {
		t.Fatal(err)
	}
	if title != "v9 item" || version != 7 || responsible.Valid {
		t.Fatalf("item after migration=%q version=%d responsible=%+v", title, version, responsible)
	}
	var receiptCount int
	var completed time.Time
	if err = upAdmin.QueryRow(ctx, `SELECT count(*),min(completed_on) FROM item_completions WHERE item_id=$1::uuid`, iid).Scan(&receiptCount, &completed); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 || completed.Format("2006-01-02") != "2026-10-07" {
		t.Fatalf("receipts=%d completed=%v", receiptCount, completed)
	}
	if _, err = upApp.Exec(ctx, `UPDATE items SET responsible_user_id=$2::uuid WHERE id=$1::uuid`, iid, member); err != nil {
		t.Fatalf("runtime assignment after migration: %v", err)
	}
	var assigned string
	if err = upAdmin.QueryRow(ctx, `SELECT responsible_user_id::text FROM items WHERE id=$1::uuid`, iid).Scan(&assigned); err != nil || assigned != member {
		t.Fatalf("assigned=%s err=%v", assigned, err)
	}
	if err = database.ValidateSchema(ctx, upAdmin); err != nil {
		t.Fatalf("schema v10 invalid: %v", err)
	}
}

func TestMigrationUpgradeFromV6PreservesData(t *testing.T) {
	_, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (7,8,9,10,11,12,13)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, iid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('Upgrade v6','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','upgrade') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'legacy') RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var done bool
	if err = upAdmin.QueryRow(ctx, `SELECT done FROM items WHERE id=$1::uuid`, iid).Scan(&done); err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
}

func sameDate(a, b *schedule.Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

var testCreateSeq atomic.Int64

// createForTest inserts through the production idempotent path with a fresh
// per-call key, failing if the insert unexpectedly resolved to an existing row.
func createForTest(ctx context.Context, r *Repository, householdID, actorID string, d item.Draft) (item.Item, error) {
	d.ActorID = actorID
	d.CreationKey = fmt.Sprintf("repository-test-%d", testCreateSeq.Add(1))
	digest := sha256.Sum256([]byte(d.CreationKey))
	d.CreationFingerprint = digest[:]
	got, inserted, err := r.CreateIdempotent(ctx, householdID, d)
	if err == nil && !inserted {
		return item.Item{}, errors.New("unexpected creation key conflict")
	}
	return got, err
}

type itemFindBarrierRepository struct {
	item.Repository
	mu       sync.Mutex
	arrived  int
	target   int
	release  chan struct{}
	released bool
}

func newItemFindBarrierRepository(repo item.Repository, target int) *itemFindBarrierRepository {
	return &itemFindBarrierRepository{Repository: repo, target: target, release: make(chan struct{})}
}

func (r *itemFindBarrierRepository) FindCreation(ctx context.Context, householdID, actorID, key string) (item.Item, []byte, error) {
	got, fingerprint, err := r.Repository.FindCreation(ctx, householdID, actorID, key)
	if errors.Is(err, item.ErrNotFound) {
		r.mu.Lock()
		if r.released {
			r.mu.Unlock()
			return got, fingerprint, err
		}
		r.arrived++
		if r.arrived == r.target {
			r.released = true
			close(r.release)
		}
		r.mu.Unlock()
		select {
		case <-r.release:
		case <-ctx.Done():
			return item.Item{}, nil, ctx.Err()
		}
	}
	return got, fingerprint, err
}

func TestMigrationUpgradeFromVersionTwelvePreservesNullItemMetadata(t *testing.T) {
	_, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `ALTER TABLE items DROP CONSTRAINT items_household_creator_creation_key_unique, DROP CONSTRAINT items_creation_fingerprint_length, DROP CONSTRAINT items_creation_key_valid, DROP CONSTRAINT items_creation_metadata_all_or_none; ALTER TABLE items DROP COLUMN creation_request_fingerprint, DROP COLUMN creation_key, DROP COLUMN created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version=13`); err != nil {
		t.Fatal(err)
	}
	var hid, uid, sid, iid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('upgrade v12 item','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('upgrade_v12_item_actor') RETURNING id::text`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, uid, hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','legacy') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,version) VALUES($1::uuid,$2::uuid,'legacy v12',7) RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err = database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("version 12 schema accepted")
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var creator, key pgtype.Text
	var fingerprint []byte
	var max int64
	var dirty bool
	if err = upAdmin.QueryRow(ctx, `SELECT created_by_user_id::text,creation_key,creation_request_fingerprint FROM items WHERE id=$1::uuid`, iid).Scan(&creator, &key, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if creator.Valid || key.Valid || fingerprint != nil {
		t.Fatalf("legacy metadata=%+v %+v %x", creator, key, fingerprint)
	}
	if err = upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 13 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
	got, err := NewRepository(upAdmin).Get(ctx, hid, iid)
	if err != nil || got.Version != 7 || got.Title != "legacy v12" {
		t.Fatalf("legacy item=%+v err=%v", got, err)
	}
}

const itemIDFloor = "00000000-0000-0000-0000-000000000000"

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
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil || role != testpostgres.MustRuntimeRole(t) {
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
	var completionUser string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('item_completion_actor') RETURNING id::text`).Scan(&completionUser); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member'),($1::uuid,$3::uuid,'member')`, completionUser, hA, hB); err != nil {
		t.Fatal(err)
	}
	sA, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }).Create(ctx, completionUser, hA, "item-person-a", "Person A", subject.TypePerson)
	if err != nil {
		t.Fatal(err)
	}
	sB, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }).Create(ctx, completionUser, hB, "item-person-b", "Person B", subject.TypePerson)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)
	clock := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	houses := func(_ context.Context, userID, householdID string) (string, error) {
		if userID != completionUser || householdID != hA {
			return "", item.ErrNotFound
		}
		return "Europe/Prague", nil
	}
	subjects := func(_ context.Context, userID, householdID, subjectID string) (bool, error) {
		if userID != completionUser || householdID != hA || subjectID != sA.ID {
			return false, item.ErrNotFound
		}
		return false, nil
	}
	members := func(ctx context.Context, candidate, household string) error {
		var role string
		err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, candidate, household).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return item.ErrNotFound
		}
		if err == nil && role != "owner" && role != "member" {
			return item.ErrNotFound
		}
		return err
	}
	service := item.NewService(repo, houses, subjects, members, func() time.Time { return clock })
	assignedUser := func(hid, login, role string) string {
		t.Helper()
		var uid string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,$3)`, uid, hid, role); err != nil {
			t.Fatal(err)
		}
		return uid
	}
	create := func(hid, sid, title string, notes *string, attention *schedule.Date) item.Item {
		t.Helper()
		got, err := createForTest(ctx, repo, hid, completionUser, item.Draft{SubjectID: sid, Title: title, Notes: notes, AttentionOn: attention})
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
	t.Run("idempotent repository creation and key scope", func(t *testing.T) {
		key := "pg-item-repository-idempotency"
		fingerprint := []byte(strings.Repeat("a", 32))
		draft := item.Draft{ActorID: completionUser, CreationKey: key, CreationFingerprint: fingerprint, SubjectID: sA.ID, Title: "repository idem first"}
		before := count(hA)
		first, inserted, err := repo.CreateIdempotent(ctx, hA, draft)
		if err != nil || !inserted {
			t.Fatalf("first=%+v inserted=%v err=%v", first, inserted, err)
		}
		different := draft
		different.Title = "ignored different body"
		different.CreationFingerprint = []byte(strings.Repeat("b", 32))
		second, inserted, err := repo.CreateIdempotent(ctx, hA, different)
		if err != nil || inserted {
			t.Fatalf("second=%+v inserted=%v err=%v", second, inserted, err)
		}
		if count(hA) != before+1 {
			t.Fatalf("count=%d before=%d", count(hA), before)
		}
		changedTitle := "changed after create"
		current, err := repo.Update(ctx, hA, first.ID, first.Version, item.Change{Title: &changedTitle})
		if err != nil {
			t.Fatal(err)
		}
		found, storedFingerprint, err := repo.FindCreation(ctx, hA, completionUser, key)
		if err != nil || found.ID != first.ID || found.Title != changedTitle || found.Version != 2 || current.Version != 2 || string(storedFingerprint) != string(fingerprint) {
			t.Fatalf("found=%+v fp=%x err=%v", found, storedFingerprint, err)
		}
		otherActor := assignedUser(hA, "item_idem_other_actor", "member")
		actorDraft := draft
		actorDraft.ActorID = otherActor
		independent, inserted, err := repo.CreateIdempotent(ctx, hA, actorDraft)
		if err != nil || !inserted || independent.ID == first.ID {
			t.Fatalf("other actor=%+v inserted=%v err=%v", independent, inserted, err)
		}
		houseDraft := draft
		houseDraft.SubjectID = sB.ID
		independent, inserted, err = repo.CreateIdempotent(ctx, hB, houseDraft)
		if err != nil || !inserted || independent.ID == first.ID {
			t.Fatalf("other household=%+v inserted=%v err=%v", independent, inserted, err)
		}
	})

	t.Run("same-key create concurrency reaches insert conflict", func(t *testing.T) {
		const workers = 8
		t.Run("identical bodies", func(t *testing.T) {
			key := "pg-item-race-identical"
			barrier := newItemFindBarrierRepository(repo, workers)
			racing := item.NewService(barrier, houses, subjects, members, func() time.Time { return clock })
			start := make(chan struct{})
			type result struct {
				i   item.Item
				err error
			}
			results := make(chan result, workers)
			for n := 0; n < workers; n++ {
				go func() {
					<-start
					created, e := racing.Create(ctx, completionUser, hA, key, item.NewItem{SubjectID: sA.ID, Title: "race identical"})
					results <- result{created, e}
				}()
			}
			close(start)
			var id string
			success := 0
			for n := 0; n < workers; n++ {
				r := <-results
				if r.err != nil {
					t.Fatalf("create: %v", r.err)
				}
				if id == "" {
					id = r.i.ID
				}
				if r.i.ID != id {
					t.Fatalf("IDs %s != %s", r.i.ID, id)
				}
				success++
			}
			var rows int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND created_by_user_id=$2::uuid AND creation_key=$3`, hA, completionUser, key).Scan(&rows); err != nil || success != workers || rows != 1 {
				t.Fatalf("success=%d rows=%d err=%v", success, rows, err)
			}
		})
		t.Run("different bodies", func(t *testing.T) {
			key := "pg-item-race-different"
			barrier := newItemFindBarrierRepository(repo, workers)
			racing := item.NewService(barrier, houses, subjects, members, func() time.Time { return clock })
			start := make(chan struct{})
			results := make(chan error, workers)
			for n := 0; n < workers; n++ {
				title := fmt.Sprintf("race intent %d", n)
				go func() {
					<-start
					_, e := racing.Create(ctx, completionUser, hA, key, item.NewItem{SubjectID: sA.ID, Title: title})
					results <- e
				}()
			}
			close(start)
			success, reused := 0, 0
			for n := 0; n < workers; n++ {
				switch e := <-results; e {
				case nil:
					success++
				case item.ErrIdempotencyKeyReused:
					reused++
				default:
					t.Fatalf("unexpected create result: %v", e)
				}
			}
			var rows int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND created_by_user_id=$2::uuid AND creation_key=$3`, hA, completionUser, key).Scan(&rows); err != nil || success != 1 || reused != workers-1 || rows != 1 {
				t.Fatalf("success=%d reused=%d rows=%d err=%v", success, reused, rows, err)
			}
		})
	})

	t.Run("historical initialization create concurrency writes one receipt", func(t *testing.T) {
		const workers = 8
		key := "pg-item-race-historical"
		dateText := "2025-10-31"
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
		barrier := newItemFindBarrierRepository(repo, workers)
		racing := item.NewService(barrier, houses, subjects, members, func() time.Time { return clock })
		start := make(chan struct{})
		type result struct {
			i   item.Item
			err error
		}
		results := make(chan result, workers)
		for n := 0; n < workers; n++ {
			go func() {
				<-start
				created, e := racing.Create(ctx, completionUser, hA, key, item.NewItem{SubjectID: sA.ID, Title: "race historical", HistoricalCompletedOn: &dateText, Recurrence: policy})
				results <- result{created, e}
			}()
		}
		close(start)
		id := ""
		for n := 0; n < workers; n++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("create: %v", r.err)
			}
			if id == "" {
				id = r.i.ID
			}
			if id != r.i.ID || r.i.Version != 2 {
				t.Fatalf("result=%+v want id=%s version 2", r.i, id)
			}
		}
		var rows, receipts int
		var version int64
		if err := admin.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM item_completions WHERE item_id=$1::uuid),max(version) FROM items WHERE id=$1::uuid`, id).Scan(&rows, &receipts, &version); err != nil || rows != 1 || receipts != 1 || version != 2 {
			t.Fatalf("rows=%d receipts=%d version=%d err=%v", rows, receipts, version, err)
		}
	})

	t.Run("historical init replay after receipt undo does not recreate receipt", func(t *testing.T) {
		key := "pg-item-init-undo-replay"
		dateText := "2025-10-31"
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
		body := item.NewItem{SubjectID: sA.ID, Title: "undo init replay", HistoricalCompletedOn: &dateText, Recurrence: policy}
		created, err := service.Create(ctx, completionUser, hA, key, body)
		if err != nil {
			t.Fatal(err)
		}
		history, err := repo.ListCompletions(ctx, hA, created.ID, itemIDFloor, 10)
		if err != nil || len(history) != 1 {
			t.Fatalf("history=%+v err=%v", history, err)
		}
		if _, err = service.UndoCompletion(ctx, completionUser, hA, created.ID, history[0].ID, created.Version); err != nil {
			t.Fatal(err)
		}
		afterUndo, err := repo.Get(ctx, hA, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := service.Create(ctx, completionUser, hA, key, body)
		if err != nil || replayed.ID != created.ID || replayed.Version != afterUndo.Version || !sameDate(replayed.AttentionOn, afterUndo.AttentionOn) {
			t.Fatalf("replay=%+v after undo=%+v err=%v", replayed, afterUndo, err)
		}
		history, err = repo.ListCompletions(ctx, hA, created.ID, itemIDFloor, 10)
		if err != nil || len(history) != 1 || history[0].UndoneAt == nil {
			t.Fatalf("receipt history=%+v err=%v", history, err)
		}
	})

	t.Run("creation metadata constraints and runtime immutability", func(t *testing.T) {
		baseDraft := item.Draft{ActorID: completionUser, CreationKey: "pg-constraint-key", CreationFingerprint: []byte(strings.Repeat("c", 32)), SubjectID: sA.ID, Title: "constraint item"}
		created, inserted, err := repo.CreateIdempotent(ctx, hA, baseDraft)
		if err != nil || !inserted {
			t.Fatalf("create=%+v err=%v", created, err)
		}
		assertConstraint := func(name, sql string, args ...any) {
			t.Helper()
			_, e := admin.Exec(ctx, sql, args...)
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != name {
				t.Fatalf("constraint=%s err=%v pg=%+v", name, e, pg)
			}
		}
		assertConstraint("items_creation_metadata_all_or_none", `INSERT INTO items(household_id,subject_id,title,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'bad',$3::uuid,NULL,NULL)`, hA, sA.ID, completionUser)
		assertConstraint("items_creation_key_valid", `INSERT INTO items(household_id,subject_id,title,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'bad',$3::uuid,'bad key',decode(repeat('ab',32),'hex'))`, hA, sA.ID, completionUser)
		assertConstraint("items_creation_key_valid", `INSERT INTO items(household_id,subject_id,title,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'bad',$3::uuid,$4,decode(repeat('ab',32),'hex'))`, hA, sA.ID, completionUser, strings.Repeat("x", 129))
		assertConstraint("items_creation_fingerprint_length", `INSERT INTO items(household_id,subject_id,title,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'bad',$3::uuid,'bad-fingerprint',decode(repeat('ab',31),'hex'))`, hA, sA.ID, completionUser)
		_, e := admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,title,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'dup',$3::uuid,$4,decode(repeat('ab',32),'hex'))`, hA, sA.ID, completionUser, baseDraft.CreationKey)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23505" || pg.ConstraintName != "items_household_creator_creation_key_unique" {
			t.Fatalf("unique constraint err=%v pg=%+v", e, pg)
		}
		for _, column := range []string{"creation_key", "created_by_user_id", "creation_request_fingerprint"} {
			_, e = app.Exec(ctx, "UPDATE items SET "+column+" = "+map[string]string{"creation_key": "'x'", "created_by_user_id": "'" + completionUser + "'::uuid", "creation_request_fingerprint": "decode(repeat('ab',32),'hex')"}[column]+" WHERE id=$1::uuid", created.ID)
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "42501" {
				t.Fatalf("runtime UPDATE %s err=%v pg=%+v", column, e, pg)
			}
		}
	})

	t.Run("responsible assignment list and FK precheck race", func(t *testing.T) {
		memberA := assignedUser(hA, "responsible_list_member", "member")
		memberB := assignedUser(hB, "responsible_list_foreign", "member")
		before := count(hA)
		assigned, err := service.Create(ctx, completionUser, hA, "responsible-list-create", item.NewItem{SubjectID: sA.ID, ResponsibleUserID: &memberA, Title: "list responsible item"})
		if err != nil || assigned.ResponsibleUserID == nil || *assigned.ResponsibleUserID != memberA {
			t.Fatalf("assigned=%+v err=%v", assigned, err)
		}
		listed, err := repo.List(ctx, hA, false, false, itemIDFloor, 100)
		if err != nil {
			t.Fatal(err)
		}
		listedItem := false
		for _, row := range listed {
			if row.ID == assigned.ID {
				listedItem = row.ResponsibleUserID != nil && *row.ResponsibleUserID == memberA
			}
		}
		if !listedItem {
			t.Fatalf("list did not return responsibility for %s: %+v", assigned.ID, listed)
		}
		var validation *item.ValidationError
		raceCreateService := item.NewService(repo, houses, subjects, func(context.Context, string, string) error { return nil }, func() time.Time { return clock })
		raceCreated, err := raceCreateService.Create(ctx, completionUser, hA, "create-precheck-race", item.NewItem{SubjectID: sA.ID, ResponsibleUserID: &memberB, Title: "create precheck race"})
		if !errors.As(err, &validation) || validation.Field != "responsibleUserId" || validation.Code != "invalid_reference" || raceCreated.ID != "" {
			t.Fatalf("race create=%+v err=%v", raceCreated, err)
		}
		if count(hA) != before+1 {
			t.Fatalf("race create changed count: before=%d after=%d", before, count(hA))
		}
		raceUpdateService := item.NewService(repo, houses, subjects, func(context.Context, string, string) error { return nil }, func() time.Time { return clock })
		failedUpdate, err := raceUpdateService.Update(ctx, completionUser, hA, assigned.ID, assigned.Version, item.Patch{ResponsibleUserID: item.Some(memberB)})
		if !errors.As(err, &validation) || validation.Field != "responsibleUserId" || validation.Code != "invalid_reference" || failedUpdate.Version != 0 {
			t.Fatalf("race update=%+v err=%v", failedUpdate, err)
		}
		unchanged, err := repo.Get(ctx, hA, assigned.ID)
		if err != nil || unchanged.ResponsibleUserID == nil || *unchanged.ResponsibleUserID != memberA || unchanged.Version != assigned.Version {
			t.Fatalf("failed update changed item=%+v err=%v", unchanged, err)
		}
	})
	t.Run("create persists Unicode fields", func(t *testing.T) {
		notes := "  notes\n家族 🚗  "
		day, _ := schedule.NewDate(2030, time.January, 2)
		got := create(hA, sA.ID, "Veselí 家族 🚗", &notes, &day)
		if got.Title != "Veselí 家族 🚗" || got.Notes == nil || *got.Notes != notes || got.AttentionOn == nil || *got.AttentionOn != day || got.Version != 1 || got.WorkflowState != item.StateOpen || got.HouseholdID != hA {
			t.Fatalf("item=%+v", got)
		}
		var receipts int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, got.ID).Scan(&receipts); err != nil || receipts != 0 {
			t.Fatalf("ordinary create receipts=%d err=%v", receipts, err)
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
	t.Run("completion preserves assignment, undo preserves intervening reassignment", func(t *testing.T) {
		responsibleA := assignedUser(hA, "completion_responsible_a", "member")
		responsibleB := assignedUser(hA, "completion_responsible_b", "owner")
		created, err := service.Create(ctx, completionUser, hA, "responsibility-create-key", item.NewItem{SubjectID: sA.ID, ResponsibleUserID: &responsibleA, Title: "completion responsibility"})
		if err != nil {
			t.Fatal(err)
		}
		receipt, _, err := service.Complete(ctx, completionUser, hA, created.ID, created.Version, "responsible-completion-test", item.CompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		immediate, err := repo.Get(ctx, hA, created.ID)
		if err != nil || immediate.ResponsibleUserID == nil || *immediate.ResponsibleUserID != responsibleA {
			t.Fatalf("completion changed responsibility=%+v err=%v", immediate.ResponsibleUserID, err)
		}
		changed, err := service.Update(ctx, completionUser, hA, created.ID, immediate.Version, item.Patch{ResponsibleUserID: item.Some(responsibleB)})
		if err != nil || changed.ResponsibleUserID == nil || *changed.ResponsibleUserID != responsibleB {
			t.Fatalf("reassignment=%+v err=%v", changed.ResponsibleUserID, err)
		}
		if _, err = service.UndoCompletion(ctx, completionUser, hA, created.ID, receipt.ID, changed.Version); err != nil {
			t.Fatal(err)
		}
		afterUndo, err := repo.Get(ctx, hA, created.ID)
		if err != nil || afterUndo.ResponsibleUserID == nil || *afterUndo.ResponsibleUserID != responsibleB {
			t.Fatalf("undo changed current responsibility=%+v err=%v", afterUndo.ResponsibleUserID, err)
		}
	})
	t.Run("historical initialization atomically writes v2 item and receipt", func(t *testing.T) {
		completed, _ := schedule.NewDate(2026, time.January, 31)
		next, _ := schedule.NewDate(2026, time.February, 28)
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
		draft := item.Draft{SubjectID: sA.ID, Title: "historically initialized", Recurrence: policy, AttentionOn: &next, InitializationReceipt: &item.Completion{CompletedOn: completed, CompletedByUserID: completionUser, Recurrence: policy, PriorWorkflowState: item.StateOpen, NextAttentionOn: &next, ItemVersionBefore: 1, Fingerprint: item.CompletionFingerprint(completionUser, func() *string { value := "2026-01-31"; return &value }())}}
		got, err := createForTest(ctx, repo, hA, completionUser, draft)
		if err != nil || got.Version != 2 || got.AttentionOn == nil || *got.AttentionOn != next || got.LastCompletedOn == nil || *got.LastCompletedOn != completed {
			t.Fatalf("item=%+v err=%v", got, err)
		}
		var version int64
		var attention, completedDB time.Time
		var receiptCount int
		var cycle pgtype.Date
		var before int64
		var interval int
		if err = admin.QueryRow(ctx, `SELECT version,attention_on,(SELECT count(*) FROM item_completions WHERE item_id=items.id),(SELECT completed_on FROM item_completions WHERE item_id=items.id),(SELECT cycle_attention_on FROM item_completions WHERE item_id=items.id),(SELECT item_version_before FROM item_completions WHERE item_id=items.id),(SELECT recurrence_interval_value FROM item_completions WHERE item_id=items.id) FROM items WHERE id=$1::uuid`, got.ID).Scan(&version, &attention, &receiptCount, &completedDB, &cycle, &before, &interval); err != nil {
			t.Fatal(err)
		}
		if version != 2 || attention.Format("2006-01-02") != "2026-02-28" || receiptCount != 1 || completedDB.Format("2006-01-02") != "2026-01-31" || cycle.Valid || before != 1 || interval != 1 {
			t.Fatalf("version=%d attention=%v count=%d completed=%v cycle=%+v before=%d interval=%d", version, attention, receiptCount, completedDB, cycle, before, interval)
		}
		read, err := repo.Get(ctx, hA, got.ID)
		if err != nil || read.LastCompletedOn == nil || *read.LastCompletedOn != completed {
			t.Fatalf("read=%+v err=%v", read, err)
		}
		var key, actor string
		var fingerprint []byte
		if err = admin.QueryRow(ctx, `SELECT idempotency_key,completed_by_user_id::text,request_fingerprint FROM item_completions WHERE item_id=$1::uuid`, got.ID).Scan(&key, &actor, &fingerprint); err != nil {
			t.Fatal(err)
		}
		var ordinaryReceipts int
		ordinary := create(hA, sA.ID, "ordinary initialized-none", nil, nil)
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, ordinary.ID).Scan(&ordinaryReceipts); err != nil || ordinary.Version != 1 || ordinaryReceipts != 0 {
			t.Fatalf("ordinary create item=%+v receipts=%d err=%v", ordinary, ordinaryReceipts, err)
		}
		_, validUUID := parseUUID(key)
		if !validUUID || actor != completionUser || string(fingerprint) != string(draft.InitializationReceipt.Fingerprint[:]) {
			t.Fatalf("key=%s actor=%s fingerprint=%x", key, actor, fingerprint)
		}
		policy2 := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}
		updated, err := repo.Update(ctx, hA, got.ID, 2, item.Change{Recurrence: item.Some(policy2)})
		if err != nil || updated.Recurrence == nil || *updated.Recurrence != policy2 {
			t.Fatalf("policy update=%+v err=%v", updated, err)
		}
		storedHistory, err := repo.ListCompletions(ctx, hA, got.ID, "00000000-0000-0000-0000-000000000000", 10)
		if err != nil || len(storedHistory) != 1 || storedHistory[0].Recurrence == nil || *storedHistory[0].Recurrence != *policy {
			t.Fatalf("policy edit mutated receipt=%+v err=%v", storedHistory, err)
		}
	})

	t.Run("service creates, completes, and undoes historical initialization in both modes", func(t *testing.T) {
		for _, mode := range []schedule.Mode{schedule.ModeFixed, schedule.ModeAfterCompletion} {
			t.Run(string(mode), func(t *testing.T) {
				completedText := "2025-10-31"
				completed, _ := schedule.NewDate(2025, time.October, 31)
				initialNext, _ := schedule.NewDate(2025, time.November, 30)
				policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: mode}
				created, err := service.Create(ctx, completionUser, hA, "service-historical-"+string(mode), item.NewItem{SubjectID: sA.ID, Title: "service historical " + string(mode), HistoricalCompletedOn: &completedText, Recurrence: &policy})
				if err != nil || created.Version != 2 || created.AttentionOn == nil || *created.AttentionOn != initialNext || created.LastCompletedOn == nil || *created.LastCompletedOn != completed {
					t.Fatalf("created=%+v err=%v", created, err)
				}
				var receiptID, actor, fingerprint, intervalValue, intervalUnit, recurrenceMode, cycleAnchor, nextStored string
				var versionBefore int64
				if err = admin.QueryRow(ctx, `SELECT id::text,completed_by_user_id::text,encode(request_fingerprint,'hex'),recurrence_interval_value::text,recurrence_interval_unit,recurrence_mode,COALESCE(cycle_attention_on::text,'NULL'),next_attention_on::text,item_version_before FROM item_completions WHERE item_id=$1::uuid`, created.ID).Scan(&receiptID, &actor, &fingerprint, &intervalValue, &intervalUnit, &recurrenceMode, &cycleAnchor, &nextStored, &versionBefore); err != nil {
					t.Fatal(err)
				}
				wantFingerprint := item.CompletionFingerprint(completionUser, &completedText)
				if actor != completionUser || fingerprint != fmt.Sprintf("%x", wantFingerprint) || intervalValue != "1" || intervalUnit != "month" || recurrenceMode != string(mode) || cycleAnchor != "NULL" || nextStored != "2025-11-30" || versionBefore != 1 {
					t.Fatalf("receipt actor=%s fingerprint=%s policy=%s/%s/%s anchor=%s next=%s before=%d", actor, fingerprint, intervalValue, intervalUnit, recurrenceMode, cycleAnchor, nextStored, versionBefore)
				}
				actual, _, err := service.Complete(ctx, completionUser, hA, created.ID, created.Version, "historical-chain-"+string(mode), item.CompletionRequest{})
				if err != nil {
					t.Fatal(err)
				}
				wantNext := "2026-01-30"
				if mode == schedule.ModeAfterCompletion {
					wantNext = "2026-02-15"
				}
				advanced, err := repo.Get(ctx, hA, created.ID)
				if err != nil || advanced.AttentionOn == nil || advanced.AttentionOn.String() != wantNext || advanced.Version != 3 {
					t.Fatalf("advanced=%+v err=%v want %s", advanced, err, wantNext)
				}
				if _, err = service.UndoCompletion(ctx, completionUser, hA, created.ID, actual.ID, advanced.Version); err != nil {
					t.Fatal(err)
				}
				restored, err := repo.Get(ctx, hA, created.ID)
				if err != nil || restored.AttentionOn == nil || restored.AttentionOn.String() != "2025-11-30" || restored.LastCompletedOn == nil || *restored.LastCompletedOn != completed || restored.Version != 4 {
					t.Fatalf("restored=%+v err=%v", restored, err)
				}
				rows, err := service.ListCompletions(ctx, completionUser, hA, created.ID, 10, "")
				if err != nil || len(rows.Items) != 2 {
					t.Fatalf("after completion undo history=%+v err=%v", rows, err)
				}
				for _, row := range rows.Items {
					if row.ID == actual.ID && row.UndoneAt == nil {
						t.Fatalf("completion receipt remains active: %+v", row)
					}
				}
				if _, err = service.UndoCompletion(ctx, completionUser, hA, created.ID, receiptID, restored.Version); err != nil {
					t.Fatal(err)
				}
				final, err := repo.Get(ctx, hA, created.ID)
				if err != nil || final.AttentionOn != nil || final.LastCompletedOn != nil || final.WorkflowState != item.StateOpen || final.Done || final.Recurrence == nil || *final.Recurrence != policy || final.Version != 5 {
					t.Fatalf("final=%+v err=%v", final, err)
				}
				rows, err = service.ListCompletions(ctx, completionUser, hA, created.ID, 10, "")
				if err != nil || len(rows.Items) != 2 {
					t.Fatalf("final receipts=%+v err=%v", rows, err)
				}
				for _, row := range rows.Items {
					if row.UndoneAt == nil {
						t.Fatalf("receipt not marked undone: %+v", row)
					}
				}
			})
		}
	})

	t.Run("fixed completion after several missed anchors keeps one backlog item and one receipt", func(t *testing.T) {
		// Household today is 2026-01-15 (Prague). Anchors 2025-10-10, 2025-11-10, 2025-12-10 and
		// 2026-01-10 were all missed; the next cycle must be the first future anchor 2026-02-10.
		title := "fixed catch-up after missed anchors"
		anchor := "2025-10-10"
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
		created, err := service.Create(ctx, completionUser, hA, "fixed-catch-up", item.NewItem{SubjectID: sA.ID, Title: title, AttentionOn: &anchor, Recurrence: &policy})
		if err != nil {
			t.Fatal(err)
		}
		receipt, replayed, err := service.Complete(ctx, completionUser, hA, created.ID, created.Version, "fixed-catch-up-done", item.CompletionRequest{})
		if err != nil || replayed {
			t.Fatalf("complete=%+v replayed=%v err=%v", receipt, replayed, err)
		}
		if receipt.CompletedOn.String() != "2026-01-15" || receipt.CycleAttentionOn == nil || receipt.CycleAttentionOn.String() != anchor || receipt.NextAttentionOn == nil || receipt.NextAttentionOn.String() != "2026-02-10" {
			t.Fatalf("receipt=%+v", receipt)
		}
		var rows, active int
		var stored string
		if err = admin.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT done AND NOT archived), max(attention_on::text) FROM items WHERE household_id=$1::uuid AND title=$2`, hA, title).Scan(&rows, &active, &stored); err != nil {
			t.Fatal(err)
		}
		if rows != 1 || active != 1 || stored != "2026-02-10" {
			t.Fatalf("items rows=%d active=%d attention_on=%s", rows, active, stored)
		}
		var receipts int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, created.ID).Scan(&receipts); err != nil || receipts != 1 {
			t.Fatalf("receipts=%d err=%v", receipts, err)
		}
		listed := 0
		for q := (item.ListQuery{Limit: 100}); ; {
			page, err := service.List(ctx, completionUser, hA, q)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range page.Items {
				if it.Title == title {
					listed++
					if it.ID != created.ID || it.AttentionOn == nil || it.AttentionOn.String() != "2026-02-10" || it.Done {
						t.Fatalf("listed=%+v", it)
					}
				}
			}
			if page.NextCursor == nil {
				break
			}
			q.Cursor = *page.NextCursor
		}
		if listed != 1 {
			t.Fatalf("active backlog lists item %d times", listed)
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
		var pgActor string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('item_pages_actor') RETURNING id::text`).Scan(&pgActor); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, pgActor, h); err != nil {
			t.Fatal(err)
		}
		s, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }).Create(ctx, pgActor, h, "pages-subject", "subject", subject.TypePerson)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for i := 0; i < 7; i++ {
			ids = append(ids, create(h, s.ID, "row "+string(rune('a'+i)), nil, nil).ID)
		}
		var got []string
		after := "00000000-0000-0000-0000-000000000000"
		for {
			rows, err := repo.List(ctx, h, false, false, after, 3)
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
		active, err := repo.List(ctx, h, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(active) != 6 {
			t.Fatalf("active=%d err=%v", len(active), err)
		}
		archived, err := repo.List(ctx, h, true, false, "00000000-0000-0000-0000-000000000000", 100)
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
	t.Run("completion concurrency", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		i := create(hA, sA.ID, "completion concurrency", nil, &anchor)
		var version int64 = 1
		makeDecide := func(key string) item.CompletionDecider {
			return func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
				if existing != nil {
					return item.CompletionPlan{Receipt: *existing, Item: current}, nil
				}
				if current.Version != version {
					return item.CompletionPlan{}, item.ErrVersionMismatch
				}
				receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: key}
				change := current
				change.AttentionOn = &next
				change.Version++
				return item.CompletionPlan{Receipt: receipt, Item: change}, nil
			}
		}
		const workers = 12
		start := make(chan struct{})
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for x := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				key := "different-" + strconv.Itoa(n)
				_, _, errs[n] = repo.Complete(ctx, hA, i.ID, key, [32]byte{byte(n + 1)}, makeDecide(key))
			}(x)
		}
		close(start)
		wg.Wait()
		wins := 0
		for _, err := range errs {
			if err == nil {
				wins++
			} else if !errors.Is(err, item.ErrVersionMismatch) {
				t.Fatalf("concurrent err=%v unwrap=%v", err, errors.Unwrap(err))
			}
		}
		if wins != 1 {
			t.Fatalf("wins=%d errors=%v", wins, errs)
		}
		var rows int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("completion count=%d err=%v", rows, err)
		}
		current, _ := repo.Get(ctx, hA, i.ID)
		version = current.Version
		start2 := make(chan struct{})
		results := make([]item.Completion, workers)
		errs2 := make([]error, workers)
		for x := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start2
				results[n], _, errs2[n] = repo.Complete(ctx, hA, i.ID, "same-key", [32]byte{99}, makeDecide("same-key"))
			}(x)
		}
		close(start2)
		wg.Wait()
		id := results[0].ID
		for x := range workers {
			if errs2[x] != nil || results[x].ID != id {
				t.Fatalf("same key result %d=%+v err=%v first=%s", x, results[x], errs2[x], id)
			}
		}
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&rows); err != nil || rows != 2 {
			t.Fatalf("same-key count=%d err=%v", rows, err)
		}
	})
	t.Run("completion vs patch race", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		i := create(hA, sA.ID, "completion patch race", nil, &anchor)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var ce, pe error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, ce = repo.Complete(ctx, hA, i.ID, "race-key", [32]byte{77}, func(cur item.Item, ex *item.Completion) (item.CompletionPlan, error) {
				if ex != nil {
					return item.CompletionPlan{Receipt: *ex, Item: cur}, nil
				}
				if cur.Version != 1 {
					return item.CompletionPlan{}, item.ErrVersionMismatch
				}
				receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: cur.WorkflowState, ItemVersionBefore: 1, IdempotencyKey: "race-key"}
				updated := cur
				updated.AttentionOn = &next
				updated.Version++
				return item.CompletionPlan{Receipt: receipt, Item: updated}, nil
			})
		}()
		go func() {
			defer wg.Done()
			<-start
			title := "patched"
			_, pe = repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title})
		}()
		close(start)
		wg.Wait()
		if ce != nil && !errors.Is(ce, item.ErrVersionMismatch) {
			t.Fatalf("completion error %v", ce)
		}
		if pe != nil && !errors.Is(pe, item.ErrVersionMismatch) {
			t.Fatalf("patch error %v", pe)
		}
		if (ce == nil) == (pe == nil) {
			t.Fatalf("expected exactly one winner completion=%v patch=%v", ce, pe)
		}
		stored, err := repo.Get(ctx, hA, i.ID)
		if err != nil || stored.Version != 2 {
			t.Fatalf("state=%+v err=%v", stored, err)
		}
		var count int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if ce == nil && (count != 1 || stored.AttentionOn == nil || stored.AttentionOn.String() != "2027-10-07") {
			t.Fatalf("completion winner state=%+v receipts=%d", stored, count)
		}
		if pe == nil && (count != 0 || stored.Title != "patched" || stored.AttentionOn == nil || stored.AttentionOn.String() != "2026-10-07") {
			t.Fatalf("patch winner state=%+v receipts=%d", stored, count)
		}
	})
	t.Run("historical initialization forced rollback removes item and receipt", func(t *testing.T) {
		completed, _ := schedule.NewDate(2026, time.March, 1)
		next, _ := schedule.NewDate(2026, time.March, 2)
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
		fingerprint := [32]byte{23}
		receipt := &item.Completion{CompletedOn: completed, CompletedByUserID: completionUser, Recurrence: policy, PriorWorkflowState: item.StateOpen, NextAttentionOn: &next, ItemVersionBefore: 1, Fingerprint: fingerprint}
		completeAfterInsertHook = func() error { return errors.New("forced rollback") }
		i, err := createForTest(ctx, repo, hA, completionUser, item.Draft{SubjectID: sA.ID, Title: "rollback initialize", Recurrence: policy, AttentionOn: &next, InitializationReceipt: receipt})
		completeAfterInsertHook = nil
		if err == nil {
			t.Fatalf("create succeeded unexpectedly: %+v", i)
		}
		var items, receipts int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND title='rollback initialize'`, hA).Scan(&items); err != nil {
			t.Fatal(err)
		}
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE household_id=$1::uuid AND completed_on=$2::date`, hA, completed.String()).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if items != 0 || receipts != 0 {
			t.Fatalf("rollback left item=%d receipts=%d", items, receipts)
		}
	})

	t.Run("completion rollback after insert hook", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		i := create(hA, sA.ID, "hook rollback", nil, &anchor)
		completeAfterInsertHook = func() error { return item.ErrUnavailable }
		_, _, err := repo.Complete(ctx, hA, i.ID, "hook-rollback", [32]byte{88}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: cur.WorkflowState, ItemVersionBefore: cur.Version, IdempotencyKey: "hook-rollback"}
			changed := cur
			changed.Done = true
			changed.Version++
			return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
		})
		completeAfterInsertHook = nil
		if !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("forced failure err=%v", err)
		}
		stored, err := repo.Get(ctx, hA, i.ID)
		if err != nil || stored.Version != 1 || stored.Done {
			t.Fatalf("stored after rollback=%+v err=%v", stored, err)
		}
		var count int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("receipt after rollback=%d err=%v", count, err)
		}
	})
	t.Run("completion overflow rolls back", func(t *testing.T) {
		end, _ := schedule.NewDate(9999, time.December, 31)
		iid := create(hA, sA.ID, "overflow completion", nil, &end).ID
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		_, err := repo.Update(ctx, hA, iid, 1, item.Change{Recurrence: item.Some(policy)})
		if err != nil {
			t.Fatal(err)
		}
		before, err := repo.Get(ctx, hA, iid)
		if err != nil {
			t.Fatal(err)
		}
		var historyBefore int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, iid).Scan(&historyBefore); err != nil || historyBefore != 0 {
			t.Fatalf("pre-overflow history=%d err=%v", historyBefore, err)
		}
		houses := func(context.Context, string, string) (string, error) { return "UTC", nil }
		subjects := func(context.Context, string, string, string) (bool, error) { return false, nil }
		svc := item.NewService(repo, houses, subjects, members, func() time.Time { return time.Date(9999, time.December, 31, 12, 0, 0, 0, time.UTC) })
		_, _, err = svc.Complete(ctx, completionUser, hA, iid, before.Version, "overflow", item.CompletionRequest{})
		var validation *item.ValidationError
		if !errors.As(err, &validation) || validation.Field != "recurrence" || validation.Code != "date_overflow" {
			t.Fatalf("overflow err=%v", err)
		}
		after, err := repo.Get(ctx, hA, iid)
		if err != nil || after.Version != before.Version || after.Done != before.Done || after.AttentionOn.String() != before.AttentionOn.String() || after.Title != before.Title || after.Recurrence == nil || *after.Recurrence != *before.Recurrence {
			t.Fatalf("overflow changed item before=%+v after=%+v err=%v", before, after, err)
		}
		var n int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, iid).Scan(&n); err != nil || n != historyBefore {
			t.Fatalf("overflow receipt count=%d err=%v", n, err)
		}
	})

	t.Run("completion receipts", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		i := create(hA, sA.ID, "completion target", nil, &anchor)
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(*policy)})
		if err != nil {
			t.Fatal(err)
		}
		decide := func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
			if existing != nil {
				return item.CompletionPlan{Receipt: *existing, Item: current}, nil
			}
			receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, CycleAttentionOn: updated.AttentionOn, Recurrence: updated.Recurrence, PriorWorkflowState: updated.WorkflowState, NextAttentionOn: &next, ItemVersionBefore: updated.Version, IdempotencyKey: "same"}
			change := current
			change.AttentionOn = &next
			change.Version++
			return item.CompletionPlan{Receipt: receipt, Item: change}, nil
		}
		finger := [32]byte{1}
		first, replayed, err := repo.Complete(ctx, hA, i.ID, "same", finger, decide)
		if err != nil || replayed {
			t.Fatalf("complete=%+v replay=%v err=%v", first, replayed, err)
		}
		after, _ := repo.Get(ctx, hA, i.ID)
		if after.Version != 3 || after.AttentionOn == nil || after.AttentionOn.String() != "2027-10-07" {
			t.Fatalf("stored item=%+v", after)
		}
		replay, replayed, err := repo.Complete(ctx, hA, i.ID, "same", finger, decide)
		if err != nil || !replayed || replay.ID != first.ID {
			t.Fatalf("replay=%+v replayed=%v err=%v", replay, replayed, err)
		}
		after, _ = repo.Get(ctx, hA, i.ID)
		if after.Version != 3 || replay.CompletedOn != first.CompletedOn || replay.ID != first.ID {
			t.Fatalf("replay changed receipt or version first=%+v replay=%+v item=%+v", first, replay, after)
		}
		rows, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10)
		if err != nil || len(rows) != 1 || rows[0].Recurrence == nil || rows[0].NextAttentionOn.String() != "2027-10-07" {
			t.Fatalf("history=%+v err=%v", rows, err)
		}
		if _, err = repo.ListCompletions(ctx, hA, first.ID, "00000000-0000-0000-0000-000000000000", 10); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("foreign/nonexistent item list=%v", err)
		}
	})
	t.Run("descending completion dates preserve version-order latest", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.March, 1)
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		i := create(hA, sA.ID, "descending dates", nil, &anchor)
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(policy)})
		if err != nil {
			t.Fatal(err)
		}
		dates := []string{"2026-03-20", "2026-03-10"}
		version := updated.Version
		var lastID string
		for n := range dates {
			on, _ := schedule.NewDate(2026, time.March, 20-10*n)
			next, _ := schedule.NewDate(2027, time.March, 1)
			key := strconv.Itoa(n)
			created, _, err := repo.Complete(ctx, hA, i.ID, key, [32]byte{byte(n + 1)}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
				receipt := item.Completion{ID: "", HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: on, PriorWorkflowState: cur.WorkflowState, Recurrence: cur.Recurrence, ItemVersionBefore: cur.Version, IdempotencyKey: key, NextAttentionOn: &next}
				changed := cur
				changed.Version++
				changed.AttentionOn = &next
				return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			lastID = created.ID
			version++
		}
		got, err := repo.Get(ctx, hA, i.ID)
		if err != nil || got.LastCompletedOn == nil || got.LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("last completion=%v err=%v", got.LastCompletedOn, err)
		}
		var storedDate string
		if err = admin.QueryRow(ctx, `SELECT completed_on::text FROM item_completions WHERE id=$1::uuid`, lastID).Scan(&storedDate); err != nil || storedDate != "2026-03-10" {
			t.Fatalf("stored latest receipt date=%s err=%v", storedDate, err)
		}
		list, err := repo.List(ctx, hA, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(list) == 0 || list[len(list)-1].LastCompletedOn == nil || list[len(list)-1].LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("list latest=%+v err=%v", list, err)
		}
		title := "after dates"
		patched, err := repo.Update(ctx, hA, i.ID, version, item.Change{Title: &title})
		if err != nil || patched.LastCompletedOn == nil || patched.LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("patch latest=%+v err=%v", patched, err)
		}
	})
	t.Run("foreign household completion is not found", func(t *testing.T) {
		i := create(hB, sB.ID, "foreign completion", nil, nil)
		decide := func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			return item.CompletionPlan{}, errors.New("decide should not be called")
		}
		if _, _, err := repo.Complete(ctx, hA, i.ID, "foreign", [32]byte{1}, decide); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("complete err=%v", err)
		}
		if _, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("list err=%v", err)
		}
		stored, err := repo.Get(ctx, hB, i.ID)
		if err != nil || stored.Version != 1 || stored.Done {
			t.Fatalf("foreign item mutated: %+v err=%v", stored, err)
		}
		var n int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("foreign receipts=%d err=%v", n, err)
		}
	})
	t.Run("household isolation and foreign subject rejected", func(t *testing.T) {
		i := create(hB, sB.ID, "private", nil, nil)
		beforeItem, err := repo.Get(ctx, hB, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		var beforeCount int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&beforeCount); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx, hA, i.ID); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("get=%v", err)
		}
		title := "stolen"
		if _, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("update=%v", err)
		}
		rows, err := repo.List(ctx, hA, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range rows {
			if v.ID == i.ID || v.HouseholdID != hA {
				t.Fatalf("foreign list entry=%+v", v)
			}
		}
		if _, _, err := repo.Complete(ctx, hA, i.ID, "foreign-key", [32]byte{5}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			return item.CompletionPlan{}, errors.New("should not reach decision")
		}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("foreign completion post err=%v", err)
		}
		if _, err := createForTest(ctx, repo, hA, completionUser, item.Draft{SubjectID: sB.ID, Title: "bad"}); !errors.Is(err, item.ErrInvalidReference) {
			t.Fatalf("foreign subject err=%v", err)
		}
		var foreignCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND subject_id=$2::uuid`, hA, sB.ID).Scan(&foreignCount); err != nil || foreignCount != 0 {
			t.Fatalf("foreign item persisted; count=%d err=%v", foreignCount, err)
		}
		afterItem, err := repo.Get(ctx, hB, i.ID)
		if err != nil || afterItem != beforeItem {
			t.Fatalf("foreign POST changed item before=%+v after=%+v err=%v", beforeItem, afterItem, err)
		}
		var afterCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&afterCount); err != nil || afterCount != beforeCount {
			t.Fatalf("foreign POST changed receipts before=%d after=%d err=%v", beforeCount, afterCount, err)
		}
	})
	t.Run("undo completion persistence behavior", func(t *testing.T) {
		today := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
		houses := func(_ context.Context, userID, householdID string) (string, error) {
			if userID != completionUser || householdID != hA {
				return "", item.ErrNotFound
			}
			return "UTC", nil
		}
		subjects := func(context.Context, string, string, string) (bool, error) { return false, nil }
		svc := item.NewService(repo, houses, subjects, members, func() time.Time { return today })
		mustDate := func(text string) *string { return &text }
		setPolicy := func(i item.Item, policy schedule.Policy) item.Item {
			t.Helper()
			got, err := svc.Update(ctx, completionUser, hA, i.ID, i.Version, item.Patch{Recurrence: item.Some(policy)})
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		getReceipt := func(itemID, receiptID string) item.Completion {
			t.Helper()
			page, err := svc.ListCompletions(ctx, completionUser, hA, itemID, 100, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, receipt := range page.Items {
				if receipt.ID == receiptID {
					return receipt
				}
			}
			t.Fatalf("receipt %s missing from %+v", receiptID, page.Items)
			return item.Completion{}
		}

		t.Run("fixed restore waiting snapshot and exact row", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.May, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo fixed", nil, &anchor)
			i = setPolicy(i, policy)
			waiting := string(item.StateWaiting)
			i, err := svc.Update(ctx, completionUser, hA, i.ID, i.Version, item.Patch{WorkflowState: &waiting})
			if err != nil {
				t.Fatal(err)
			}
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-fixed", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			beforeUndo, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, beforeUndo.Version)
			if err != nil {
				t.Fatal(err)
			}
			var storedAnchor, state string
			var done bool
			var version int64
			var undoneAt *time.Time
			var undoneBy *string
			if err = admin.QueryRow(ctx, `SELECT attention_on::text,workflow_state,done,version FROM items WHERE id=$1::uuid`, i.ID).Scan(&storedAnchor, &state, &done, &version); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, `SELECT undone_at,undone_by_user_id::text FROM item_completions WHERE id=$1::uuid`, completed.ID).Scan(&undoneAt, &undoneBy); err != nil {
				t.Fatal(err)
			}
			if storedAnchor != "2026-05-01" || state != "waiting" || done || version != beforeUndo.Version+1 || undoneAt == nil || undoneBy == nil || *undoneBy != completionUser || receipt.UndoneAt == nil || receipt.UndoneByUserID == nil || *receipt.UndoneByUserID != completionUser {
				t.Fatalf("restored row attention=%s state=%s done=%v version=%d receipt=%+v dbUndo=%v/%v", storedAnchor, state, done, version, receipt, undoneAt, undoneBy)
			}
			got, err := repo.Get(ctx, hA, i.ID)
			if err != nil || got.LastCompletedOn != nil {
				t.Fatalf("lastCompletedOn=%v err=%v", got.LastCompletedOn, err)
			}
		})

		t.Run("one-off retained and listed", func(t *testing.T) {
			i := create(hA, sA.ID, "undo one-off", nil, nil)
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-oneoff", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			done, err := repo.Get(ctx, hA, i.ID)
			if err != nil || !done.Done {
				t.Fatalf("before undo=%+v err=%v", done, err)
			}
			undone, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, done.Version)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repo.Get(ctx, hA, i.ID)
			if err != nil || stored.Done || stored.Version != done.Version+1 {
				t.Fatalf("after undo=%+v err=%v", stored, err)
			}
			listed := getReceipt(i.ID, completed.ID)
			var n int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE id=$1::uuid`, completed.ID).Scan(&n); err != nil || n != 1 || listed.UndoneAt == nil || undone.UndoneAt == nil {
				t.Fatalf("receipt rows=%d listed=%+v returned=%+v err=%v", n, listed, undone, err)
			}
		})

		t.Run("initialization receipt is ordinarily undoable and can chain with completion", func(t *testing.T) {
			completed, _ := schedule.NewDate(2026, time.March, 1)
			next, _ := schedule.NewDate(2026, time.March, 2)
			policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
			fingerprint := item.CompletionFingerprint(completionUser, func() *string { value := "2026-03-01"; return &value }())
			initial, err := createForTest(ctx, repo, hA, completionUser, item.Draft{SubjectID: sA.ID, Title: "undo initialization", Recurrence: policy, AttentionOn: &next, InitializationReceipt: &item.Completion{CompletedOn: completed, CompletedByUserID: completionUser, Recurrence: policy, PriorWorkflowState: item.StateOpen, NextAttentionOn: &next, ItemVersionBefore: 1, Fingerprint: fingerprint}})
			if err != nil || initial.Version != 2 {
				t.Fatalf("item=%+v err=%v", initial, err)
			}
			history, err := svc.ListCompletions(ctx, completionUser, hA, initial.ID, 50, "")
			if err != nil || len(history.Items) != 1 {
				t.Fatalf("history=%+v err=%v", history, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, initial.ID, history.Items[0].ID, initial.Version); err != nil {
				t.Fatal(err)
			}
			undone, err := repo.Get(ctx, hA, initial.ID)
			if err != nil || undone.Version != 3 || undone.AttentionOn != nil || undone.WorkflowState != item.StateOpen || undone.LastCompletedOn != nil {
				t.Fatalf("undo=%+v err=%v", undone, err)
			}
			actual, _, err := svc.Complete(ctx, completionUser, hA, initial.ID, undone.Version, "after-init-undo", item.CompletionRequest{CompletedOn: mustDate("2026-03-05")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, initial.ID, actual.ID, undone.Version+1); err != nil {
				t.Fatal(err)
			}
			restored, err := repo.Get(ctx, hA, initial.ID)
			if err != nil || restored.AttentionOn != nil || restored.LastCompletedOn != nil {
				t.Fatalf("chained undo=%+v err=%v", restored, err)
			}
		})

		t.Run("latest active chained undo uses version order", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.March, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo chain", nil, &anchor)
			i = setPolicy(i, policy)
			first, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "chain-first", item.CompletionRequest{CompletedOn: mustDate("2026-03-20")})
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version+1, "chain-second", item.CompletionRequest{CompletedOn: mustDate("2026-03-10")})
			if err != nil {
				t.Fatal(err)
			}
			current, err := repo.Get(ctx, hA, i.ID)
			if err != nil || current.LastCompletedOn == nil || current.LastCompletedOn.String() != "2026-03-10" {
				t.Fatalf("latest by version before undo=%+v err=%v", current, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, first.ID, current.Version); !errors.Is(err, item.ErrCompletionNotLatest) {
				t.Fatalf("first undo error=%v", err)
			}
			unchanged, err := repo.Get(ctx, hA, i.ID)
			if err != nil || unchanged.Version != current.Version || unchanged.LastCompletedOn == nil || unchanged.LastCompletedOn.String() != "2026-03-10" {
				t.Fatalf("rejected undo mutated state %+v err=%v", unchanged, err)
			}
			firstStillActive := getReceipt(i.ID, first.ID)
			secondStillActive := getReceipt(i.ID, second.ID)
			if firstStillActive.UndoneAt != nil || secondStillActive.UndoneAt != nil {
				t.Fatalf("rejected undo changed receipt rows first=%+v second=%+v", firstStillActive, secondStillActive)
			}
			var activeCount int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid AND undone_at IS NULL`, i.ID).Scan(&activeCount); err != nil || activeCount != 2 {
				t.Fatalf("rejected undo active receipt count=%d err=%v", activeCount, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, second.ID, current.Version); err != nil {
				t.Fatal(err)
			}
			middle, err := repo.Get(ctx, hA, i.ID)
			if err != nil || middle.AttentionOn == nil || middle.AttentionOn.String() != "2027-03-01" || middle.LastCompletedOn == nil || middle.LastCompletedOn.String() != "2026-03-20" {
				t.Fatalf("after second undo=%+v err=%v", middle, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, first.ID, middle.Version); err != nil {
				t.Fatal(err)
			}
			final, err := repo.Get(ctx, hA, i.ID)
			if err != nil || final.AttentionOn == nil || final.AttentionOn.String() != "2026-03-01" || final.LastCompletedOn != nil {
				t.Fatalf("after chained undo=%+v err=%v", final, err)
			}
		})

		t.Run("undo replay stale version one bump and complete again", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 15)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo replay complete again", nil, &anchor)
			i = setPolicy(i, policy)
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-replay", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			before, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			first, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, before.Version)
			if err != nil {
				t.Fatal(err)
			}
			after, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, 1)
			if err != nil || replay.ID != first.ID || replay.UndoneAt == nil || after.Version != before.Version+1 {
				t.Fatalf("replay=%+v after=%+v err=%v", replay, after, err)
			}
			repeated, _, err := svc.Complete(ctx, completionUser, hA, i.ID, after.Version, "undo-recomplete", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			var beforeVersion int64
			var next string
			if err = admin.QueryRow(ctx, `SELECT item_version_before,next_attention_on::text FROM item_completions WHERE id=$1::uuid`, repeated.ID).Scan(&beforeVersion, &next); err != nil {
				t.Fatal(err)
			}
			if beforeVersion != after.Version || next != "2027-01-15" {
				t.Fatalf("new receipt version=%d want=%d next=%s", beforeVersion, after.Version, next)
			}
			originalReplay, replayed, err := svc.Complete(ctx, completionUser, hA, i.ID, after.Version, "undo-replay", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil || !replayed || originalReplay.ID != completed.ID || originalReplay.CompletedOn.String() != "2026-10-06" || originalReplay.UndoneAt == nil || originalReplay.NextAttentionOn == nil || originalReplay.NextAttentionOn.String() != "2027-01-15" {
				t.Fatalf("original key/body replay=%+v replayed=%v err=%v", originalReplay, replayed, err)
			}
			replayedItem, err := repo.Get(ctx, hA, i.ID)
			if err != nil || replayedItem.Version != after.Version+1 {
				t.Fatalf("idempotent original replay changed item=%+v err=%v", replayedItem, err)
			}
			var count int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid AND item_version_before=$2`, i.ID, beforeVersion).Scan(&count); err != nil || count != 1 {
				t.Fatalf("version uniqueness count=%d err=%v", count, err)
			}
		})

		t.Run("foreign item and household completion IDs not found", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "foreign undo source", nil, &anchor)
			i = setPolicy(i, policy)
			c, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "foreign-undo", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			wrong := create(hA, sA.ID, "wrong target", nil, &anchor)
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, wrong.ID, c.ID, wrong.Version); !errors.Is(err, item.ErrNotFound) {
				t.Fatalf("wrong-item undo=%v", err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hB, i.ID, c.ID, i.Version+1); !errors.Is(err, item.ErrNotFound) {
				t.Fatalf("foreign-household undo=%v", err)
			}
			stored, err := repo.Get(ctx, hA, i.ID)
			if err != nil || stored.Version != i.Version+1 {
				t.Fatalf("source changed=%+v err=%v", stored, err)
			}
			if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='owner' WHERE household_id=$1::uuid AND user_id=$2::uuid`, hA, completionUser); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("concurrent duplicate undo and patch race", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo concurrent", nil, &anchor)
			i = setPolicy(i, policy)
			c, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-concurrent", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			current, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			const workers = 8
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, workers)
			for n := range workers {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					<-start
					_, errs[n] = svc.UndoCompletion(ctx, completionUser, hA, i.ID, c.ID, current.Version)
				}(n)
			}
			close(start)
			wg.Wait()
			for n, callErr := range errs {
				if callErr != nil && !errors.Is(callErr, item.ErrVersionMismatch) {
					t.Fatalf("undo call %d err=%v", n, callErr)
				}
			}
			final, err := repo.Get(ctx, hA, i.ID)
			if err != nil || final.Version != current.Version+1 || final.AttentionOn == nil || final.AttentionOn.String() != "2026-01-01" {
				t.Fatalf("concurrent undo final=%+v err=%v", final, err)
			}
			// A fresh item has one undo and one ordinary item PATCH competing for its row version.
			j := create(hA, sA.ID, "undo patch race", nil, &anchor)
			j = setPolicy(j, policy)
			receipt, _, err := svc.Complete(ctx, completionUser, hA, j.ID, j.Version, "undo-patch-race", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			versioned, err := repo.Get(ctx, hA, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			start = make(chan struct{})
			var undoErr, patchErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, undoErr = svc.UndoCompletion(ctx, completionUser, hA, j.ID, receipt.ID, versioned.Version)
			}()
			go func() {
				defer wg.Done()
				<-start
				title := "raced edit"
				_, patchErr = svc.Update(ctx, completionUser, hA, j.ID, versioned.Version, item.Patch{Title: &title})
			}()
			close(start)
			wg.Wait()
			if (undoErr == nil) == (patchErr == nil) || (undoErr != nil && !errors.Is(undoErr, item.ErrVersionMismatch)) || (patchErr != nil && !errors.Is(patchErr, item.ErrVersionMismatch)) {
				t.Fatalf("undo/patch race undo=%v patch=%v", undoErr, patchErr)
			}
			winner, err := repo.Get(ctx, hA, j.ID)
			if err != nil || winner.Version != versioned.Version+1 {
				t.Fatalf("race winner state=%+v err=%v", winner, err)
			}
		})
	})

	t.Run("completion table restrictions and constraints", func(t *testing.T) {
		i := create(hA, sA.ID, "completion protected", nil, nil)
		uid := completionUser
		if _, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-10-07',$3::uuid,'open',1,'key',decode(repeat('01',32),'hex'))`, hA, i.ID, uid); err != nil {
			t.Fatalf("baseline receipt insert: %v", err)
		}
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("baseline receipt count=%d want=1", n)
		}
		if _, err := app.Exec(ctx, `UPDATE item_completions SET undone_at=now(), undone_by_user_id=$2::uuid WHERE id=(SELECT id FROM item_completions WHERE item_id=$1::uuid)`, i.ID, uid); err != nil {
			t.Fatalf("runtime undo column update: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE item_completions SET undone_at=NULL, undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		for _, stmt := range []string{`UPDATE item_completions SET id=uuidv7()`, `UPDATE item_completions SET completed_on=completed_on`, `DELETE FROM item_completions`, `TRUNCATE item_completions`} {
			if _, err := app.Exec(ctx, stmt); err == nil {
				t.Fatalf("runtime role allowed %s", stmt)
			} else {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("%s error=%v", stmt, err)
				}
			}
		}
		for _, tc := range []struct{ name, key, fp, value, unit, mode, state string }{{"key", "bad key ", strings.Repeat("01", 32), "", "", "", "open"}, {"fingerprint", "bad-fingerprint", "01", "", "", "", "open"}, {"partial recurrence", "partial", strings.Repeat("01", 32), "1", "", "", "open"},
			{"interval null value", "null-value", strings.Repeat("01", 32), "", "day", "fixed", "open"},
			{"interval null unit", "null-unit", strings.Repeat("01", 32), "1", "", "fixed", "open"},
			{"interval null mode", "null-mode", strings.Repeat("01", 32), "1", "day", "", "open"}, {"interval lower bound", "low", strings.Repeat("01", 32), "0", "day", "fixed", "open"}, {"interval upper bound", "high", strings.Repeat("01", 32), "1000", "day", "fixed", "open"}, {"unit enum", "unit", strings.Repeat("01", 32), "1", "fortnight", "fixed", "open"}, {"mode enum", "mode", strings.Repeat("01", 32), "1", "day", "weekly", "open"}, {"workflow enum", "state", strings.Repeat("01", 32), "", "", "", "done"}} {
			_, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES($1::uuid,$2::uuid,'2026-10-07',$3::uuid,$4,2,$5,decode($6,'hex'),NULLIF($7,'')::int,NULLIF($8,''),NULLIF($9,''))`, hA, i.ID, uid, tc.state, tc.key, tc.fp, tc.value, tc.unit, tc.mode)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("%s SQLSTATE err=%v", tc.name, err)
			}
			want := map[string]string{"key": "item_completions_idempotency_key_check", "fingerprint": "item_completions_request_fingerprint_check", "partial recurrence": "item_completions_check", "interval null value": "item_completions_check", "interval null unit": "item_completions_check", "interval null mode": "item_completions_check", "interval lower bound": "item_completions_recurrence_interval_value_check", "interval upper bound": "item_completions_recurrence_interval_value_check", "unit enum": "item_completions_recurrence_interval_unit_check", "mode enum": "item_completions_recurrence_mode_check", "workflow enum": "item_completions_prior_workflow_state_check"}[tc.name]
			if pgErr.ConstraintName != want {
				t.Fatalf("%s constraint=%s want=%s err=%v", tc.name, pgErr.ConstraintName, want, err)
			}
		}
		_, pairErr := admin.Exec(ctx, `UPDATE item_completions SET undone_at=now(), undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID)
		var pairPG *pgconn.PgError
		if !errors.As(pairErr, &pairPG) || pairPG.Code != "23514" || pairPG.ConstraintName != "item_completions_undo_actor_pair_check" {
			t.Fatalf("undo actor pair constraint err=%v", pairErr)
		}
		if _, err := admin.Exec(ctx, `UPDATE item_completions SET undone_at=NULL, undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		_, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,uuidv7(),'2026-10-07',$2::uuid,'open',2,'bad-fk',decode(repeat('01',32),'hex'))`, hA, uid)
		var fk *pgconn.PgError
		if !errors.As(err, &fk) || fk.Code != "23503" || fk.ConstraintName != "item_completions_household_id_item_id_fkey" {
			t.Fatalf("composite FK err=%v", err)
		}
		_ = n
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
		for _, tc := range []struct{ name, statement, constraint string }{
			{"partial value", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value) VALUES ($1::uuid,$2::uuid,'bad',1)`, "items_recurrence_all_or_none"},
			{"partial unit", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_unit) VALUES ($1::uuid,$2::uuid,'bad','day')`, "items_recurrence_all_or_none"},
			{"partial mode", `INSERT INTO items(household_id,subject_id,title,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad','fixed')`, "items_recurrence_all_or_none"},
			{"out of range low", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',0,'day','fixed')`, "items_recurrence_interval_value_check"},
			{"out of range high", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1000,'day','fixed')`, "items_recurrence_interval_value_check"},
			{"invalid unit", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1,'fortnight','fixed')`, "items_recurrence_interval_unit_check"},
			{"invalid mode", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1,'day','weekly')`, "items_recurrence_mode_check"},
		} {
			_, err := admin.Exec(ctx, tc.statement, hA, sA.ID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != tc.constraint {
				t.Fatalf("%s constraint err=%v pgErr=%+v want=%s", tc.name, err, pgErr, tc.constraint)
			}
		}
	})
	t.Run("household deletion cascades", func(t *testing.T) {
		h := household("cascade")
		cascadeMember := assignedUser(h, "cascade_member", "member")
		s, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }).Create(ctx, cascadeMember, h, "cascade-subject", "child", subject.TypePerson)
		if err != nil {
			t.Fatal(err)
		}
		cascadeItem, err := createForTest(ctx, repo, h, completionUser, item.Draft{SubjectID: s.ID, ResponsibleUserID: &cascadeMember, Title: "cascade assigned item"})
		if err != nil || cascadeItem.ResponsibleUserID == nil {
			t.Fatalf("cascade fixture item=%+v err=%v", cascadeItem, err)
		}
		if _, err := admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, h); err != nil {
			t.Fatal(err)
		}
		if count(h) != 0 {
			t.Fatal("items survived household deletion")
		}
		var completionCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE household_id=$1::uuid`, h).Scan(&completionCount); err != nil || completionCount != 0 {
			t.Fatalf("completion rows survived household deletion count=%d err=%v", completionCount, err)
		}
	})
}

func TestMigrationUpgradeFromV7PreservesUndoableReceipt(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, cfg)
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
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; REVOKE UPDATE (undone_at, undone_by_user_id) ON item_completions FROM tendo; ALTER TABLE item_completions DROP CONSTRAINT item_completions_undo_actor_pair_check; ALTER TABLE item_completions DROP COLUMN undone_by_user_id; ALTER TABLE item_completions DROP COLUMN undone_at; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (8,9,10,11,12,13)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, actor, iid, cid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('v7 undo','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('v7_undo_actor') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, actor, hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','v7 subject') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,version) VALUES($1::uuid,$2::uuid,'v7 item','2027-01-01',1,'year','fixed',2) RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,cycle_attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,prior_workflow_state,next_attention_on,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-10-06',$3::uuid,'2026-01-01',1,'year','fixed','waiting','2027-01-01',1,'v7-receipt',decode(repeat('01',32),'hex')) RETURNING id::text`, hid, iid, actor).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	if err = database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("version 7 database accepted by version 13 application")
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var completed string
	var undoneAt *time.Time
	if err = upAdmin.QueryRow(ctx, `SELECT completed_on::text,undone_at FROM item_completions WHERE id=$1::uuid`, cid).Scan(&completed, &undoneAt); err != nil || completed != "2026-10-06" || undoneAt != nil {
		t.Fatalf("upgraded receipt date=%s undone=%v err=%v", completed, undoneAt, err)
	}
	repo := NewRepository(upApp)
	svc := item.NewService(repo, func(_ context.Context, user, house string) (string, error) {
		if user != actor || house != hid {
			return "", item.ErrNotFound
		}
		return "UTC", nil
	}, func(context.Context, string, string, string) (bool, error) { return false, nil }, func(context.Context, string, string) error { return nil }, func() time.Time { return time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC) })
	undone, err := svc.UndoCompletion(ctx, actor, hid, iid, cid, 2)
	if err != nil || undone.UndoneAt == nil {
		t.Fatalf("undo migrated receipt=%+v err=%v", undone, err)
	}
	var attention, state string
	var version int64
	if err = upAdmin.QueryRow(ctx, `SELECT attention_on::text,workflow_state,version FROM items WHERE id=$1::uuid`, iid).Scan(&attention, &state, &version); err != nil || attention != "2026-01-01" || state != "waiting" || version != 3 {
		t.Fatalf("migrated undo state=%s %s %d err=%v", attention, state, version, err)
	}
}

func TestMigrationUpgradeFromVersionFivePreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
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
	if _, err := upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_subject_id_fkey; ALTER TABLE items DROP CONSTRAINT items_recurrence_all_or_none; ALTER TABLE items DROP COLUMN recurrence_interval_value, DROP COLUMN recurrence_interval_unit, DROP COLUMN recurrence_mode; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (6,7,8,9,10,11,12,13)`); err != nil {
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
		t.Fatal("version 5 database accepted by version 13 application")
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
	if err := upAdmin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&max); err != nil || max != 13 {
		t.Fatalf("version=%d err=%v", max, err)
	}
}

func TestMigrationUpgradeFromVersionFourPreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
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
	if _, err := upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; DROP TABLE items; ALTER TABLE subjects DROP CONSTRAINT subjects_household_id_id_key; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DELETE FROM tendo_schema_migrations WHERE version IN (5,6,7,8,9,10,11,12,13)`); err != nil {
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
		t.Fatal("version 4 database accepted by version 13 application")
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
	var upgradeActor string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('upgrade_actor') RETURNING id::text`).Scan(&upgradeActor); err != nil {
		t.Fatal(err)
	}
	created, err := createForTest(ctx, NewRepository(upApp), hid, upgradeActor, item.Draft{SubjectID: sid, Title: "After upgrade"})
	if err != nil || created.Version != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var max int64
	var dirty bool
	if err := upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 13 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
