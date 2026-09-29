// Package migrations bundles the database migrations used at startup.
package migrations

import "embed"

// Files contains the versioned Goose SQL migrations.
//
//go:embed *.sql
var Files embed.FS
