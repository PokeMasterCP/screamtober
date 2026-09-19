# AGENTS.md

## Project and product rules

Screamtober is a self-hosted October movie challenge: up to 31 picks per year,
shared viewing progress, and personal ratings. Preserve previous years as the
catalog grows. Watchlists may be incomplete and may repeat movies.

| Session | Permissions |
| --- | --- |
| Visitor | View public challenge data only |
| Personal (owner or member) | View challenges and submit or edit their own ratings |
| Admin | Manage movies, arrangements, and household access; no personal ratings |

- Enforce permissions on every relevant backend request. Public access excludes
  credentials, sessions, and account management; visitors cannot change data.
- Ratings are whole stars from 1–5, tied to each challenge entry. Average only
  submitted ratings. Saving a rating must atomically mark that entry watched for
  the household, preserving its first watched timestamp on subsequent saves.
- The admin provisions one owner and up to three members with random personal
  tokens distributed privately. There is no public registration. The owner label
  grants no administrative privileges through a personal session.
- `ADMIN_TOKEN` is a runtime administrator secret, never a rating credential.
  Store only personal token hashes. Replacing a token or disabling access revokes
  existing access while preserving profiles and ratings. Disabled profiles count
  toward the household limit.
- `/login` accepts both token types. Admin and personal sessions use separate
  cookies but are mutually exclusive in a browser. Admin sign-in revokes the
  current personal session; require admin sign-out before personal use. Enforce
  this on the server, including direct navigation and sign-in attempts.

## Stack and implementation

Use Go, SQLite, Goose migrations, sqlc-generated queries, server-rendered HTML/CSS,
and server-side TMDB integration. Package with Docker. Follow existing package,
error-handling, metadata-storage, and caching conventions.

- Keep changes small and focused. Separate request handling, business rules, and
  database access where the existing design supports it; avoid unnecessary
  abstractions, dependencies, and unrelated refactoring.
- Use request contexts for database and external API operations, and timeouts for
  external calls. Use parameterized SQL and transactions for atomic changes.
- Escape untrusted content with HTML templates. Use semantic HTML, accessible
  labels, and clear validation errors. Calendar arrangement is intentionally
  drag-and-drop only; carousel cards use posters without visible titles or descriptions.
- Keep TMDB credentials and authenticated calls on the server. Handle missing
  fields, timeouts, rate limits, and failures without exposing secrets or internal
  errors. Existing history should not depend on live TMDB requests. Check current
  TMDB requirements when changing attribution or API usage.
- The production site is live. Preserve existing data and applied Goose migrations;
  add migrations for schema changes. Never manually alter production schemas,
  recreate production databases, or delete databases at startup. Explain destructive
  changes and their consequences before implementation.
- Edit SQL in `queries/` and regenerate `internal/store/`; never edit generated code.
  Add database constraints for established invariants alongside application validation.

## Deployment and security

Support the same Docker image with either operator-managed HTTPS ingress or a
Cloudflare Tunnel. Keep code and documentation provider-neutral; do not add
provider-specific environment detection or requirements. Run one app instance.
Discuss scaling or storage changes before adding instances with independent SQLite files.

- The Go server serves HTTP. Hosted deployments require HTTPS termination and
  secure cookies; `AUTH_INSECURE_COOKIE=true` is for local HTTP development only.
- In both modes, the app remains responsible for authentication, authorization,
  input validation, session security, and CSRF protection. Never mutate state via GET.
- With `CLOUDFLARE_TUNNEL=true`, allow public ingress only through the tunnel and
  trust internal services with app access. Trust one valid IPv4 or IPv6
  `CF-Connecting-IP`; missing, malformed, or duplicate values fall back to the
  socket peer IP. Do not require proxy CIDR configuration.
- With tunnel mode unset or false, use the socket peer IP and ignore forwarded IP
  headers. Never infer trust from a header's presence. Tunnel mode does not create
  a tunnel or enable forwarded scheme trust. Operators configure ingress and edge
  protections; Cloudflare routing alone does not guarantee WAF, rate limiting, or
  bot protection.
- Keep SQLite private on a mounted persistent volume. `DATABASE_DIR` defaults to
  `/data` in Docker and `data` locally; open only `screamtober.db` directly inside
  it. Do not search for or select other database files. Deployment must verify the
  persistent mount.
- Keep `ADDR`, `DATABASE_DIR`, `CLOUDFLARE_TUNNEL`, `ADMIN_TOKEN`,
  `AUTH_INSECURE_COOKIE`, and `LOG_LEVEL` configurable at runtime; Docker values
  are overridable defaults. Supply secrets at runtime and keep them out of source
  control, logs, and public responses. Personal tokens are shown only when issued.
- Use volume snapshots where available and periodic SQLite-consistent portable
  backups. Account for journal/WAL state; verify restoration before relying on backups.

## Workflow and validation

Read relevant code, configuration, tests, and [DEVELOPMENT.md](DEVELOPMENT.md)
before changes. Use its documented commands and pinned tools. Preserve unrelated
work. Explain a short plan for larger tasks and discuss material changes to
architecture, dependencies, project structure, or data flow. Voice concrete
concerns and suggest simpler alternatives; continue routine authorized work
without unnecessary approval requests.

Create feature branches from up-to-date `main` and open PRs directly into `main`.
Use `<type>(service): summary` titles, such as `feat(watchlist): add viewing services`.
CI checks PRs and pushes to `main`; test changes in the temporary PR environment
before merging. There is no staging promotion step.

- Format changed Go code with `gofmt` and run relevant checks, including
  `go test ./...` for Go changes. Test behavior, especially authorization, year
  isolation, ratings, and data integrity; avoid tests that repeat implementation.
- For database changes, regenerate affected sqlc output and validate migrations
  against disposable databases, including upgrades from existing schemas.
  Never run destructive validation on production.
- For UI changes, verify affected pages, forms, validation errors, and relevant
  signed-in and visitor states.
- For deployment changes, verify both Docker modes: private tunnel ingress,
  ignored forwarded IP headers in normal mode, and persistent storage in both.
- Follow the logging and page conventions in DEVELOPMENT.md. Update documentation
  when behavior, configuration, setup, or deployment requirements change.
- At completion, summarize changes, checks, and remaining limitations or decisions.
  Distinguish checks that passed from checks that could not run.

## Documentation audience

- `README.md`: what the app does, requirements, setup, configuration, use, and
  essential operations (storage, backups, upgrades, troubleshooting). Keep logging
  guidance limited to finding logs, verbosity, and privacy when useful to operators.
- `DEVELOPMENT.md`: developer commands, implementation conventions, and logging
  details. Keep internal APIs, query mechanics, and test details out of the README.
- `AGENTS.md`: durable project rules and contributor workflow. Prefer concise,
  task-oriented guidance; avoid feature changelogs and repeated explanations.
