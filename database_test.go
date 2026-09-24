package main

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestDatabasePersistenceAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "test?#.db")
	db, schema, err := openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if schema.version != 3 || fmt.Sprint(schema.applied) != "[1 2 3]" {
		t.Fatalf("fresh schema = %+v", schema)
	}
	t.Cleanup(func() { db.Close() })
	for pragma, want := range map[string]string{"foreign_keys": "1", "journal_mode": "wal", "busy_timeout": "5000"} {
		var got string
		if err := db.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %q, want %q (error: %v)", pragma, got, want, err)
		}
	}
	// Simulate existing application data to verify migration operations preserve it.
	if _, err := db.ExecContext(ctx, "CREATE TABLE existing_data (value TEXT NOT NULL); INSERT INTO existing_data VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, schema, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if schema.version != 3 || schema.applied == nil || len(schema.applied) != 0 {
		t.Fatalf("reopened schema = %+v", schema)
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM existing_data").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("persisted value = %q, error = %v", value, err)
	}
	var version int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil || version != 3 {
		t.Fatalf("migration version = %d, error = %v", version, err)
	}
}

func TestDatabasePath(t *testing.T) {
	directory := t.TempDir()
	for _, tt := range []struct{ name, directory, want string }{
		{"local default", "", filepath.Join("data", "screamtober.db")},
		{"custom directory", directory, filepath.Join(directory, "screamtober.db")},
		{"relative directory", "custom-data", filepath.Join("custom-data", "screamtober.db")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := databasePath(tt.directory)
			want, absErr := filepath.Abs(tt.want)
			if err != nil || absErr != nil || got != want {
				t.Fatalf("path = %q, want %q, error = %v", got, want, err)
			}
		})
	}
}

func TestOpenDatabaseCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, _, err := openDatabase(ctx, filepath.Join(t.TempDir(), "test.db")); err == nil {
		db.Close()
		t.Fatal("expected canceled initialization to fail")
	}
}
