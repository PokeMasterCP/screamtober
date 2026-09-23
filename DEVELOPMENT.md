# Development

See [README.md](README.md) to run the app and [AGENTS.md](AGENTS.md) for product,
security, and contribution rules.

## Tools and checks

Use Go 1.27.0 (`go.mod`). From the repository root, format changed Go files with
`gofmt`, then run the checks used by CI:

```sh
go test ./...
go vet ./...
```

After editing SQL in `queries/` or changing the schema, regenerate and validate
with sqlc 1.31.1, then rerun the checks:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go generate ./...
sqlc compile
```

`sqlc.yaml` reads `migrations/` as its schema and generates `internal/store/`.
Never hand-edit generated code. Keep only queries needed by the app; tests can
inspect fixtures with parameterized SQL.

## Database changes

Add new Goose migrations; preserve applied migrations and production data:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations -s create describe_change sql
```

Use `-- +goose Up` and `-- +goose Down` sections. Test fresh installs and upgrades
on disposable databases containing representative history. Rollback is for
disposable test databases; the initial migration's rollback deletes all app data.

The catalog retains the first stored metadata for each TMDB ID. Repeated picks
have their own position, service, watched time, and ratings. Submission references
prevent duplicate POST retries while allowing intentional repeats.

Use `withTransaction` for atomic changes. Rating saves and marking an entry watched
commit together, preserving its first watched time without backfilling history at
startup. Calendar reordering clears and assigns positions in one transaction,
retaining entry IDs. Scope writes to the requested challenge and entry.

## Pages

Use `site_style.html`, the buffered `renderPage` helper, and `movie_ratings.html`
for shared rating controls and verdicts. Poster-only carousel cards must retain
accessible movie names. Home page summaries (month calendar, countdown, critics,
top pick, unrated reminders) derive from the challenge's existing movie and rating
queries; keep them free of extra queries and live TMDB requests. Calendar nights
use `moviePosterThumbURL` for smaller TMDB images. Keep server-side validation, conflict detection, and
rollback coverage for calendar arrangement.

Keep `Referrer-Policy: strict-origin`: browsers may omit `Sec-Fetch-Site` on HTTP
private-IP hosts, and `no-referrer` can produce `Origin: null` on form POSTs,
breaking cross-origin request protection.

## Logging

Write structured JSON with request ID, client IP, method, path, status, duration,
and response size. Use one completion event per HTTP request; handlers call
`setRequestEvent` synchronously to add operation, outcome, and relevant errors.
Avoid separate handler and access logs for the same event. Independent startup
and lifecycle events get their own records.

Never log credentials, cookies, authorization headers, request bodies, or complete
query strings. Validated search titles are intentionally recorded as `search_term`;
invalid input is omitted. `LOG_LEVEL` accepts `debug`, `info` (default), `warn`, or
`error`.

Successful login returns a 200 confirmation page with a continuation link instead
of an automatic redirect. Explicit navigation is a separate logged request; never
suppress real requests to reduce log counts.
