# Screamtober

Screamtober is a web application for an annual October movie challenge: build a
31-movie watchlist, track viewing progress, and rate movies, with challenge history
organized by year.

Designed for personal use by 1–4 people, with public viewing available to everyone.
The current Go application serves a placeholder page with shared-token sign-in
for testing. Individual accounts, movie tracking, and database storage are still
to come. There is no public registration.

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
  -e AUTH_TOKEN -e AUTH_INSECURE_COOKIE=true screamtober
```

Open [localhost:8080](http://127.0.0.1:8080). Rebuild the image after making changes.
To stop the container, run `docker stop screamtober` in another terminal.

`AUTH_INSECURE_COOKIE=true` is only for local HTTP testing. For hosted HTTPS,
configure `AUTH_TOKEN` as a runtime secret and leave `AUTH_INSECURE_COOKIE` unset.

## Development checks

```sh
go test ./...
go vet ./...
```
