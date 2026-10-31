package database

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestApplyMigrationsSortsAndSkipsAppliedVersions(t *testing.T) {
	files := fstest.MapFS{
		"003_third.sql":  {Data: []byte("-- +goose Up\nthird\n-- +goose Down\ndown")},
		"001_first.sql":  {Data: []byte("not a migration")},
		"002_second.sql": {Data: []byte("-- +goose Up\nsecond\n-- +goose Down\ndown")},
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var steps []migrationStep
	err = applyMigrations(files, entries, 1, func(step migrationStep) error {
		steps = append(steps, step)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].version != 2 || steps[0].up != "\nsecond\n" || steps[1].version != 3 || steps[1].up != "\nthird\n" {
		t.Fatalf("steps=%+v", steps)
	}
}

func TestMigrationVersionRejectsInvalidFilename(t *testing.T) {
	files := fstest.MapFS{"bad.sql": {Data: []byte("-- +goose Up\nup\n-- +goose Down\ndown")}}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationVersion(entries[0]); err == nil || err.Error() != "invalid migration filename" {
		t.Fatalf("error=%v", err)
	}
}

func TestMigrationForEntryRequiresDownMarker(t *testing.T) {
	files := fstest.MapFS{"001_first.sql": {Data: []byte("-- +goose Up\nup")}}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationForEntry(files, entries[0], 1); err == nil || err.Error() != "invalid migration" {
		t.Fatalf("error=%v", err)
	}
}

func TestApplyMigrationsMovesVersionWatermark(t *testing.T) {
	files := fstest.MapFS{
		"001_first.sql":  {Data: []byte("-- +goose Up\nfirst\n-- +goose Down\ndown")},
		"001_second.sql": {Data: []byte("-- +goose Up\nsecond\n-- +goose Down\ndown")},
		"002_next.sql":   {Data: []byte("-- +goose Up\nnext\n-- +goose Down\ndown")},
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var applied []int64
	if err := applyMigrations(files, entries, 0, func(step migrationStep) error {
		applied = append(applied, step.version)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[0] != 1 || applied[1] != 2 {
		t.Fatalf("applied versions=%v, want [1 2]", applied)
	}
}

func TestApplyMigrationsAppliesBeforeLaterMalformedFile(t *testing.T) {
	files := fstest.MapFS{
		"001_first.sql": {Data: []byte("-- +goose Up\nfirst\n-- +goose Down\ndown")},
		"002_bad.sql":   {Data: []byte("not a migration")},
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var applied []int64
	err = applyMigrations(files, entries, 0, func(step migrationStep) error {
		applied = append(applied, step.version)
		return nil
	})
	if err == nil || err.Error() != "invalid migration" {
		t.Fatalf("error=%v, want invalid migration", err)
	}
	if len(applied) != 1 || applied[0] != 1 {
		t.Fatalf("applied versions=%v, want [1] before later error", applied)
	}
}
