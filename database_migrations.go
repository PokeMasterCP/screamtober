package main

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"
)

func newMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectSQLite3, db, migrations,
		goose.WithDisableGlobalRegistry(true),
		goose.WithGoMigrations(goose.NewGoMigration(2,
			&goose.GoFunc{RunTx: addViewingServiceColumn},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				// Rollback discards viewing-service selections only, retaining all other data.
				_, err := tx.ExecContext(ctx, "ALTER TABLE challenge_movies DROP COLUMN viewing_service")
				return err
			}},
		)),
	)
}

// An earlier PR build added this column to migration 1. Those databases still
// report version 1. Adopt the compatible column without losing saved choices;
// normal production databases receive the column here. Both run transactionally
// with Goose's version record. Keep schema/viewing_service.sql in sync for sqlc.
func addViewingServiceColumn(ctx context.Context, tx *sql.Tx) error {
	var columnType string
	var notNull int
	var defaultValue sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT type, "notnull", dflt_value
 FROM pragma_table_info('challenge_movies') WHERE name = 'viewing_service'`).Scan(&columnType, &notNull, &defaultValue)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "ALTER TABLE challenge_movies ADD COLUMN viewing_service TEXT NOT NULL DEFAULT ''")
		return err
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(columnType, "TEXT") || notNull != 1 || !defaultValue.Valid || defaultValue.String != "''" {
		return errors.New("existing challenge_movies.viewing_service has an incompatible definition; expected TEXT NOT NULL DEFAULT ''")
	}
	return nil
}
