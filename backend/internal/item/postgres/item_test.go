package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	householddomain "github.com/bigtcze/tendo/backend/internal/household"
	householdpg "github.com/bigtcze/tendo/backend/internal/household/postgres"
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
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id;  DROP TABLE IF EXISTS oidc_identities; REVOKE UPDATE (responsible_user_id) ON items FROM tendo; ALTER TABLE items DROP CONSTRAINT items_responsible_membership_fk; DROP INDEX items_responsible_membership_idx; ALTER TABLE items DROP COLUMN responsible_user_id; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,10,11,12,13,14)`); err != nil {
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
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id;  DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,7,8,9,10,11,12,13,14)`); err != nil {
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

func mustTestDate(year int, month time.Month, day int) schedule.Date {
	d, err := schedule.NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

func assertItemAndReceiptUnchanged(t *testing.T, ctx context.Context, admin *pgxpool.Pool, itemID string, version int64, receiptCount int) {
	t.Helper()
	var storedVersion int64
	var receipts int
	var active int
	var storedSubject, storedHousehold string
	if err := admin.QueryRow(ctx, `SELECT version,subject_id::text,household_id::text FROM items WHERE id=$1::uuid`, itemID).Scan(&storedVersion, &storedSubject, &storedHousehold); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE undone_at IS NULL) FROM item_completions WHERE item_id=$1::uuid`, itemID).Scan(&receipts, &active); err != nil {
		t.Fatal(err)
	}
	if storedVersion != version || receipts != receiptCount || active != receiptCount {
		t.Fatalf("item/receipts changed item=%s household=%s subject=%s version=%d/%d receipts=%d active=%d want=%d", itemID, storedHousehold, storedSubject, storedVersion, version, receipts, active, receiptCount)
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
	if _, err = upAdmin.Exec(ctx, `ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id; ALTER TABLE items DROP CONSTRAINT items_household_creator_creation_key_unique, DROP CONSTRAINT items_creation_fingerprint_length, DROP CONSTRAINT items_creation_key_valid, DROP CONSTRAINT items_creation_metadata_all_or_none; ALTER TABLE items DROP COLUMN creation_request_fingerprint, DROP COLUMN creation_key, DROP COLUMN created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,13,14)`); err != nil {
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
	if err = upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 16 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
	got, err := NewRepository(upAdmin).Get(ctx, hid, iid)
	if err != nil || got.Version != 7 || got.Title != "legacy v12" {
		t.Fatalf("legacy item=%+v err=%v", got, err)
	}
}

const itemIDFloor = "00000000-0000-0000-0000-000000000000"

func TestFilteredListAgainstPostgres(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	house := func(name, zone string) string {
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,$2) RETURNING id::text`, name, zone).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	h1, h2 := house("filtered-list-a", "America/New_York"), house("filtered-list-b", "UTC")
	user := ""
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('filtered_list_user') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner'),($1::uuid,$3::uuid,'owner')`, user, h1, h2); err != nil {
		t.Fatal(err)
	}
	subjectFor := func(h, name string) string {
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person',$2) RETURNING id::text`, h, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	s1, s2 := subjectFor(h1, "filter-subject-a"), subjectFor(h1, "filter-subject-b")
	foreignSubject := subjectFor(h2, "filter-subject-foreign")
	member := ""
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('filtered_list_member') RETURNING id::text`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, h1); err != nil {
		t.Fatal(err)
	}
	foreignUser := ""
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('filtered_list_foreign') RETURNING id::text`).Scan(&foreignUser); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, foreignUser, h2); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, time.March, 2, 4, 30, 0, 0, time.UTC) // New York business date is March 1, UTC date is March 2.
	houses := func(_ context.Context, u, h string) (string, error) {
		if u != user || (h != h1 && h != h2) {
			return "", item.ErrNotFound
		}
		if h == h1 {
			return "America/New_York", nil
		}
		return "UTC", nil
	}
	members := func(_ context.Context, u, h string) error {
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, u, h).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return item.ErrNotFound
		}
		return nil
	}
	repo := NewRepository(app)
	svc := item.NewService(repo, houses, nil, members, func() time.Time { return clock })
	insert := func(h, s, title, responsible, state string, attention *string, recurring, archived, done bool) string {
		var id string
		err := admin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,responsible_user_id,workflow_state,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,archived,done) VALUES($1::uuid,$2::uuid,$3,NULLIF($4,'')::uuid,$5,NULLIF($6,'')::date,CASE WHEN $7 THEN 1 END,CASE WHEN $7 THEN 'day' END,CASE WHEN $7 THEN 'fixed' END,$8,$9) RETURNING id::text`, h, s, title, responsible, state, func() string {
			if attention == nil {
				return ""
			}
			return *attention
		}(), recurring, archived, done).Scan(&id)
		if err != nil {
			t.Fatalf("insert %s: %v", title, err)
		}
		return id
	}
	ids := map[string]string{}
	for _, tc := range []struct {
		name, sub, res, state, day string
		rec, arch, done            bool
	}{
		{"null-unassigned", s1, "", "open", "", false, false, false},
		{"yesterday", s1, member, "in_progress", "2026-02-28", true, false, false},
		{"today", s1, member, "waiting", "2026-03-01", false, false, false},
		// 2026-03-02 is upcoming in New York (Mar 1) but would be due by the UTC date (Mar 2).
		{"tomorrow", s1, "", "paused", "2026-03-02", true, false, false},
		{"archived-s2", s2, "", "paused", "2026-03-03", false, true, false},
		{"done-s1", s1, member, "open", "2026-02-01", false, false, true},
		{"archived-s1", s1, "", "open", "", true, true, false},
		{"archived-done-s1", s1, "", "open", "", false, true, true},
	} {
		var d *string
		if tc.day != "" {
			d = &tc.day
		}
		ids[tc.name] = insert(h1, tc.sub, tc.name, tc.res, tc.state, d, tc.rec, tc.arch, tc.done)
	}
	for _, state := range []string{"open", "in_progress", "waiting", "paused"} {
		ids["state-"+state] = insert(h1, s2, "state-"+state, "", state, nil, false, false, false)
	}
	foreignItem := insert(h2, foreignSubject, "foreign-house-item", "", "open", nil, false, false, false)
	list := func(h string, q item.ListQuery) []item.Item {
		t.Helper()
		p, e := svc.List(ctx, user, h, q)
		if e != nil {
			t.Fatalf("list %s %+v: %v", h, q, e)
		}
		return p.Items
	}
	// check asserts the exact matching set (in ascending id order) for h1.
	check := func(name string, q item.ListQuery, names ...string) {
		t.Helper()
		want := make([]string, 0, len(names))
		for _, n := range names {
			id, ok := ids[n]
			if !ok {
				t.Fatalf("%s: unknown fixture %q", name, n)
			}
			want = append(want, id)
		}
		sort.Strings(want)
		got := itemIDs(list(h1, q))
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: got %v want %v (%v)", name, got, want, names)
		}
	}
	states := []string{"state-open", "state-in_progress", "state-waiting", "state-paused"}
	with := func(extra ...string) []string { return append(append([]string{}, extra...), states...) }
	check("subject", item.ListQuery{SubjectID: s1}, "null-unassigned", "yesterday", "today", "tomorrow")
	check("subject uppercase", item.ListQuery{SubjectID: strings.ToUpper(s1)}, "null-unassigned", "yesterday", "today", "tomorrow")
	check("responsible uuid", item.ListQuery{ResponsibleUserID: member}, "yesterday", "today")
	check("unassigned NULL", item.ListQuery{ResponsibleUserID: "unassigned"}, with("null-unassigned", "tomorrow")...)
	check("workflow open", item.ListQuery{WorkflowState: "open"}, "null-unassigned", "state-open")
	check("workflow in_progress", item.ListQuery{WorkflowState: "in_progress"}, "yesterday", "state-in_progress")
	check("workflow waiting", item.ListQuery{WorkflowState: "waiting"}, "today", "state-waiting")
	check("workflow paused", item.ListQuery{WorkflowState: "paused"}, "tomorrow", "state-paused")
	check("attention household-local null/yesterday/today", item.ListQuery{Attention: "needs_attention"}, with("null-unassigned", "yesterday", "today")...)
	check("attention upcoming household-local tomorrow", item.ListQuery{Attention: "upcoming"}, "tomorrow")
	for _, r := range list(h1, item.ListQuery{Attention: "upcoming"}) {
		if r.Attention != schedule.Upcoming {
			t.Fatalf("filtered upcoming row derived %q", r.Attention)
		}
	}
	check("recurring true", item.ListQuery{Recurring: ptrBool(true)}, "yesterday", "tomorrow")
	check("recurring false", item.ListQuery{Recurring: ptrBool(false)}, with("null-unassigned", "today")...)
	check("AND subject/state", item.ListQuery{SubjectID: s1, WorkflowState: "paused"}, "tomorrow")
	check("AND attention/state", item.ListQuery{Attention: "needs_attention", WorkflowState: "paused"}, "state-paused")
	check("AND responsible/recurring", item.ListQuery{ResponsibleUserID: member, Recurring: ptrBool(true)}, "yesterday")
	check("archived only", item.ListQuery{Archived: true, SubjectID: s2}, "archived-s2")
	check("archived upcoming", item.ListQuery{Archived: true, Attention: "upcoming"}, "archived-s2")
	check("archived recurring", item.ListQuery{Archived: true, Recurring: ptrBool(true)}, "archived-s1")
	check("done only", item.ListQuery{Done: true, SubjectID: s1}, "done-s1")
	check("done responsible", item.ListQuery{Done: true, ResponsibleUserID: member}, "done-s1")
	check("archived+done", item.ListQuery{Archived: true, Done: true, SubjectID: s1}, "archived-done-s1")
	if got := list(h1, item.ListQuery{SubjectID: "0198a2f0-7c1e-7a53-9b0e-000000000099"}); len(got) != 0 {
		t.Fatalf("unknown subject=%v", itemIDs(got))
	}
	if got := list(h1, item.ListQuery{SubjectID: foreignSubject}); len(got) != 0 {
		t.Fatalf("foreign subject=%v", itemIDs(got))
	}
	if got := list(h1, item.ListQuery{ResponsibleUserID: foreignUser}); len(got) != 0 {
		t.Fatalf("foreign user=%v", itemIDs(got))
	}
	if got := list(h2, item.ListQuery{SubjectID: s1}); len(got) != 0 {
		t.Fatalf("reverse subject isolation=%v", itemIDs(got))
	}
	if got := list(h2, item.ListQuery{ResponsibleUserID: member}); len(got) != 0 {
		t.Fatalf("reverse responsible isolation=%v", itemIDs(got))
	}
	if got := list(h2, item.ListQuery{SubjectID: foreignSubject}); len(got) != 1 || got[0].ID != foreignItem {
		t.Fatalf("own household filtered list=%v", itemIDs(got))
	}
	if got := list(h2, item.ListQuery{ResponsibleUserID: "unassigned"}); len(got) != 1 || got[0].ID != foreignItem {
		t.Fatalf("own household unassigned list=%v", itemIDs(got))
	}
	// Clearing a policy moves the row from recurring=true to recurring=false.
	if _, err := repo.Update(ctx, h1, ids["yesterday"], 1, item.Change{Recurrence: item.Null[schedule.Policy]()}); err != nil {
		t.Fatal(err)
	}
	check("recurring true after clear", item.ListQuery{Recurring: ptrBool(true)}, "tomorrow")
	check("recurring false after clear", item.ListQuery{Recurring: ptrBool(false)}, with("null-unassigned", "yesterday", "today")...)
	// Sparse interleaved set on its own subject: 3 of every 7 rows match. Walk the real
	// service/repository with limit 2; every page boundary is followed by non-matching rows.
	s3 := subjectFor(h1, "filter-subject-sparse")
	var sparseIDs []string
	for n := 0; n < 35; n++ {
		sub := s2
		if n%7 < 3 {
			sub = s3
		}
		id := insert(h1, sub, fmt.Sprintf("sparse-%02d", n), "", "open", nil, false, false, false)
		if sub == s3 {
			sparseIDs = append(sparseIDs, id)
		}
	}
	var actual []string
	cursor := ""
	pages := 0
	for {
		p, e := svc.List(ctx, user, h1, item.ListQuery{SubjectID: s3, Limit: 2, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		pages++
		if p.NextCursor != nil && len(p.Items) != 2 {
			t.Fatalf("short non-final page %d: %v", pages, itemIDs(p.Items))
		}
		actual = append(actual, itemIDs(p.Items)...)
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	sort.Strings(sparseIDs) // uuidv7 order is not guaranteed to follow insertion order across connections.
	if len(sparseIDs) != 15 || pages != 8 || strings.Join(actual, ",") != strings.Join(sparseIDs, ",") {
		t.Fatalf("sparse pages=%d IDs=%v want=%v", pages, actual, sparseIDs)
	}
	// Both filter cursor mismatch and household binding; caller is a member of both.
	first, e := svc.List(ctx, user, h1, item.ListQuery{SubjectID: s1, Limit: 1})
	if e != nil || first.NextCursor == nil {
		t.Fatalf("first cursor=%+v %v", first, e)
	}
	var invalid *item.InvalidQueryError
	if _, e = svc.List(ctx, user, h1, item.ListQuery{SubjectID: s2, Cursor: *first.NextCursor}); !errors.As(e, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("changed subject cursor=%v", e)
	}
	houseCursor, e := svc.List(ctx, user, h1, item.ListQuery{WorkflowState: "open", Limit: 1})
	if e != nil || houseCursor.NextCursor == nil {
		t.Fatalf("household binding source=%+v %v", houseCursor, e)
	}
	if _, e = svc.List(ctx, user, h2, item.ListQuery{WorkflowState: "open", Cursor: *houseCursor.NextCursor}); !errors.As(e, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("household cursor=%v", e)
	}
	att, e := svc.List(ctx, user, h1, item.ListQuery{Attention: "needs_attention", Limit: 1})
	if e != nil || att.NextCursor == nil {
		t.Fatalf("attention cursor=%+v %v", att, e)
	}
	subOnly, e := svc.List(ctx, user, h1, item.ListQuery{SubjectID: s1, Limit: 1})
	if e != nil || subOnly.NextCursor == nil {
		t.Fatalf("subject cursor=%+v %v", subOnly, e)
	}
	clock = clock.Add(2 * time.Hour) // crosses New York local midnight from Mar 1 to Mar 2.
	if _, e = svc.List(ctx, user, h1, item.ListQuery{Attention: "needs_attention", Cursor: *att.NextCursor}); !errors.As(e, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("attention midnight cursor=%v", e)
	}
	if _, e = svc.List(ctx, user, h1, item.ListQuery{SubjectID: s1, Cursor: *subOnly.NextCursor}); e != nil {
		t.Fatalf("date-independent cursor invalid after midnight: %v", e)
	}
}

func ptrBool(v bool) *bool { return &v }
func itemIDs(rows []item.Item) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestAttentionSortPaginationAndMutationsAgainstPostgres(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, actor, subjectID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('attention pagination','UTC') RETURNING id::text`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('attention pagination actor') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, actor, h); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','attention target') RETURNING id::text`, h).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	// Generated UUIDv7 values deliberately alternate key order and attention date; nulls and ties span pages.
	rows := []struct{ id, date string }{}
	for _, d := range []string{"", "", "", "", "", "2026-01-01", "2026-01-01", "2026-01-01", "2026-01-01", "2026-01-01", "2026-02-01", "2026-03-01", "2026-04-01"} {
		rows = append(rows, struct{ id, date string }{nextAttentionID(), d})
	}
	for n, r := range rows {
		if _, err := admin.Exec(ctx, `INSERT INTO items(id,household_id,subject_id,title,attention_on,workflow_state,archived,done) VALUES($1::uuid,$2::uuid,$3::uuid,$4,NULLIF($5,'')::date,'open',false,false)`, r.id, h, subjectID, fmt.Sprintf("row-%d", n), r.date); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(app)
	q := item.ListQuery{Sort: item.SortAttention, Limit: 2}
	houses := func(context.Context, string, string) (string, error) { return "UTC", nil }
	svc := item.NewService(repo, houses, nil, nil, func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) })
	var got []string
	pages := 0
	for {
		p, err := svc.List(ctx, actor, h, q)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if p.NextCursor != nil && len(p.Items) != q.Limit {
			t.Fatalf("short nonfinal page=%+v", p)
		}
		for _, v := range p.Items {
			got = append(got, v.ID)
		}
		if p.NextCursor == nil {
			break
		}
		q.Cursor = *p.NextCursor
		q.Limit = map[int]int{2: 3, 3: 1, 1: 2}[q.Limit]
	}
	want := []string{rows[0].id, rows[1].id, rows[2].id, rows[3].id, rows[4].id, rows[5].id, rows[6].id, rows[7].id, rows[8].id, rows[9].id, rows[10].id, rows[11].id, rows[12].id}
	if pages < 3 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pages=%d got=%v want=%v", pages, got, want)
	}
	// Boundary is encoded, not refetched from the current row value.
	first, err := svc.List(ctx, actor, h, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err = admin.Exec(ctx, `UPDATE items SET attention_on='2027-01-01' WHERE id=$1::uuid`, first.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	continued, err := svc.List(ctx, actor, h, item.ListQuery{Sort: item.SortAttention, Limit: 5, Cursor: *first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(continued.Items) != 5 {
		t.Fatalf("continuation=%v", continued.Items)
	}
}

type capturingDB struct {
	pgx.Tx
	query string
	args  []any
}

func (c *capturingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.query = sql
	c.args = append([]any(nil), args...)
	return c.Tx.Query(ctx, sql, args...)
}

func (c *capturingDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.query = sql
	c.args = append([]any(nil), args...)
	return c.Tx.QueryRow(ctx, sql, args...)
}

func TestItemListExplainIndexesAtScale(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, s, other, os, actor, sparseSubject string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('explain scale','UTC') RETURNING id::text`).Scan(&h))
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','scale target') RETURNING id::text`, h).Scan(&s))
	must(admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('explain other','UTC') RETURNING id::text`).Scan(&other))
	must(admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('explain actor') RETURNING id::text`).Scan(&actor))
	mustExec := func(sql string, args ...any) { t.Helper(); _, err := admin.Exec(ctx, sql, args...); must(err) }
	mustExec(`INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, actor, h)
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','other target') RETURNING id::text`, other).Scan(&os))
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','sparse target') RETURNING id::text`, h).Scan(&sparseSubject))
	// IDs ascend with n so created pages and cursor positions are deterministic. Target has 10k rows; other household contributes a further 10k.
	mustExec(`INSERT INTO items(id,household_id,subject_id,title,attention_on,responsible_user_id,workflow_state,archived,done) SELECT ('0198a2f0-7c1e-7a53-9b0e-'||lpad((100000+n)::text,12,'0'))::uuid,CASE WHEN n%2=0 THEN $1::uuid ELSE $2::uuid END,CASE WHEN n%100=0 AND n%2=0 THEN $5::uuid WHEN n%2=0 THEN $3::uuid ELSE $4::uuid END,'explain-'||n,CASE WHEN n%7=0 THEN NULL ELSE DATE '2026-01-01'+(n%5) END,CASE WHEN n%10=0 THEN $6::uuid ELSE NULL END,CASE n%4 WHEN 0 THEN 'open' WHEN 1 THEN 'in_progress' WHEN 2 THEN 'waiting' ELSE 'paused' END,(n%11=0),(n%13=0) FROM generate_series(1,20000) n`, h, other, s, os, sparseSubject, actor)
	mustExec(`INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) SELECT household_id,id,DATE '2026-01-01', $1::uuid,'open',1,'explain-'||id::text,decode(repeat('ab',32),'hex') FROM items WHERE household_id=$2::uuid AND id IN (SELECT id FROM items WHERE household_id=$2::uuid ORDER BY id LIMIT 20)`, actor, h)
	mustExec(`ANALYZE items`)
	conn, err := app.Acquire(ctx)
	must(err)
	defer conn.Release()
	type explainCase struct {
		name      string
		sort      item.ListSort
		filters   item.ListFilters
		after     string
		afterDate *schedule.Date
		wantIndex string
		broad     bool
		predicate string
	}
	d := mustTestDate(2026, time.January, 1)
	cases := []explainCase{
		{name: "created first", sort: item.SortCreated, after: "00000000-0000-0000-0000-000000000000", wantIndex: createdOrderIndexes, broad: true, predicate: `id>'00000000-0000-0000-0000-000000000000'::uuid`},
		{name: "created deep", sort: item.SortCreated, after: "0198a2f0-7c1e-7a53-9b0e-000000110000", wantIndex: createdOrderIndexes, broad: true, predicate: `id>'0198a2f0-7c1e-7a53-9b0e-000000110000'::uuid`},
		{name: "attention first", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", wantIndex: "items_household_archived_done_attention_id_idx", broad: true, predicate: `(attention_on IS NOT NULL OR (attention_on IS NULL AND id>'00000000-0000-0000-0000-000000000000'::uuid))`},
		{name: "attention null cursor", sort: item.SortAttention, after: "0198a2f0-7c1e-7a53-9b0e-000000107000", wantIndex: "items_household_archived_done_attention_id_idx", predicate: `(attention_on IS NOT NULL OR (attention_on IS NULL AND id>'0198a2f0-7c1e-7a53-9b0e-000000107000'::uuid))`},
		{name: "attention repeated date cursor", sort: item.SortAttention, after: "0198a2f0-7c1e-7a53-9b0e-000000105010", afterDate: &d, wantIndex: "items_household_archived_done_attention_id_idx", broad: true, predicate: `(attention_on,id) > (DATE '2026-01-01','0198a2f0-7c1e-7a53-9b0e-000000105010'::uuid)`},
		{name: "attention upcoming", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", filters: item.ListFilters{Attention: schedule.Upcoming}, wantIndex: "items_household_archived_done_attention_id_idx", broad: true, predicate: `attention_on>DATE '2026-01-03'`},
		{name: "attention needs open", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", filters: item.ListFilters{Attention: schedule.NeedsAttention, WorkflowState: item.StateOpen}, wantIndex: "items_household_archived_done_attention_id_idx", predicate: `(attention_on IS NULL OR attention_on<=DATE '2026-01-03') AND workflow_state='open'`},
		{name: "attention sparse subject", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", filters: item.ListFilters{SubjectID: sparseSubject}, wantIndex: "items_household_archived_done_attention_id_idx", predicate: `subject_id='` + sparseSubject + `'::uuid`},
		{name: "attention responsible UUID", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", filters: item.ListFilters{ResponsibleUserID: item.Some(actor)}, wantIndex: "items_household_archived_done_attention_id_idx", predicate: `responsible_user_id='` + actor + `'::uuid`},
		{name: "attention unassigned", sort: item.SortAttention, after: "00000000-0000-0000-0000-000000000000", filters: item.ListFilters{ResponsibleUserID: item.Null[string]()}, wantIndex: "items_household_archived_done_attention_id_idx", predicate: `responsible_user_id IS NULL`},
	}

	var customIDs []string
	for _, tc := range cases {
		tx, err := conn.Begin(ctx)
		must(err)
		_, err = tx.Exec(ctx, `SET LOCAL plan_cache_mode=force_custom_plan`)
		must(err)
		capture := &capturingDB{Tx: tx}
		repo := NewRepository(capture)
		got, err := repo.List(ctx, h, item.ListOptions{Filters: tc.filters, Today: mustTestDate(2026, time.January, 3), Sort: tc.sort, AfterID: tc.after, AfterAttentionOn: tc.afterDate, Limit: 51})
		must(err)
		if len(got) > 51 {
			t.Fatalf("%s returned %d rows", tc.name, len(got))
		}
		order := "id ASC"
		if tc.sort == item.SortAttention {
			order = "attention_on ASC NULLS FIRST,id ASC"
		}
		oracleSQL := `SELECT id::text FROM items WHERE household_id=$1::uuid AND archived=false AND done=false AND ` + tc.predicate + ` ORDER BY ` + order + ` LIMIT 51`
		oracleRows, err := admin.Query(ctx, oracleSQL, h)
		must(err)
		var expected []string
		for oracleRows.Next() {
			var id string
			must(oracleRows.Scan(&id))
			expected = append(expected, id)
		}
		must(oracleRows.Err())
		oracleRows.Close()
		if strings.Join(itemIDs(got), ",") != strings.Join(expected, ",") {
			t.Fatalf("%s repo IDs differ from oracle: got=%v want=%v", tc.name, itemIDs(got), expected)
		}
		if tc.name == "attention first" {
			customIDs = itemIDs(got)
		}
		var payload []byte
		must(tx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)\n"+capture.query, capture.args...).Scan(&payload))
		var docs []map[string]any
		must(json.Unmarshal(payload, &docs))
		plan := docs[0]["Plan"].(map[string]any)
		t.Logf("EXPLAIN custom %s ids=%v plan=%s", tc.name, itemIDs(got), payload)
		if tc.broad {
			if !planOrderedIndex(plan, tc.wantIndex) {
				t.Errorf("%s main path lacks ordered index %s", tc.name, tc.wantIndex)
			}
			if mainHasSort(plan) {
				t.Errorf("%s main path contains Sort", tc.name)
			}
		}
		must(tx.Commit(ctx))
	}
	// Production SQL auto-plan warmup and prepared-plan counters, on one runtime connection.
	tx, err := conn.Begin(ctx)
	must(err)
	capture := &capturingDB{Tx: tx}
	_, err = NewRepository(capture).List(ctx, h, item.ListOptions{Today: mustTestDate(2026, time.January, 3), Sort: item.SortAttention, AfterID: "00000000-0000-0000-0000-000000000000", Limit: 51})
	must(err)
	must(tx.Commit(ctx))
	for n := 0; n < 12; n++ {
		rows, err := conn.Query(ctx, capture.query, capture.args...)
		must(err)
		for rows.Next() {
			_, err := rows.Values()
			must(err)
		}
		rows.Close()
		must(rows.Err())
	}
	var prepared string
	must(conn.QueryRow(ctx, `SELECT COALESCE(json_agg(json_build_object('name',name,'generic_plans',generic_plans,'custom_plans',custom_plans))::text,'[]') FROM pg_prepared_statements`).Scan(&prepared))
	t.Logf("pg_prepared_statements=%s", prepared)
	// Force generic for diagnostics only; require no temp spill and equivalent page IDs.
	tx, err = conn.Begin(ctx)
	must(err)
	_, err = tx.Exec(ctx, `SET LOCAL plan_cache_mode=force_generic_plan`)
	must(err)
	captureGeneric := &capturingDB{Tx: tx}
	generic, err := NewRepository(captureGeneric).List(ctx, h, item.ListOptions{Today: mustTestDate(2026, time.January, 3), Sort: item.SortAttention, AfterID: "00000000-0000-0000-0000-000000000000", Limit: 51})
	must(err)
	var payload []byte
	must(tx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)\n"+captureGeneric.query, captureGeneric.args...).Scan(&payload))
	var docs []map[string]any
	must(json.Unmarshal(payload, &docs))
	gp := docs[0]["Plan"].(map[string]any)
	t.Logf("EXPLAIN forced generic attention first ids=%v plan=%s", itemIDs(generic), payload)
	if planTempWritten(gp) != 0 {
		t.Errorf("generic plan spilled temp blocks: %d", planTempWritten(gp))
	}
	if strings.Join(itemIDs(generic), ",") != strings.Join(customIDs, ",") {
		t.Errorf("generic page mismatch: generic=%v custom=%v", itemIDs(generic), customIDs)
	}
	tx.Rollback(ctx)
}

// createdOrderIndexes lists the indexes that can each legitimately serve the unchanged
// V1 id-ordered list without a Sort; the planner chooses by cost (with this fixture's
// 50/50 household split it may walk items_pkey and filter the household), so none is pinned.
const createdOrderIndexes = "items_pkey|items_household_id_id_key|items_household_archived_done_id_idx"

// planOrderedIndex reports whether the main (non-subplan) path contains an ordered
// Index Scan or Index Only Scan on one of the "|"-separated index names.
func planOrderedIndex(node map[string]any, index string) bool {
	if name, ok := node["Index Name"].(string); ok && (node["Node Type"] == "Index Scan" || node["Node Type"] == "Index Only Scan") {
		for _, want := range strings.Split(index, "|") {
			if name == want {
				return true
			}
		}
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		m := child.(map[string]any)
		rel, _ := m["Parent Relationship"].(string)
		if rel == "SubPlan" || rel == "InitPlan" {
			continue
		}
		if planOrderedIndex(m, index) {
			return true
		}
	}
	return false
}
func mainHasSort(node map[string]any) bool {
	if node["Node Type"] == "Sort" || node["Node Type"] == "Incremental Sort" {
		return true
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		m := child.(map[string]any)
		rel, _ := m["Parent Relationship"].(string)
		if rel == "SubPlan" || rel == "InitPlan" {
			continue
		}
		if mainHasSort(m) {
			return true
		}
	}
	return false
}
func planTempWritten(node map[string]any) int {
	total := 0
	if n, ok := node["Temp Written Blocks"].(float64); ok {
		total += int(n)
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		total += planTempWritten(child.(map[string]any))
	}
	return total
}

func planUsesIndex(node map[string]any, index string) bool {
	if node["Index Name"] == index {
		return true
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if planUsesIndex(child.(map[string]any), index) {
			return true
		}
	}
	return false
}

func TestMigrationUpgradeFromV14ToV15PreservesData(t *testing.T) {
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
	var h, actor, assignee, sid, i1, i2, i3 string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('upgrade v14','UTC') RETURNING id::text`).Scan(&h))
	for _, v := range []struct {
		login  string
		target *string
	}{{"upgrade actor", &actor}, {"upgrade assignee", &assignee}} {
		must(upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, v.login).Scan(v.target))
	}
	must(func() error {
		_, e := upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner'),($3::uuid,$2::uuid,'member')`, actor, h, assignee)
		return e
	}())
	must(upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','upgrade subject') RETURNING id::text`, h).Scan(&sid))
	// Different schedule/workflow partitions, responsible assignment and idempotent create metadata all survive the v14 rewind.
	for _, row := range []struct {
		title      string
		date       any
		rec        bool
		arch, done bool
		version    int64
		creation   string
		into       *string
	}{{"null attention", nil, false, false, false, 4, "v14-item-1", &i1}, {"past recurring", "2026-01-02", true, true, false, 7, "v14-item-2", &i2}, {"future done", "2030-02-03", false, false, true, 9, "v14-item-3", &i3}} {
		must(upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,responsible_user_id,title,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,archived,done,version,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::date,CASE WHEN $6 THEN 2 END,CASE WHEN $6 THEN 'week' END,CASE WHEN $6 THEN 'fixed' END,$7,$8,$9,$10::uuid,$11,decode(repeat('cd',32),'hex')) RETURNING id::text`, h, sid, assignee, row.title, row.date, row.rec, row.arch, row.done, row.version, actor, row.creation).Scan(row.into))
	}
	// Receipt facts include an undone receipt with actor/time metadata and an active completion.
	must(func() error {
		_, e := upAdmin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,cycle_attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,prior_workflow_state,next_attention_on,item_version_before,idempotency_key,request_fingerprint,undone_at,undone_by_user_id) VALUES($1::uuid,$2::uuid,'2026-02-01',$3::uuid,NULL,2,'week','fixed','waiting','2026-02-15',3,'upgrade-undone',decode(repeat('ab',32),'hex'),'2026-02-02 03:04:05+00',$3::uuid),($1::uuid,$4::uuid,'2026-02-03',$3::uuid,'2026-01-20',2,'week','fixed','open','2026-02-17',8,'upgrade-active',decode(repeat('ef',32),'hex'),NULL,NULL)`, h, i2, actor, i1)
		return e
	}())
	// Rewind migration 015 and its ledger first; then compare complete durable rows.
	must(func() error {
		_, e := upAdmin.Exec(ctx, `DROP TRIGGER items_search_text ON items; DROP FUNCTION tendo_items_search_text(); DROP INDEX items_search_text_trgm_idx; ALTER TABLE items DROP COLUMN search_text; DROP FUNCTION tendo_item_search_pattern(text); DROP FUNCTION tendo_item_search_normalize(text); DROP INDEX items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,16)`)
		return e
	}())
	snapshot := func() string {
		t.Helper()
		var got string
		must(upAdmin.QueryRow(ctx, `SELECT jsonb_build_object('items',(SELECT jsonb_agg(to_jsonb(x)-'search_text' ORDER BY x.id) FROM items x),'completions',(SELECT jsonb_agg(to_jsonb(x) ORDER BY x.id) FROM item_completions x),'subjects',(SELECT jsonb_agg(to_jsonb(x) ORDER BY x.id) FROM subjects x),'memberships',(SELECT jsonb_agg(to_jsonb(x) ORDER BY x.user_id,x.household_id) FROM household_memberships x))::text`).Scan(&got))
		return got
	}
	before := snapshot()
	if err = database.ValidateSchema(ctx, upApp); err == nil {
		t.Fatal("v14 accepted")
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("database rows changed across v14->v15\nbefore=%s\nafter=%s", before, after)
	}
	// Migration 016 backfills every pre-existing row with independently normalized title + notes text.
	var backfilled, mismatched int
	must(upAdmin.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE search_text IS DISTINCT FROM normalize(unaccent(casefold(normalize(title || E'\n' || COALESCE(notes, ''), NFC) COLLATE pg_unicode_fast)), NFC)) FROM items`).Scan(&backfilled, &mismatched))
	if backfilled == 0 || mismatched != 0 {
		t.Fatalf("search_text backfill rows=%d mismatched=%d", backfilled, mismatched)
	}
	if err = database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatalf("runtime schema validation: %v", err)
	}
	var version int
	var dirty bool
	must(upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty))
	if version != 16 || dirty {
		t.Fatalf("version=%d dirty=%v", version, dirty)
	}
	var def string
	var valid bool
	must(upAdmin.QueryRow(ctx, `SELECT pg_get_indexdef('items_household_archived_done_attention_id_idx'::regclass),indisvalid FROM pg_index WHERE indexrelid='items_household_archived_done_attention_id_idx'::regclass`).Scan(&def, &valid))
	const wantDef = "CREATE INDEX items_household_archived_done_attention_id_idx ON public.items USING btree (household_id, archived, done, attention_on NULLS FIRST, id)"
	if def != wantDef || !valid {
		t.Fatalf("attention index def=%q valid=%v want=%q", def, valid, wantDef)
	}
	var oldExists bool
	must(upAdmin.QueryRow(ctx, `SELECT to_regclass('items_household_archived_done_id_idx') IS NOT NULL`).Scan(&oldExists))
	if !oldExists {
		t.Fatal("legacy created index missing")
	}
	repo := NewRepository(upApp)
	for _, sortBy := range []item.ListSort{item.SortCreated, item.SortAttention} {
		rows, e := repo.List(ctx, h, item.ListOptions{Today: mustTestDate(2026, time.February, 4), Sort: sortBy, AfterID: "00000000-0000-0000-0000-000000000000", Limit: 10})
		must(e)
		if len(rows) != 1 {
			t.Fatalf("sort=%s rows=%v", sortBy, rows)
		}
		if rows[0].Title != "null attention" {
			t.Errorf("sort=%s row=%+v", sortBy, rows[0])
		}
	}
	var canSelect, canDelete, canTruncate, canCreate bool
	must(upApp.QueryRow(ctx, `SELECT has_table_privilege(current_user,'items','SELECT'),has_table_privilege(current_user,'items','DELETE'),has_table_privilege(current_user,'items','TRUNCATE'),has_schema_privilege(current_user,'public','CREATE')`).Scan(&canSelect, &canDelete, &canTruncate, &canCreate))
	if !canSelect || canDelete || canTruncate || canCreate {
		t.Fatalf("runtime privileges select=%v delete=%v truncate=%v create-public=%v", canSelect, canDelete, canTruncate, canCreate)
	}
}

func TestMigrationUpgradeFromV15ToV16BackfillsSearch(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	// Rewind only the new migration ledger row; schema is a faithful v15 shape below.
	var household, actor, subjectID, czechID, nullID, receiptID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('search v15 upgrade','UTC') RETURNING id::text`).Scan(&household))
	must(admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('search v15 actor') RETURNING id::text`).Scan(&actor))
	_, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, actor, household)
	must(err)
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','search upgrade') RETURNING id::text`, household).Scan(&subjectID))
	must(admin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,notes,created_by_user_id,creation_key,creation_request_fingerprint) VALUES($1::uuid,$2::uuid,'Žluťoučký kůň','Poznámky žluté',$3::uuid,'search-upgrade-czech',decode(repeat('ab',32),'hex')) RETURNING id::text`, household, subjectID, actor).Scan(&czechID))
	must(admin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,notes) VALUES($1::uuid,$2::uuid,'Null note target',NULL) RETURNING id::text`, household, subjectID).Scan(&nullID))
	must(admin.QueryRow(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-01-02',$3::uuid,'open',1,'search-upgrade-receipt',decode(repeat('cd',32),'hex')) RETURNING id::text`, household, czechID, actor).Scan(&receiptID))
	_, err = admin.Exec(ctx, `DROP TRIGGER items_search_text ON items; DROP FUNCTION tendo_items_search_text(); DROP INDEX items_search_text_trgm_idx; ALTER TABLE items DROP COLUMN search_text; DROP FUNCTION tendo_item_search_pattern(text); DROP FUNCTION tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16`)
	must(err)
	if err := database.ValidateSchema(ctx, app); err == nil {
		t.Fatal("v15 schema accepted by ValidateSchema")
	}
	snapshot := func() string {
		t.Helper()
		var value string
		must(admin.QueryRow(ctx, `SELECT jsonb_build_object('items',(SELECT jsonb_agg(to_jsonb(i)-'search_text' ORDER BY i.id) FROM items i),'completions',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM item_completions c),'subjects',(SELECT jsonb_agg(to_jsonb(s) ORDER BY s.id) FROM subjects s))::text`).Scan(&value))
		return value
	}
	before := snapshot()
	var v15, v16 int
	must(admin.QueryRow(ctx, `SELECT count(*) FILTER (WHERE version=15),count(*) FILTER (WHERE version=16) FROM tendo_schema_migrations`).Scan(&v15, &v16))
	if v15 != 1 || v16 != 0 {
		t.Fatalf("ledger v15=%d v16=%d", v15, v16)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); got != before {
		t.Fatalf("durable rows changed during 15->16 migration\nbefore=%s\nafter=%s", before, got)
	}
	for id, want := range map[string]string{czechID: "zlutoucky kun\npoznamky zlute", nullID: "null note target\n"} {
		var got string
		must(admin.QueryRow(ctx, `SELECT search_text FROM items WHERE id=$1::uuid`, id).Scan(&got))
		if got != want {
			t.Errorf("search_text id=%s=%q want=%q", id, got, want)
		}
	}
	if err := database.ValidateSchema(ctx, app); err != nil {
		t.Fatalf("runtime validation: %v", err)
	}
	repo := NewRepository(app)
	service := item.NewService(repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, nil, nil, func() time.Time { return time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC) })
	for q, id := range map[string]string{"zlutoucky": czechID, "poznamky": czechID, "null note": nullID} {
		page, e := service.List(ctx, actor, household, item.ListQuery{Q: q, Limit: 10})
		if e != nil || len(page.Items) != 1 || page.Items[0].ID != id {
			t.Errorf("search q=%q page=%+v err=%v", q, page, e)
		}
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	if got := snapshot(); got != before {
		t.Fatalf("repeat migration changed rows\nbefore=%s\nafter=%s", before, got)
	}
	var maxVersion int
	must(admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&maxVersion))
	if maxVersion != 16 {
		t.Fatalf("max migration version=%d", maxVersion)
	}
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
	sA, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }, func(context.Context, string, string) error { return nil }).Create(ctx, completionUser, hA, "item-person-a", "Person A", subject.TypePerson, nil)
	if err != nil {
		t.Fatal(err)
	}
	sB, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }, func(context.Context, string, string) error { return nil }).Create(ctx, completionUser, hB, "item-person-b", "Person B", subject.TypePerson, nil)
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
		history, err := repo.ListCompletions(ctx, hA, created.ID, itemIDFloor, 10, false)
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
		history, err = repo.ListCompletions(ctx, hA, created.ID, itemIDFloor, 10, false)
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
		listed, err := repo.List(ctx, hA, item.ListOptions{Filters: item.ListFilters{}, Today: mustTestDate(2026, time.January, 15), AfterID: itemIDFloor, Limit: 100})
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
		storedHistory, err := repo.ListCompletions(ctx, hA, got.ID, "00000000-0000-0000-0000-000000000000", 10, false)
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
				rows, err := service.ListCompletions(ctx, completionUser, hA, created.ID, 10, "", item.CompletionOrderOldest)
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
				rows, err = service.ListCompletions(ctx, completionUser, hA, created.ID, 10, "", item.CompletionOrderOldest)
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
		s, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }, func(context.Context, string, string) error { return nil }).Create(ctx, pgActor, h, "pages-subject", "subject", subject.TypePerson, nil)
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
			rows, err := repo.List(ctx, h, item.ListOptions{Filters: item.ListFilters{}, Today: mustTestDate(2026, time.January, 15), AfterID: after, Limit: 3})
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
		active, err := repo.List(ctx, h, item.ListOptions{Filters: item.ListFilters{}, Today: mustTestDate(2026, time.January, 15), AfterID: "00000000-0000-0000-0000-000000000000", Limit: 100})
		if err != nil || len(active) != 6 {
			t.Fatalf("active=%d err=%v", len(active), err)
		}
		archived, err := repo.List(ctx, h, item.ListOptions{Filters: item.ListFilters{Archived: true}, Today: mustTestDate(2026, time.January, 15), AfterID: "00000000-0000-0000-0000-000000000000", Limit: 100})
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
		rows, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10, false)
		if err != nil || len(rows) != 1 || rows[0].Recurrence == nil || rows[0].NextAttentionOn.String() != "2027-10-07" {
			t.Fatalf("history=%+v err=%v", rows, err)
		}
		if _, err := admin.Exec(ctx, `UPDATE items SET archived=true,done=false,version=version+1 WHERE id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		archivedRows, err := repo.ListCompletions(ctx, hA, i.ID, itemIDFloor, 10, false)
		if err != nil || len(archivedRows) != 1 {
			t.Fatalf("archived history=%+v err=%v", archivedRows, err)
		}
		if _, err := admin.Exec(ctx, `UPDATE items SET archived=false,done=true,version=version+1 WHERE id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		doneRows, err := repo.ListCompletions(ctx, hA, i.ID, itemIDFloor, 10, false)
		if err != nil || len(doneRows) != 1 {
			t.Fatalf("done history=%+v err=%v", doneRows, err)
		}
		if _, err = repo.ListCompletions(ctx, hA, first.ID, "00000000-0000-0000-0000-000000000000", 10, false); !errors.Is(err, item.ErrNotFound) {
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
		list, err := repo.List(ctx, hA, item.ListOptions{Filters: item.ListFilters{}, Today: mustTestDate(2026, time.January, 15), AfterID: "00000000-0000-0000-0000-000000000000", Limit: 100})
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
		if _, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10, false); !errors.Is(err, item.ErrNotFound) {
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
	t.Run("service rejects foreign subject updates without mutation", func(t *testing.T) {
		i := create(hA, sA.ID, "foreign subject update", nil, nil)
		before, err := repo.Get(ctx, hA, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		serviceWithMembership := item.NewService(repo, houses, func(_ context.Context, _, householdID, subjectID string) (bool, error) {
			var exists bool
			err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM subjects WHERE household_id=$1::uuid AND id=$2::uuid)`, householdID, subjectID).Scan(&exists)
			if err != nil {
				return false, err
			}
			if !exists {
				return false, item.ErrNotFound
			}
			return false, nil
		}, members, func() time.Time { return clock })
		foreignSubject := sB.ID
		_, err = serviceWithMembership.Update(ctx, completionUser, hA, i.ID, before.Version, item.Patch{SubjectID: &foreignSubject})
		var validation *item.ValidationError
		if !errors.As(err, &validation) || validation.Field != "subjectId" || validation.Code != "invalid_reference" {
			t.Fatalf("foreign subject update=%v", err)
		}
		var subjectID string
		var version int64
		if err = admin.QueryRow(ctx, `SELECT subject_id::text,version FROM items WHERE id=$1::uuid`, i.ID).Scan(&subjectID, &version); err != nil || subjectID != before.SubjectID || version != before.Version {
			t.Fatalf("item changed subject=%s version=%d before=%+v err=%v", subjectID, version, before, err)
		}
	})

	t.Run("service authorization and completion receipt isolation", func(t *testing.T) {
		foreignItem := create(hB, sB.ID, "isolated completion", nil, nil)
		var outsider string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('item_household_outsider') RETURNING id::text`).Scan(&outsider); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, outsider, hA); err != nil {
			t.Fatal(err)
		}
		membership := householdpg.NewMembershipRepository(app)
		houseRepo := householdpg.NewRepository(app)
		isolated := item.NewService(repo, func(ctx context.Context, userID, householdID string) (string, error) {
			h, err := houseRepo.FindForMember(ctx, userID, householdID)
			if errors.Is(err, householddomain.ErrNotFound) {
				return "", item.ErrNotFound
			}
			if err != nil {
				return "", err
			}
			return h.Timezone, nil
		}, func(ctx context.Context, userID, householdID, subjectID string) (bool, error) {
			if _, err := membership.GetMembership(ctx, userID, householdID); errors.Is(err, householddomain.ErrNotFound) {
				return false, item.ErrNotFound
			} else if err != nil {
				return false, err
			}
			subjectRow, err := subRepo.Get(ctx, householdID, subjectID)
			if errors.Is(err, subject.ErrNotFound) {
				return false, item.ErrNotFound
			}
			return subjectRow.Archived, err
		}, members, func() time.Time { return clock })
		before, err := repo.Get(ctx, hB, foreignItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = isolated.Complete(ctx, outsider, hB, foreignItem.ID, before.Version, "outsider-complete", item.CompletionRequest{}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("nonmember completion=%v", err)
		}
		assertItemAndReceiptUnchanged(t, ctx, admin, foreignItem.ID, before.Version, 0)
		activeReceipt, _, err := isolated.Complete(ctx, completionUser, hB, foreignItem.ID, before.Version, "authorized-complete", item.CompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = isolated.UndoCompletion(ctx, outsider, hB, foreignItem.ID, activeReceipt.ID, before.Version+1); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("nonmember undo=%v", err)
		}
		assertItemAndReceiptUnchanged(t, ctx, admin, foreignItem.ID, before.Version+1, 1)

		own := create(hA, sA.ID, "receipt target", nil, nil)
		other := create(hA, sA.ID, "receipt source", nil, nil)
		changedTitle := "changed version"
		own, err = repo.Update(ctx, hA, own.ID, own.Version, item.Change{Title: &changedTitle})
		if err != nil {
			t.Fatal(err)
		}
		ownCompletion, _, err := service.Complete(ctx, completionUser, hA, own.ID, own.Version, "own-receipt-target", item.CompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		otherCompletion, _, err := service.Complete(ctx, completionUser, hA, other.ID, other.Version, "same-house-other-item", item.CompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		foreignCompletion := create(hB, sB.ID, "foreign receipt", nil, nil)
		foreignCompletionReceipt, _, err := isolated.Complete(ctx, completionUser, hB, foreignCompletion.ID, foreignCompletion.Version, "foreign-receipt-source", item.CompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name, itemID string
			receipt      item.Completion
		}{{"foreign household receipt", own.ID, foreignCompletionReceipt}, {"same household other item receipt", own.ID, otherCompletion}} {
			if _, err = service.UndoCompletion(ctx, completionUser, hA, tc.itemID, tc.receipt.ID, ownCompletion.ItemVersionBefore+1); !errors.Is(err, item.ErrNotFound) {
				t.Fatalf("%s undo=%v", tc.name, err)
			}
			assertItemAndReceiptUnchanged(t, ctx, admin, tc.itemID, ownCompletion.ItemVersionBefore+1, 1)
			assertItemAndReceiptUnchanged(t, ctx, admin, tc.receipt.ItemID, tc.receipt.ItemVersionBefore+1, 1)
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
		rows, err := repo.List(ctx, hA, item.ListOptions{Filters: item.ListFilters{}, Today: mustTestDate(2026, time.January, 15), AfterID: "00000000-0000-0000-0000-000000000000", Limit: 100})
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
	t.Run("ordered completion walks and isolation", func(t *testing.T) {
		i := create(hA, sA.ID, "ordered completion walk", nil, nil)
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		i, err = service.Update(ctx, completionUser, hA, i.ID, i.Version, item.Patch{Recurrence: item.Some(policy)})
		if err != nil {
			t.Fatal(err)
		}
		otherItem := create(hA, sA.ID, "other completion item", nil, nil)
		version := i.Version
		for n := 0; n < 5; n++ {
			_, _, err = service.Complete(ctx, completionUser, hA, i.ID, version, fmt.Sprintf("order-%d", n), item.CompletionRequest{CompletedOn: func() *string { s := "2020-01-01"; return &s }()})
			if err != nil {
				t.Fatalf("completion %d version=%d: %v", n, version, err)
			}
			version++
		}
		if _, _, err := service.Complete(ctx, completionUser, hA, otherItem.ID, 1, "other-item", item.CompletionRequest{}); err != nil {
			t.Fatal(err)
		}
		oldest, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 2, "", item.CompletionOrderOldest)
		if err != nil || len(oldest.Items) != 2 || oldest.NextCursor == nil {
			t.Fatalf("oldest=%+v err=%v", oldest, err)
		}
		oldestContinuation, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 10, *oldest.NextCursor, item.CompletionOrderOldest)
		if err != nil || len(oldestContinuation.Items) != 3 {
			t.Fatalf("oldest continuation=%+v err=%v", oldestContinuation, err)
		}
		newest, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 2, "", item.CompletionOrderNewest)
		if err != nil || len(newest.Items) != 2 || newest.Items[0].ID <= newest.Items[1].ID || newest.NextCursor == nil {
			t.Fatalf("newest=%+v err=%v", newest, err)
		}
		newestNext, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 10, *newest.NextCursor, item.CompletionOrderNewest)
		if err != nil || len(newestNext.Items) != 3 {
			t.Fatalf("newest continuation=%+v err=%v", newestNext, err)
		}
		for _, receipt := range newestNext.Items {
			if receipt.ID > newest.Items[1].ID {
				t.Fatalf("newest cursor walk did not continue strictly older: first=%+v next=%+v", newest, newestNext)
			}
		}
		oldRows, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 10, "", item.CompletionOrderOldest)
		if err != nil || len(oldRows.Items) != 5 {
			t.Fatalf("ascending walk=%+v err=%v", oldRows, err)
		}
		for n := range newest.Items {
			if newest.Items[n].ID != oldRows.Items[len(oldRows.Items)-1-n].ID {
				t.Fatalf("newest is not reverse ascending: newest=%v oldest=%v", newest.Items, oldRows.Items)
			}
		}
		newCursor := *newest.NextCursor
		if _, _, err := service.Complete(ctx, completionUser, hA, i.ID, version, "between-page", item.CompletionRequest{}); err != nil {
			t.Fatal(err)
		}
		next, err := service.ListCompletions(ctx, completionUser, hA, i.ID, 10, newCursor, item.CompletionOrderNewest)
		if err != nil || len(next.Items) != 3 {
			t.Fatalf("live continuation=%+v err=%v", next, err)
		}
		for _, c := range next.Items {
			if c.ID > newest.Items[1].ID {
				t.Fatalf("new receipt crossed page boundary: %+v", c)
			}
		}
		foreignItem := create(hB, sB.ID, "foreign household receipt", nil, nil)
		if _, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,DATE '2026-01-01',$3::uuid,'open',1,'foreign-isolation',decode(repeat('aa',32),'hex'))`, hB, foreignItem.ID, completionUser); err != nil {
			t.Fatal(err)
		}
		rows, err := repo.ListCompletions(ctx, hA, i.ID, itemIDFloor, 20, false)
		if err != nil || len(rows) != 6 {
			t.Fatalf("household rows=%d err=%v", len(rows), err)
		}
		otherRows, err := repo.ListCompletions(ctx, hA, otherItem.ID, itemIDFloor, 20, false)
		if err != nil || len(otherRows) != 1 {
			t.Fatalf("other item rows=%d err=%v", len(otherRows), err)
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
			page, err := svc.ListCompletions(ctx, completionUser, hA, itemID, 100, "", item.CompletionOrderOldest)
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
			history, err := svc.ListCompletions(ctx, completionUser, hA, initial.ID, 50, "", item.CompletionOrderOldest)
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
		s, err := subject.NewService(subRepo, func(context.Context, string, string) error { return nil }, func(context.Context, string, string) error { return nil }).Create(ctx, cascadeMember, h, "cascade-subject", "child", subject.TypePerson, nil)
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
	if _, err = upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id;  DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; REVOKE UPDATE (undone_at, undone_by_user_id) ON item_completions FROM tendo; ALTER TABLE item_completions DROP CONSTRAINT item_completions_undo_actor_pair_check; ALTER TABLE item_completions DROP COLUMN undone_by_user_id; ALTER TABLE item_completions DROP COLUMN undone_at; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,8,9,10,11,12,13,14)`); err != nil {
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
		t.Fatal("version 7 database accepted by version 15 application")
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
	if _, err := upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id;  DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_subject_id_fkey; ALTER TABLE items DROP CONSTRAINT items_recurrence_all_or_none; ALTER TABLE items DROP COLUMN recurrence_interval_value, DROP COLUMN recurrence_interval_unit, DROP COLUMN recurrence_mode; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,6,7,8,9,10,11,12,13,14)`); err != nil {
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
		t.Fatal("version 5 database accepted by version 15 application")
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
	if err := upAdmin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&max); err != nil || max != 16 {
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
	if _, err := upAdmin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_linked_user_person_only, DROP CONSTRAINT IF EXISTS subjects_linked_membership_fk; DROP INDEX IF EXISTS subjects_linked_membership_idx; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS linked_user_id;  DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DROP TABLE item_completions; DROP TABLE items; ALTER TABLE subjects DROP CONSTRAINT subjects_household_id_id_key; ALTER TABLE IF EXISTS subjects DROP CONSTRAINT IF EXISTS subjects_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS subjects_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS subjects_creation_key_valid, DROP CONSTRAINT IF EXISTS subjects_creation_metadata_all_or_none; ALTER TABLE IF EXISTS subjects DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; ALTER TABLE IF EXISTS items DROP CONSTRAINT IF EXISTS items_household_creator_creation_key_unique, DROP CONSTRAINT IF EXISTS items_creation_fingerprint_length, DROP CONSTRAINT IF EXISTS items_creation_key_valid, DROP CONSTRAINT IF EXISTS items_creation_metadata_all_or_none; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS creation_request_fingerprint, DROP COLUMN IF EXISTS creation_key, DROP COLUMN IF EXISTS created_by_user_id; DROP TRIGGER IF EXISTS items_search_text ON items; DROP FUNCTION IF EXISTS tendo_items_search_text(); DROP INDEX IF EXISTS items_search_text_trgm_idx; ALTER TABLE IF EXISTS items DROP COLUMN IF EXISTS search_text; DROP FUNCTION IF EXISTS tendo_item_search_pattern(text); DROP FUNCTION IF EXISTS tendo_item_search_normalize(text); DELETE FROM tendo_schema_migrations WHERE version=16; DROP INDEX IF EXISTS items_household_archived_done_attention_id_idx; DELETE FROM tendo_schema_migrations WHERE version IN (15,5,6,7,8,9,10,11,12,13,14)`); err != nil {
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
		t.Fatal("version 4 database accepted by version 15 application")
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
	if err := upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 16 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
