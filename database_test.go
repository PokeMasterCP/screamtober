package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestDatabasePersistenceAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "test?#.db")
	db, err := openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
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
	db, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM existing_data").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("persisted value = %q, error = %v", value, err)
	}
	var version int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil || version != 2 {
		t.Fatalf("migration version = %d, error = %v", version, err)
	}
}

func TestDatabasePath(t *testing.T) {
	volume := t.TempDir()
	for _, tt := range []struct {
		name, path, volume string
		railway, wantError bool
	}{
		{name: "local default"},
		{name: "volume default", volume: volume, railway: true},
		{name: "volume explicit", path: filepath.Join(volume, "app.db"), volume: volume, railway: true},
		{name: "missing volume", railway: true, wantError: true},
		{name: "outside volume", path: filepath.Join(volume, "..", "app.db"), volume: volume, railway: true, wantError: true},
		{name: "volume itself", path: volume, volume: volume, railway: true, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, err := databasePath(tt.path, tt.volume, tt.railway)
			if (err != nil) != tt.wantError {
				t.Fatalf("path = %q, error = %v", path, err)
			}
			if err == nil && !filepath.IsAbs(path) {
				t.Fatalf("database path is not absolute: %q", path)
			}
		})
	}
}

func TestOpenDatabaseCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := openDatabase(ctx, filepath.Join(t.TempDir(), "test.db")); err == nil {
		db.Close()
		t.Fatal("expected canceled initialization to fail")
	}
}
