package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/subject"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testCreationFingerprint(actor, name string, t subject.Type, linked *string) []byte {
	fields := []string{"tendo.subject.create.v1", strings.ToLower(actor), strings.TrimSpace(name), string(t)}
	if linked != nil {
		value := strings.ToLower(*linked)
		fields = append(fields, value)
	}
	var encoded []byte
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		encoded = append(encoded, size[:]...)
		encoded = append(encoded, field...)
	}
	digest := sha256.Sum256(encoded)
	return digest[:]
}

// TestPreEighteenLinkedCreateReplaySurvivesCanonicalization creates a linked
// person with an Idempotency-Key on the version-17 schema, makes it a
// non-canonical duplicate, migrates to 18 (which unlinks it), then replays the
// original create intent. The replay must return the current unlinked row and
// must not create a second subject or report member_person_exists.
func TestPreEighteenLinkedCreateReplaySurvivesCanonicalization(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
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
	if _, err = upAdmin.Exec(ctx, testpostgres.RevertVersion18SQL); err != nil {
		t.Fatalf("rewind to version 17: %v", err)
	}
	var home, actor string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('pre18 replay','UTC') RETURNING id::text`).Scan(&home); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('pre18_replay_actor') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, actor, home); err != nil {
		t.Fatal(err)
	}
	// Canonical person created earlier so the idempotent linked row becomes the
	// non-canonical duplicate and is unlinked by migration 018.
	if _, err = upAdmin.Exec(ctx, `INSERT INTO subjects(household_id,type,name,linked_user_id,created_at) VALUES($1::uuid,'person','Canonical member person',$2::uuid,'2026-01-01')`, home, actor); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(upApp)
	fingerprint := testCreationFingerprint(actor, "Old linked", subject.TypePerson, &actor)
	created, inserted, err := repo.CreateIdempotent(ctx, home, actor, "pre18-linked", "Old linked", subject.TypePerson, &actor, fingerprint)
	if err != nil || !inserted || created.LinkedUserID == nil || *created.LinkedUserID != actor {
		t.Fatalf("v17 linked create=%+v inserted=%v err=%v", created, inserted, err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatalf("migrate 18: %v", err)
	}
	unlinked, err := NewRepository(upApp).Get(ctx, home, created.ID)
	if err != nil || unlinked.LinkedUserID != nil || unlinked.Version != 2 || unlinked.Name != "Old linked" {
		t.Fatalf("canonicalization=%+v err=%v", unlinked, err)
	}
	var count int
	if err = upAdmin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE household_id=$1::uuid`, home).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rows before replay=%d err=%v", count, err)
	}
	allow := func(context.Context, string, string) error { return nil }
	service := subject.NewService(NewRepository(upApp), allow, allow)
	replayed, err := service.Create(ctx, actor, home, "pre18-linked", "Old linked", subject.TypePerson, &actor)
	if err != nil {
		t.Fatalf("replay after canonicalization: %v", err)
	}
	if replayed.ID != created.ID || replayed.LinkedUserID != nil || replayed.Version != 2 {
		t.Fatalf("replayed=%+v want id=%s linked=nil version=2", replayed, created.ID)
	}
	if err = upAdmin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE household_id=$1::uuid`, home).Scan(&count); err != nil || count != 2 {
		t.Fatalf("replay created a duplicate: rows=%d err=%v", count, err)
	}
	var canonicalLink any
	if err = upAdmin.QueryRow(ctx, `SELECT linked_user_id FROM subjects WHERE id<>$1::uuid AND household_id=$2::uuid`, created.ID, home).Scan(&canonicalLink); err != nil || canonicalLink == nil {
		t.Fatalf("canonical link=%v err=%v", canonicalLink, err)
	}
}
