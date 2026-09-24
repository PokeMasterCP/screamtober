package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// databasePath uses one fixed filename; never discover or select other databases.
func databasePath(directory string) (string, error) {
	if directory == "" {
		directory = "data"
	}
	path, err := filepath.Abs(filepath.Join(directory, "screamtober.db"))
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	return path, nil
}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// URI encoding keeps filenames containing '?' or '#' from becoming options.
	params := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"}}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Serialize this small application's writes; pragmas apply to replacement connections too.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err == nil {
		var provider *goose.Provider
		provider, err = goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
		if err == nil {
			_, err = provider.Up(ctx)
		}
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, nil
}

// loggedDB adds each statement's time, including waiting for the single
// connection, to the request's completion log. The driver runs a query to its
// first row before returning, so later rows are read untimed.
type loggedDB struct{ store.DBTX }

func newQueries(db store.DBTX) *store.Queries {
	return store.New(loggedDB{db})
}

func (l loggedDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	defer recordDatabase(ctx, time.Now(), 1)
	return l.DBTX.ExecContext(ctx, query, args...)
}

func (l loggedDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	defer recordDatabase(ctx, time.Now(), 1)
	return l.DBTX.QueryContext(ctx, query, args...)
}

func (l loggedDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	defer recordDatabase(ctx, time.Now(), 1)
	return l.DBTX.QueryRowContext(ctx, query, args...)
}

func withTransaction(ctx context.Context, db *sql.DB, fn func(*store.Queries) error) error {
	started := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	recordDatabase(ctx, started, 0)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(newQueries(tx)); err != nil {
		return err
	}
	defer recordDatabase(ctx, time.Now(), 0)
	return tx.Commit()
}
