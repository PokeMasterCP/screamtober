# Screamtober

Screamtober is a web application for an annual October movie challenge: build a
31-movie watchlist, track viewing progress, and rate movies, with challenge history
organized by year.

Designed for personal use by 1–4 people, with public viewing available to everyone.
The current Go application serves public challenge pages from SQLite with
individual token sign-in and an administrator-only household portal. Personal
rating controls are available on each movie card. There is no public registration.

## Challenge pages

- `GET /` shows the current calendar year's watchlist (using the server's local
  time), or a pending lineup message when no movies have been added. “Up next”
  highlights the first unwatched entry: scheduled movies in day order, then
  unscheduled movies in the order added. Once all selected movies are watched,
  the page shows “You’re all caught up.”
- `GET /challenges/{year}` shows that year's ordered watchlist, shared watched status,
  individual ratings, and the average of submitted ratings. Unknown or malformed
  years return 404.

Year links let visitors browse earlier challenges. Pages read SQLite on each request
and use cached movie metadata; no live TMDB API call is needed. The featured movie
has a large poster, with the rest of the lineup in a horizontal carousel. Swipe,
scroll, or use the arrow buttons to browse; keyboard users can focus the lineup
and use arrow keys, Home, or End. Movie cards show posters and full descriptions,
with equal-sized cards throughout the carousel. Swiping and scrolling also work
without JavaScript.
Posters load from TMDB's image service; unavailable posters have a placeholder.
Entries without votes show “No ratings yet.” Database failures return a generic
500 response and enrich the existing request log with the failed operation.

Sign in with your personal token to choose 1–5 whole stars on a movie card, then
select **Save rating**. Your existing score is selected when you return; change it
and select **Update rating** to edit your vote. Everyone can see individual scores
and the household average. Each appearance of a movie has separate ratings, even
within the same year. Administrator sessions cannot submit personal ratings.

## Setup

An `ADMIN_TOKEN` of at least 32 bytes is required. Generate a random value once:

```sh
export ADMIN_TOKEN="$(openssl rand -hex 32)"
```

Keep it in your password manager and reuse it between runs. For deployment, set it as
an application runtime secret. **`ADMIN_TOKEN` replaces the old shared `AUTH_TOKEN`**;
the old environment variable no longer grants access. Never share the administrator
token with household members.

## TMDB movie lookup

Set **`TMDB_API_KEY`** to your TMDB **v3 API key**, available in your
[TMDB API settings](https://www.themoviedb.org/settings/api). This is distinct
from the API Read Access Token. Supply it through your shell environment or
runtime secret configuration; do not commit the key to the repository.

With that variable exported, retrieve a movie by its TMDB ID:

```sh
go run ./cmd/movie-info 11
```

The command prints JSON with the title, overview, release date, runtime in
minutes, genres, and poster/backdrop paths. Unknown optional values may be null
or empty. Image paths are TMDB-relative paths, not complete image URLs.
It uses TMDB's [movie details endpoint](https://developer.themoviedb.org/reference/movie-details)
and [API key authentication](https://developer.themoviedb.org/docs/authentication-application).

Search by title using the same environment variable:

```sh
go run ./cmd/movie-search "Halloween"
go run ./cmd/movie-search -year 1978 "Halloween"
go run ./cmd/movie-search -page 2 "Halloween"
```

Put flags before the quoted title. The command uses TMDB's
[movie search endpoint](https://developer.themoviedb.org/reference/search-movie)
and returns JSON with matching titles, TMDB IDs, release dates, overviews, and
poster paths, plus page and total counts. `-year` filters the primary release
year. Searches exclude adult results and fetch one page at a time (default 1,
maximum 500); an empty results array means no matches. Select an ID and use
`movie-info` for full details. The client exposes this as
`SearchMovies(ctx, title, tmdb.SearchOptions{Year: 1978})`.

The reusable `internal/tmdb` client accepts a context and has a ten-second HTTP
timeout. It reports missing configuration, invalid IDs, unavailable movies,
rejected credentials, rate limits, and upstream failures without exposing the
key or upstream error bodies. Requests are not automatically retried.

Sign in as administrator and choose **Search movies**, or open
`/admin/movies/search`. Choose a challenge year, search for a title, and select a result by title and
release year. Results are shown ten at a time; use **Next** and **Previous** to page
through more matches. Choose a service under **Watching on**, or leave **Not decided**
selected. The choice belongs to that yearly pick, so repeats can use different
services. Its logo appears on the challenge page as **Watching on** or **Watched on**.
This records your choice; it does not check regional streaming availability.
Click **Add to [year]** to save it to the shared catalog and add an
unscheduled pick for that year. Each year allows up to 31 picks, including repeats.
Retrying the same selection does not add another pick; search again to intentionally
add a repeat. Existing catalog metadata and previous years are preserved.
If your results expire (after ten minutes) or the app restarts, search again.
Personal sessions and visitors cannot access this admin tool.

Choose **Arrange movies** in the admin panel to schedule a year’s added movies.
Drag posters from the unscheduled tray onto the 31-day October calendar; dropping
onto an occupied day swaps the two entries. Drag a poster back to the tray to
unschedule it. Arranging is drag-and-drop only and requires JavaScript and a
pointer or touch device. Choose **Save arrangement** to
keep your changes. Ratings and watched status stay with each entry when it moves.
If another tab changes the lineup, reload and arrange the latest version.

Set `TMDB_API_KEY` in the web server environment and restart the app. In Docker,
add `-e TMDB_API_KEY` to either deployment command after exporting the variable.
The web app still starts without the key and serves cached challenge data;
search displays a configuration message until the key is supplied. The CLI
commands remain local developer tools and are not bundled in the Docker image.
Watchlist editing will follow later.

## Logging

Logs are structured JSON. Prefer **one event per log record**: each HTTP request
has one completion record containing its request ID, client IP, method, path,
status, duration, and response size. Handlers enrich that record with the outcome
or error instead of emitting a duplicate event. Startup and lifecycle events have
their own records. Tokens, cookies, authorization headers, bodies, and query
strings are excluded. Validated movie titles are intentionally logged separately
as `search_term`. `LOG_LEVEL` accepts `debug`, `info` (default), `warn`, or
`error`; records below that threshold are filtered.

Movie searches use the stable message `movie search`, with `outcome` describing
`success`, `invalid_query`, `not_configured`, `rate_limited`, or `upstream_failure`.
Search attempts include the trimmed, validated `search_term` on success and
upstream failure; invalid input is omitted. They omit a redundant `operation` field. Opening the form without submitting a
query remains an ordinary `http request` event.

Successful personal and administrator sign-ins return a 200 confirmation page
with a continuation link, avoiding an automatic redirect GET. Following the link
is a separate navigation request and is logged normally. Refreshing the
confirmation page may prompt the browser to resubmit the form. Existing failed
sign-in and other redirects retain their behavior.

## Household onboarding

1. Visit `/login` and enter the administrator token to open household management.
2. Create your owner profile at `/admin/users`. Copy its personal login token.
3. Create up to three member profiles and copy their individual tokens.
4. Deliver each token privately outside the app. Each person signs in at `/login`.
5. Sign out of admin before entering your own personal token on that same page.

Personal tokens are generated from 32 cryptographically random bytes and shown only
in the response that creates them. SQLite stores only their SHA-256 hashes. Tokens
are not included in URLs or logs and cannot be retrieved later. If a token is lost,
use **Replace login token**; the old token and all of that person's sessions stop
working. **Disable access** removes their token and signs them out, preserving their
profile and ratings. A replacement token re-enables a disabled profile. Disabled
profiles still count toward the one-owner/three-member limit.

The owner label identifies your household profile; it does **not** grant admin
permissions through your personal token. `/login` accepts either token type and
chooses the appropriate session. Invalid tokens receive the same generic error.
Admin and personal sessions use separate cookies but are mutually exclusive in the
same browser. Signing in as admin revokes the browser's previous personal session.
While admin is active, product pages redirect to the panel and product writes or
another sign-in are rejected. Use **Sign out of admin** to return to `/login`, then
enter a personal token. Expiring or ending admin does not restore the old personal
session. Admin sessions last one hour; personal sessions last
12 hours. Both are stored in memory, so restarting the app signs everyone out.
Personal login tokens persist in SQLite until replaced or disabled. Rotate the
runtime admin token and restart the app to revoke administrator access.

The backend checks the current personal credential in SQLite on each authenticated
request, so revocation does not depend only on clearing the in-memory session map.
All portal mutations require an admin session and POST, with the app's cross-origin
request protection. Management pages and token responses use `Cache-Control:
no-store`. To retain portability with the single-instance setup, no external identity
provider or session service is needed.

| Route | Action |
| --- | --- |
| `GET /admin/login` | Compatibility redirect to `/login`; no separate login form |
| `POST /admin/logout` | Clear the browser's sessions and return to `/login` |
| `GET /admin/users` | Household access management |
| `POST /admin/users` | Create a profile and its personal token atomically |
| `POST /admin/users/{id}/rename` | Update the display name |
| `POST /admin/users/{id}/token` | Replace the token and re-enable access atomically |
| `POST /admin/users/{id}/disable` | Disable access and remove its credential atomically |
| `GET/POST /login` | Unified sign-in; admin token opens the panel, personal token opens the product |
| `POST /logout` | End only the personal session |

## Run locally

Install Go 1.27.0 (see `go.mod`), complete the setup above, then run from the
repository root:

```sh
AUTH_INSECURE_COOKIE=true go run .
```

Open [localhost:8080](http://127.0.0.1:8080). Restart the app after changing code or
templates. Set `ADDR` to use a different listening address.

## Deploy with Docker

Use the same image on any Docker-compatible host, with or without Cloudflare
Tunnel. Export `ADMIN_TOKEN` as described above, then build:

```sh
docker build -t screamtober .
```

### Without Cloudflare Tunnel (default)

```sh
docker run -d --name screamtober --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v screamtober-data:/data \
  -e ADMIN_TOKEN -e CLOUDFLARE_TUNNEL=false screamtober
```

For hosted use, configure your HTTPS reverse proxy or platform ingress to forward
to the app's HTTP port 8080. This example binds the published port to host
loopback for a host-based proxy; adapt networking to your environment. The Go
server does not terminate TLS. The operator manages HTTPS and edge protections.

For local HTTP testing, add `-e AUTH_INSECURE_COOKIE=true` and open
[localhost:8080](http://127.0.0.1:8080). Leave that setting unset for hosted HTTPS.

### With Cloudflare Tunnel

Create a remotely managed tunnel and configure its public hostname to forward to
`http://screamtober:8080`. Supply its token as the runtime environment variable
`TUNNEL_TOKEN` for the connector, then run:

```sh
docker network create screamtober-net
docker run -d --name screamtober --restart unless-stopped \
  --network screamtober-net -v screamtober-data:/data \
  -e ADMIN_TOKEN -e CLOUDFLARE_TUNNEL=true screamtober
docker run -d --name cloudflared --restart unless-stopped \
  --network screamtober-net -e TUNNEL_TOKEN \
  cloudflare/cloudflared:latest tunnel --no-autoupdate run
```

The app has no published port in this mode. Keep it private and allow public
traffic only through the tunnel; services with internal access must be trusted.
The connector needs outbound network access. Select and pin a connector version
or digest for a repeatable deployment.
See [Cloudflare Tunnel setup](https://developers.cloudflare.com/tunnel/setup/)
and [connector run parameters](https://developers.cloudflare.com/tunnel/advanced/run-parameters/).
Configure desired edge protections explicitly; a tunnel alone does not enable
every WAF, rate-limiting, or bot-protection feature.

These are alternative examples: stop and remove an existing app container before
switching modes, retaining its named data volume. Rebuild the image to deploy
code changes. Run only one app instance against the database.

### Deployment configuration and client IPs

| Variable | Behavior |
| --- | --- |
| `CLOUDFLARE_TUNNEL` | Unset/false: socket peer IP. True: trust a valid `CF-Connecting-IP` supplied through the private tunnel path. Invalid boolean values prevent startup. |
| `ADDR` | Docker defaults to `:8080`; local Go runs default to `127.0.0.1:8080`. |
| `DATABASE_DIR` | Directory containing `screamtober.db`: Docker defaults to `/data`, local Go runs to `data`. |
| `ADMIN_TOKEN` | Required administrator secret, at least 32 bytes. |
| `LOG_LEVEL` | `debug`, `info` (default), `warn`, or `error`. |
| `AUTH_INSECURE_COOKIE` | Default false; true permits cookies over HTTP for local development only. |

All settings above can be supplied at container startup with `docker run -e` or
your deployment platform’s environment configuration. Dockerfile `ENV` entries
are defaults, not fixed values. For example, to change the listening port:

```sh
docker run -d --name screamtober --restart unless-stopped \
  -p 127.0.0.1:9090:9090 -v screamtober-data:/data \
  -e ADMIN_TOKEN -e ADDR=:9090 screamtober
```

Update the container port mapping or tunnel origin URL to match `ADDR`.
`EXPOSE 8080` is image metadata; it does not restrict the listening port.
`TUNNEL_TOKEN` is configured separately on the connector container.

`CLOUDFLARE_TUNNEL` declares the deployment's trust boundary; it does not create
a tunnel, change networking, or configure TLS. No proxy CIDR list is required.
With tunnel mode enabled, missing, malformed, or duplicate `CF-Connecting-IP`
headers fall back to the socket peer IP. Only a single valid IPv4 or IPv6 value
is accepted. Without tunnel mode, the header is ignored even when present.

`X-Forwarded-For` and forwarded scheme headers are not trusted in either mode.
With a normal reverse proxy, logs therefore show the proxy's socket IP. Cloudflare
Pseudo IPv4 should be Off or Add Header to preserve original IPv6 addresses;
Overwrite Headers replaces `CF-Connecting-IP`. See
[Cloudflare's header documentation](https://developers.cloudflare.com/fundamentals/reference/http-headers/#cf-connecting-ip).

## Database and migrations

Production databases must be preserved. Before upgrading, take a SQLite-consistent
backup of your persistent database (see the backup instructions below). Keep the
existing `DATABASE_DIR` volume mounted and restart with the new application image;
new Goose migrations apply automatically. Do not delete or recreate the database.
The viewing-service update adds a column in place; existing picks start as
**Not decided**, preserving movies, challenge history, watched status, profiles,
personal tokens, and ratings.

Local runs create `data/screamtober.db`; set `DATABASE_DIR` to change its directory.
The app opens that directory’s `screamtober.db` directly, creating it when missing;
it does not search for database files. The directory is created if needed. Database files are ignored by
Git and excluded from Docker builds. SQLite uses WAL mode, foreign-key enforcement,
a five-second busy timeout, and a single pooled connection. The pure Go driver
keeps the Docker build independent of CGO.

The application applies its schema automatically before starting the HTTP server.
A database or migration failure prevents startup. Fresh databases include the
movie catalog, yearly picks, ratings, and household access tables. Sign-in
sessions remain in memory, so restarting signs everyone out.

### Schema

| Table | Purpose and constraints |
| --- | --- |
| `users` | Display name, owner/member label, and disabled status; at most one owner and three members |
| `user_tokens` | One hashed personal login token per provisioned user |
| `movies` | Shared catalog with a unique TMDB ID and nullable cached metadata |
| `challenges` | One challenge per year |
| `challenge_movies` | Ordered slots 1–31, unique within each challenge; shared `watched_at` |
| `ratings` | One whole-star score from 1–5 per user per challenge entry |

Watchlists can have fewer than 31 entries and can repeat a movie in separate slots.
Each appearance has independent viewing status and ratings, including across years.
Users may edit their own ratings; the rating upsert also updates `updated_at`.
Aggregate scores are calculated from submitted ratings, excluding missing votes.
Foreign keys reject deletion of referenced records to prevent implicit loss of history.
Reordering must use a transaction that handles the unique slot constraint.

Administration requires the runtime admin credential. Personal tokens identify
rating authors, including the owner; visitors have read-only access. The schema
does not authorize HTTP requests. The database allows an empty household for
bootstrapping, with profiles and credentials created from the admin portal.

Rolling back the initial schema deletes all application tables and their data.
Use rollback only against disposable databases.

For authoring and inspecting migrations, install the pinned Goose CLI:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations -s create add_movie_catalog sql
goose -dir migrations sqlite3 ./data/screamtober.db status
```

Goose migrations use `-- +goose Up` and `-- +goose Down` sections. Never edit
applied migrations now that production is live; add a new migration instead. Restart
the local app (or rebuild the Docker image) to apply new migrations. Test rollback
only against disposable databases. Goose [migration documentation](https://pressly.github.io/goose/documentation/cli-commands/)
describes the CLI commands.

### Persistent storage and backups

In both Docker modes, mount persistent storage at `/data`, writable by container
UID/GID `65532:65532`. The examples use the named volume `screamtober-data`.
For a different mount location, set `DATABASE_DIR` to that location. Use direct
paths without symlinks to other storage. Existing deployments using a custom
database filename must arrange for that database to be named `screamtober.db`
in the configured directory before starting this version; the app does not
automatically move or rename existing data.

The app does not verify whether the directory is a persistent mount.
Deployment must verify the mount and preserve it across container replacement.
Local Go runs without a volume setting use `data/screamtober.db`.

Keep volume snapshots where available and periodic portable SQLite backups using
a SQLite-consistent method, such as SQLite's `.backup` command. Do not copy a
live database file without its WAL state. Verify restoration to a separate
database before relying on backups; backup scheduling is not configured here.

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

The queries cover user records and credential management, cached movie upserts, challenges by year,
ordered watchlists, shared watched status, and editable ratings. Movie upserts
replace the cached metadata (including nullable fields) while retaining the movie
ID. Rating upserts retain the rating ID and refresh its timestamp. Watch-status and
rating writes require matching challenge and entry IDs; mismatches return
`sql.ErrNoRows`. Rating averages can be calculated from the returned votes.

Create a query handle with `store.New(db)` using the open `*sql.DB`, and pass the
request context to each operation. For atomic changes, call `db.BeginTx`, use
`queries.WithTx(tx)` for every operation, and commit or roll back the transaction.
Queries do not begin transactions automatically.

This is an internal data-access layer, not authorization. Go handlers must require
an admin session before administration or watched-status writes and derive the
rating user ID from the authenticated personal session. Never grant admin access
based only on the household role stored in `users`.
Calendar reordering clears and assigns positions in one transaction, retaining entry IDs.

## Development checks

```sh
sqlc compile
go test ./...
go vet ./...
```
