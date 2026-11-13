package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
)

// TestMigrationEighteenCanonicalizesMemberPeople runs against a populated
// version-17 fixture: duplicate links, an archived canonical person, a
// zero-link member, a same-name unlinked person, a child and a second household.
func TestMigrationEighteenCanonicalizesMemberPeople(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion20SQL+" ; "+testpostgres.RevertVersion19SQL+" ; "+testpostgres.RevertVersion18SQL); err != nil {
		t.Fatalf("rewind to version 17: %v", err)
	}

	insertID := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newHousehold := func(name string) string {
		return insertID(`INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name)
	}
	newUser := func(login string) string {
		return insertID(`INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login)
	}
	newSubject := func(householdID string, kind, name string, linked any, archived bool, createdAt time.Time) string {
		return insertID(`INSERT INTO subjects(household_id, type, name, linked_user_id, archived, created_at) VALUES ($1::uuid, $2, $3, $4::uuid, $5, $6) RETURNING id::text`,
			householdID, kind, name, linked, archived, createdAt)
	}

	homeA, homeB := newHousehold("member people A"), newHousehold("member people B")
	ownerA := newUser("alice_owner")
	zeroLinked := newUser("bob_member")
	duplicateMember := newUser("carol_duplicate")
	archivedMember := newUser("dana_archived")
	homeBMember := newUser("eve_other_home")
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES
		($1::uuid,$5::uuid,'admin'),($2::uuid,$5::uuid,'member'),($3::uuid,$5::uuid,'member'),($4::uuid,$5::uuid,'member'),
		($6::uuid,$7::uuid,'member')`, ownerA, zeroLinked, duplicateMember, archivedMember, homeA, homeBMember, homeB); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	duplicates := []string{
		newSubject(homeA, "person", "carol canonical", duplicateMember, false, base),
		newSubject(homeA, "person", "carol older duplicate", duplicateMember, false, base.Add(time.Hour)),
		newSubject(homeA, "person", "carol newest duplicate", duplicateMember, false, base.Add(2*time.Hour)),
	}
	archivedCanonical := newSubject(homeA, "person", "dana canonical", archivedMember, true, base)
	archivedDuplicate := newSubject(homeA, "person", "dana duplicate", archivedMember, false, base.Add(time.Hour))
	// An unlinked person with the same display name as a member must never be
	// claimed by name matching.
	sameNameUnlinked := newSubject(homeA, "person", "bob_member", nil, false, base)
	child := newSubject(homeA, "person", "Child", nil, false, base)
	otherHomeSameName := newSubject(homeB, "person", "bob_member", nil, false, base)

	// Creation metadata on a duplicate must survive the unlink untouched.
	fingerprint := `decode(repeat('cd',32),'hex')`
	if _, err := admin.Exec(ctx, `UPDATE subjects SET created_by_user_id=$1::uuid, creation_key='legacy-key', creation_request_fingerprint=`+fingerprint+` WHERE id=$2::uuid`, ownerA, duplicates[2]); err != nil {
		t.Fatal(err)
	}
	// Items and one completion keep their subject associations and facts.
	canonicalItem := insertID(`INSERT INTO items(household_id, subject_id, title) VALUES ($1::uuid,$2::uuid,'canonical item') RETURNING id::text`, homeA, duplicates[0])
	duplicateItem := insertID(`INSERT INTO items(household_id, subject_id, title, archived) VALUES ($1::uuid,$2::uuid,'duplicate item', true) RETURNING id::text`, homeA, duplicates[2])
	archivedItem := insertID(`INSERT INTO items(household_id, subject_id, title, archived) VALUES ($1::uuid,$2::uuid,'archived person item', true) RETURNING id::text`, homeA, archivedCanonical)
	if _, err := admin.Exec(ctx, `INSERT INTO item_completions(id, household_id, item_id, completed_on, completed_by_user_id, prior_workflow_state, item_version_before, idempotency_key, request_fingerprint)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '2026-01-05', $3::uuid, 'open', 1, 'migration-completion', `+fingerprint+`)`, homeA, duplicateItem, ownerA); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 18: %v", err)
	}

	var version int64
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 21 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}

	linkedCount := func(householdID, userID string) int {
		t.Helper()
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE household_id=$1::uuid AND linked_user_id=$2::uuid`, householdID, userID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	for _, user := range []string{ownerA, zeroLinked, duplicateMember, archivedMember} {
		if got := linkedCount(homeA, user); got != 1 {
			t.Fatalf("user %s linked people=%d want 1", user, got)
		}
	}
	if got := linkedCount(homeB, homeBMember); got != 1 {
		t.Fatalf("second household linked people=%d want 1", got)
	}

	var zeroName, zeroType string
	var zeroArchived bool
	if err := admin.QueryRow(ctx, `SELECT name, type, archived FROM subjects WHERE linked_user_id=$1::uuid AND household_id=$2::uuid`, zeroLinked, homeA).Scan(&zeroName, &zeroType, &zeroArchived); err != nil || zeroName != "bob_member" || zeroType != "person" || zeroArchived {
		t.Fatalf("backfilled zero-link person name=%q type=%q archived=%v err=%v", zeroName, zeroType, zeroArchived, err)
	}

	var canonicalLink any
	var canonicalVersion int64
	if err := admin.QueryRow(ctx, `SELECT linked_user_id, version FROM subjects WHERE id=$1::uuid`, duplicates[0]).Scan(&canonicalLink, &canonicalVersion); err != nil || canonicalLink == nil || canonicalVersion != 1 {
		t.Fatalf("canonical link=%v version=%d err=%v", canonicalLink, canonicalVersion, err)
	}
	for _, duplicate := range duplicates[1:] {
		var link any
		var name string
		var rowVersion int64
		if err := admin.QueryRow(ctx, `SELECT linked_user_id, name, version FROM subjects WHERE id=$1::uuid`, duplicate).Scan(&link, &name, &rowVersion); err != nil || link != nil || rowVersion != 2 {
			t.Fatalf("duplicate %s link=%v name=%q version=%d err=%v", duplicate, link, name, rowVersion, err)
		}
		var logged int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM member_person_migration_unlinks WHERE subject_id=$1::uuid AND household_id=$2::uuid AND user_id=$3::uuid`, duplicate, homeA, duplicateMember).Scan(&logged); err != nil || logged != 1 {
			t.Fatalf("duplicate %s log rows=%d err=%v", duplicate, logged, err)
		}
	}
	var metadataKey, metadataActor, metadataFingerprint, duplicateName string
	if err := admin.QueryRow(ctx, `SELECT creation_key, created_by_user_id::text, encode(creation_request_fingerprint,'hex'), name FROM subjects WHERE id=$1::uuid`, duplicates[2]).Scan(&metadataKey, &metadataActor, &metadataFingerprint, &duplicateName); err != nil || metadataKey != "legacy-key" || metadataActor != ownerA || metadataFingerprint != strings.Repeat("cd", 32) || duplicateName != "carol newest duplicate" {
		t.Fatalf("duplicate creation metadata key=%q actor=%q fingerprint=%q name=%q err=%v", metadataKey, metadataActor, metadataFingerprint, duplicateName, err)
	}

	if err := admin.QueryRow(ctx, `SELECT archived, version FROM subjects WHERE id=$1::uuid`, archivedCanonical).Scan(&zeroArchived, &canonicalVersion); err != nil || zeroArchived || canonicalVersion != 2 {
		t.Fatalf("archived canonical archived=%v version=%d err=%v", zeroArchived, canonicalVersion, err)
	}
	var archivedLink any
	if err := admin.QueryRow(ctx, `SELECT linked_user_id FROM subjects WHERE id=$1::uuid`, archivedDuplicate).Scan(&archivedLink); err != nil || archivedLink != nil {
		t.Fatalf("archived duplicate link=%v err=%v", archivedLink, err)
	}

	for label, id := range map[string]string{"same-name unlinked": sameNameUnlinked, "child": child, "other household person": otherHomeSameName} {
		var link any
		var rowVersion int64
		if err := admin.QueryRow(ctx, `SELECT linked_user_id, version FROM subjects WHERE id=$1::uuid`, id).Scan(&link, &rowVersion); err != nil || link != nil || rowVersion != 1 {
			t.Fatalf("%s link=%v version=%d err=%v", label, link, rowVersion, err)
		}
	}

	var itemSubject, itemArchived string
	if err := admin.QueryRow(ctx, `SELECT subject_id::text, archived::text FROM items WHERE id=$1::uuid`, duplicateItem).Scan(&itemSubject, &itemArchived); err != nil || itemSubject != duplicates[2] || itemArchived != "true" {
		t.Fatalf("duplicate item subject=%q archived=%q err=%v", itemSubject, itemArchived, err)
	}
	if err := admin.QueryRow(ctx, `SELECT subject_id::text FROM items WHERE id=$1::uuid`, canonicalItem).Scan(&itemSubject); err != nil || itemSubject != duplicates[0] {
		t.Fatalf("canonical item subject=%q err=%v", itemSubject, err)
	}
	var archivedItemFlag bool
	if err := admin.QueryRow(ctx, `SELECT archived FROM items WHERE id=$1::uuid`, archivedItem).Scan(&archivedItemFlag); err != nil || !archivedItemFlag {
		t.Fatalf("item of archived canonical archived=%v err=%v", archivedItemFlag, err)
	}
	var completions int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, duplicateItem).Scan(&completions); err != nil || completions != 1 {
		t.Fatalf("completions=%d err=%v", completions, err)
	}
	var uniqueIndex bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname='subjects_member_person_unique')`).Scan(&uniqueIndex); err != nil || !uniqueIndex {
		t.Fatalf("unique member-person index=%v err=%v", uniqueIndex, err)
	}
	var runtimeSelect, runtimeInsert, runtimeUpdate, runtimeDelete bool
	if err := admin.QueryRow(ctx, `SELECT
		has_table_privilege('tendo','member_person_migration_unlinks','SELECT'),
		has_table_privilege('tendo','member_person_migration_unlinks','INSERT'),
		has_table_privilege('tendo','member_person_migration_unlinks','UPDATE'),
		has_table_privilege('tendo','member_person_migration_unlinks','DELETE')`).Scan(&runtimeSelect, &runtimeInsert, &runtimeUpdate, &runtimeDelete); err != nil || runtimeSelect || runtimeInsert || runtimeUpdate || runtimeDelete {
		t.Fatalf("runtime audit privileges select=%v insert=%v update=%v delete=%v err=%v", runtimeSelect, runtimeInsert, runtimeUpdate, runtimeDelete, err)
	}

	// Re-running Migrate is a no-op.
	var ledgerBefore int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM tendo_schema_migrations`).Scan(&ledgerBefore); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	var ledgerAfter int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM tendo_schema_migrations`).Scan(&ledgerAfter); err != nil || ledgerAfter != ledgerBefore {
		t.Fatalf("repeat migrate ledger %d -> %d err=%v", ledgerBefore, ledgerAfter, err)
	}
	if got := linkedCount(homeA, duplicateMember); got != 1 {
		t.Fatalf("repeat migrate changed linked people=%d", got)
	}

	// Down is non-destructive and Up is idempotent over the retained rows.
	// Revert 019 first so the ledger can present version 17 exactly.
	down19, err := database.MigrationDownSQL(19)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, down19); err != nil {
		t.Fatalf("migration 019 Down: %v", err)
	}
	down, err := database.MigrationDownSQL(18)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, down); err != nil {
		t.Fatalf("migration 018 Down: %v", err)
	}
	var logTable, uniqueAfterDown bool
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.member_person_migration_unlinks') IS NOT NULL`).Scan(&logTable); err != nil || logTable {
		t.Fatalf("log table after Down=%v err=%v", logTable, err)
	}
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname='subjects_member_person_unique')`).Scan(&uniqueAfterDown); err != nil || uniqueAfterDown {
		t.Fatalf("unique index after Down=%v err=%v", uniqueAfterDown, err)
	}
	var nonUnique bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname='subjects_linked_membership_idx')`).Scan(&nonUnique); err != nil || !nonUnique {
		t.Fatalf("legacy index after Down=%v err=%v", nonUnique, err)
	}
	if got := linkedCount(homeA, duplicateMember); got != 1 {
		t.Fatalf("Down removed links=%d", got)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM tendo_schema_migrations WHERE version IN (18,19)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate up after Down: %v", err)
	}
	if got := linkedCount(homeA, duplicateMember); got != 1 {
		t.Fatalf("up after Down linked people=%d", got)
	}
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 21 || dirty {
		t.Fatalf("round trip version=%d dirty=%v err=%v", version, dirty, err)
	}
}
