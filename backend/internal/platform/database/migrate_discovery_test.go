package database

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestDiscoverMigrations(t *testing.T) {
	files := fstest.MapFS{
		"003_third.sql":  {Data: []byte("-- +goose Up\nthird\n-- +goose Down\ndown")},
		"001_first.sql":  {Data: []byte("-- +goose Up\nfirst\n-- +goose Down\ndown")},
		"002_second.sql": {Data: []byte("-- +goose Up\nsecond\n-- +goose Down\ndown")},
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := discoverMigrations(files, entries, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].version != 2 || steps[0].up != "\nsecond\n" || steps[1].version != 3 || steps[1].up != "\nthird\n" {
		t.Fatalf("steps=%+v", steps)
	}
}

func TestDiscoverMigrationsRejectsInvalidFilename(t *testing.T) {
	files := fstest.MapFS{"bad.sql": {Data: []byte("-- +goose Up\nup\n-- +goose Down\ndown")}}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverMigrations(files, entries, 0); err == nil || err.Error() != "invalid migration filename" {
		t.Fatalf("error=%v", err)
	}
}

func TestDiscoverMigrationsRequiresDownMarker(t *testing.T) {
	files := fstest.MapFS{"001_first.sql": {Data: []byte("-- +goose Up\nup")}}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverMigrations(files, entries, 0); err == nil || err.Error() != "invalid migration" {
		t.Fatalf("error=%v", err)
	}
}

func TestDiscoverMigrationsSkipsAppliedVersions(t *testing.T) {
	files := fstest.MapFS{
		"001_bad.sql":  {Data: []byte("not a migration")},
		"002_next.sql": {Data: []byte("-- +goose Up\nup\n-- +goose Down\ndown")},
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := discoverMigrations(files, entries, 1)
	if err != nil || len(steps) != 1 || steps[0].version != 2 {
		t.Fatalf("steps=%+v err=%v", steps, err)
	}
}
