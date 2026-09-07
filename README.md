# Screamtober

Screamtober is a web application for an annual October movie challenge: build a
31-movie watchlist, track viewing progress, and rate movies, with challenge history
organized by year.

Designed for personal use by 1–4 people, with public viewing available to everyone.
The current Go application serves a placeholder page with shared-token sign-in
for testing, with SQLite storage and Goose migrations initialized at startup.
Individual accounts and movie tracking are still to come. There is no public registration.

## Setup

An `AUTH_TOKEN` of at least 32 bytes is required; the app will not start without it.
Generate one in your terminal:

```sh
export AUTH_TOKEN="$(openssl rand -hex 32)"
```

Keep this token private, share it only with the people who should sign in, and
reuse it between runs. Open `/login` and enter the token to sign in; you can display
it in your terminal with `printf '%s\n' "$AUTH_TOKEN"`. Everyone else can browse
public pages without signing in. Sessions expire after 12 hours; restarting the
app signs everyone out.

## Run locally

Install Go 1.27.0 (see `go.mod`), complete the setup above, then run from the
repository root:

```sh
AUTH_INSECURE_COOKIE=true go run .
```

Open [localhost:8080](http://127.0.0.1:8080). Restart the app after changing code or
templates. Set `ADDR` to use a different listening address.

## Run with Docker

After exporting `AUTH_TOKEN`, build and start the container from the repository root:

```sh
docker build -t screamtober .
docker run --rm --name screamtober -p 127.0.0.1:8080:8080 \
  -v screamtober-data:/data \
  -e AUTH_TOKEN -e AUTH_INSECURE_COOKIE=true screamtober
```

Open [localhost:8080](http://127.0.0.1:8080). Rebuild the image after making changes.
To stop the container, run `docker stop screamtober` in another terminal.

`AUTH_INSECURE_COOKIE=true` is only for local HTTP testing. For hosted HTTPS,
configure `AUTH_TOKEN` as a runtime secret and leave `AUTH_INSECURE_COOKIE` unset.

## Database and migrations

Local runs create `data/screamtober.db`; set `DATABASE_PATH` to override the file
location. The parent directory is created if needed. Database files are ignored by
Git and excluded from Docker builds. SQLite uses WAL mode, foreign-key enforcement,
a five-second busy timeout, and a single pooled connection. The pure Go driver
keeps the Docker build independent of CGO.

Goose SQL migrations in `migrations/` are embedded in the binary and applied before
the HTTP server starts. A database or migration error prevents startup. A
successful connection and migration check emit an info-level `database initialized`
log event with the database path on every startup. The initial
baseline establishes Goose version tracking; the second migration adds the challenge
schema described below. sqlc generates database operations from SQL in `queries/`.
Sign-in sessions remain in memory, and the shared token does not yet identify
individual database users.

### Schema

| Table | Purpose and constraints |
| --- | --- |
| `users` | Display name and owner/member role; at most one owner and three members |
| `movies` | Shared catalog with a unique TMDB ID and nullable cached metadata |
| `challenges` | One challenge per year |
| `challenge_movies` | Ordered slots 1–31, unique within each challenge; shared `watched_at` |
| `ratings` | One whole-star score from 1–5 per user per challenge entry |

Watchlists can have fewer than 31 entries and can repeat a movie in separate slots.
Each appearance has independent viewing status and ratings, including across years.
Users may edit their own ratings; the rating upsert also updates `updated_at`.
Aggregate scores will be calculated from submitted ratings, excluding missing votes.
Foreign keys reject deletion of referenced records to prevent implicit loss of history.
Reordering must use a transaction that handles the unique slot constraint.

Only the owner may administer challenges, the catalog, ordering, and shared viewing
status. Members may rate entries; public visitors have read-only access. These are
requirements for the future Go handlers: the schema does not authorize requests,
and this migration does not add endpoints or account provisioning. Account setup
must create the owner; the database allows an empty household for bootstrapping.

Rolling back migration 2 drops all five application tables and their data. Use
rollback only on disposable databases unless that data loss is explicitly intended.

For authoring and inspecting migrations, install the pinned Goose CLI:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations -s create add_movie_catalog sql
goose -dir migrations sqlite3 ./data/screamtober.db status
```

Put schema changes in new migrations with `-- +goose Up` and `-- +goose Down`
sections; never rewrite migrations already applied to a deployed database. Restart
the local app (or rebuild the Docker image) to apply new migrations. Test rollback
only against disposable databases. Goose [migration documentation](https://pressly.github.io/goose/documentation/cli-commands/)
describes the CLI commands.

### Railway storage

Mount a persistent Railway volume at `/data`, writable by container UID/GID
`65532:65532`, and keep `DATABASE_PATH=/data/screamtober.db`. Railway supplies
`RAILWAY_VOLUME_MOUNT_PATH`; when `RAILWAY_PROJECT_ID` is present, startup rejects
a missing volume configuration or a database path outside that volume. Use direct
paths within the mount, without symlinks to other storage. The app validates path
configuration; deployment must still verify that a persistent volume is mounted.

Run one application instance. Do not enable a public Railway endpoint: route
public traffic through Cloudflare Tunnel to the application's private address.
The Docker port mapping above is only for local testing.

Keep Railway volume snapshots and periodic portable SQLite backups using a
SQLite-consistent method (for example, SQLite's `.backup` command). Do not copy a
live database file without its WAL state. Verify restoration to a separate database
before relying on backups; backup scheduling is not configured by this setup.

## SQL queries

Install the pinned sqlc version, then regenerate from the repository root:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go generate ./...
```

`sqlc.yaml` reads Goose migrations directly as its schema source, using sqlc's
[migration support](https://docs.sqlc.dev/en/latest/howto/ddl.html). Edit SQL in
`queries/`, regenerate, and commit the resulting `internal/store/` files alongside
the SQL. Do not edit generated Go files. Docker builds use that generated code and
do not need sqlc installed.

The initial queries cover user records, cached movie upserts, challenges by year,
ordered watchlists, shared watched status, and editable ratings. Movie upserts
replace the cached metadata (including nullable fields) while retaining the movie
ID. Rating upserts retain the rating ID and refresh its timestamp. Watch-status and
rating writes require matching challenge and entry IDs; mismatches return
`sql.ErrNoRows`. Rating averages can be calculated from the returned votes.

Create a query handle with `store.New(db)` using the open `*sql.DB`, and pass the
request context to each operation. For atomic changes, call `db.BeginTx`, use
`queries.WithTx(tx)` for every operation, and commit or roll back the transaction.
Queries do not begin transactions automatically.

This is an internal data-access layer, not authorization. Future Go handlers must
check owner permissions before administration or watched-status writes and derive
the rating user ID from the authenticated session. No new HTTP endpoints are exposed.
Removal and reordering queries will follow with their transactional feature logic.

## Development checks

```sh
sqlc compile
go test ./...
go vet ./...
```
