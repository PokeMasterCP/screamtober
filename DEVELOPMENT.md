# Development

Use Go 1.27.0 and sqlc 1.31.1. Run from the repository root:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go generate ./...
sqlc compile
go test ./...
go vet ./...
```

Edit SQL in `queries/` and regenerate `internal/store/`; never hand-edit generated
code. `sqlc.yaml` reads the Goose migrations as its schema. Keep only queries
needed by the web application. Tests can inspect fixtures with parameterized SQL.

## Data and transactions

The catalog preserves the first stored metadata for each TMDB ID. Challenge entries
may repeat a movie, and carry their own position, service, watched time, and ratings.
Submission references prevent duplicate POST retries while allowing intentional repeats.

Use request contexts for database operations. `withTransaction` supplies a query
handle for atomic operations. Saving a rating and setting the entry's watched time
must commit together; subsequent saves preserve an existing watched time. This
behavior applies to new saves without backfilling historical entries on startup.
Calendar reordering clears and assigns positions in one transaction, retaining IDs.

Handlers enforce authorization. Admin sessions manage movies and household access;
personal sessions submit only their own ratings, which also mark the entry watched.
Scope writes to the requested challenge and entry. The owner role is a household
label, not an administration credential.

## Migrations

Production data and applied Goose migrations must be preserved. Add new migrations
for schema changes and verify upgrades against a disposable database containing
existing data. Never run destructive validation on production. To author migrations:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations -s create describe_change sql
```

Use `-- +goose Up` and `-- +goose Down` sections. Rollback is for disposable test
databases; rolling back the initial migration destroys all application data.

## Pages and logging

Templates share `site_style.html` and the buffered `renderPage` helper. HTML
pages set `Referrer-Policy: strict-origin` so same-origin form POSTs still send
Origin; browsers omit `Sec-Fetch-Site` for HTTP private-IP hosts, and
`no-referrer` can make them send `Origin: null`. Rating
controls and verdicts use `movie_ratings.html` in both featured and carousel cards.
Carousel cards are poster-only; their accessible names identify the movies.
Calendar arrangement intentionally supports drag-and-drop only. Keep server-side
validation, conflict detection, and rollback coverage regardless of the interaction.

Each HTTP request produces one completion event. Handlers call `setRequestEvent`
synchronously to attach the operation, outcome, and relevant error. Independent
startup and lifecycle events have their own records. Never log credentials, cookies,
authorization headers, request bodies, or complete query strings. Validated search
titles are intentionally recorded as `search_term`; invalid input is omitted.
Successful login returns a confirmation page, and explicit navigation is a separate
logged request.
